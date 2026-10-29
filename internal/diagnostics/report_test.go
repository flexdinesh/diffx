package diagnostics

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestBoundsPreserveTotalsAndSlowest(t *testing.T) {
	r := New(Metadata{AppVersion: "1.2.3", Operation: "sync"})
	for i := 0; i < 300; i++ {
		stage := r.BeginStage(StageInfo{Name: "collection", Scope: "all", Attempt: i + 1})
		finish := r.BeginCommand(CommandInfo{StageInfo: StageInfo{Name: "collection", Scope: "all"}, Operation: "diff"})
		status := 0
		finish(CommandResult{Elapsed: time.Duration(i) * time.Millisecond, QueueWait: 2 * time.Millisecond, StdoutBytes: 10, StderrBytes: 3, ExitStatus: &status, DeadlineKind: "none"})
		finish(CommandResult{StdoutBytes: 999}) // idempotent
		stage(nil)
		stage(&Failure{Kind: "git"})
		r.AddWorkload(Workload{Files: 2, Bytes: 100, Attempts: 1})
	}
	report := r.Finish(Outcome{Status: "success"})
	if len(report.LatestCommands) != 128 || len(report.SlowestCommands) != 20 || len(report.Stages) != 128 || report.DroppedCommands != 172 || report.DroppedStages != 172 {
		t.Fatalf("bounds: latest=%d slowest=%d stages=%d dropped=%d/%d", len(report.LatestCommands), len(report.SlowestCommands), len(report.Stages), report.DroppedCommands, report.DroppedStages)
	}
	if report.Totals.Commands != 300 || report.Totals.CompletedCommands != 300 || report.Totals.CommandElapsedMS != 44850 || report.Totals.QueueWaitMS != 600 || report.Totals.StdoutBytes != 3000 || report.Totals.StderrBytes != 900 || report.Totals.Workload.Files != 600 || report.Totals.Workload.Attempts != 300 {
		t.Fatalf("totals: %+v", report.Totals)
	}
	if report.LatestCommands[0].Sequence != 173 || report.SlowestCommands[0].ElapsedMS != 299 || report.SlowestCommands[19].ElapsedMS != 280 {
		t.Fatal("wrong latest/slowest commands")
	}
	if total := report.Totals.StageTotals[0]; total.Started != 300 || total.Completed != 300 || total.Attempts != 300 || total.Failed != 0 {
		t.Fatalf("stage totals: %+v", total)
	}
	if _, err := encode(report); err != nil {
		t.Fatal(err)
	}
}

func TestRedactionAndReportLocalPseudonyms(t *testing.T) {
	secret := "SECRET_password@example.org/private"
	metadata := Metadata{AppVersion: secret, BuildCommit: secret, BuildDate: secret, Platform: secret, Architecture: secret, GoVersion: secret, GitVersion: secret, GitPath: "/home/" + secret, Operation: secret}
	info := StageInfo{Name: secret, Scope: secret, Repository: "/home/" + secret, Ref: secret, URL: "https://user:" + secret + "?token=secret"}
	r := New(metadata)
	stage := r.BeginStage(info)
	stage(&Failure{Kind: secret, Detail: secret})
	for i := 0; i < 2; i++ {
		finish := r.BeginCommand(CommandInfo{StageInfo: info, Operation: secret})
		finish(CommandResult{DeadlineKind: secret, Failure: &Failure{Kind: "git", Detail: secret}})
	}
	report := r.Finish(Outcome{Status: "failure", Primary: &Failure{Kind: "git", Detail: secret}, Cleanup: []Failure{{Kind: secret, Detail: secret}}, Cancelled: true})
	data, err := encode(report)
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{"SECRET", "password", "example.org", "private", "https://", "/home/", "token="} {
		if strings.Contains(string(data), raw) {
			t.Fatalf("leaked %q: %s", raw, data)
		}
	}
	first, second := report.LatestCommands[0], report.LatestCommands[1]
	if first.Repository == "" || first.Repository != second.Repository || first.Repository != report.Metadata.GitPath || first.Ref == first.Repository {
		t.Fatal("pseudonyms are inconsistent or lack category separation")
	}
	other := New(metadata)
	other.BeginCommand(CommandInfo{StageInfo: info, Operation: "diff"})
	if other.Snapshot().InFlightCommands[0].Repository == first.Repository {
		t.Fatal("pseudonym stable across reports")
	}
	if report.Outcome.Primary.Kind != "git" || !report.Outcome.Cancelled || report.Outcome.Cleanup[0].Kind != "unknown" {
		t.Fatalf("outcome channels: %+v", report.Outcome)
	}
}

func TestInflightSnapshotsAndIndependentCopies(t *testing.T) {
	r := New(Metadata{})
	stage := r.BeginStage(StageInfo{Name: "collection"})
	finish := r.BeginCommand(CommandInfo{StageInfo: StageInfo{Name: "git", Repository: "/secret/repo"}, Operation: "diff"})
	snapshot := r.Snapshot()
	if len(snapshot.InFlightCommands) != 1 || !snapshot.InFlightCommands[0].InFlight || !snapshot.Stages[0].InFlight {
		t.Fatal("missing active lifecycle")
	}
	status := 128
	finish(CommandResult{ExitStatus: &status, DeadlineKind: "collection", Failure: &Failure{Kind: "timeout", Detail: "deadline-exceeded"}})
	stage(&Failure{Kind: "timeout"})
	status = 0
	report := r.Finish(Outcome{Status: "failure", Primary: &Failure{Kind: "timeout"}, Cancelled: true})
	if len(report.InFlightCommands) != 0 || *report.LatestCommands[0].ExitStatus != 128 || report.Totals.FailedCommands != 1 {
		t.Fatalf("completion: %+v", report)
	}
	report.LatestCommands[0].Failure.Kind = "modified"
	*report.LatestCommands[0].ExitStatus = 0
	report.Totals.StageTotals[0].Started = 99
	if r.Snapshot().LatestCommands[0].Failure.Kind != "timeout" || *r.Snapshot().LatestCommands[0].ExitStatus != 128 || r.Snapshot().Totals.StageTotals[0].Started != 1 {
		t.Fatal("snapshot aliases recorder memory")
	}
	if snapshot.InFlightCommands[0].InFlight != true {
		t.Fatal("previous snapshot changed")
	}
	frozen := r.Finish(Outcome{Status: "success"})
	if frozen.Outcome.Status != "failure" || frozen.ElapsedMS != report.ElapsedMS {
		t.Fatal("finish changed final outcome/time")
	}
}

func TestConcurrentRecorderAndInflightBounds(t *testing.T) {
	r := New(Metadata{})
	var finishes []func(CommandResult)
	for i := 0; i < 150; i++ {
		finishes = append(finishes, r.BeginCommand(CommandInfo{Operation: "status"}))
	}
	if report := r.Snapshot(); len(report.InFlightCommands) != 128 || report.DroppedInFlight != 22 || report.Totals.Commands != 150 {
		t.Fatal("active commands unbounded or totals lost")
	}
	var wg sync.WaitGroup
	for _, finish := range finishes {
		wg.Go(func() {
			finish(CommandResult{DeadlineKind: "none"})
			r.AddWorkload(Workload{Files: 1})
			_ = r.Snapshot()
		})
	}
	wg.Wait()
	report := r.Finish(Outcome{Status: "cancelled", Cancelled: true})
	if report.Totals.CompletedCommands != 150 || report.Totals.Workload.Files != 150 || len(report.InFlightCommands) != 0 {
		t.Fatal("concurrent events lost")
	}
	if _, err := json.Marshal(report); err != nil {
		t.Fatal(err)
	}
}

func TestLatestUsesStartOrderAndSlowestPreservesOldCommand(t *testing.T) {
	r := New(Metadata{})
	old := r.BeginCommand(CommandInfo{Operation: "diff"})
	for i := 0; i < 140; i++ {
		r.BeginCommand(CommandInfo{Operation: "status"})(CommandResult{Elapsed: time.Millisecond, DeadlineKind: "none"})
	}
	old(CommandResult{Elapsed: time.Hour, DeadlineKind: "none"})
	report := r.Snapshot()
	if report.LatestCommands[0].Sequence != 14 || report.SlowestCommands[0].Sequence != 1 {
		t.Fatal("late completion displaced newest commands or lost old slow command")
	}
}

func TestBuildMetadataVersions(t *testing.T) {
	for _, gitVersion := range []string{"2.51.0", "2.51.0.windows.1", "2.50.1 (Apple Git-155)"} {
		r := New(Metadata{AppVersion: "v1.2.3-rc1", BuildCommit: "0123456789abcdef", BuildDate: "2026-10-29T10:00:00+11:00", GoVersion: "go1.27.3", GitVersion: "git version " + gitVersion})
		report := r.Snapshot()
		if report.Metadata.GitVersion != gitVersion || report.Metadata.GoVersion != "1.27.3" || report.Metadata.BuildDate != "2026-10-28T23:00:00Z" {
			t.Fatalf("metadata lost: %+v", report.Metadata)
		}
		if _, err := encode(report); err != nil {
			t.Fatal(err)
		}
	}
}

func TestFinishFreezesInflightReport(t *testing.T) {
	r := New(Metadata{})
	stage := r.BeginStage(StageInfo{Name: "collection", Scope: "committed"})
	command := r.BeginCommand(CommandInfo{Operation: "patch-id"})
	finished := r.Finish(Outcome{Status: "cancelled", Cancelled: true})
	stage(nil)
	command(CommandResult{StdoutBytes: 100})
	r.BeginStage(StageInfo{Name: "cleanup"})(nil)
	r.BeginCommand(CommandInfo{Operation: "status"})(CommandResult{})
	r.AddWorkload(Workload{Files: 99})
	before, _ := json.Marshal(finished)
	after, _ := json.Marshal(r.Snapshot())
	if string(before) != string(after) {
		t.Fatal("events changed finished report")
	}
}
