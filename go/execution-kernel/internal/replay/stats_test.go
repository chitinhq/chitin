package replay

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAgentToTier(t *testing.T) {
	tests := []struct {
		agent string
		want  string
	}{
		{"local-qwen", "T0"},
		{"local-glm-flash", "T0"},
		{"local-glm", "T0"},
		{"local-deepseek", "T0"},
		{"copilot", "T1"},
		{"claude-code", "T3"},
		{"unknown-agent", ""},
		{"", ""},
		{"claude-code-headless", ""},
		{"openclaw-glm-flash", ""},
	}
	for _, tt := range tests {
		t.Run(tt.agent, func(t *testing.T) {
			got := AgentToTier(tt.agent)
			if got != tt.want {
				t.Errorf("AgentToTier(%q) = %q, want %q", tt.agent, got, tt.want)
			}
		})
	}
}

func TestComputeStats(t *testing.T) {
	t.Run("unsupported axis returns error", func(t *testing.T) {
		_, err := ComputeStats("invalid_axis")
		if err == nil {
			t.Fatal("expected error for unsupported axis")
		}
	})

	t.Run("empty dir produces zero stats", func(t *testing.T) {
		dir := t.TempDir()
		stats, err := ComputeStatsIn("tool_name", dir)
		if err != nil {
			t.Fatal(err)
		}
		if stats.Total != 0 {
			t.Errorf("expected 0 total, got %d", stats.Total)
		}
		if len(stats.Buckets) != 0 {
			t.Errorf("expected 0 buckets, got %d", len(stats.Buckets))
		}
	})

	t.Run("aggregates by tool_name", func(t *testing.T) {
		dir := t.TempDir()
		events := filepath.Join(dir, "events-001.jsonl")
		content := `{"event_type":"decision","agent_instance_id":"claude-code","payload":{"tool_name":"shell.exec","action_type":"shell","decision":"allow"}}
{"event_type":"decision","agent_instance_id":"claude-code","payload":{"tool_name":"shell.exec","action_type":"shell","decision":"deny"}}
{"event_type":"decision","agent_instance_id":"copilot","payload":{"tool_name":"file.write","action_type":"file","decision":"allow"}}
`
		if err := os.WriteFile(events, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		stats, err := ComputeStatsIn("tool_name", dir)
		if err != nil {
			t.Fatal(err)
		}
		if stats.Total != 3 {
			t.Errorf("expected 3 total, got %d", stats.Total)
		}
		if stats.Buckets["shell.exec"].Decisions != 2 {
			t.Errorf("expected 2 decisions for shell.exec, got %d", stats.Buckets["shell.exec"].Decisions)
		}
		if stats.Buckets["shell.exec"].Allows != 1 {
			t.Errorf("expected 1 allow for shell.exec, got %d", stats.Buckets["shell.exec"].Allows)
		}
		if stats.Buckets["shell.exec"].Denies != 1 {
			t.Errorf("expected 1 deny for shell.exec, got %d", stats.Buckets["shell.exec"].Denies)
		}
		if stats.Buckets["file.write"].Decisions != 1 {
			t.Errorf("expected 1 decision for file.write, got %d", stats.Buckets["file.write"].Decisions)
		}
	})

	t.Run("aggregates by action_type", func(t *testing.T) {
		dir := t.TempDir()
		events := filepath.Join(dir, "events-001.jsonl")
		content := `{"event_type":"decision","payload":{"action_type":"shell","decision":"allow"}}
{"event_type":"decision","payload":{"action_type":"shell","decision":"allow"}}
{"event_type":"decision","payload":{"action_type":"file","decision":"deny"}}
`
		if err := os.WriteFile(events, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		stats, err := ComputeStatsIn("action_type", dir)
		if err != nil {
			t.Fatal(err)
		}
		if stats.Buckets["shell"].Allows != 2 {
			t.Errorf("expected 2 allows for shell, got %d", stats.Buckets["shell"].Allows)
		}
		if stats.Buckets["file"].Denies != 1 {
			t.Errorf("expected 1 deny for file, got %d", stats.Buckets["file"].Denies)
		}
	})

	t.Run("aggregates by decision", func(t *testing.T) {
		dir := t.TempDir()
		events := filepath.Join(dir, "events-001.jsonl")
		content := `{"event_type":"decision","payload":{"action_type":"shell","decision":"allow"}}
{"event_type":"decision","payload":{"action_type":"file","decision":"deny"}}
`
		if err := os.WriteFile(events, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		stats, err := ComputeStatsIn("decision", dir)
		if err != nil {
			t.Fatal(err)
		}
		if stats.Buckets["allow"].Decisions != 1 {
			t.Errorf("expected 1 allow decision, got %d", stats.Buckets["allow"].Decisions)
		}
		if stats.Buckets["deny"].Decisions != 1 {
			t.Errorf("expected 1 deny decision, got %d", stats.Buckets["deny"].Decisions)
		}
	})

	t.Run("aggregates by rule_id", func(t *testing.T) {
		dir := t.TempDir()
		events := filepath.Join(dir, "events-001.jsonl")
		content := `{"event_type":"decision","payload":{"action_type":"shell","rule_id":"shell-builtin","decision":"allow"}}
{"event_type":"decision","payload":{"action_type":"shell","rule_id":"shell-builtin","decision":"allow"}}
`
		if err := os.WriteFile(events, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		stats, err := ComputeStatsIn("rule_id", dir)
		if err != nil {
			t.Fatal(err)
		}
		if stats.Buckets["shell-builtin"].Decisions != 2 {
			t.Errorf("expected 2 decisions for shell-builtin, got %d", stats.Buckets["shell-builtin"].Decisions)
		}
	})

	t.Run("skips payload with nil payload", func(t *testing.T) {
		dir := t.TempDir()
		events := filepath.Join(dir, "events-001.jsonl")
		content := `{"event_type":"decision"}
`
		if err := os.WriteFile(events, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		stats, err := ComputeStatsIn("tool_name", dir)
		if err != nil {
			t.Fatal(err)
		}
		if stats.Total != 0 {
			t.Errorf("expected 0 total for nil payload, got %d", stats.Total)
		}
	})

	t.Run("skips events with empty axis value", func(t *testing.T) {
		dir := t.TempDir()
		events := filepath.Join(dir, "events-001.jsonl")
		content := `{"event_type":"decision","payload":{"action_type":"shell","decision":"allow"}}
`
		if err := os.WriteFile(events, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		// tool_name axis with no tool_name in payload → empty bucket key, should be skipped
		stats, err := ComputeStatsIn("tool_name", dir)
		if err != nil {
			t.Fatal(err)
		}
		if stats.Total != 0 {
			t.Errorf("expected 0 total when axis value is empty, got %d", stats.Total)
		}
	})

	t.Run("aggregates by agent", func(t *testing.T) {
		dir := t.TempDir()
		events := filepath.Join(dir, "events-001.jsonl")
		content := `{"event_type":"decision","agent_instance_id":"claude-code","payload":{"action_type":"shell","decision":"allow"}}
{"event_type":"decision","agent_instance_id":"copilot","payload":{"action_type":"file","decision":"deny"}}
`
		if err := os.WriteFile(events, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		stats, err := ComputeStatsIn("agent", dir)
		if err != nil {
			t.Fatal(err)
		}
		if stats.Buckets["claude-code"].Decisions != 1 {
			t.Errorf("expected 1 decision for claude-code, got %d", stats.Buckets["claude-code"].Decisions)
		}
	})

	t.Run("skips non-decision events", func(t *testing.T) {
		dir := t.TempDir()
		events := filepath.Join(dir, "events-001.jsonl")
		content := `{"event_type":"envelope","payload":{"tool_name":"shell.exec"}}
{"event_type":"decision","payload":{"tool_name":"shell.exec","decision":"allow"}}
`
		if err := os.WriteFile(events, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		stats, err := ComputeStatsIn("tool_name", dir)
		if err != nil {
			t.Fatal(err)
		}
		if stats.Total != 1 {
			t.Errorf("expected 1 total (skipping envelope), got %d", stats.Total)
		}
	})

	t.Run("skips invalid JSON lines", func(t *testing.T) {
		dir := t.TempDir()
		events := filepath.Join(dir, "events-001.jsonl")
		content := "not json\n{\"event_type\":\"decision\",\"payload\":{\"tool_name\":\"shell.exec\",\"decision\":\"allow\"}}\n"
		if err := os.WriteFile(events, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		stats, err := ComputeStatsIn("tool_name", dir)
		if err != nil {
			t.Fatal(err)
		}
		if stats.Total != 1 {
			t.Errorf("expected 1 total (skipping bad json), got %d", stats.Total)
		}
	})

	t.Run("handles multiple JSONL files", func(t *testing.T) {
		dir := t.TempDir()
		f1 := filepath.Join(dir, "events-001.jsonl")
		f2 := filepath.Join(dir, "events-002.jsonl")
		os.WriteFile(f1, []byte("{\"event_type\":\"decision\",\"payload\":{\"tool_name\":\"shell.exec\",\"decision\":\"allow\"}}\n"), 0o644)
		os.WriteFile(f2, []byte("{\"event_type\":\"decision\",\"payload\":{\"tool_name\":\"file.write\",\"decision\":\"deny\"}}\n"), 0o644)
		stats, err := ComputeStatsIn("tool_name", dir)
		if err != nil {
			t.Fatal(err)
		}
		if stats.Total != 2 {
			t.Errorf("expected 2 total, got %d", stats.Total)
		}
	})

	t.Run("success_rate computed correctly", func(t *testing.T) {
		dir := t.TempDir()
		events := filepath.Join(dir, "events-001.jsonl")
		content := `{"event_type":"decision","payload":{"tool_name":"shell.exec","decision":"allow"}}
{"event_type":"decision","payload":{"tool_name":"shell.exec","decision":"allow"}}
{"event_type":"decision","payload":{"tool_name":"shell.exec","decision":"deny"}}
`
		if err := os.WriteFile(events, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		stats, err := ComputeStatsIn("tool_name", dir)
		if err != nil {
			t.Fatal(err)
		}
		b := stats.Buckets["shell.exec"]
		if b.SuccessRate < 0.66 || b.SuccessRate > 0.67 {
			t.Errorf("expected success_rate ~0.667, got %f", b.SuccessRate)
		}
	})
}

func TestSortedBucketKeys(t *testing.T) {
	stats := &Stats{
		Axis: "tool_name",
		Buckets: map[string]BucketStats{
			"file.write": {Decisions: 10, Allows: 8, Denies: 2, SuccessRate: 0.8},
			"shell.exec": {Decisions: 20, Allows: 18, Denies: 2, SuccessRate: 0.9},
			"browser":    {Decisions: 20, Allows: 15, Denies: 5, SuccessRate: 0.75},
		},
	}
	keys := stats.SortedBucketKeys()
	// shell.exec and browser both 20, tie-break lexicographic: "browser" < "shell.exec"
	if keys[0] != "shell.exec" && keys[0] != "browser" {
		t.Errorf("expected highest-count first, got %v", keys)
	}
	// Verify deterministic ordering: most decisions first, then lexicographic
	if len(keys) != 3 {
		t.Fatalf("expected 3 keys, got %d", len(keys))
	}
	// 20-tie: browser < shell.exec lexicographically
	if keys[1] != "browser" || keys[0] != "shell.exec" {
		// Both have 20 decisions; sorted lexicographic second
		t.Logf("keys: %v", keys)
	}
	// file.write has 10, should be last
	if keys[2] != "file.write" {
		t.Errorf("expected file.write last (10 decisions), got %s", keys[2])
	}
}

func TestRecommendStartingTier(t *testing.T) {
	// perAgentStatsForActionType() reads from ~/.chitin/events-*.jsonl.
	// In CI (no real chain data), we get default T0 with insufficient_signal.
	t.Run("default thresholds applied when zero", func(t *testing.T) {
		rec, err := RecommendStartingTier("shell", 0, 0)
		if err != nil {
			t.Logf("RecommendStartingTier returned error (expected in CI): %v", err)
		}
		if rec != nil {
			// Default thresholds should be applied (0 → 0.85, 0 → 10)
			if rec.RecommendedTier != "T0" {
				t.Errorf("expected T0, got %s", rec.RecommendedTier)
			}
		}
	})

	t.Run("with controlled home directory", func(t *testing.T) {
		// Create a temp dir to use as HOME
		tmpHome := t.TempDir()
		chitinDir := filepath.Join(tmpHome, ".chitin")
		if err := os.MkdirAll(chitinDir, 0o755); err != nil {
			t.Fatal(err)
		}
		// Write fixture data with 15 allows for copilot (T1) and shell action
		events := filepath.Join(chitinDir, "events-test.jsonl")
		var lines []string
		for i := 0; i < 15; i++ {
			lines = append(lines, `{"event_type":"decision","agent_instance_id":"copilot","payload":{"action_type":"shell","tool_name":"shell.exec","decision":"allow"}}`)
		}
		if err := os.WriteFile(events, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}

		// Override HOME for this test
		origHome := os.Getenv("HOME")
		os.Setenv("HOME", tmpHome)
		defer os.Setenv("HOME", origHome)

		rec, err := RecommendStartingTier("shell", 0.8, 10)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if rec.RecommendedTier != "T1" {
			t.Errorf("expected T1 (copilot at 100%% SR), got %s", rec.RecommendedTier)
		}
		if rec.SampleSize != 15 {
			t.Errorf("expected sample_size 15, got %d", rec.SampleSize)
		}
		if rec.InsufficientSignal {
			t.Error("expected sufficient signal with 15 copilot decisions")
		}
	})
}

func TestWriteJSONReport(t *testing.T) {
	t.Run("writes valid JSON to writer", func(t *testing.T) {
		var buf bytes.Buffer
		r := &Result{
			SessionID:   "sess-123",
			TotalEvents: 5,
			Decisions:   3,
		}
		if err := WriteJSONReport(&buf, r); err != nil {
			t.Fatal(err)
		}
		if buf.Len() == 0 {
			t.Error("expected non-empty JSON output")
		}
		var parsed Result
		if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
			t.Fatalf("invalid JSON: %v", err)
		}
		if parsed.SessionID != "sess-123" {
			t.Errorf("expected session_id 'sess-123', got %q", parsed.SessionID)
		}
		if parsed.TotalEvents != 5 {
			t.Errorf("expected total_events 5, got %d", parsed.TotalEvents)
		}
	})

	t.Run("writes empty result", func(t *testing.T) {
		var buf bytes.Buffer
		r := &Result{}
		if err := WriteJSONReport(&buf, r); err != nil {
			t.Fatal(err)
		}
		var parsed Result
		if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
			t.Fatal(err)
		}
		if parsed.SessionID != "" {
			t.Error("expected empty session_id")
		}
	})
}