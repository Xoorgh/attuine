package state

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMissingFile(t *testing.T) {
	s, err := Load(t.TempDir())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.LastProfile != "" {
		t.Errorf("expected empty LastProfile, got %q", s.LastProfile)
	}
}

func TestSaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	s := &State{LastProfile: "Core"}
	if err := s.Save(dir); err != nil {
		t.Fatalf("save error: %v", err)
	}

	loaded, err := Load(dir)
	if err != nil {
		t.Fatalf("load error: %v", err)
	}
	if loaded.LastProfile != "Core" {
		t.Errorf("expected Core, got %q", loaded.LastProfile)
	}
}

func TestSaveCreatesDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "dir")
	s := &State{LastProfile: "Full"}
	if err := s.Save(dir); err != nil {
		t.Fatalf("save error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "state.json")); err != nil {
		t.Errorf("state file not created: %v", err)
	}
}
