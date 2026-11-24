//go:build unix

package ownership_test

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"oxorg/attuine/internal/ownership"
)

// tree builds a small checkout; every file in it is owned by the test's uid.
func tree(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, p := range []string{
		"README.md",
		"tmp/cache/a",
		"tmp/cache/b",
		"frontend/build/index.html",
		"nested/.git/HEAD",
		"nested/src.go",
		"worktree/.git",
	} {
		full := filepath.Join(dir, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestScan_OwnTreeIsClean(t *testing.T) {
	got, err := ownership.Scan(tree(t), os.Getuid())
	if err != nil {
		t.Fatalf("Scan() error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("Scan() = %v, want nothing", got)
	}
}

// Scanning for another uid makes every entry foreign, which shows that Scan
// reports only the topmost entries and skips nested repositories.
func TestScan_ReportsTopmostForeignEntries(t *testing.T) {
	got, err := ownership.Scan(tree(t), os.Getuid()+1)
	if err != nil {
		t.Fatalf("Scan() error: %v", err)
	}
	want := []string{"README.md", "frontend", "nested", "tmp", "worktree"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Scan() = %v, want %v", got, want)
	}
}

func TestScan_SkipsNestedRepositories(t *testing.T) {
	dir := tree(t)
	// An unreadable directory inside a nested repository would be reported if
	// Scan descended into it; the nested repository gets its own scan instead.
	locked := filepath.Join(dir, "nested", "locked")
	if err := os.Mkdir(locked, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(locked, 0o755) })

	got, err := ownership.Scan(dir, os.Getuid())
	if err != nil {
		t.Fatalf("Scan() error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("Scan() = %v, want nothing (nested repos are scanned on their own)", got)
	}
}

func TestScan_ReportsUnreadableDirectory(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root can read any directory")
	}
	dir := tree(t)
	locked := filepath.Join(dir, "tmp", "locked")
	if err := os.Mkdir(locked, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(locked, 0o755) })

	got, err := ownership.Scan(dir, os.Getuid())
	if err != nil {
		t.Fatalf("Scan() error: %v", err)
	}
	if want := []string{"tmp/locked"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Scan() = %v, want %v", got, want)
	}
}

func TestFixArgs(t *testing.T) {
	got := ownership.FixArgs("busybox:1.37", "/src/app", 1000, 1001)
	want := []string{
		"run", "--rm", "-i", "--network", "none", "--user", "0",
		"-v", "/src/app:/w", "-w", "/w", "--entrypoint", "xargs",
		"busybox:1.37", "-0", "chown", "-hR", "1000:1001", "--",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("FixArgs() =\n %v\nwant\n %v", got, want)
	}
}
