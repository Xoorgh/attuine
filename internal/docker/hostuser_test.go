package docker_test

import (
	"reflect"
	"testing"

	"oxorg/attuine/internal/docker"
)

func TestHostUserEnv_AddsIDs(t *testing.T) {
	got := docker.HostUserEnv([]string{"PATH=/bin"}, 1000, 1001)
	want := []string{"PATH=/bin", "HOST_UID=1000", "HOST_GID=1001"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("HostUserEnv() = %v, want %v", got, want)
	}
}

func TestHostUserEnv_KeepsExistingValues(t *testing.T) {
	got := docker.HostUserEnv([]string{"HOST_UID=42"}, 1000, 1000)
	want := []string{"HOST_UID=42", "HOST_GID=1000"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("HostUserEnv() = %v, want %v", got, want)
	}
}

func TestHostUserEnv_SkipsNegativeIDs(t *testing.T) {
	got := docker.HostUserEnv([]string{"PATH=/bin"}, -1, -1)
	want := []string{"PATH=/bin"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("HostUserEnv() = %v, want %v", got, want)
	}
}

func TestHostUserEnv_DoesNotMutateInput(t *testing.T) {
	in := make([]string, 1, 4)
	in[0] = "PATH=/bin"
	docker.HostUserEnv(in, 1000, 1000)
	if got := in[:cap(in)][1]; got != "" {
		t.Errorf("HostUserEnv wrote into the caller's backing array: %q", got)
	}
}

func TestExecArgs(t *testing.T) {
	cases := []struct {
		name        string
		user        string
		interactive bool
		want        []string
	}{
		{"streamed", "", false, []string{"exec", "-T", "app", "sh", "-c", "ls"}},
		{"streamed as user", "0", false, []string{"exec", "-T", "-u", "0", "app", "sh", "-c", "ls"}},
		{"interactive", "", true, []string{"exec", "app", "sh", "-c", "ls"}},
		{"interactive as user", "root", true, []string{"exec", "-u", "root", "app", "sh", "-c", "ls"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := docker.ExecArgs("app", tc.user, tc.interactive, "ls")
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("ExecArgs() = %v, want %v", got, tc.want)
			}
		})
	}
}
