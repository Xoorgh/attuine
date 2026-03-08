package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/spf13/cobra/doc"
)

var manCmd = &cobra.Command{
	Use:    "man DIRECTORY",
	Short:  "Generate man pages",
	Args:   cobra.ExactArgs(1),
	Hidden: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		dir := args[0]
		header := &doc.GenManHeader{
			Title:   "ATTUINE",
			Section: "1",
			Source:  "Attuine",
			Manual:  "Attuine Manual",
		}
		if err := doc.GenManTree(rootCmd, header, dir); err != nil {
			return fmt.Errorf("generating man pages: %w", err)
		}
		fmt.Printf("Man pages generated in %s\n", dir)
		return nil
	},
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		return nil // skip config loading
	},
}

func init() {
	rootCmd.AddCommand(manCmd)
}
