// sibling_rebase_dispatch.go — spec 112 US2 sibling-rebase dispatch from
// the factory-listen /webhook/pr handler.
//
// Flow:
//   1. handlePR receives a pull_request event with action=closed.
//   2. If the PR is merged AND carries a sched/run/<id> label, the merge
//      is a chitin-authored sibling-merge: every other open PR with the
//      same sched/run/<id> label is a candidate for auto-rebase.
//   3. For each candidate, dispatchSiblingRebase fires one
//      SiblingRebaseWorkflow. The handler returns immediately; the
//      workflow runs durably under Temporal.
//
// The activity itself folds every rebase outcome — success, conflict,
// push failure — into a typed result and emits a sibling_rebase_dispatched
// or sibling_rebase_failed chain event. The CLI dispatcher's job is only
// to identify the candidates and start one workflow per candidate.

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"

	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/sdk/client"

	"github.com/chitinhq/chitin/go/orchestrator/activities"
	"github.com/chitinhq/chitin/go/orchestrator/workflows"
)

// siblingRebaseDispatchInput is the closed input to dispatchSiblingRebase.
// Populated by handlePR from the parsed prPayload of the source (merged) PR
// and the operator-host flags.
type siblingRebaseDispatchInput struct {
	// Repo is the GitHub owner/name pair, e.g. "chitinhq/chitin". Used to
	// query siblings via `gh pr list --repo`.
	Repo string
	// SourcePRNumber is the merged PR whose merge triggered the dispatch.
	SourcePRNumber int
	// SchedulerRunID is the run id parsed from the source PR's sched/run/<id>
	// label. Siblings are every other open PR carrying the same label.
	SchedulerRunID string
	// BaseBranch is the branch the merge landed on — almost always "main".
	BaseBranch string
	// TargetRepo is the absolute path to the local working repository the
	// rebase activity will mint its worktree under.
	TargetRepo string
	// TemporalHost is the dialer target for ExecuteWorkflow.
	TemporalHost string
}

// siblingRebaseDispatchResult is what dispatchSiblingRebase returns so the
// handlePR response can surface "how many siblings did we kick off" to the
// webhook caller. Per-sibling outcomes are not waited on — those land via
// the chain events the activity itself emits.
type siblingRebaseDispatchResult struct {
	// Siblings is the count of open sibling PRs found via the label query.
	Siblings int
	// Dispatched is the count of SiblingRebaseWorkflows successfully started.
	Dispatched int
	// PRNumbers is the per-sibling PR numbers dispatched, sorted ascending
	// for deterministic test output and operator-log greppability.
	PRNumbers []int
	// FailureKind names the first dispatch fault when Dispatched < Siblings.
	FailureKind string
	// Detail is the first dispatch fault's human-readable message.
	Detail string
}

// siblingListEntry is the subset of `gh pr list --json` output we consume.
// Kept tight — we only need to dispatch a rebase against the head ref.
type siblingListEntry struct {
	Number     int    `json:"number"`
	HeadRefName string `json:"headRefName"`
}

// siblingLister is the narrow interface dispatchSiblingRebase needs to find
// the open siblings. Extracted so tests inject a stub instead of shelling out
// to a real `gh` binary.
type siblingLister func(ctx context.Context, repo, label string, excludePR int) ([]siblingListEntry, error)

// dispatchSiblingRebase finds every open chitin-authored sibling PR (same
// sched/run/<id> label as the just-merged source PR) and fires one
// SiblingRebaseWorkflow per sibling. Returns a summary so handlePR can
// include it in the webhook response.
//
// Default `lister` is `listOpenSiblingsViaGH` (shells out to the gh CLI);
// tests inject a stub. Default `dialer` is `dialTemporalAsStarter`; tests
// inject a stub too.
func dispatchSiblingRebase(
	ctx context.Context,
	in siblingRebaseDispatchInput,
	lister siblingLister,
	dialer temporalDialer,
	stderr io.Writer,
) siblingRebaseDispatchResult {
	if in.SchedulerRunID == "" {
		return siblingRebaseDispatchResult{}
	}
	if lister == nil {
		lister = listOpenSiblingsViaGH
	}

	label := activities.SchedRunLabelPrefix + in.SchedulerRunID
	siblings, err := lister(ctx, in.Repo, label, in.SourcePRNumber)
	if err != nil {
		warnDispatch(stderr, "list siblings failed: %v", err)
		return siblingRebaseDispatchResult{
			FailureKind: "sibling_list_failed",
			Detail:      err.Error(),
		}
	}
	out := siblingRebaseDispatchResult{Siblings: len(siblings)}
	if len(siblings) == 0 {
		return out
	}

	// Dial Temporal once; reuse the client across siblings. A dial failure
	// fails the whole dispatch — no point trying to start workflows we can't
	// reach.
	if dialer == nil {
		dialer = dialTemporalAsStarter
	}
	c, host, err := dialer(ctx, in.TemporalHost)
	if err != nil {
		warnDispatch(stderr, "temporal dial %s failed: %v", host, err)
		return siblingRebaseDispatchResult{
			Siblings:    len(siblings),
			FailureKind: "temporal_unreachable",
			Detail:      err.Error(),
		}
	}
	defer c.Close()

	baseBranch := in.BaseBranch
	if baseBranch == "" {
		baseBranch = "main"
	}

	for _, s := range siblings {
		// Deterministic WorkflowID per (sibling PR, source PR) so a duplicate
		// webhook delivery (GitHub's at-least-once contract) does not start
		// a second rebase — Temporal's WorkflowID uniqueness rejects the
		// repeat with an AlreadyStarted error which we treat as no-op.
		workflowID := fmt.Sprintf(
			"sibling-rebase-pr-%d-from-%d",
			s.Number, in.SourcePRNumber)
		startOpts := client.StartWorkflowOptions{
			ID:        workflowID,
			TaskQueue: TaskQueue,
			// REJECT_DUPLICATE makes the dedup explicit on the server side:
			// a redelivered webhook attempting the same WorkflowID gets the
			// AlreadyStarted error rather than racing with the prior run.
			WorkflowIDReusePolicy: enumspb.WORKFLOW_ID_REUSE_POLICY_REJECT_DUPLICATE,
		}
		wfIn := workflows.SiblingRebaseInput{
			PRNumber:       s.Number,
			PRBranch:       s.HeadRefName,
			TargetRepo:     in.TargetRepo,
			BaseBranch:     baseBranch,
			SchedulerRunID: in.SchedulerRunID,
			SourcePRNumber: in.SourcePRNumber,
			Repo:           in.Repo,
		}
		if _, err := c.ExecuteWorkflow(ctx, startOpts, workflows.SiblingRebaseWorkflow, wfIn); err != nil {
			// AlreadyStarted is the dedup-success path: the workflow exists
			// from a prior webhook delivery for the same merge. Count it as
			// dispatched (the work is in flight or completed) rather than as
			// a failure, so GitHub redeliveries don't surface as faults.
			if isAlreadyStartedErr(err) {
				out.Dispatched++
				out.PRNumbers = append(out.PRNumbers, s.Number)
				continue
			}
			warnDispatch(stderr, "dispatch for PR #%d failed: %v", s.Number, err)
			if out.FailureKind == "" {
				out.FailureKind = "dispatch_error"
				out.Detail = err.Error()
			}
			continue
		}
		out.Dispatched++
		out.PRNumbers = append(out.PRNumbers, s.Number)
	}
	sortIntsAsc(out.PRNumbers)
	return out
}

// isAlreadyStartedErr reports whether err is Temporal's "workflow already
// started" error — surfaced by ExecuteWorkflow when a deterministic
// WorkflowID conflicts with a prior, still-known run. Treated as a dedup
// success by the dispatcher.
func isAlreadyStartedErr(err error) bool {
	if err == nil {
		return false
	}
	var aErr *serviceerror.WorkflowExecutionAlreadyStarted
	if errors.As(err, &aErr) {
		return true
	}
	// Some Temporal SDK versions wrap or stringify the error; substring match
	// is the safety net so a future SDK rename does not silently break dedup.
	return strings.Contains(err.Error(), "WorkflowExecutionAlreadyStarted") ||
		strings.Contains(err.Error(), "already started")
}

// warnDispatch logs a sibling-rebase dispatch warning, tolerating a nil
// writer (the factory-listen handler always sets stderr to os.Stderr; tests
// or direct callers may pass nil).
func warnDispatch(stderr io.Writer, format string, args ...any) {
	if stderr == nil {
		return
	}
	fmt.Fprintf(stderr, "warning: sibling-rebase: "+format+"\n", args...)
}

// listOpenSiblingsViaGH shells out to `gh pr list --repo <repo> --label
// <label> --state open --json number,headRefName` and returns every entry
// whose number differs from excludePR. The default lister in production —
// tests inject a stub.
func listOpenSiblingsViaGH(ctx context.Context, repo, label string, excludePR int) ([]siblingListEntry, error) {
	if _, err := exec.LookPath("gh"); err != nil {
		return nil, fmt.Errorf("gh CLI not available: %w", err)
	}
	args := []string{
		"pr", "list",
		"--label", label,
		"--state", "open",
		"--json", "number,headRefName",
		"--limit", "100",
	}
	if repo != "" {
		args = append(args, "--repo", repo)
	}
	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, "gh", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("gh pr list: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	var entries []siblingListEntry
	if err := json.Unmarshal(stdout.Bytes(), &entries); err != nil {
		return nil, fmt.Errorf("gh pr list json decode: %w", err)
	}
	out := entries[:0]
	for _, e := range entries {
		if e.Number == excludePR {
			continue
		}
		if e.HeadRefName == "" {
			continue
		}
		out = append(out, e)
	}
	return out, nil
}

// labelSchedRunID returns the run id encoded in a `sched/run/<id>` label
// name, or "" if name does not match the convention. The first matching
// label wins — siblings carry exactly one sched/run/* by construction.
func labelSchedRunID(labels []struct {
	Name string `json:"name"`
}) string {
	for _, l := range labels {
		if strings.HasPrefix(l.Name, activities.SchedRunLabelPrefix) {
			runID := strings.TrimPrefix(l.Name, activities.SchedRunLabelPrefix)
			if runID != "" {
				return runID
			}
		}
	}
	return ""
}

// sortIntsAsc sorts s in place ascending. Tiny insertion sort — sibling
// counts are always small.
func sortIntsAsc(s []int) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j-1] > s[j]; j-- {
			s[j-1], s[j] = s[j], s[j-1]
		}
	}
}
