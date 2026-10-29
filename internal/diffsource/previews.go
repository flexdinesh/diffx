package diffsource

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/flexdinesh/diffx/internal/review"
)

// CollectionPreviews owns only collection-local bounded reads. Stored Sources
// retain their scalar API; callers choose the bulk reader at collection time.
type CollectionPreviews struct {
	source                                Source
	patches                               map[string]review.FilePatch
	patchBytes, patchBudget, patchMaximum int
	blobs                                 map[string]string
	blobBytes                             int
	blobOrder                             []string
	unavailable                           map[string]error
	scalarPatches                         map[string]bool
}

// Keep pathspecs below native command-line limits, including Windows UTF-16
// and macOS's shared argv/environment budget. Rename pairs stay together.
const patchPathspecBytes = 16 << 10

func patchFileBatches(files []review.ChangedFile) [][]review.ChangedFile {
	var batches [][]review.ChangedFile
	for len(files) > 0 {
		end, bytes := 0, 0
		for end < len(files) {
			cost := len(files[end].Path) + 1
			if files[end].OldPath != nil {
				cost += len(*files[end].OldPath) + 1
			}
			if end > 0 && bytes+cost > patchPathspecBytes {
				break
			}
			bytes += cost
			end++
		}
		batches = append(batches, files[:end])
		files = files[end:]
	}
	return batches
}

func NewCollectionPreviews(source Source) *CollectionPreviews {
	return &CollectionPreviews{source: source, patches: make(map[string]review.FilePatch), blobs: make(map[string]string), unavailable: make(map[string]error), scalarPatches: make(map[string]bool)}
}
func (reader *CollectionPreviews) target(snapshot review.RepositoryDiff) Source {
	git, ok := reader.source.(*gitSource)
	if !ok || snapshot.Mode != review.DiffCommit || snapshot.Head == nil || git.collection == nil {
		return reader.source
	}
	target, ok := git.collection.targets[*snapshot.Head]
	if !ok {
		return reader.source
	}
	return &gitSource{root: git.root, comparison: git.comparison, commit: target, collection: git.collection}
}

// PreparePatches streams comparisons independently and immutable commits in one
// diff-tree process. The cache retains at most the request's preview budget;
// a cache miss later reads a small file batch, preserving allocator semantics.
func (reader *CollectionPreviews) PreparePatches(ctx context.Context, snapshots []review.RepositoryDiff, maximum, budget int) error {
	reader.patchMaximum, reader.patchBudget = min(maximum, MaxFileBytes), max(budget, 8*min(maximum, MaxFileBytes))
	git, ok := reader.source.(*gitSource)
	if !ok {
		return ctx.Err()
	}
	commits := make([]review.RepositoryDiff, 0)
	for _, snapshot := range snapshots {
		if len(snapshot.Files) == 0 {
			continue
		}
		if snapshot.Mode == review.DiffCommit {
			commits = append(commits, snapshot)
			continue
		}
		target, ok := reader.target(snapshot).(*gitSource)
		if !ok {
			return errors.New("Git preview source unavailable")
		}
		endpoints, err := target.previewEndpoints(ctx, snapshot.Mode, snapshot.Head)
		if err != nil {
			return err
		}
		for _, files := range patchFileBatches(snapshot.Files) {
			batch := snapshot
			batch.Files = files
			args := append(endpoints.diffArgs(), "--patch", "--unified=5", "--submodule=short", "--")
			for _, file := range files {
				if file.OldPath != nil {
					args = append(args, *file.OldPath)
				}
				args = append(args, file.Path)
			}
			if err := reader.readPatches(commandScope(ctx, string(snapshot.Mode)), git.root, nil, []review.RepositoryDiff{batch}, false, args); err != nil {
				return err
			}
		}
	}
	if len(commits) == 0 {
		return ctx.Err()
	}
	var input strings.Builder
	for _, snapshot := range commits {
		target, ok := reader.target(snapshot).(*gitSource)
		if !ok || target.commit == nil {
			return errors.New("immutable commit preview unavailable")
		}
		if target.commit.root {
			fmt.Fprintf(&input, "%s\n", target.commit.oid)
		} else {
			fmt.Fprintf(&input, "%s %s\n", target.commit.oid, target.commit.parent)
		}
	}
	args := []string{"diff-tree", "--stdin", "--root", "-r", "-z", "--patch", "--unified=5", "--submodule=short", "--find-renames", "--no-ext-diff", "--no-textconv", "--ignore-submodules=none"}
	args = append(args, patchFormat...)
	return reader.readPatches(commandScope(ctx, string(review.DiffCommit)), git.root, strings.NewReader(input.String()), commits, false, args)
}

func quoteGitPath(path string) string {
	if !strings.ContainsAny(path, "\"\\\a\b\t\n\v\f\r") && strings.IndexFunc(path, func(c rune) bool { return c < 32 || c == 127 }) < 0 {
		return path
	}
	var quoted strings.Builder
	quoted.WriteByte('"')
	for _, c := range []byte(path) {
		switch c {
		case '"', '\\':
			quoted.WriteByte('\\')
			quoted.WriteByte(c)
		case '\a':
			quoted.WriteString("\\a")
		case '\b':
			quoted.WriteString("\\b")
		case '\t':
			quoted.WriteString("\\t")
		case '\n':
			quoted.WriteString("\\n")
		case '\v':
			quoted.WriteString("\\v")
		case '\f':
			quoted.WriteString("\\f")
		case '\r':
			quoted.WriteString("\\r")
		default:
			if c < 32 || c == 127 {
				fmt.Fprintf(&quoted, "\\%03o", c)
			} else {
				quoted.WriteByte(c)
			}
		}
	}
	quoted.WriteByte('"')
	return quoted.String()
}
func patchHeader(file review.ChangedFile) string {
	previous := file.Path
	if file.OldPath != nil {
		previous = *file.OldPath
	}
	return "diff --git " + quoteGitPath("a/"+previous) + " " + quoteGitPath("b/"+file.Path) + "\n"
}
func (reader *CollectionPreviews) cachePatch(key string, patch review.FilePatch, replace bool) {
	cost := len(patch.Patch)
	if old, ok := reader.patches[key]; ok {
		reader.patchBytes -= len(old.Patch)
		delete(reader.patches, key)
	}
	if cost > reader.patchBudget {
		return
	}
	if !replace && reader.patchBytes+cost > reader.patchBudget {
		return
	}
	for reader.patchBytes+cost > reader.patchBudget {
		for key, old := range reader.patches {
			reader.patchBytes -= len(old.Patch)
			delete(reader.patches, key)
			break
		}
	}
	reader.patches[key] = patch
	reader.patchBytes += cost
}
func (reader *CollectionPreviews) readPatches(ctx context.Context, root string, input io.Reader, snapshots []review.RepositoryDiff, replace bool, args []string) error {
	headers := make([]map[string]string, len(snapshots))
	oids := make(map[string]int)
	for index, snapshot := range snapshots {
		headers[index] = make(map[string]string, len(snapshot.Files))
		if snapshot.Mode == review.DiffCommit && snapshot.Head != nil {
			oids[*snapshot.Head] = index
		}
		for _, file := range snapshot.Files {
			if file.Status == "?" || file.Recreated || file.Binary || file.Status == "U" {
				continue
			}
			header := patchHeader(file)
			if previous, duplicate := headers[index][header]; duplicate {
				reader.scalarPatches[previous] = true
				reader.scalarPatches[file.Fingerprint] = true
				headers[index][header] = ""
			} else {
				headers[index][header] = file.Fingerprint
			}
			reader.cachePatch(file.Fingerprint, review.FilePatch{Message: Message("No text changes (file mode or metadata changed).")}, replace)
		}
	}
	err := runGitStream(ctx, root, input, func(stream *bufio.Reader) error {
		unit := 0
		key := ""
		size := 0
		var patch bytes.Buffer
		sizes := make(map[string]int)
		flush := func() {
			if key == "" {
				return
			}
			previousSize := sizes[key]
			sizes[key] += size
			result := review.FilePatch{Patch: patch.String()}
			old, cached := reader.patches[key]
			if sizes[key] > MaxFileBytes {
				result = review.FilePatch{Message: Message("Diff exceeds the 2 MiB preview limit.")}
			} else if sizes[key] > reader.patchMaximum {
				result = review.FilePatch{Message: Message("Diff exceeds the 1 MiB preview limit.")}
			} else if previousSize > 0 {
				if !cached {
					return
				}
				result.Patch = old.Patch + result.Patch
			}
			reader.cachePatch(key, result, replace)
		}
		lineStart := true
		for {
			part, err := stream.ReadSlice('\n')
			if lineStart {
				for {
					nul := bytes.IndexByte(part, 0)
					if nul < 0 {
						break
					}
					index, ok := oids[string(part[:nul])]
					if !ok {
						break
					}
					flush()
					key = ""
					patch.Reset()
					size = 0
					unit = index
					part = part[nul+1:]
				}
				if bytes.HasPrefix(part, []byte("diff --git ")) || bytes.HasPrefix(part, []byte("diff --cc ")) || bytes.HasPrefix(part, []byte("diff --combined ")) {
					flush()
					patch.Reset()
					size = 0
					key = headers[unit][string(part)]
				}
			}
			if key != "" {
				size += len(part)
				if size <= reader.patchMaximum {
					_, _ = patch.Write(part)
				}
			}
			if errors.Is(err, bufio.ErrBufferFull) {
				lineStart = false
				continue
			}
			lineStart = true
			if errors.Is(err, io.EOF) {
				flush()
				return nil
			}
			if err != nil {
				return err
			}
		}
	}, args...)
	var failure *gitFailure
	if errors.As(err, &failure) {
		return Error(409, "File changed while loading. Refresh to try again.")
	}
	return err
}

func (reader *CollectionPreviews) Patch(ctx context.Context, snapshot review.RepositoryDiff, file review.ChangedFile) (review.FilePatch, error) {
	ctx = commandScope(ctx, string(snapshot.Mode))
	if err := ctx.Err(); err != nil {
		return review.FilePatch{}, err
	}
	source := reader.target(snapshot)
	git, ok := source.(*gitSource)
	if !ok {
		return source.Patch(ctx, snapshot.Mode, file, snapshot.Head)
	}
	endpoints, err := git.previewEndpoints(ctx, snapshot.Mode, snapshot.Head)
	if err != nil {
		return review.FilePatch{}, err
	}
	if patch, handled, err := git.specialPatch(ctx, endpoints, file, snapshot.Head); handled || err != nil {
		return patch, err
	}
	// Git's no-index formatter also applies repository attributes and settings.
	// Keep this special path scalar rather than synthesizing differing patches.
	if file.Status == "?" || reader.scalarPatches[file.Fingerprint] {
		return git.Patch(ctx, snapshot.Mode, file, snapshot.Head)
	}
	if git.collection != nil {
		if objects, ok := git.collection.files[file.Fingerprint]; ok && !endpoints.objectsOnly() && (objects.before.mode == "160000" || objects.after.mode == "160000") {
			return git.Patch(ctx, snapshot.Mode, file, snapshot.Head)
		}
	}
	if patch, ok := reader.patches[file.Fingerprint]; ok {
		return patch, nil
	}
	// A bounded refill prevents large comparisons from becoming per-file reads.
	position := 0
	for index, candidate := range snapshot.Files {
		if candidate.ID == file.ID {
			position = index
			break
		}
	}
	batch := snapshot
	batch.Files = snapshot.Files[position:min(position+8, len(snapshot.Files))]

	args := append(endpoints.diffArgs(), "--patch", "--unified=5", "--submodule=short", "--")
	for _, candidate := range batch.Files {
		if candidate.OldPath != nil {
			args = append(args, *candidate.OldPath)
		}
		args = append(args, candidate.Path)
	}
	// Clear old prefetch entries so every member of this refill fits together.
	reader.patches = make(map[string]review.FilePatch)
	reader.patchBytes = 0
	if err := reader.readPatches(ctx, git.root, nil, []review.RepositoryDiff{batch}, true, args); err != nil {
		return review.FilePatch{}, err
	}
	if patch, ok := reader.patches[file.Fingerprint]; ok {
		return patch, nil
	}
	return review.FilePatch{}, errors.New("patch exceeds collection cache capacity")
}
