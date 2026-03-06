package main

import (
	"flag"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"oxorg/attuine/internal/config"
	"oxorg/attuine/internal/docker"
	"oxorg/attuine/internal/tui"
)

func main() {
	configPath := flag.String("config", "", "path to attuine.yml (default: auto-discover)")
	flag.Parse()

	var cfgPath string
	if *configPath != "" {
		cfgPath = *configPath
	} else {
		wd, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		found, err := config.Discover(wd)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		cfgPath = found
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	if err := docker.CheckAvailable(); err != nil {
		fmt.Fprintf(os.Stderr, "warning: %v\n", err)
	}

	model := tui.New(cfg)
	p := tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseCellMotion())

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
