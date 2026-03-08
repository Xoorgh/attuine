package git

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// RepoStatus holds the current state of a git repository.
type RepoStatus struct {
	Branch string
	Clean  bool
	Ahead  int
	Behind int
}

// CurrentBranch returns the current branch name of the repo at dir.
func CurrentBranch(ctx context.Context, dir string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--abbrev-ref", "HEAD")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git rev-parse: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// IsClean returns true if the working tree has no uncommitted changes.
func IsClean(ctx context.Context, dir string) (bool, error) {
	cmd := exec.CommandContext(ctx, "git", "status", "--porcelain")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return false, fmt.Errorf("git status: %w", err)
	}
	return strings.TrimSpace(string(out)) == "", nil
}

// Fetch runs git fetch in the repo at dir.
func Fetch(ctx context.Context, dir string) error {
	cmd := exec.CommandContext(ctx, "git", "fetch")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git fetch: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

// Pull runs git pull in the repo at dir and returns the output.
func Pull(ctx context.Context, dir string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "pull")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("git pull: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return strings.TrimSpace(string(out)), nil
}

// Checkout checks out the given branch in the repo at dir.
func Checkout(ctx context.Context, dir string, branch string) error {
	cmd := exec.CommandContext(ctx, "git", "checkout", branch)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git checkout %s: %s: %w", branch, strings.TrimSpace(string(out)), err)
	}
	return nil
}

// CreateBranch creates and checks out a new branch.
func CreateBranch(ctx context.Context, dir string, name string) error {
	cmd := exec.CommandContext(ctx, "git", "checkout", "-b", name)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git checkout -b %s: %s: %w", name, strings.TrimSpace(string(out)), err)
	}
	return nil
}

// Log returns the last n commit summaries (one-line format).
func Log(ctx context.Context, dir string, n int) ([]string, error) {
	cmd := exec.CommandContext(ctx, "git", "log", "--oneline", "-n", strconv.Itoa(n))
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git log: %w", err)
	}
	raw := strings.TrimSpace(string(out))
	if raw == "" {
		return nil, nil
	}
	return strings.Split(raw, "\n"), nil
}

// Add stages files in the repo at dir.
func Add(ctx context.Context, dir string, paths ...string) error {
	args := append([]string{"add"}, paths...)
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git add: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

// Commit creates a commit with the given message.
func Commit(ctx context.Context, dir string, message string) error {
	cmd := exec.CommandContext(ctx, "git", "commit", "-m", message)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git commit: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

// AheadBehind returns how many commits the current branch is ahead/behind
// its upstream tracking branch. Returns (0, 0) if no upstream is configured.
func AheadBehind(ctx context.Context, dir string) (ahead, behind int, err error) {
	cmd := exec.CommandContext(ctx, "git", "rev-list", "--left-right", "--count", "HEAD...@{upstream}")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		// No upstream configured
		return 0, 0, nil
	}
	parts := strings.Fields(strings.TrimSpace(string(out)))
	if len(parts) != 2 {
		return 0, 0, nil
	}
	a, _ := strconv.Atoi(parts[0])
	b, _ := strconv.Atoi(parts[1])
	return a, b, nil
}

// Status returns the full status of a repo.
func Status(ctx context.Context, dir string) (*RepoStatus, error) {
	branch, err := CurrentBranch(ctx, dir)
	if err != nil {
		return nil, err
	}
	clean, err := IsClean(ctx, dir)
	if err != nil {
		return nil, err
	}
	ahead, behind, _ := AheadBehind(ctx, dir)

	return &RepoStatus{
		Branch: branch,
		Clean:  clean,
		Ahead:  ahead,
		Behind: behind,
	}, nil
}
