package activities

import (
	"context"
	"fmt"
)

// EscalateInternalRereviewReason is the closed taxonomy of spec 116
// escalation reasons. Each value names a distinct situation the operator
// is being asked to look at. Mirrors the chain event_type variants the
// workflow emits in parallel — same reason names so the Discord ping and
// the chain entry are grep-equivalent.
type EscalateInternalRereviewReason string

const (
	// ReasonRereviewLowConfidence — re-reviewer returned an approve-shaped
	// verdict with Confidence=low. Spec 116 FR-010: the label still gets
	// applied (autopilot proceeds) AND the operator gets pinged so the
	// gap between "approve" and "rubber-stamped" is observable.
	ReasonRereviewLowConfidence EscalateInternalRereviewReason = "rereview_low_confidence"
	// ReasonRereviewRequestChanges — re-reviewer blocked the fixup. PR
	// stays open, no label applied, operator must triage.
	ReasonRereviewRequestChanges EscalateInternalRereviewReason = "rereview_request_changes"
	// ReasonRereviewAbstain — re-reviewer abstained. Operator decides.
	ReasonRereviewAbstain EscalateInternalRereviewReason = "rereview_abstain"
	// ReasonRereviewSkipped — re-review didn't run (empty pool, registry
	// missing, etc.). Operator decides whether to act on the Copilot-only
	// review or wait for re-review configuration to land.
	ReasonRereviewSkipped EscalateInternalRereviewReason = "rereview_skipped"
	// ReasonRereviewFailed — re-review ran but the driver returned a
	// non-verdict (malformed JSON, validation failure, driver fault).
	// Operator decides whether to retry or merge anyway.
	ReasonRereviewFailed EscalateInternalRereviewReason = "rereview_failed"
)

// EscalateInternalRereviewInput is the typed input. Carries every field
// the Discord renderer needs PLUS the workflow's verdict context so the
// operator gets a self-explanatory ping (no "see chain event X" — the
// PR link and reason are right there).
type EscalateInternalRereviewInput struct {
	// PRNumber is the chitin-authored PR involved.
	PRNumber int `json:"pr_number"`
	// PRURL is the GitHub URL for the operator-clickable link. Required;
	// notifyDiscordEscalation drops notices with empty URL.
	PRURL string `json:"pr_url"`
	// PRTitle is the PR title (optional). When set, included in the ping
	// so the operator recognises the PR without clicking through.
	PRTitle string `json:"pr_title,omitempty"`
	// Reason classifies the escalation.
	Reason EscalateInternalRereviewReason `json:"reason"`
	// ReviewerDriver is the driver that produced (or failed to produce)
	// the verdict — included in Detail so the operator knows which
	// driver's output to investigate.
	ReviewerDriver string `json:"reviewer_driver,omitempty"`
	// Detail is a one-line free-text amplifier (e.g., the failure_kind,
	// the blocker count, the skip_reason). Kept short — Discord truncates
	// long content.
	Detail string `json:"detail,omitempty"`
}

// EscalateInternalRereviewResult is the typed outcome. Notify is
// best-effort (notifyDiscordEscalation never errors); the result records
// what the activity did so the workflow's chain event has the same view.
type EscalateInternalRereviewResult struct {
	// Notified is true when the Discord post was attempted (i.e., the
	// PR URL was present + a webhook was resolved). Posts that fail at
	// the network layer still count as Notified=true here because the
	// helper's fail-soft contract treats the post itself as fire-and-
	// forget.
	Notified bool `json:"notified"`
	// Explanation is the human-readable account.
	Explanation string `json:"explanation"`
}

// EscalateInternalRereview is the spec-116 activity that fires the
// Discord escalation for one re-review outcome. Pure side-effect; no
// validation of the verdict itself (the workflow has already decided
// this case warrants escalation).
type EscalateInternalRereview struct{}

// NewEscalateInternalRereview returns the activity.
func NewEscalateInternalRereview() *EscalateInternalRereview { return &EscalateInternalRereview{} }

// ActivityName is the stable Temporal name.
func (a *EscalateInternalRereview) ActivityName() string { return "EscalateInternalRereview" }

// Execute fires the Discord notification. Always returns nil error per
// the fail-soft contract.
func (a *EscalateInternalRereview) Execute(ctx context.Context, in EscalateInternalRereviewInput) (EscalateInternalRereviewResult, error) {
	if in.PRURL == "" {
		return EscalateInternalRereviewResult{
			Explanation: "skipped: empty PRURL",
		}, nil
	}
	if in.Reason == "" {
		return EscalateInternalRereviewResult{
			Explanation: "skipped: empty Reason",
		}, nil
	}
	notice := EscalationNotice{
		EventType: "internal_rereview",
		Severity:  severityFor(in.Reason),
		PRNumber:  in.PRNumber,
		PRTitle:   in.PRTitle,
		PRURL:     in.PRURL,
		Reason:    string(in.Reason),
		Detail:    composeDetail(in),
	}
	notifyDiscordEscalation(ctx, notice)
	return EscalateInternalRereviewResult{
		Notified: true,
		Explanation: fmt.Sprintf(
			"escalated PR #%d to Discord: reason=%s reviewer=%s",
			in.PRNumber, in.Reason, in.ReviewerDriver),
	}, nil
}

// severityFor maps a spec-116 escalation reason to its Discord visual
// severity. low_confidence is the one ambiguous case — the verdict was
// approve-shaped so the autopilot did proceed (ready emoji), but the
// operator is being pinged for awareness. Render as alert so it stands
// out in the operator's feed; "the loop merged on uncertain ground"
// is the kind of thing the operator should NOT silently sleep through.
func severityFor(r EscalateInternalRereviewReason) EscalationSeverity {
	return SeverityAlert
}

// composeDetail prepends the reviewer-driver attribution (when known) to
// the operator-supplied Detail so every ping has the same shape:
// "reviewer=<driver> | <free-text>".
func composeDetail(in EscalateInternalRereviewInput) string {
	if in.ReviewerDriver == "" {
		return in.Detail
	}
	if in.Detail == "" {
		return "reviewer=" + in.ReviewerDriver
	}
	return "reviewer=" + in.ReviewerDriver + " | " + in.Detail
}
