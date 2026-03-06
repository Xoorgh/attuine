package runner_test

import (
	"context"
	"testing"
	"time"

	"oxorg/attuine/internal/runner"
)

func TestRunHost_Success(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var lines []string
	ch, err := runner.RunHost(ctx, t.TempDir(), "echo hello")
	if err != nil {
		t.Fatalf("RunHost() error: %v", err)
	}
	for line := range ch {
		lines = append(lines, line)
	}
	if len(lines) != 1 || lines[0] != "hello" {
		t.Errorf("output = %v, want [hello]", lines)
	}
}

func TestRunHost_ExitCode(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ch, err := runner.RunHost(ctx, t.TempDir(), "exit 42")
	if err != nil {
		t.Fatalf("RunHost() error: %v", err)
	}
	var lastLine string
	for line := range ch {
		lastLine = line
	}
	if lastLine != "[exited with code 42]" {
		t.Errorf("last line = %q, want %q", lastLine, "[exited with code 42]")
	}
}

func TestRunHost_MultiLine(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ch, err := runner.RunHost(ctx, t.TempDir(), `printf "a\nb\nc"`)
	if err != nil {
		t.Fatalf("RunHost() error: %v", err)
	}
	var lines []string
	for line := range ch {
		lines = append(lines, line)
	}
	if len(lines) != 3 {
		t.Errorf("len(lines) = %d, want 3, got: %v", len(lines), lines)
	}
}
