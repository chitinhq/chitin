package activities

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"github.com/chitinhq/chitin/go/orchestrator/activities/review/verdict"
)

// ReadyToMergeLabel is the spec 116 PR label applied when an internal
// re-review returns an approve-shaped verdict. It signals to the operator
// (and to any future auto-merge worker) that the PR has cleared the
// re-review bar and is safe to merge — distinct from a Copilot-only
// approval which only signals "someone looked".
const ReadyToMergeLabel = "chitin/ready-to-merge"

// PostStructuredReviewInput is the typed input to the spec 116
// PostStructuredReview activity. Called after DispatchInternalReview
// returns a successful (Body non-empty) verdict — the JSON body is
// posted as a PR review with the matching review event.
type PostStructuredReviewInput struct {
	// PRNumber is the chitin-authored PR to post the review on.
	PRNumber int `json:"pr_number"`
	// Repo is the GitHub owner/name for the gh-api call.
	Repo string `json:"repo"`
	// WorktreePath is a checkout (any worktree under the PR's repo will
	// do — gh infers the repo from `origin`). Required by the gh CLI's
	// "current repo" detection.
	WorktreePath string `json:"worktree_path"`
	// ReviewerDriver is the driver id that produced this verdict — used
	// in the review body header so the human reader knows who reviewed.
	ReviewerDriver string `json:"reviewer_driver"`
	// VerdictBody is the canonical StructuredVerdict JSON returned by
	// DispatchInternalReview.Body. Must parse cleanly; this activity
	// re-validates rather than trusting the upstream caller.
	VerdictBody string `json:"verdict_body"`
}

// PostStructuredReviewResult is the typed outcome.
type PostStructuredReviewResult struct {
	// Posted is true when the gh-api call succeeded and the review is
	// live on the PR.
	Posted bool `json:"posted"`
	// ReviewEvent names the gh review event the activity used
	// (APPROVE / REQUEST_CHANGES / COMMENT).
	ReviewEvent string `json:"review_event,omitempty"`
	// FailureKind names why Posted is false. Empty on success.
	FailureKind string `json:"failure_kind,omitempty"`
	// Explanation is the human-readable account.
	Explanation string `json:"explanation"`
}

// PostStructuredReview posts the spec-116 internal re-review verdict to
// GitHub as a PR review. Fail-soft — every outcome lands in the result;
// the activity never returns a Temporal error so the workflow doesn't
// blind-retry a duplicate-review post (which gh treats as an error after
// the first call, masking the original failure mode).
type PostStructuredReview struct{}

// NewPostStructuredReview returns the activity. No bindings — the
// activity shells out to gh.
func NewPostStructuredReview() *PostStructuredReview { return &PostStructuredReview{} }

// ActivityName is the stable Temporal name.
func (a *PostStructuredReview) ActivityName() string { return "PostStructuredReview" }

// Execute posts the review. Always returns nil error.
func (a *PostStructuredReview) Execute(ctx context.Context, in PostStructuredReviewInput) (PostStructuredReviewResult, error) {
	if in.PRNumber == 0 || in.Repo == "" || in.WorktreePath == "" || in.VerdictBody == "" {
		return PostStructuredReviewResult{
			FailureKind: "invalid_input",
			Explanation: "missing required field (PRNumber / Repo / WorktreePath / VerdictBody)",
		}, nil
	}

	// Re-parse so we know what event to use. We've already validated
	// upstream; re-doing it here keeps this activity self-defending if
	// the contract upstream ever loosens.
	var v verdict.StructuredVerdict
	if err := json.Unmarshal([]byte(in.VerdictBody), &v); err != nil {
		return PostStructuredReviewResult{
			FailureKind: "verdict_body_unparseable",
			Explanation: fmt.Sprintf("verdict body not StructuredVerdict JSON: %v", err),
		}, nil
	}
	if err := verdict.Validate(v); err != nil {
		return PostStructuredReviewResult{
			FailureKind: "verdict_body_invalid",
			Explanation: fmt.Sprintf("verdict body failed validation: %v", err),
		}, nil
	}

	event := reviewEventFor(v.Verdict)
	if event == "" {
		return PostStructuredReviewResult{
			FailureKind: "no_review_event_for_verdict",
			Explanation: fmt.Sprintf("no gh review event mapping for verdict %q", string(v.Verdict)),
		}, nil
	}

	body := renderReviewBody(in.ReviewerDriver, v, in.VerdictBody)

	prRef := fmt.Sprintf("%d", in.PRNumber)
	cmd := exec.CommandContext(ctx, "gh", "pr", "review", prRef,
		"--repo", in.Repo,
		"--"+strings.ToLower(eventFlag(event)),
		"--body", body,
	)
	cmd.Dir = in.WorktreePath
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return PostStructuredReviewResult{
			FailureKind: "gh_pr_review_failed",
			ReviewEvent: event,
			Explanation: fmt.Sprintf("gh pr review %s failed: %v: %s",
				prRef, err, strings.TrimSpace(stderr.String())),
		}, nil
	}
	return PostStructuredReviewResult{
		Posted:      true,
		ReviewEvent: event,
		Explanation: fmt.Sprintf("posted %s review on PR #%d by %s", event, in.PRNumber, in.ReviewerDriver),
	}, nil
}

// reviewEventFor maps a verdict enum to the gh review event flag. Both
// approve-shaped values map to APPROVE — the dialectic gate's distinction
// between Approve and ApproveWithComments lives in the body, not in the
// gh event. Abstain is intentionally posted as a COMMENT event (not
// dropped silently) so the operator sees the re-reviewer's abstention on
// the PR timeline.
func reviewEventFor(v verdict.Enum) string {
	switch v {
	case verdict.Approve, verdict.ApproveWithComments:
		return "APPROVE"
	case verdict.RequestChanges:
		return "REQUEST_CHANGES"
	case verdict.Abstain:
		return "COMMENT"
	default:
		return ""
	}
}

// eventFlag returns the gh CLI flag name for a review event (gh pr review
// uses --approve, --request-changes, --comment — not the GraphQL event
// names directly).
func eventFlag(event string) string {
	switch event {
	case "APPROVE":
		return "approve"
	case "REQUEST_CHANGES":
		return "request-changes"
	case "COMMENT":
		return "comment"
	default:
		return "comment"
	}
}

// renderReviewBody formats the PR-review body. The body is BOTH
// human-readable (header + concerns/recommendations/blockers as bullets)
// AND machine-readable (the canonical StructuredVerdict JSON in a fenced
// block at the bottom). The dialectic gate and any downstream automation
// parse the fenced JSON; humans read the bullets. Keeping both shapes in
// one body — rather than two posts — preserves the spec 094 contract
// that a verdict is one indivisible record.
func renderReviewBody(reviewer string, v verdict.StructuredVerdict, canonical string) string {
	var b strings.Builder
	confidence := v.Confidence.Normalize()
	fmt.Fprintf(&b, "**Internal re-review by `%s`** — verdict: `%s` (confidence: `%s`)\n\n",
		reviewer, v.Verdict, confidence)
	if len(v.Blockers) > 0 {
		b.WriteString("**Blockers:**\n")
		for _, s := range v.Blockers {
			fmt.Fprintf(&b, "- %s\n", s)
		}
		b.WriteString("\n")
	}
	if len(v.Concerns) > 0 {
		b.WriteString("**Concerns:**\n")
		for _, s := range v.Concerns {
			fmt.Fprintf(&b, "- %s\n", s)
		}
		b.WriteString("\n")
	}
	if len(v.Recommendations) > 0 {
		b.WriteString("**Recommendations:**\n")
		for _, s := range v.Recommendations {
			fmt.Fprintf(&b, "- %s\n", s)
		}
		b.WriteString("\n")
	}
	if v.Reason != "" {
		fmt.Fprintf(&b, "**Reason:** %s\n\n", v.Reason)
	}
	b.WriteString("<details><summary>StructuredVerdict JSON (spec 094)</summary>\n\n")
	b.WriteString("```json\n")
	b.WriteString(canonical)
	b.WriteString("\n```\n")
	b.WriteString("</details>\n")
	b.WriteString("\n_Posted by the Chitin orchestrator (spec 116 internal re-review)._\n")
	return b.String()
}

// ApplyReadyToMergeLabelInput is the typed input. PRURL is preferred over
// (Repo, PRNumber) because applyPRLabel speaks gh-URL-or-number-equally;
// the spec 113 / 116 callers already have the URL handy from the
// pull_request_review webhook.
type ApplyReadyToMergeLabelInput struct {
	// PRURL is the full https://github.com/owner/name/pull/N URL.
	PRURL string `json:"pr_url"`
	// WorktreePath is any checkout under the same repo; gh uses it to
	// resolve the owner/name pair.
	WorktreePath string `json:"worktree_path"`
}

// ApplyReadyToMergeLabelResult is the typed outcome.
type ApplyReadyToMergeLabelResult struct {
	// Applied is true when the chitin/ready-to-merge label is now on
	// the PR.
	Applied bool `json:"applied"`
	// FailureKind names why Applied is false. Empty on success.
	FailureKind string `json:"failure_kind,omitempty"`
	// Explanation is the human-readable account.
	Explanation string `json:"explanation"`
}

// ApplyReadyToMergeLabel adds the chitin/ready-to-merge label to a PR.
// Thin wrapper around the existing applyPRLabel helper from deliver.go —
// keeping the activity boundary distinct lets the workflow record the
// label-apply outcome as its own chain event, separate from the verdict
// post. Fail-soft per the activity contract.
type ApplyReadyToMergeLabel struct{}

// NewApplyReadyToMergeLabel returns the activity.
func NewApplyReadyToMergeLabel() *ApplyReadyToMergeLabel { return &ApplyReadyToMergeLabel{} }

// ActivityName is the stable Temporal name.
func (a *ApplyReadyToMergeLabel) ActivityName() string { return "ApplyReadyToMergeLabel" }

// Execute applies the label. Always returns nil error.
func (a *ApplyReadyToMergeLabel) Execute(ctx context.Context, in ApplyReadyToMergeLabelInput) (ApplyReadyToMergeLabelResult, error) {
	if in.PRURL == "" || in.WorktreePath == "" {
		return ApplyReadyToMergeLabelResult{
			FailureKind: "invalid_input",
			Explanation: "missing required field (PRURL / WorktreePath)",
		}, nil
	}
	if _, err := applyPRLabel(ctx, in.WorktreePath, in.PRURL, ReadyToMergeLabel); err != nil {
		return ApplyReadyToMergeLabelResult{
			FailureKind: "apply_label_failed",
			Explanation: fmt.Sprintf("applyPRLabel %s -> %s: %v", in.PRURL, ReadyToMergeLabel, err),
		}, nil
	}
	return ApplyReadyToMergeLabelResult{
		Applied:     true,
		Explanation: fmt.Sprintf("applied %s to %s", ReadyToMergeLabel, in.PRURL),
	}, nil
}
