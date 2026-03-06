package docker

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

type Compose struct {
	file    string
	envFile string
	dir     string
}

func NewCompose(composeFile, envFile, dir string) *Compose {
	file := composeFile
	if !filepath.IsAbs(file) {
		file = filepath.Join(dir, file)
	}
	env := envFile
	if env != "" && !filepath.IsAbs(env) {
		env = filepath.Join(dir, env)
	}
	return &Compose{file: file, envFile: env, dir: dir}
}

func (c *Compose) BuildArgs(args ...string) []string {
	base := []string{"compose", "-f", c.file}
	if c.envFile != "" {
		base = append(base, "--env-file", c.envFile)
	}
	return append(base, args...)
}

type ServiceStatus struct {
	Service string
	State   string
	Health  string
	Ports   []string
}

type psEntry struct {
	Name       string      `json:"Name"`
	Service    string      `json:"Service"`
	State      string      `json:"State"`
	Health     string      `json:"Health"`
	Publishers []publisher `json:"Publishers"`
}

type publisher struct {
	PublishedPort int `json:"PublishedPort"`
}

func ParseStatus(lines []string) ([]ServiceStatus, error) {
	var statuses []ServiceStatus
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var entry psEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			return nil, fmt.Errorf("parsing status JSON: %w", err)
		}
		var ports []string
		for _, p := range entry.Publishers {
			if p.PublishedPort > 0 {
				port := strconv.Itoa(p.PublishedPort)
				found := false
				for _, existing := range ports {
					if existing == port {
						found = true
						break
					}
				}
				if !found {
					ports = append(ports, port)
				}
			}
		}
		statuses = append(statuses, ServiceStatus{
			Service: entry.Service,
			State:   entry.State,
			Health:  entry.Health,
			Ports:   ports,
		})
	}
	return statuses, nil
}

func (c *Compose) Status(ctx context.Context) ([]ServiceStatus, error) {
	args := c.BuildArgs("ps", "--format", "json")
	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Dir = c.dir
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("docker compose ps: %w", err)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return nil, nil
	}
	return ParseStatus(lines)
}

func (c *Compose) Up(ctx context.Context, profiles []string, services ...string) error {
	var args []string
	for _, p := range profiles {
		args = append(args, "--profile", p)
	}
	args = append(args, "up", "-d")
	args = append(args, services...)
	cmd := exec.CommandContext(ctx, "docker", c.BuildArgs(args...)...)
	cmd.Dir = c.dir
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("docker compose up: %s: %w", string(out), err)
	}
	return nil
}

func (c *Compose) Down(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "docker", c.BuildArgs("down")...)
	cmd.Dir = c.dir
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("docker compose down: %s: %w", string(out), err)
	}
	return nil
}

func (c *Compose) Stop(ctx context.Context, services ...string) error {
	stopArgs := append([]string{"stop"}, services...)
	cmd := exec.CommandContext(ctx, "docker", c.BuildArgs(stopArgs...)...)
	cmd.Dir = c.dir
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("docker compose stop: %s: %w", string(out), err)
	}
	return nil
}

func (c *Compose) Build(ctx context.Context, services ...string) error {
	buildArgs := append([]string{"build"}, services...)
	cmd := exec.CommandContext(ctx, "docker", c.BuildArgs(buildArgs...)...)
	cmd.Dir = c.dir
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("docker compose build: %s: %w", string(out), err)
	}
	return nil
}

func (c *Compose) Logs(ctx context.Context, service string) (<-chan string, context.CancelFunc) {
	ctx, cancel := context.WithCancel(ctx)
	ch := make(chan string, 64)

	go func() {
		defer close(ch)
		cmd := exec.CommandContext(ctx, "docker", c.BuildArgs("logs", "-f", "--no-log-prefix", service)...)
		cmd.Dir = c.dir
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			return
		}
		cmd.Stderr = cmd.Stdout
		if err := cmd.Start(); err != nil {
			return
		}
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			select {
			case ch <- scanner.Text():
			case <-ctx.Done():
				return
			}
		}
		cmd.Wait()
	}()

	return ch, cancel
}

func (c *Compose) Exec(ctx context.Context, service string, command string) (<-chan string, context.CancelFunc) {
	ctx, cancel := context.WithCancel(ctx)
	ch := make(chan string, 64)

	go func() {
		defer close(ch)
		shellArgs := []string{"exec", "-T", service, "sh", "-c", command}
		cmd := exec.CommandContext(ctx, "docker", c.BuildArgs(shellArgs...)...)
		cmd.Dir = c.dir
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			return
		}
		cmd.Stderr = cmd.Stdout
		if err := cmd.Start(); err != nil {
			return
		}
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			select {
			case ch <- scanner.Text():
			case <-ctx.Done():
				return
			}
		}
		cmd.Wait()
	}()

	return ch, cancel
}

func (c *Compose) ExecInteractive(ctx context.Context, service string, command string) *exec.Cmd {
	shellArgs := []string{"exec", service, "sh", "-c", command}
	cmd := exec.CommandContext(ctx, "docker", c.BuildArgs(shellArgs...)...)
	cmd.Dir = c.dir
	return cmd
}

func (c *Compose) Shell(ctx context.Context, service string) *exec.Cmd {
	shellArgs := []string{"exec", service, "sh"}
	cmd := exec.CommandContext(ctx, "docker", c.BuildArgs(shellArgs...)...)
	cmd.Dir = c.dir
	return cmd
}

func CheckAvailable() error {
	cmd := exec.Command("docker", "compose", "version")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker compose not available: %w", err)
	}
	return nil
}
