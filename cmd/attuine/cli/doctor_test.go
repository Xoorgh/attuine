package cli

import (
	"reflect"
	"testing"
)

func TestGroupPaths(t *testing.T) {
	paths := []string{
		"tmp/cache/bootsnap/a/b",
		"tmp/cache/bootsnap/c",
		"tmp/cache/x",
		"frontend/build",
		"README.md",
		"log/development.log",
	}
	got := groupPaths(paths, 3)
	want := []pathGroup{
		{Prefix: "tmp/cache/bootsnap/", Count: 2},
		{Prefix: "README.md", Count: 1},
		{Prefix: "frontend/", Count: 1},
		{Prefix: "log/", Count: 1},
		{Prefix: "tmp/cache/", Count: 1},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("groupPaths() =\n %v\nwant\n %v", got, want)
	}
}
