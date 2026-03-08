package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"oxorg/attuine/internal/docker"
)

var serviceCmd = &cobra.Command{
	Use:   "service",
	Short: "Manage Docker Compose services",
}

var serviceListCmd = &cobra.Command{
	Use:   "list",
	Short: "List Docker services with status",
	RunE: func(cmd *cobra.Command, args []string) error {
		compose := docker.NewCompose(cfg.ComposeFile, cfg.ComposeEnv, cfg.Dir)
		statuses, err := compose.Status(context.Background())
		if err != nil {
			return fmt.Errorf("getting service status: %w", err)
		}

		type serviceEntry struct {
			Name   string   `json:"name"`
			State  string   `json:"state"`
			Health string   `json:"health,omitempty"`
			Ports  []string `json:"ports,omitempty"`
		}

		var services []serviceEntry
		for _, s := range statuses {
			services = append(services, serviceEntry{
				Name:   s.Service,
				State:  s.State,
				Health: s.Health,
				Ports:  s.Ports,
			})
		}

		if jsonFlag {
			return printJSON(map[string]any{"services": services})
		}

		var lines []string
		for _, s := range services {
			ports := ""
			if len(s.Ports) > 0 {
				ports = strings.Join(s.Ports, ",")
			}
			lines = append(lines, fmt.Sprintf("%-15s %-10s %s", s.Name, s.State, ports))
		}
		printText(lines)
		return nil
	},
}

func init() {
	serviceCmd.AddCommand(serviceListCmd)
	rootCmd.AddCommand(serviceCmd)
}
