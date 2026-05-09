package replay

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
}

// TestSortedBucketKeys_TieBreakerIsLexicographic pins the tie-breaker
// promised by the doc comment. Without it, two runs over the same
// data produce different orderings via map iteration randomness,
// which breaks operator dashboards + test fixtures.
func TestExtractAxis(t *testing.T) {
	ev := map[string]interface{}{"agent_instance_id": "agent-42"}
	payload := map[string]interface{}{
		"tool_name":     "Bash",
		"action_type":   "shell.exec",
		"rule_id":       "default-allow-shell",
		"decision":      "allow",
	}

	cases := []struct {
		axis string
		want string
	}{
		{"tool_name", "Bash"},
		{"action_type", "shell.exec"},
		{"rule_id", "default-allow-shell"},
		{"decision", "allow"},
		{"agent", "agent-42"},
		{"unknown_axis", ""},
	}
	for _, c := range cases {
		got := extractAxis(ev, payload, c.axis)
		if got != c.want {
			t.Errorf("extractAxis(_, _, %q) = %q, want %q", c.axis, got, c.want)
		}
	}
}

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
