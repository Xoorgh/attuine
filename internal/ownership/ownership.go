// Package ownership finds files in a checkout that the invoking user does not
// own, typically ones a container running as root wrote through a bind mount,
// and repairs them through a throwaway container so no sudo is needed.
package ownership

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// DefaultFixImage is the image Fix runs chown in. Any image with a chown and
// an xargs that accepts -0 works.
const DefaultFixImage = "busybox:1.37"

// Supported reports whether this platform has POSIX file ownership to check.
func Supported() bool { return supported }

// Owner returns the uid that owns path, without following a final symlink.
func Owner(path string) (int, error) {
	fi, err := os.Lstat(path)
	if err != nil {
		return 0, err
	}
	uid, ok := ownerOf(fi)
	if !ok {
		return 0, errors.New("file ownership is not available on this platform")
	}
	return uid, nil
}

// Scan walks dir and returns the topmost entries under it that uid does not
// own, as slash-separated paths relative to dir, sorted.
//
// It does not descend into an entry it reports: a recursive chown of that
// entry repairs everything below it, and a root-owned directory is often
// unreadable anyway. Symlinks are judged by their own owner and never
// followed. A nested directory holding a `.git` entry is another repository
// or worktree, which gets its own scan, so Scan skips it. A directory it may
// not read is reported, since whatever is inside cannot be checked either.
func Scan(dir string, uid int) ([]string, error) {
	if !supported {
		return nil, nil
	}
	var found []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, walkErr error) error {
		if path == dir {
			return walkErr
		}
		rel, _ := filepath.Rel(dir, path)
		rel = filepath.ToSlash(rel)
		if walkErr != nil {
			if errors.Is(walkErr, fs.ErrPermission) {
				found = append(found, rel)
			}
			// The entry vanished or cannot be read; nothing more to learn here.
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		fi, err := d.Info()
		if err != nil {
			return nil
		}
		if owner, ok := ownerOf(fi); ok && owner != uid {
			found = append(found, rel)
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			if _, err := os.Lstat(filepath.Join(path, ".git")); err == nil {
				return fs.SkipDir
			}
		}
		return nil
	})
	sort.Strings(found)
	return found, err
}

// FixArgs returns the `docker run` arguments that mount dir at /w and chown,
// recursively and without following symlinks, every path read NUL-separated
// from stdin (relative to dir) to uid:gid.
func FixArgs(image, dir string, uid, gid int) []string {
	owner := strconv.Itoa(uid) + ":" + strconv.Itoa(gid)
	return []string{
		"run", "--rm", "-i", "--network", "none",
		"--user", "0",
		"-v", dir + ":/w", "-w", "/w",
		"--entrypoint", "xargs",
		image, "-0", "chown", "-hR", owner, "--",
	}
}

// Fix chowns paths (relative to dir, as Scan returns them) to uid:gid through
// a throwaway container, which runs as root under a rootful Docker daemon.
func Fix(ctx context.Context, image, dir string, paths []string, uid, gid int) error {
	if len(paths) == 0 {
		return nil
	}
	var stdin bytes.Buffer
	for _, p := range paths {
		stdin.WriteString(p)
		stdin.WriteByte(0)
	}
	cmd := exec.CommandContext(ctx, "docker", FixArgs(image, dir, uid, gid)...)
	cmd.Stdin = &stdin
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("chown through %s: %s: %w", image, strings.TrimSpace(string(out)), err)
	}
	return nil
}
