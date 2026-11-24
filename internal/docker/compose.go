package docker

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
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

// command returns a docker invocation of `compose <args>` that runs in the
// config directory with HOST_UID and HOST_GID set (see HostUserEnv).
func (c *Compose) command(ctx context.Context, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "docker", c.BuildArgs(args...)...)
	cmd.Dir = c.dir
	cmd.Env = HostUserEnv(os.Environ(), os.Getuid(), os.Getgid())
	return cmd
}

// ExecArgs builds the `exec` arguments for running command through `sh -c` in
// service. interactive keeps a TTY attached; otherwise -T disables it so the
// output can be streamed. A non-empty user is passed as -u.
func ExecArgs(service, user string, interactive bool, command string) []string {
	args := []string{"exec"}
	if !interactive {
		args = append(args, "-T")
	}
	if user != "" {
		args = append(args, "-u", user)
	}
	return append(args, service, "sh", "-c", command)
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

// ListServices returns all service names defined in the compose file.
// It passes --profile for each known profile so profiled services are included.
func (c *Compose) ListServices(ctx context.Context, profiles []string) ([]string, error) {
	var profileArgs []string
	for _, p := range profiles {
		profileArgs = append(profileArgs, "--profile", p)
	}
	profileArgs = append(profileArgs, "config", "--services")
	cmd := c.command(ctx, profileArgs...)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("docker compose config --services: %w", err)
	}
	var services []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			services = append(services, line)
		}
	}
	return services, nil
}

func (c *Compose) Status(ctx context.Context) ([]ServiceStatus, error) {
	cmd := c.command(ctx, "ps", "--format", "json")
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
	args = append(args, "up", "-d", "--build")
	args = append(args, services...)
	cmd := c.command(ctx, args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("docker compose up: %s: %w", string(out), err)
	}
	return nil
}

// UpStream starts "docker compose up -d --build" and streams combined
// stdout/stderr line by line. The returned channel is closed when the
// process exits.
func (c *Compose) UpStream(ctx context.Context, profiles []string, services ...string) (<-chan string, error) {
	var args []string
	for _, p := range profiles {
		args = append(args, "--profile", p)
	}
	args = append(args, "up", "-d", "--build")
	args = append(args, services...)
	cmd := c.command(ctx, args...)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("docker compose up: %w", err)
	}
	cmd.Stderr = cmd.Stdout

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("docker compose up: %w", err)
	}

	ch := make(chan string, 64)
	go func() {
		defer close(ch)
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			select {
			case ch <- scanner.Text():
			case <-ctx.Done():
				return
			}
		}
		if err := cmd.Wait(); err != nil {
			select {
			case ch <- fmt.Sprintf("[compose up failed: %v]", err):
			case <-ctx.Done():
			}
		}
	}()

	return ch, nil
}

func (c *Compose) Down(ctx context.Context, profiles []string) error {
	var args []string
	for _, p := range profiles {
		args = append(args, "--profile", p)
	}
	args = append(args, "down")
	cmd := c.command(ctx, args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("docker compose down: %s: %w", string(out), err)
	}
	return nil
}

func (c *Compose) Stop(ctx context.Context, services ...string) error {
	stopArgs := append([]string{"stop"}, services...)
	cmd := c.command(ctx, stopArgs...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("docker compose stop: %s: %w", string(out), err)
	}
	return nil
}

func (c *Compose) Build(ctx context.Context, services ...string) error {
	buildArgs := append([]string{"build"}, services...)
	cmd := c.command(ctx, buildArgs...)
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
		cmd := c.command(ctx, "logs", "-f", "--no-log-prefix", service)
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

// Exec runs command in service and streams its combined output. user, when not
// empty, overrides the service's configured user for this one command.
func (c *Compose) Exec(ctx context.Context, service, user, command string) (<-chan string, context.CancelFunc) {
	ctx, cancel := context.WithCancel(ctx)
	ch := make(chan string, 64)

	go func() {
		defer close(ch)
		cmd := c.command(ctx, ExecArgs(service, user, false, command)...)
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

// ExecInteractive returns an attached `compose exec` for command in service.
// user, when not empty, overrides the service's configured user.
func (c *Compose) ExecInteractive(ctx context.Context, service, user, command string) *exec.Cmd {
	cmd := c.command(ctx, ExecArgs(service, user, true, command)...)
	return cmd
}

func (c *Compose) Shell(ctx context.Context, service string) *exec.Cmd {
	cmd := c.command(ctx, "exec", service, "sh")
	return cmd
}

func CheckAvailable() error {
	cmd := exec.Command("docker", "compose", "version")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker compose not available: %w", err)
	}
	return nil
}
