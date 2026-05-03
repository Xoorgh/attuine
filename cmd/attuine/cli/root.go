package cli

import (
	"errors"
	"fmt"
	"os"
	"sort"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"oxorg/attuine/internal/config"
	"oxorg/attuine/internal/docker"
	"oxorg/attuine/internal/state"
	"oxorg/attuine/internal/tui"
)

// errPartialSuccess signals that some operations succeeded and some failed.
var errPartialSuccess = errors.New("partial success")

var (
	cfgPath    string
	jsonFlag   bool
	repoFilter []string
	cfg        *config.Config
)

var rootCmd = &cobra.Command{
	Use:   "attuine",
	Short: "Dev orchestration tool for multi-repo projects",
	Long: `Attuine manages Docker Compose services, git repositories, and
development workflows across a multi-repo project.

Run without a subcommand to launch the interactive TUI.`,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		if cmd.Name() == "help" || cmd.Name() == "completion" || cmd.Name() == "man" {
			return nil
		}
		return loadConfig()
	},
	Run: func(cmd *cobra.Command, args []string) {
		if err := docker.CheckAvailable(); err != nil {
			fmt.Fprintf(os.Stderr, "warning: %v\n", err)
		}
		stateDir, _ := state.DefaultDir()
		tui.LoadTheme()
		tui.ApplyTheme()
		model := tui.New(cfg, stateDir)
		p := tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseCellMotion())
		if _, err := p.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
	},
}

func loadConfig() error {
	var path string
	if cfgPath != "" {
		path = cfgPath
	} else {
		wd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("getting working directory: %w", err)
		}
		found, err := config.Discover(wd)
		if err != nil {
			return err
		}
		path = found
	}
	var err error
	cfg, err = config.Load(path)
	return err
}

// filterRepos returns repo names filtered by --repo flags, or all repos sorted.
func filterRepos() []string {
	if len(repoFilter) > 0 {
		return repoFilter
	}
	var names []string
	for name := range cfg.Repos {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func init() {
	rootCmd.PersistentFlags().StringVar(&cfgPath, "config", "", "path to attuine.yml (default: auto-discover)")
	rootCmd.PersistentFlags().BoolVar(&jsonFlag, "json", false, "output JSON instead of human-readable text")
	rootCmd.PersistentFlags().StringArrayVar(&repoFilter, "repo", nil, "filter to specific repos (repeatable)")
}

// Execute runs the root command.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		if errors.Is(err, errPartialSuccess) {
			os.Exit(2)
		}
		os.Exit(1)
	}
}
