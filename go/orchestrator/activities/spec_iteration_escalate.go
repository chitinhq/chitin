package activities

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// EmitSpecIterationEscalationInput is the typed input — one
// `spec_iteration_escalated` chain event the SpecIterationWorkflow
// asks the kernel to record (spec 115 T014 / FR-009 / FR-010).
//
// T014 is the narrow caller: emit one event when the design-judgement
// partition of a review is non-empty. T016 (EmitSpecIterationTelemetry)
// is the broader spec-iteration emitter — when it lands, this activity
// becomes a thin alias or is folded in. Keeping the surfaces separate
// for now means T014 ships standalone without blocking on T016.
type EmitSpecIterationEscalationInput struct {
	// PRNumber is the spec pull request being escalated.
	PRNumber int `json:"pr_number"`
	// ReviewID is the GitHub review id that triggered the escalation.
	ReviewID int64 `json:"review_id"`
	// Round is the iteration round number (1 for v1 — single-round cap
	// per spec 115 FR-005 / spec 113 FR-007).
	Round int `json:"round"`
	// RoundsAttempted is the count of iteration rounds attempted so far
	// (1 in the single-round MVP). Carried because FR-009's
	// `spec_iteration_escalated` event names this as a payload field
	// distinct from Round (which is the round THIS event was emitted in).
	RoundsAttempted int `json:"rounds_attempted"`
	// Reason is the closed-taxonomy reason string from FR-010. For T014
	// the only producer is "design_judgement_required"; the field is
	// kept generic so T016 can reuse the activity for the other reasons.
	Reason string `json:"reason"`
	// JudgementCommentIDs is the set of comment ids the classifier put
	// in the DesignJudgement partition (spec 115 FR-007). Carried into
	// the event payload so the operator queue (spec 114 + spec 115
	// FR-008) can deep-link to the specific comments.
	JudgementCommentIDs []int64 `json:"judgement_comment_ids,omitempty"`
}

// EmitSpecIterationEscalationResult is the typed output. The activity
// is fail-soft (a kernel-emit failure is logged but does not fail the
// workflow — the round outcome is the load-bearing signal, the chain
// entry is supplementary audit, matching the spec-112 / spec-113 emit
// pattern). Emitted == true means the kernel accepted the event.
type EmitSpecIterationEscalationResult struct {
	Emitted     bool   `json:"emitted"`
	Explanation string `json:"explanation"`
}

// EmitSpecIterationEscalation writes one `spec_iteration_escalated`
// chain event for the design-judgement partition of a spec-PR review.
// Honours CHITIN_DISABLE_CHAIN_EMIT=1 (sets Emitted=false, returns nil
// error) so hermetic tests don't have to install a fake kernel.
//
// Wire path mirrors emitPRIterationEvent / emitSiblingRebaseEvent —
// temp file + `chitin-kernel emit -event-file <path>`, fail-soft.
// CHITIN_KERNEL_BIN overrides the binary; CHITIN_DIR overrides the
// chain dir (default ~/.chitin) so the worker host's chain captures
// the event.
func EmitSpecIterationEscalation(
	ctx context.Context,
	in EmitSpecIterationEscalationInput,
) (EmitSpecIterationEscalationResult, error) {
	if in.PRNumber <= 0 || in.ReviewID <= 0 || in.Reason == "" {
		return EmitSpecIterationEscalationResult{}, fmt.Errorf(
			"EmitSpecIterationEscalation: PRNumber, ReviewID, Reason are required")
	}
	if os.Getenv("CHITIN_DISABLE_CHAIN_EMIT") == "1" {
		return EmitSpecIterationEscalationResult{
			Emitted:     false,
			Explanation: "chain emit disabled via CHITIN_DISABLE_CHAIN_EMIT=1",
		}, nil
	}
	binPath := os.Getenv("CHITIN_KERNEL_BIN")
	if binPath == "" {
		binPath = "chitin-kernel"
	}
	payload := map[string]any{
		"pr_number":             in.PRNumber,
		"review_id":             in.ReviewID,
		"round":                 in.Round,
		"rounds_attempted":      in.RoundsAttempted,
		"reason":                in.Reason,
		"last_review_id":        in.ReviewID,
		"judgement_comment_ids": in.JudgementCommentIDs,
	}
	envelope := map[string]any{
		"schema_version":    "2",
		"event_type":        "spec_iteration_escalated",
		"run_id":            fmt.Sprintf("spec-iteration-%d-%d-r%d", in.PRNumber, in.ReviewID, in.Round),
		"session_id":        fmt.Sprintf("chitin-orchestrator-spec-iterate-%d", in.PRNumber),
		"surface":           "chitin-orchestrator",
		"agent_instance_id": "chitin-orchestrator",
		"chain_type":        "spec-iteration",
		"ts":                time.Now().UTC().Format(time.RFC3339Nano),
		"payload":           payload,
	}
	body, err := json.Marshal(envelope)
	if err != nil {
		return EmitSpecIterationEscalationResult{
			Emitted:     false,
			Explanation: fmt.Sprintf("marshal envelope: %v", err),
		}, nil
	}
	tmp, err := os.CreateTemp("", "chitin-spec-iterate-emit-*.json")
	if err != nil {
		return EmitSpecIterationEscalationResult{
			Emitted:     false,
			Explanation: fmt.Sprintf("temp file: %v", err),
		}, nil
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.Write(body); err != nil {
		tmp.Close()
		return EmitSpecIterationEscalationResult{
			Emitted:     false,
			Explanation: fmt.Sprintf("temp write: %v", err),
		}, nil
	}
	if err := tmp.Close(); err != nil {
		return EmitSpecIterationEscalationResult{
			Emitted:     false,
			Explanation: fmt.Sprintf("temp close: %v", err),
		}, nil
	}
	chitinDir := os.Getenv("CHITIN_DIR")
	if chitinDir == "" {
		if home, err := os.UserHomeDir(); err == nil {
			chitinDir = home + "/.chitin"
		} else {
			chitinDir = ".chitin"
		}
	}
	emitCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(emitCtx, binPath, "emit", "-dir", chitinDir, "-event-file", tmpPath)
	var stderrBuf strings.Builder
	cmd.Stderr = &stderrBuf
	if err := cmd.Run(); err != nil {
		tail := strings.TrimSpace(stderrBuf.String())
		if len(tail) > 200 {
			tail = tail[len(tail)-200:]
		}
		return EmitSpecIterationEscalationResult{
			Emitted:     false,
			Explanation: fmt.Sprintf("kernel emit failed: %v (stderr: %s)", err, tail),
		}, nil
	}
	return EmitSpecIterationEscalationResult{
		Emitted:     true,
		Explanation: fmt.Sprintf("emitted spec_iteration_escalated{reason=%s} for pr=%d review=%d", in.Reason, in.PRNumber, in.ReviewID),
	}, nil
}
