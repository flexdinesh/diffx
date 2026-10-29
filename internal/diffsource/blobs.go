package diffsource

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/flexdinesh/diffx/internal/review"
)

const collectionBlobBytes = 16 << 20

func emptyObject(object indexObject) bool {
	return object.mode == "000000" || object.oid == "" || strings.Trim(object.oid, "0") == ""
}

func (reader *CollectionPreviews) loadBlobs(ctx context.Context, git *gitSource, files []review.ChangedFile) error {
	var oids []string
	seen := make(map[string]bool)
	touched := make(map[string]bool)
	for _, file := range files {
		objects, ok := git.collection.files[file.Fingerprint]
		if !ok || file.Binary || file.Status == "U" {
			continue
		}
		for _, object := range []indexObject{objects.before, objects.after} {
			if emptyObject(object) || object.mode == "160000" || seen[object.oid] {
				continue
			}
			seen[object.oid] = true
			if _, ok := reader.blobs[object.oid]; ok {
				touched[object.oid] = true
				continue
			}
			if _, ok := reader.unavailable[object.oid]; ok {
				continue
			}
			oids = append(oids, object.oid)
		}
	}
	if len(oids) == 0 {
		return ctx.Err()
	}
	// Keep already cached members of this batch until all of its new members
	// arrive. Their combined payload is bounded by the four-file read window.
	if len(touched) > 0 {
		order := make([]string, 0, len(reader.blobOrder))
		for _, oid := range reader.blobOrder {
			if !touched[oid] {
				order = append(order, oid)
			}
		}
		for _, oid := range reader.blobOrder {
			if touched[oid] {
				order = append(order, oid)
			}
		}
		reader.blobOrder = order
	}
	var contents []string
	err := runGitStream(ctx, git.root, strings.NewReader(strings.Join(oids, "\n")+"\n"), func(stream *bufio.Reader) error {
		for _, oid := range oids {
			line, err := readDelimited(stream, '\n', 256)
			if err != nil {
				return err
			}
			fields := strings.Fields(line)
			if len(fields) != 3 || fields[0] != oid {
				return fmt.Errorf("invalid cat-file size frame: %q", line)
			}
			size, err := strconv.ParseInt(fields[2], 10, 64)
			if err != nil || size < 0 {
				return errors.New("invalid cat-file size")
			}
			if fields[1] != "blob" {
				reader.unavailable[oid] = Error(400, "Full text is unavailable for this file")
			} else if size > MaxFileBytes {
				reader.unavailable[oid] = errOutputLimit
			} else {
				contents = append(contents, oid)
			}
		}
		_, err := stream.Peek(1)
		if errors.Is(err, io.EOF) {
			return nil
		}
		return errors.New("unexpected cat-file size output")
	}, "cat-file", "--batch-check=%(objectname) %(objecttype) %(objectsize)")
	if err != nil || len(contents) == 0 {
		return err
	}
	return runGitStream(ctx, git.root, strings.NewReader(strings.Join(contents, "\n")+"\n"), func(stream *bufio.Reader) error {
		for _, oid := range contents {
			line, err := readDelimited(stream, '\n', 256)
			if err != nil {
				return err
			}
			fields := strings.Fields(line)
			if len(fields) != 3 || fields[0] != oid || fields[1] != "blob" {
				return errors.New("invalid cat-file contents frame")
			}
			size, err := strconv.Atoi(fields[2])
			if err != nil || size < 0 || size > MaxFileBytes {
				return errors.New("invalid cat-file contents size")
			}
			raw := make([]byte, size)
			if _, err := io.ReadFull(stream, raw); err != nil {
				return err
			}
			newline, err := stream.ReadByte()
			if err != nil {
				return err
			}
			if newline != '\n' {
				return errors.New("invalid cat-file contents terminator")
			}
			value := string(raw)
			for reader.blobBytes+len(value) > collectionBlobBytes {
				oldest := reader.blobOrder[0]
				reader.blobOrder = reader.blobOrder[1:]
				reader.blobBytes -= len(reader.blobs[oldest])
				delete(reader.blobs, oldest)
			}
			reader.blobs[oid] = value
			reader.blobBytes += len(value)
			reader.blobOrder = append(reader.blobOrder, oid)
		}
		_, err := stream.Peek(1)
		if errors.Is(err, io.EOF) {
			return nil
		}
		return errors.New("unexpected cat-file contents output")
	}, "cat-file", "--batch")
}

func (reader *CollectionPreviews) Contents(ctx context.Context, snapshot review.RepositoryDiff, file review.ChangedFile) (review.FileContents, error) {
	ctx = commandScope(ctx, string(snapshot.Mode))
	if err := ctx.Err(); err != nil {
		return review.FileContents{}, err
	}
	source := reader.target(snapshot)
	git, ok := source.(*gitSource)
	if !ok || git.collection == nil {
		return source.Contents(ctx, snapshot.Mode, file, snapshot.Head)
	}
	objects, known := git.collection.files[file.Fingerprint]
	if !known && file.Status != "?" {
		return source.Contents(ctx, snapshot.Mode, file, snapshot.Head)
	}
	if !known {
		objects = fileObjects{before: indexObject{mode: "000000"}, after: indexObject{mode: "000000"}}
	}
	if objects.before.mode == "160000" || objects.after.mode == "160000" {
		return source.Contents(ctx, snapshot.Mode, file, snapshot.Head)
	}
	if file.Binary || file.Status == "U" {
		return review.FileContents{}, Error(400, "Full text is unavailable for this file")
	}
	endpoints, err := git.previewEndpoints(ctx, snapshot.Mode, snapshot.Head)
	if err != nil {
		return review.FileContents{}, err
	}
	// At most four files, with two <=2 MiB blobs each, fit the 16 MiB cache.
	position := 0
	for index, candidate := range snapshot.Files {
		if candidate.ID == file.ID {
			position = index
			break
		}
	}
	needsRead := false
	for _, object := range []indexObject{objects.before, objects.after} {
		_, cached := reader.blobs[object.oid]
		_, unavailable := reader.unavailable[object.oid]
		needsRead = needsRead || !emptyObject(object) && object.mode != "160000" && !cached && !unavailable
	}
	if needsRead {
		if err := reader.loadBlobs(ctx, git, snapshot.Files[position:min(position+4, len(snapshot.Files))]); err != nil {
			return review.FileContents{}, err
		}
	}
	read := func(object indexObject) (string, error) {
		if emptyObject(object) {
			return "", nil
		}
		if object.mode == "160000" {
			return "", Error(400, "Full text is unavailable for this file")
		}
		if err, ok := reader.unavailable[object.oid]; ok {
			return "", err
		}
		value, ok := reader.blobs[object.oid]
		if !ok {
			return "", errors.New("blob missing from collection batch")
		}
		return value, nil
	}
	before, err := read(objects.before)
	if err != nil {
		return review.FileContents{}, err
	}
	after := ""
	if endpoints.after.kind != worktreeEndpoint {
		after, err = read(objects.after)
	} else if file.Status != "D" || file.Recreated {
		path := filepath.Join(git.root, filepath.FromSlash(file.Path))
		stat, statErr := os.Lstat(path)
		if statErr != nil {
			return review.FileContents{}, statErr
		}
		if stat.Size() > MaxFileBytes {
			return review.FileContents{}, Error(413, "File exceeds the 2 MiB preview limit")
		}
		if stat.Mode()&os.ModeSymlink != 0 {
			after, err = os.Readlink(path)
		} else {
			// A bounded read also handles a file growing after Lstat.
			input, openErr := os.Open(path)
			if openErr != nil {
				return review.FileContents{}, openErr
			}
			raw, readErr := io.ReadAll(io.LimitReader(&contextReader{ctx: ctx, reader: input}, MaxFileBytes+1))
			err = errors.Join(readErr, input.Close())
			if len(raw) > MaxFileBytes {
				return review.FileContents{}, errOutputLimit
			}
			after = string(raw)
		}
	}
	if err != nil {
		return review.FileContents{}, err
	}
	if !validUTF8(before) || !validUTF8(after) {
		return review.FileContents{}, Error(400, "Full text is unavailable for binary files")
	}
	return review.FileContents{Before: before, After: after}, nil
}
