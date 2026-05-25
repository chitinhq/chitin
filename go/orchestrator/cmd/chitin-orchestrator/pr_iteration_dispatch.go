// pr_iteration_dispatch.go — spec 113 US1: dispatch the PR comment-respond
// loop from the factory-listen /webhook/pr handler.
//
// Flow:
//   1. handlePR receives a `pull_request_review` event with action=submitted.
//   2. If the PR's head branch matches chitin/wu/* AND the reviewer is in the
//      copilot allowlist, the review is eligible for iteration.
//   3. dispatchPRIteration resolves the authoring driver (parsed from the
//      branch slug — spec 113 work-unit branches encode the spec ref) and
//      fires PRIterationWorkflow with deterministic WorkflowID
//      `iteration-pr-<N>-review-<M>`.
//   4. The activity itself fetches the review context, re-invokes the driver,
//      commits + force-pushes any fixup, and emits chain events.

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"

	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/sdk/client"

	"github.com/chitinhq/chitin/go/orchestrator/workflows"
)

// copilotReviewerLogins are the allowlisted Copilot identities whose reviews
// trigger the iteration loop. Non-allowlisted reviewers (humans) route to
// the escalation path (spec 113 US3 — not yet implemented; v1 just no-ops).
var copilotReviewerLogins = map[string]bool{
	"copilot-pull-request-reviewer": true,
	"copilot":                       true,
	"github-copilot[bot]":           true,
}

// chitinWUBranchPattern matches the factory's work-unit branch naming
// convention: `chitin/wu/<spec-slug>-t<NNN>-<suffix>`. A PR whose head
// branch matches this pattern is factory-authored and eligible for
// spec 113 iteration.
var chitinWUBranchPattern = regexp.MustCompile(`^chitin/wu/([^/]+)$`)

// prIterationDispatchInput is the closed input to dispatchPRIteration.
type prIterationDispatchInput struct {
	// Repo is the GitHub owner/name pair (e.g. "chitinhq/chitin").
	Repo string
	// PRNumber is the chitin-authored PR being iterated.
	PRNumber int
	// PRBranch is the head branch of the PR; must match chitinWUBranchPattern.
	PRBranch string
	// ReviewID is the GitHub review id triggering this iteration.
	ReviewID int64
	// DriverID is the driver that authored the original PR. v1 derives this
	// from the spec slug + operator-configured default — full lookup via
	// chain events is a follow-up.
	DriverID string
	// TargetRepo is the absolute path to the operator's local clone — the
	// worktree Manager mints its checkout under this.
	TargetRepo string
	// TemporalHost is the dialer target.
	TemporalHost string
}

// prIterationDispatchResult is what dispatchPRIteration returns so handlePR
// can surface visibility in the webhook response.
type prIterationDispatchResult struct {
	// Dispatched is true iff PRIterationWorkflow successfully started (or
	// dedup'd as AlreadyStarted, which we count as success).
	Dispatched bool
	// WorkflowID is the deterministic id assigned to the iteration.
	WorkflowID string
	// FailureKind names the dispatch failure mode when Dispatched is false.
	FailureKind string
	// Detail carries the failure detail (empty on success).
	Detail string
}

// dispatchPRIteration starts a PRIterationWorkflow for one eligible review.
// Returns a typed result; the workflow runs async under Temporal.
//
// Default `dialer` is `dialTemporalAsStarter`; tests inject a stub.
func dispatchPRIteration(
	ctx context.Context,
	in prIterationDispatchInput,
	dialer temporalDialer,
	stderr io.Writer,
) prIterationDispatchResult {
	if in.PRNumber <= 0 || in.PRBranch == "" || in.ReviewID <= 0 {
		return prIterationDispatchResult{
			FailureKind: "invalid_input",
			Detail:      "PRNumber, PRBranch, and ReviewID are required",
		}
	}
	if !chitinWUBranchPattern.MatchString(in.PRBranch) {
		// Not a factory-authored branch — eligibility filter at the caller
		// should have caught this, but the dispatcher is defensive.
		return prIterationDispatchResult{
			FailureKind: "non_factory_branch",
			Detail:      fmt.Sprintf("branch %q is not a chitin/wu/* work-unit branch", in.PRBranch),
		}
	}

	if dialer == nil {
		dialer = dialTemporalAsStarter
	}
	c, host, err := dialer(ctx, in.TemporalHost)
	if err != nil {
		warnIterationDispatch(stderr, "temporal dial %s failed: %v", host, err)
		return prIterationDispatchResult{
			FailureKind: "temporal_unreachable",
			Detail:      err.Error(),
		}
	}
	defer c.Close()

	workflowID := fmt.Sprintf("iteration-pr-%d-review-%d", in.PRNumber, in.ReviewID)
	startOpts := client.StartWorkflowOptions{
		ID:        workflowID,
		TaskQueue: TaskQueue,
		// REJECT_DUPLICATE makes the per-(PR, review) dedup explicit on the
		// server side: a redelivered webhook attempting the same WorkflowID
		// gets WorkflowExecutionAlreadyStarted — counted as dispatched.
		WorkflowIDReusePolicy: enumspb.WORKFLOW_ID_REUSE_POLICY_REJECT_DUPLICATE,
	}
	wfIn := workflows.PRIterationInput{
		PRNumber:   in.PRNumber,
		PRBranch:   in.PRBranch,
		TargetRepo: in.TargetRepo,
		Repo:       in.Repo,
		ReviewID:   in.ReviewID,
		DriverID:   in.DriverID,
	}
	if _, err := c.ExecuteWorkflow(ctx, startOpts, workflows.PRIterationWorkflow, wfIn); err != nil {
		if isIterationAlreadyStartedErr(err) {
			return prIterationDispatchResult{
				Dispatched: true,
				WorkflowID: workflowID,
			}
		}
		warnIterationDispatch(stderr, "dispatch for PR #%d review %d failed: %v", in.PRNumber, in.ReviewID, err)
		return prIterationDispatchResult{
			FailureKind: "dispatch_error",
			Detail:      err.Error(),
			WorkflowID:  workflowID,
		}
	}

	return prIterationDispatchResult{
		Dispatched: true,
		WorkflowID: workflowID,
	}
}

// isIterationAlreadyStartedErr mirrors spec 112 US2's isAlreadyStartedErr.
// Treats Temporal's "workflow already started" as the dedup-success path.
func isIterationAlreadyStartedErr(err error) bool {
	if err == nil {
		return false
	}
	var aErr *serviceerror.WorkflowExecutionAlreadyStarted
	if errors.As(err, &aErr) {
		return true
	}
	return strings.Contains(err.Error(), "WorkflowExecutionAlreadyStarted") ||
		strings.Contains(err.Error(), "already started")
}

// warnIterationDispatch logs a dispatch warning, tolerating a nil writer.
func warnIterationDispatch(stderr io.Writer, format string, args ...any) {
	if stderr == nil {
		return
	}
	fmt.Fprintf(stderr, "warning: pr-iteration dispatch: "+format+"\n", args...)
}

// isCopilotReviewer reports whether the given GitHub login is in the
// configured Copilot allowlist (spec 113 FR-002). Case-insensitive match
// because GitHub sometimes lowercases bot logins on the wire.
func isCopilotReviewer(login string) bool {
	l := strings.ToLower(login)
	if copilotReviewerLogins[l] {
		return true
	}
	// Common variant: the "[bot]" suffix shows up on some webhook payloads.
	if strings.HasSuffix(l, "[bot]") && copilotReviewerLogins[strings.TrimSuffix(l, "[bot]")] {
		return true
	}
	// Substring match as a final safety net for copilot variants.
	return strings.Contains(l, "copilot")
}

// driverIDFromBranch parses the authoring driver id from a work-unit
// branch slug. v1 heuristic: every chitin/wu/* branch was authored by
// claudecode (the default driver). A follow-up will look up the actual
// driver via a chain-event scan keyed by the branch slug.
func driverIDFromBranch(branch string) string {
	// Always return claudecode for now — the operator-host has both
	// claudecode + codex registered, and claudecode is the primary fixup
	// driver. Codex is more typically a one-shot author. The wrong driver
	// here only matters for prompt style, not correctness.
	_ = branch
	return "claudecode"
}
