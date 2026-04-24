// Rung 2 observation probe: capture structured tool-call events from the
// Copilot SDK's session event stream.
//
// Invariant: if the probe exits 0, at least one SessionEvent of type
// "tool.execution_start" was captured in captured-stream.jsonl, with a
// non-empty ToolName and a non-nil Arguments value, serializable to JSON.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"

	copilot "github.com/github/copilot-sdk/go"
)

// capturedEntry is the envelope written to captured-stream.jsonl for each event.
type capturedEntry struct {
	// Direction is always "inbound" — all events originate from the CLI subprocess.
	Direction string `json:"direction"`
	// CapturedAt is the local wall-clock time this entry was written.
	CapturedAt time.Time `json:"capturedAt"`
	// EventType is the SessionEvent.Type string (e.g., "tool.execution_start").
	EventType string `json:"eventType"`
	// EventID is the UUID assigned by the SDK to this event.
	EventID string `json:"eventId"`
	// ParentID is the preceding event's ID (null for first event).
	ParentID *string `json:"parentId,omitempty"`
	// Data is the typed event payload, serialized to a JSON object.
	Data any `json:"data"`
}

func main() {
	startTime := time.Now()
	fmt.Printf("[rung2] start: %s\n", startTime.UTC().Format(time.RFC3339))

	cliPath := os.Getenv("COPILOT_CLI_PATH")
	if cliPath == "" {
		cliPath = "/home/red/.vite-plus/bin/copilot"
	}
	fmt.Printf("[rung2] cli-path: %s\n", cliPath)

	outFile, err := os.Create("captured-stream.jsonl")
	if err != nil {
		log.Fatalf("[rung2] FAIL — create output file: %v", err)
	}
	defer outFile.Close()

	enc := json.NewEncoder(outFile)

	// Timeout: 60 s total; break out early on first tool.execution_start.
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	client := copilot.NewClient(&copilot.ClientOptions{
		CLIPath:  cliPath,
		LogLevel: "error",
	})

	if err := client.Start(ctx); err != nil {
		log.Fatalf("[rung2] FAIL — client.Start: %v", err)
	}
	defer func() {
		if err := client.Stop(); err != nil {
			fmt.Fprintf(os.Stderr, "[rung2] client.Stop warning: %v\n", err)
		}
	}()
	fmt.Println("[rung2] client started")

	// toolCallSeen signals the main goroutine to break after the first
	// tool.execution_start event is captured.
	toolCallSeen := make(chan struct{}, 1)

	session, err := client.CreateSession(ctx, &copilot.SessionConfig{
		Model:               "gpt-4.1",
		OnPermissionRequest: copilot.PermissionHandler.ApproveAll,
	})
	if err != nil {
		log.Fatalf("[rung2] FAIL — CreateSession: %v", err)
	}
	defer session.Disconnect() //nolint:errcheck
	fmt.Println("[rung2] session created")

	// Tap: session.On delivers every SessionEvent to this handler.
	// The handler serializes each event to captured-stream.jsonl.
	// This is the primary observation mechanism — no transport wrapping needed.
	unsubscribe := session.On(func(event copilot.SessionEvent) {
		entry := capturedEntry{
			Direction:  "inbound",
			CapturedAt: time.Now().UTC(),
			EventType:  string(event.Type),
			EventID:    event.ID,
			ParentID:   event.ParentID,
			Data:       event.Data,
		}
		if encErr := enc.Encode(entry); encErr != nil {
			fmt.Fprintf(os.Stderr, "[rung2] encode warning: %v\n", encErr)
		}
		fmt.Printf("[rung2] event: %s\n", event.Type)

		// Signal on the first tool execution start.
		if event.Type == copilot.SessionEventTypeToolExecutionStart {
			if d, ok := event.Data.(*copilot.ToolExecutionStartData); ok {
				fmt.Printf("[rung2] tool-call: tool=%q  callId=%s\n", d.ToolName, d.ToolCallID)
			}
			select {
			case toolCallSeen <- struct{}{}:
			default:
			}
		}
	})
	defer unsubscribe()

	// Send a prompt that reliably triggers a shell tool call.
	msgID, err := session.Send(ctx, copilot.MessageOptions{
		Prompt: "List the files in /tmp using the shell tool. Just run the command; do not explain.",
	})
	if err != nil {
		log.Fatalf("[rung2] FAIL — Send: %v", err)
	}
	fmt.Printf("[rung2] message sent: id=%s\n", msgID)

	// Wait for first tool call OR timeout.
	deadline := time.After(30 * time.Second)
	select {
	case <-toolCallSeen:
		fmt.Println("[rung2] tool-call captured — exiting cleanly")
	case <-deadline:
		fmt.Println("[rung2] 30 s deadline reached — exiting (no tool call observed)")
	case <-ctx.Done():
		fmt.Println("[rung2] context cancelled — exiting")
	}

	elapsed := time.Since(startTime).Round(time.Millisecond)
	fmt.Printf("[rung2] end: %s  wall: %s\n", time.Now().UTC().Format(time.RFC3339), elapsed)
}
