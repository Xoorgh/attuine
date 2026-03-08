package cli

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"

	"oxorg/attuine/internal/git"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show git status for all repositories",
	RunE: func(cmd *cobra.Command, args []string) error {
		type repoStatus struct {
			Name   string `json:"name"`
			Branch string `json:"branch"`
			Clean  bool   `json:"clean"`
			Ahead  int    `json:"ahead"`
			Behind int    `json:"behind"`
			Error  string `json:"error,omitempty"`
		}

		repos := filterRepos()
		var statuses []repoStatus

		for _, name := range repos {
			r := cfg.Repos[name]
			dir := r.Path
			if !filepath.IsAbs(dir) {
				dir = filepath.Join(cfg.Dir, dir)
			}

			s, err := git.Status(context.Background(), dir)
			if err != nil {
				statuses = append(statuses, repoStatus{Name: name, Error: err.Error()})
				continue
			}
			statuses = append(statuses, repoStatus{
				Name:   name,
				Branch: s.Branch,
				Clean:  s.Clean,
				Ahead:  s.Ahead,
				Behind: s.Behind,
			})
		}

		if jsonFlag {
			return printJSON(map[string]any{"repos": statuses})
		}

		var lines []string
		for _, s := range statuses {
			if s.Error != "" {
				lines = append(lines, fmt.Sprintf("%-15s error: %s", s.Name, s.Error))
				continue
			}
			cleanStr := "clean"
			if !s.Clean {
				cleanStr = "dirty"
			}
			extra := ""
			if s.Ahead > 0 {
				extra += fmt.Sprintf(" ↑%d", s.Ahead)
			}
			if s.Behind > 0 {
				extra += fmt.Sprintf(" ↓%d", s.Behind)
			}
			lines = append(lines, fmt.Sprintf("%-15s %-15s %-6s%s", s.Name, s.Branch, cleanStr, extra))
		}
		printText(lines)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(statusCmd)
}
