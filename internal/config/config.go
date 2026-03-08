package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

const FileName = "attuine.yml"

type Config struct {
	ComposeFile string             `yaml:"compose_file"`
	ComposeEnv  string             `yaml:"compose_env"`
	Hooks       Hooks              `yaml:"hooks"`
	Profiles    []Profile          `yaml:"profiles"`
	Projects    map[string]Project `yaml:"projects"`
	Repos       map[string]Repo    `yaml:"repos"`
	Dir         string             `yaml:"-"`
}

type Hooks struct {
	PreUp []Hook `yaml:"pre_up"`
}

type Hook struct {
	Name string `yaml:"name"`
	Run  string `yaml:"run"`
}

type Profile struct {
	Name     string   `yaml:"name"`
	Profiles []string `yaml:"profiles"`
}

type Project struct {
	Path     string    `yaml:"path"`
	Commands []Command `yaml:"commands"`
}

type Repo struct {
	Path          string `yaml:"path"`
	DefaultBranch string `yaml:"default_branch"`
}

type Command struct {
	Name        string `yaml:"name"`
	Run         string `yaml:"run"`
	Service     string `yaml:"service"`
	Interactive bool   `yaml:"interactive"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	if cfg.ComposeFile == "" {
		return nil, fmt.Errorf("compose_file is required in %s", path)
	}

	for name, repo := range cfg.Repos {
		if repo.DefaultBranch == "" {
			repo.DefaultBranch = "master"
			cfg.Repos[name] = repo
		}
	}

	cfg.Dir = filepath.Dir(path)
	return &cfg, nil
}

func Discover(startDir string) (string, error) {
	dir, err := filepath.Abs(startDir)
	if err != nil {
		return "", err
	}

	for {
		candidate := filepath.Join(dir, FileName)
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("%s not found (searched from %s to /)", FileName, startDir)
		}
		dir = parent
	}
}
