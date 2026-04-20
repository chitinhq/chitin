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
	jsonl := filepath.Join(dir, "events-testrun.jsonl")
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
	jsonl := filepath.Join(dir, "events-testrun.jsonl")
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

// File-level errors accumulate in FailedFiles; scanning continues for other
// files and the overall Gather call still returns nil error. One bad file
// must not black-box the health signal for every other file.
func TestGather_FailedFileDoesNotBlockOthers(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses file mode permission checks")
	}
	dir := t.TempDir()
	recent := time.Now().UTC().Add(-1 * time.Hour).Format(time.RFC3339)

	// File A: unreadable — should land in FailedFiles.
	bad := filepath.Join(dir, "events-bad.jsonl")
	if err := os.WriteFile(bad, []byte("{}\n"), 0o000); err != nil {
		t.Fatalf("write bad fixture: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(bad, 0o644) })

	// File B: valid event — should still be counted.
	good := filepath.Join(dir, "events-good.jsonl")
	line := `{"schema_version":"2","ts":"` + recent + `","event_type":"session_start","surface":"claude-code"}`
	if err := os.WriteFile(good, []byte(line+"\n"), 0o644); err != nil {
		t.Fatalf("write good fixture: %v", err)
	}

	rep, err := Gather(dir, 24*time.Hour)
	if err != nil {
		t.Fatalf("want nil err (accumulated, not bailed), got %v", err)
	}
	if rep.EventsTotal != 1 {
		t.Errorf("want 1 event from good file, got %d", rep.EventsTotal)
	}
	if len(rep.FailedFiles) != 1 {
		t.Errorf("want 1 failed file, got %d (entries: %v)", len(rep.FailedFiles), rep.FailedFiles)
	}
	if len(rep.FailedFiles) == 1 && !strings.Contains(rep.FailedFiles[0], "events-bad.jsonl") {
		t.Errorf("failed file entry should name the bad path, got: %q", rep.FailedFiles[0])
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
	if !rep.DirExists {
		t.Errorf("want DirExists=true for extant tempdir")
	}
}

// When the .chitin dir itself doesn't exist, DirExists must be false and no
// error returned — the caller decides how to surface the missing dir.
func TestGather_AbsentDirSetsDirExistsFalse(t *testing.T) {
	parent := t.TempDir()
	missing := filepath.Join(parent, "nope")
	rep, err := Gather(missing, 24*time.Hour)
	if err != nil {
		t.Fatalf("want no error on missing dir, got %v", err)
	}
	if rep.DirExists {
		t.Errorf("want DirExists=false for nonexistent dir")
	}
	if rep.EventsTotal != 0 {
		t.Errorf("want 0 events, got %d", rep.EventsTotal)
	}
}

// Clock-skew detection: an event stamped more than 1h in the future flags
// ClockSkewSuspected. Events without skew do not.
func TestGather_DetectsClockSkewFromFutureTs(t *testing.T) {
	dir := t.TempDir()
	future := time.Now().UTC().Add(48 * time.Hour).Format(time.RFC3339)
	jsonl := filepath.Join(dir, "events-testrun.jsonl")
	line := `{"schema_version":"2","ts":"` + future + `","event_type":"session_start","surface":"claude-code"}`
	if err := os.WriteFile(jsonl, []byte(line+"\n"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	rep, err := Gather(dir, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if !rep.ClockSkewSuspected {
		t.Errorf("want ClockSkewSuspected=true for future-stamped event")
	}
}

func TestGather_NoClockSkewOnRecentEvents(t *testing.T) {
	dir := t.TempDir()
	recent := time.Now().UTC().Add(-1 * time.Hour).Format(time.RFC3339)
	jsonl := filepath.Join(dir, "events-testrun.jsonl")
	line := `{"schema_version":"2","ts":"` + recent + `","event_type":"session_start","surface":"claude-code"}`
	if err := os.WriteFile(jsonl, []byte(line+"\n"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	rep, err := Gather(dir, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if rep.ClockSkewSuspected {
		t.Errorf("want ClockSkewSuspected=false for recent event")
	}
}

// Window is [WindowStart, now]. Events stamped AFTER now are excluded from
// EventsByWindow regardless of magnitude. The ClockSkewSuspected flag is
// separate — only events past now + 1h (clockSkewFutureTolerance) set it.
// Events in the narrow band (now, now+1h] are silently excluded without
// flagging, treated as NTP jitter. This test uses a 48h future event, which
// satisfies both rules: excluded from counts AND flagged as skew.
func TestGather_ExcludesFutureEventsFromCounts(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().UTC()
	past := now.Add(-1 * time.Hour).Format(time.RFC3339)
	future := now.Add(48 * time.Hour).Format(time.RFC3339)
	lines := []string{
		`{"schema_version":"2","ts":"` + past + `","event_type":"session_start","surface":"claude-code"}`,
		`{"schema_version":"2","ts":"` + future + `","event_type":"session_start","surface":"claude-code"}`,
	}
	jsonl := filepath.Join(dir, "events-testrun.jsonl")
	if err := os.WriteFile(jsonl, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	rep, err := Gather(dir, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if rep.EventsTotal != 1 {
		t.Errorf("want 1 in-window event, got %d (future event must not count)", rep.EventsTotal)
	}
	if rep.EventsByWindow["claude-code"] != 1 {
		t.Errorf("want 1 claude-code event in window, got %d", rep.EventsByWindow["claude-code"])
	}
	if !rep.ClockSkewSuspected {
		t.Errorf("want ClockSkewSuspected=true when a future event is present")
	}
}
