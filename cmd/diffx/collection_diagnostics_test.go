package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/flexdinesh/diffx/internal/collector"
	"github.com/flexdinesh/diffx/internal/diagnostics"
	"github.com/flexdinesh/diffx/internal/ingestion"
	"github.com/flexdinesh/diffx/internal/review"
)

func TestCollectionDiagnosticsEmptyWorkspaceRetainsDiscoveryFailure(t *testing.T) {
	directory := t.TempDir()
	t.Setenv("DIFFX_RUNTIME_DIR", directory)
	ctx := withCollectionDiagnostics(t.Context(), options{}, &bytes.Buffer{})
	_, err := collector.CollectWorkspaces(ctx, collector.Workspaces{Paths: []string{t.TempDir()}}, collector.Options{SourceID: "test", Hostname: "test"}, func(ingestion.Request) error { t.Fatal("published an empty workspace"); return nil })
	var annotated *review.DiagnosticError
	if !errors.As(err, &annotated) || annotated.Stage != "discovery" {
		t.Fatalf("missing discovery failure: %v", err)
	}
	report := savedCollectionReport(t, directory)
	if report.ID != annotated.ID || report.Outcome.Status != "failure" || report.Totals.Commands == 0 {
		t.Fatalf("missing discovery evidence: %+v", report)
	}
}

func savedCollectionReport(t *testing.T, directory string) diagnostics.Report {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(directory, "diagnostics", "*.json"))
	if err != nil || len(files) != 1 {
		t.Fatalf("reports %v: %v", files, err)
	}
	raw, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	var report diagnostics.Report
	if err := json.Unmarshal(raw, &report); err != nil {
		t.Fatal(err)
	}
	return report
}

func TestCollectionDiagnosticsFailurePreservesCauseAndExports(t *testing.T) {
	directory := t.TempDir()
	t.Setenv("DIFFX_RUNTIME_DIR", directory)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	var output bytes.Buffer
	ctx = withCollectionDiagnostics(ctx, options{}, &output)
	_, err := collector.Collect(ctx, filepath.Join(t.TempDir(), "private-checkout"), collector.Options{SourceID: "test", Hostname: "test"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("lost cancellation: %v", err)
	}
	var annotated *review.DiagnosticError
	if !errors.As(err, &annotated) || annotated.Code != "collection_cancelled" {
		t.Fatalf("missing report reference: %v", err)
	}
	report := savedCollectionReport(t, directory)
	if report.ID != annotated.ID || report.Outcome.Primary == nil || report.Outcome.Primary.Kind != "cancelled" {
		t.Fatalf("outcome %+v", report.Outcome)
	}
	if !strings.Contains(output.String(), "diffx diagnostics export "+report.ID) {
		t.Fatalf("missing export command: %s", output.String())
	}
	exported := filepath.Join(t.TempDir(), "failure.json")
	if err := runDiagnostics([]string{"export", annotated.ID, "--output", exported}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
}

func TestCollectionDiagnosticsDebugContainsMetadataWithoutContents(t *testing.T) {
	root := cliRepository(t)
	secret := "private-source-contents-9b6f"
	if err := os.WriteFile(filepath.Join(root, "secret.txt"), []byte(secret+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	t.Setenv("DIFFX_RUNTIME_DIR", directory)
	var output bytes.Buffer
	_, err := collectSubmission(t.Context(), options{directory: root, sourceID: "test", debug: true, diagnosticWriter: &output})
	if err != nil {
		t.Fatal(err)
	}
	report := savedCollectionReport(t, directory)
	if report.Outcome.Status != "success" || report.Totals.Commands == 0 || report.Totals.Workload.Files == 0 {
		t.Fatalf("incomplete report: %+v", report.Totals)
	}
	for _, command := range report.LatestCommands {
		if command.InFlight || command.DeadlineKind != "none" {
			t.Fatalf("unfinished command: %+v", command)
		}
	}
	raw, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{root, "secret.txt", secret} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatalf("report leaked %q", forbidden)
		}
	}
	if !strings.Contains(output.String(), "collect ·") {
		t.Fatalf("missing progress: %s", output.String())
	}
}

func TestCollectionDiagnosticsSuccessIsNotPersistedByDefault(t *testing.T) {
	root := cliRepository(t)
	directory := t.TempDir()
	t.Setenv("DIFFX_RUNTIME_DIR", directory)
	if _, err := collectSubmission(t.Context(), options{directory: root, sourceID: "test"}); err != nil {
		t.Fatal(err)
	}
	files, err := filepath.Glob(filepath.Join(directory, "diagnostics", "*.json"))
	if err != nil || len(files) != 0 {
		t.Fatalf("unexpected success report: %v %v", files, err)
	}
}

func TestCollectionDiagnosticsStorageFailureDoesNotReplaceCollectionFailure(t *testing.T) {
	file := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(file, []byte("occupied"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DIFFX_RUNTIME_DIR", file)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	var output bytes.Buffer
	ctx = withCollectionDiagnostics(ctx, options{}, &output)
	_, err := collector.Collect(ctx, t.TempDir(), collector.Options{SourceID: "test", Hostname: "test"})
	var annotated *review.DiagnosticError
	if !errors.Is(err, context.Canceled) || errors.As(err, &annotated) {
		t.Fatalf("storage replaced failure: %v", err)
	}
	if !strings.Contains(output.String(), "could not save collection diagnostics") {
		t.Fatal("missing persistence warning")
	}
}
