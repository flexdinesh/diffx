package diagnostics

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

func sampleReport() Report {
	return New(Metadata{AppVersion: "1.2.3", GitVersion: "git version 2.51.0", GoVersion: "go1.27.3", Platform: runtime.GOOS, Architecture: runtime.GOARCH, Operation: "sync"}).Finish(Outcome{Status: "failure", Primary: &Failure{Kind: "git", Detail: "command-failed"}})
}

func TestSaveExportPermissionsAndRetention(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "diagnostics")
	var last Report
	for i := 0; i < 8; i++ {
		last = sampleReport()
		if err := Save(directory, last); err != nil {
			t.Fatal(err)
		}
		// Explicit timestamps make retention ordering deterministic on every FS.
		stamp := time.Unix(100+int64(i), 0)
		if err := os.Chtimes(filepath.Join(directory, last.ID+".json"), stamp, stamp); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".json") {
			count++
			info, _ := entry.Info()
			if info.Size() > MaxReportBytes {
				t.Fatal("oversized saved report")
			}
			assertMode(t, filepath.Join(directory, entry.Name()), 0o600)
		}
	}
	if count != 5 {
		t.Fatalf("retained %d reports", count)
	}
	assertMode(t, directory, 0o700)
	assertMode(t, filepath.Join(directory, ".lock"), 0o600)
	output := filepath.Join(t.TempDir(), "failure.json")
	if err := Export(directory, "", true, output); err != nil {
		t.Fatal(err)
	}
	assertMode(t, output, 0o600)
	data, _ := os.ReadFile(output)
	var copied Report
	if err := json.Unmarshal(data, &copied); err != nil {
		t.Fatal(err)
	}
	if copied.ID != last.ID || copied.SchemaVersion != 1 {
		t.Fatalf("wrong latest report: %+v", copied)
	}
	original, _ := os.ReadFile(filepath.Join(directory, last.ID+".json"))
	if !bytes.Equal(data, original) {
		t.Fatal("export changed report")
	}
	if err := Export(directory, last.ID, false, output); !errors.Is(err, os.ErrExist) {
		t.Fatalf("overwrite: %v", err)
	}
	if after, _ := os.ReadFile(output); !bytes.Equal(after, data) {
		t.Fatal("existing output modified")
	}
}

func TestConcurrentSavesCoordinateRetention(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "diagnostics")
	var wg sync.WaitGroup
	for i := 0; i < 24; i++ {
		wg.Go(func() {
			if err := Save(directory, sampleReport()); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".json") {
			if entry.Name() != ".lock" {
				t.Errorf("leftover temporary file: %s", entry.Name())
			}
			continue
		}
		count++
		if err := Export(directory, strings.TrimSuffix(entry.Name(), ".json"), false, filepath.Join(t.TempDir(), "export.json")); err != nil {
			t.Fatal(err)
		}
	}
	if count != MaxReports {
		t.Fatalf("concurrent retention = %d", count)
	}
}

func TestExportValidationAndSymlinks(t *testing.T) {
	directory := t.TempDir()
	report := sampleReport()
	if err := Save(directory, report); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"../escape", report.ID + ".json", "", "ABC", uuid.Nil.String(), strings.ToUpper(report.ID)} {
		if err := Export(directory, id, false, filepath.Join(t.TempDir(), "out")); err == nil {
			t.Fatalf("accepted ID %q", id)
		}
	}
	if err := Export(directory, report.ID, true, filepath.Join(t.TempDir(), "out")); err == nil {
		t.Fatal("accepted ID plus latest")
	}
	if err := Export(directory, report.ID, false, ""); err == nil {
		t.Fatal("accepted missing output")
	}
	if err := Export(directory, uuid.NewString(), false, filepath.Join(t.TempDir(), "out")); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if err := Export(t.TempDir(), "", true, filepath.Join(t.TempDir(), "out")); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if runtime.GOOS == "windows" {
		return
	}
	target := filepath.Join(t.TempDir(), "target")
	if err := os.WriteFile(target, []byte("private data"), 0o600); err != nil {
		t.Fatal(err)
	}
	id := uuid.NewString()
	if err := os.Symlink(target, filepath.Join(directory, id+".json")); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "out")
	if err := Export(directory, id, false, output); !errors.Is(err, ErrInvalidReport) {
		t.Fatalf("followed report symlink: %v", err)
	}
	if _, err := os.Stat(output); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("created output for invalid report")
	}
	if err := os.Symlink(target, output); err != nil {
		t.Fatal(err)
	}
	if err := Export(directory, report.ID, false, output); !errors.Is(err, os.ErrExist) {
		t.Fatalf("followed output symlink: %v", err)
	}
	if data, _ := os.ReadFile(target); string(data) != "private data" {
		t.Fatal("symlink target modified")
	}
}

func TestRefusesInvalidStoredReports(t *testing.T) {
	for _, mutation := range []string{"version", "unknown-field", "oversized", "id", "secret", "duplicate", "duplicate-case", "duplicate-unicode", "trailing-json", "malformed"} {
		t.Run(mutation, func(t *testing.T) {
			directory := t.TempDir()
			report := sampleReport()
			data, _ := encode(report)
			switch mutation {
			case "version":
				data = bytes.Replace(data, []byte(`"schemaVersion": 1`), []byte(`"schemaVersion": 2`), 1)
			case "unknown-field":
				data = append([]byte(`{"rawArgs":"secret",`), data[1:]...)
			case "oversized":
				data = bytes.Repeat([]byte(" "), MaxReportBytes+1)
			case "id":
				data = bytes.ReplaceAll(data, []byte(report.ID), []byte(uuid.NewString()))
			case "secret":
				data = bytes.Replace(data, []byte(`"command-failed"`), []byte(`"https://password@secret"`), 1)
			case "duplicate":
				data = bytes.Replace(data, []byte(`"detail": "command-failed"`), []byte(`"detail": "secret", "detail": "command-failed"`), 1)
			case "duplicate-case":
				data = bytes.Replace(data, []byte(`"detail": "command-failed"`), []byte(`"Detail": "secret", "detail": "command-failed"`), 1)
			case "duplicate-unicode":
				data = bytes.Replace(data, []byte(`"status": "failure"`), []byte(`"ſtatus": "secret", "status": "failure"`), 1)
			case "trailing-json":
				data = append(data, []byte(`{}`)...)
			case "malformed":
				data = []byte("{broken")
			}
			if err := os.WriteFile(filepath.Join(directory, report.ID+".json"), data, 0o600); err != nil {
				t.Fatal(err)
			}
			output := filepath.Join(t.TempDir(), "out")
			if err := Export(directory, report.ID, false, output); !errors.Is(err, ErrInvalidReport) {
				t.Fatalf("accepted %s: %v", mutation, err)
			}
			if _, err := os.Stat(output); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("invalid report created export")
			}
		})
	}
}

func TestSaveRejectsInvalidSchemaAndUnwritableStorage(t *testing.T) {
	report := sampleReport()
	report.SchemaVersion = 2
	if err := Save(t.TempDir(), report); !errors.Is(err, ErrInvalidReport) {
		t.Fatal(err)
	}
	report = sampleReport()
	report.Metadata.GitPath = "/private/token"
	if err := Save(t.TempDir(), report); !errors.Is(err, ErrInvalidReport) {
		t.Fatal("accepted unredacted report")
	}
	blocked := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocked, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Save(filepath.Join(blocked, "diagnostics"), sampleReport()); err == nil {
		t.Fatal("saved into a file")
	}
	if err := Export(blocked, "", true, filepath.Join(t.TempDir(), "out")); err == nil {
		t.Fatal("read a file as directory")
	}
	if err := Export(filepath.Join(t.TempDir(), "absent"), "", true, filepath.Join(t.TempDir(), "out")); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	directory := t.TempDir()
	report = sampleReport()
	if err := Save(directory, report); err != nil {
		t.Fatal(err)
	}
	if err := Export(directory, report.ID, false, filepath.Join(blocked, "out")); err == nil {
		t.Fatal("exported into unwritable output")
	}
	if runtime.GOOS != "windows" {
		directory := t.TempDir()
		if err := os.Symlink(blocked, filepath.Join(directory, ".lock")); err != nil {
			t.Fatal(err)
		}
		if err := Save(directory, sampleReport()); err == nil {
			t.Fatal("accepted lock symlink")
		}
	}
}

func assertMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	if runtime.GOOS == "windows" {
		return
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != want {
		t.Fatalf("%s mode=%o want=%o", path, info.Mode().Perm(), want)
	}
}
