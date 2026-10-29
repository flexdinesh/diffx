package diagnostics

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"

	"github.com/flexdinesh/diffx/internal/processlock"
	"github.com/google/uuid"
)

var (
	ErrInvalidReport = errors.New("invalid diagnostics report")
	ErrInvalidID     = errors.New("invalid diagnostics report ID")
	ErrNotFound      = errors.New("diagnostics report not found")
)

// ValidID permits canonical random UUIDs only, never file paths.
func ValidID(id string) bool {
	parsed, err := uuid.Parse(id)
	return err == nil && parsed.Version() == 4 && parsed.Variant() == uuid.RFC4122 && parsed.String() == id
}

// Save atomically stores a report under directory and retains the five newest
// reports. Every reader/writer uses the same persistent process lock. The caller
// decides whether a save error should be logged; it must not replace collection's
// primary result. No configuration or application services are consulted here.
func Save(directory string, report Report) error {
	data, err := encode(report)
	if err != nil {
		return err
	}
	root, lock, err := openStore(directory, true)
	if err != nil {
		return err
	}
	defer root.Close()
	defer lock.Close()
	name := ".tmp-" + uuid.NewString()
	file, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	defer root.Remove(name)
	if err = file.Chmod(0o600); err == nil {
		_, err = file.Write(data)
	}
	if err == nil {
		err = file.Sync()
	}
	err = errors.Join(err, file.Close())
	if err != nil {
		return err
	}
	if err := root.Rename(name, report.ID+".json"); err != nil {
		return err
	}
	entries, err := reports(root)
	if err != nil {
		return err
	}
	for _, entry := range entries[min(len(entries), MaxReports):] {
		if err := root.Remove(entry.Name()); err != nil {
			return err
		}
	}
	return nil
}

// Export copies one validated schema-1 report to a newly created private output
// file. latest and id are mutually exclusive. Existing output is never replaced,
// including symlinks. Reads reject report symlinks and files over 1 MiB.
func Export(directory, id string, latest bool, output string) error {
	if latest == (id != "") {
		return errors.New("choose exactly one report ID or --latest")
	}
	if id != "" && !ValidID(id) {
		return ErrInvalidID
	}
	if output == "" {
		return errors.New("diagnostics export requires --output")
	}
	root, lock, err := openStore(directory, false)
	if err != nil {
		return err
	}
	defer root.Close()
	defer lock.Close()
	if latest {
		entries, err := reports(root)
		if err != nil {
			return err
		}
		if len(entries) == 0 {
			return ErrNotFound
		}
		id = strings.TrimSuffix(entries[0].Name(), ".json")
	}
	data, err := readReport(root, id)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(output, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if err = file.Chmod(0o600); err == nil {
		_, err = file.Write(data)
	}
	if err == nil {
		err = file.Sync()
	}
	err = errors.Join(err, file.Close())
	if err != nil {
		_ = os.Remove(output)
	}
	return err
}

func openStore(directory string, create bool) (*os.Root, *processlock.Lock, error) {
	if create {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			return nil, nil, err
		}
	}
	info, err := os.Lstat(directory)
	if errors.Is(err, os.ErrNotExist) && !create {
		return nil, nil, ErrNotFound
	}
	if err != nil {
		return nil, nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, nil, errors.New("diagnostics directory must be a regular directory")
	}
	if err := os.Chmod(directory, 0o700); err != nil {
		return nil, nil, err
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return nil, nil, err
	}
	// processlock owns kernel coordination; never remove its persistent inode.
	if info, err := root.Lstat(".lock"); err == nil && !info.Mode().IsRegular() {
		root.Close()
		return nil, nil, errors.New("invalid diagnostics lock file")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		root.Close()
		return nil, nil, err
	}
	lock, err := processlock.Acquire(filepath.Join(directory, ".lock"))
	if err != nil {
		root.Close()
		return nil, nil, err
	}
	if err := root.Chmod(".lock", 0o600); err != nil {
		lock.Close()
		root.Close()
		return nil, nil, err
	}
	return root, lock, nil
}

func reports(root *os.Root) ([]os.FileInfo, error) {
	directory, err := root.Open(".")
	if err != nil {
		return nil, err
	}
	entries, err := directory.ReadDir(-1)
	err = errors.Join(err, directory.Close())
	if err != nil {
		return nil, err
	}
	var result []os.FileInfo
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".json") || !ValidID(strings.TrimSuffix(entry.Name(), ".json")) {
			continue
		}
		info, err := root.Lstat(entry.Name())
		if err != nil {
			return nil, err
		}
		if info.Mode().IsRegular() {
			result = append(result, info)
		}
	}
	slices.SortFunc(result, func(a, b os.FileInfo) int {
		if a.ModTime().After(b.ModTime()) {
			return -1
		}
		if a.ModTime().Before(b.ModTime()) {
			return 1
		}
		return strings.Compare(a.Name(), b.Name())
	})
	return result, nil
}

func readReport(root *os.Root, id string) ([]byte, error) {
	name := id + ".json"
	info, err := root.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > MaxReportBytes {
		return nil, ErrInvalidReport
	}
	file, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil {
		return nil, err
	}
	// A symlink swap between Lstat and Open must not result in reading its target.
	if !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
		return nil, ErrInvalidReport
	}
	data, err := io.ReadAll(io.LimitReader(file, MaxReportBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > MaxReportBytes {
		return nil, ErrInvalidReport
	}
	if !uniqueJSONKeys(data) {
		return nil, ErrInvalidReport
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var report Report
	if err := decoder.Decode(&report); err != nil {
		return nil, ErrInvalidReport
	}
	if decoder.Decode(new(json.RawMessage)) != io.EOF {
		return nil, ErrInvalidReport
	}
	if report.ID != id || validate(report) != nil {
		return nil, ErrInvalidReport
	}
	return data, nil
}

func encode(report Report) ([]byte, error) {
	if err := validate(report); err != nil {
		return nil, err
	}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return nil, ErrInvalidReport
	}
	data = append(data, '\n')
	if len(data) > MaxReportBytes {
		return nil, ErrInvalidReport
	}
	return data, nil
}

func validate(report Report) error {
	if report.SchemaVersion != SchemaVersion || !ValidID(report.ID) || report.StartedAt.IsZero() || report.ElapsedMS < 0 {
		return ErrInvalidReport
	}
	if cleanMetadata(report.Metadata) != report.Metadata || !reflect.DeepEqual(cleanOutcome(report.Outcome), report.Outcome) {
		return ErrInvalidReport
	}
	totals := report.Totals
	w := totals.Workload
	if totals.CompletedCommands > totals.Commands || totals.FailedCommands > totals.CompletedCommands || totals.CommandElapsedMS < 0 || totals.QueueWaitMS < 0 || totals.StdoutBytes < 0 || totals.StderrBytes < 0 || w.Repositories < 0 || w.Checkouts < 0 || w.Scopes < 0 || w.Attempts < 0 || w.Files < 0 || w.Commits < 0 || w.Bytes < 0 {
		return ErrInvalidReport
	}
	if len(report.Stages) > MaxStages || len(report.LatestCommands) > MaxCommands || len(report.SlowestCommands) > MaxSlowest || len(report.InFlightCommands) > MaxCommands {
		return ErrInvalidReport
	}
	if len(report.Totals.StageTotals) > MaxStageTotals {
		return ErrInvalidReport
	}
	for _, total := range report.Totals.StageTotals {
		if !validStage(StageInfo{Name: total.Name, Scope: total.Scope}) || total.ElapsedMS < 0 {
			return ErrInvalidReport
		}
	}
	for _, stage := range report.Stages {
		if !validStage(stage.StageInfo) || stage.StartedMS < 0 || stage.ElapsedMS < 0 || !reflect.DeepEqual(cleanFailure(stage.Failure), stage.Failure) {
			return ErrInvalidReport
		}
	}
	for _, commands := range [][]CommandRecord{report.LatestCommands, report.SlowestCommands, report.InFlightCommands} {
		for _, command := range commands {
			if !validStage(command.StageInfo) || commandOperation(command.Operation) != command.Operation || choice(command.DeadlineKind, "none", "command", "collection", "caller", "cancelled") != command.DeadlineKind || command.StartedMS < 0 || command.ElapsedMS < 0 || command.QueueWaitMS < 0 || command.StdoutBytes < 0 || command.StderrBytes < 0 || !reflect.DeepEqual(cleanFailure(command.Failure), command.Failure) {
				return ErrInvalidReport
			}
		}
	}
	return nil
}

// encoding/json accepts duplicate (even differently cased) struct keys. Reject
// them before copying bytes so an overwritten field cannot smuggle raw secrets.
func uniqueJSONKeys(data []byte) bool {
	decoder := json.NewDecoder(bytes.NewReader(data))
	var value func() bool
	value = func() bool {
		token, err := decoder.Token()
		if err != nil {
			return false
		}
		delimiter, container := token.(json.Delim)
		if !container {
			return true
		}
		switch delimiter {
		case '{':
			seen := make(map[string]bool)
			for decoder.More() {
				token, err := decoder.Token()
				if err != nil {
					return false
				}
				key, ok := token.(string)
				if !ok {
					return false
				}
				// Schema keys are ASCII. Unicode case folding in encoding/json
				// otherwise admits aliases such as long-s for an ASCII s.
				for _, character := range key {
					if character > 127 {
						return false
					}
				}
				key = strings.ToLower(key)
				if seen[key] {
					return false
				}
				seen[key] = true
				if !value() {
					return false
				}
			}
		case '[':
			for decoder.More() {
				if !value() {
					return false
				}
			}
		default:
			return false
		}
		_, err = decoder.Token()
		return err == nil
	}
	if !value() {
		return false
	}
	_, err := decoder.Token()
	return err == io.EOF
}

func validStage(info StageInfo) bool {
	for _, value := range []string{info.Repository, info.Ref, info.URL} {
		if value != "" && !pseudoPattern.MatchString(value) {
			return false
		}
	}
	info.Repository, info.Ref, info.URL = "", "", ""
	probe := &Recorder{}
	return probe.cleanStage(info) == info
}
