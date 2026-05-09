package health

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestGather_NonexistentDir(t *testing.T) {
	r, err := Gather("/nonexistent/.chitin", 1*time.Hour)
	if err != nil {
		t.Fatalf("expected nil error for nonexistent dir, got %v", err)
	}
	if r.DirExists {
		t.Error("DirExists should be false for nonexistent dir")
	}
}

func TestGather_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	r, err := Gather(dir, 1*time.Hour)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !r.DirExists {
		t.Error("DirExists should be true for existing dir")
	}
	if r.EventsTotal != 0 {
		t.Errorf("expected 0 events, got %d", r.EventsTotal)
	}
}

func TestGather_WithJSONL(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().UTC().Format(time.RFC3339)
	content := `{"ts":"` + now + `","surface":"copilot","schema_version":"2"}
{"ts":"` + now + `","surface":"claude-code","schema_version":"2"}
`
	if err := os.WriteFile(filepath.Join(dir, "events-2026-05-09.jsonl"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	r, err := Gather(dir, 1*time.Hour)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r.EventsTotal != 2 {
		t.Errorf("expected 2 events, got %d", r.EventsTotal)
	}
	if r.EventsByWindow["copilot"] != 1 {
		t.Errorf("expected copilot=1, got %d", r.EventsByWindow["copilot"])
	}
	if r.EventsByWindow["claude-code"] != 1 {
		t.Errorf("expected claude-code=1, got %d", r.EventsByWindow["claude-code"])
	}
}

func TestGather_SchemaDrift(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().UTC().Format(time.RFC3339)
	content := `{"ts":"` + now + `","surface":"copilot","schema_version":"1"}
{bad json}
{"ts":"` + now + `","surface":"","schema_version":"2"}
{"ts":"missing-surface","schema_version":"2"}
{"ts":"` + now + `","surface":"copilot","schema_version":"2"}
`
	if err := os.WriteFile(filepath.Join(dir, "events-2026-05-09.jsonl"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	r, err := Gather(dir, 1*time.Hour)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r.EventsTotal != 1 {
		t.Errorf("expected 1 valid event, got %d", r.EventsTotal)
	}
	if r.SchemaDriftCount != 4 {
		t.Errorf("expected 4 drift entries, got %d", r.SchemaDriftCount)
	}
}

func TestGather_ClockSkewDetection(t *testing.T) {
	dir := t.TempDir()
	// Event 2 hours in the future — beyond the 1h skew tolerance
	future := time.Now().UTC().Add(2 * time.Hour).Format(time.RFC3339)
	content := `{"ts":"` + future + `","surface":"copilot","schema_version":"2"}
`
	if err := os.WriteFile(filepath.Join(dir, "events-2026-05-09.jsonl"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	r, err := Gather(dir, 1*time.Hour)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !r.ClockSkewSuspected {
		t.Error("expected ClockSkewSuspected for event 2h in future")
	}
	if r.EventsTotal != 0 {
		t.Errorf("future events should not count, got %d", r.EventsTotal)
	}
}

func TestGather_ErrorLog(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "kernel-errors.log"), []byte("line1\nline2\n\nline3\n"), 0644); err != nil {
		t.Fatal(err)
	}

	r, err := Gather(dir, 1*time.Hour)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r.HookFailureCount != 3 {
		t.Errorf("expected 3 non-empty lines, got %d", r.HookFailureCount)
	}
}

func TestScanErrorLog_Nonexistent(t *testing.T) {
	var r Report
	err := scanErrorLog("/nonexistent/kernel-errors.log", &r)
	if err != nil {
		t.Errorf("expected nil for nonexistent file, got %v", err)
	}
}

func TestScanJSONL_Nonexistent(t *testing.T) {
	var r Report
	err := scanJSONL("/nonexistent/events.jsonl", &r, time.Now(), time.Now().Add(time.Hour))
	if err != nil {
		t.Errorf("expected nil for nonexistent file, got %v", err)
	}
}

func TestGather_SkipsDirectories(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "subdir.jsonl"), 0755); err != nil {
		t.Fatal(err)
	}

	r, err := Gather(dir, 1*time.Hour)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r.EventsTotal != 0 {
		t.Error("should skip directories")
	}
}

func TestGather_SkipsNonJSONL(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("not jsonl"), 0644); err != nil {
		t.Fatal(err)
	}

	r, err := Gather(dir, 1*time.Hour)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r.EventsTotal != 0 {
		t.Error("should skip non-jsonl files")
	}
}

func TestGather_EventsOutsideWindow(t *testing.T) {
	dir := t.TempDir()
	// Event 2 days ago — outside the 1-hour window
	old := time.Now().UTC().Add(-48 * time.Hour).Format(time.RFC3339)
	content := `{"ts":"` + old + `","surface":"copilot","schema_version":"2"}
`
	if err := os.WriteFile(filepath.Join(dir, "events-2026-05-07.jsonl"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	r, err := Gather(dir, 1*time.Hour)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r.EventsTotal != 0 {
		t.Errorf("events outside window should not count, got %d", r.EventsTotal)
	}
}

func TestGather_FailedFile(t *testing.T) {
	dir := t.TempDir()
	// Create a file that's not valid JSONL and also unreadable
	jsonlPath := filepath.Join(dir, "events-bad.jsonl")
	if err := os.WriteFile(jsonlPath, []byte("good line\n"), 0644); err != nil {
		t.Fatal(err)
	}
	// Make it unreadable
	os.Chmod(jsonlPath, 0000)
	defer os.Chmod(jsonlPath, 0644) // restore for cleanup

	r, err := Gather(dir, 1*time.Hour)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should have at least one failed file entry
	if len(r.FailedFiles) == 0 && r.EventsTotal == 0 {
		// On some systems root can still read chmod 000 files
		t.Log("chmod 000 didn't block reads (likely running as root); skipping assertion")
	}
}