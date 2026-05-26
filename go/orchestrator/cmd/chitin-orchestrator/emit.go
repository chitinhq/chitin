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
	// Mode is the spec 119 FR-010 closed-taxonomy dispatch mode:
	// "whole-spec" or "per-task". Chain consumers (spec 114 queue,
	// spec 118 silent-drop detector) tolerate the new field via
	// omitempty — older payloads decoded against the new struct still
	// reconstruct.
	Mode string `json:"mode,omitempty"`
	// WholeSpecTaskCount is the count of unchecked tasks the whole-spec
	// driver is being asked to deliver. Zero in per-task mode (where the
	// dispatch is granular and the per-task count lives in NodeCount).
	WholeSpecTaskCount int `json:"whole_spec_task_count,omitempty"`
}

// SchedulerCanceledPayload is the spec 097 "scheduler_canceled" event payload
// (contracts/chain-events.md Event 2).
type SchedulerCanceledPayload struct {
	RunID  string `json:"run_id"`
	Reason string `json:"reason"`
}

// CopilotDispatchedPayload is the spec 099 "copilot_dispatched" event
// payload (specs/099-github-native-dispatch/contracts/chain-events.md
// Event 1). Emitted by `chitin-orchestrator schedule --driver copilot`
// after the GitHub issue is successfully created.
type CopilotDispatchedPayload struct {
	Repo         string `json:"repo"`
	SpecRef      string `json:"spec_ref"`
	IssueURL     string `json:"issue_url"`
	IssueNumber  int    `json:"issue_number"`
	DispatchedAt string `json:"dispatched_at"`
}

// CopilotPRActivityPayload is the spec 099 "copilot_pr_activity" event
// payload (specs/099-github-native-dispatch/contracts/chain-events.md
// Event 5). FR-013 telemetry stream: every pull_request.* /
// pull_request_review.* / issue_comment.created webhook for a PR
// carrying `chitin-dispatch` emits one of these regardless of
// eligibility — the deliberate partial-recovery of telemetry we lose
// by dispatching off-machine.
type CopilotPRActivityPayload struct {
	Repo        string          `json:"repo"`
	PRNumber    int             `json:"pr_number"`
	EventType   string          `json:"event_type"`
	EventAction string          `json:"event_action"`
	DeliveryID  string          `json:"delivery_id"`
	Payload     json.RawMessage `json:"payload"`
	ReceivedAt  string          `json:"received_at"`
}

// CopilotPRDetectedPayload is the spec 099 "copilot_pr_detected" event
// payload (contracts/chain-events.md Event 2). Emitted by the
// factory-listen /webhook/pr handler on the first eligible PR event
// per (repo, pr_number) — at most one per PR per FR-008 (dedup
// enforced by chain query before emit).
type CopilotPRDetectedPayload struct {
	Repo                string `json:"repo"`
	PRNumber            int    `json:"pr_number"`
	PRURL               string `json:"pr_url"`
	SpecRef             string `json:"spec_ref"` // "unknown" if Closes ref not recoverable
	IssueNumber         int    `json:"issue_number"`
	Commits             int    `json:"commits"`
	Additions           int    `json:"additions"`
	Deletions           int    `json:"deletions"`
	ChangedFiles        int    `json:"changed_files"`
	DetectedAt          string `json:"detected_at"`
	ReviewWorkflowRunID string `json:"review_workflow_run_id,omitempty"`
}

// CopilotReviewFailedPayload is the spec 099 "copilot_review_failed"
// event payload (contracts/chain-events.md Event 4). Emitted when the
// PRReviewWorkflow start fails synchronously (e.g. Temporal
// unreachable). Workflow-runtime failures are emitted by the workflow
// itself in a follow-up slice.
type CopilotReviewFailedPayload struct {
	Repo         string `json:"repo"`
	PRNumber     int    `json:"pr_number"`
	ReviewRunID  string `json:"review_run_id"`
	FailureKind  string `json:"failure_kind"`
	Detail       string `json:"detail"`
	FailedAt     string `json:"failed_at"`
}

// emitSchedulerStarted writes a scheduler_started chain event via the
// kernel's emit subcommand. The workflowRunID is used as the chain RunID
// so the resulting events-*.jsonl filename matches the Temporal RunID
// and the chain entry can be joined to the workflow by id.
// Returns nil unconditionally — emit failures log a warning and continue
// per D8.
func emitSchedulerStarted(ctx context.Context, payload SchedulerStartedPayload, stderr io.Writer) {
	emitChainEvent(ctx, "scheduler_started", payload.RunID, payload, stderr)
}

// emitSchedulerCanceled writes a scheduler_canceled chain event. The
// workflowRunID is sourced from the payload. Same fail-soft contract as
// emitSchedulerStarted.
func emitSchedulerCanceled(ctx context.Context, payload SchedulerCanceledPayload, stderr io.Writer) {
	emitChainEvent(ctx, "scheduler_canceled", payload.RunID, payload, stderr)
}

// emitCopilotDispatched writes a copilot_dispatched chain event for the
// spec 099 dispatch path. The workflowRunID is empty — Copilot dispatches
// don't have a Temporal SchedulerWorkflow (that's the whole point of
// the off-machine path); operators correlate by (repo, issue_number).
// Same fail-soft contract as emitSchedulerStarted.
func emitCopilotDispatched(ctx context.Context, payload CopilotDispatchedPayload, stderr io.Writer) {
	emitChainEvent(ctx, "copilot_dispatched", "", payload, stderr)
}

// emitCopilotPRActivity writes a copilot_pr_activity chain event
// (FR-013 PR-level telemetry stream). workflowRunID is empty — the
// activity stream is per-webhook-delivery, not tied to a workflow.
// Same fail-soft contract as emitSchedulerStarted.
func emitCopilotPRActivity(ctx context.Context, payload CopilotPRActivityPayload, stderr io.Writer) {
	emitChainEvent(ctx, "copilot_pr_activity", "", payload, stderr)
}

// emitCopilotPRDetected writes a copilot_pr_detected chain event
// (contracts/chain-events.md Event 2). The workflowRunID is the
// PRReviewWorkflow's Temporal run ID so the chain entry can be joined
// to the workflow in audit replay.
func emitCopilotPRDetected(ctx context.Context, payload CopilotPRDetectedPayload, stderr io.Writer) {
	emitChainEvent(ctx, "copilot_pr_detected", payload.ReviewWorkflowRunID, payload, stderr)
}

// emitCopilotReviewFailed writes a copilot_review_failed chain event
// (contracts/chain-events.md Event 4). Synchronous start failures
// only; workflow runtime failures land via the workflow's own emit
// (slice 5).
func emitCopilotReviewFailed(ctx context.Context, payload CopilotReviewFailedPayload, stderr io.Writer) {
	emitChainEvent(ctx, "copilot_review_failed", payload.ReviewRunID, payload, stderr)
}

// emitChainEvent constructs a chain-event JSON record, writes it to a
// temp file, and invokes `chitin-kernel emit -event-file <path>`. Per
// D6 this uses the canonical kernel emit path so the chain stays
// single-write-seam even when the writer is a separate orchestrator
// binary.
//
// We use a temp file rather than stdin because the chitin-kernel emit
// subcommand accepts a `-event-file <path>` flag, not `-event-json -`
// (verified against the live binary 2026-05-23). Either form would work
// for the chain — the file path is what the kernel exposes today.
//
// Kernel binary resolution: $CHITIN_KERNEL_BIN if set, else "chitin-kernel"
// (PATH lookup).
//
// Failure modes — none propagate to the caller (per D8 — chain emit
// failure must not block the load-bearing schedule/cancel action):
//   - $CHITIN_KERNEL_BIN points at a missing file: log warn, return.
//   - PATH lookup fails: log warn, return.
//   - Temp file write fails: log warn, return.
//   - emit subprocess exits non-zero: log warn (with stderr tail), return.
//   - emit subprocess hangs past 5 seconds: SIGKILL via ctx timeout, log warn, return.
func emitChainEvent(ctx context.Context, eventType string, workflowRunID string, payload any, stderr io.Writer) {
	binPath := os.Getenv("CHITIN_KERNEL_BIN")
	if binPath == "" {
		binPath = "chitin-kernel"
	}

	// Build a v2 Event-shaped envelope. The kernel writes events to
	// events-<run_id>.jsonl, so setting run_id to the Temporal WorkflowID
	// makes the chain entry findable by the workflow's identifier. The
	// chain framing (chain_id, prev_hash, this_hash, seq) is populated by
	// the kernel's emit.Emitter; we leave those empty and let the kernel
	// compute them via its chain index.
	pid := os.Getpid()
	agentID := fmt.Sprintf("chitin-orchestrator-cli-%d", pid)
	envelope := map[string]any{
		"schema_version":    "2",
		"event_type":        eventType,
		"run_id":            workflowRunID,
		"session_id":        agentID,
		"surface":           "chitin-orchestrator",
		"agent_instance_id": agentID,
		"chain_type":        "operator-cli",
		"ts":                time.Now().UTC().Format(time.RFC3339Nano),
		"payload":           payload,
	}
	body, err := json.Marshal(envelope)
	if err != nil {
		warn(stderr, "chain emit failed: marshal: %v — %s succeeded; the audit chain lost this entry", err, eventType)
		return
	}

	// Write the event JSON to a temp file (the kernel reads from disk).
	tmpFile, err := os.CreateTemp("", "chitin-orch-emit-*.json")
	if err != nil {
		warn(stderr, "chain emit failed: create temp file: %v — %s succeeded; the audit chain lost this entry", err, eventType)
		return
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)
	if _, err := tmpFile.Write(body); err != nil {
		tmpFile.Close()
		warn(stderr, "chain emit failed: write temp file: %v — %s succeeded; the audit chain lost this entry", err, eventType)
		return
	}
	if err := tmpFile.Close(); err != nil {
		warn(stderr, "chain emit failed: close temp file: %v — %s succeeded; the audit chain lost this entry", err, eventType)
		return
	}

	// Hard cap the subprocess so a wedged kernel binary doesn't hang the CLI.
	emitCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	// -dir points at ~/.chitin explicitly. The kernel defaults `dir` to
	// the relative path ".chitin"; that resolves correctly only when the
	// kernel is invoked from ~/. The orchestrator CLI may be invoked from
	// any cwd (operator's worktree, a different repo, etc.) so we pin
	// the kernel's chain state dir to its canonical $HOME/.chitin
	// location. CHITIN_DIR overrides this for sandboxes.
	chitinDir := os.Getenv("CHITIN_DIR")
	if chitinDir == "" {
		if home, err := os.UserHomeDir(); err == nil {
			chitinDir = home + "/.chitin"
		} else {
			chitinDir = ".chitin"
		}
	}
	cmd := exec.CommandContext(emitCtx, binPath, "emit", "-dir", chitinDir, "-event-file", tmpPath)
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
