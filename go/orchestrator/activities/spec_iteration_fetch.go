package activities

import (
	"context"
	"encoding/json"
	"fmt"
)

// SpecReviewComment is one inline Copilot comment on a spec PR, in the
// shape the SpecIterationWorkflow needs to classify and partition
// (spec 115 T014). Mirrors reviewLineComment's wire fields but is
// exported because the workflow consumes it as activity output.
type SpecReviewComment struct {
	// ID is the GitHub review-comment id; used as the stable key the
	// workflow carries into IterateSpecReview as a filter and into the
	// escalation chain event as the judgement subset.
	ID int64 `json:"id"`
	// Path is the file the comment is anchored to (informational —
	// classification is body-only per FR-007).
	Path string `json:"path"`
	// Line is the file line the comment is anchored to.
	Line int `json:"line"`
	// Body is the comment text the classifier matches against.
	Body string `json:"body"`
}

// FetchSpecReviewCommentsInput is the typed input. Mirrors the upstream
// pr_iteration fetch shape — Repo/PRNumber/ReviewID are the only fields
// the gh-api endpoint needs.
type FetchSpecReviewCommentsInput struct {
	Repo     string `json:"repo"`
	PRNumber int    `json:"pr_number"`
	ReviewID int64  `json:"review_id"`
}

// FetchSpecReviewCommentsResult is the typed output. Comments may be
// empty (a review with only a body and no inline comments — the workflow
// handles that as "no partition, no driver, no escalation"). ReviewBody
// is carried even though T014 doesn't classify it; future telemetry
// (T016) records it in the chain event payload.
type FetchSpecReviewCommentsResult struct {
	ReviewBody string              `json:"review_body"`
	Comments   []SpecReviewComment `json:"comments"`
}

// FetchSpecReviewComments fetches the Copilot review body + every inline
// line comment for a spec-PR review (spec 115 US3 — T014 partition input).
// Identical I/O shape to the spec-113 fetchReviewContext helper that
// powers IteratePRReview; lifted into a standalone activity here because
// the spec-iteration workflow needs the comment bodies BEFORE choosing
// whether to dispatch the driver, so the partition decision can run in
// workflow code (the only side effect — gh api — must run in an activity
// for replay safety).
//
// Pagination matters: `gh api --paginate` walks every Link-header page so
// large reviews (>30 inline comments) classify in full. Without it, a
// judgement comment on page 2 would silently slip into the driver round.
//
// Errors are surfaced (not folded) — a fetch failure must abort the
// workflow rather than dispatch the driver against an empty mechanical
// set and emit a spurious escalation. The workflow upstream catches the
// error and returns a failed result.
func FetchSpecReviewComments(ctx context.Context, in FetchSpecReviewCommentsInput) (FetchSpecReviewCommentsResult, error) {
	var res FetchSpecReviewCommentsResult

	if in.Repo == "" || in.PRNumber <= 0 || in.ReviewID <= 0 {
		return res, fmt.Errorf("FetchSpecReviewComments: Repo, PRNumber, ReviewID are required")
	}

	reviewPath := fmt.Sprintf("repos/%s/pulls/%d/reviews/%d", in.Repo, in.PRNumber, in.ReviewID)
	body, err := ghApi(ctx, reviewPath)
	if err != nil {
		return res, fmt.Errorf("fetch review body: %w", err)
	}
	var reviewMeta struct {
		Body string `json:"body"`
	}
	if err := json.Unmarshal(body, &reviewMeta); err != nil {
		return res, fmt.Errorf("decode review body: %w", err)
	}
	res.ReviewBody = reviewMeta.Body

	commentsPath := fmt.Sprintf("repos/%s/pulls/%d/comments?per_page=100", in.Repo, in.PRNumber)
	commentsRaw, err := ghApiPaginated(ctx, commentsPath)
	if err != nil {
		return res, fmt.Errorf("fetch pr comments: %w", err)
	}
	var allComments []struct {
		ID                  int64  `json:"id"`
		Path                string `json:"path"`
		Line                int    `json:"line"`
		Body                string `json:"body"`
		PullRequestReviewID int64  `json:"pull_request_review_id"`
	}
	if err := json.Unmarshal(commentsRaw, &allComments); err != nil {
		return res, fmt.Errorf("decode pr comments: %w", err)
	}
	for _, c := range allComments {
		if c.PullRequestReviewID != in.ReviewID {
			continue
		}
		res.Comments = append(res.Comments, SpecReviewComment{
			ID:   c.ID,
			Path: c.Path,
			Line: c.Line,
			Body: c.Body,
		})
	}
	return res, nil
}
