package activities

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeSpecKernelBin writes a tiny bash script standing in for
// chitin-kernel. The script extracts the value of `-event-file <path>`
// from argv, copies that JSON to a sentinel path the test can inspect,
// and exits with the given code. Mirrors the helper in
// cmd/chitin-orchestrator/emit_test.go but defined here so the activity
// tests don't reach across packages.
func fakeSpecKernelBin(t *testing.T, exitCode int) (binPath, sentinelPath string) {
	t.Helper()
	dir := t.TempDir()
	binPath = filepath.Join(dir, "chitin-kernel")
	sentinelPath = filepath.Join(dir, "captured.json")
	exit := "0"
	if exitCode != 0 {
		exit = itoaPositive(exitCode)
	}
	script := "#!/usr/bin/env bash\n" +
		"set -e\n" +
		"event_file=\"\"\n" +
		"while [[ $# -gt 0 ]]; do\n" +
		"  case \"$1\" in\n" +
		"    -event-file) event_file=\"$2\"; shift 2 ;;\n" +
		"    *) shift ;;\n" +
		"  esac\n" +
		"done\n" +
		"if [[ -n \"$event_file\" ]]; then\n" +
		"  cp \"$event_file\" " + sentinelPath + "\n" +
		"fi\n" +
		"exit " + exit + "\n"
	if err := os.WriteFile(binPath, []byte(script), 0o755); err != nil {
		t.Fatalf("setup fake kernel: %v", err)
	}
	return binPath, sentinelPath
}

// itoaPositive renders a non-negative int as decimal without pulling in
// strconv (keeps the test file lean and parallels the activities/
// package's existing util style).
func itoaPositive(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

// captureEmit shells the activity through the fake kernel and returns
// the decoded chain envelope written to the sentinel. Centralizes the
// per-event setup so each table case stays focused on assertions on
// the payload shape.
func captureEmit(t *testing.T, in EmitSpecIterationTelemetryInput) map[string]any {
	t.Helper()
	bin, sentinel := fakeSpecKernelBin(t, 0)
	t.Setenv("CHITIN_KERNEL_BIN", bin)
	t.Setenv("CHITIN_DIR", t.TempDir())
	// Ensure no inherited disable flag (parallel tests share env).
	t.Setenv("CHITIN_DISABLE_CHAIN_EMIT", "0")
	var warn bytes.Buffer
	if err := emitSpecIterationChainEvent(context.Background(), in, &warn); err != nil {
		t.Fatalf("emit returned err: %v", err)
	}
	if warn.Len() > 0 {
		t.Fatalf("expected silent emit on success, got stderr: %s", warn.String())
	}
	raw, err := os.ReadFile(sentinel)
	if err != nil {
		t.Fatalf("reading sentinel: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, raw)
	}
	return got
}

// TestEmitSpecIteration_LintCompleted_PayloadShape asserts the
// spec_lint_completed event carries pr_number + rule_violations[]
// exactly as FR-009 specifies, and that a nil violations slice
// canonicalizes to an empty array (not null) so downstream consumers
// can rely on the array shape.
func TestEmitSpecIteration_LintCompleted_PayloadShape(t *testing.T) {
	env := captureEmit(t, EmitSpecIterationTelemetryInput{
		EventType: SpecLintCompletedEvent,
		PRNumber:  1234,
		RuleViolations: []SpecRuleViolation{
			{Rule: "L02", File: "spec.md", Line: 17, Severity: "error"},
			{Rule: "L05", File: "spec.md", Line: 78, Severity: "warning"},
		},
	})
	if env["event_type"] != "spec_lint_completed" {
		t.Errorf("event_type = %v, want spec_lint_completed", env["event_type"])
	}
	if env["chain_type"] != "spec-iteration" {
		t.Errorf("chain_type = %v, want spec-iteration", env["chain_type"])
	}
	p := payloadMap(t, env)
	if p["pr_number"] != float64(1234) {
		t.Errorf("payload.pr_number = %v", p["pr_number"])
	}
	violations, ok := p["rule_violations"].([]any)
	if !ok {
		t.Fatalf("rule_violations not an array: %T", p["rule_violations"])
	}
	if len(violations) != 2 {
		t.Fatalf("expected 2 violations, got %d", len(violations))
	}
	first := violations[0].(map[string]any)
	if first["rule"] != "L02" || first["file"] != "spec.md" || first["line"] != float64(17) || first["severity"] != "error" {
		t.Errorf("violation[0] = %+v", first)
	}
}

// TestEmitSpecIteration_LintCompleted_NilViolationsBecomesEmptyArray
// asserts the canonical-shape contract: a spec with no violations emits
// rule_violations: [] rather than rule_violations: null. Downstream
// consumers (e.g. the operator queue digest) shouldn't have to
// type-switch on nullity to count violations.
func TestEmitSpecIteration_LintCompleted_NilViolationsBecomesEmptyArray(t *testing.T) {
	env := captureEmit(t, EmitSpecIterationTelemetryInput{
		EventType:      SpecLintCompletedEvent,
		PRNumber:       42,
		RuleViolations: nil,
	})
	p := payloadMap(t, env)
	violations, ok := p["rule_violations"].([]any)
	if !ok {
		t.Fatalf("rule_violations should be an array, got %T (value: %v)", p["rule_violations"], p["rule_violations"])
	}
	if len(violations) != 0 {
		t.Errorf("expected empty array, got %d entries", len(violations))
	}
}

// TestEmitSpecIteration_RoundStarted_PayloadShape asserts the
// round_started event carries all five FR-009 fields and that the
// reviewer string + comment counters round-trip through JSON cleanly.
func TestEmitSpecIteration_RoundStarted_PayloadShape(t *testing.T) {
	env := captureEmit(t, EmitSpecIterationTelemetryInput{
		EventType:           SpecIterationRoundStartedEvent,
		PRNumber:            7,
		Round:               2,
		Reviewer:            "copilot",
		CommentCount:        5,
		LintViolationsCount: 3,
	})
	p := payloadMap(t, env)
	if p["round"] != float64(2) {
		t.Errorf("payload.round = %v", p["round"])
	}
	if p["reviewer"] != "copilot" {
		t.Errorf("payload.reviewer = %v", p["reviewer"])
	}
	if p["comment_count"] != float64(5) {
		t.Errorf("payload.comment_count = %v", p["comment_count"])
	}
	if p["lint_violations_count"] != float64(3) {
		t.Errorf("payload.lint_violations_count = %v", p["lint_violations_count"])
	}
}

// TestEmitSpecIteration_Completed_PayloadShape asserts the completed
// event carries the nested action_counts object exactly as FR-009
// specifies (fix/reply/skip/lint_fix) — the operator queue's digest
// rendering keys off these names.
func TestEmitSpecIteration_Completed_PayloadShape(t *testing.T) {
	env := captureEmit(t, EmitSpecIterationTelemetryInput{
		EventType:     SpecIterationCompletedEvent,
		PRNumber:      99,
		Round:         1,
		FixupSHA:      "deadbeef1234",
		RepliesPosted: 2,
		ActionCounts: SpecIterationActionCounts{
			Fix:     4,
			Reply:   2,
			Skip:    1,
			LintFix: 3,
		},
	})
	p := payloadMap(t, env)
	if p["fixup_sha"] != "deadbeef1234" {
		t.Errorf("payload.fixup_sha = %v", p["fixup_sha"])
	}
	if p["replies_posted"] != float64(2) {
		t.Errorf("payload.replies_posted = %v", p["replies_posted"])
	}
	ac, ok := p["action_counts"].(map[string]any)
	if !ok {
		t.Fatalf("action_counts not an object: %T", p["action_counts"])
	}
	checks := map[string]float64{
		"fix":      4,
		"reply":    2,
		"skip":     1,
		"lint_fix": 3,
	}
	for k, want := range checks {
		if ac[k] != want {
			t.Errorf("action_counts.%s = %v, want %v", k, ac[k], want)
		}
	}
}

// TestEmitSpecIteration_Failed_PayloadShape asserts the failed event
// carries failure_kind + detail and the round number — operator
// triage needs both the closed-taxonomy label and the human detail.
func TestEmitSpecIteration_Failed_PayloadShape(t *testing.T) {
	env := captureEmit(t, EmitSpecIterationTelemetryInput{
		EventType:   SpecIterationFailedEvent,
		PRNumber:    300,
		Round:       3,
		FailureKind: "driver_fault",
		Detail:      "claudecode subprocess exited with code 137",
	})
	p := payloadMap(t, env)
	if p["round"] != float64(3) {
		t.Errorf("payload.round = %v", p["round"])
	}
	if p["failure_kind"] != "driver_fault" {
		t.Errorf("payload.failure_kind = %v", p["failure_kind"])
	}
	if !strings.Contains(p["detail"].(string), "code 137") {
		t.Errorf("payload.detail = %v", p["detail"])
	}
}

// TestEmitSpecIteration_Escalated_PayloadShape asserts the escalated
// event carries the three correlation fields the operator queue uses
// to surface the escalation (rounds_attempted, last_review_id, reason
// from the FR-010 closed taxonomy).
func TestEmitSpecIteration_Escalated_PayloadShape(t *testing.T) {
	env := captureEmit(t, EmitSpecIterationTelemetryInput{
		EventType:       SpecIterationEscalatedEvent,
		PRNumber:        555,
		RoundsAttempted: 2,
		LastReviewID:    9876543210,
		Reason:          "design_judgement_required",
	})
	p := payloadMap(t, env)
	if p["rounds_attempted"] != float64(2) {
		t.Errorf("payload.rounds_attempted = %v", p["rounds_attempted"])
	}
	if p["last_review_id"] != float64(9876543210) {
		t.Errorf("payload.last_review_id = %v", p["last_review_id"])
	}
	if p["reason"] != "design_judgement_required" {
		t.Errorf("payload.reason = %v", p["reason"])
	}
}

// TestEmitSpecIteration_Skipped_PayloadShape asserts the skipped event
// has the minimal shape FR-009 lists — pr_number + reason — and that
// fields irrelevant to the skipped event don't leak into the payload.
func TestEmitSpecIteration_Skipped_PayloadShape(t *testing.T) {
	env := captureEmit(t, EmitSpecIterationTelemetryInput{
		EventType: SpecIterationSkippedEvent,
		PRNumber:  88,
		// Stray fields that should NOT appear in the skipped payload:
		Round:        99,
		FailureKind:  "ignored_at_skipped",
		FixupSHA:     "should_not_appear",
		CommentCount: 7,
		// Skipped events only carry pr_number + reason per FR-009.
		Reason: "human_reviewer_present",
	})
	p := payloadMap(t, env)
	if p["reason"] != "human_reviewer_present" {
		t.Errorf("payload.reason = %v", p["reason"])
	}
	for _, k := range []string{"round", "failure_kind", "fixup_sha", "comment_count", "action_counts", "rule_violations"} {
		if _, present := p[k]; present {
			t.Errorf("payload should NOT contain %q for skipped event, got %v", k, p[k])
		}
	}
}

// TestEmitSpecIteration_UnknownEventType_ReturnsError asserts a
// misconfigured caller (a buggy workflow passing the wrong event_type
// or a typo) gets a clear non-retryable activity error rather than
// silently dropping the audit entry.
func TestEmitSpecIteration_UnknownEventType_ReturnsError(t *testing.T) {
	t.Setenv("CHITIN_DISABLE_CHAIN_EMIT", "1") // belt + suspenders: never shell-out
	var warn bytes.Buffer
	err := emitSpecIterationChainEvent(context.Background(),
		EmitSpecIterationTelemetryInput{EventType: "pr_iteration_completed", PRNumber: 1},
		&warn)
	if err == nil {
		t.Fatal("expected error for unknown event_type, got nil")
	}
	if !strings.Contains(err.Error(), "unknown event_type") {
		t.Errorf("error should mention unknown event_type, got: %v", err)
	}
}

// TestEmitSpecIteration_MissingPRNumber_ReturnsError asserts the
// pr_number guard fires: every FR-009 event carries pr_number in its
// payload, so a zero-value PRNumber is a programming error and should
// fail loudly rather than emit a broken chain entry.
func TestEmitSpecIteration_MissingPRNumber_ReturnsError(t *testing.T) {
	t.Setenv("CHITIN_DISABLE_CHAIN_EMIT", "1")
	err := emitSpecIterationChainEvent(context.Background(),
		EmitSpecIterationTelemetryInput{EventType: SpecLintCompletedEvent, PRNumber: 0},
		new(bytes.Buffer))
	if err == nil || !strings.Contains(err.Error(), "pr_number is required") {
		t.Fatalf("expected pr_number guard error, got: %v", err)
	}
}

// TestEmitSpecIteration_DisableChainEmit_ShortCircuits asserts
// CHITIN_DISABLE_CHAIN_EMIT=1 prevents the kernel shell-out entirely.
// Sets CHITIN_KERNEL_BIN to a definitely-missing path; if the
// short-circuit failed, the emit would warn-and-return-nil with a
// "kernel emit failed" message.
func TestEmitSpecIteration_DisableChainEmit_ShortCircuits(t *testing.T) {
	t.Setenv("CHITIN_DISABLE_CHAIN_EMIT", "1")
	t.Setenv("CHITIN_KERNEL_BIN", filepath.Join(t.TempDir(), "definitely-not-here"))

	var warn bytes.Buffer
	err := emitSpecIterationChainEvent(context.Background(),
		EmitSpecIterationTelemetryInput{
			EventType: SpecIterationCompletedEvent,
			PRNumber:  1,
			Round:     1,
		}, &warn)
	if err != nil {
		t.Fatalf("expected nil error on disable, got %v", err)
	}
	if warn.Len() != 0 {
		t.Errorf("expected silent short-circuit on disable, got stderr: %s", warn.String())
	}
}

// TestEmitSpecIteration_KernelMissing_FailsSoft asserts a missing
// kernel binary doesn't fault the activity. The chain entry is
// supplementary audit — losing it warns to stderr but the workflow
// outcome carries the load-bearing signal.
func TestEmitSpecIteration_KernelMissing_FailsSoft(t *testing.T) {
	t.Setenv("CHITIN_DISABLE_CHAIN_EMIT", "0")
	t.Setenv("CHITIN_KERNEL_BIN", filepath.Join(t.TempDir(), "definitely-not-here"))
	t.Setenv("CHITIN_DIR", t.TempDir())

	var warn bytes.Buffer
	err := emitSpecIterationChainEvent(context.Background(),
		EmitSpecIterationTelemetryInput{
			EventType: SpecIterationSkippedEvent,
			PRNumber:  1,
			Reason:    "iteration_cap_hit",
		}, &warn)
	if err != nil {
		t.Fatalf("missing kernel must be fail-soft, got err: %v", err)
	}
	if !strings.Contains(warn.String(), "kernel emit failed") {
		t.Errorf("expected warning on missing kernel, got: %q", warn.String())
	}
	if !strings.Contains(warn.String(), "spec_iteration_skipped") {
		t.Errorf("warning should name the event type, got: %q", warn.String())
	}
}

// TestEmitSpecIteration_KernelExitsNonZero_FailsSoft asserts a kernel
// exit != 0 (e.g. the chain state file is locked, the kernel binary is
// older than the schema) doesn't fault the activity. Same audit-vs-
// load-bearing argument as the missing-binary case.
func TestEmitSpecIteration_KernelExitsNonZero_FailsSoft(t *testing.T) {
	bin, _ := fakeSpecKernelBin(t, 1)
	t.Setenv("CHITIN_DISABLE_CHAIN_EMIT", "0")
	t.Setenv("CHITIN_KERNEL_BIN", bin)
	t.Setenv("CHITIN_DIR", t.TempDir())

	var warn bytes.Buffer
	err := emitSpecIterationChainEvent(context.Background(),
		EmitSpecIterationTelemetryInput{
			EventType:   SpecIterationFailedEvent,
			PRNumber:    1,
			Round:       1,
			FailureKind: "driver_fault",
			Detail:      "fake kernel returned non-zero",
		}, &warn)
	if err != nil {
		t.Fatalf("non-zero exit must be fail-soft, got err: %v", err)
	}
	if !strings.Contains(warn.String(), "kernel emit failed") {
		t.Errorf("expected warning on non-zero exit, got: %q", warn.String())
	}
}

// TestEmitSpecIteration_EnvelopeFields asserts the envelope wrapping
// is the spec-112/113 shape: schema_version=2, surface=chitin-
// orchestrator, chain_type=spec-iteration, and a parseable RFC3339
// timestamp.
func TestEmitSpecIteration_EnvelopeFields(t *testing.T) {
	env := captureEmit(t, EmitSpecIterationTelemetryInput{
		EventType: SpecLintCompletedEvent,
		PRNumber:  1,
	})
	if env["schema_version"] != "2" {
		t.Errorf("schema_version = %v, want 2", env["schema_version"])
	}
	if env["surface"] != "chitin-orchestrator" {
		t.Errorf("surface = %v", env["surface"])
	}
	if env["chain_type"] != "spec-iteration" {
		t.Errorf("chain_type = %v", env["chain_type"])
	}
	if env["agent_instance_id"] != "chitin-orchestrator" {
		t.Errorf("agent_instance_id = %v", env["agent_instance_id"])
	}
	if !strings.HasPrefix(env["run_id"].(string), "spec-iteration-pr-1") {
		t.Errorf("run_id = %v, want spec-iteration-pr-1 prefix", env["run_id"])
	}
	if !strings.Contains(env["session_id"].(string), "spec-iterate-1") {
		t.Errorf("session_id = %v", env["session_id"])
	}
	if _, ok := env["ts"].(string); !ok {
		t.Errorf("ts should be a string, got %T", env["ts"])
	}
}

// TestSpecIterationEventTypes_ClosedSet asserts the exported helper
// returns exactly the six FR-009 event names — the L04 linter rule and
// any downstream consumer can rely on this set being the source of
// truth.
func TestSpecIterationEventTypes_ClosedSet(t *testing.T) {
	got := SpecIterationEventTypes()
	want := []string{
		"spec_lint_completed",
		"spec_iteration_round_started",
		"spec_iteration_completed",
		"spec_iteration_failed",
		"spec_iteration_escalated",
		"spec_iteration_skipped",
	}
	if len(got) != len(want) {
		t.Fatalf("got %d event types, want %d: %v", len(got), len(want), got)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("event_types[%d] = %q, want %q", i, got[i], w)
		}
	}
}

// TestEmitSpecIteration_ActivityName asserts the stable Temporal
// activity name — the workflow dispatch is string-based, so a name
// change is a silent breakage for any registered workflow.
func TestEmitSpecIteration_ActivityName(t *testing.T) {
	a := NewEmitSpecIterationTelemetry()
	if got := a.ActivityName(); got != "EmitSpecIterationTelemetry" {
		t.Errorf("ActivityName() = %q, want EmitSpecIterationTelemetry", got)
	}
}

// payloadMap extracts envelope["payload"] as a map and fails the test
// cleanly when it isn't shaped right. Centralizes the type-switch so
// every case test doesn't repeat it.
func payloadMap(t *testing.T, env map[string]any) map[string]any {
	t.Helper()
	p, ok := env["payload"].(map[string]any)
	if !ok {
		t.Fatalf("payload not an object: %T", env["payload"])
	}
	return p
}
