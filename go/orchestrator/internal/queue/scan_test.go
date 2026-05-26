package queue

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeJSONL serialises one record per element into a JSONL file at path.
// Non-map elements are written verbatim (used to inject malformed lines).
func writeJSONL(t *testing.T, path string, recs []any) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create %s: %v", path, err)
	}
	defer f.Close()
	for _, r := range recs {
		switch v := r.(type) {
		case string:
			if _, err := f.WriteString(v + "\n"); err != nil {
				t.Fatalf("write raw: %v", err)
			}
		default:
			b, err := json.Marshal(v)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			b = append(b, '\n')
			if _, err := f.Write(b); err != nil {
				t.Fatalf("write json: %v", err)
			}
		}
	}
}

// piEscalated builds a pr_iteration_escalated envelope shaped the way
// emit-time canonical-JSON would.
func piEscalated(pr int, reason, ts, runID string) map[string]any {
	return map[string]any{
		"event_type": "pr_iteration_escalated",
		"run_id":     runID,
		"ts":         ts,
		"payload": map[string]any{
			"pr_number":        pr,
			"reason":           reason,
			"rounds_attempted": 3,
			"last_review_id":   "RV_42",
		},
	}
}

func siblingRebaseFailed(pr int, ts, runID string) map[string]any {
	return map[string]any{
		"event_type": "sibling_rebase_failed",
		"run_id":     runID,
		"ts":         ts,
		"payload": map[string]any{
			"pr_number":      pr,
			"conflict_files": []string{"go.mod"},
		},
	}
}

func silentDrop(pr int, specRef, taskID, ts, runID string) map[string]any {
	return map[string]any{
		"event_type": "work_unit_completed_without_deliverable",
		"run_id":     runID,
		"ts":         ts,
		"payload": map[string]any{
			"pr_number":        pr,
			"spec_ref":         specRef,
			"task_id":          taskID,
			"work_unit_id":     "wu-" + taskID,
			"deliverable_kind": "pr",
			"reason":           "gh_pr_create_failed",
		},
	}
}

func TestScan_EmptyDir_ReturnsEmptyMap(t *testing.T) {
	dir := t.TempDir()
	got, err := Scan(dir, time.Time{})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("want empty map, got %v", got)
	}
}

func TestScan_NonexistentDir_ReturnsEmptyMap(t *testing.T) {
	got, err := Scan(filepath.Join(t.TempDir(), "does-not-exist"), time.Time{})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("want empty map, got %v", got)
	}
}

func TestScan_IndexesEachReasonKind(t *testing.T) {
	dir := t.TempDir()
	ts := "2026-05-25T10:00:00Z"

	recs := []any{
		piEscalated(101, "iteration_cap_hit", ts, "run-a"),
		piEscalated(102, "human_reviewer_present", ts, "run-b"),
		piEscalated(103, "lease_lost", ts, "run-c"),
		piEscalated(104, "iteration_completed_with_skips", ts, "run-d"),
		siblingRebaseFailed(105, ts, "run-e"),
		silentDrop(106, "118-test", "T009", ts, "run-f"),
	}
	writeJSONL(t, filepath.Join(dir, "events-run-mix.jsonl"), recs)

	got, err := Scan(dir, time.Time{})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	wantReason := map[int]string{
		101: "iteration_cap_hit",
		102: "human_reviewer_present",
		103: "lease_lost",
		104: "iteration_completed_with_skips",
		105: "sibling_rebase_failed",
		106: "silent_drop",
	}
	for pr, reason := range wantReason {
		evs, ok := got[pr]
		if !ok || len(evs) != 1 {
			t.Errorf("PR %d: want 1 event, got %d", pr, len(evs))
			continue
		}
		if evs[0].Reason != reason {
			t.Errorf("PR %d: want reason %q, got %q", pr, reason, evs[0].Reason)
		}
		if evs[0].PRNumber != pr {
			t.Errorf("PR %d: want PRNumber %d, got %d", pr, pr, evs[0].PRNumber)
		}
		if len(evs[0].Payload) == 0 {
			t.Errorf("PR %d: payload not preserved", pr)
		}
	}
}

func TestScan_IndexesSilentDropWithoutPR(t *testing.T) {
	dir := t.TempDir()
	ts := "2026-05-25T10:00:00Z"
	writeJSONL(t, filepath.Join(dir, "events-silent-drop.jsonl"), []any{
		silentDrop(0, "118-factory-dispatch-failed-reason-taxonomy", "T008", ts, "wu-118-T008"),
	})
	got, err := Scan(dir, time.Time{})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want one silent-drop bucket, got %+v", got)
	}
	for _, evs := range got {
		if len(evs) != 1 {
			t.Fatalf("want one event, got %d", len(evs))
		}
		ev := evs[0]
		if ev.Reason != "silent_drop" || ev.PRNumber != 0 || ev.SpecRef != "118-factory-dispatch-failed-reason-taxonomy" || ev.TaskID != "T008" {
			t.Fatalf("unexpected event: %+v", ev)
		}
	}
}

func TestScan_SkipsNonEscalationEventTypes(t *testing.T) {
	dir := t.TempDir()
	ts := "2026-05-25T10:00:00Z"
	recs := []any{
		map[string]any{
			"event_type": "pr_iteration_completed",
			"run_id":     "run-x",
			"ts":         ts,
			"payload": map[string]any{
				"pr_number": 200,
				"round":     1,
			},
		},
		map[string]any{
			"event_type": "pr_iteration_round_started",
			"run_id":     "run-y",
			"ts":         ts,
			"payload":    map[string]any{"pr_number": 201},
		},
		piEscalated(202, "iteration_cap_hit", ts, "run-z"),
	}
	writeJSONL(t, filepath.Join(dir, "events-mixed.jsonl"), recs)

	got, _ := Scan(dir, time.Time{})
	if _, hit := got[200]; hit {
		t.Errorf("pr_iteration_completed must not appear in queue index")
	}
	if _, hit := got[201]; hit {
		t.Errorf("pr_iteration_round_started must not appear in queue index")
	}
	if len(got[202]) != 1 {
		t.Errorf("want PR 202 escalation, got %d events", len(got[202]))
	}
}

func TestScan_SkipsUnknownReason(t *testing.T) {
	dir := t.TempDir()
	ts := "2026-05-25T10:00:00Z"
	recs := []any{
		// Reason outside FR-008 closed set — must be dropped per
		// spec 113 FR-010 "MUST NOT invent additional event types".
		piEscalated(300, "operator_panicked", ts, "run-q"),
		piEscalated(301, "iteration_cap_hit", ts, "run-q"),
	}
	writeJSONL(t, filepath.Join(dir, "events-unknown.jsonl"), recs)

	got, _ := Scan(dir, time.Time{})
	if _, hit := got[300]; hit {
		t.Errorf("unknown reason must not be indexed")
	}
	if len(got[301]) != 1 {
		t.Errorf("known reason must still be indexed; got %d", len(got[301]))
	}
}

func TestScan_SkipsMalformedLines(t *testing.T) {
	dir := t.TempDir()
	ts := "2026-05-25T10:00:00Z"
	// Mix of: a truncated JSON, a non-JSON garble that contains a
	// matching substring (pre-filter triggers but parse fails),
	// and a valid escalation. The valid one must still be indexed.
	recs := []any{
		`{"event_type": "pr_iteration_escalated", "ts": "2026-05-25T10:00:00Z", "payload": {`,
		`pr_iteration_escalated this is not even JSON`,
		piEscalated(400, "iteration_cap_hit", ts, "run-m"),
	}
	writeJSONL(t, filepath.Join(dir, "events-malformed.jsonl"), recs)

	got, err := Scan(dir, time.Time{})
	if err != nil {
		t.Fatalf("Scan: %v — must tolerate malformed lines", err)
	}
	if len(got[400]) != 1 {
		t.Errorf("valid line after malformed should still index; got %d", len(got[400]))
	}
}

func TestScan_SkipsOrphanPRRef(t *testing.T) {
	dir := t.TempDir()
	ts := "2026-05-25T10:00:00Z"
	recs := []any{
		// pr_number missing
		map[string]any{
			"event_type": "pr_iteration_escalated",
			"run_id":     "run-o",
			"ts":         ts,
			"payload":    map[string]any{"reason": "iteration_cap_hit"},
		},
		// pr_number zero
		map[string]any{
			"event_type": "pr_iteration_escalated",
			"run_id":     "run-o",
			"ts":         ts,
			"payload":    map[string]any{"pr_number": 0, "reason": "iteration_cap_hit"},
		},
		// pr_number negative
		map[string]any{
			"event_type": "pr_iteration_escalated",
			"run_id":     "run-o",
			"ts":         ts,
			"payload":    map[string]any{"pr_number": -1, "reason": "iteration_cap_hit"},
		},
		piEscalated(500, "iteration_cap_hit", ts, "run-o"),
	}
	writeJSONL(t, filepath.Join(dir, "events-orphan.jsonl"), recs)

	got, _ := Scan(dir, time.Time{})
	if len(got) != 1 {
		t.Fatalf("want exactly one PR indexed, got %d entries: %+v", len(got), got)
	}
	if len(got[500]) != 1 {
		t.Errorf("expected PR 500 only")
	}
}

func TestScan_AppliesSinceFilter(t *testing.T) {
	dir := t.TempDir()
	old := "2026-05-20T10:00:00Z"
	recent := "2026-05-25T10:00:00Z"
	recs := []any{
		piEscalated(600, "iteration_cap_hit", old, "run-old"),
		piEscalated(601, "iteration_cap_hit", recent, "run-new"),
	}
	writeJSONL(t, filepath.Join(dir, "events-since.jsonl"), recs)

	cutoff, _ := time.Parse(time.RFC3339, "2026-05-24T00:00:00Z")
	got, _ := Scan(dir, cutoff)
	if _, hit := got[600]; hit {
		t.Errorf("event older than since must be dropped")
	}
	if len(got[601]) != 1 {
		t.Errorf("recent event must be retained")
	}
}

func TestScan_MultipleEventsPerPR_PreservesOrder(t *testing.T) {
	dir := t.TempDir()
	ts1 := "2026-05-25T10:00:00Z"
	ts2 := "2026-05-25T11:00:00Z"
	ts3 := "2026-05-25T12:00:00Z"
	recs := []any{
		piEscalated(700, "iteration_cap_hit", ts1, "run-1"),
		piEscalated(700, "human_reviewer_present", ts2, "run-2"),
		piEscalated(700, "lease_lost", ts3, "run-3"),
	}
	writeJSONL(t, filepath.Join(dir, "events-multi.jsonl"), recs)

	got, _ := Scan(dir, time.Time{})
	evs := got[700]
	if len(evs) != 3 {
		t.Fatalf("want 3 events on PR 700, got %d", len(evs))
	}
	want := []string{"iteration_cap_hit", "human_reviewer_present", "lease_lost"}
	for i, ev := range evs {
		if ev.Reason != want[i] {
			t.Errorf("event %d: want reason %q, got %q", i, want[i], ev.Reason)
		}
	}
}

func TestScan_RFC3339NanoTsAccepted(t *testing.T) {
	dir := t.TempDir()
	recs := []any{
		piEscalated(800, "iteration_cap_hit", "2026-05-25T10:00:00.123456789Z", "run-nano"),
	}
	writeJSONL(t, filepath.Join(dir, "events-nano.jsonl"), recs)

	got, _ := Scan(dir, time.Time{})
	if len(got[800]) != 1 {
		t.Fatalf("RFC3339Nano ts not parsed")
	}
}

func TestScan_UnparseableTsSkipped(t *testing.T) {
	dir := t.TempDir()
	recs := []any{
		map[string]any{
			"event_type": "pr_iteration_escalated",
			"run_id":     "run-bad-ts",
			"ts":         "not-a-timestamp",
			"payload":    map[string]any{"pr_number": 900, "reason": "iteration_cap_hit"},
		},
		piEscalated(901, "iteration_cap_hit", "2026-05-25T10:00:00Z", "run-good-ts"),
	}
	writeJSONL(t, filepath.Join(dir, "events-bad-ts.jsonl"), recs)

	got, _ := Scan(dir, time.Time{})
	if _, hit := got[900]; hit {
		t.Errorf("unparseable ts must skip the row")
	}
	if len(got[901]) != 1 {
		t.Errorf("good-ts row must still be indexed")
	}
}

func TestScan_WalksMultipleFiles(t *testing.T) {
	dir := t.TempDir()
	ts := "2026-05-25T10:00:00Z"
	writeJSONL(t, filepath.Join(dir, "events-run-1.jsonl"),
		[]any{piEscalated(1001, "iteration_cap_hit", ts, "run-1")})
	writeJSONL(t, filepath.Join(dir, "events-run-2.jsonl"),
		[]any{piEscalated(1002, "lease_lost", ts, "run-2")})
	// A non-events file in the same dir must be ignored.
	if err := os.WriteFile(filepath.Join(dir, "chain-index.db"), []byte("binary blob"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, _ := Scan(dir, time.Time{})
	if len(got) != 2 {
		t.Fatalf("want 2 PRs, got %d", len(got))
	}
	if got[1001][0].Reason != "iteration_cap_hit" || got[1002][0].Reason != "lease_lost" {
		t.Errorf("wrong reasons across files: %+v", got)
	}
}

func TestResolveChainDir_EnvOverride(t *testing.T) {
	t.Setenv("CHITIN_DIR", "/tmp/custom-chitin-dir")
	if got := ResolveChainDir(); got != "/tmp/custom-chitin-dir" {
		t.Errorf("ResolveChainDir() = %q, want override", got)
	}
}

func TestResolveChainDir_HomeFallback(t *testing.T) {
	t.Setenv("CHITIN_DIR", "")
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir on this host")
	}
	want := filepath.Join(home, ".chitin")
	if got := ResolveChainDir(); got != want {
		t.Errorf("ResolveChainDir() = %q, want %q", got, want)
	}
}

func TestClassifyReason_ClosedTaxonomy(t *testing.T) {
	cases := []struct {
		eventType, payloadReason, wantReason string
		wantOK                               bool
	}{
		{"sibling_rebase_failed", "", "sibling_rebase_failed", true},
		// Spec 112 US2 event carries no payload.reason — the event type IS
		// the reason. A stray reason on the payload must NOT change the
		// classified reason.
		{"sibling_rebase_failed", "ignored", "sibling_rebase_failed", true},
		{"work_unit_completed_without_deliverable", "ignored", "silent_drop", true},
		{"pr_iteration_escalated", "iteration_cap_hit", "iteration_cap_hit", true},
		{"pr_iteration_escalated", "human_reviewer_present", "human_reviewer_present", true},
		{"pr_iteration_escalated", "lease_lost", "lease_lost", true},
		{"pr_iteration_escalated", "iteration_completed_with_skips", "iteration_completed_with_skips", true},
		{"pr_iteration_escalated", "operator_panicked", "", false},
		{"pr_iteration_escalated", "", "", false},
		{"pr_iteration_completed", "iteration_cap_hit", "", false},
	}
	for _, c := range cases {
		gotReason, gotOK := classifyReason(c.eventType, c.payloadReason)
		if gotOK != c.wantOK || gotReason != c.wantReason {
			t.Errorf("classifyReason(%q,%q) = (%q,%v), want (%q,%v)",
				c.eventType, c.payloadReason, gotReason, gotOK, c.wantReason, c.wantOK)
		}
	}
}
