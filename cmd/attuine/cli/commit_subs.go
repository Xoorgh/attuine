package cli

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"

	"oxorg/attuine/internal/git"
)

var commitSubsMsg string

var commitSubsCmd = &cobra.Command{
	Use:   "commit-subs",
	Short: "Stage and commit submodule pointer changes (requires layout: submodules)",
	RunE: func(cmd *cobra.Command, args []string) error {
		if cfg.Layout != "submodules" {
			return fmt.Errorf("commit-subs requires layout: submodules in config")
		}
		ctx := context.Background()
		parentDir := cfg.Dir

		hasParent := false
		for _, repo := range cfg.Repos {
			if repo.Path == "." {
				hasParent = true
				break
			}
		}
		if !hasParent {
			return fmt.Errorf("no parent repo configured (need a repo with path: .)")
		}

		var paths []string
		for _, repo := range cfg.Repos {
			if repo.Path == "." {
				continue
			}
			p := repo.Path
			if filepath.IsAbs(p) {
				if rel, err := filepath.Rel(parentDir, p); err == nil {
					p = rel
				}
			}
			paths = append(paths, p)
		}

		if err := git.Add(ctx, parentDir, paths...); err != nil {
			return fmt.Errorf("staging submodules: %w", err)
		}

		clean, err := git.IsClean(ctx, parentDir)
		if err != nil {
			return fmt.Errorf("checking status: %w", err)
		}
		if clean {
			if jsonFlag {
				return printJSON(map[string]any{"action": "none", "message": "nothing to commit"})
			}
			fmt.Println("Nothing to commit — submodules already up to date.")
			return nil
		}

		msg := commitSubsMsg
		if msg == "" {
			msg = "Update submodule pointers"
		}

		if err := git.Commit(ctx, parentDir, msg); err != nil {
			return fmt.Errorf("committing: %w", err)
		}

		if jsonFlag {
			return printJSON(map[string]any{"action": "committed", "message": msg})
		}
		fmt.Printf("Committed: %s\n", msg)
		return nil
	},
}

func init() {
	commitSubsCmd.Flags().StringVarP(&commitSubsMsg, "message", "m", "", `commit message (default: "Update submodule pointers")`)
	rootCmd.AddCommand(commitSubsCmd)
}
