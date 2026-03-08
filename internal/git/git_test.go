package git_test

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"oxorg/attuine/internal/git"
)

// initTestRepo creates a git repo in a temp dir with one commit.
func initTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s: %v", args, out, err)
		}
	}
	run("init", "-b", "master")
	run("config", "user.email", "test@test.com")
	run("config", "user.name", "Test")
	run("commit", "--allow-empty", "-m", "initial")
	return dir
}

func TestCurrentBranch(t *testing.T) {
	repo := initTestRepo(t)
	branch, err := git.CurrentBranch(context.Background(), repo)
	if err != nil {
		t.Fatalf("CurrentBranch() error: %v", err)
	}
	if branch != "master" {
		t.Errorf("CurrentBranch() = %q, want %q", branch, "master")
	}
}

func TestIsClean(t *testing.T) {
	repo := initTestRepo(t)

	clean, err := git.IsClean(context.Background(), repo)
	if err != nil {
		t.Fatalf("IsClean() error: %v", err)
	}
	if !clean {
		t.Error("IsClean() = false, want true for clean repo")
	}

	// Make dirty
	if err := exec.Command("sh", "-c", "echo dirty > "+filepath.Join(repo, "file.txt")).Run(); err != nil {
		t.Fatal(err)
	}

	clean, err = git.IsClean(context.Background(), repo)
	if err != nil {
		t.Fatalf("IsClean() error: %v", err)
	}
	if clean {
		t.Error("IsClean() = true, want false for dirty repo")
	}
}

func TestCreateBranch(t *testing.T) {
	repo := initTestRepo(t)

	err := git.CreateBranch(context.Background(), repo, "feat/test")
	if err != nil {
		t.Fatalf("CreateBranch() error: %v", err)
	}

	branch, _ := git.CurrentBranch(context.Background(), repo)
	if branch != "feat/test" {
		t.Errorf("after CreateBranch, CurrentBranch() = %q, want %q", branch, "feat/test")
	}
}

func TestCheckout(t *testing.T) {
	repo := initTestRepo(t)
	git.CreateBranch(context.Background(), repo, "other")
	git.Checkout(context.Background(), repo, "master")

	branch, _ := git.CurrentBranch(context.Background(), repo)
	if branch != "master" {
		t.Errorf("after Checkout, CurrentBranch() = %q, want %q", branch, "master")
	}
}

func TestLog(t *testing.T) {
	repo := initTestRepo(t)
	// Add a second commit
	cmd := exec.Command("git", "commit", "--allow-empty", "-m", "second")
	cmd.Dir = repo
	cmd.Run()

	entries, err := git.Log(context.Background(), repo, 5)
	if err != nil {
		t.Fatalf("Log() error: %v", err)
	}
	if len(entries) != 2 {
		t.Errorf("Log() returned %d entries, want 2", len(entries))
	}
}

func TestStatus(t *testing.T) {
	repo := initTestRepo(t)

	status, err := git.Status(context.Background(), repo)
	if err != nil {
		t.Fatalf("Status() error: %v", err)
	}
	if status.Branch != "master" {
		t.Errorf("Status().Branch = %q, want %q", status.Branch, "master")
	}
	if !status.Clean {
		t.Error("Status().Clean = false, want true")
	}
}

func TestFetch(t *testing.T) {
	// Fetch on a repo with no remote should return an error (or succeed silently)
	repo := initTestRepo(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// No remote configured, so fetch just returns (no-op or error)
	_ = git.Fetch(ctx, repo)
}

func TestAdd(t *testing.T) {
	repo := initTestRepo(t)
	exec.Command("sh", "-c", "echo test > "+filepath.Join(repo, "new.txt")).Run()

	err := git.Add(context.Background(), repo, "new.txt")
	if err != nil {
		t.Fatalf("Add() error: %v", err)
	}

	// Verify file is staged
	cmd := exec.Command("git", "diff", "--cached", "--name-only")
	cmd.Dir = repo
	out, _ := cmd.Output()
	if string(out) != "new.txt\n" {
		t.Errorf("staged files = %q, want new.txt", string(out))
	}
}

func TestCommit(t *testing.T) {
	repo := initTestRepo(t)
	exec.Command("sh", "-c", "echo test > "+filepath.Join(repo, "new.txt")).Run()
	git.Add(context.Background(), repo, "new.txt")

	err := git.Commit(context.Background(), repo, "add new file")
	if err != nil {
		t.Fatalf("Commit() error: %v", err)
	}

	entries, _ := git.Log(context.Background(), repo, 5)
	if len(entries) != 2 {
		t.Errorf("after Commit, Log() returned %d entries, want 2", len(entries))
	}
}
