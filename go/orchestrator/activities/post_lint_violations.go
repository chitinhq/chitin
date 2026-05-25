package activities

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
)

// LintViolation is one structured finding from `chitin-orchestrator
// spec-lint` (spec 115 FR-003). The shape matches the linter's stdout JSON
// (`[{rule, file, line, severity, message}]`). Declared locally so this
// activity is decoupled from the speclint package's import surface; the
// integration step reconciles against the canonical type.
type LintViolation struct {
	Rule     string `json:"rule"`
	File     string `json:"file"`
	Line     int    `json:"line"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
}

// PostLintViolationsInput is the typed input — the spec PR receiving the
// lint review and the violations to surface (FR-004).
type PostLintViolationsInput struct {
	// Repo is the GitHub owner/name (e.g. "chitinhq/chitin"), used for the
	// `repos/<owner>/<repo>/...` gh-api path.
	Repo string `json:"repo"`
	// PRNumber is the spec PR whose review thread receives the comments.
	PRNumber int `json:"pr_number"`
	// Violations is the full set the linter returned. Only error-severity
	// rows are posted (FR-004 + edge-case "warning is informational");
	// warnings are returned in SkippedWarning for the caller's telemetry.
	Violations []LintViolation `json:"violations"`
}

// PostLintViolationsResult records what the activity did. Like the spec
// 113 IteratePRReview activity, every outcome folds into the result so
// the workflow settles cleanly without blind retries.
type PostLintViolationsResult struct {
	// Posted is the count of NEW error-severity violations that landed as
	// review comments in this run.
	Posted int `json:"posted"`
	// SkippedDuplicate is the count of error-severity violations that
	// already had a matching chitin-authored comment on the PR; these were
	// suppressed to keep re-runs idempotent.
	SkippedDuplicate int `json:"skipped_duplicate"`
	// SkippedWarning is the count of warning-severity violations the
	// activity received but did not post (FR-004 — warnings are
	// informational, only errors gate iteration).
	SkippedWarning int `json:"skipped_warning"`
	// ReviewID is the id of the review GitHub minted for the posted
	// comments; empty when nothing was posted (all duplicates / warnings).
	ReviewID int64 `json:"review_id"`
	// Explanation is a human-readable summary suitable for the workflow's
	// log trail.
	Explanation string `json:"explanation"`
}

// PostLintViolations is the spec 115 US2 activity that surfaces deterministic
// linter findings as PR review comments. The iteration loop (US1) then
// addresses them like any Copilot comment.
//
// Sequence:
//
//  1. Filter input to error-severity violations.
//  2. Fetch existing review comments on the PR; build a dedup set from any
//     chitin-authored comment whose body carries the embedded marker
//     `<!-- chitin-spec-lint:rule=<R> file=<F> line=<L> -->`.
//  3. For each new (rule, file, line) triple, build one inline review
//     comment carrying the marker + a human-readable message.
//  4. POST a single review with `event=COMMENT` and the batched comments
//     array via `gh api repos/<owner>/<repo>/pulls/<N>/reviews`.
//
// The activity is fail-soft: a fetch fault, a marshalling slip, or a
// gh-api non-zero exit folds into Result.Explanation and returns a nil
// error so the workflow doesn't retry blindly.
type PostLintViolations struct{}

// NewPostLintViolations returns a zero-dependency activity instance. The
// gh CLI invocation is the only external touchpoint; injecting it would
// add ceremony without isolating anything the tests can't already cover
// via CHITIN_GH_BIN.
func NewPostLintViolations() *PostLintViolations { return &PostLintViolations{} }

// ActivityName is the stable Temporal activity name.
func (a *PostLintViolations) ActivityName() string { return "PostLintViolations" }

// Execute runs the post-and-dedup flow. Always returns nil error.
func (a *PostLintViolations) Execute(ctx context.Context, in PostLintViolationsInput) (PostLintViolationsResult, error) {
	var res PostLintViolationsResult

	if in.Repo == "" || in.PRNumber == 0 {
		res.Explanation = "missing Repo or PRNumber — nothing posted"
		return res, nil
	}

	// Partition: errors are candidates, warnings are reported only.
	var errs []LintViolation
	for _, v := range in.Violations {
		switch strings.ToLower(v.Severity) {
		case "error":
			errs = append(errs, v)
		case "warning":
			res.SkippedWarning++
		default:
			// Unknown severity is treated as a warning — safer than gating
			// iteration on a value the linter didn't declare.
			res.SkippedWarning++
		}
	}

	if len(errs) == 0 {
		res.Explanation = fmt.Sprintf(
			"no error-severity violations to post (%d warnings skipped)",
			res.SkippedWarning)
		return res, nil
	}

	// Fetch existing review comments on the PR. A failure here is treated
	// as "no existing comments known" — the dedup set is empty and we
	// proceed. This errs on the side of double-posting once rather than
	// silently dropping violations the operator needs to see; the
	// explanation records the fetch fault for audit.
	existing, fetchErr := fetchExistingLintMarkers(ctx, in.Repo, in.PRNumber)
	fetchNote := ""
	if fetchErr != nil {
		fetchNote = fmt.Sprintf(" (existing-comment fetch faulted: %v — dedup disabled)", fetchErr)
	}

	var fresh []reviewComment
	for _, v := range errs {
		key := lintMarkerKey(v.Rule, v.File, v.Line)
		if _, dup := existing[key]; dup {
			res.SkippedDuplicate++
			continue
		}
		fresh = append(fresh, reviewComment{
			Path: v.File,
			Line: v.Line,
			Body: buildLintCommentBody(v),
		})
		// Mark in-memory so a single batch with two violations on the same
		// (rule, file, line) — shouldn't happen, but defensible — posts only once.
		existing[key] = struct{}{}
	}

	if len(fresh) == 0 {
		res.Explanation = fmt.Sprintf(
			"all %d error violations already posted (skipped as duplicates)%s",
			res.SkippedDuplicate, fetchNote)
		return res, nil
	}

	reviewID, postErr := postLintReview(ctx, in.Repo, in.PRNumber, fresh)
	if postErr != nil {
		res.Explanation = fmt.Sprintf("gh api review POST failed: %v%s", postErr, fetchNote)
		return res, nil
	}
	res.Posted = len(fresh)
	res.ReviewID = reviewID
	res.Explanation = fmt.Sprintf(
		"posted %d new lint violation(s) on PR #%d (skipped %d duplicate, %d warning)%s",
		res.Posted, in.PRNumber, res.SkippedDuplicate, res.SkippedWarning, fetchNote)
	return res, nil
}

// reviewComment is the inline-comment shape the GitHub reviews endpoint
// accepts under `comments[]`.
type reviewComment struct {
	Path string `json:"path"`
	Line int    `json:"line"`
	Body string `json:"body"`
	// Side defaults to RIGHT — the post-image — which is what we want
	// for spec PRs (the violation is on the version the spec author
	// proposes). Omitted to let GitHub apply its default.
}

// lintMarkerPrefix is the literal HTML-comment marker prefix embedded in
// every chitin-posted lint comment. Stable across runs so dedup is a
// substring match.
const lintMarkerPrefix = "<!-- chitin-spec-lint:"

// lintMarkerRe matches the embedded marker and captures the (rule, file,
// line) triple. Files may contain `.` and `/`; rule is `L` + digits per
// FR-003. Line is at least one digit.
var lintMarkerRe = regexp.MustCompile(
	`<!-- chitin-spec-lint:rule=([A-Za-z0-9_-]+) file=(\S+) line=(\d+) -->`)

// lintMarkerKey builds the dedup key — the literal marker substring that
// appears in any previously-posted chitin comment. Matching is exact;
// two violations with the same (rule, file, line) collide regardless of
// message text drift.
func lintMarkerKey(rule, file string, line int) string {
	return fmt.Sprintf("rule=%s file=%s line=%d", rule, file, line)
}

// buildLintCommentBody composes the human-readable comment body. The
// leading marker line is the dedup anchor; the operator-facing body
// follows. Kept terse so PR review threads stay readable.
func buildLintCommentBody(v LintViolation) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%srule=%s file=%s line=%d -->\n",
		lintMarkerPrefix, v.Rule, v.File, v.Line)
	fmt.Fprintf(&b, "**spec-lint %s (%s)** — %s",
		v.Rule, v.Severity, strings.TrimSpace(v.Message))
	return b.String()
}

// fetchExistingLintMarkers walks every existing PR review comment via
// `gh api --paginate` and returns the set of (rule, file, line) keys
// that already carry a chitin lint marker. Used to suppress duplicate
// posts on re-runs (FR-004).
//
// Uses the package-level ghApiPaginated helper (declared in pr_iteration.go)
// so list pagination is transparent.
func fetchExistingLintMarkers(ctx context.Context, repo string, prNumber int) (map[string]struct{}, error) {
	out := map[string]struct{}{}
	path := fmt.Sprintf("repos/%s/pulls/%d/comments?per_page=100", repo, prNumber)
	raw, err := ghApiPaginated(ctx, path)
	if err != nil {
		return out, err
	}
	var comments []struct {
		Body string `json:"body"`
	}
	if err := json.Unmarshal(raw, &comments); err != nil {
		return out, fmt.Errorf("decode existing pr comments: %w", err)
	}
	for _, c := range comments {
		if !strings.Contains(c.Body, lintMarkerPrefix) {
			continue
		}
		m := lintMarkerRe.FindStringSubmatch(c.Body)
		if len(m) != 4 {
			continue
		}
		var line int
		if _, err := fmt.Sscanf(m[3], "%d", &line); err != nil {
			continue
		}
		out[lintMarkerKey(m[1], m[2], line)] = struct{}{}
	}
	return out, nil
}

// postLintReview POSTs a single review with `event=COMMENT` and the
// supplied inline comments. Returns the new review's id (for telemetry).
//
// gh api's flag-based payload encoding can't express a nested
// `comments=[{...}]` array cleanly, so the request body is written to a
// temp JSON file and passed via `--input`. Mirrors the temp-file emit
// pattern from spec 112's chain-emit helper.
func postLintReview(ctx context.Context, repo string, prNumber int, comments []reviewComment) (int64, error) {
	payload := map[string]any{
		"event":    "COMMENT",
		"body":     "Spec linter (chitin-orchestrator spec-lint) flagged the following:",
		"comments": comments,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return 0, fmt.Errorf("marshal review payload: %w", err)
	}
	tmp, err := os.CreateTemp("", "chitin-lint-review-*.json")
	if err != nil {
		return 0, fmt.Errorf("temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.Write(body); err != nil {
		tmp.Close()
		return 0, fmt.Errorf("temp write: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return 0, fmt.Errorf("temp close: %w", err)
	}

	ghBin := os.Getenv("CHITIN_GH_BIN")
	if ghBin == "" {
		ghBin = "gh"
	}
	if _, err := exec.LookPath(ghBin); err != nil {
		return 0, fmt.Errorf("gh CLI not available: %w", err)
	}
	apiPath := fmt.Sprintf("repos/%s/pulls/%d/reviews", repo, prNumber)
	cmd := exec.CommandContext(ctx, ghBin, "api",
		"--method", "POST",
		"-H", "Accept: application/vnd.github+json",
		"--input", tmpPath,
		apiPath,
	)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		tail := strings.TrimSpace(stderr.String())
		if len(tail) > 200 {
			tail = tail[len(tail)-200:]
		}
		return 0, fmt.Errorf("gh api %s: %w: %s", apiPath, err, tail)
	}
	var resp struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal([]byte(stdout.String()), &resp); err != nil {
		// Body parse failure isn't fatal — the review landed; the id is
		// nice-to-have for telemetry.
		return 0, nil
	}
	return resp.ID, nil
}
