package activities

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"time"
)

// LintViolation is one finding produced by the spec-lint subcommand
// (spec 115 FR-003). Mirrors the JSON shape emitted by
// `chitin-orchestrator spec-lint` on stdout so the orchestrator can
// decode the linter's output and pipe it directly into this activity.
type LintViolation struct {
	// Rule is the rule code (e.g. "L01", "L05"). Carried into the
	// comment marker for dedup.
	Rule string `json:"rule"`
	// File is the path inside the repo the violation refers to
	// (e.g. ".specify/specs/115-spec-review-gate/spec.md"). Required —
	// the activity skips violations with no file because the GitHub
	// review-comment endpoint demands a path.
	File string `json:"file"`
	// Line is the 1-based line number the violation refers to.
	// Required for the same reason.
	Line int `json:"line"`
	// Severity is "error" or "warning". Only "error" gates a POST per
	// spec 115 FR-004 ("warning" violations are informational and never
	// posted to the PR by this activity).
	Severity string `json:"severity"`
	// Message is the human-readable violation explanation rendered as
	// the comment body.
	Message string `json:"message"`
}

// PostLintViolationsInput is the typed input to the activity. One
// invocation per (PR, lint run) — the activity is idempotent on its
// own input via the dedup pass against existing chitin-authored review
// comments (FR-004).
type PostLintViolationsInput struct {
	// PRNumber is the spec PR receiving the lint comments.
	PRNumber int `json:"pr_number"`
	// Repo is the GitHub owner/name (e.g. "chitinhq/chitin").
	Repo string `json:"repo"`
	// Violations is the linter's output. The activity filters to
	// error-severity entries and dedups by (rule, file, line) before
	// posting.
	Violations []LintViolation `json:"violations"`
	// CommitSHA is the head SHA the review comments anchor against. The
	// GitHub reviews endpoint requires commit_id for line comments to
	// land on a specific revision; without it, GitHub may reject or
	// silently relocate the comment. Caller must pass the PR's current
	// head SHA.
	CommitSHA string `json:"commit_sha"`
}

// PostLintViolationsResult folds every outcome — fetch failure, POST
// failure, full dedup-skip — into a typed shape so the workflow settles
// cleanly. Mirrors the nil-error convention used by IteratePRReview.
type PostLintViolationsResult struct {
	// Eligible is the count of error-severity violations seen on the
	// input. (Warning-severity is filtered out before dedup.)
	Eligible int `json:"eligible"`
	// Posted is the count of violations actually included in the
	// POSTed review (eligible minus dedups).
	Posted int `json:"posted"`
	// Skipped is the count of error-severity violations dropped because
	// an equivalent chitin-authored comment already exists on the PR.
	Skipped int `json:"skipped"`
	// Explanation is a one-line human-readable summary of what the
	// activity did, mirroring IteratePRReview's audit shape.
	Explanation string `json:"explanation"`
}

// lintCommentMarkerPrefix is embedded in every chitin-posted lint
// comment body so a re-run can identify its own prior posts cheaply
// and skip duplicates without needing a separate index. The marker
// also makes the comments greppable in PR threads.
const lintCommentMarkerPrefix = "<!-- chitin-spec-lint:"

// PostLintViolations is the spec 115 US2 / FR-004 activity. Posts one
// PR review comment per error-severity violation, deduping against
// existing chitin-authored review comments by (rule, file, line) so a
// re-run on the same PR doesn't double-post.
//
// Stateless: no Manager / Registry dependency — the activity shells
// out to `gh api` for both the read (existing comments) and the write
// (new review POST). Always returns a nil error; every fault path
// folds into the result with an Explanation.
type PostLintViolations struct{}

// NewPostLintViolations returns a ready-to-register activity. No
// dependencies to inject; the constructor exists for symmetry with
// the other activities in this package and to keep the worker-side
// registration call site uniform.
func NewPostLintViolations() *PostLintViolations { return &PostLintViolations{} }

// ActivityName is the stable Temporal activity name.
func (a *PostLintViolations) ActivityName() string { return "PostLintViolations" }

// Execute is the activity entrypoint. Always returns a nil error.
func (a *PostLintViolations) Execute(ctx context.Context, in PostLintViolationsInput) (PostLintViolationsResult, error) {
	var res PostLintViolationsResult

	if in.PRNumber == 0 || in.Repo == "" {
		res.Explanation = "missing PRNumber or Repo — post not attempted"
		return res, nil
	}

	// Filter to error-severity with a usable (file, line) anchor —
	// GitHub's review-comments endpoint rejects entries without a
	// path, and a line<=0 would land on an unpredictable hunk.
	eligible := make([]LintViolation, 0, len(in.Violations))
	for _, v := range in.Violations {
		if !strings.EqualFold(v.Severity, "error") {
			continue
		}
		if v.File == "" || v.Line <= 0 {
			continue
		}
		eligible = append(eligible, v)
	}
	res.Eligible = len(eligible)
	if len(eligible) == 0 {
		res.Explanation = "no error-severity violations to post"
		return res, nil
	}

	// Fetch existing chitin-authored review comments so we can dedup.
	existing, err := fetchChitinLintMarkers(ctx, in.Repo, in.PRNumber)
	if err != nil {
		res.Explanation = fmt.Sprintf("existing-comment fetch failed: %v", err)
		return res, nil
	}

	// Drop any eligible violation whose marker key already exists.
	fresh := make([]LintViolation, 0, len(eligible))
	for _, v := range eligible {
		if _, dup := existing[markerKey(v.Rule, v.File, v.Line)]; dup {
			res.Skipped++
			continue
		}
		fresh = append(fresh, v)
	}

	if len(fresh) == 0 {
		res.Explanation = fmt.Sprintf(
			"all %d error-severity violation(s) already posted — nothing to add",
			res.Eligible)
		return res, nil
	}

	if in.CommitSHA == "" {
		res.Explanation = "missing CommitSHA — cannot anchor review comments"
		return res, nil
	}

	// Deterministic ordering: sort by (file, line, rule) so the
	// posted review reads top-to-bottom and tests can assert on
	// payload shape without race-flakes from map iteration.
	sort.SliceStable(fresh, func(i, j int) bool {
		if fresh[i].File != fresh[j].File {
			return fresh[i].File < fresh[j].File
		}
		if fresh[i].Line != fresh[j].Line {
			return fresh[i].Line < fresh[j].Line
		}
		return fresh[i].Rule < fresh[j].Rule
	})

	comments := make([]map[string]any, 0, len(fresh))
	for _, v := range fresh {
		comments = append(comments, map[string]any{
			"path": v.File,
			"line": v.Line,
			"side": "RIGHT",
			"body": renderLintCommentBody(v),
		})
	}
	payload := map[string]any{
		"commit_id": in.CommitSHA,
		"event":     "COMMENT",
		"comments":  comments,
	}
	if err := ghApiPost(ctx,
		fmt.Sprintf("repos/%s/pulls/%d/reviews", in.Repo, in.PRNumber),
		payload,
	); err != nil {
		res.Explanation = fmt.Sprintf("gh api POST failed: %v", err)
		return res, nil
	}

	res.Posted = len(fresh)
	res.Explanation = fmt.Sprintf(
		"posted %d error-severity violation(s) on PR #%d (skipped %d already-present)",
		res.Posted, in.PRNumber, res.Skipped)
	return res, nil
}

// renderLintCommentBody formats one violation as the review-comment
// body. The leading HTML-comment marker is what fetchChitinLintMarkers
// keys on for dedup. Keep the prefix EXACT — changing it would break
// dedup on every PR with prior lint posts.
func renderLintCommentBody(v LintViolation) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s rule=%s file=%s line=%d -->\n\n",
		lintCommentMarkerPrefix, v.Rule, v.File, v.Line)
	fmt.Fprintf(&b, "**spec-lint %s**: %s",
		v.Rule, strings.TrimSpace(v.Message))
	return b.String()
}

// markerKey is the dedup tuple. Kept private so callers can't drift
// from the encoding used in renderLintCommentBody / parseMarker.
func markerKey(rule, file string, line int) string {
	return fmt.Sprintf("%s|%s|%d", rule, file, line)
}

// fetchChitinLintMarkers returns the set of (rule|file|line) keys
// already posted by this activity on the PR. Pulls the full inline-
// comment list via `gh api --paginate` (matching pr_iteration's
// pagination contract — large reviews would otherwise drop entries
// past page 1) and scans each body for the lintCommentMarkerPrefix.
//
// Author-name filtering is intentionally NOT applied. The marker
// itself is the dedup signal — the comment body is the source of
// truth — so token rotations or GitHub-app renames don't break
// dedup. If a human ever copy-pastes a marker into a comment, the
// activity will treat it as a dup and skip the re-post; that is the
// correct (conservative) behavior.
func fetchChitinLintMarkers(ctx context.Context, repo string, prNumber int) (map[string]struct{}, error) {
	path := fmt.Sprintf("repos/%s/pulls/%d/comments?per_page=100", repo, prNumber)
	raw, err := ghApiPaginated(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("fetch pr comments: %w", err)
	}
	var all []struct {
		Body string `json:"body"`
	}
	if err := json.Unmarshal(raw, &all); err != nil {
		return nil, fmt.Errorf("decode pr comments: %w", err)
	}
	keys := make(map[string]struct{}, len(all))
	for _, c := range all {
		if key, ok := parseMarker(c.Body); ok {
			keys[key] = struct{}{}
		}
	}
	return keys, nil
}

// parseMarker pulls (rule, file, line) out of a chitin-posted lint
// comment body and returns its dedup key. Returns ok=false when the
// body is not a chitin lint comment — that includes human comments,
// Copilot comments, and malformed markers (a defensive contract
// against future renderLintCommentBody changes that fail to update
// this parser).
func parseMarker(body string) (string, bool) {
	idx := strings.Index(body, lintCommentMarkerPrefix)
	if idx < 0 {
		return "", false
	}
	rest := body[idx+len(lintCommentMarkerPrefix):]
	end := strings.Index(rest, "-->")
	if end < 0 {
		return "", false
	}
	header := strings.TrimSpace(rest[:end])
	var rule, file string
	var line int
	for _, tok := range strings.Fields(header) {
		k, v, ok := strings.Cut(tok, "=")
		if !ok {
			continue
		}
		switch k {
		case "rule":
			rule = v
		case "file":
			file = v
		case "line":
			if n, err := parseInt(v); err == nil {
				line = n
			}
		}
	}
	if rule == "" || file == "" || line <= 0 {
		return "", false
	}
	return markerKey(rule, file, line), true
}

// parseInt is a tiny strconv.Atoi wrapper kept local so parseMarker
// stays self-contained and the test for it doesn't depend on import
// order from elsewhere in the package.
func parseInt(s string) (int, error) {
	var n int
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, fmt.Errorf("non-digit %q in %q", r, s)
		}
		n = n*10 + int(r-'0')
	}
	if len(s) == 0 {
		return 0, fmt.Errorf("empty")
	}
	return n, nil
}

// ghApiPost runs `gh api --method POST --input - <path>` feeding the
// JSON-encoded payload on stdin. Used for the review-create endpoint
// which expects a structured body (event + comments[]) that doesn't
// fit gh's -f/-F flag model (-F can't express arrays of objects).
//
// Caps the call at 30s — POST is sync, GitHub's review endpoint
// usually answers in <2s, and an unbounded hang would block the
// activity's deadline. Failures bubble up; the caller folds them
// into the result Explanation.
func ghApiPost(ctx context.Context, path string, payload any) error {
	if _, err := exec.LookPath("gh"); err != nil {
		return fmt.Errorf("gh CLI not available: %w", err)
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}
	postCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(postCtx, "gh", "api", "--method", "POST", "--input", "-", path)
	cmd.Stdin = bytes.NewReader(body)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		tail := strings.TrimSpace(stderr.String())
		if len(tail) > 400 {
			tail = tail[len(tail)-400:]
		}
		return fmt.Errorf("gh api POST %s: %w: %s", path, err, tail)
	}
	return nil
}

