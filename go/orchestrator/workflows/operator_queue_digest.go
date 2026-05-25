package workflows

import (
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/chitinhq/chitin/go/orchestrator/activities"
	"github.com/chitinhq/chitin/go/orchestrator/schedules"
)

// operatorQueueDigestSince is the rolling window the digest summarises —
// equivalent to `queue --since 24h --format md` on the CLI. Hard-coded per
// spec 114 US2 FR-009; the scheduled cadence is also 24h, so any other window
// would over- or under-count.
const operatorQueueDigestSince = 24 * time.Hour

// OperatorQueueDigestResult is the typed outcome of one OperatorQueueDigest
// run — durable evidence in Temporal history of what the digest covered and
// whether the Discord post fired.
type OperatorQueueDigestResult struct {
	// JobName echoes schedules.JobSpec.Name ("operator-queue-digest").
	JobName string `json:"job_name"`
	// Window is the duration the digest summarised.
	Window time.Duration `json:"window"`
	// Markdown is the rendered body that was handed to DiscordNotify.
	Markdown string `json:"markdown"`
}

// OperatorQueueDigestWorkflow is the spec 114 US2 daily PR-queue digest
// workflow. The Temporal Schedule operatorQueueDigestSpec triggers it at
// 09:00 America/Detroit; the workflow renders `queue --since 24h --format md`
// IN-PROCESS via the RenderOperatorQueueDigest activity, then posts the
// rendered markdown to the operator's Discord through the same DiscordNotify
// activity that surfaces single-event escalations (spec 080 US2).
//
// In-process — not a subprocess hop — so the digest survives a missing $PATH
// or a relocated chitin-orchestrator binary on the worker host. The two
// activities are sequenced (render → notify) because the second consumes the
// first's output.
//
// The DiscordNotify call is best-effort by spec 080 FR-007: a Discord outage
// MUST NOT fail the digest workflow. The activity error return is discarded;
// the workflow settles with the rendered markdown so the next 09:00 cycle
// still fires.
func OperatorQueueDigestWorkflow(ctx workflow.Context, spec schedules.JobSpec) (OperatorQueueDigestResult, error) {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		// Generous bound: the queue scan reads chain files + paginates the
		// gh API; SC-002 caps the CLI at 2s but a worker under load may
		// take longer. 5 minutes is well above the worst case while still
		// surfacing a wedge.
		StartToCloseTimeout: 5 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    10 * time.Second,
			BackoffCoefficient: 2.0,
			MaximumInterval:    1 * time.Minute,
			// Modest retry — a transient gh / chain-IO fault is worth a few
			// quick retries, but a persistently failing renderer should
			// surface on the next daily cycle rather than retry forever.
			MaximumAttempts: 3,
		},
	})

	var rendered activities.OperatorQueueDigestResult
	if err := workflow.ExecuteActivity(ctx, activities.RenderOperatorQueueDigestActivityName,
		activities.OperatorQueueDigestInput{Since: operatorQueueDigestSince}).Get(ctx, &rendered); err != nil {
		return OperatorQueueDigestResult{
			JobName: spec.Name,
			Window:  operatorQueueDigestSince,
		}, err
	}

	// Post to Discord through DiscordNotify. NotifyOperatorDigest tells
	// NotificationEvent.line() to return Summary verbatim so the markdown
	// table renders cleanly — a "[chitin] operator-digest — ..." prefix
	// would corrupt the table.
	ev := activities.NotificationEvent{
		Kind:    activities.NotifyOperatorDigest,
		Summary: rendered.Markdown,
	}
	// Best-effort per spec 080 FR-007 — a Discord fault must never fail the
	// digest. DiscordNotify itself already returns nil on every degraded
	// path; the ignored Get error is the activity-transport fault.
	_ = workflow.ExecuteActivity(ctx, activities.DiscordNotifyActivityName, ev).Get(ctx, nil)

	return OperatorQueueDigestResult{
		JobName:  spec.Name,
		Window:   rendered.Window,
		Markdown: rendered.Markdown,
	}, nil
}
