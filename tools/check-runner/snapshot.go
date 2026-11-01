package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

type sourceSnapshot struct {
	Root, Commit, Tree, Identity string
}

// snapshotGit does not inherit Git's checkout overrides or personal configuration.
func snapshotGit(ctx context.Context, root string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = root
	for _, value := range os.Environ() {
		if !strings.HasPrefix(value, "GIT_") {
			cmd.Env = append(cmd.Env, value)
		}
	}
	cmd.Env = append(cmd.Env, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0", "GIT_AUTHOR_DATE=2000-01-01T00:00:00Z", "GIT_COMMITTER_DATE=2000-01-01T00:00:00Z")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	data, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return data, nil
}

func cleanSnapshotSource(ctx context.Context, root string) error {
	status, err := snapshotGit(ctx, root, "status", "--porcelain=v1", "--untracked-files=all")
	if err != nil {
		return err
	}
	if len(status) != 0 {
		return errors.New("PR validation requires committed HEAD and a clean working tree, including non-ignored untracked files")
	}
	return nil
}

func captureSnapshot(ctx context.Context, root string, committed bool) (sourceSnapshot, func(), error) {
	var result sourceSnapshot
	noop := func() {}
	if committed {
		if err := cleanSnapshotSource(ctx, root); err != nil {
			return result, noop, err
		}
	}
	head, err := snapshotGit(ctx, root, "rev-parse", "HEAD", "HEAD^{tree}")
	if err != nil {
		return result, noop, err
	}
	identifiers := strings.Fields(string(head))
	if len(identifiers) != 2 {
		return result, noop, errors.New("cannot resolve snapshot commit and tree")
	}
	result.Commit, result.Tree = identifiers[0], identifiers[1]
	result.Root, err = os.MkdirTemp("", "diffx-check-source-")
	if err != nil {
		return result, noop, err
	}
	cleanup := func() { _ = os.RemoveAll(result.Root) }
	fail := func(err error) (sourceSnapshot, func(), error) {
		cleanup()
		return sourceSnapshot{}, noop, err
	}
	if committed {
		err = copyCommittedSnapshot(ctx, root, result.Root, result.Commit)
	} else {
		err = copyWorkingSnapshot(ctx, root, result.Root)
	}
	if err != nil {
		return fail(err)
	}
	result.Identity, err = fingerprintFiles(result.Root, nil)
	if err != nil {
		return fail(err)
	}
	if committed {
		if err := cleanSnapshotSource(ctx, root); err != nil {
			return fail(err)
		}
		current, err := snapshotGit(ctx, root, "rev-parse", "HEAD")
		if err != nil {
			return fail(err)
		}
		if strings.TrimSpace(string(current)) != result.Commit {
			return fail(errors.New("HEAD changed while capturing validation source; retry"))
		}
	} else {
		second, err := os.MkdirTemp("", "diffx-check-compare-")
		if err != nil {
			return fail(err)
		}
		defer os.RemoveAll(second)
		if err := copyWorkingSnapshot(ctx, root, second); err != nil {
			return fail(err)
		}
		identity, err := fingerprintFiles(second, nil)
		if err != nil {
			return fail(err)
		}
		if identity != result.Identity {
			return fail(errors.New("working files changed while capturing validation source; retry"))
		}
	}
	if err := initializeSnapshotGit(ctx, result.Root); err != nil {
		return fail(err)
	}
	return result, cleanup, nil
}

func snapshotDestination(root, name string) (string, error) {
	if !filepath.IsLocal(name) || strings.Split(filepath.ToSlash(name), "/")[0] == ".git" {
		return "", fmt.Errorf("unsafe snapshot path %q", name)
	}
	return filepath.Join(root, filepath.FromSlash(name)), nil
}

func writeSnapshotFile(root, name string, data []byte, mode fs.FileMode) error {
	destination, err := snapshotDestination(root, name)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return err
	}
	if mode&os.ModeSymlink != 0 {
		return os.Symlink(string(data), destination)
	}
	return os.WriteFile(destination, data, mode.Perm())
}

func copyCommittedSnapshot(ctx context.Context, root, destination, commit string) error {
	data, err := snapshotGit(ctx, root, "ls-tree", "-rz", commit)
	if err != nil {
		return err
	}
	for _, entry := range bytes.Split(data, []byte{0}) {
		if len(entry) == 0 {
			continue
		}
		metadata, name, ok := strings.Cut(string(entry), "\t")
		fields := strings.Fields(metadata)
		if !ok || len(fields) != 3 || fields[1] != "blob" {
			return fmt.Errorf("unsupported Git snapshot entry %q (submodules must be materialized explicitly)", entry)
		}
		var mode fs.FileMode
		switch fields[0] {
		case "100644":
			mode = 0o644
		case "100755":
			mode = 0o755
		case "120000":
			mode = os.ModeSymlink | 0o777
		default:
			return fmt.Errorf("unsupported Git mode %s", fields[0])
		}
		content, err := snapshotGit(ctx, root, "cat-file", "blob", fields[2])
		if err != nil {
			return err
		}
		if err := writeSnapshotFile(destination, name, content, mode); err != nil {
			return err
		}
	}
	return nil
}

func copyWorkingSnapshot(ctx context.Context, root, destination string) error {
	data, err := snapshotGit(ctx, root, "ls-files", "-z", "--cached", "--others", "--exclude-standard")
	if err != nil {
		return err
	}
	names := strings.Split(strings.TrimSuffix(string(data), "\x00"), "\x00")
	slices.Sort(names)
	for _, name := range slices.Compact(names) {
		if name == "" {
			continue
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		path, err := snapshotDestination(root, name)
		if err != nil {
			return err
		}
		info, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue // A tracked deletion is part of the working snapshot.
		}
		if err != nil {
			return err
		}
		mode := info.Mode()
		var content []byte
		if mode&os.ModeSymlink != 0 {
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			content = []byte(target)
		} else if mode.IsRegular() {
			content, err = os.ReadFile(path)
			if err != nil {
				return err
			}
			mode = 0o644 | (mode.Perm() & 0o111)
		} else {
			return fmt.Errorf("unsupported snapshot file %s", name)
		}
		if err := writeSnapshotFile(destination, name, content, mode); err != nil {
			return err
		}
	}
	return nil
}

func initializeSnapshotGit(ctx context.Context, root string) error {
	for _, args := range [][]string{
		{"init", "--quiet", "--initial-branch=validation", "--template="},
		{"-c", "core.hooksPath=/dev/null", "add", "--force", "--all"},
		{"-c", "core.hooksPath=/dev/null", "-c", "commit.gpgsign=false", "-c", "user.name=Validation", "-c", "user.email=validation@localhost", "commit", "--quiet", "--allow-empty", "--date=2000-01-01T00:00:00Z", "-m", "Validation source"},
		// Rebuild the index from the tree so host inode/mtime data does not enter
		// Dagger's imported directory identity.
		{"read-tree", "--empty"},
		{"read-tree", "HEAD"},
	} {
		if _, err := snapshotGit(ctx, root, args...); err != nil {
			return err
		}
	}
	return nil
}

// fingerprintFiles is independent of filesystem timestamps and checkout paths.
func fingerprintFiles(root string, includes func(string) bool) (string, error) {
	hash := sha256.New()
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		name = filepath.ToSlash(name)
		if name == ".git" {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() || (includes != nil && !includes(name)) {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		mode := info.Mode()
		var data []byte
		if mode&os.ModeSymlink != 0 {
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			data = []byte(target)
			mode = os.ModeSymlink
		} else if mode.IsRegular() {
			data, err = os.ReadFile(path)
			if err != nil {
				return err
			}
			mode = mode.Perm() & 0o111
		} else {
			return fmt.Errorf("unsupported fingerprint file %s", name)
		}
		fmt.Fprintf(hash, "%d:%s:%d:%d:", len(name), name, mode, len(data))
		hash.Write(data)
		return nil
	})
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func checkSnapshotCurrent(ctx context.Context, root string, snapshot sourceSnapshot, committed bool) error {
	current, cleanup, err := captureSnapshot(ctx, root, committed)
	defer cleanup()
	if err != nil {
		return err
	}
	if current.Identity != snapshot.Identity || (committed && current.Commit != snapshot.Commit) {
		return errors.New("checkout changed during validation; results do not certify the current checkout")
	}
	return nil
}
