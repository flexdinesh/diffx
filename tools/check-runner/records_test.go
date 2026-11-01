package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestValidationRecordMatchesCompleteInputIdentity(t *testing.T) {
	directory := t.TempDir()
	record := validationRecord{Group: "go", Fingerprint: strings.Repeat("a", 64), Commit: "trace-only", Artifacts: "/past/artifacts", CompletedAt: time.Now().UTC()}
	if err := saveValidationRecord(directory, record); err != nil {
		t.Fatal(err)
	}
	got, found, err := loadValidationRecord(directory, record.Group, record.Fingerprint)
	if err != nil || !found || got != record {
		t.Fatalf("round trip: %+v, %v, %v", got, found, err)
	}
	if _, found, err := loadValidationRecord(directory, record.Group, strings.Repeat("b", 64)); err != nil || found {
		t.Fatalf("different inputs reused success: %v, %v", found, err)
	}
	if _, found, err := loadValidationRecord(directory, "browser", record.Fingerprint); err != nil || found {
		t.Fatalf("different group reused success: %v, %v", found, err)
	}
	entries, err := os.ReadDir(filepath.Join(directory, record.Group))
	if err != nil || len(entries) != 1 || strings.HasPrefix(entries[0].Name(), ".") {
		t.Fatalf("temporary record leaked: %v, %v", entries, err)
	}
}

func TestValidationRecordRejectsIncompleteAndCorrupt(t *testing.T) {
	directory := t.TempDir()
	record := validationRecord{Group: "go", Fingerprint: strings.Repeat("a", 64)}
	if err := saveValidationRecord(directory, record); err == nil {
		t.Fatal("saved incomplete validation")
	}
	path, err := validationRecordPath(directory, record.Group, record.Fingerprint)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, found, err := loadValidationRecord(directory, record.Group, record.Fingerprint); err != nil || found {
		t.Fatalf("corruption certified success: %v, %v", found, err)
	}
	for _, group := range []string{"", "../escape", "..", "a/b"} {
		if _, err := validationRecordPath(directory, group, record.Fingerprint); err == nil {
			t.Fatalf("accepted group %q", group)
		}
	}
}
