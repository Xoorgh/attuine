package cli

import (
	"fmt"
	"sort"

	"github.com/spf13/cobra"
)

var repoCmd = &cobra.Command{
	Use:   "repo",
	Short: "Manage configured repositories",
}

var repoListCmd = &cobra.Command{
	Use:   "list",
	Short: "List configured repositories",
	RunE: func(cmd *cobra.Command, args []string) error {
		type repoEntry struct {
			Name          string `json:"name"`
			Path          string `json:"path"`
			DefaultBranch string `json:"default_branch"`
		}

		var repos []repoEntry
		var names []string
		for name := range cfg.Repos {
			names = append(names, name)
		}
		sort.Strings(names)

		for _, name := range names {
			r := cfg.Repos[name]
			repos = append(repos, repoEntry{
				Name:          name,
				Path:          r.Path,
				DefaultBranch: r.DefaultBranch,
			})
		}

		if jsonFlag {
			return printJSON(map[string]any{"repos": repos})
		}

		var lines []string
		for _, r := range repos {
			lines = append(lines, fmt.Sprintf("%-15s %-20s %s", r.Name, r.Path, r.DefaultBranch))
		}
		printText(lines)
		return nil
	},
}

func init() {
	repoCmd.AddCommand(repoListCmd)
	rootCmd.AddCommand(repoCmd)
}
