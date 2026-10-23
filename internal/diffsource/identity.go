package diffsource

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/flexdinesh/diffx/internal/review"
)

// ContentHash identifies complete Git contents independently of filesystem
// timestamps and preview limits. An empty hash means complete identity cannot
// be proven, so callers must not deduplicate changed snapshots by content.
func ContentHash(ctx context.Context, source Source, snapshots []review.RepositoryDiff) (string, error) {
	git, ok := source.(*gitSource)
	if !ok {
		return "", nil
	}
	if err := git.verifyComparison(ctx); err != nil {
		return "", err
	}
	_, base, err := git.headAndBase(ctx)
	if err != nil {
		return "", err
	}
	// Stage records preserve every conflict blob as well as staged contents.
	index := ""
	usesIndex := false
	for _, snapshot := range snapshots {
		endpoints, err := git.endpoints(snapshot.Mode, base)
		if err != nil {
			return "", err
		}
		usesIndex = usesIndex || !endpoints.objectsOnly()
	}
	if usesIndex {
		index, err = runGit(ctx, git.root, 16<<20, "ls-files", "--stage", "-z")
		if err != nil {
			return "", err
		}
	}
	indexed := stageZeroObjects(index)
	hash := sha256.New()
	encoder := json.NewEncoder(hash)
	if err := encoder.Encode([]string{"git-content-v1", index}); err != nil {
		return "", err
	}
	if git.comparison != nil {
		if err := encoder.Encode([]any{"branch-comparison-v1", git.comparison.BaseOID, git.comparison.HeadOID, git.comparison.ObjectsOnly}); err != nil {
			return "", err
		}
	}
	scopes := append([]review.RepositoryDiff(nil), snapshots...)
	sort.Slice(scopes, func(i, j int) bool { return scopes[i].Mode < scopes[j].Mode })
	contents := make(map[string][]string)
	for _, snapshot := range scopes {
		endpoints, err := git.endpoints(snapshot.Mode, base)
		if err != nil {
			return "", err
		}
		raw, err := runGit(ctx, git.root, 16<<20, append(endpoints.diffArgs(), "--raw", "--no-abbrev", "-z", "--")...)
		if err != nil {
			return "", err
		}
		files, err := parseRaw(raw)
		if err != nil {
			return "", err
		}
		if err := encoder.Encode(snapshot.Mode); err != nil {
			return "", err
		}
		// Absent descriptors retain legacy identity bytes for saved captures.
		if snapshot.ComparisonKind != "" {
			if err := encoder.Encode(snapshot.ComparisonKind); err != nil {
				return "", err
			}
		}
		ordered := append([]review.ChangedFile(nil), snapshot.Files...)
		sort.Slice(ordered, func(i, j int) bool { return ordered[i].Path < ordered[j].Path })
		for _, file := range ordered {
			header := "untracked"
			if rawFile := files[file.Path]; rawFile != nil {
				header = rawFile.Fingerprint
				fields := strings.Fields(header)
				if len(fields) < 5 || !endpoints.objectsOnly() && (fields[0] == ":160000" || fields[1] == "160000") {
					return "", nil
				}
				// Without index refresh, a stat-dirty working path can have an
				// unknown raw target OID even when status proved it matches the
				// index. Restore the known OID to preserve existing content hashes.
				if target, ok := indexed[file.Path]; ok && endpoints.after.kind == worktreeEndpoint && file.WorktreeStatus == " " && !file.Recreated && fields[1] == target.mode && strings.Trim(fields[3], "0") == "" {
					fields[3] = target.oid
					header = strings.Join(fields, " ")
				}
			}
			var content []string
			if endpoints.after.kind == worktreeEndpoint && (file.Status != "D" || file.Recreated) {
				var found bool
				content, found = contents[file.Path]
				if !found {
					content, err = worktreeContent(ctx, filepath.Join(git.root, filepath.FromSlash(file.Path)))
					if err != nil {
						if file.Status == "U" && errors.Is(err, os.ErrNotExist) {
							return "", nil
						}
						return "", err
					}
					if content == nil {
						return "", nil
					}
					contents[file.Path] = content
				}
			}
			if err := encoder.Encode([]any{file.Path, file.OldPath, file.Status, file.IndexStatus, file.WorktreeStatus, header, content}); err != nil {
				return "", err
			}
		}
	}
	if err := git.verifyComparison(ctx); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

type indexObject struct{ mode, oid string }

func stageZeroObjects(index string) map[string]indexObject {
	objects := make(map[string]indexObject)
	for _, record := range strings.Split(index, "\x00") {
		header, path, ok := strings.Cut(record, "\t")
		fields := strings.Fields(header)
		if ok && len(fields) == 3 && fields[2] == "0" {
			objects[path] = indexObject{mode: fields[0], oid: fields[1]}
		}
	}
	return objects
}

func worktreeContent(ctx context.Context, path string) ([]string, error) {
	stat, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	mode := "100644"
	if stat.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(path)
		return []string{"120000", digest(target)}, err
	}
	if !stat.Mode().IsRegular() {
		return nil, nil
	}
	if stat.Mode()&0o111 != 0 {
		mode = "100755"
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	hash := sha256.New()
	_, readErr := io.Copy(hash, &contextReader{ctx: ctx, reader: file})
	if err := errors.Join(readErr, file.Close()); err != nil {
		return nil, err
	}
	return []string{mode, hex.EncodeToString(hash.Sum(nil))}, nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (reader *contextReader) Read(buffer []byte) (int, error) {
	if err := reader.ctx.Err(); err != nil {
		return 0, err
	}
	return reader.reader.Read(buffer)
}
