package docker_test

import (
	"testing"

	"oxorg/attuine/internal/docker"
)

func TestParseStatus_Running(t *testing.T) {
	jsonLines := []string{
		`{"Name":"oxorg-dev-postgres-1","Service":"postgres","State":"running","Health":"","Publishers":[{"URL":"0.0.0.0","TargetPort":5432,"PublishedPort":5432,"Protocol":"tcp"}]}`,
		`{"Name":"oxorg-dev-galevera-1","Service":"galevera","State":"running","Health":"healthy","Publishers":[{"URL":"0.0.0.0","TargetPort":3000,"PublishedPort":3000,"Protocol":"tcp"}]}`,
	}

	statuses, err := docker.ParseStatus(jsonLines)
	if err != nil {
		t.Fatalf("ParseStatus() error: %v", err)
	}
	if len(statuses) != 2 {
		t.Fatalf("len(statuses) = %d, want 2", len(statuses))
	}

	if statuses[0].Service != "postgres" {
		t.Errorf("statuses[0].Service = %q, want %q", statuses[0].Service, "postgres")
	}
	if statuses[0].State != "running" {
		t.Errorf("statuses[0].State = %q, want %q", statuses[0].State, "running")
	}
	if statuses[1].Health != "healthy" {
		t.Errorf("statuses[1].Health = %q, want %q", statuses[1].Health, "healthy")
	}
	if len(statuses[1].Ports) == 0 || statuses[1].Ports[0] != "3000" {
		t.Errorf("statuses[1].Ports[0] = %v, want [3000]", statuses[1].Ports)
	}
}

func TestParseStatus_Empty(t *testing.T) {
	statuses, err := docker.ParseStatus(nil)
	if err != nil {
		t.Fatalf("ParseStatus() error: %v", err)
	}
	if len(statuses) != 0 {
		t.Errorf("len(statuses) = %d, want 0", len(statuses))
	}
}

func TestBuildArgs_Up(t *testing.T) {
	c := docker.NewCompose("dev/docker-compose.yml", "dev/.env", "/project")

	args := c.BuildArgs("up", "-d", "--profile", "core")
	found := map[string]bool{}
	for _, a := range args {
		found[a] = true
	}
	if !found["up"] {
		t.Error("missing 'up' in args")
	}
	if !found["-d"] {
		t.Error("missing '-d' in args")
	}
	if !found["--env-file"] {
		t.Error("missing '--env-file' in args")
	}
}

func TestBuildArgs_NoEnvFile(t *testing.T) {
	c := docker.NewCompose("dc.yml", "", "/project")

	args := c.BuildArgs("ps")
	for _, a := range args {
		if a == "--env-file" {
			t.Error("should not include --env-file when empty")
		}
	}
}
