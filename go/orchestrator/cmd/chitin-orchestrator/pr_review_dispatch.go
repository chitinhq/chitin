// pr_review_dispatch.go — spec 099 slice 4: dedup-gated PR review
// dispatch from the factory-listen /webhook/pr handler.
//
// Flow per FR-008 + FR-009:
//   1. Query the chain for existing copilot_pr_detected for
//      (repo, pr_number). If found → return dedup_skipped result.
//   2. Otherwise dial Temporal, start PRReviewWorkflow (spec 094),
//      emit copilot_pr_detected with the workflow's RunID.
//   3. On dial / start failure emit copilot_review_failed and
//      return a result populated with the failure kind.
//
// On the read side the dedup is per-(repo, pr_number) which makes
// re-deliveries (GitHub's at-least-once webhook contract) safe per
// SC-003: 100 redeliveries → 1 PRReviewWorkflow start.

package main

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/google/uuid"
	"go.temporal.io/sdk/client"

	"github.com/chitinhq/chitin/go/orchestrator/activities/review"
	"github.com/chitinhq/chitin/go/orchestrator/workflows"
)

// workflowStarter is the narrow interface dispatchPRReview needs from
// a Temporal client. Extracted so tests inject a stub without depending
// on go.temporal.io/sdk/mocks (which would pull a substantial set of
// transitive test dependencies into the build graph).
type workflowStarter interface {
	ExecuteWorkflow(ctx context.Context, options client.StartWorkflowOptions, workflow any, args ...any) (client.WorkflowRun, error)
	Close()
}

// prDispatchInput is the closed input to dispatchPRReview. All fields
// are sourced from the parsed prPayload + eligibility check; the
// dispatcher is pure with respect to its inputs (modulo Temporal +
// chain side effects).
type prDispatchInput struct {
	Repo         string
	PRNumber     int
	PRURL        string
	SpecRef      string // "unknown" if Closes ref not recoverable
	IssueNumber  int
	Commits      int
	Additions    int
	Deletions    int
	ChangedFiles int
	TemporalHost string // forwarded from factoryHandler.temporalHost
}

// prDispatchResult is what dispatchPRReview returns. handlePR uses it
// to populate the prResponse body fields (ReviewStarted, ReviewRunID,
// DedupSkipped).
type prDispatchResult struct {
	ReviewStarted bool
	ReviewRunID   string
	DedupSkipped  bool
	FailureKind   string // populated when ReviewStarted==false && DedupSkipped==false
	Detail        string
}

// dialTemporalAsStarter is the production dialer: wraps the existing
// dialTemporal helper in the narrow workflowStarter interface used by
// dispatchPRReview. Used as the default when no test dialer is
// injected.
func dialTemporalAsStarter(ctx context.Context, host string) (workflowStarter, string, error) {
	c, resolvedHost, err := dialTemporal(ctx, host)
	if err != nil {
		return nil, resolvedHost, err
	}
	return c, resolvedHost, nil
}

// dispatchPRReview is the dedup-gated dispatch entry point invoked
// from handlePR. Pulled out as a free function (vs a method on
// factoryHandler) so tests can drive it without spinning the HTTP
// server.
//
// Default `dialer` is `dialTemporalAsStarter`; tests inject a fake to
// avoid needing a temporal-dev server.
type temporalDialer func(ctx context.Context, host string) (workflowStarter, string, error)

func dispatchPRReview(ctx context.Context, in prDispatchInput, dialer temporalDialer, stderr io.Writer) prDispatchResult {
	// FR-008: dedup gate. Read-only chain scan; tolerate IO errors
	// fail-open (better to re-emit than silently drop).
	found, err := hasPriorPRDetection("", in.Repo, in.PRNumber)
	if err != nil {
		fmt.Fprintf(stderr, "warning: dedup chain scan failed (proceeding fail-open): %v\n", err)
	}
	if found {
		return prDispatchResult{DedupSkipped: true}
	}

	// Dial Temporal. dialer abstraction lets tests bypass.
	if dialer == nil {
		dialer = dialTemporalAsStarter
	}
	c, host, err := dialer(ctx, in.TemporalHost)
	if err != nil {
		emitCopilotReviewFailed(ctx, CopilotReviewFailedPayload{
			Repo:         in.Repo,
			PRNumber:     in.PRNumber,
			FailureKind:  "temporal_unreachable",
			Detail:       fmt.Sprintf("dial %s: %v", host, err),
			FailedAt:     time.Now().UTC().Format(time.RFC3339),
		}, stderr)
		return prDispatchResult{FailureKind: "temporal_unreachable", Detail: err.Error()}
	}
	defer c.Close()

	workflowID := uuid.NewString()
	reviewIn := review.PRReviewInput{
		Repo:        in.Repo,
		PRNumber:    in.PRNumber,
		PRAuthor:    "copilot", // FR-005 no-self-review exclusion: Copilot's PRs are author=copilot
		PolicyClass: "impl",    // Copilot-dispatched PRs default to impl class; future spec may make this per-spec configurable
		ArbiterType: review.ArbiterMachine,
	}
	startOpts := client.StartWorkflowOptions{
		ID:        workflowID,
		TaskQueue: TaskQueue,
	}
	if _, err := c.ExecuteWorkflow(ctx, startOpts, workflows.PRReviewWorkflow, reviewIn); err != nil {
		emitCopilotReviewFailed(ctx, CopilotReviewFailedPayload{
			Repo:         in.Repo,
			PRNumber:     in.PRNumber,
			ReviewRunID:  workflowID,
			FailureKind:  "dispatch_error",
			Detail:       err.Error(),
			FailedAt:     time.Now().UTC().Format(time.RFC3339),
		}, stderr)
		return prDispatchResult{FailureKind: "dispatch_error", Detail: err.Error(), ReviewRunID: workflowID}
	}

	emitCopilotPRDetected(ctx, CopilotPRDetectedPayload{
		Repo:                in.Repo,
		PRNumber:            in.PRNumber,
		PRURL:               in.PRURL,
		SpecRef:             in.SpecRef,
		IssueNumber:         in.IssueNumber,
		Commits:             in.Commits,
		Additions:           in.Additions,
		Deletions:           in.Deletions,
		ChangedFiles:        in.ChangedFiles,
		DetectedAt:          time.Now().UTC().Format(time.RFC3339),
		ReviewWorkflowRunID: workflowID,
	}, stderr)

	return prDispatchResult{ReviewStarted: true, ReviewRunID: workflowID}
}
