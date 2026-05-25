package workflows

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"

	"github.com/chitinhq/chitin/go/orchestrator/activities"
	"github.com/chitinhq/chitin/go/orchestrator/schedules"
)

// Spec 114 US2 T013 — integration test for OperatorQueueDigestWorkflow.
//
// The workflow renders the spec 114 PR queue as markdown via the
// RenderOperatorQueueDigest activity, then posts that markdown to Discord via
// the DiscordNotify activity. Both activities are stubbed under the Temporal
// testsuite environment so the workflow runs hermetically and replays
// deterministically — the TestWorkflowEnvironment panics on any
// non-determinism, which is the real assertion behind the integration framing.

// digestRunOutcome captures everything the test needs to assert about one
// OperatorQueueDigestWorkflow invocation — the rendered/posted bodies, the
// arguments the workflow handed each activity, the typed result it returned,
// and the workflow error (nil on the happy path).
//
// notifyCount is an int (not a bool) so the test can assert the "exactly once"
// contract: DiscordNotify MUST fire once per digest cycle, never twice. If a
// retry policy regression silently produced duplicate Discord posts, a bool
// would not detect it; the count does.
type digestRunOutcome struct {
	result      OperatorQueueDigestResult
	workflowErr error
	renderInput activities.OperatorQueueDigestInput
	notifyEvent activities.NotificationEvent
	notifyCount int
}

// runDigestWorkflow executes OperatorQueueDigestWorkflow once under the
// Temporal testsuite environment, registering stub activities that:
//   - record the input the workflow passed to RenderOperatorQueueDigest, and
//     return renderResult / renderErr
//   - record the NotificationEvent the workflow passed to DiscordNotify, and
//     return notifyErr
//
// The activity names match the strings the production workflow dispatches to
// (`"RenderOperatorQueueDigest"`, `"DiscordNotify"`), so the test exercises
// the exact same dispatch path the worker uses in production.
func runDigestWorkflow(
	t *testing.T,
	spec schedules.JobSpec,
	renderResult activities.OperatorQueueDigestResult,
	renderErr error,
	notifyErr error,
) digestRunOutcome {
	t.Helper()

	var (
		mu  sync.Mutex
		out digestRunOutcome
	)

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	env.RegisterActivityWithOptions(
		func(_ context.Context, in activities.OperatorQueueDigestInput) (activities.OperatorQueueDigestResult, error) {
			mu.Lock()
			out.renderInput = in
			mu.Unlock()
			return renderResult, renderErr
		},
		activity.RegisterOptions{Name: "RenderOperatorQueueDigest"},
	)

	env.RegisterActivityWithOptions(
		func(_ context.Context, ev activities.NotificationEvent) error {
			mu.Lock()
			out.notifyEvent = ev
			out.notifyCount++
			mu.Unlock()
			return notifyErr
		},
		activity.RegisterOptions{Name: "DiscordNotify"},
	)

	env.ExecuteWorkflow(OperatorQueueDigestWorkflow, spec)

	if !env.IsWorkflowCompleted() {
		t.Fatalf("OperatorQueueDigestWorkflow did not complete")
	}
	out.workflowErr = env.GetWorkflowError()
	if out.workflowErr == nil {
		if err := env.GetWorkflowResult(&out.result); err != nil {
			t.Fatalf("decoding OperatorQueueDigestResult: %v", err)
		}
	}
	return out
}

// TestOperatorQueueDigestWorkflow_PostsMarkdownToDiscord is the spec 114 US2
// integration happy path. The stub renderer produces a known markdown body
// and the test asserts:
//
//   - the workflow asked the renderer for the 24h window the spec hard-codes
//     (FR-009)
//   - the DiscordNotify activity fired exactly once
//   - the event carried Kind=NotifyOperatorDigest, so notify.line() returns
//     the body verbatim and the markdown table is not corrupted by a
//     "[chitin] operator-digest — ..." prefix
//   - the event's Summary is byte-for-byte the markdown the renderer returned
//   - the workflow's typed result echoes JobName / Window / Markdown for
//     durable evidence in Temporal history of what each digest covered
func TestOperatorQueueDigestWorkflow_PostsMarkdownToDiscord(t *testing.T) {
	const knownMarkdown = "## chitin queue — last 24h\n" +
		"| PR | reason | age |\n" +
		"|----|--------|-----|\n" +
		"| [#42](https://github.com/chitinhq/chitin/pull/42) | sibling_rebase_failed | 3h |\n" +
		"| [#57](https://github.com/chitinhq/chitin/pull/57) | iteration_cap_hit | 11h |\n"

	spec := schedules.JobSpec{
		Name:     "operator-queue-digest",
		Cron:     "0 9 * * *",
		TimeZone: "America/Detroit",
		Workflow: schedules.OperatorQueueDigestWorkflowName,
	}

	out := runDigestWorkflow(t, spec,
		activities.OperatorQueueDigestResult{
			Markdown: knownMarkdown,
			Window:   24 * time.Hour,
		},
		nil, nil,
	)

	if out.workflowErr != nil {
		t.Fatalf("workflow errored on happy path: %v", out.workflowErr)
	}

	// Renderer received the spec 114 FR-009 24h window — the workflow's
	// dispatch contract with the queue subcommand.
	if out.renderInput.Since != 24*time.Hour {
		t.Errorf("renderer input Since = %v, want 24h (spec 114 FR-009)", out.renderInput.Since)
	}

	// DiscordNotify fired exactly once with the exact markdown body, under
	// the digest kind so notify.line() posts it verbatim. The "exactly once"
	// assertion guards against a retry-policy regression that would double-
	// post the digest to the operator's Discord.
	if out.notifyCount != 1 {
		t.Fatalf("DiscordNotify invocations = %d, want 1 (exactly once per digest cycle)", out.notifyCount)
	}
	if out.notifyEvent.Kind != activities.NotifyOperatorDigest {
		t.Errorf("DiscordNotify event Kind = %q, want NotifyOperatorDigest (else line() would prefix the markdown and break the table)",
			out.notifyEvent.Kind)
	}
	if out.notifyEvent.Summary != knownMarkdown {
		t.Errorf("DiscordNotify event Summary did not match the rendered markdown.\n got:  %q\n want: %q",
			out.notifyEvent.Summary, knownMarkdown)
	}

	// Typed result echoes the durable evidence the workflow records.
	if out.result.JobName != "operator-queue-digest" {
		t.Errorf("result JobName = %q, want %q", out.result.JobName, "operator-queue-digest")
	}
	if out.result.Window != 24*time.Hour {
		t.Errorf("result Window = %v, want 24h", out.result.Window)
	}
	if out.result.Markdown != knownMarkdown {
		t.Errorf("result Markdown did not match the rendered markdown")
	}
}

// TestOperatorQueueDigestWorkflow_DiscordFaultIsBestEffort proves a Discord
// outage does NOT fail the digest workflow — spec 080 FR-007's best-effort
// contract. The workflow still settles with the rendered markdown so the
// next 09:00 cycle still fires; the operator simply misses one morning.
func TestOperatorQueueDigestWorkflow_DiscordFaultIsBestEffort(t *testing.T) {
	const body = "## chitin queue — last 24h\n_(nothing to report)_\n"
	spec := schedules.JobSpec{Name: "operator-queue-digest"}

	out := runDigestWorkflow(t, spec,
		activities.OperatorQueueDigestResult{Markdown: body, Window: 24 * time.Hour},
		nil,
		errors.New("discord webhook 500"),
	)

	if out.workflowErr != nil {
		t.Fatalf("a Discord fault must NOT fail the digest workflow (spec 080 FR-007): %v",
			out.workflowErr)
	}
	// Still exactly once — even on a Discord-fault path, the production
	// DiscordNotify always returns nil (FR-007), so the workflow does not
	// retry. The stub returns the fault directly; we still expect a single
	// attempt because the workflow ignores the activity's transport error.
	if out.notifyCount != 1 {
		t.Fatalf("DiscordNotify invocations on Discord-fault path = %d, want 1 (no retry, no duplicate)", out.notifyCount)
	}
	if out.result.Markdown != body {
		t.Errorf("result Markdown should still echo the rendered body on Discord fault")
	}
}

// TestOperatorQueueDigestWorkflow_RenderFaultFailsWorkflow proves a renderer
// fault surfaces as a workflow error after the retry policy is exhausted —
// distinct from the best-effort Discord path. There is nothing to post when
// the renderer fails, so the workflow must fail loudly so the Schedule's
// next cycle can retry and an operator alert fires upstream.
func TestOperatorQueueDigestWorkflow_RenderFaultFailsWorkflow(t *testing.T) {
	spec := schedules.JobSpec{Name: "operator-queue-digest"}

	out := runDigestWorkflow(t, spec,
		activities.OperatorQueueDigestResult{},
		errors.New("gh api: 502 bad gateway"),
		nil,
	)

	if out.workflowErr == nil {
		t.Fatal("a persistent renderer fault must surface as a workflow error, not be swallowed")
	}
	if out.notifyCount != 0 {
		t.Errorf("DiscordNotify invocations on render-fault path = %d, want 0 (no body to post)", out.notifyCount)
	}
}
