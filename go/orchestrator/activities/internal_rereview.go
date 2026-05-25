package activities

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/chitinhq/chitin/go/orchestrator/activities/review/verdict"
	"github.com/chitinhq/chitin/go/orchestrator/driver"
	"github.com/chitinhq/chitin/go/orchestrator/worktree"
)

// DispatchInternalReviewInput is the typed input to the internal re-review
// activity (spec 116 US1). Called sequentially from spec 113's
// PRIterationWorkflow on the PushedFixup==true path: spec 113 ships a
// fixup commit, then this activity asks a different driver whether the
// fixup actually addressed the original Copilot comments.
type DispatchInternalReviewInput struct {
	// PRNumber is the chitin-authored PR whose fixup was just pushed.
	PRNumber int `json:"pr_number"`
	// PRBranch is the head branch — used for worktree.Manager.Checkout.
	PRBranch string `json:"pr_branch"`
	// Repo is the GitHub owner/name pair, used for gh-api review fetch
	// and PR-URL construction.
	Repo string `json:"repo"`
	// TargetRepo is the absolute path to the local working repository
	// the worktree Manager mints its checkout under.
	TargetRepo string `json:"target_repo"`
	// FixupSHA is the SHA of the fixup commit spec 113 just pushed.
	// The activity diffs this commit (via `git show <sha>`) to feed
	// the re-reviewer the exact changes being assessed.
	FixupSHA string `json:"fixup_sha"`
	// OriginalReviewID is the Copilot review id whose comments spec 113
	// iterated. Re-reviewer reads those comments to check whether each
	// was addressed.
	OriginalReviewID int64 `json:"original_review_id"`
	// FixupAuthor is the driver id that authored the fixup. Excluded
	// from the re-reviewer pool per spec 094 R-AUTHORID — no
	// self-review.
	FixupAuthor string `json:"fixup_author"`
	// WorkUnitID is the orchestration handle for this re-review; used
	// to slug the worktree directory.
	WorkUnitID string `json:"work_unit_id"`
}

// DispatchInternalReviewResult is the typed outcome. The activity always
// returns a nil error and folds every outcome — empty pool, driver fault,
// malformed verdict, valid verdict — into the result so the workflow
// settles without blind retries.
type DispatchInternalReviewResult struct {
	// Skipped is true when the re-review didn't run at all (empty pool
	// after R-AUTHORID exclusion, no registry bound, etc.). When true,
	// Verdict / Confidence / Body are all empty and SkipReason names the
	// cause for telemetry routing.
	Skipped bool `json:"skipped"`
	// SkipReason is populated when Skipped is true. Matches the
	// PoolSelectionReason taxonomy from the pool resolver.
	SkipReason string `json:"skip_reason,omitempty"`
	// ReviewerDriver is the driver id that ran the re-review. Empty when
	// Skipped is true.
	ReviewerDriver string `json:"reviewer_driver,omitempty"`
	// Verdict is the parsed verdict enum on success. Empty on Skipped
	// or on a malformed-verdict failure path.
	Verdict verdict.Enum `json:"verdict,omitempty"`
	// Confidence is the reviewer's confidence (high|medium|low). Normalized
	// to medium on the empty default per spec 116 FR-009.
	Confidence verdict.Confidence `json:"confidence,omitempty"`
	// Body is the canonical JSON of the validated StructuredVerdict —
	// what gets posted as a PR review by the follow-on
	// PostStructuredReview activity. Empty on Skipped / failure.
	Body string `json:"body,omitempty"`
	// FailureKind names the failure mode when Skipped is false but
	// Body is empty (driver fault, malformed verdict, etc.). Empty on
	// success.
	FailureKind string `json:"failure_kind,omitempty"`
	// Explanation is a human-readable account of the outcome.
	Explanation string `json:"explanation"`
}

// DispatchInternalReview is the spec 116 US1 internal re-review activity.
// It picks a different driver from the pool (R-AUTHORID-excluded against
// the fixup author), gives it the fixup diff + original review comments +
// PR description, asks for a StructuredVerdict, and returns the canonical
// JSON the workflow's follow-on activities will post + label-act on.
//
// Reuses the spec-113 / spec-112-US2 infrastructure: same worktree.Manager
// .Checkout for PR-branch checkout, same fail-soft activity contract (no
// Temporal error on any outcome), same shell-out style for git commands.
type DispatchInternalReview struct {
	manager  *worktree.Manager
	registry *driver.Registry
}

// NewDispatchInternalReview returns the activity bound to mgr + registry.
// Both required — driver lookup is by id from the registry.
func NewDispatchInternalReview(mgr *worktree.Manager, reg *driver.Registry) *DispatchInternalReview {
	return &DispatchInternalReview{manager: mgr, registry: reg}
}

// ActivityName is the stable Temporal name.
func (a *DispatchInternalReview) ActivityName() string { return "DispatchInternalReview" }

// Execute runs the re-review. Always returns nil error per the
// fail-soft contract.
func (a *DispatchInternalReview) Execute(ctx context.Context, in DispatchInternalReviewInput) (DispatchInternalReviewResult, error) {
	if a.manager == nil || a.registry == nil {
		return DispatchInternalReviewResult{
			Skipped:     true,
			SkipReason:  string(PoolReasonNoRegistry),
			Explanation: "no Manager or Registry bound — re-review not attempted",
		}, nil
	}
	if in.PRBranch == "" || in.TargetRepo == "" || in.Repo == "" || in.FixupSHA == "" || in.OriginalReviewID == 0 {
		return DispatchInternalReviewResult{
			Skipped:     true,
			SkipReason:  "invalid_input",
			Explanation: "missing required field (PRBranch / TargetRepo / Repo / FixupSHA / OriginalReviewID)",
		}, nil
	}

	// Resolve the re-reviewer driver. Empty selection short-circuits to
	// Skipped with the closed PoolSelectionReason.
	sel := resolveRereviewerDriver(a.registry, in.FixupAuthor)
	if sel.DriverID == "" {
		return DispatchInternalReviewResult{
			Skipped:     true,
			SkipReason:  string(sel.Reason),
			Explanation: fmt.Sprintf("no eligible re-reviewer: %s (fixup_author=%s)", sel.Reason, in.FixupAuthor),
		}, nil
	}

	workUnitID := in.WorkUnitID
	if workUnitID == "" {
		workUnitID = fmt.Sprintf("rereview-pr-%d-review-%d", in.PRNumber, in.OriginalReviewID)
	}

	// Mint a dedicated checkout of the PR branch — same Manager.Checkout
	// path that spec 112 US2 + spec 113 use. Fetches origin first so the
	// fixup commit is present locally.
	wt, err := a.manager.Checkout(in.TargetRepo, in.PRBranch, workUnitID)
	if err != nil {
		return DispatchInternalReviewResult{
			ReviewerDriver: sel.DriverID,
			FailureKind:    "worktree_checkout_failed",
			Explanation:    fmt.Sprintf("worktree checkout failed: %v", err),
		}, nil
	}
	defer func() { _ = a.manager.Teardown(wt) }()

	// Diff the fixup commit — the precise change the reviewer is judging.
	fixupDiff, err := gitInWorktree(ctx, wt, "show", "--no-color", in.FixupSHA)
	if err != nil {
		return DispatchInternalReviewResult{
			ReviewerDriver: sel.DriverID,
			FailureKind:    "fixup_diff_failed",
			Explanation:    fmt.Sprintf("git show %s failed: %v", in.FixupSHA, err),
		}, nil
	}

	// Re-fetch the original review's body + line comments (reusing the
	// spec 113 fetchReviewContext helper from pr_iteration.go — same
	// gh-api endpoints, same shape).
	origReview, err := fetchReviewContext(ctx, in.Repo, in.PRNumber, in.OriginalReviewID)
	if err != nil {
		return DispatchInternalReviewResult{
			ReviewerDriver: sel.DriverID,
			FailureKind:    "original_review_fetch_failed",
			Explanation:    fmt.Sprintf("fetch original review %d: %v", in.OriginalReviewID, err),
		}, nil
	}

	// Build the re-review prompt + invoke the driver.
	prompt := BuildInternalReviewPrompt(in, fixupDiff, origReview)
	wu := driver.WorkUnit{
		ID:           workUnitID,
		SpecID:       "116",
		TaskID:       "internal-rereview",
		Context:      prompt,
		WorktreePath: wt,
	}
	d, ok := a.registry.Driver(sel.DriverID)
	if !ok {
		// Pool resolver said this driver is registered; if Lookup says
		// otherwise we lost a race against registration shutdown.
		return DispatchInternalReviewResult{
			ReviewerDriver: sel.DriverID,
			FailureKind:    "driver_unregistered",
			Explanation:    fmt.Sprintf("driver %q was in the pool but no longer registered", sel.DriverID),
		}, nil
	}
	invRes, invErr := d.Invoke(ctx, wu)
	if invErr != nil {
		return DispatchInternalReviewResult{
			ReviewerDriver: sel.DriverID,
			FailureKind:    "driver_invocation_faulted",
			Explanation:    fmt.Sprintf("driver %s invocation faulted: %v", sel.DriverID, invErr),
		}, nil
	}
	if invRes.Status != driver.StatusSucceeded {
		return DispatchInternalReviewResult{
			ReviewerDriver: sel.DriverID,
			FailureKind:    "driver_returned_non_success",
			Explanation:    fmt.Sprintf("driver %s returned status %s: %s", sel.DriverID, invRes.Status, invRes.Explanation),
		}, nil
	}

	// Parse the StructuredVerdict from the driver's output. Spec 094's
	// extract+unmarshal+validate path is wrapped in the claudecode /
	// codex driver layer normally; here we operate on Result.Explanation
	// directly (the spec 094 contract).
	body := strings.TrimSpace(invRes.Explanation)
	var v verdict.StructuredVerdict
	if err := json.Unmarshal([]byte(body), &v); err != nil {
		return DispatchInternalReviewResult{
			ReviewerDriver: sel.DriverID,
			FailureKind:    "malformed_verdict_json",
			Explanation:    fmt.Sprintf("driver output not StructuredVerdict JSON: %v; raw: %s", err, truncateForReview(body)),
		}, nil
	}
	if err := verdict.Validate(v); err != nil {
		return DispatchInternalReviewResult{
			ReviewerDriver: sel.DriverID,
			FailureKind:    "verdict_validation_failed",
			Explanation:    fmt.Sprintf("verdict validation failed: %v; raw: %s", err, truncateForReview(body)),
		}, nil
	}

	// Normalize confidence to default-medium per spec 116 FR-009.
	v.Confidence = v.Confidence.Normalize()

	// Re-marshal so the body the workflow posts is canonical (sorted keys
	// via json.Marshal's struct-tag order; no stray whitespace from the
	// driver).
	canonical, err := json.Marshal(v)
	if err != nil {
		return DispatchInternalReviewResult{
			ReviewerDriver: sel.DriverID,
			FailureKind:    "canonical_marshal_failed",
			Explanation:    fmt.Sprintf("verdict re-marshal: %v", err),
		}, nil
	}

	return DispatchInternalReviewResult{
		ReviewerDriver: sel.DriverID,
		Verdict:        v.Verdict,
		Confidence:     v.Confidence,
		Body:           string(canonical),
		Explanation: fmt.Sprintf(
			"re-reviewed PR #%d fixup %s with driver %s: verdict=%s confidence=%s",
			in.PRNumber, shortHex(in.FixupSHA), sel.DriverID, v.Verdict, v.Confidence),
	}, nil
}

// BuildInternalReviewPrompt assembles the re-review prompt. Pure function
// — exported so tests can assert on the shape independently of the
// activity's IO.
//
// The prompt asks the driver to verify whether the fixup commit addressed
// the original Copilot review's comments, and to emit a StructuredVerdict
// (spec 094) JSON envelope as its ONLY output. Includes the fixup diff,
// each original comment, and the verdict-shape contract inline.
func BuildInternalReviewPrompt(in DispatchInternalReviewInput, fixupDiff string, origReview reviewContext) string {
	var b strings.Builder
	fmt.Fprintf(&b,
		"You are re-reviewing a fixup commit on PR #%d. The fixup was authored by driver %q "+
			"in response to a Copilot review. Your job: decide whether the fixup addressed the "+
			"original comments well enough to merge.\n\n",
		in.PRNumber, in.FixupAuthor)
	b.WriteString("You MUST emit exactly ONE StructuredVerdict JSON document as your output ")
	b.WriteString("(no surrounding prose, no markdown fence). The schema is:\n\n")
	b.WriteString("```\n")
	b.WriteString(`{"verdict":"approve|approve-with-comments|request-changes|abstain",` + "\n")
	b.WriteString(` "concerns":["..."], "recommendations":["..."], "blockers":["..."],` + "\n")
	b.WriteString(` "confidence":"high|medium|low"}` + "\n")
	b.WriteString("```\n\n")
	b.WriteString("Invariants: `approve` requires empty blockers. `approve-with-comments` requires empty blockers AND at least one concern or recommendation. `request-changes` requires non-empty blockers. `confidence` defaults to medium if omitted; use `low` if you're not sure whether the fixup is correct.\n\n")
	if strings.TrimSpace(origReview.Body) != "" {
		fmt.Fprintf(&b, "ORIGINAL COPILOT REVIEW BODY:\n%s\n\n", strings.TrimSpace(origReview.Body))
	}
	if len(origReview.LineComments) > 0 {
		b.WriteString("ORIGINAL COPILOT LINE COMMENTS (each one is what the fixup is supposed to address):\n")
		for i, c := range origReview.LineComments {
			fmt.Fprintf(&b, "  [%d] %s:%d\n      %s\n", i+1, c.Path, c.Line, strings.TrimSpace(c.Body))
		}
		b.WriteString("\n")
	}
	b.WriteString("FIXUP COMMIT DIFF (what the authoring driver actually changed):\n")
	b.WriteString("```\n")
	b.WriteString(strings.TrimSpace(fixupDiff))
	b.WriteString("\n```\n\n")
	b.WriteString("Now emit ONLY the StructuredVerdict JSON. Do not run tests. Do not write commit messages. ")
	b.WriteString("Output a single JSON document and exit.\n")
	return b.String()
}

// truncateForReview caps long driver output for inclusion in failure
// explanations. 1 KiB is the same cap the spec 109/110 driver wrappers
// use for malformed_verdict explanations.
func truncateForReview(s string) string {
	const cap = 1024
	if len(s) <= cap {
		return s
	}
	return s[:cap]
}
