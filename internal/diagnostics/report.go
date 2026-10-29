// Package diagnostics records bounded, content-free collection diagnostics.
// Callers supply metadata and structured events, never argv, output or error text.
package diagnostics

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"slices"
	"sync"
	"time"

	"github.com/google/uuid"
)

const (
	SchemaVersion  = 1
	MaxCommands    = 128
	MaxSlowest     = 20
	MaxStages      = 128
	MaxStageTotals = 512
	MaxReportBytes = 1 << 20
	MaxReports     = 5
)

// Metadata contains build/runtime facts only. GitPath is pseudonymized by New.
// Unrecognized versions/platforms/operations are replaced with "unknown".
type Metadata struct {
	AppVersion    string `json:"appVersion"`
	BuildCommit   string `json:"buildCommit,omitempty"`
	BuildDate     string `json:"buildDate,omitempty"`
	BuildModified bool   `json:"buildModified"`
	Platform      string `json:"platform"`
	Architecture  string `json:"architecture"`
	GoVersion     string `json:"goVersion"`
	GitVersion    string `json:"gitVersion"`
	GitPath       string `json:"gitPath,omitempty"`
	Operation     string `json:"operation"`
}

// Failure deliberately has no error/string-message field. Unknown values become
// "unknown"; Detail is an allowlisted summary code, never exception text.
type Failure struct {
	Kind   string `json:"kind"`
	Detail string `json:"detail,omitempty"`
	Phase  string `json:"phase,omitempty"`
	Code   string `json:"code,omitempty"`
}

type Outcome struct {
	Status    string    `json:"status"`
	Primary   *Failure  `json:"primary,omitempty"`
	Cancelled bool      `json:"cancelled"`
	Cleanup   []Failure `json:"cleanup,omitempty"`
}

type Workload struct {
	Repositories int64 `json:"repositories"`
	Checkouts    int64 `json:"checkouts"`
	Scopes       int64 `json:"scopes"`
	Attempts     int64 `json:"attempts"`
	Files        int64 `json:"files"`
	Commits      int64 `json:"commits"`
	Bytes        int64 `json:"bytes"`
}

// StageInfo and CommandInfo use fixed category names. Repository, Ref and URL
// accept raw identifiers only to hash them immediately, with a report-local key.
type StageInfo struct {
	Name       string `json:"name"`
	Scope      string `json:"scope,omitempty"`
	Attempt    int    `json:"attempt"`
	Repository string `json:"repository,omitempty"`
	Ref        string `json:"ref,omitempty"`
	URL        string `json:"url,omitempty"`
}

type StageRecord struct {
	StageInfo
	Sequence  uint64   `json:"sequence"`
	StartedMS int64    `json:"startedMs"`
	ElapsedMS int64    `json:"elapsedMs"`
	InFlight  bool     `json:"inFlight"`
	Failure   *Failure `json:"failure,omitempty"`
}

type CommandInfo struct {
	StageInfo
	Operation string `json:"operation"`
}

// CommandResult includes queue wait separately from process execution time.
// ExitStatus is nil when a process never produced an exit status.
type CommandResult struct {
	QueueWait    time.Duration
	Elapsed      time.Duration
	StdoutBytes  int64
	StderrBytes  int64
	ExitStatus   *int
	DeadlineKind string
	Failure      *Failure
}

type CommandRecord struct {
	CommandInfo
	Sequence     uint64   `json:"sequence"`
	StartedMS    int64    `json:"startedMs"`
	ElapsedMS    int64    `json:"elapsedMs"`
	QueueWaitMS  int64    `json:"queueWaitMs"`
	StdoutBytes  int64    `json:"stdoutBytes"`
	StderrBytes  int64    `json:"stderrBytes"`
	ExitStatus   *int     `json:"exitStatus,omitempty"`
	DeadlineKind string   `json:"deadlineKind"`
	InFlight     bool     `json:"inFlight"`
	Failure      *Failure `json:"failure,omitempty"`
}

type Totals struct {
	Commands          uint64       `json:"commands"`
	CompletedCommands uint64       `json:"completedCommands"`
	FailedCommands    uint64       `json:"failedCommands"`
	CommandElapsedMS  int64        `json:"commandElapsedMs"`
	QueueWaitMS       int64        `json:"queueWaitMs"`
	StdoutBytes       int64        `json:"stdoutBytes"`
	StderrBytes       int64        `json:"stderrBytes"`
	Stages            uint64       `json:"stages"`
	Workload          Workload     `json:"workload"`
	StageTotals       []StageTotal `json:"stageTotals"`
}

// StageTotal aggregates every event by its fixed stage and scope categories,
// independently of the bounded individual stage list.
type StageTotal struct {
	Name      string `json:"name"`
	Scope     string `json:"scope,omitempty"`
	Started   uint64 `json:"started"`
	Completed uint64 `json:"completed"`
	Failed    uint64 `json:"failed"`
	Attempts  uint64 `json:"attempts"`
	ElapsedMS int64  `json:"elapsedMs"`
}

type Report struct {
	SchemaVersion    int             `json:"schemaVersion"`
	ID               string          `json:"id"`
	Metadata         Metadata        `json:"metadata"`
	StartedAt        time.Time       `json:"startedAt"`
	ElapsedMS        int64           `json:"elapsedMs"`
	Outcome          Outcome         `json:"outcome"`
	Totals           Totals          `json:"totals"`
	Stages           []StageRecord   `json:"stages"`
	LatestCommands   []CommandRecord `json:"latestCommands"`
	SlowestCommands  []CommandRecord `json:"slowestCommands"`
	InFlightCommands []CommandRecord `json:"inFlightCommands"`
	DroppedCommands  uint64          `json:"droppedCommands"`
	DroppedStages    uint64          `json:"droppedStages"`
	DroppedInFlight  uint64          `json:"droppedInFlight"`
}

// Recorder is safe for concurrent workers. A snapshot includes active commands;
// it cannot survive a process crash unless the caller saves it before the crash.
type Recorder struct {
	mu       sync.Mutex
	key      [32]byte
	start    time.Time
	finished time.Time
	report   Report
	active   map[uint64]CommandRecord
}

func New(metadata Metadata) *Recorder {
	r := &Recorder{start: time.Now(), active: make(map[uint64]CommandRecord)}
	// rand.Read fails by panicking rather than ever using a predictable key.
	_, _ = rand.Read(r.key[:])
	metadata.GitPath = r.pseudonym("path", metadata.GitPath)
	r.report = Report{SchemaVersion: SchemaVersion, ID: uuid.NewString(), Metadata: cleanMetadata(metadata), StartedAt: r.start.UTC(), Outcome: Outcome{Status: "running"}}
	return r
}

func (r *Recorder) pseudonym(kind, value string) string {
	if value == "" {
		return ""
	}
	mac := hmac.New(sha256.New, r.key[:])
	_, _ = mac.Write([]byte(kind + "\x00" + value))
	return kind + "-" + hex.EncodeToString(mac.Sum(nil)[:12])
}

func (r *Recorder) cleanStage(info StageInfo) StageInfo {
	info.Name = choice(info.Name, "discovery", "collection", "repository", "checkout", "resolve", "baseline", "scopes", "patches", "contents", "commits", "activity", "identity", "parse", "fingerprint", "verification", "publication", "submission", "upload", "retry", "startup", "sync", "hook", "cleanup", "git")
	if info.Scope != "" {
		info.Scope = choice(info.Scope, "all", "committed", "uncommitted", "staged", "unstaged", "branch", "commit", "working-tree", "untracked")
	}
	info.Attempt = max(0, info.Attempt)
	info.Repository = r.pseudonym("path", info.Repository)
	info.Ref = r.pseudonym("ref", info.Ref)
	info.URL = r.pseudonym("url", info.URL)
	return info
}

// BeginStage returns an idempotent completion callback. Active stages remain in
// the bounded latest stage list; totals count even stages omitted from that list.
func (r *Recorder) BeginStage(info StageInfo) func(*Failure) {
	r.mu.Lock()
	if !r.finished.IsZero() {
		r.mu.Unlock()
		return func(*Failure) {}
	}
	r.report.Totals.Stages++
	id, started := r.report.Totals.Stages, time.Now()
	info = r.cleanStage(info)
	summary := -1
	for i, total := range r.report.Totals.StageTotals {
		if total.Name == info.Name && total.Scope == info.Scope {
			summary = i
			break
		}
	}
	if summary < 0 {
		summary = len(r.report.Totals.StageTotals)
		r.report.Totals.StageTotals = append(r.report.Totals.StageTotals, StageTotal{Name: info.Name, Scope: info.Scope})
	}
	r.report.Totals.StageTotals[summary].Started++
	if info.Attempt > 0 {
		r.report.Totals.StageTotals[summary].Attempts++
	}
	r.report.Stages = append(r.report.Stages, StageRecord{StageInfo: info, Sequence: id, StartedMS: started.Sub(r.start).Milliseconds(), InFlight: true})
	if len(r.report.Stages) > MaxStages {
		r.report.Stages = r.report.Stages[1:]
		r.report.DroppedStages++
	}
	r.mu.Unlock()
	var once sync.Once
	return func(failure *Failure) {
		once.Do(func() {
			r.mu.Lock()
			defer r.mu.Unlock()
			if !r.finished.IsZero() {
				return
			}
			total := &r.report.Totals.StageTotals[summary]
			total.Completed++
			total.ElapsedMS += time.Since(started).Milliseconds()
			if failure != nil {
				total.Failed++
			}
			for i := range r.report.Stages {
				if r.report.Stages[i].Sequence == id {
					r.report.Stages[i].ElapsedMS = time.Since(started).Milliseconds()
					r.report.Stages[i].InFlight = false
					r.report.Stages[i].Failure = cleanFailure(failure)
				}
			}
		})
	}
}

// BeginCommand starts before queue admission. Complete it even on cancellation.
// At most 128 active commands are retained, independently of the latest list.
func (r *Recorder) BeginCommand(info CommandInfo) func(CommandResult) {
	r.mu.Lock()
	if !r.finished.IsZero() {
		r.mu.Unlock()
		return func(CommandResult) {}
	}
	r.report.Totals.Commands++
	id, started := r.report.Totals.Commands, time.Now()
	info.StageInfo = r.cleanStage(info.StageInfo)
	info.Operation = commandOperation(info.Operation)
	record := CommandRecord{CommandInfo: info, Sequence: id, StartedMS: started.Sub(r.start).Milliseconds(), InFlight: true, DeadlineKind: "none"}
	if len(r.active) < MaxCommands {
		r.active[id] = record
	} else {
		r.report.DroppedInFlight++
	}
	r.mu.Unlock()
	var once sync.Once
	return func(result CommandResult) {
		once.Do(func() {
			r.mu.Lock()
			defer r.mu.Unlock()
			if !r.finished.IsZero() {
				return
			}
			delete(r.active, id)
			record.InFlight = false
			record.ElapsedMS = max(0, result.Elapsed.Milliseconds())
			record.QueueWaitMS = max(0, result.QueueWait.Milliseconds())
			record.StdoutBytes, record.StderrBytes = max(0, result.StdoutBytes), max(0, result.StderrBytes)
			if result.ExitStatus != nil {
				value := *result.ExitStatus
				record.ExitStatus = &value
			}
			if result.DeadlineKind == "" {
				result.DeadlineKind = "none"
			}
			record.DeadlineKind = choice(result.DeadlineKind, "none", "command", "collection", "caller", "cancelled")
			record.Failure = cleanFailure(result.Failure)
			t := &r.report.Totals
			t.CompletedCommands++
			if record.Failure != nil || (record.ExitStatus != nil && *record.ExitStatus != 0) {
				t.FailedCommands++
			}
			t.CommandElapsedMS += record.ElapsedMS
			t.QueueWaitMS += record.QueueWaitMS
			t.StdoutBytes += record.StdoutBytes
			t.StderrBytes += record.StderrBytes
			r.report.LatestCommands = append(r.report.LatestCommands, record)
			// Sequence is start order, including commands completing out of order.
			slices.SortFunc(r.report.LatestCommands, func(a, b CommandRecord) int {
				if a.Sequence < b.Sequence {
					return -1
				}
				if a.Sequence > b.Sequence {
					return 1
				}
				return 0
			})
			if len(r.report.LatestCommands) > MaxCommands {
				r.report.LatestCommands = r.report.LatestCommands[1:]
				r.report.DroppedCommands++
			}
			r.report.SlowestCommands = append(r.report.SlowestCommands, record)
			slices.SortStableFunc(r.report.SlowestCommands, func(a, b CommandRecord) int {
				if a.ElapsedMS > b.ElapsedMS {
					return -1
				}
				if a.ElapsedMS < b.ElapsedMS {
					return 1
				}
				return 0
			})
			if len(r.report.SlowestCommands) > MaxSlowest {
				r.report.SlowestCommands = r.report.SlowestCommands[:MaxSlowest]
			}
		})
	}
}

// AddWorkload adds nonnegative deltas; counters survive event truncation.
func (r *Recorder) AddWorkload(delta Workload) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.finished.IsZero() {
		return
	}
	w := &r.report.Totals.Workload
	w.Repositories += max(0, delta.Repositories)
	w.Checkouts += max(0, delta.Checkouts)
	w.Scopes += max(0, delta.Scopes)
	w.Attempts += max(0, delta.Attempts)
	w.Files += max(0, delta.Files)
	w.Commits += max(0, delta.Commits)
	w.Bytes += max(0, delta.Bytes)
}

func (r *Recorder) Snapshot() Report {
	r.mu.Lock()
	defer r.mu.Unlock()
	report := r.report
	report.ElapsedMS = time.Since(r.start).Milliseconds()
	if !r.finished.IsZero() {
		report.ElapsedMS = r.finished.Sub(r.start).Milliseconds()
	}
	report.Totals.StageTotals = slices.Clone(report.Totals.StageTotals)
	report.Stages = slices.Clone(report.Stages)
	for i := range report.Stages {
		if report.Stages[i].InFlight {
			report.Stages[i].ElapsedMS = report.ElapsedMS - report.Stages[i].StartedMS
		}
	}
	report.LatestCommands = cloneCommands(report.LatestCommands)
	report.SlowestCommands = cloneCommands(report.SlowestCommands)
	report.Outcome = cleanOutcome(report.Outcome)
	for _, command := range r.active {
		command.ElapsedMS = report.ElapsedMS - command.StartedMS
		report.InFlightCommands = append(report.InFlightCommands, command)
	}
	slices.SortFunc(report.InFlightCommands, func(a, b CommandRecord) int {
		if a.Sequence < b.Sequence {
			return -1
		}
		return 1
	})
	for i := range report.Stages {
		report.Stages[i].Failure = cleanFailure(report.Stages[i].Failure)
	}
	return report
}

// Finish sets the overall outcome without replacing its primary error with a
// cancellation or cleanup failure. It also retains unfinished command metadata.
// The first call freezes the recorder; subsequent events and finishes are ignored.
func (r *Recorder) Finish(outcome Outcome) Report {
	r.mu.Lock()
	if r.finished.IsZero() {
		r.report.Outcome = cleanOutcome(outcome)
		r.finished = time.Now()
	}
	r.mu.Unlock()
	return r.Snapshot()
}

func cloneCommands(commands []CommandRecord) []CommandRecord {
	commands = slices.Clone(commands)
	for i := range commands {
		commands[i].Failure = cleanFailure(commands[i].Failure)
		if commands[i].ExitStatus != nil {
			value := *commands[i].ExitStatus
			commands[i].ExitStatus = &value
		}
	}
	return commands
}

func choice(value string, allowed ...string) string {
	if slices.Contains(allowed, value) {
		return value
	}
	return "unknown"
}

func commandOperation(value string) string {
	return choice(value, "rev-parse", "rev-list", "patch-id", "status", "diff", "show", "log", "merge-base", "for-each-ref", "symbolic-ref", "config", "worktree", "ls-files", "ls-tree", "cat-file", "check-attr", "check-ignore", "version", "remote", "fetch", "hash-object", "update-index")
}

var versionPattern = regexp.MustCompile(`^(dev|v?[0-9]+\.[0-9]+(\.[0-9]+)?(-(dev|rc|beta|alpha)[0-9.]*)?(\+[0-9a-f]{7,40})?)$`)
var gitVersionPattern = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(\.windows\.[0-9]+| \(Apple Git-[0-9]+\))?$`)
var commitPattern = regexp.MustCompile(`^[0-9a-f]{7,64}$`)
var pseudoPattern = regexp.MustCompile(`^(path|ref|url)-[0-9a-f]{24}$`)

func version(value string) string {
	if versionPattern.MatchString(value) && len(value) <= 80 {
		return value
	}
	return "unknown"
}
func cleanMetadata(m Metadata) Metadata {
	m.AppVersion = version(m.AppVersion)
	if !commitPattern.MatchString(m.BuildCommit) {
		m.BuildCommit = ""
	}
	if date, err := time.Parse(time.RFC3339, m.BuildDate); err == nil {
		m.BuildDate = date.UTC().Format(time.RFC3339)
	} else {
		m.BuildDate = ""
	}
	m.Platform = choice(m.Platform, "linux", "darwin", "windows", "freebsd", "openbsd", "netbsd", "android", "ios", "illumos", "solaris", "aix", "dragonfly", "plan9", "js", "wasip1")
	m.Architecture = choice(m.Architecture, "amd64", "arm64", "386", "arm", "ppc64", "ppc64le", "riscv64", "s390x", "loong64", "wasm", "mips", "mipsle", "mips64", "mips64le")
	goVersion := m.GoVersion
	if len(goVersion) > 2 && goVersion[:2] == "go" {
		goVersion = goVersion[2:]
	}
	m.GoVersion = version(goVersion)
	gitVersion := m.GitVersion
	if len(gitVersion) > 12 && gitVersion[:12] == "git version " {
		gitVersion = gitVersion[12:]
	}
	m.GitVersion = version(gitVersion)
	if len(gitVersion) <= 80 && gitVersionPattern.MatchString(gitVersion) {
		m.GitVersion = gitVersion
	}
	if !pseudoPattern.MatchString(m.GitPath) {
		m.GitPath = ""
	}
	m.Operation = choice(m.Operation, "startup", "sync", "hook", "collection", "retry")
	return m
}

func cleanFailure(f *Failure) *Failure {
	if f == nil {
		return nil
	}
	result := Failure{Kind: choice(f.Kind, "git", "timeout", "cancelled", "io", "permission", "not-found", "invalid-input", "parse", "limit", "network", "authentication", "publication", "storage", "cleanup", "internal")}
	if f.Detail != "" {
		result.Detail = choice(f.Detail, "command-failed", "deadline-exceeded", "context-cancelled", "not-a-repository", "missing-ref", "missing-object", "unsupported", "invalid-data", "budget-exceeded", "permission-denied", "not-found", "unavailable", "conflict", "retry-exhausted")
	}
	if f.Phase != "" {
		result.Phase = choice(f.Phase, "cancellation", "cleanup", "output")
	}
	if f.Code != "" {
		result.Code = choice(f.Code, "EPERM", "EACCES", "ESRCH", "ENOENT", "EIO")
	}
	return &result
}

func cleanOutcome(o Outcome) Outcome {
	o.Status = choice(o.Status, "running", "success", "failure", "cancelled")
	o.Primary = cleanFailure(o.Primary)
	cleanup := o.Cleanup
	o.Cleanup = nil
	for _, failure := range cleanup[:min(len(cleanup), 20)] {
		o.Cleanup = append(o.Cleanup, *cleanFailure(&failure))
	}
	return o
}
