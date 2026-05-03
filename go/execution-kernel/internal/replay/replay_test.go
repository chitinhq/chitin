package replay

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRun_NoEvents(t *testing.T) {
	_, err := Run(context.Background(), "nonexistent-session", "/tmp")
	if err == nil {
		t.Error("expected error for missing session; got nil")
	}
}

func TestFindMostRecentSession_Empty(t *testing.T) {
	tmp := t.TempDir()
	chitinDir := filepath.Join(tmp, ".chitin")
	if err := os.MkdirAll(chitinDir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", tmp)
	_, err := FindMostRecentSession()
	if err == nil {
		t.Error("expected error when no chain files; got nil")
	}
}

func TestFindMostRecentSession_PickNewest(t *testing.T) {
	tmp := t.TempDir()
	chitinDir := filepath.Join(tmp, ".chitin")
	if err := os.MkdirAll(chitinDir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", tmp)
	older := filepath.Join(chitinDir, "events-aaa.jsonl")
	newer := filepath.Join(chitinDir, "events-bbb.jsonl")
	if err := os.WriteFile(older, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newer, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	pastTime := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := os.Chtimes(older, pastTime, pastTime); err != nil {
		t.Fatal(err)
	}
	got, err := FindMostRecentSession()
	if err != nil {
		t.Fatal(err)
	}
	if got != "bbb" {
		t.Errorf("FindMostRecentSession=%q want bbb", got)
	}
}

func TestWriteHumanReport_NoDiffs(t *testing.T) {
	r := &Result{
		SessionID:   "test-session",
		TotalEvents: 5,
		Decisions:   3,
		Summary:     Summary{UnchangedDecisions: 3},
	}
	var buf strings.Builder
	WriteHumanReport(&buf, r)
	out := buf.String()
	if !strings.Contains(out, "test-session") {
		t.Errorf("output missing session id: %q", out)
	}
	if !strings.Contains(out, "No diffs") {
		t.Errorf("output missing 'No diffs' message: %q", out)
	}
}

func TestWriteHumanReport_WithDiffs(t *testing.T) {
	r := &Result{
		SessionID:   "diff-session",
		TotalEvents: 2,
		Decisions:   2,
		Diffs: []DecisionDiff{
			{
				Ts:             "2026-05-03T10:00:00Z",
				ToolName:       "Bash",
				ActionTarget:   "rm -rf /tmp/foo",
				OriginalRule:   "default-allow-shell",
				OriginalAllow:  true,
				ReplayedAllow:  false,
				ReplayedReason: "blast-radius:recursive-delete",
			},
		},
		Summary: Summary{UnchangedDecisions: 1, NowDenied: 1},
	}
	var buf strings.Builder
	WriteHumanReport(&buf, r)
	out := buf.String()
	if !strings.Contains(out, "NOW DENIED") {
		t.Errorf("output missing NOW DENIED label: %q", out)
	}
	if !strings.Contains(out, "blast-radius:recursive-delete") {
		t.Errorf("output missing replay reason: %q", out)
	}
}

