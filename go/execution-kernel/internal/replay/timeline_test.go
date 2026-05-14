package replay

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

func TestBuildTimeline_AggregatesAndJoins(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CHITIN_HOME", home)

	writeReplayFile(t, filepath.Join(home, "events-run-a.jsonl"), strings.Join([]string{
		mustJSON(t, map[string]any{
			"schema_version": "2", "run_id": "run-a", "session_id": "sess-1", "surface": "codex",
			"agent_instance_id": "agent-1", "agent_fingerprint": "fp", "event_type": "session_start",
			"chain_id": "sess-1", "chain_type": "session", "seq": 0, "this_hash": "h0",
			"ts": "2026-05-13T10:00:00Z", "labels": map[string]any{"driver": "codex", "agent_instance_id": "agent-1"},
			"payload": map[string]any{},
		}),
		mustJSON(t, map[string]any{
			"schema_version": "2", "run_id": "run-a", "session_id": "sess-1", "surface": "codex",
			"agent_instance_id": "agent-1", "agent_fingerprint": "fp", "event_type": "assistant_turn",
			"chain_id": "sess-1", "chain_type": "session", "seq": 1, "prev_hash": "h0", "this_hash": "h1",
			"ts": "2026-05-13T10:00:01Z", "labels": map[string]any{"driver": "codex", "agent_instance_id": "agent-1"},
			"payload": map[string]any{
				"text":     "done",
				"thinking": "plan",
				"usage":    map[string]any{"input_tokens": 10, "output_tokens": 5, "thinking_tokens": 2},
			},
		}),
		mustJSON(t, map[string]any{
			"schema_version": "2", "run_id": "run-a", "session_id": "sess-1", "surface": "codex",
			"agent_instance_id": "agent-1", "agent_fingerprint": "fp", "event_type": "decision",
			"chain_id": "sess-1", "chain_type": "session", "seq": 2, "prev_hash": "h1", "this_hash": "h2",
			"ts": "2026-05-13T10:00:02Z", "labels": map[string]any{"driver": "codex", "agent_instance_id": "agent-1"},
			"payload": map[string]any{
				"event_id":  "evt-decision",
				"tool_name": "shell.exec", "action_type": "shell.exec", "action_target": "echo hi",
				"decision": "allow", "rule_id": "allow-shell",
			},
		}),
		mustJSON(t, map[string]any{
			"schema_version": "2", "run_id": "run-a", "session_id": "sess-1", "surface": "codex",
			"agent_instance_id": "agent-1", "agent_fingerprint": "fp", "event_type": "post_tool_use",
			"chain_id": "sess-1", "chain_type": "session", "seq": 3, "prev_hash": "h2", "this_hash": "h3",
			"ts": "2026-05-13T10:00:03Z", "labels": map[string]any{"driver": "codex", "agent_instance_id": "agent-1"},
			"payload": map[string]any{"event_id": "evt-post", "tool_name": "shell.exec", "duration_ms": 42},
		}),
	}, "\n")+"\n")

	writeReplayFile(t, filepath.Join(home, "gov-decisions-2026-05-13.jsonl"), mustJSON(t, map[string]any{
		"allowed": true, "mode": "enforce", "rule_id": "allow-shell", "reason": "ok",
		"action_type": "shell.exec", "action_target": "echo hi", "ts": "2026-05-13T10:00:02Z",
		"cost_usd": 0.125, "input_bytes": 12, "output_bytes": 3,
		"agent_instance_id": "agent-1", "agent": "agent-1", "driver": "codex", "model": "gpt-5.5",
		"predicted_blast": 0.7, "floundering_score": 0.2, "routing_decision": "watch",
	})+"\n")

	db, err := sql.Open("sqlite", filepath.Join(home, "sidecar.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE event_blobs (event_id TEXT NOT NULL, blob_type TEXT NOT NULL, blob BLOB, redacted BOOL, ts INTEGER, PRIMARY KEY (event_id, blob_type))`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO event_blobs (event_id, blob_type, blob, redacted, ts) VALUES (?, ?, ?, 0, 0)`,
		"evt-decision", "tool_input", []byte(`{"command":"echo hi"}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO event_blobs (event_id, blob_type, blob, redacted, ts) VALUES (?, ?, ?, 0, 0)`,
		"evt-post", "tool_output", []byte(`{"stdout":"hi"}`)); err != nil {
		t.Fatal(err)
	}

	timeline, err := BuildTimeline(ReplayOptions{SessionID: "sess-1"})
	if err != nil {
		t.Fatalf("BuildTimeline: %v", err)
	}
	if len(timeline.Steps) != 4 {
		t.Fatalf("steps=%d want 4", len(timeline.Steps))
	}
	if timeline.Summary.ToolCallCount != 1 {
		t.Fatalf("tool_calls=%d want 1", timeline.Summary.ToolCallCount)
	}
	if timeline.Summary.DecisionsPerRule["allow-shell"] != 1 {
		t.Fatalf("decisions_per_rule=%v", timeline.Summary.DecisionsPerRule)
	}
	if timeline.Summary.TimeOnToolMs["shell.exec"] != 42 {
		t.Fatalf("time_on_tool_ms=%v", timeline.Summary.TimeOnToolMs)
	}
	if timeline.Summary.TotalCostUSD != 0.125 {
		t.Fatalf("total_cost=%.3f want 0.125", timeline.Summary.TotalCostUSD)
	}
	if timeline.Summary.TotalInputTokens != 10 || timeline.Summary.TotalOutputTokens != 5 || timeline.Summary.TotalTokens != 17 {
		t.Fatalf("token summary=%+v", timeline.Summary)
	}
	if timeline.Summary.DispatchCount != 1 || timeline.Summary.AllowedCount != 1 || timeline.Summary.SuccessRate != 1 {
		t.Fatalf("dispatch summary=%+v", timeline.Summary)
	}
	if got := timeline.Summary.CostByDriver["codex"]; got != 0.125 {
		t.Fatalf("cost_by_driver=%v", timeline.Summary.CostByDriver)
	}
	if got := timeline.Summary.CostByTool["shell.exec"]; got != 0.125 {
		t.Fatalf("cost_by_tool=%v", timeline.Summary.CostByTool)
	}
	if len(timeline.Summary.CostTimeline) != 4 || timeline.Summary.CostTimeline[2].CumulativeUSD != 0.125 {
		t.Fatalf("cost_timeline=%+v", timeline.Summary.CostTimeline)
	}
	if len(timeline.Summary.CostHeatmap) != 1 || timeline.Summary.CostHeatmap[0].Model != "gpt-5.5" {
		t.Fatalf("cost_heatmap=%+v", timeline.Summary.CostHeatmap)
	}

	decisionStep := timeline.Steps[2]
	if decisionStep.Input == nil {
		t.Fatalf("decision input missing: %+v", decisionStep)
	}
	if decisionStep.CostUSD != 0.125 || decisionStep.Model != "gpt-5.5" {
		t.Fatalf("decision step cost/model missing: %+v", decisionStep)
	}
	if decisionStep.Prediction == nil || decisionStep.Prediction.PredictedBlast != 0.7 {
		t.Fatalf("prediction missing: %+v", decisionStep.Prediction)
	}
	postStep := timeline.Steps[3]
	if postStep.Output == nil {
		t.Fatalf("post output missing: %+v", postStep)
	}
}

func TestBuildTimeline_ReconcilesMissingCostFromEnvelopeSpend(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CHITIN_HOME", home)

	writeReplayFile(t, filepath.Join(home, "events-run-c.jsonl"), strings.Join([]string{
		mustJSON(t, map[string]any{
			"schema_version": "2", "run_id": "run-c", "session_id": "sess-3", "surface": "codex",
			"agent_instance_id": "agent-3", "agent_fingerprint": "fp", "event_type": "session_start",
			"chain_id": "sess-3", "chain_type": "session", "seq": 0, "this_hash": "c0",
			"ts": "2026-05-13T12:00:00Z", "labels": map[string]any{"driver": "codex", "agent_instance_id": "agent-3"},
			"payload": map[string]any{"cwd": "/tmp/swarm-codex-t_7ab3d45c"},
		}),
		mustJSON(t, map[string]any{
			"schema_version": "2", "run_id": "run-c", "session_id": "sess-3", "surface": "codex",
			"agent_instance_id": "agent-3", "agent_fingerprint": "fp", "event_type": "decision",
			"chain_id": "sess-3", "chain_type": "session", "seq": 1, "this_hash": "c1",
			"ts": "2026-05-13T12:00:01Z", "labels": map[string]any{"driver": "codex", "agent_instance_id": "agent-3"},
			"payload": map[string]any{"tool_name": "file.read", "action_type": "file.read", "action_target": "README.md", "decision": "allow"},
		}),
	}, "\n")+"\n")

	writeReplayFile(t, filepath.Join(home, "gov-decisions-2026-05-13.jsonl"), mustJSON(t, map[string]any{
		"allowed": true, "mode": "enforce", "rule_id": "allow-reads",
		"action_type": "file.read", "action_target": "README.md", "ts": "2026-05-13T12:00:01Z",
		"agent_instance_id": "agent-3", "agent": "agent-3", "driver": "codex", "envelope_id": "env-1",
	})+"\n")

	db, err := sql.Open("sqlite", filepath.Join(home, "gov.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE envelopes (
		id TEXT PRIMARY KEY,
		created_at TEXT NOT NULL,
		closed_at TEXT,
		max_tool_calls INTEGER NOT NULL DEFAULT 0,
		max_input_bytes INTEGER NOT NULL DEFAULT 0,
		budget_usd REAL NOT NULL DEFAULT 0,
		spent_calls INTEGER NOT NULL DEFAULT 0,
		spent_bytes INTEGER NOT NULL DEFAULT 0,
		spent_usd REAL NOT NULL DEFAULT 0,
		last_spend_at TEXT
	)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO envelopes (id, created_at, spent_usd) VALUES ('env-1', '2026-05-13T12:00:00Z', 0.42)`); err != nil {
		t.Fatal(err)
	}

	timeline, err := BuildTimeline(ReplayOptions{SessionID: "sess-3"})
	if err != nil {
		t.Fatalf("BuildTimeline: %v", err)
	}
	if got := timeline.Steps[1].CostUSD; got != 0.42 {
		t.Fatalf("reconciled cost=%.2f want 0.42", got)
	}
}

func TestFindSessionForTicket(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CHITIN_HOME", home)

	writeReplayFile(t, filepath.Join(home, "events-run-ticket.jsonl"), strings.Join([]string{
		mustJSON(t, map[string]any{
			"schema_version": "2", "run_id": "run-ticket", "session_id": "sess-old", "surface": "codex",
			"agent_instance_id": "agent-1", "agent_fingerprint": "fp", "event_type": "session_start",
			"chain_id": "sess-old", "chain_type": "session", "seq": 0, "this_hash": "t0",
			"ts": "2026-05-13T11:00:00Z", "labels": map[string]any{"driver": "codex"},
			"payload": map[string]any{"cwd": "/tmp/swarm-codex-t_7ab3d45c-old"},
		}),
		mustJSON(t, map[string]any{
			"schema_version": "2", "run_id": "run-ticket", "session_id": "sess-new", "surface": "codex",
			"agent_instance_id": "agent-1", "agent_fingerprint": "fp", "event_type": "session_start",
			"chain_id": "sess-new", "chain_type": "session", "seq": 1, "this_hash": "t1",
			"ts": "2026-05-13T12:00:00Z", "labels": map[string]any{"driver": "codex"},
			"payload": map[string]any{"cwd": "/tmp/swarm-codex-t_7ab3d45c"},
		}),
	}, "\n")+"\n")

	got, err := FindSessionForTicket("t_7ab3d45c")
	if err != nil {
		t.Fatalf("FindSessionForTicket: %v", err)
	}
	if got != "sess-new" {
		t.Fatalf("session=%s want sess-new", got)
	}
}

func TestBuildTimeline_Filters(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CHITIN_HOME", home)

	writeReplayFile(t, filepath.Join(home, "events-run-b.jsonl"), strings.Join([]string{
		mustJSON(t, map[string]any{
			"schema_version": "2", "run_id": "run-b", "session_id": "sess-2", "surface": "codex",
			"agent_instance_id": "agent-2", "agent_fingerprint": "fp", "event_type": "decision",
			"chain_id": "sess-2", "chain_type": "session", "seq": 0, "this_hash": "a0",
			"ts": "2026-05-13T11:00:00Z", "labels": map[string]any{"driver": "codex", "agent_instance_id": "agent-2"},
			"payload": map[string]any{"tool_name": "shell.exec", "action_type": "shell.exec", "action_target": "echo hi", "decision": "allow"},
		}),
		mustJSON(t, map[string]any{
			"schema_version": "2", "run_id": "run-b", "session_id": "sess-2", "surface": "codex",
			"agent_instance_id": "agent-2", "agent_fingerprint": "fp", "event_type": "decision",
			"chain_id": "sess-2", "chain_type": "session", "seq": 1, "this_hash": "a1",
			"ts": "2026-05-13T11:01:00Z", "labels": map[string]any{"driver": "hermes", "agent_instance_id": "agent-2"},
			"payload": map[string]any{"tool_name": "file.read", "action_type": "file.read", "action_target": "README.md", "decision": "allow"},
		}),
	}, "\n")+"\n")

	timeline, err := BuildTimeline(ReplayOptions{
		SessionID: "sess-2",
		From:      "2026-05-13T11:00:30Z",
		Driver:    "hermes",
		Tool:      "file.read",
	})
	if err != nil {
		t.Fatalf("BuildTimeline: %v", err)
	}
	if len(timeline.Steps) != 1 {
		t.Fatalf("filtered steps=%d want 1", len(timeline.Steps))
	}
	if timeline.Steps[0].Driver != "hermes" || timeline.Steps[0].Tool != "file.read" {
		t.Fatalf("filtered step=%+v", timeline.Steps[0])
	}
}

func TestListRecentSessions(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CHITIN_HOME", home)

	writeReplayFile(t, filepath.Join(home, "events-one.jsonl"), mustJSON(t, map[string]any{
		"schema_version": "2", "run_id": "one", "session_id": "sess-old", "surface": "codex",
		"agent_instance_id": "a1", "agent_fingerprint": "fp", "event_type": "decision",
		"chain_id": "sess-old", "chain_type": "session", "seq": 0, "this_hash": "x1",
		"ts": "2026-05-13T09:00:00Z", "labels": map[string]any{"driver": "codex", "agent_instance_id": "a1"},
		"payload": map[string]any{"tool_name": "file.read"},
	})+"\n")
	writeReplayFile(t, filepath.Join(home, "events-two.jsonl"), mustJSON(t, map[string]any{
		"schema_version": "2", "run_id": "two", "session_id": "sess-new", "surface": "codex",
		"agent_instance_id": "a2", "agent_fingerprint": "fp", "event_type": "decision",
		"chain_id": "sess-new", "chain_type": "session", "seq": 0, "this_hash": "x2",
		"ts": "2026-05-13T12:00:00Z", "labels": map[string]any{"driver": "hermes", "agent_instance_id": "a2"},
		"payload": map[string]any{"tool_name": "shell.exec"},
	})+"\n")

	sessions, err := ListRecentSessions(1)
	if err != nil {
		t.Fatalf("ListRecentSessions: %v", err)
	}
	if len(sessions) != 1 || sessions[0].SessionID != "sess-new" {
		t.Fatalf("sessions=%+v", sessions)
	}
}

func writeReplayFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}
