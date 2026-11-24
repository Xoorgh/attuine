package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"oxorg/attuine/internal/docker"
	"oxorg/attuine/internal/ownership"
)

var (
	doctorFix      bool
	doctorFixImage string
)

// errProblemsFound makes doctor exit non-zero while problems remain, so it can
// gate a script or a hook.
var errProblemsFound = errors.New("problems found")

// Text output groups a repo's foreign paths by their leading directories, at
// most doctorGroupDepth of them, and lists at most doctorTextLimit groups;
// --json lists every path.
const (
	doctorGroupDepth = 3
	doctorTextLimit  = 10
)

type pathGroup struct {
	Prefix string
	Count  int
}

// groupPaths counts slash-separated paths by their first depth directory
// components, largest group first. A path with fewer directories than depth
// groups under its own parent, and a top-level file under itself.
func groupPaths(paths []string, depth int) []pathGroup {
	counts := map[string]int{}
	for _, p := range paths {
		parts := strings.Split(p, "/")
		n := len(parts) - 1 // directory components
		if n > depth {
			n = depth
		}
		key := p
		if n > 0 {
			key = strings.Join(parts[:n], "/") + "/"
		}
		counts[key]++
	}
	groups := make([]pathGroup, 0, len(counts))
	for k, c := range counts {
		groups = append(groups, pathGroup{Prefix: k, Count: c})
	}
	sort.Slice(groups, func(i, j int) bool {
		if groups[i].Count != groups[j].Count {
			return groups[i].Count > groups[j].Count
		}
		return groups[i].Prefix < groups[j].Prefix
	})
	return groups
}

type repoOwnership struct {
	Repo    string   `json:"repo"`
	Path    string   `json:"path"`
	Foreign []string `json:"foreign,omitempty"`
	Skipped string   `json:"skipped,omitempty"`
	Error   string   `json:"error,omitempty"`
}

type mountpointCheck struct {
	docker.NestedMountpoint
	Problem string `json:"problem,omitempty"`
}

type doctorReport struct {
	UID         int               `json:"uid"`
	Repos       []repoOwnership   `json:"repos"`
	Mountpoints []mountpointCheck `json:"mountpoints"`
	Fixed       []string          `json:"fixed,omitempty"`
	FixError    string            `json:"fix_error,omitempty"`
	Problems    int               `json:"problems"`
}

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Find files in the repos you do not own, and fix them",
	Long: `Doctor checks for the files a container running as root leaves in a
bind-mounted checkout, which later make git pull, checkout and host builds fail
with "Permission denied".

It reports, for each repo, the topmost paths not owned by you, and every
volume mountpoint nested inside a bind mount (for example a node_modules
volume over /app/node_modules) whose host directory is missing: the Docker
daemon would create that directory as root.

With --fix it chowns the reported paths back to you through a throwaway
container (no sudo needed) and creates the missing mountpoint directories.

It exits 1 while any problem remains.`,
	// Doctor prints its own report and errors; the exit code carries the rest.
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		if !ownership.Supported() {
			if jsonFlag {
				return printJSON(map[string]any{"supported": false})
			}
			fmt.Println("File ownership is not checked on this platform.")
			return nil
		}

		ctx := context.Background()
		uid, gid := os.Getuid(), os.Getgid()
		compose := docker.NewCompose(cfg.ComposeFile, cfg.ComposeEnv, cfg.Dir)

		report := runDoctorChecks(ctx, compose, uid)
		if doctorFix && report.Problems > 0 {
			fixed, err := fixDoctorProblems(ctx, report, uid, gid)
			report = runDoctorChecks(ctx, compose, uid)
			report.Fixed = fixed
			if err != nil {
				report.FixError = err.Error()
			}
		}

		if jsonFlag {
			if err := printJSON(report); err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
				return err
			}
		} else {
			printDoctorReport(report)
		}

		if report.Problems > 0 {
			return errProblemsFound
		}
		return nil
	},
}

func runDoctorChecks(ctx context.Context, compose *docker.Compose, uid int) doctorReport {
	report := doctorReport{UID: uid, Repos: []repoOwnership{}, Mountpoints: []mountpointCheck{}}

	for _, name := range filterRepos() {
		r, ok := cfg.Repos[name]
		if !ok {
			report.Repos = append(report.Repos, repoOwnership{Repo: name, Error: "not in attuine.yml"})
			report.Problems++
			continue
		}
		dir := r.Path
		if !filepath.IsAbs(dir) {
			dir = filepath.Join(cfg.Dir, dir)
		}
		entry := repoOwnership{Repo: name, Path: dir}
		if _, err := os.Stat(dir); errors.Is(err, os.ErrNotExist) {
			entry.Skipped = "not cloned"
			report.Repos = append(report.Repos, entry)
			continue
		}
		foreign, err := ownership.Scan(dir, uid)
		entry.Foreign = foreign
		if err != nil {
			entry.Error = err.Error()
			report.Problems++
		}
		report.Problems += len(foreign)
		report.Repos = append(report.Repos, entry)
	}

	// Mountpoints come from the compose file, not from the repos, so a --repo
	// filter skips them.
	if len(repoFilter) == 0 {
		mounts, err := compose.NestedMountpoints(ctx)
		if err != nil {
			report.Mountpoints = append(report.Mountpoints, mountpointCheck{Problem: err.Error()})
			report.Problems++
		}
		for _, m := range mounts {
			// An existing mountpoint's ownership is the repo scan's job;
			// checking it here as well would count one path twice.
			check := mountpointCheck{NestedMountpoint: m}
			if _, err := os.Lstat(m.HostPath); errors.Is(err, os.ErrNotExist) {
				check.Problem = "missing (the Docker daemon would create it as root)"
			} else if err != nil {
				check.Problem = err.Error()
			}
			if check.Problem != "" {
				report.Problems++
			}
			report.Mountpoints = append(report.Mountpoints, check)
		}
	}
	return report
}

// fixDoctorProblems chowns each repo's foreign paths, then creates the missing
// mountpoint directories (after the chown, since a missing directory's parent
// may have been one of the foreign paths). It returns what it fixed.
func fixDoctorProblems(ctx context.Context, report doctorReport, uid, gid int) ([]string, error) {
	var fixed []string
	var errs []error
	for _, r := range report.Repos {
		if len(r.Foreign) == 0 {
			continue
		}
		if err := ownership.Fix(ctx, doctorFixImage, r.Path, r.Foreign, uid, gid); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", r.Repo, err))
			continue
		}
		fixed = append(fixed, fmt.Sprintf("%s: chowned %d path(s)", r.Repo, len(r.Foreign)))
	}
	for _, m := range report.Mountpoints {
		if m.HostPath == "" {
			continue
		}
		if _, err := os.Lstat(m.HostPath); !errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err := os.MkdirAll(m.HostPath, 0o755); err != nil {
			errs = append(errs, fmt.Errorf("mountpoint %s: %w", m.HostPath, err))
			continue
		}
		fixed = append(fixed, "created "+m.HostPath)
	}
	return fixed, errors.Join(errs...)
}

func printDoctorReport(report doctorReport) {
	if report.FixError != "" {
		fmt.Fprintf(os.Stderr, "fix: %s\n", report.FixError)
	}
	var lines []string
	for _, f := range report.Fixed {
		lines = append(lines, "fixed: "+f)
	}
	if len(report.Fixed) > 0 {
		lines = append(lines, "")
	}

	for _, r := range report.Repos {
		switch {
		case r.Error != "" && len(r.Foreign) == 0:
			lines = append(lines, fmt.Sprintf("✗ %s: %s", r.Repo, r.Error))
		case r.Skipped != "":
			lines = append(lines, fmt.Sprintf("- %s: %s", r.Repo, r.Skipped))
		case len(r.Foreign) == 0:
			lines = append(lines, fmt.Sprintf("✓ %s", r.Repo))
		default:
			lines = append(lines, fmt.Sprintf("✗ %s: %d path(s) not owned by you (uid %d):", r.Repo, len(r.Foreign), report.UID))
			groups := groupPaths(r.Foreign, doctorGroupDepth)
			for i, g := range groups {
				if i == doctorTextLimit {
					lines = append(lines, fmt.Sprintf("    ... and %d more location(s) (--json lists every path)", len(groups)-doctorTextLimit))
					break
				}
				lines = append(lines, fmt.Sprintf("    %6d  %s", g.Count, g.Prefix))
			}
			if r.Error != "" {
				lines = append(lines, "    scan error: "+r.Error)
			}
		}
	}

	var bad []mountpointCheck
	for _, m := range report.Mountpoints {
		if m.Problem != "" {
			bad = append(bad, m)
		}
	}
	if len(bad) > 0 {
		lines = append(lines, "", "Nested volume mountpoints:")
		for _, m := range bad {
			if m.Service == "" {
				lines = append(lines, "  ✗ "+m.Problem)
				continue
			}
			lines = append(lines, fmt.Sprintf("  ✗ %s %s -> %s: %s", m.Service, m.Target, m.HostPath, m.Problem))
		}
	}

	if report.Problems > 0 {
		lines = append(lines, "")
		if doctorFix {
			lines = append(lines, fmt.Sprintf("%d problem(s) remain after --fix.", report.Problems))
		} else {
			lines = append(lines, fmt.Sprintf("%d problem(s) found. Run `attuine doctor --fix` to repair them.", report.Problems))
		}
	}
	printText(lines)
}

func init() {
	doctorCmd.Flags().BoolVar(&doctorFix, "fix", false, "chown foreign paths back to you and create missing mountpoints")
	doctorCmd.Flags().StringVar(&doctorFixImage, "fix-image", ownership.DefaultFixImage, "image --fix runs chown in")
	rootCmd.AddCommand(doctorCmd)
}
