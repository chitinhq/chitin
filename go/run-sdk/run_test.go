package manifest

import (
	"strings"
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

func TestNewRunManifestEmptyIDsGenerateUUIDs(t *testing.T) {
	manifest, err := NewRunManifest(RunManifestInput{
		Surface: "third-party-agent",
		DriverIdentity: DriverIdentity{
			User:               "red",
			MachineID:          "workstation",
			MachineFingerprint: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		},
		AgentFingerprint: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
	})
	if err != nil {
		t.Fatalf("NewRunManifest: %v", err)
	}
	for name, value := range map[string]string{
		"run_id":            manifest.RunID,
		"session_id":        manifest.SessionID,
		"agent_instance_id": manifest.AgentInstanceID,
	} {
		if !isUUID(value) {
			t.Fatalf("%s = %q, want generated UUID", name, value)
		}
	}
}

func TestNewRunManifestAcceptsMaxUUIDs(t *testing.T) {
	parentAgentID := "ffffffff-ffff-ffff-ffff-ffffffffffff"
	_, err := NewRunManifest(RunManifestInput{
		RunID:           "ffffffff-ffff-ffff-ffff-ffffffffffff",
		SessionID:       "ffffffff-ffff-ffff-ffff-ffffffffffff",
		Surface:         "third-party-agent",
		AgentInstanceID: "ffffffff-ffff-ffff-ffff-ffffffffffff",
		ParentAgentID:   &parentAgentID,
		DriverIdentity: DriverIdentity{
			User:               "red",
			MachineID:          "workstation",
			MachineFingerprint: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		},
		AgentFingerprint: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
	})
	if err != nil {
		t.Fatalf("NewRunManifest: %v", err)
	}
}

func TestNewRunManifestErrorsOnInvalidUUIDs(t *testing.T) {
	base := RunManifestInput{
		RunID:           "550e8400-e29b-41d4-a716-446655440000",
		SessionID:       "550e8400-e29b-41d4-a716-446655440001",
		Surface:         "third-party-agent",
		AgentInstanceID: "550e8400-e29b-41d4-a716-446655440002",
		DriverIdentity: DriverIdentity{
			User:               "red",
			MachineID:          "workstation",
			MachineFingerprint: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		},
		AgentFingerprint: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
	}
	for name, mutate := range map[string]func(*RunManifestInput){
		"run_id": func(input *RunManifestInput) {
			input.RunID = "not-a-uuid"
		},
		"session_id": func(input *RunManifestInput) {
			input.SessionID = "not-a-uuid"
		},
		"agent_instance_id": func(input *RunManifestInput) {
			input.AgentInstanceID = "not-a-uuid"
		},
		"parent_agent_id": func(input *RunManifestInput) {
			parentAgentID := "not-a-uuid"
			input.ParentAgentID = &parentAgentID
		},
	} {
		t.Run(name, func(t *testing.T) {
			input := base
			mutate(&input)
			if _, err := NewRunManifest(input); err == nil {
				t.Fatal("NewRunManifest error = nil, want UUID validation error")
			}
		})
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

func TestEmitEventRejectsUnknownEventAndChainType(t *testing.T) {
	run := testRun(t)
	if _, err := run.EmitEvent("not_a_real_event", map[string]any{}, EmitOptions{}); err == nil {
		t.Fatal("expected error for unknown event_type")
	}
	if _, err := run.EmitEvent(
		"assistant_turn", map[string]any{"text": "hi"}, EmitOptions{ChainType: "bogus"},
	); err == nil {
		t.Fatal("expected error for unknown chain_type")
	}
}

func TestFinalizeClosesRun(t *testing.T) {
	run := testRun(t)
	if _, err := run.Finalize(SessionEndPayload{Reason: "clean"}, EmitOptions{}); err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	// The kernel emitter rejects appends after a session_end tail — so must the SDK.
	if _, err := run.EmitEvent("assistant_turn", map[string]any{"text": "late"}, EmitOptions{}); err == nil {
		t.Fatal("expected error emitting after Finalize")
	}
}

func TestEmitEventSnapshotsPayload(t *testing.T) {
	run := testRun(t)
	payload := map[string]any{"text": "original"}
	event, err := run.EmitEvent("assistant_turn", payload, EmitOptions{})
	if err != nil {
		t.Fatalf("EmitEvent: %v", err)
	}
	// Mutating the caller's payload after emit must not change the stored
	// event — JSONL() re-marshals it, and this_hash is already fixed.
	payload["text"] = "mutated"
	jsonl, err := run.JSONL()
	if err != nil {
		t.Fatalf("JSONL: %v", err)
	}
	if !strings.Contains(string(jsonl), `"text":"original"`) {
		t.Fatalf("stored payload was not snapshotted: %s", jsonl)
	}
	if strings.Contains(string(jsonl), `"text":"mutated"`) {
		t.Fatalf("caller mutation leaked into stored event: %s", jsonl)
	}
	_ = event
}
