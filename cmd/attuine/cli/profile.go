package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"oxorg/attuine/internal/docker"
	"oxorg/attuine/internal/runner"
)

const hookFailPrefix = "[exited with code "

var profileCmd = &cobra.Command{
	Use:   "profile",
	Short: "Manage Docker Compose profiles",
}

var profileListCmd = &cobra.Command{
	Use:   "list",
	Short: "List available profiles",
	RunE: func(cmd *cobra.Command, args []string) error {
		type profileEntry struct {
			Name            string   `json:"name"`
			ComposeProfiles []string `json:"compose_profiles"`
		}

		var profiles []profileEntry
		for _, p := range cfg.Profiles {
			profiles = append(profiles, profileEntry{
				Name:            p.Name,
				ComposeProfiles: p.Profiles,
			})
		}

		if jsonFlag {
			return printJSON(map[string]any{"profiles": profiles})
		}

		var lines []string
		for _, p := range profiles {
			lines = append(lines, fmt.Sprintf("%-15s [%s]", p.Name, strings.Join(p.ComposeProfiles, ", ")))
		}
		printText(lines)
		return nil
	},
}

var profileUpCmd = &cobra.Command{
	Use:   "up NAME",
	Short: "Bring up a profile's services",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		profileName := args[0]

		var profiles []string
		found := false
		for _, p := range cfg.Profiles {
			if p.Name == profileName {
				profiles = p.Profiles
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("profile %q not found", profileName)
		}

		compose := docker.NewCompose(cfg.ComposeFile, cfg.ComposeEnv, cfg.Dir)

		for _, hook := range cfg.Hooks.PreUp {
			if !jsonFlag {
				fmt.Printf("Running hook: %s\n", hook.Name)
			}
			ch, err := runner.RunHost(context.Background(), cfg.Dir, hook.Run)
			if err != nil {
				return fmt.Errorf("hook %s: %w", hook.Name, err)
			}
			var hookFailed bool
			for line := range ch {
				if strings.HasPrefix(line, hookFailPrefix) {
					hookFailed = true
				}
				if !jsonFlag {
					fmt.Println(line)
				}
			}
			if hookFailed {
				return fmt.Errorf("hook %s failed", hook.Name)
			}
		}

		if err := compose.Up(context.Background(), profiles); err != nil {
			return fmt.Errorf("compose up: %w", err)
		}

		if jsonFlag {
			return printJSON(map[string]any{"action": "up", "profile": profileName})
		}
		fmt.Printf("Profile %s is up.\n", profileName)
		return nil
	},
}

var profileDownCmd = &cobra.Command{
	Use:   "down",
	Short: "Bring down all services",
	RunE: func(cmd *cobra.Command, args []string) error {
		compose := docker.NewCompose(cfg.ComposeFile, cfg.ComposeEnv, cfg.Dir)
		if err := compose.Down(context.Background()); err != nil {
			return fmt.Errorf("compose down: %w", err)
		}

		if jsonFlag {
			return printJSON(map[string]any{"action": "down"})
		}
		fmt.Println("All services stopped.")
		return nil
	},
}

func init() {
	profileCmd.AddCommand(profileListCmd)
	profileCmd.AddCommand(profileUpCmd)
	profileCmd.AddCommand(profileDownCmd)
	rootCmd.AddCommand(profileCmd)
}
