package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// validationRecord records only completed successful groups. The caller owns
// the complete execution-input fingerprint and must never save partial results.
type validationRecord struct {
	Group       string    `json:"group"`
	Fingerprint string    `json:"fingerprint"`
	Commit      string    `json:"commit"`
	Artifacts   string    `json:"artifacts"`
	CompletedAt time.Time `json:"completedAt"`
}

func recordDirectory(ctx context.Context, root string) (string, error) {
	common, err := snapshotGit(ctx, root, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return "", err
	}
	canonical, err := filepath.EvalSymlinks(strings.TrimSpace(string(common)))
	if err != nil {
		return "", err
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	identity := sha256.Sum256([]byte(canonical))
	return filepath.Join(cache, "diffx", "validation", hex.EncodeToString(identity[:])), nil
}

func validationRecordPath(directory, group, fingerprint string) (string, error) {
	if group == "" || strings.ContainsAny(group, `/\`) || group == "." || group == ".." {
		return "", fmt.Errorf("invalid validation group %q", group)
	}
	decoded, err := hex.DecodeString(fingerprint)
	if err != nil || len(decoded) != sha256.Size {
		return "", errors.New("validation fingerprint must be a SHA-256 digest")
	}
	return filepath.Join(directory, group, fingerprint+".json"), nil
}

func loadValidationRecord(directory, group, fingerprint string) (validationRecord, bool, error) {
	var record validationRecord
	path, err := validationRecordPath(directory, group, fingerprint)
	if err != nil {
		return record, false, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return record, false, nil
	}
	if err != nil {
		return record, false, err
	}
	if err := json.Unmarshal(data, &record); err != nil {
		return validationRecord{}, false, nil // Corruption cannot certify success.
	}
	if record.Group != group || record.Fingerprint != fingerprint || record.CompletedAt.IsZero() {
		return validationRecord{}, false, nil
	}
	return record, true, nil
}

func saveValidationRecord(directory string, record validationRecord) error {
	path, err := validationRecordPath(directory, record.Group, record.Fingerprint)
	if err != nil {
		return err
	}
	if record.CompletedAt.IsZero() {
		return errors.New("cannot save validation without a completion time")
	}
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".validation-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	_, writeErr := file.Write(append(data, '\n'))
	err = errors.Join(writeErr, file.Sync(), file.Close())
	if err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}
