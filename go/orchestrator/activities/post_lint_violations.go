package activities

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// LintViolation is one structured finding emitted by the spec linter (spec
// 115 FR-003 — L01..L07). The shape matches the JSON the `chitin-orchestrator
// spec-lint` subcommand prints on stdout, so violations round-trip from the
// linter into this activity without translation.
type LintViolation struct {
	Rule     string `json:"rule"`
	File     string `json:"file"`
	Line     int    `json:"line"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
}

// PostLintViolationsInput is the typed input to the PostLintViolations
// activity (spec 115 FR-004): for one spec PR, post the linter's
// error-severity violations as inline review comments, deduped against
// chitin's prior posts so a re-run on the same PR doesn't double-post.
type PostLintViolationsInput struct {
	// PRNumber is the spec pull request to comment on.
	PRNumber int `json:"pr_number"`
	// Repo is the GitHub owner/name pair (e.g. "chitinhq/chitin") used in
	// the `gh api repos/<owner>/<repo>/...` path.
	Repo string `json:"repo"`
	// Violations is the full set the linter emitted — both severities. The
	// activity filters to severity=="error" before posting (warnings are
	// informational per spec 115 edge cases, only errors gate iteration).
	Violations []LintViolation `json:"violations"`
}

// PostLintViolationsResult is the typed outcome of one PostLintViolations
// call. The activity is fail-soft: every outcome — gh fault, dedup-only,
// no errors to post — folds into the result so the workflow settles
// without retrying a non-transient API problem.
type PostLintViolationsResult struct {
	// PostedCount is the number of new inline comments included in the
	// review POST. Zero when everything was deduped or filtered.
	PostedCount int `json:"posted_count"`
	// DedupedCount is the number of error-severity violations skipped
	// because a chitin-authored marker for the same (rule, file, line)
	// already exists on the PR.
	DedupedCount int `json:"deduped_count"`
	// SkippedNonError is the number of input violations whose severity
	// is not "error" — warnings the linter emitted but FR-004 doesn't
	// gate on.
	SkippedNonError int `json:"skipped_non_error"`
	// ReviewID is the GitHub review id returned by the POST. Zero when
	// no review was created (deduped-only, no errors, or POST failed).
	ReviewID int64 `json:"review_id"`
	// Explanation is a human-readable account of how far the post got.
	Explanation string `json:"explanation"`
}

// PostLintViolations is the spec 115 FR-004 activity. It posts the
// linter's error-severity violations as inline PR review comments and
// dedupes by (rule, file, line) so a re-run on the same PR doesn't
// double-post.
//
// The dedup signal is a hidden HTML-comment marker baked into every
// chitin-authored comment body (see lintMarker). The activity fetches
// every review comment on the PR, extracts markers, and skips
// violations whose key already has a marker present.
//
// Fail-soft per the spec-113 activity pattern: every outcome (gh fault,
// JSON decode fault, all-deduped) folds into the result with a nil
// error return. The workflow reads PostedCount + Explanation to settle.
type PostLintViolations struct{}

// NewPostLintViolations returns a PostLintViolations activity. It takes
// no dependencies — the activity is a self-contained sequence over gh
// CLI subprocess I/O.
func NewPostLintViolations() *PostLintViolations { return &PostLintViolations{} }

// ActivityName is the stable Temporal activity name.
func (a *PostLintViolations) ActivityName() string { return "PostLintViolations" }

// Execute is the activity entrypoint. Always returns a nil error.
func (a *PostLintViolations) Execute(ctx context.Context, in PostLintViolationsInput) (PostLintViolationsResult, error) {
	var res PostLintViolationsResult

	if in.PRNumber == 0 || strings.TrimSpace(in.Repo) == "" {
		res.Explanation = "missing PRNumber or Repo — nothing posted"
		return res, nil
	}

	errOnly := filterErrorSeverity(in.Violations)
	res.SkippedNonError = len(in.Violations) - len(errOnly)
	if len(errOnly) == 0 {
		res.Explanation = fmt.Sprintf(
			"no error-severity violations to post (%d non-error skipped)",
			res.SkippedNonError)
		return res, nil
	}

	existing, err := fetchExistingLintMarkersFn(ctx, in.Repo, in.PRNumber)
	if err != nil {
		res.Explanation = fmt.Sprintf("fetch existing review comments failed: %v", err)
		return res, nil
	}

	toPost := dedupViolations(errOnly, existing)
	res.DedupedCount = len(errOnly) - len(toPost)
	if len(toPost) == 0 {
		res.Explanation = fmt.Sprintf(
			"all %d error violation(s) already posted; nothing new",
			len(errOnly))
		return res, nil
	}

	reviewID, err := postLintReviewFn(ctx, in.Repo, in.PRNumber, toPost)
	if err != nil {
		res.Explanation = fmt.Sprintf("post review failed: %v", err)
		return res, nil
	}
	res.PostedCount = len(toPost)
	res.ReviewID = reviewID
	res.Explanation = fmt.Sprintf(
		"posted review %d on PR #%d with %d new violation comment(s); %d deduped, %d non-error skipped",
		reviewID, in.PRNumber, res.PostedCount, res.DedupedCount, res.SkippedNonError)
	return res, nil
}

// lintMarkerPrefix and lintMarkerSuffix bracket a hidden HTML comment
// baked into every chitin-authored spec-lint comment. The marker carries
// (rule, file, line) so a future invocation can dedup without parsing
// natural language. GitHub renders HTML comments invisibly in PR UI.
const (
	lintMarkerPrefix = "<!-- chitin-spec-lint:"
	lintMarkerSuffix = " -->"
)

// lintMarker renders the dedup marker for one violation. The format is
// `<!-- chitin-spec-lint:RULE|FILE|LINE -->`. Pipe separators chosen
// because rule ids and file paths never contain them in this codebase.
func lintMarker(rule, file string, line int) string {
	return fmt.Sprintf("%s%s|%s|%d%s",
		lintMarkerPrefix, rule, file, line, lintMarkerSuffix)
}

// parseLintMarker extracts (rule, file, line) from the first
// chitin-spec-lint marker in body. Returns ok=false when no marker is
// present or its payload is malformed.
func parseLintMarker(body string) (rule, file string, line int, ok bool) {
	i := strings.Index(body, lintMarkerPrefix)
	if i < 0 {
		return "", "", 0, false
	}
	rest := body[i+len(lintMarkerPrefix):]
	j := strings.Index(rest, lintMarkerSuffix)
	if j < 0 {
		return "", "", 0, false
	}
	parts := strings.SplitN(rest[:j], "|", 3)
	if len(parts) != 3 {
		return "", "", 0, false
	}
	n, err := strconv.Atoi(parts[2])
	if err != nil {
		return "", "", 0, false
	}
	return parts[0], parts[1], n, true
}

// BuildLintCommentBody renders the GitHub review comment body for one
// violation. Includes the hidden dedup marker so a future re-run finds
// it. Exported so tests + the future workflow can assert on the shape.
func BuildLintCommentBody(v LintViolation) string {
	var b strings.Builder
	fmt.Fprintf(&b, "**spec-lint %s** — %s\n\n", v.Rule, v.Severity)
	b.WriteString(strings.TrimSpace(v.Message))
	b.WriteString("\n\n")
	b.WriteString(lintMarker(v.Rule, v.File, v.Line))
	return b.String()
}

// filterErrorSeverity returns the subset of vs whose Severity is
// exactly "error". Warnings flow through the result as SkippedNonError
// for the operator's audit without gating iteration.
func filterErrorSeverity(vs []LintViolation) []LintViolation {
	out := make([]LintViolation, 0, len(vs))
	for _, v := range vs {
		if v.Severity == "error" {
			out = append(out, v)
		}
	}
	return out
}

// dedupViolations returns the subset of vs whose (rule, file, line)
// key is NOT already present in existing. Order is preserved so the
// posted review comments appear in the linter's emission order.
func dedupViolations(vs []LintViolation, existing map[string]struct{}) []LintViolation {
	out := make([]LintViolation, 0, len(vs))
	for _, v := range vs {
		key := violationKey(v.Rule, v.File, v.Line)
		if _, present := existing[key]; present {
			continue
		}
		out = append(out, v)
	}
	return out
}

// violationKey is the dedup key joining (rule, file, line). Centralised
// so the marker writer + reader can't drift.
func violationKey(rule, file string, line int) string {
	return fmt.Sprintf("%s|%s|%d", rule, file, line)
}

// fetchExistingLintMarkersFn is the package-level hook the activity
// calls to discover previously-posted markers. Tests reassign it to a
// stub so Execute is exercised without `gh` on PATH.
var fetchExistingLintMarkersFn = fetchExistingLintMarkers

// postLintReviewFn is the package-level hook the activity calls to POST
// the new review. Tests reassign it to a stub for hermetic coverage.
var postLintReviewFn = postLintReview

// fetchExistingLintMarkers fetches every review comment on the PR via
// `gh api --paginate` and returns the set of (rule|file|line) keys
// embedded in chitin-authored markers. Uses --paginate so a PR with >30
// comments doesn't silently miss markers past page one.
func fetchExistingLintMarkers(ctx context.Context, repo string, prNumber int) (map[string]struct{}, error) {
	path := fmt.Sprintf("repos/%s/pulls/%d/comments?per_page=100", repo, prNumber)
	raw, err := ghApiPaginated(ctx, path)
	if err != nil {
		return nil, err
	}
	var comments []struct {
		Body string `json:"body"`
	}
	if err := json.Unmarshal(raw, &comments); err != nil {
		return nil, fmt.Errorf("decode pr comments: %w", err)
	}
	out := make(map[string]struct{}, len(comments))
	for _, c := range comments {
		rule, file, line, ok := parseLintMarker(c.Body)
		if !ok {
			continue
		}
		out[violationKey(rule, file, line)] = struct{}{}
	}
	return out, nil
}

// postLintReview POSTs one PR review carrying one inline comment per
// violation. Body shape per the GitHub Reviews API:
//
//	{ "event": "COMMENT", "comments": [ {path, line, side, body}, ... ] }
//
// Submitted via `gh api --method POST --input - <path>` with the JSON
// body on stdin (gh's `-F` flag doesn't accept nested arrays, so stdin
// is the canonical path for non-scalar review payloads).
func postLintReview(ctx context.Context, repo string, prNumber int, vs []LintViolation) (int64, error) {
	type apiComment struct {
		Path string `json:"path"`
		Line int    `json:"line"`
		Side string `json:"side"`
		Body string `json:"body"`
	}
	comments := make([]apiComment, 0, len(vs))
	for _, v := range vs {
		comments = append(comments, apiComment{
			Path: v.File,
			Line: v.Line,
			Side: "RIGHT",
			Body: BuildLintCommentBody(v),
		})
	}
	payload := map[string]any{
		"event":    "COMMENT",
		"comments": comments,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return 0, fmt.Errorf("marshal review body: %w", err)
	}
	path := fmt.Sprintf("repos/%s/pulls/%d/reviews", repo, prNumber)
	stdout, err := ghApiPostJSON(ctx, path, body)
	if err != nil {
		return 0, err
	}
	var resp struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(stdout, &resp); err != nil {
		return 0, fmt.Errorf("decode review response: %w", err)
	}
	return resp.ID, nil
}

// ghApiPostJSON runs `gh api --method POST --input - <path>` with body
// piped on stdin and returns the response stdout. Mirrors the existing
// `ghApi` / `ghApiPaginated` helpers in pr_iteration.go; duplicated here
// rather than promoted to a shared helper to keep the spec-115 activity
// self-contained (the same pragmatic call as the activity-package-
// private `gitInWorktree` helper makes in pr_iteration.go).
func ghApiPostJSON(ctx context.Context, path string, body []byte) ([]byte, error) {
	if _, err := exec.LookPath("gh"); err != nil {
		return nil, fmt.Errorf("gh CLI not available: %w", err)
	}
	cmd := exec.CommandContext(ctx, "gh", "api", "--method", "POST", "--input", "-", path)
	cmd.Stdin = bytes.NewReader(body)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("gh api POST %s: %w: %s",
			path, err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}
