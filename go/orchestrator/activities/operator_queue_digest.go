package activities

import (
	"context"
	"fmt"
	"time"
)

// OperatorQueueDigestInput is the typed input to the RenderOperatorQueueDigest
// activity — a closed shape so the workflow's call site is decoupled from any
// future renderer parameters.
type OperatorQueueDigestInput struct {
	// Since is the rolling window the renderer summarises — equivalent to the
	// CLI's `queue --since DURATION`. Spec 114 US2 fixes this to 24h for the
	// daily digest cadence.
	Since time.Duration `json:"since"`
}

// OperatorQueueDigestResult is the typed result of one digest render — the
// markdown body the calling workflow hands to DiscordNotify.
type OperatorQueueDigestResult struct {
	// Markdown is the rendered `queue --format md` body, ready to post.
	Markdown string `json:"markdown"`
	// Window echoes the Since value the renderer summarised — durable
	// evidence in Temporal history of what each digest covered.
	Window time.Duration `json:"window"`
}

// QueueRenderer is the seam between the OperatorQueueDigest activity and the
// spec 114 US1 queue computation (T001–T008). The activity is registered with
// a renderer at worker-host startup; the workflow never imports the queue
// package, so the queue subcommand's implementation can land in a separate PR
// without coupling.
//
// An implementation MUST be deterministic in the time-since sense — given the
// same chain events + live PR list at the same instant, it produces the same
// markdown. It also MUST be IN-PROCESS — no subprocess hop — so the digest
// survives a missing $PATH or a relocated chitin-orchestrator binary.
type QueueRenderer interface {
	// Render produces the GitHub-flavoured markdown body equivalent to
	// `chitin-orchestrator queue --since DURATION --format md`. It MUST NOT
	// return an empty string on success — the spec 114 edge case "no PRs
	// need attention" surfaces as the literal "✅ no PRs need attention"
	// line, never as silence.
	Render(ctx context.Context, since time.Duration) (string, error)
}

// OperatorQueueDigest is the in-process queue digest activity (spec 114 US2
// FR-009). It calls QueueRenderer once per scheduled fire and returns the
// rendered markdown for the calling workflow to hand to DiscordNotify.
//
// A nil renderer is replaced with a stub that returns a placeholder line
// noting the queue subcommand is not yet wired — the activity is still
// registered, the schedule still fires, and the operator sees that the
// digest plumbing reached Discord even before the queue computation lands.
type OperatorQueueDigest struct {
	renderer QueueRenderer
}

// NewOperatorQueueDigest returns the activity bound to renderer. A nil
// renderer falls back to a stub so the schedule and Discord plumbing remain
// verifiable in production before the spec 114 US1 queue subcommand lands.
func NewOperatorQueueDigest(renderer QueueRenderer) *OperatorQueueDigest {
	if renderer == nil {
		renderer = stubQueueRenderer{}
	}
	return &OperatorQueueDigest{renderer: renderer}
}

// ActivityName is the stable Temporal activity name the workflow dispatches
// to. Kept in sync with RegisterSchedulerActivities and the workflow's
// ExecuteActivity call.
func (a *OperatorQueueDigest) ActivityName() string { return "RenderOperatorQueueDigest" }

// Execute renders one queue digest. A renderer fault is a real activity
// error so Temporal retries per the workflow's RetryPolicy — a transient
// gh / chain-file IO failure SHOULD retry, while the workflow's modest
// MaximumAttempts caps the blast radius.
func (a *OperatorQueueDigest) Execute(ctx context.Context, in OperatorQueueDigestInput) (OperatorQueueDigestResult, error) {
	md, err := a.renderer.Render(ctx, in.Since)
	if err != nil {
		return OperatorQueueDigestResult{Window: in.Since}, fmt.Errorf("rendering operator queue digest: %w", err)
	}
	return OperatorQueueDigestResult{Markdown: md, Window: in.Since}, nil
}

// stubQueueRenderer is the fallback renderer used when no concrete
// QueueRenderer is bound at worker startup. It is intentionally informative:
// the operator sees that the digest schedule fired and DiscordNotify reached
// Discord, but the message itself explains the queue subcommand is pending.
type stubQueueRenderer struct{}

// Render returns a placeholder body. The "✅" prefix mirrors the spec 114
// "no PRs need attention" edge case so the message reads as benign during
// the wire-up phase.
func (stubQueueRenderer) Render(_ context.Context, since time.Duration) (string, error) {
	return fmt.Sprintf(
		"**chitin operator queue digest** — last %s\n_queue rendering pending spec 114 T001–T008; schedule and Discord plumbing verified._",
		since), nil
}
