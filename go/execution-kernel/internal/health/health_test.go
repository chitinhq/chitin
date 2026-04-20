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
	os.WriteFile(jsonl, []byte(strings.Join(lines, "\n")+"\n"), 0o644)

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
}

func TestGather_DetectsHookFailureRecords(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "kernel-errors.log")
	os.WriteFile(log, []byte(`{"ts":"2026-04-19T10:00:00Z","error":"emit","message":"parse_event"}`+"\n"), 0o644)

	rep, err := Gather(dir, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if rep.HookFailureCount != 1 {
		t.Errorf("want 1 hook failure, got %d", rep.HookFailureCount)
	}
}
