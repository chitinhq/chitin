package copilot

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	copilotsdk "github.com/github/copilot-sdk/go"
	"github.com/chitinhq/chitin/go/execution-kernel/internal/gov"
)

// TestIntegration_ReplaysSpikeStream reads every tool.execution_start event
// from the spike's Rung 2 stream capture and synthesizes a PermissionRequest
// for each, then runs it through Normalize + a mock-gated Handler. Proves
// the event parser + Normalize + Handler chain works against real observed
// SDK output.
func TestIntegration_ReplaysSpikeStream(t *testing.T) {
	f, err := os.Open(filepath.Join("testdata", "spike-rung2-stream.jsonl"))
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	defer f.Close()

	// Mock gate that allows everything so we see every tool call reach the handler.
	allowGate := &mockGate{
		decision: gov.Decision{Allowed: true, RuleID: "test-allow-all"},
	}
	h := &Handler{
		Gate:  allowGate,
		Agent: "copilot-cli-integration-test",
		Cwd:   "/work",
	}

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024) // some events are large (e.g., system.message)
	toolCallsSeen := 0

	for scanner.Scan() {
		line := scanner.Bytes()
		var env struct {
			EventType string          `json:"eventType"`
			Data      json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal(line, &env); err != nil {
			t.Fatalf("unmarshal line: %v\nline: %s", err, string(line))
		}

		if env.EventType != "tool.execution_start" {
			continue
		}

		// The data shape of tool.execution_start (from spike Rung 2 findings):
		//   { "toolCallId": "...", "toolName": "...", "arguments": { ... } }
		var tc struct {
			ToolCallID string                 `json:"toolCallId"`
			ToolName   string                 `json:"toolName"`
			Arguments  map[string]interface{} `json:"arguments"`
		}
		if err := json.Unmarshal(env.Data, &tc); err != nil {
			t.Fatalf("unmarshal tool call: %v", err)
		}

		req := synthesizePermissionRequestFromToolCall(tc.ToolName, tc.Arguments)
		_, callErr := h.OnPermissionRequest(req, copilotsdk.PermissionInvocation{SessionID: "fixture"})
		if callErr != nil {
			t.Errorf("handler rejected a tool call that mock-gate allowed (%s): %v",
				tc.ToolName, callErr)
		}
		toolCallsSeen++
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scanner error: %v", err)
	}

	if toolCallsSeen == 0 {
		t.Fatal("no tool.execution_start events in fixture — fixture stale or event type renamed?")
	}
	t.Logf("replayed %d tool.execution_start events through handler", toolCallsSeen)
}

// synthesizePermissionRequestFromToolCall translates a captured
// tool.execution_start event into the shape of a PermissionRequest that
// would have preceded it. This mirrors the Kind dispatch Normalize uses.
func synthesizePermissionRequestFromToolCall(toolName string, args map[string]interface{}) copilotsdk.PermissionRequest {
	switch toolName {
	case "bash", "shell", "terminal":
		cmd, _ := args["command"].(string)
		return copilotsdk.PermissionRequest{
			Kind:            copilotsdk.PermissionRequestKindShell,
			FullCommandText: &cmd,
		}
	case "str_replace_editor", "write", "edit":
		path, _ := args["path"].(string)
		return copilotsdk.PermissionRequest{
			Kind:     copilotsdk.PermissionRequestKindWrite,
			FileName: &path,
		}
	case "read":
		path, _ := args["path"].(string)
		return copilotsdk.PermissionRequest{
			Kind: copilotsdk.PermissionRequestKindRead,
			Path: &path,
		}
	default:
		// Custom tools, MCP calls, etc. — give it a CustomTool kind with the tool name
		tn := toolName
		return copilotsdk.PermissionRequest{
			Kind:     copilotsdk.PermissionRequestKindCustomTool,
			ToolName: &tn,
		}
	}
}
