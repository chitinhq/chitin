package manifest

import (
	"testing"
)

func testRun(t *testing.T) *Run {
	t.Helper()
	run, err := NewRun(RunManifestInput{
		RunID:     "550e8400-e29b-41d4-a716-446655440000",
		SessionID: "550e8400-e29b-41d4-a716-446655440001",
		Surface:   "third-party-agent",
		DriverIdentity: DriverIdentity{
			User:               "red",
			MachineID:          "workstation",
			MachineFingerprint: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		},
		AgentInstanceID:  "550e8400-e29b-41d4-a716-446655440002",
		AgentFingerprint: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		Labels:           map[string]string{"source": "sdk"},
	})
	if err != nil {
		t.Fatalf("NewRun: %v", err)
	}
	return run
}

func TestNewRun(t *testing.T) {
	run := testRun(t)
	if run.Manifest.SchemaVersion != "2" {
		t.Fatalf("schema version = %q, want 2", run.Manifest.SchemaVersion)
	}
	if run.Manifest.ParentAgentID != nil {
		t.Fatalf("parent agent id = %v, want nil", run.Manifest.ParentAgentID)
	}
}

func TestEmitEvent(t *testing.T) {
	run := testRun(t)
	started, err := run.EmitEvent("session_start", SessionStartPayload{
		Cwd:               "/tmp",
		ClientInfo:        ClientInfo{Name: "sdk-test", Version: "1.0.0"},
		Model:             Model{Name: "demo", Provider: "demo"},
		SystemPromptHash:  "0000000000000000000000000000000000000000000000000000000000000000",
		ToolAllowlistHash: "1111111111111111111111111111111111111111111111111111111111111111",
		AgentVersion:      "1.0.0",
	}, EmitOptions{})
	if err != nil {
		t.Fatalf("EmitEvent start: %v", err)
	}
	tool, err := run.EmitEvent("intended", map[string]any{
		"tool_name":   "Read",
		"raw_input":   map[string]any{"path": "/tmp/input.txt"},
		"action_type": "read",
	}, EmitOptions{
		ChainID:   "tool-call-1",
		ChainType: "tool_call",
	})
	if err != nil {
		t.Fatalf("EmitEvent intended: %v", err)
	}
	ended, err := run.Finalize(SessionEndPayload{
		Reason: "clean",
		Totals: Totals{
			TurnCount:         1,
			ToolCallCount:     1,
			TotalInputTokens:  0,
			TotalOutputTokens: 0,
			TotalDurationMs:   20,
		},
	}, EmitOptions{})
	if err != nil {
		t.Fatalf("Finalize: %v", err)
	}

	if started.Seq != 0 || started.PrevHash != nil {
		t.Fatalf("start seq/prev = %d/%v, want 0/nil", started.Seq, started.PrevHash)
	}
	if tool.Seq != 0 || tool.ParentChainID == nil || *tool.ParentChainID != run.Manifest.SessionID {
		t.Fatalf("tool event parent chain = %v, want %q", tool.ParentChainID, run.Manifest.SessionID)
	}
	if ended.Seq != 1 || ended.PrevHash == nil || *ended.PrevHash != started.ThisHash {
		t.Fatalf("end seq/prev = %d/%v, want 1/%q", ended.Seq, ended.PrevHash, started.ThisHash)
	}
	if len(run.Events()) != 3 {
		t.Fatalf("events = %d, want 3", len(run.Events()))
	}
}

func TestJSONL(t *testing.T) {
	run := testRun(t)
	if _, err := run.EmitEvent("session_start", SessionStartPayload{
		Cwd:               "/tmp",
		ClientInfo:        ClientInfo{Name: "sdk-test", Version: "1.0.0"},
		Model:             Model{Name: "demo", Provider: "demo"},
		SystemPromptHash:  "0000000000000000000000000000000000000000000000000000000000000000",
		ToolAllowlistHash: "1111111111111111111111111111111111111111111111111111111111111111",
		AgentVersion:      "1.0.0",
	}, EmitOptions{}); err != nil {
		t.Fatalf("EmitEvent: %v", err)
	}
	jsonl, err := run.JSONL()
	if err != nil {
		t.Fatalf("JSONL: %v", err)
	}
	if len(jsonl) == 0 {
		t.Fatal("expected JSONL bytes")
	}
}
