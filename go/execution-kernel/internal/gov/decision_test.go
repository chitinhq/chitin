package gov

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteLog_AppendsOneJSONLine(t *testing.T) {
	dir := t.TempDir()
	d := Decision{
		Allowed: false, Mode: "guide", RuleID: "no-rm",
		Reason: "no rm", Ts: "2026-04-22T00:00:00Z",
	}
	if err := WriteLog(d, dir); err != nil {
		t.Fatalf("WriteLog: %v", err)
	}

	// Find the file
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("expected 1 log file, got %d", len(entries))
	}
	path := filepath.Join(dir, entries[0].Name())
	f, _ := os.Open(path)
	defer f.Close()
	sc := bufio.NewScanner(f)
	lines := 0
	for sc.Scan() {
		lines++
		var got map[string]any
		if err := json.Unmarshal(sc.Bytes(), &got); err != nil {
			t.Errorf("line is not valid JSON: %v", err)
		}
		if got["rule_id"] != "no-rm" {
			t.Errorf("RuleID roundtrip: got %q", got["rule_id"])
		}
	}
	if lines != 1 {
		t.Errorf("expected 1 line, got %d", lines)
	}
}

func TestWriteLog_MultipleAppends(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 5; i++ {
		_ = WriteLog(Decision{
			Allowed: true, Mode: "monitor", RuleID: "x",
			Ts: "2026-04-22T00:00:00Z",
		}, dir)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("should still be 1 file")
	}
	path := filepath.Join(dir, entries[0].Name())
	f, _ := os.Open(path)
	defer f.Close()
	sc := bufio.NewScanner(f)
	lines := 0
	for sc.Scan() {
		lines++
	}
	if lines != 5 {
		t.Errorf("expected 5 lines, got %d", lines)
	}
}
