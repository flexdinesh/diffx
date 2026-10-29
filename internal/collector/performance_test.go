package collector

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// collectionWorkload creates 100 commits changing four of 400 files apiece.
// Fast import keeps fixture construction out of the measured Git workload.
func collectionWorkload(tb testing.TB, files int) string {
	tb.Helper()
	root := tb.TempDir()
	run := func(input string, args ...string) {
		tb.Helper()
		command := exec.Command("git", args...)
		command.Dir = root
		command.Stdin = strings.NewReader(input)
		if output, err := command.CombinedOutput(); err != nil {
			tb.Fatalf("git %v: %v: %s", args, err, output)
		}
	}
	run("", "init", "-b", "main")
	var input strings.Builder
	input.WriteString("commit refs/heads/main\ncommitter Fixture <fixture@example.com> 1700000000 +0000\ndata 4\nbase\n")
	for i := range files {
		fmt.Fprintf(&input, "M 100644 inline file-%04d\ndata 5\nbase\n", i)
	}
	input.WriteString("\n")
	for commit := range 100 {
		fmt.Fprintf(&input, "commit refs/heads/feature\ncommitter Fixture <fixture@example.com> %d +0000\ndata 6\nchange\n", 1700000001+commit)
		if commit == 0 {
			input.WriteString("from refs/heads/main\n")
		}
		for offset := range 4 {
			content := fmt.Sprintf("change %d %d\n", commit, offset)
			fmt.Fprintf(&input, "M 100644 inline file-%04d\ndata %d\n%s", (commit*4+offset)%files, len(content), content)
		}
		input.WriteString("\n")
	}
	run(input.String(), "fast-import", "--quiet")
	run("", "switch", "feature")
	return root
}

func BenchmarkCollectBranchWorkload(b *testing.B) {
	root := collectionWorkload(b, 400)
	trace := filepath.Join(b.TempDir(), "trace.json")
	b.Setenv("GIT_TRACE2_EVENT", trace)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, err := Collect(context.Background(), root, Options{SourceID: "benchmark", Hostname: "fixture"}); err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
	raw, err := os.ReadFile(trace)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportMetric(float64(strings.Count(string(raw), `"event":"start"`))/float64(b.N), "git/op")
}

// This boundary budget distinguishes batched reads from the original thousands
// of per-file processes without depending on the exact fixed metadata count.
func TestCollectionWorkloadProcessGrowth(t *testing.T) {
	var counts []int
	for _, files := range []int{40, 400} {
		root := collectionWorkload(t, files)
		trace := filepath.Join(t.TempDir(), "trace.json")
		t.Setenv("GIT_TRACE2_EVENT", trace)
		request, err := Collect(t.Context(), root, Options{SourceID: "regression", Hostname: "fixture"})
		if err != nil {
			t.Fatal(err)
		}
		if len(request.Commits) != 100 || request.CommitsTotal != 100 {
			t.Fatal("workload lost commits")
		}
		raw, err := os.ReadFile(trace)
		if err != nil {
			t.Fatal(err)
		}
		count := strings.Count(string(raw), `"event":"start"`)
		counts = append(counts, count)
		t.Logf("files=%d commits=100 Git processes=%d", files, count)
		if count > 500 {
			t.Fatalf("collection spawned %d processes for %d files; reads must grow in batches", count, files)
		}
	}
	if counts[1]-counts[0] > 200 {
		t.Fatalf("360 extra comparison files added %d processes: %v", counts[1]-counts[0], counts)
	}
}
