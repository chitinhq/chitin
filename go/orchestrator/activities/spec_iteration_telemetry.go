package activities

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"go.temporal.io/sdk/temporal"
)

// emitSpecIterationInvalidInputErrorType is the stable Temporal application
// error type string for input-validation faults — kept as a constant so
// workflows that pattern-match on the error type don't drift if the
// human-readable message gets reworded.
const emitSpecIterationInvalidInputErrorType = "InvalidSpecIterationTelemetryInput"

// Spec 115 FR-009 chain-event taxonomy. Closed set — the activity rejects
// any other event_type so the linter (L04 in the same spec) can rely on
// the canonical names not drifting between the spec text and the emit
// surface.
const (
	SpecLintCompletedEvent           = "spec_lint_completed"
	SpecIterationRoundStartedEvent   = "spec_iteration_round_started"
	SpecIterationCompletedEvent      = "spec_iteration_completed"
	SpecIterationFailedEvent         = "spec_iteration_failed"
	SpecIterationEscalatedEvent      = "spec_iteration_escalated"
	SpecIterationSkippedEvent        = "spec_iteration_skipped"
	emitSpecIterationActivityName    = "EmitSpecIterationTelemetry"
	emitSpecIterationKernelTimeoutS  = 5
	emitSpecIterationChainType       = "spec-iteration"
	emitSpecIterationSurface         = "chitin-orchestrator"
	emitSpecIterationAgentInstanceID = "chitin-orchestrator"
	emitSpecIterationSchemaVersion   = "2"
)

// SpecRuleViolation is one entry in the spec_lint_completed payload's
// rule_violations array (FR-009). The field set matches the per-rule
// output of the deterministic spec linter (FR-003, T002–T009).
type SpecRuleViolation struct {
	Rule     string `json:"rule"`
	File     string `json:"file"`
	Line     int    `json:"line"`
	Severity string `json:"severity"`
}

// SpecIterationActionCounts is the action_counts sub-object on
// spec_iteration_completed (FR-009). Mirrors the verb taxonomy the
// driver applies per round: fix the spec, reply to a comment, skip a
// no-op comment, or patch the linter allowlist.
type SpecIterationActionCounts struct {
	Fix     int `json:"fix"`
	Reply   int `json:"reply"`
	Skip    int `json:"skip"`
	LintFix int `json:"lint_fix"`
}

// EmitSpecIterationTelemetryInput is the typed input to the spec-115
// telemetry emit activity. One activity invocation per FR-009 event;
// EventType picks which payload shape the activity assembles. Fields
// irrelevant to the chosen EventType are ignored — the assembler emits
// only the keys that event spec lists, so a stray field set on the wrong
// event doesn't leak into the chain entry.
type EmitSpecIterationTelemetryInput struct {
	// EventType is the FR-009 chain event being emitted; must be one of
	// the six canonical names. Unknown event types return a
	// non-retryable activity error so a misconfigured caller surfaces
	// rather than silently dropping audit.
	EventType string `json:"event_type"`
	// PRNumber is the spec PR the chain entry belongs to. Required on
	// every event — FR-009 lists it in every payload shape.
	PRNumber int `json:"pr_number"`
	// Round is the 1-based iteration round number. Set on
	// round_started, completed, failed. Ignored on the others.
	Round int `json:"round,omitempty"`
	// Reviewer names the reviewer whose comments seeded the round
	// (typically "copilot"). round_started only.
	Reviewer string `json:"reviewer,omitempty"`
	// CommentCount is how many Copilot line comments the round saw.
	// round_started only.
	CommentCount int `json:"comment_count,omitempty"`
	// LintViolationsCount is how many linter findings the round saw.
	// round_started only.
	LintViolationsCount int `json:"lint_violations_count,omitempty"`
	// RuleViolations is the linter's per-rule findings. spec_lint_completed
	// only; the encoder substitutes an empty array when nil so the chain
	// entry has a well-typed payload shape even on a clean spec.
	RuleViolations []SpecRuleViolation `json:"rule_violations,omitempty"`
	// FixupSHA is the new HEAD SHA after the driver's fixup commit.
	// completed only; empty when no fixup was pushed.
	FixupSHA string `json:"fixup_sha,omitempty"`
	// RepliesPosted is how many PR review replies the round posted.
	// completed only.
	RepliesPosted int `json:"replies_posted,omitempty"`
	// ActionCounts is the per-verb tally of how the round resolved each
	// comment. completed only.
	ActionCounts SpecIterationActionCounts `json:"action_counts,omitempty"`
	// FailureKind is the closed-taxonomy failure label (e.g.
	// "driver_fault", "push_failed"). failed only.
	FailureKind string `json:"failure_kind,omitempty"`
	// Detail is the human-readable explanation of the failure. failed
	// only — FR-009 escalated payloads carry the closed-taxonomy Reason
	// instead, so Detail is dropped from the escalated event by the
	// payload builder.
	Detail string `json:"detail,omitempty"`
	// RoundsAttempted is how many rounds the workflow ran before the
	// escalation triggered. escalated only.
	RoundsAttempted int `json:"rounds_attempted,omitempty"`
	// LastReviewID is the GitHub review id whose comments triggered the
	// escalating round. escalated only.
	LastReviewID int64 `json:"last_review_id,omitempty"`
	// Reason is the FR-010 closed-taxonomy reason string for an
	// escalation or skip (e.g. "design_judgement_required",
	// "lint_violation_unresolvable"). escalated + skipped.
	Reason string `json:"reason,omitempty"`
}

// EmitSpecIterationTelemetry is the spec-115 T016 chain-event emitter
// activity. One activity invocation per FR-009 event; the activity
// builds the canonical payload for the named EventType, marshals an
// envelope mirroring the spec-112/113 chain entries, and shells out to
// `chitin-kernel emit` via a temp file (same pattern as
// emitPRIterationEvent in pr_iteration.go and emitSiblingRebaseEvent in
// sibling_rebase.go).
//
// Hermetic-test contract: CHITIN_DISABLE_CHAIN_EMIT=1 short-circuits
// before any subprocess is launched, so workflow tests under Temporal's
// testsuite never depend on the kernel binary being present.
//
// Fail-soft for runtime faults: a missing kernel binary, marshal error,
// temp-file error, or non-zero kernel exit only logs a warning to
// stderr and returns a nil error. The workflow's load-bearing signal is
// the workflow result; the chain entry is supplementary audit.
//
// Input validation faults DO return a Temporal non-retryable
// application error (type "InvalidSpecIterationTelemetryInput") so a
// misconfigured caller surfaces rather than silently losing audit. The
// validator covers: unknown event_type, missing pr_number, and the
// per-event required-field invariants from FR-009 (round > 0 where
// applicable, non-empty failure_kind/detail/reason/reviewer, counts
// >= 0). Workflows calling this activity inherit the non-retryable
// contract from the SDK regardless of their RetryPolicy.
type EmitSpecIterationTelemetry struct{}

// NewEmitSpecIterationTelemetry returns a zero-value activity handle.
// The activity carries no startup-bound dependencies — kernel binary
// resolution + the disable-emit env var are read per-call.
func NewEmitSpecIterationTelemetry() *EmitSpecIterationTelemetry {
	return &EmitSpecIterationTelemetry{}
}

// ActivityName is the stable Temporal activity name. Workflows dispatch
// by this name via workflow.ExecuteActivity so there is no compile-time
// coupling between the workflow package and this one.
func (*EmitSpecIterationTelemetry) ActivityName() string {
	return emitSpecIterationActivityName
}

// Execute is the activity entrypoint. See the type doc for the failure
// contract; in short: input-shape errors return; runtime emit failures
// warn-and-return-nil.
func (a *EmitSpecIterationTelemetry) Execute(ctx context.Context, in EmitSpecIterationTelemetryInput) error {
	return emitSpecIterationChainEvent(ctx, in, os.Stderr)
}

// emitSpecIterationChainEvent does the actual work. Split from Execute
// so tests can inject a buffer for the warning sink and assert on its
// contents without racing the real os.Stderr.
func emitSpecIterationChainEvent(ctx context.Context, in EmitSpecIterationTelemetryInput, warnSink io.Writer) error {
	if in.PRNumber <= 0 {
		return temporal.NewNonRetryableApplicationError(
			fmt.Sprintf("spec-iteration telemetry: pr_number is required (got %d)", in.PRNumber),
			emitSpecIterationInvalidInputErrorType, nil)
	}
	if err := validateSpecIterationInput(in); err != nil {
		return err
	}

	payload, err := buildSpecIterationPayload(in)
	if err != nil {
		return err
	}

	// CHITIN_DISABLE_CHAIN_EMIT=1 honored AFTER input validation so a
	// misconfigured caller in a hermetic test still gets the loud
	// programming-error signal back.
	if os.Getenv("CHITIN_DISABLE_CHAIN_EMIT") == "1" {
		return nil
	}

	envelope := map[string]any{
		"schema_version":    emitSpecIterationSchemaVersion,
		"event_type":        in.EventType,
		"run_id":            fmt.Sprintf("spec-iteration-pr-%d", in.PRNumber),
		"session_id":        fmt.Sprintf("chitin-orchestrator-spec-iterate-%d", in.PRNumber),
		"surface":           emitSpecIterationSurface,
		"agent_instance_id": emitSpecIterationAgentInstanceID,
		"chain_type":        emitSpecIterationChainType,
		"ts":                time.Now().UTC().Format(time.RFC3339Nano),
		"payload":           payload,
	}
	body, err := json.Marshal(envelope)
	if err != nil {
		warnSpecIteration(warnSink, "marshal: %v — %s recorded only in workflow result", err, in.EventType)
		return nil
	}

	tmp, err := os.CreateTemp("", "chitin-spec-iterate-emit-*.json")
	if err != nil {
		warnSpecIteration(warnSink, "temp file: %v — %s recorded only in workflow result", err, in.EventType)
		return nil
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.Write(body); err != nil {
		tmp.Close()
		warnSpecIteration(warnSink, "temp write: %v — %s recorded only in workflow result", err, in.EventType)
		return nil
	}
	if err := tmp.Close(); err != nil {
		warnSpecIteration(warnSink, "temp close: %v — %s recorded only in workflow result", err, in.EventType)
		return nil
	}

	binPath := os.Getenv("CHITIN_KERNEL_BIN")
	if binPath == "" {
		binPath = "chitin-kernel"
	}
	chitinDir := os.Getenv("CHITIN_DIR")
	if chitinDir == "" {
		if home, err := os.UserHomeDir(); err == nil {
			chitinDir = home + "/.chitin"
		} else {
			chitinDir = ".chitin"
		}
	}

	emitCtx, cancel := context.WithTimeout(ctx, emitSpecIterationKernelTimeoutS*time.Second)
	defer cancel()
	cmd := exec.CommandContext(emitCtx, binPath, "emit", "-dir", chitinDir, "-event-file", tmpPath)
	var stderrBuf strings.Builder
	cmd.Stderr = &stderrBuf
	if err := cmd.Run(); err != nil {
		tail := strings.TrimSpace(stderrBuf.String())
		if len(tail) > 200 {
			tail = tail[len(tail)-200:]
		}
		warnSpecIteration(warnSink, "kernel emit failed: %v (stderr: %s) — %s recorded only in workflow result", err, tail, in.EventType)
	}
	return nil
}

// buildSpecIterationPayload assembles the per-event payload map per
// FR-009. Each event lists exactly the keys named in its spec entry —
// fields belonging to other events are NOT injected, so a stray field
// set on the wrong event doesn't pollute the chain entry. Returns a
// non-retryable activity error when EventType is outside the closed set
// so a misconfigured workflow loud-fails rather than silently dropping
// audit.
func buildSpecIterationPayload(in EmitSpecIterationTelemetryInput) (map[string]any, error) {
	switch in.EventType {
	case SpecLintCompletedEvent:
		// Canonicalize nil → empty array so the chain entry's payload
		// shape is stable (downstream consumers can rely on
		// rule_violations being an array, not null).
		violations := in.RuleViolations
		if violations == nil {
			violations = []SpecRuleViolation{}
		}
		return map[string]any{
			"pr_number":       in.PRNumber,
			"rule_violations": violations,
		}, nil
	case SpecIterationRoundStartedEvent:
		return map[string]any{
			"pr_number":             in.PRNumber,
			"round":                 in.Round,
			"reviewer":              in.Reviewer,
			"comment_count":         in.CommentCount,
			"lint_violations_count": in.LintViolationsCount,
		}, nil
	case SpecIterationCompletedEvent:
		return map[string]any{
			"pr_number":      in.PRNumber,
			"round":          in.Round,
			"fixup_sha":      in.FixupSHA,
			"replies_posted": in.RepliesPosted,
			"action_counts": map[string]int{
				"fix":      in.ActionCounts.Fix,
				"reply":    in.ActionCounts.Reply,
				"skip":     in.ActionCounts.Skip,
				"lint_fix": in.ActionCounts.LintFix,
			},
		}, nil
	case SpecIterationFailedEvent:
		return map[string]any{
			"pr_number":    in.PRNumber,
			"round":        in.Round,
			"failure_kind": in.FailureKind,
			"detail":       in.Detail,
		}, nil
	case SpecIterationEscalatedEvent:
		return map[string]any{
			"pr_number":        in.PRNumber,
			"rounds_attempted": in.RoundsAttempted,
			"last_review_id":   in.LastReviewID,
			"reason":           in.Reason,
		}, nil
	case SpecIterationSkippedEvent:
		return map[string]any{
			"pr_number": in.PRNumber,
			"reason":    in.Reason,
		}, nil
	default:
		return nil, temporal.NewNonRetryableApplicationError(
			fmt.Sprintf("spec-iteration telemetry: unknown event_type %q (expected one of: %s)",
				in.EventType, strings.Join(SpecIterationEventTypes(), ", ")),
			emitSpecIterationInvalidInputErrorType, nil)
	}
}

// validateSpecIterationInput enforces the per-event required-field
// invariants from FR-009. The type doc already calls a misconfigured
// caller a programming error; this guard converts the silent-truncation
// behavior (a payload missing required fields would still emit) into a
// loud non-retryable failure so the misuse surfaces in the workflow
// result. Counts are checked for non-negativity defensively — Go ints
// can carry negative values, and a negative count in the chain would
// poison downstream aggregations.
func validateSpecIterationInput(in EmitSpecIterationTelemetryInput) error {
	fail := func(msg string) error {
		return temporal.NewNonRetryableApplicationError(
			"spec-iteration telemetry: "+msg,
			emitSpecIterationInvalidInputErrorType, nil)
	}
	switch in.EventType {
	case SpecLintCompletedEvent:
		// pr_number already checked in the caller; rule_violations is
		// canonicalized from nil to [] by the payload builder, so there
		// is nothing else to require here.
		return nil
	case SpecIterationRoundStartedEvent:
		if in.Round <= 0 {
			return fail(fmt.Sprintf("round must be > 0 for %s (got %d)", in.EventType, in.Round))
		}
		if in.Reviewer == "" {
			return fail(fmt.Sprintf("reviewer is required for %s", in.EventType))
		}
		if in.CommentCount < 0 || in.LintViolationsCount < 0 {
			return fail(fmt.Sprintf("comment_count and lint_violations_count must be >= 0 for %s (got %d, %d)",
				in.EventType, in.CommentCount, in.LintViolationsCount))
		}
		return nil
	case SpecIterationCompletedEvent:
		if in.Round <= 0 {
			return fail(fmt.Sprintf("round must be > 0 for %s (got %d)", in.EventType, in.Round))
		}
		if in.RepliesPosted < 0 {
			return fail(fmt.Sprintf("replies_posted must be >= 0 for %s (got %d)", in.EventType, in.RepliesPosted))
		}
		if in.ActionCounts.Fix < 0 || in.ActionCounts.Reply < 0 ||
			in.ActionCounts.Skip < 0 || in.ActionCounts.LintFix < 0 {
			return fail(fmt.Sprintf("action_counts must be >= 0 for %s (got %+v)", in.EventType, in.ActionCounts))
		}
		return nil
	case SpecIterationFailedEvent:
		if in.Round <= 0 {
			return fail(fmt.Sprintf("round must be > 0 for %s (got %d)", in.EventType, in.Round))
		}
		if in.FailureKind == "" {
			return fail(fmt.Sprintf("failure_kind is required for %s", in.EventType))
		}
		if in.Detail == "" {
			return fail(fmt.Sprintf("detail is required for %s", in.EventType))
		}
		return nil
	case SpecIterationEscalatedEvent:
		if in.RoundsAttempted <= 0 {
			return fail(fmt.Sprintf("rounds_attempted must be > 0 for %s (got %d)", in.EventType, in.RoundsAttempted))
		}
		if in.Reason == "" {
			return fail(fmt.Sprintf("reason is required for %s", in.EventType))
		}
		return nil
	case SpecIterationSkippedEvent:
		if in.Reason == "" {
			return fail(fmt.Sprintf("reason is required for %s", in.EventType))
		}
		return nil
	default:
		// Unknown event_type is rejected by buildSpecIterationPayload;
		// the caller invokes that step right after this one, so let
		// that single source-of-truth check handle the listing of
		// canonical names.
		return nil
	}
}

// SpecIterationEventTypes returns the closed-set list of canonical
// FR-009 event names. Exported so the spec linter (L04, T006) and tests
// can iterate the same source-of-truth set rather than maintaining a
// parallel copy.
func SpecIterationEventTypes() []string {
	return []string{
		SpecLintCompletedEvent,
		SpecIterationRoundStartedEvent,
		SpecIterationCompletedEvent,
		SpecIterationFailedEvent,
		SpecIterationEscalatedEvent,
		SpecIterationSkippedEvent,
	}
}

// warnSpecIteration logs a chain-emit warning to the configured sink.
// Mirrors warnIteration in pr_iteration.go — goes to stderr by default
// so the worker host's journald entry captures it; the round outcome
// never depends on it.
func warnSpecIteration(sink io.Writer, format string, args ...any) {
	if sink == nil {
		return
	}
	fmt.Fprintf(sink, "warning: spec-iteration chain emit: "+format+"\n", args...)
}
