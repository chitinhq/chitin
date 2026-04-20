package health

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGather_CountsEventsInLastWindow(t *testing.T) {
	dir := t.TempDir()
	jsonl := filepath.Join(dir, "events.jsonl")
	now := time.Now().UTC()
	old := now.Add(-48 * time.Hour).Format(time.RFC3339)
	recent := now.Add(-1 * time.Hour).Format(time.RFC3339)
	lines := []string{
		`{"schema_version":"2","ts":"` + old + `","event_type":"session_start","surface":"claude-code"}`,
		`{"schema_version":"2","ts":"` + recent + `","event_type":"session_start","surface":"claude-code"}`,
		`{"schema_version":"2","ts":"` + recent + `","event_type":"session_end","surface":"claude-code"}`,
	}
	if err := os.WriteFile(jsonl, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	rep, err := Gather(dir, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if rep.EventsByWindow["claude-code"] != 2 {
		t.Errorf("want 2 recent claude-code events, got %d", rep.EventsByWindow["claude-code"])
	}
	if rep.HookFailureCount != 0 {
		t.Errorf("want 0 hook failures, got %d", rep.HookFailureCount)
	}
	if rep.SchemaDriftCount != 0 {
		t.Errorf("want 0 schema drift, got %d", rep.SchemaDriftCount)
	}
}

func TestGather_DetectsHookFailureRecords(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "kernel-errors.log")
	if err := os.WriteFile(log, []byte(`{"ts":"2026-04-19T10:00:00Z","error":"emit","message":"parse_event"}`+"\n"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	rep, err := Gather(dir, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if rep.HookFailureCount != 1 {
		t.Errorf("want 1 hook failure, got %d", rep.HookFailureCount)
	}
}

// Drift invariant: an event counts toward EventsTotal iff it parses, has
// schema_version == "2", has non-empty surface, and has a parseable ts. Any
// other shape bumps SchemaDriftCount exactly once and is NOT counted as a
// real event.
func TestGather_SchemaDriftRules(t *testing.T) {
	dir := t.TempDir()
	recent := time.Now().UTC().Add(-1 * time.Hour).Format(time.RFC3339)
	lines := []string{
		// Valid — counted
		`{"schema_version":"2","ts":"` + recent + `","event_type":"session_start","surface":"claude-code"}`,
		// Drift: parse failure
		`{not json`,
		// Drift: schema_version missing
		`{"ts":"` + recent + `","surface":"claude-code"}`,
		// Drift: schema_version != "2"
		`{"schema_version":"1","ts":"` + recent + `","surface":"claude-code"}`,
		// Drift: surface missing
		`{"schema_version":"2","ts":"` + recent + `"}`,
		// Drift: surface empty
		`{"schema_version":"2","ts":"` + recent + `","surface":""}`,
		// Drift: unparseable ts
		`{"schema_version":"2","ts":"not-a-date","surface":"claude-code"}`,
	}
	jsonl := filepath.Join(dir, "events.jsonl")
	if err := os.WriteFile(jsonl, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	rep, err := Gather(dir, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if rep.EventsTotal != 1 {
		t.Errorf("want 1 valid event, got %d", rep.EventsTotal)
	}
	if rep.SchemaDriftCount != 6 {
		t.Errorf("want 6 drift events, got %d", rep.SchemaDriftCount)
	}
}

// Open-error propagation: a non-ErrNotExist error on a *.jsonl file must not
// be silenced into a zero report. Simulate by making the file unreadable.
func TestGather_PropagatesNonErrNotExistOnJSONL(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses file mode permission checks")
	}
	dir := t.TempDir()
	jsonl := filepath.Join(dir, "events.jsonl")
	if err := os.WriteFile(jsonl, []byte("{}\n"), 0o000); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(jsonl, 0o644) })

	_, err := Gather(dir, 24*time.Hour)
	if err == nil {
		t.Errorf("want permission error, got nil")
	}
}

// ErrNotExist on jsonl is still silenced — missing files are the normal path
// for a fresh .chitin.
func TestGather_SilencesMissingJSONL(t *testing.T) {
	dir := t.TempDir()
	rep, err := Gather(dir, 24*time.Hour)
	if err != nil {
		t.Fatalf("want no error on empty dir, got %v", err)
	}
	if rep.EventsTotal != 0 {
		t.Errorf("want 0 events, got %d", rep.EventsTotal)
	}
}
