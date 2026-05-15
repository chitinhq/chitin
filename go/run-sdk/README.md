# `github.com/chitinhq/chitin/go/run-sdk`

Standalone Go SDK for emitting chitin-compatible run events without
importing the execution kernel.

## Quickstart

```go
package main

import (
	"fmt"

	"github.com/chitinhq/chitin/go/run-sdk"
)

func main() {
	run, err := manifest.NewRun(manifest.RunManifestInput{
		Surface: "third-party-agent",
		DriverIdentity: manifest.DriverIdentity{
			User:               "red",
			MachineID:          "workstation",
			MachineFingerprint: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		},
		AgentFingerprint: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		Labels: map[string]string{"source": "sdk"},
	})
	if err != nil {
		panic(err)
	}

	_, _ = run.EmitEvent("session_start", manifest.SessionStartPayload{
		Cwd: "/tmp",
		ClientInfo: manifest.ClientInfo{Name: "demo-tool", Version: "1.0.0"},
		Model: manifest.Model{Name: "demo-model", Provider: "demo"},
		SystemPromptHash:  "0000000000000000000000000000000000000000000000000000000000000000",
		ToolAllowlistHash: "1111111111111111111111111111111111111111111111111111111111111111",
		AgentVersion:      "1.0.0",
	}, manifest.EmitOptions{})

	event, _ := run.Finalize(manifest.SessionEndPayload{
		Reason: "clean",
		Totals: manifest.Totals{
			TurnCount:        1,
			ToolCallCount:    1,
			TotalInputTokens: 0,
			TotalOutputTokens: 0,
			TotalDurationMs:  15,
		},
	}, manifest.EmitOptions{})

	fmt.Println(event.ThisHash)
}
```
