package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"oxorg/attuine/internal/config"
)

func TestLoad_FullConfig(t *testing.T) {
	yaml := `
compose_file: dev/docker-compose.yml
compose_env: dev/.env

hooks:
  pre_up:
    - name: Resolve versions
      run: ./dev/resolve-versions.sh

profiles:
  - name: Core
    profiles: [core]
  - name: Full Stack
    profiles: [full]

projects:
  myapp:
    path: ./myapp
    commands:
      - name: Test
        run: make test
      - name: Console
        run: bin/rails console
        service: myapp
        interactive: true
`
	dir := t.TempDir()
	err := os.WriteFile(filepath.Join(dir, "attuine.yml"), []byte(yaml), 0644)
	if err != nil {
		t.Fatal(err)
	}

	cfg, err := config.Load(filepath.Join(dir, "attuine.yml"))
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.ComposeFile != "dev/docker-compose.yml" {
		t.Errorf("ComposeFile = %q, want %q", cfg.ComposeFile, "dev/docker-compose.yml")
	}
	if cfg.ComposeEnv != "dev/.env" {
		t.Errorf("ComposeEnv = %q, want %q", cfg.ComposeEnv, "dev/.env")
	}
	if len(cfg.Hooks.PreUp) != 1 {
		t.Fatalf("len(Hooks.PreUp) = %d, want 1", len(cfg.Hooks.PreUp))
	}
	if cfg.Hooks.PreUp[0].Name != "Resolve versions" {
		t.Errorf("Hooks.PreUp[0].Name = %q, want %q", cfg.Hooks.PreUp[0].Name, "Resolve versions")
	}
	if len(cfg.Profiles) != 2 {
		t.Fatalf("len(Profiles) = %d, want 2", len(cfg.Profiles))
	}
	if cfg.Profiles[0].Name != "Core" {
		t.Errorf("Profiles[0].Name = %q, want %q", cfg.Profiles[0].Name, "Core")
	}
	if len(cfg.Profiles[0].Profiles) != 1 || cfg.Profiles[0].Profiles[0] != "core" {
		t.Errorf("Profiles[0].Profiles = %v, want [core]", cfg.Profiles[0].Profiles)
	}

	proj, ok := cfg.Projects["myapp"]
	if !ok {
		t.Fatal("Projects[myapp] not found")
	}
	if proj.Path != "./myapp" {
		t.Errorf("proj.Path = %q, want %q", proj.Path, "./myapp")
	}
	if len(proj.Commands) != 2 {
		t.Fatalf("len(proj.Commands) = %d, want 2", len(proj.Commands))
	}
	if proj.Commands[0].Name != "Test" {
		t.Errorf("Commands[0].Name = %q, want %q", proj.Commands[0].Name, "Test")
	}
	if proj.Commands[1].Service != "myapp" {
		t.Errorf("Commands[1].Service = %q, want %q", proj.Commands[1].Service, "myapp")
	}
	if !proj.Commands[1].Interactive {
		t.Error("Commands[1].Interactive = false, want true")
	}
}

func TestLoad_MissingComposeFile(t *testing.T) {
	yaml := `
profiles:
  - name: Core
    profiles: [core]
`
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "attuine.yml"), []byte(yaml), 0644)

	_, err := config.Load(filepath.Join(dir, "attuine.yml"))
	if err == nil {
		t.Fatal("Load() should error when compose_file is missing")
	}
}

func TestLoad_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "attuine.yml"), []byte("{{invalid"), 0644)

	_, err := config.Load(filepath.Join(dir, "attuine.yml"))
	if err == nil {
		t.Fatal("Load() should error on invalid YAML")
	}
}

func TestLoad_WithRepos(t *testing.T) {
	yaml := `
compose_file: dev/docker-compose.yml

repos:
  myapp:
    path: ./myapp
    default_branch: main
  lib:
    path: ./lib
`
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "attuine.yml"), []byte(yaml), 0644)

	cfg, err := config.Load(filepath.Join(dir, "attuine.yml"))
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if len(cfg.Repos) != 2 {
		t.Fatalf("len(Repos) = %d, want 2", len(cfg.Repos))
	}

	myapp, ok := cfg.Repos["myapp"]
	if !ok {
		t.Fatal("Repos[myapp] not found")
	}
	if myapp.Path != "./myapp" {
		t.Errorf("myapp.Path = %q, want %q", myapp.Path, "./myapp")
	}
	if myapp.DefaultBranch != "main" {
		t.Errorf("myapp.DefaultBranch = %q, want %q", myapp.DefaultBranch, "main")
	}

	lib := cfg.Repos["lib"]
	if lib.DefaultBranch != "master" {
		t.Errorf("lib.DefaultBranch = %q, want %q (default)", lib.DefaultBranch, "master")
	}
}

func TestDiscover(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "attuine.yml"), []byte("compose_file: dc.yml\n"), 0644)

	nested := filepath.Join(root, "a", "b", "c")
	os.MkdirAll(nested, 0755)

	found, err := config.Discover(nested)
	if err != nil {
		t.Fatalf("Discover() error: %v", err)
	}
	expected := filepath.Join(root, "attuine.yml")
	if found != expected {
		t.Errorf("Discover() = %q, want %q", found, expected)
	}
}

func TestDiscover_NotFound(t *testing.T) {
	dir := t.TempDir()
	_, err := config.Discover(dir)
	if err == nil {
		t.Fatal("Discover() should error when no attuine.yml found")
	}
}
