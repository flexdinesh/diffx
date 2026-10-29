package diffsource

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"

	"github.com/flexdinesh/diffx/internal/review"
)

type fileObjects struct{ before, after indexObject }
type collectionObjects struct {
	files   map[string]fileObjects
	commits map[string]review.RepositoryDiff
	targets map[string]*commitTarget
}

func newCollectionObjects() *collectionObjects {
	return &collectionObjects{files: make(map[string]fileObjects), commits: make(map[string]review.RepositoryDiff), targets: make(map[string]*commitTarget)}
}
func parseFileObjects(header string) (fileObjects, bool) {
	fields := strings.Fields(header)
	if len(fields) != 5 || !strings.HasPrefix(fields[0], ":") {
		return fileObjects{}, false
	}
	return fileObjects{before: indexObject{mode: strings.TrimPrefix(fields[0], ":"), oid: fields[2]}, after: indexObject{mode: fields[1], oid: fields[3]}}, true
}

// readDelimited bounds framing metadata even when a child never terminates it.
func readDelimited(reader *bufio.Reader, delimiter byte, maximum int) (string, error) {
	var value strings.Builder
	for {
		part, err := reader.ReadSlice(delimiter)
		if value.Len()+len(part) > maximum {
			return "", errOutputLimit
		}
		value.Write(part)
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		return value.String(), err
	}
}

// runGitStream joins both the bounded parser and the owned process before
// returning, including cancellation and malformed-output paths.
func runGitStream(ctx context.Context, root string, input io.Reader, parse func(*bufio.Reader) error, args ...string) error {
	reader, writer := io.Pipe()
	parsed := make(chan error, 1)
	go func() {
		err := parse(bufio.NewReaderSize(reader, 64<<10))
		_ = reader.CloseWithError(err)
		parsed <- err
	}()
	err := runGitOutput(ctx, root, input, writer, args...)
	_ = writer.CloseWithError(err)
	return errors.Join(err, <-parsed)
}

// PrepareCommitSnapshots reads immutable commit metadata in two processes,
// retaining raw modes/OIDs without changing the stored source contract.
func PrepareCommitSnapshots(ctx context.Context, source Source, commits []review.Commit) error {
	ctx = commandScope(ctx, string(review.DiffCommit))
	git, ok := source.(*gitSource)
	if !ok || git.comparison == nil {
		return errors.New("commit diffs require a branch comparison")
	}
	if git.collection == nil {
		git.collection = newCollectionObjects()
	}
	selected := make(map[string]*gitSource)
	var input strings.Builder
	for _, commit := range commits {
		if commit.Merge() {
			continue
		}
		opened, err := CommitSource(ctx, source, commit)
		if err != nil {
			return err
		}
		target, ok := opened.(*gitSource)
		if !ok {
			return errors.New("Git commit source unavailable")
		}
		selected[commit.OID] = target
		git.collection.targets[commit.OID] = target.commit
		if target.commit.root {
			fmt.Fprintf(&input, "%s\n", target.commit.oid)
		} else {
			fmt.Fprintf(&input, "%s %s\n", target.commit.oid, target.commit.parent)
		}
	}
	if len(selected) == 0 {
		return ctx.Err()
	}
	read := func(format string) (map[string]string, error) {
		output := make(map[string]string)
		args := []string{"diff-tree", "--stdin", "--root", "-r", "--no-abbrev", "-z", "--find-renames", "--no-ext-diff", "--no-textconv", "--ignore-submodules=none", format}
		args = append(args, patchFormat...)
		err := runGitStream(ctx, git.root, strings.NewReader(input.String()), func(reader *bufio.Reader) error {
			oid := ""
			paths := 0
			var records strings.Builder
			for {
				token, err := readDelimited(reader, 0, 16<<20)
				if errors.Is(err, io.EOF) && token == "" {
					if oid != "" {
						output[oid] = records.String()
					}
					return nil
				}
				if err != nil {
					return err
				}
				value := strings.TrimSuffix(token, "\x00")
				if _, known := selected[value]; known && paths == 0 {
					if oid != "" {
						output[oid] = records.String()
					}
					oid = value
					records.Reset()
					continue
				}
				if oid == "" {
					return errors.New("missing diff-tree commit frame")
				}
				if records.Len()+len(token) > 16<<20 {
					return errOutputLimit
				}
				records.WriteString(token)
				if paths > 0 {
					paths--
					continue
				}
				if format == "--raw" && strings.HasPrefix(value, ":") {
					paths = 1
					fields := strings.Fields(value)
					if len(fields) == 5 && strings.HasPrefix(fields[4], "R") || len(fields) == 5 && strings.HasPrefix(fields[4], "C") {
						paths = 2
					}
				} else if format == "--numstat" && strings.HasSuffix(value, "\t") {
					paths = 2
				}
			}
		}, args...)
		return output, err
	}
	raw, err := read("--raw")
	if err != nil {
		return err
	}
	stats, err := read("--numstat")
	if err != nil {
		return err
	}
	for oid, target := range selected {
		files, err := parseRaw(raw[oid])
		if err != nil {
			return err
		}
		applyStats(files, stats[oid])
		snapshot := review.RepositoryDiff{Source: "local", Root: git.root, Name: filepath.Base(git.root), Branch: git.comparison.branch, Head: &target.commit.oid, Mode: review.DiffCommit, ComparisonKind: review.ComparisonCommit, Files: make([]review.ChangedFile, 0, len(files))}
		for _, file := range files {
			content := fingerprintContent{ID: file.ID, Path: file.Path, OldPath: file.OldPath, Status: file.Status, Additions: file.Additions, Deletions: file.Deletions, Binary: file.Binary, Fingerprint: file.Fingerprint, Recreated: file.Recreated}
			objects, known := parseFileObjects(file.Fingerprint)
			encoded, _ := json.Marshal([]any{review.DiffCommit, snapshot.Head, target.commit.parent, content, nil, nil, nil})
			file.Fingerprint = digest(string(encoded))
			if known {
				git.collection.files[file.Fingerprint] = objects
			}
			file.IndexStatus, file.WorktreeStatus = "", ""
			snapshot.Files = append(snapshot.Files, *file)
		}
		sort.Slice(snapshot.Files, func(i, j int) bool { return snapshot.Files[i].Path < snapshot.Files[j].Path })
		encoded, _ := json.Marshal([]any{snapshot.Head, snapshot.Branch, target.commit.parent, snapshot.Files})
		snapshot.Revision = digest(string(encoded))
		git.collection.commits[oid] = snapshot
	}
	return nil
}
