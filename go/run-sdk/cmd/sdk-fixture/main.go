package main

import (
	"fmt"
	"os"

	manifest "github.com/chitinhq/chitin/go/run-sdk"
)

func main() {
	run, err := manifest.NewRun(manifest.RunManifestInput{
		RunID:     "550e8400-e29b-41d4-a716-446655440000",
		SessionID: "550e8400-e29b-41d4-a716-446655440001",
		Surface:   "third-party-agent",
		DriverIdentity: manifest.DriverIdentity{
			User:               "red",
			MachineID:          "workstation",
			MachineFingerprint: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		},
		AgentInstanceID:  "550e8400-e29b-41d4-a716-446655440002",
		AgentFingerprint: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		Labels:           map[string]string{"source": "go-sdk"},
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if _, err := run.EmitEvent("session_start", manifest.SessionStartPayload{
		Cwd:               "/tmp",
		ClientInfo:        manifest.ClientInfo{Name: "sdk-fixture", Version: "1.0.0"},
		Model:             manifest.Model{Name: "demo", Provider: "demo"},
		SystemPromptHash:  "0000000000000000000000000000000000000000000000000000000000000000",
		ToolAllowlistHash: "1111111111111111111111111111111111111111111111111111111111111111",
		AgentVersion:      "1.0.0",
	}, manifest.EmitOptions{}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if _, err := run.EmitEvent("intended", map[string]any{
		"tool_name":   "Read",
		"raw_input":   map[string]any{"path": "/tmp/input.txt"},
		"action_type": "read",
	}, manifest.EmitOptions{
		ChainID:   "tool-call-1",
		ChainType: "tool_call",
	}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if _, err := run.Finalize(manifest.SessionEndPayload{
		Reason: "clean",
		Totals: manifest.Totals{
			TurnCount:         1,
			ToolCallCount:     1,
			TotalInputTokens:  0,
			TotalOutputTokens: 0,
			TotalDurationMs:   20,
		},
	}, manifest.EmitOptions{}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	jsonl, err := run.JSONL()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Print(string(jsonl))
}
