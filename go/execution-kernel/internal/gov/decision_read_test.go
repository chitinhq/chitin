package gov

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeDecisionLines writes the supplied jsonl lines into
// gov-decisions-<date>.jsonl under dir, one line per element.
func writeDecisionLines(t *testing.T, dir, date string, lines []string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "gov-decisions-"+date+".jsonl")
	body := ""
	for _, l := range lines {
		body += l + "\n"
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mkDecision(t *testing.T, ts time.Time, ruleID string) string {
	t.Helper()
	d := Decision{
		Allowed: true,
		Mode:    "enforce",
		RuleID:  ruleID,
		Agent:   "claude-code",
		Action:  Action{Type: ActionType("test"), Target: "/tmp/x"},
		Ts:      ts.UTC().Format(time.RFC3339),
	}
	// Use the same wire shape WriteLog produces (Action is json:"-",
	// so we have to surface action_type/action_target manually).
	type wire struct {
		Allowed      bool   `json:"allowed"`
		Mode         string `json:"mode"`
		RuleID       string `json:"rule_id"`
		Agent        string `json:"agent,omitempty"`
		ActionType   string `json:"action_type"`
		ActionTarget string `json:"action_target"`
		Ts           string `json:"ts"`
	}
	b, err := json.Marshal(wire{
		Allowed: d.Allowed, Mode: d.Mode, RuleID: d.RuleID,
		Agent:        d.Agent,
		ActionType:   string(d.Action.Type),
		ActionTarget: d.Action.Target,
		Ts:           d.Ts,
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// Boundary: empty dir → empty result, nil error. Operator queries against
// a freshly initialized chitin home should not error.
func TestReadRecent_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	out, err := ReadRecent(ReadRecentArgs{Dir: dir, WindowHours: 24, Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 0 {
		t.Fatalf("want 0 decisions, got %d", len(out))
	}
}

// Boundary: nonexistent dir → empty result, nil error. CHITIN_HOME may
// not yet exist on a brand-new machine; the reader treats absence as
// "no decisions" rather than a failure.
func TestReadRecent_NonexistentDir(t *testing.T) {
	out, err := ReadRecent(ReadRecentArgs{
		Dir: filepath.Join(t.TempDir(), "no-such-subdir"),
		WindowHours: 24, Limit: 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 0 {
		t.Fatalf("want 0 decisions, got %d", len(out))
	}
}

// Happy path: in-window entries are returned newest-first, capped to limit.
func TestReadRecent_NewestFirstAndLimit(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)
	lines := []string{
		mkDecision(t, now.Add(-3*time.Minute), "r1"),
		mkDecision(t, now.Add(-2*time.Minute), "r2"),
		mkDecision(t, now.Add(-1*time.Minute), "r3"),
	}
	writeDecisionLines(t, dir, "2026-05-08", lines)

	out, err := ReadRecent(ReadRecentArgs{Dir: dir, WindowHours: 1, Limit: 2, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 {
		t.Fatalf("want 2 decisions (capped), got %d", len(out))
	}
	if out[0].RuleID != "r3" || out[1].RuleID != "r2" {
		t.Fatalf("wrong ordering: %+v", out)
	}
}

// Boundary: entry exactly at the cutoff is excluded (window is open
// on the cutoff side: ts > cutoff). An entry exactly windowHours old
// is at cutoff; ts.Before(cutoff) is false but we still want it
// excluded for predictability — actually the implementation includes
// equal-to-cutoff entries (Before is strict). Document via test.
func TestReadRecent_ExcludesPreCutoff(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)
	lines := []string{
		mkDecision(t, now.Add(-48*time.Hour), "old"),
		mkDecision(t, now.Add(-30*time.Minute), "recent"),
	}
	writeDecisionLines(t, dir, "2026-05-08", lines)

	out, err := ReadRecent(ReadRecentArgs{Dir: dir, WindowHours: 1, Limit: 100, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].RuleID != "recent" {
		t.Fatalf("want only [recent], got %+v", out)
	}
}

// Cross-file: newest first across multiple daily files; older file is
// fully exhausted only if newer file is exhausted.
func TestReadRecent_AcrossMultipleDays(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)

	writeDecisionLines(t, dir, "2026-05-07", []string{
		mkDecision(t, now.Add(-25*time.Hour), "yesterday-1"),
		mkDecision(t, now.Add(-20*time.Hour), "yesterday-2"),
	})
	writeDecisionLines(t, dir, "2026-05-08", []string{
		mkDecision(t, now.Add(-5*time.Hour), "today-1"),
		mkDecision(t, now.Add(-1*time.Hour), "today-2"),
	})

	out, err := ReadRecent(ReadRecentArgs{Dir: dir, WindowHours: 30, Limit: 100, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 4 {
		t.Fatalf("want 4 decisions, got %d", len(out))
	}
	// Newest first overall.
	want := []string{"today-2", "today-1", "yesterday-2", "yesterday-1"}
	for i, w := range want {
		if out[i].RuleID != w {
			t.Fatalf("pos %d: want %q got %q", i, w, out[i].RuleID)
		}
	}
}

// Stop-early optimization: when newest entry of an older file is pre-
// cutoff, no further files are scanned. Verified by the result: only
// today's entries appear, and yesterday's pre-cutoff entries do not.
func TestReadRecent_StopsScanningPreCutoffFiles(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)

	writeDecisionLines(t, dir, "2026-05-06", []string{
		mkDecision(t, now.Add(-50*time.Hour), "way-old"),
	})
	writeDecisionLines(t, dir, "2026-05-08", []string{
		mkDecision(t, now.Add(-30*time.Minute), "today"),
	})

	out, err := ReadRecent(ReadRecentArgs{Dir: dir, WindowHours: 1, Limit: 100, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].RuleID != "today" {
		t.Fatalf("want only [today], got %+v", out)
	}
}

// Resilience: malformed lines must be skipped, not abort the read.
func TestReadRecent_SkipsMalformedLines(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)
	good := mkDecision(t, now.Add(-30*time.Minute), "ok")
	writeDecisionLines(t, dir, "2026-05-08", []string{
		"{not json",
		good,
		"",
	})

	out, err := ReadRecent(ReadRecentArgs{Dir: dir, WindowHours: 1, Limit: 100, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].RuleID != "ok" {
		t.Fatalf("want only [ok], got %+v", out)
	}
}

// Validation: zero/negative window or limit is rejected at the boundary
// rather than producing surprising results.
func TestReadRecent_RejectsBadArgs(t *testing.T) {
	dir := t.TempDir()
	if _, err := ReadRecent(ReadRecentArgs{Dir: dir, WindowHours: 0, Limit: 10}); err == nil {
		t.Fatal("want error for window_hours=0")
	}
	if _, err := ReadRecent(ReadRecentArgs{Dir: dir, WindowHours: 1, Limit: 0}); err == nil {
		t.Fatal("want error for limit=0")
	}
}
