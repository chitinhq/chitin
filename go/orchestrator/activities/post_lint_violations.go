package activities

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

// LintViolation is the wire shape one spec-lint rule (FR-003, T002-T009)
// produces per finding. The lint subcommand emits a JSON array of these on
// stdout; the orchestrator decodes them and hands the slice to
// PostLintViolations as input. The shape is also the canonical input
// contract for any future linter so the activity stays decoupled from the
// internal/speclint package's evolution.
type LintViolation struct {
	// Rule is the rule id (e.g. "L01", "L05") that fired. Used as the
	// first segment of the dedup marker so re-running the same rule on
	// the same line is a no-op.
	Rule string `json:"rule"`
	// File is the path the violation points at, repo-relative. May be
	// empty for spec-level violations that aren't anchored to a file
	// (frontmatter-missing on tasks.md, for example); empty-File
	// violations cannot become PR review-line comments, so they are
	// degraded to a single repo-level review-summary comment.
	File string `json:"file"`
	// Line is the 1-indexed line in File the violation points at. Zero
	// means "no anchor" — same treatment as empty File: degrades to a
	// review-summary line rather than a per-line comment.
	Line int `json:"line"`
	// Severity is "error" or "warning". Only "error" violations gate
	// the iteration (spec 115 edge case "linter has a bug and posts
	// false positives") and only error-severity ones are posted by
	// this activity. Warnings are surfaced via chain events only.
	Severity string `json:"severity"`
	// Message is the operator-facing finding text. Posted verbatim as
	// the comment body (with a hidden dedup marker prepended).
	Message string `json:"message"`
}

// PostLintViolationsInput is the typed input to the PostLintViolations
// activity. The workflow constructs it from the spec-lint output (T002)
// plus the PR identifiers it already carries.
type PostLintViolationsInput struct {
	// Repo is the GitHub repo in owner/name form, e.g. "chitinhq/chitin".
	Repo string `json:"repo"`
	// PRNumber is the open spec PR to comment on.
	PRNumber int `json:"pr_number"`
	// CommitSHA is the PR head commit the comments should be anchored
	// to. Required by GitHub's reviews API for line comments; the
	// workflow reads it from the same gh-pr-view that produced the
	// spec-PR discriminator (T001).
	CommitSHA string `json:"commit_sha"`
	// Violations is the full spec-lint output. The activity filters
	// to error-severity and dedupes against existing chitin-authored
	// comments before posting.
	Violations []LintViolation `json:"violations"`
}

// PostLintViolationsResult is the typed outcome of one PostLintViolations
// invocation. The activity always returns a nil error and folds every
// outcome — including a gh-api fault — into the result, mirroring the
// fail-soft contract of DeliverWorkProduct and RebaseSiblingPR.
type PostLintViolationsResult struct {
	// Posted is the number of NEW review comments POSTed in this run
	// (zero on a clean re-run where every error-violation was already
	// commented on a previous round).
	Posted int `json:"posted"`
	// Deduped is the number of error-violations skipped because a
	// chitin-authored comment with the same (rule, file, line) marker
	// already exists on the PR.
	Deduped int `json:"deduped"`
	// Skipped is the number of violations dropped because they had no
	// file/line anchor and could not become a review-line comment.
	Skipped int `json:"skipped"`
	// ReviewID is the GitHub review id created by this run, empty when
	// no comments were posted.
	ReviewID int64 `json:"review_id"`
	// Explanation is a human-readable account of what the activity did.
	Explanation string `json:"explanation"`
}

// PostLintViolations is the spec-115 FR-004 activity: after spec-lint
// (FR-003) runs against a spec PR, this activity surfaces the error-
// severity violations as PR review-line comments so the SpecIterationWorkflow
// can iterate them through the spec-author driver alongside Copilot's
// own review comments. Dedup is by (rule, file, line) — a re-run on the
// same PR doesn't double-post, which lets the linter run every round
// without flooding the PR.
//
// Side effects: at most two `gh api` invocations — one GET to list
// existing review comments for dedup, one POST to create a single
// review with all new line comments attached. Both shell out to the
// operator-host's authenticated `gh`, so this MUST run as an activity,
// never in workflow code (workflow determinism).
//
// The activity carries one injectable dependency — a GhRunner — so unit
// tests can drive it with a fake; production binds the default
// exec.CommandContext("gh", ...) shell-out via a nil runner.
//
// Fail-soft: a gh-api fault on either step is folded into the Result's
// Explanation with a nil error return. The lint outcome is already
// captured in the chain event the workflow emits BEFORE this activity
// runs; missing review comments degrade iteration quality but do not
// halt the gate.
type PostLintViolations struct {
	gh ghRunner
}

// NewPostLintViolations builds a PostLintViolations activity. A nil
// runner uses the default `gh` shell-out; production passes nil.
func NewPostLintViolations(gh ghRunner) *PostLintViolations {
	if gh == nil {
		gh = defaultGhRunner{}
	}
	return &PostLintViolations{gh: gh}
}

// ActivityName is the stable Temporal activity name PostLintViolations
// registers under and SpecIterationWorkflow dispatches to.
func (a *PostLintViolations) ActivityName() string { return "PostLintViolations" }

// Execute posts one PR review comment per error-severity violation in
// input.Violations, deduped by (rule, file, line) against the PR's
// existing review comments. Algorithm:
//
//  1. Filter Violations to severity == "error" with non-empty File and
//     Line > 0. Empty-anchor violations are counted as Skipped — they
//     have no review-line target, so the workflow surfaces them via
//     chain event instead.
//  2. `gh api repos/<repo>/pulls/<N>/comments` to list existing review
//     comments. Parse the chitin dedup marker out of each body.
//  3. Filter out violations whose (rule, file, line) already has a
//     marker on the PR. These count as Deduped.
//  4. If anything remains, POST a single review via
//     `gh api repos/<repo>/pulls/<N>/reviews --input -` with stdin
//     carrying {event: COMMENT, commit_id, comments: [{path, line, body}, ...]}.
//     Each comment body is prefixed with the chitin marker so the next
//     run dedupes against it.
//
// Returns Result.Posted with the count of new comments and ReviewID
// with the created review's id. On any gh fault, Explanation carries
// the detail and the error return is nil — the workflow does not
// retry the comment post, it iterates on whatever Copilot review
// landed.
func (a *PostLintViolations) Execute(ctx context.Context, in PostLintViolationsInput) (PostLintViolationsResult, error) {
	if in.Repo == "" {
		return PostLintViolationsResult{
			Explanation: "no repo — nothing to post",
		}, nil
	}
	if in.PRNumber <= 0 {
		return PostLintViolationsResult{
			Explanation: fmt.Sprintf("invalid PR number %d — nothing to post", in.PRNumber),
		}, nil
	}

	// Step 1 — filter to error-severity violations with a line anchor.
	// Warnings are informational only (spec 115 edge case
	// "linter has a bug and posts false positives"); anchor-less
	// violations cannot become a review-line comment.
	var anchored []LintViolation
	skipped := 0
	for _, v := range in.Violations {
		if v.Severity != "error" {
			continue
		}
		if strings.TrimSpace(v.File) == "" || v.Line <= 0 {
			skipped++
			continue
		}
		anchored = append(anchored, v)
	}
	if len(anchored) == 0 {
		return PostLintViolationsResult{
			Skipped:     skipped,
			Explanation: "no error-severity violations with a file/line anchor",
		}, nil
	}

	// Step 2 — list existing review comments and extract chitin dedup
	// markers. A gh fault here is recoverable: we degrade to "post
	// without dedup" rather than skip the whole activity, because a
	// duplicate comment is less bad than a missing one. The decision
	// is the inverse of CapturePRSnapshot's where the diff is load-
	// bearing — here, dedup is the optimization, not the load-bearing
	// signal.
	existing, listErr := a.listExistingMarkers(ctx, in.Repo, in.PRNumber)
	if listErr != nil {
		// Don't return — proceed without dedup so the operator still
		// gets the comments. Record the degradation in Explanation.
		existing = nil
	}

	// Step 3 — filter out already-posted violations.
	var toPost []LintViolation
	deduped := 0
	for _, v := range anchored {
		if _, ok := existing[markerKey(v)]; ok {
			deduped++
			continue
		}
		toPost = append(toPost, v)
	}
	if len(toPost) == 0 {
		return PostLintViolationsResult{
			Deduped:     deduped,
			Skipped:     skipped,
			Explanation: fmt.Sprintf("all %d error-violations already posted", deduped),
		}, nil
	}

	// Step 4 — POST the review with all new comments. One review with
	// many comments rather than many one-comment reviews is the GitHub-
	// recommended shape and surfaces in the PR as a single review block
	// the iteration loop can address with one fixup commit.
	reviewID, postErr := a.postReview(ctx, in.Repo, in.PRNumber, in.CommitSHA, toPost)
	if postErr != nil {
		res := PostLintViolationsResult{
			Deduped: deduped,
			Skipped: skipped,
			Explanation: fmt.Sprintf(
				"listed %d existing markers, %d new violations to post; gh-api review POST failed: %v",
				len(existing), len(toPost), postErr),
		}
		if listErr != nil {
			res.Explanation += "; listing existing comments also failed: " + listErr.Error()
		}
		return res, nil
	}

	res := PostLintViolationsResult{
		Posted:   len(toPost),
		Deduped:  deduped,
		Skipped:  skipped,
		ReviewID: reviewID,
		Explanation: fmt.Sprintf(
			"posted review %d with %d new line comment(s); %d deduped, %d skipped",
			reviewID, len(toPost), deduped, skipped),
	}
	if listErr != nil {
		// The post succeeded despite the list fault — but the operator
		// should see that dedup was disabled, so a re-run might double-post.
		res.Explanation += fmt.Sprintf(
			"; NOTE: existing-comment listing failed (%v), dedup was disabled",
			listErr)
	}
	return res, nil
}

// chitinLintMarkerPrefix is the literal HTML-comment prefix every
// chitin-authored lint comment carries. The dedup logic scans existing
// PR review-comment bodies for this prefix and parses the (rule, file,
// line) triple out of the match. HTML comments are invisible in the
// GitHub-rendered comment body but preserved verbatim by the API, so
// the marker round-trips through list+post without surfacing to humans.
const chitinLintMarkerPrefix = "<!-- chitin-lint:"

// chitinLintMarkerSuffix is the closing token of the marker. The
// (rule, file, line) triple lives between prefix and suffix as
// `<rule>:<url-encoded-file>:<line>`. URL-encoding the file segment
// keeps colons in the path from breaking the parser (rare in practice
// but possible on Windows-authored fixtures).
const chitinLintMarkerSuffix = " -->"

// chitinLintMarkerRe parses one marker out of an existing comment body.
// Anchored at start because chitin always writes the marker as the
// first line; a permissive search would risk matching a quoted marker
// inside a human reply.
var chitinLintMarkerRe = regexp.MustCompile(
	`^<!-- chitin-lint:([A-Z0-9]+):([^:]+):([0-9]+) -->`)

// markerFor returns the dedup marker line for one violation. The line
// is the first line of the comment body the activity POSTs; the parser
// (chitinLintMarkerRe) extracts the triple back out on a subsequent run.
func markerFor(v LintViolation) string {
	return fmt.Sprintf("%s%s:%s:%d%s",
		chitinLintMarkerPrefix,
		v.Rule,
		url.QueryEscape(v.File),
		v.Line,
		chitinLintMarkerSuffix)
}

// markerKey is the dedup key for one violation — the same triple the
// regex extracts from a posted comment. Used to populate the existing-
// markers set and to look each new violation up before posting.
func markerKey(v LintViolation) string {
	return fmt.Sprintf("%s|%s|%d", v.Rule, v.File, v.Line)
}

// ghReviewComment is the JSON shape one element of the comments array
// takes in a `gh api ... /pulls/<N>/comments` response. Only the fields
// the dedup logic reads are declared; `gh api` returns the full GitHub
// shape and json.Unmarshal silently drops unknown fields.
type ghReviewComment struct {
	Body string `json:"body"`
}

// listExistingMarkers fetches every review comment on the PR and
// returns the set of (rule, file, line) keys for chitin-authored ones
// (identified by the chitinLintMarkerPrefix). Returns an empty set if
// no chitin comments exist. Returns an error on gh-api failure so the
// caller can record the dedup degradation.
//
// Uses `--paginate` so a PR with more than 30 review comments (the
// default page size) does not silently miss any — a tail comment that
// was missed would re-post on the next round, defeating dedup. The
// gh-api response on `--paginate` is one concatenated JSON array per
// page; the caller decodes each page separately.
func (a *PostLintViolations) listExistingMarkers(ctx context.Context, repo string, prNumber int) (map[string]struct{}, error) {
	out, err := a.gh.Run(ctx, "api",
		"--paginate",
		fmt.Sprintf("repos/%s/pulls/%d/comments", repo, prNumber))
	if err != nil {
		return nil, fmt.Errorf("gh api list review comments: %w", err)
	}

	markers := map[string]struct{}{}
	// `gh api --paginate` concatenates page results. Each page is a
	// JSON array; the concatenation is `][` between pages. Replace the
	// inter-page boundary with `,` so json.Unmarshal sees one array.
	// This is the gh-recommended pattern for consuming --paginate
	// output (see `gh api --help`).
	joined := bytes.ReplaceAll(out, []byte("]["), []byte(","))
	var comments []ghReviewComment
	if err := json.Unmarshal(joined, &comments); err != nil {
		return nil, fmt.Errorf("unmarshal review comments: %w", err)
	}
	for _, c := range comments {
		m := chitinLintMarkerRe.FindStringSubmatch(c.Body)
		if m == nil {
			continue
		}
		rule := m[1]
		file, urlErr := url.QueryUnescape(m[2])
		if urlErr != nil {
			// Marker is malformed — skip rather than count as a dedup
			// hit. The downside is we might double-post on a marker
			// chitin wrote with a broken encoding; the upside is the
			// operator sees the violation rather than silent dropping.
			continue
		}
		line, lineErr := strconv.Atoi(m[3])
		if lineErr != nil || line <= 0 {
			continue
		}
		markers[fmt.Sprintf("%s|%s|%d", rule, file, line)] = struct{}{}
	}
	return markers, nil
}

// ghReviewResponse is the GitHub `POST .../reviews` response shape.
// Only the `id` field is read so the activity can carry the review id
// in its Result for chain-event correlation.
type ghReviewResponse struct {
	ID int64 `json:"id"`
}

// postReview POSTs one review with one line comment per violation. The
// review event is COMMENT (not APPROVE or REQUEST_CHANGES) because
// these are mechanical lint findings, not a human verdict — the
// iteration loop addresses them, then Copilot's REQUEST_CHANGES is the
// load-bearing gate.
//
// Uses `gh api --input -` with the full JSON body on stdin rather than
// `-f event=COMMENT -F comments=...` because gh's `-F` flag flattens
// JSON-typed values inconsistently for nested arrays — passing the
// whole envelope as JSON on stdin sidesteps the flag-parsing edge cases
// (verified against gh 2.40+).
func (a *PostLintViolations) postReview(ctx context.Context, repo string, prNumber int, commitSHA string, violations []LintViolation) (int64, error) {
	type postComment struct {
		Path string `json:"path"`
		Line int    `json:"line"`
		Body string `json:"body"`
	}
	type postEnvelope struct {
		Event    string        `json:"event"`
		CommitID string        `json:"commit_id,omitempty"`
		Comments []postComment `json:"comments"`
	}
	envelope := postEnvelope{
		Event:    "COMMENT",
		CommitID: commitSHA,
		Comments: make([]postComment, len(violations)),
	}
	for i, v := range violations {
		envelope.Comments[i] = postComment{
			Path: v.File,
			Line: v.Line,
			Body: markerFor(v) + "\n\n" + strings.TrimSpace(v.Message),
		}
	}
	body, err := json.Marshal(envelope)
	if err != nil {
		return 0, fmt.Errorf("marshal review envelope: %w", err)
	}

	out, err := a.gh.RunWithStdin(ctx, body, "api",
		"--method", "POST",
		"--input", "-",
		fmt.Sprintf("repos/%s/pulls/%d/reviews", repo, prNumber))
	if err != nil {
		return 0, fmt.Errorf("gh api POST review: %w", err)
	}

	var resp ghReviewResponse
	if err := json.Unmarshal(out, &resp); err != nil {
		// The POST may have succeeded even if the response decoding
		// fails (gh occasionally returns warnings prefixed to the JSON
		// body in older versions). Surface the decode error so the
		// operator can investigate; the review_id will be missing from
		// the chain event but the comments are on the PR.
		return 0, fmt.Errorf("decode review response: %w", err)
	}
	return resp.ID, nil
}

// ghRunner is the abstraction over `gh` CLI invocation, scoped to the
// activities package. Mirrors activities/review.GhRunner but adds
// RunWithStdin so the POST path can pipe a JSON envelope through gh's
// `--input -` flag without needing a temp file. Production binds the
// default exec.CommandContext shell-out; tests inject a fake.
type ghRunner interface {
	Run(ctx context.Context, args ...string) ([]byte, error)
	RunWithStdin(ctx context.Context, stdin []byte, args ...string) ([]byte, error)
}

// defaultGhRunner is the production gh-shell-out. It honors ctx
// cancellation — long-hung `gh` calls would otherwise stall the
// activity past its StartToCloseTimeout.
type defaultGhRunner struct{}

// Run shells out to `gh` and returns stdout. exec.CommandContext honors
// ctx — if the workflow's activity timeout fires, gh is SIGKILL'd.
func (defaultGhRunner) Run(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "gh", args...)
	out, err := cmd.Output()
	if err != nil {
		var stderr string
		if ee, ok := err.(*exec.ExitError); ok {
			stderr = strings.TrimSpace(string(ee.Stderr))
		}
		if stderr != "" {
			return nil, fmt.Errorf("gh %s: %w; stderr=%s", strings.Join(args, " "), err, stderr)
		}
		return nil, fmt.Errorf("gh %s: %w", strings.Join(args, " "), err)
	}
	return out, nil
}

// RunWithStdin shells out to `gh` with stdin attached. Used for the
// POST path where the review envelope travels via `--input -`.
func (defaultGhRunner) RunWithStdin(ctx context.Context, stdin []byte, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "gh", args...)
	cmd.Stdin = bytes.NewReader(stdin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		tail := strings.TrimSpace(stderr.String())
		if tail != "" {
			return nil, fmt.Errorf("gh %s: %w; stderr=%s", strings.Join(args, " "), err, tail)
		}
		return nil, fmt.Errorf("gh %s: %w", strings.Join(args, " "), err)
	}
	return stdout.Bytes(), nil
}
