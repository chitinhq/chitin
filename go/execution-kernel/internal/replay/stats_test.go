package replay

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeChain materializes a synthetic events-*.jsonl in dir with
// the given decision events. Each event is a one-line JSON object
// shaped like the kernel's chain emitter.
func writeChain(t *testing.T, dir, sessionID string, events string) {
	t.Helper()
	path := filepath.Join(dir, "events-"+sessionID+".jsonl")
	if err := os.WriteFile(path, []byte(events), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestComputeStatsIn_BucketCountsAndSuccessRate(t *testing.T) {
	tmp := t.TempDir()
	writeChain(t, tmp, "s1",
		`{"event_type":"decision","payload":{"tool_name":"Bash","action_type":"shell.exec","decision":"allow","rule_id":"default-allow-shell"}}`+"\n"+
			`{"event_type":"decision","payload":{"tool_name":"Bash","action_type":"shell.exec","decision":"allow","rule_id":"default-allow-shell"}}`+"\n"+
			`{"event_type":"decision","payload":{"tool_name":"Bash","action_type":"shell.exec","decision":"deny","rule_id":"no-rm-recursive"}}`+"\n"+
			`{"event_type":"decision","payload":{"tool_name":"Read","action_type":"file.read","decision":"allow","rule_id":"default-allow-reads"}}`+"\n",
	)

	s, err := ComputeStatsIn("tool_name", tmp)
	if err != nil {
		t.Fatalf("ComputeStatsIn: %v", err)
	}
	if s.Total != 4 {
		t.Errorf("Total=%d want 4", s.Total)
	}
	bash := s.Buckets["Bash"]
	if bash.Decisions != 3 || bash.Allows != 2 || bash.Denies != 1 {
		t.Errorf("Bash bucket=%+v want decisions=3 allows=2 denies=1", bash)
	}
	wantRate := 2.0 / 3.0
	if bash.SuccessRate < wantRate-1e-9 || bash.SuccessRate > wantRate+1e-9 {
		t.Errorf("Bash.SuccessRate=%v want ≈%v", bash.SuccessRate, wantRate)
	}
	read := s.Buckets["Read"]
	if read.Decisions != 1 || read.Allows != 1 {
		t.Errorf("Read bucket=%+v", read)
	}
	if s.Floundering == nil {
		t.Fatal("expected floundering calibration metrics")
	}
}

func TestComputeStatsIn_UnknownAxisErrors(t *testing.T) {
	tmp := t.TempDir()
	_, err := ComputeStatsIn("not_a_real_axis", tmp)
	if err == nil {
		t.Fatal("expected error for unsupported axis; got nil")
	}
	if !strings.Contains(err.Error(), "unsupported axis") {
		t.Errorf("error text=%q want to mention unsupported axis", err.Error())
	}
}

func TestComputeStatsInWindow_FiltersOldEvents(t *testing.T) {
	tmp := t.TempDir()
	now := time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC)
	writeChain(t, tmp, "s1",
		`{"ts":"`+now.Add(-30*time.Minute).Format(time.RFC3339)+`","event_type":"decision","payload":{"tool_name":"Bash","action_type":"shell.exec","decision":"allow","rule_id":"allow"}}`+"\n"+
			`{"ts":"`+now.Add(-3*time.Hour).Format(time.RFC3339)+`","event_type":"decision","payload":{"tool_name":"Read","action_type":"file.read","decision":"allow","rule_id":"allow"}}`+"\n",
	)

	s, err := ComputeStatsInWindow("tool_name", tmp, 1, now)
	if err != nil {
		t.Fatalf("ComputeStatsInWindow: %v", err)
	}
	if s.Total != 1 {
		t.Fatalf("Total=%d want 1", s.Total)
	}
	if _, ok := s.Buckets["Bash"]; !ok {
		t.Fatalf("expected Bash bucket in window, got %+v", s.Buckets)
	}
	if _, ok := s.Buckets["Read"]; ok {
		t.Fatalf("old Read bucket should be excluded, got %+v", s.Buckets)
	}
	if s.Window != "1h" {
		t.Fatalf("Window=%q want 1h", s.Window)
	}
}

func TestComputeStatsIn_SkipsNonDecisionEvents(t *testing.T) {
	tmp := t.TempDir()
	writeChain(t, tmp, "s1",
		`{"event_type":"audit","payload":{"tool_name":"Bash"}}`+"\n"+
			`{"event_type":"decision","payload":{"tool_name":"Bash","decision":"allow"}}`+"\n",
	)
	s, err := ComputeStatsIn("tool_name", tmp)
	if err != nil {
		t.Fatal(err)
	}
	if s.Total != 1 {
		t.Errorf("Total=%d want 1 (audit event must be skipped)", s.Total)
	}
}

func TestComputeStatsIn_EmptyDirYieldsEmptyStats(t *testing.T) {
	tmp := t.TempDir()
	s, err := ComputeStatsIn("tool_name", tmp)
	if err != nil {
		t.Fatal(err)
	}
	if s.Total != 0 || len(s.Buckets) != 0 {
		t.Errorf("expected empty stats, got %+v", s)
	}
	if s.Floundering == nil || s.Floundering.Sessions != 0 {
		t.Errorf("expected empty floundering stats, got %+v", s.Floundering)
	}
}

func TestComputeStatsIn_FlounderingFalsePositiveRates(t *testing.T) {
	tmp := t.TempDir()
	writeChain(t, tmp, "legit-edit",
		`{"event_type":"decision","payload":{"tool_name":"file.write","action_type":"file.write","action_target":"/repo/a.go","decision":"allow"}}`+"\n"+
			`{"event_type":"decision","payload":{"tool_name":"file.write","action_type":"file.write","action_target":"/repo/a.go","decision":"allow"}}`+"\n"+
			`{"event_type":"decision","payload":{"tool_name":"git.commit","action_type":"git.commit","action_target":"git commit","decision":"allow"}}`+"\n",
	)
	writeChain(t, tmp, "terminal-file-read",
		`{"event_type":"decision","payload":{"tool_name":"file.read","action_type":"file.read","action_target":"/repo/a.go","decision":"allow"}}`+"\n"+
			`{"event_type":"decision","payload":{"tool_name":"file.read","action_type":"file.read","action_target":"/repo/a.go","decision":"allow"}}`+"\n",
	)
	writeChain(t, tmp, "terminal-loop",
		`{"event_type":"decision","payload":{"tool_name":"shell.exec","action_type":"shell.exec","action_target":"ollama ps || true","decision":"allow"}}`+"\n"+
			`{"event_type":"decision","payload":{"tool_name":"shell.exec","action_type":"shell.exec","action_target":"ollama ps || true","decision":"allow"}}`+"\n",
	)

	s, err := ComputeStatsIn("tool_name", tmp)
	if err != nil {
		t.Fatal(err)
	}
	f := s.Floundering
	if f == nil {
		t.Fatal("expected floundering stats")
	}
	if f.FixedFalsePositiveRate <= f.AdaptiveFalsePositiveRate {
		t.Fatalf("adaptive false-positive rate did not improve: fixed=%v adaptive=%v", f.FixedFalsePositiveRate, f.AdaptiveFalsePositiveRate)
	}
	if f.AdaptiveFalseNegativeRate != f.FixedFalseNegativeRate {
		t.Fatalf("adaptive false-negative rate changed: fixed=%v adaptive=%v", f.FixedFalseNegativeRate, f.AdaptiveFalseNegativeRate)
	}
	if f.FalsePositiveReduction < 0.2 {
		t.Fatalf("false-positive reduction=%v want >= 0.2", f.FalsePositiveReduction)
	}
}

// TestSortedBucketKeys_TieBreakerIsLexicographic pins the tie-breaker
// promised by the doc comment. Without it, two runs over the same
// data produce different orderings via map iteration randomness,
// which breaks operator dashboards + test fixtures.
func TestSortedBucketKeys_TieBreakerIsLexicographic(t *testing.T) {
	s := &Stats{
		Buckets: map[string]BucketStats{
			"zebra": {Decisions: 5},
			"apple": {Decisions: 5},
			"mango": {Decisions: 5},
			"baby":  {Decisions: 10},
		},
	}
	// Run 100 times to defeat randomness — lexicographic tie-breaker
	// must yield the same order every time.
	want := []string{"baby", "apple", "mango", "zebra"}
	for i := 0; i < 100; i++ {
		got := s.SortedBucketKeys()
		if len(got) != len(want) {
			t.Fatalf("len=%d want %d", len(got), len(want))
		}
		for j := range want {
			if got[j] != want[j] {
				t.Errorf("iter %d: got[%d]=%q want %q", i, j, got[j], want[j])
				return
			}
		}
	}
}

func TestIsSupportedAxis(t *testing.T) {
	for _, a := range SupportedAxes {
		if !IsSupportedAxis(a) {
			t.Errorf("expected %q to be supported", a)
		}
	}
	if IsSupportedAxis("garbage") {
		t.Error("expected 'garbage' to be unsupported")
	}
}
