// emit.go — chain-event emission helper for spec 097 subcommands.
//
// Wraps `chitin-kernel emit -event-json -` to write spec 097's new chain
// events (scheduler_started, scheduler_canceled) per constitution §1 (the
// kernel is the only chain writer; this binary shells out to it rather
// than writing to ~/.chitin/events-*.jsonl directly).
//
// Fail-soft per spec 097 research.md D8: chain-emit failure logs a warning
// to stderr but never propagates as a user-visible error. The schedule or
// cancel action already succeeded in Temporal; the audit chain lost an
// entry — operators can replay from Temporal history if forensic
// reconstruction is needed.

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"
)

// SchedulerStartedPayload is the spec 097 "scheduler_started" event payload
// (contracts/chain-events.md Event 1).
type SchedulerStartedPayload struct {
	SpecRef              string   `json:"spec_ref"`
	RunID                string   `json:"run_id"`
	NodeCount            int      `json:"node_count"`
	CapabilitiesRequired []string `json:"capabilities_required"`
}

// SchedulerCanceledPayload is the spec 097 "scheduler_canceled" event payload
// (contracts/chain-events.md Event 2).
type SchedulerCanceledPayload struct {
	RunID  string `json:"run_id"`
	Reason string `json:"reason"`
}

// emitSchedulerStarted writes a scheduler_started chain event via the
// kernel's emit subcommand. Returns nil unconditionally — emit failures
// log a warning and continue per D8.
func emitSchedulerStarted(ctx context.Context, payload SchedulerStartedPayload, stderr io.Writer) {
	emitChainEvent(ctx, "scheduler_started", payload, stderr)
}

// emitSchedulerCanceled writes a scheduler_canceled chain event. Same
// fail-soft contract as emitSchedulerStarted.
func emitSchedulerCanceled(ctx context.Context, payload SchedulerCanceledPayload, stderr io.Writer) {
	emitChainEvent(ctx, "scheduler_canceled", payload, stderr)
}

// emitChainEvent constructs a chain-event JSON record and pipes it to
// `chitin-kernel emit -event-json -`. Per D6 this uses the canonical kernel
// emit path so the chain stays single-write-seam even when the writer is
// a separate orchestrator binary.
//
// Kernel binary resolution: $CHITIN_KERNEL_BIN if set, else "chitin-kernel"
// (PATH lookup). Both forms are acceptable; the env var is the operator
// override for sandboxes and dev rigs.
//
// Failure modes — none propagate to the caller:
//   - $CHITIN_KERNEL_BIN points at a missing file: log warn, return.
//   - PATH lookup fails: log warn, return.
//   - JSON marshal of the envelope fails (basically impossible): log warn, return.
//   - emit subprocess exits non-zero: log warn (including stderr tail), return.
//   - emit subprocess hangs past 5 seconds: SIGKILL via ctx timeout, log warn, return.
func emitChainEvent(ctx context.Context, eventType string, payload any, stderr io.Writer) {
	binPath := os.Getenv("CHITIN_KERNEL_BIN")
	if binPath == "" {
		binPath = "chitin-kernel"
	}

	// Build the envelope. Field names match the existing chain event
	// schemas used by other emitters (openclaw plugin's emitStopSignalIgnored
	// follows the same shape).
	envelope := map[string]any{
		"event_type":         eventType,
		"agent_instance_id":  fmt.Sprintf("chitin-orchestrator-cli-%d", os.Getpid()),
		"session_id":         fmt.Sprintf("chitin-orchestrator-cli-%d", os.Getpid()),
		"ts":                 time.Now().UTC().Format(time.RFC3339Nano),
		"payload":            payload,
	}
	body, err := json.Marshal(envelope)
	if err != nil {
		warn(stderr, "chain emit failed: marshal: %v — %s succeeded; the audit chain lost this entry", err, eventType)
		return
	}

	// Hard cap the subprocess so a wedged kernel binary doesn't hang the CLI.
	emitCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(emitCtx, binPath, "emit", "-event-json", "-")
	cmd.Stdin = strings.NewReader(string(body))
	var stderrBuf strings.Builder
	cmd.Stderr = &stderrBuf
	if err := cmd.Run(); err != nil {
		tail := strings.TrimSpace(stderrBuf.String())
		if len(tail) > 200 {
			tail = tail[len(tail)-200:]
		}
		warn(stderr, "chain emit failed: %v (stderr: %s) — %s succeeded; the audit chain lost this entry", err, tail, eventType)
		return
	}
}

func warn(stderr io.Writer, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	if stderr != nil {
		fmt.Fprintln(stderr, "warning: "+msg)
	} else {
		log.Printf("warning: %s", msg)
	}
}
