package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const fileName = "state.json"

type State struct {
	LastProfile string `json:"last_profile"`
}

// Load reads state from dir/state.json. Returns empty state if file doesn't exist.
func Load(dir string) (*State, error) {
	path := filepath.Join(dir, fileName)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &State{}, nil
		}
		return nil, fmt.Errorf("reading state: %w", err)
	}
	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("parsing state: %w", err)
	}
	return &s, nil
}

// Save writes state to dir/state.json, creating the directory if needed.
func (s *State) Save(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating state dir: %w", err)
	}
	data, err := json.Marshal(s)
	if err != nil {
		return fmt.Errorf("encoding state: %w", err)
	}
	return os.WriteFile(filepath.Join(dir, fileName), data, 0o644)
}

// DefaultDir returns the default state directory: ~/.local/state/attuine
func DefaultDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "state", "attuine"), nil
}
