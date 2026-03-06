package runner

import (
	"bufio"
	"context"
	"fmt"
	"os/exec"
)

func RunHost(ctx context.Context, dir string, command string) (<-chan string, error) {
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Dir = dir

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("creating stdout pipe: %w", err)
	}
	cmd.Stderr = cmd.Stdout

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting command: %w", err)
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
			if exitErr, ok := err.(*exec.ExitError); ok {
				select {
				case ch <- fmt.Sprintf("[exited with code %d]", exitErr.ExitCode()):
				case <-ctx.Done():
				}
			}
		}
	}()

	return ch, nil
}

func RunHostInteractive(ctx context.Context, dir string, command string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Dir = dir
	return cmd
}
