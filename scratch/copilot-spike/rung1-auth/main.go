// Rung 1 auth probe: authenticate with the Copilot CLI SDK and complete
// one round-trip to the Copilot backend.
//
// Invariant: if this program exits 0, the SDK spawned the CLI subprocess,
// negotiated JSON-RPC, created a session with valid credentials, sent one
// prompt, and received a non-empty assistant reply — one full auth round-trip.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	copilot "github.com/github/copilot-sdk/go"
)

func main() {
	startTime := time.Now()
	fmt.Printf("[rung1] start: %s\n", startTime.UTC().Format(time.RFC3339))

	cliPath := os.Getenv("COPILOT_CLI_PATH")
	if cliPath == "" {
		// Known location on this box from `which copilot`
		cliPath = "/home/red/.vite-plus/bin/copilot"
	}
	fmt.Printf("[rung1] cli-path: %s\n", cliPath)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	client := copilot.NewClient(&copilot.ClientOptions{
		CLIPath:  cliPath,
		LogLevel: "error",
	})

	if err := client.Start(ctx); err != nil {
		log.Fatalf("[rung1] FAIL — client.Start: %v", err)
	}
	defer func() {
		if err := client.Stop(); err != nil {
			fmt.Fprintf(os.Stderr, "[rung1] client.Stop warning: %v\n", err)
		}
	}()
	fmt.Println("[rung1] client started (CLI subprocess running)")

	session, err := client.CreateSession(ctx, &copilot.SessionConfig{
		Model:               "gpt-4.1",
		OnPermissionRequest: copilot.PermissionHandler.ApproveAll,
	})
	if err != nil {
		log.Fatalf("[rung1] FAIL — CreateSession: %v", err)
	}
	defer session.Disconnect() //nolint:errcheck
	fmt.Println("[rung1] session created")

	// Single lowest-overhead round-trip: a one-word prompt.
	response, err := session.SendAndWait(ctx, copilot.MessageOptions{
		Prompt: "Reply with exactly: AUTH_OK",
	})
	if err != nil {
		log.Fatalf("[rung1] FAIL — SendAndWait: %v", err)
	}

	if response == nil {
		log.Fatal("[rung1] FAIL — response is nil")
	}

	switch d := response.Data.(type) {
	case *copilot.AssistantMessageData:
		fmt.Printf("[rung1] PASS — response: %q\n", d.Content)
	default:
		fmt.Printf("[rung1] PASS — response type: %T  data: %+v\n", response.Data, response.Data)
	}

	elapsed := time.Since(startTime).Round(time.Millisecond)
	fmt.Printf("[rung1] end: %s  wall: %s\n", time.Now().UTC().Format(time.RFC3339), elapsed)
}
