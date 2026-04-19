// chitin-kernel reads a Claude Code (or compatible) PreToolUse payload on
// stdin, normalizes it to a canonical Event, and appends it to the local
// JSONL ground truth. Phase 1 is monitor-only — the binary always exits 0.
package main

import (
	"fmt"
	"os"
	"time"

	"github.com/chitinhq/chitin/go/execution-kernel/internal/canon"
	"github.com/chitinhq/chitin/go/execution-kernel/internal/emit"
	"github.com/chitinhq/chitin/go/execution-kernel/internal/event"
	"github.com/chitinhq/chitin/go/execution-kernel/internal/hook"
	"github.com/chitinhq/chitin/go/execution-kernel/internal/normalize"

	"github.com/google/uuid"
)

func main() {
	// Always exit 0 — monitor-only. Never block the agent loop.
	defer os.Exit(0)

	start := time.Now()

	in, err := hook.ReadClaudeInput()
	if err != nil {
		fmt.Fprintf(os.Stderr, "chitin-kernel: %v\n", err)
		return
	}

	// Phase 1 only records PreToolUse events.
	if in.Event != "PreToolUse" {
		return
	}

	workspace := os.Getenv("CHITIN_WORKSPACE")
	if workspace == "" {
		cwd, _ := os.Getwd()
		workspace = cwd
	}

	runID := os.Getenv("CHITIN_RUN_ID")
	if runID == "" {
		runID = uuid.NewString()
	}

	sessionID := in.SessionID
	if sessionID == "" {
		sessionID = runID
	}

	action := normalize.Normalize(in.Tool, in.Input)

	canonicalForm := buildCanonicalForm(action)

	ev := event.Event{
		RunID:         runID,
		SessionID:     sessionID,
		Surface:       "claude-code",
		Driver:        "claude",
		AgentID:       os.Getenv("CHITIN_AGENT_ID"),
		ToolName:      in.Tool,
		RawInput:      in.Input,
		CanonicalForm: canonicalForm,
		ActionType:    event.ActionType(action.Type),
		Result:        event.ResultSuccess, // Phase 1: monitor-only, always success.
		DurationMs:    time.Since(start).Milliseconds(),
		Error:         nil,
		TS:            time.Now().UTC(),
		Metadata:      map[string]any{},
	}

	if err := emit.AppendEvent(workspace, ev); err != nil {
		fmt.Fprintf(os.Stderr, "chitin-kernel: emit: %v\n", err)
	}
}

// buildCanonicalForm derives the canonical_form map from a normalized Action.
// For shell commands (Bash), it runs canon.Parse and emits a pipeline. For
// other tools, it emits a minimal shape with tool + path.
func buildCanonicalForm(a *normalize.Action) map[string]any {
	if a.Command != "" {
		p := canon.Parse(a.Command)
		segs := make([]map[string]any, len(p.Segments))
		for i, s := range p.Segments {
			segs[i] = map[string]any{
				"op":     string(s.Op),
				"tool":   s.Command.Tool,
				"action": s.Command.Action,
				"flags":  s.Command.Flags,
				"args":   s.Command.Args,
				"digest": s.Command.Digest,
			}
		}
		return map[string]any{"segments": segs}
	}
	return map[string]any{
		"tool": a.Tool,
		"path": a.Path,
	}
}
