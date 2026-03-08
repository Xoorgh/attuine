package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"oxorg/attuine/internal/git"
)

var branchCmd = &cobra.Command{
	Use:   "branch",
	Short: "Manage branches across repositories",
}

var branchCreateCmd = &cobra.Command{
	Use:   "create NAME",
	Short: "Create a new branch in repositories",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		branchName := args[0]
		repos := filterRepos()

		type branchResult struct {
			Repo   string `json:"repo"`
			Action string `json:"action"`
			Branch string `json:"branch"`
			Error  string `json:"error,omitempty"`
		}

		var results []branchResult
		var errCount int
		for _, name := range repos {
			r := cfg.Repos[name]
			dir := r.Path
			if !filepath.IsAbs(dir) {
				dir = filepath.Join(cfg.Dir, dir)
			}

			if err := git.CreateBranch(context.Background(), dir, branchName); err != nil {
				results = append(results, branchResult{Repo: name, Action: "error", Branch: branchName, Error: err.Error()})
				errCount++
				if !jsonFlag {
					fmt.Fprintf(os.Stderr, "✕ %s: %v\n", name, err)
				}
				continue
			}
			results = append(results, branchResult{Repo: name, Action: "created", Branch: branchName})
			if !jsonFlag {
				fmt.Printf("✓ %s: created %s\n", name, branchName)
			}
		}

		if jsonFlag {
			if err := printJSON(map[string]any{"results": results}); err != nil {
				return err
			}
		}
		if errCount > 0 && errCount < len(repos) {
			return errPartialSuccess
		}
		return nil
	},
}

var branchListCmd = &cobra.Command{
	Use:   "list",
	Short: "List current branch for each repository",
	RunE: func(cmd *cobra.Command, args []string) error {
		repos := filterRepos()

		type branchEntry struct {
			Name   string `json:"name"`
			Branch string `json:"branch"`
			Error  string `json:"error,omitempty"`
		}

		var entries []branchEntry
		for _, name := range repos {
			r := cfg.Repos[name]
			dir := r.Path
			if !filepath.IsAbs(dir) {
				dir = filepath.Join(cfg.Dir, dir)
			}

			branch, err := git.CurrentBranch(context.Background(), dir)
			if err != nil {
				entries = append(entries, branchEntry{Name: name, Error: err.Error()})
				continue
			}
			entries = append(entries, branchEntry{Name: name, Branch: branch})
		}

		if jsonFlag {
			return printJSON(map[string]any{"repos": entries})
		}

		var lines []string
		for _, e := range entries {
			if e.Error != "" {
				lines = append(lines, fmt.Sprintf("%-15s error: %s", e.Name, e.Error))
			} else {
				lines = append(lines, fmt.Sprintf("%-15s %s", e.Name, e.Branch))
			}
		}
		printText(lines)
		return nil
	},
}

var branchCheckoutCmd = &cobra.Command{
	Use:   "checkout NAME",
	Short: "Checkout an existing branch in repositories",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		branchName := args[0]
		repos := filterRepos()

		type checkoutResult struct {
			Repo   string `json:"repo"`
			Action string `json:"action"`
			Branch string `json:"branch"`
			Error  string `json:"error,omitempty"`
		}

		var results []checkoutResult
		var errCount int
		for _, name := range repos {
			r := cfg.Repos[name]
			dir := r.Path
			if !filepath.IsAbs(dir) {
				dir = filepath.Join(cfg.Dir, dir)
			}

			if err := git.Checkout(context.Background(), dir, branchName); err != nil {
				results = append(results, checkoutResult{Repo: name, Action: "error", Branch: branchName, Error: err.Error()})
				errCount++
				if !jsonFlag {
					fmt.Fprintf(os.Stderr, "✕ %s: %v\n", name, err)
				}
				continue
			}
			results = append(results, checkoutResult{Repo: name, Action: "checked_out", Branch: branchName})
			if !jsonFlag {
				fmt.Printf("✓ %s: checked out %s\n", name, branchName)
			}
		}

		if jsonFlag {
			if err := printJSON(map[string]any{"results": results}); err != nil {
				return err
			}
		}
		if errCount > 0 && errCount < len(repos) {
			return errPartialSuccess
		}
		return nil
	},
}

func init() {
	branchCmd.AddCommand(branchCreateCmd)
	branchCmd.AddCommand(branchListCmd)
	branchCmd.AddCommand(branchCheckoutCmd)
	rootCmd.AddCommand(branchCmd)
}
