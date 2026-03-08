package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"oxorg/attuine/internal/git"
)

var syncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Sync all repos: fetch, checkout default branch, pull",
	RunE: func(cmd *cobra.Command, args []string) error {
		type syncResult struct {
			Repo   string `json:"repo"`
			Action string `json:"action"`
			Branch string `json:"branch,omitempty"`
			Output string `json:"output,omitempty"`
			Reason string `json:"reason,omitempty"`
		}

		repos := filterRepos()
		var results []syncResult
		synced, skipped := 0, 0

		for _, name := range repos {
			r := cfg.Repos[name]
			dir := r.Path
			if !filepath.IsAbs(dir) {
				dir = filepath.Join(cfg.Dir, dir)
			}
			ctx := context.Background()

			if err := git.Fetch(ctx, dir); err != nil {
				if !jsonFlag {
					fmt.Fprintf(os.Stderr, "⚠ %s: fetch warning: %v\n", name, err)
				}
			}

			clean, err := git.IsClean(ctx, dir)
			if err != nil {
				results = append(results, syncResult{Repo: name, Action: "skipped", Reason: err.Error()})
				skipped++
				if !jsonFlag {
					fmt.Fprintf(os.Stderr, "⚠ %s: error: %v\n", name, err)
				}
				continue
			}
			if !clean {
				results = append(results, syncResult{Repo: name, Action: "skipped", Reason: "dirty"})
				skipped++
				if !jsonFlag {
					fmt.Fprintf(os.Stderr, "⚠ %s: dirty, skipped\n", name)
				}
				continue
			}

			if err := git.Checkout(ctx, dir, r.DefaultBranch); err != nil {
				results = append(results, syncResult{Repo: name, Action: "skipped", Reason: fmt.Sprintf("checkout: %v", err)})
				skipped++
				if !jsonFlag {
					fmt.Fprintf(os.Stderr, "⚠ %s: checkout error: %v\n", name, err)
				}
				continue
			}

			out, err := git.Pull(ctx, dir)
			if err != nil {
				results = append(results, syncResult{Repo: name, Action: "skipped", Reason: fmt.Sprintf("pull: %v", err)})
				skipped++
				if !jsonFlag {
					fmt.Fprintf(os.Stderr, "⚠ %s: pull error: %v\n", name, err)
				}
				continue
			}

			results = append(results, syncResult{Repo: name, Action: "synced", Branch: r.DefaultBranch, Output: out})
			synced++
			if !jsonFlag {
				fmt.Printf("✓ %s: %s\n", name, out)
			}
		}

		if jsonFlag {
			return printJSON(map[string]any{
				"results": results,
				"summary": map[string]int{
					"synced":  synced,
					"skipped": skipped,
					"total":   len(repos),
				},
			})
		}

		fmt.Printf("\nSynced %d/%d repos", synced, len(repos))
		if skipped > 0 {
			fmt.Printf(" (%d skipped)", skipped)
		}
		fmt.Println()

		if skipped > 0 {
			return errPartialSuccess
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(syncCmd)
}
