package activities

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/chitinhq/chitin/go/orchestrator/driver"
	"github.com/chitinhq/chitin/go/orchestrator/worktree"
)

// IteratePRReviewInput is the typed input to the IteratePRReview activity —
// one chitin-authored pull request that received a Copilot review (spec 113
// US1). Re-invokes the authoring driver against the PR branch with the
// comment context, commits any fixups, and emits chain events for the round.
type IteratePRReviewInput struct {
	// PRNumber is the chitin-authored pull request being iterated.
	PRNumber int `json:"pr_number"`
	// PRBranch is the head branch of the PR — the rebase target.
	PRBranch string `json:"pr_branch"`
	// TargetRepo is the absolute path to the local working repository the
	// activity will mint its worktree under (reuses spec 112 US2's Checkout).
	TargetRepo string `json:"target_repo"`
	// Repo is the GitHub owner/name (e.g. "chitinhq/chitin"), used to fetch
	// the review body + line comments via gh api.
	Repo string `json:"repo"`
	// ReviewID is the GitHub review id whose comments are being addressed.
	// Carried into the workflow ID for dedup and into chain events for
	// correlation.
	ReviewID int64 `json:"review_id"`
	// Round is the 1-based iteration round number for this review (1..cap).
	// Recorded in chain events so the operator can see how many rounds the
	// loop took.
	Round int `json:"round"`
	// DriverID is the driver that authored the original PR. The iteration
	// re-invokes this same driver so the fixup style matches.
	DriverID string `json:"driver_id"`
	// WorkUnitID is the orchestration handle for this iteration round; used
	// to slug the worktree directory.
	WorkUnitID string `json:"work_unit_id"`
}

// IteratePRReviewResult is the typed outcome of one iteration round. The
// activity always returns a nil error and folds every outcome — driver
// success, driver no-op, push failure, comment fetch failure — into the
// result so the workflow settles cleanly without blind retries.
type IteratePRReviewResult struct {
	// PushedFixup is true iff the driver produced changes that were
	// committed and force-pushed.
	PushedFixup bool `json:"pushed_fixup"`
	// FixupSHA is the new HEAD SHA after the force-push; empty when
	// PushedFixup is false.
	FixupSHA string `json:"fixup_sha"`
	// CommentCount is how many line comments the iteration saw on the
	// review; informational, used for telemetry.
	CommentCount int `json:"comment_count"`
	// Explanation is a human-readable account of what happened.
	Explanation string `json:"explanation"`
}

// IteratePRReview is the spec 113 US1 PR-comment-respond activity. On a
// chitin-authored PR review, this activity:
//
//   1. Mints a worktree on the PR branch via worktree.Manager.Checkout
//      (reuses spec 112 US2's infra).
//   2. Fetches the review body + line comments via gh api.
//   3. Builds a re-prompt for the same driver that authored the PR.
//   4. Invokes the driver against the worktree.
//   5. If driver produced changes: commits + force-with-lease pushes.
//   6. Emits a `pr_iteration_completed` chain event.
//
// Worktree is reclaimed via deferred Teardown regardless of outcome. The
// activity is engineered to return a nil error for every outcome — push
// failure, driver no-op, gh api fault all fold into the result so the
// workflow doesn't retry blindly.
type IteratePRReview struct {
	manager  *worktree.Manager
	registry *driver.Registry
}

// NewIteratePRReview returns an IteratePRReview activity bound to mgr +
// the driver registry. Both are required — driver lookup is by id.
func NewIteratePRReview(mgr *worktree.Manager, reg *driver.Registry) *IteratePRReview {
	return &IteratePRReview{manager: mgr, registry: reg}
}

// ActivityName is the stable Temporal activity name.
func (a *IteratePRReview) ActivityName() string { return "IteratePRReview" }

// Execute is the activity entrypoint. Always returns a nil error.
//
// Every outcome — guard failure, fetch failure, driver no-op, push fault —
// is audited via a single deferred `pr_iteration_completed` chain event
// (see the defer at the top of the function body). This makes the chain
// the source-of-truth for "this iteration round happened" regardless of
// which path the activity took.
func (a *IteratePRReview) Execute(ctx context.Context, in IteratePRReviewInput) (IteratePRReviewResult, error) {
	var res IteratePRReviewResult
	// Single defer guarantees the chain event fires on every return path
	// — guard failure, fetch failure, push failure, success. Addresses
	// Copilot review feedback that earlier returns silently dropped the
	// audit event.
	defer func() { emitPRIterationEvent(ctx, "pr_iteration_completed", in, res) }()

	if a.manager == nil || a.registry == nil {
		res.Explanation = "no Manager or Registry bound — iteration not attempted"
		return res, nil
	}
	if in.PRBranch == "" || in.TargetRepo == "" || in.Repo == "" {
		res.Explanation = "missing PRBranch, TargetRepo, or Repo — iteration not attempted"
		return res, nil
	}

	workUnitID := in.WorkUnitID
	if workUnitID == "" {
		workUnitID = fmt.Sprintf("iterate-pr-%d-review-%d-r%d", in.PRNumber, in.ReviewID, in.Round)
	}

	// Mint a dedicated checkout of the PR branch. Manager.Checkout fetches
	// origin first so the rebase base is fresh.
	wt, err := a.manager.Checkout(in.TargetRepo, in.PRBranch, workUnitID)
	if err != nil {
		res.Explanation = fmt.Sprintf("worktree checkout failed: %v", err)
		return res, nil
	}
	defer func() { _ = a.manager.Teardown(wt) }()

	// Fetch the review context (body + line comments) via gh api.
	reviewCtx, err := fetchReviewContext(ctx, in.Repo, in.PRNumber, in.ReviewID)
	if err != nil {
		res.Explanation = fmt.Sprintf("review context fetch failed: %v", err)
		return res, nil
	}

	res.CommentCount = len(reviewCtx.LineComments)

	if len(reviewCtx.LineComments) == 0 && strings.TrimSpace(reviewCtx.Body) == "" {
		res.Explanation = "review carried no body and no line comments — nothing to address"
		return res, nil
	}

	// Resolve the driver from the registry by id. A missing driver fails
	// the round but does not block the workflow.
	d, ok := a.registry.Driver(in.DriverID)
	if !ok {
		res.Explanation = fmt.Sprintf("driver %q not registered — cannot iterate", in.DriverID)
		return res, nil
	}

	// Build the iteration prompt + invoke the driver against the worktree.
	prompt := BuildIterationPrompt(in, reviewCtx)
	wu := driver.WorkUnit{
		ID:           workUnitID,
		SpecID:       "113",
		TaskID:       "iterate-review",
		Context:      prompt,
		WorktreePath: wt,
	}
	invRes, invErr := d.Invoke(ctx, wu)
	if invErr != nil {
		res.Explanation = fmt.Sprintf("driver invocation faulted: %v", invErr)
		return res, nil
	}
	if invRes.Status != driver.StatusSucceeded {
		res.Explanation = fmt.Sprintf("driver returned non-success status %s: %s", invRes.Status, invRes.Explanation)
		return res, nil
	}

	// Check if the driver actually produced changes.
	status, err := gitInWorktree(ctx, wt, "status", "--porcelain")
	if err != nil {
		res.Explanation = fmt.Sprintf("worktree status check failed: %v", err)
		return res, nil
	}
	if strings.TrimSpace(status) == "" {
		res.Explanation = "driver returned success but produced no changes"
		return res, nil
	}

	// Commit + force-with-lease push.
	if _, err := gitInWorktree(ctx, wt, "add", "-A"); err != nil {
		res.Explanation = fmt.Sprintf("git add failed: %v", err)
		return res, nil
	}
	subject := fmt.Sprintf("review fix (round %d): address review #%d", in.Round, in.ReviewID)
	body := fmt.Sprintf(
		"Auto-fixup commit produced by the Chitin Orchestrator (spec 113 US1) "+
			"in response to review #%d on PR #%d. Round %d of the iteration loop.",
		in.ReviewID, in.PRNumber, in.Round)
	if _, err := gitInWorktree(ctx, wt,
		"-c", "user.name=Chitin Orchestrator",
		"-c", "user.email=orchestrator@chitin.local",
		"commit", "-m", subject, "-m", body,
	); err != nil {
		res.Explanation = fmt.Sprintf("git commit failed: %v", err)
		return res, nil
	}
	if sha, shaErr := gitInWorktree(ctx, wt, "rev-parse", "HEAD"); shaErr == nil {
		res.FixupSHA = sha
	}
	if _, err := gitInWorktree(ctx, wt, "push", "--force-with-lease", "origin", in.PRBranch); err != nil {
		res.Explanation = fmt.Sprintf("force-push lost lease or failed: %v", err)
		return res, nil
	}

	res.PushedFixup = true
	res.Explanation = fmt.Sprintf(
		"iterated PR #%d review #%d (round %d): pushed fixup %s addressing %d line comment(s)",
		in.PRNumber, in.ReviewID, in.Round, shortHex(res.FixupSHA), res.CommentCount)
	return res, nil
}

// reviewContext is the closed shape of the data the iteration prompt needs.
type reviewContext struct {
	Body         string
	LineComments []reviewLineComment
}

// reviewLineComment is one inline review comment.
type reviewLineComment struct {
	ID    int64  `json:"id"`
	Path  string `json:"path"`
	Line  int    `json:"line"`
	Body  string `json:"body"`
}

// fetchReviewContext fetches the review body + every line comment for the
// review via gh api. Uses the canonical repos/<owner>/<repo>/... form (per
// spec 113 FR-006 and the #1050 Copilot fix). Uses `--paginate` on the
// comments endpoint so large reviews (>30 inline comments — the default
// page size) are fetched in full; without it, the iteration prompt could
// silently miss comments past page one.
func fetchReviewContext(ctx context.Context, repo string, prNumber int, reviewID int64) (reviewContext, error) {
	var rc reviewContext

	// Body of the review itself. The endpoint returns a single object, no
	// pagination needed. Decode errors are surfaced rather than swallowed
	// so an unexpected response shape doesn't silently drop the body and
	// proceed with incomplete context.
	reviewPath := fmt.Sprintf("repos/%s/pulls/%d/reviews/%d", repo, prNumber, reviewID)
	body, err := ghApi(ctx, reviewPath)
	if err != nil {
		return rc, fmt.Errorf("fetch review body: %w", err)
	}
	var reviewMeta struct {
		Body string `json:"body"`
	}
	if err := json.Unmarshal(body, &reviewMeta); err != nil {
		return rc, fmt.Errorf("decode review body: %w", err)
	}
	rc.Body = reviewMeta.Body

	// Inline line comments — scoped to the PR, then filter to this review
	// via pull_request_review_id (the API doesn't expose a per-review-
	// comments endpoint directly). `--paginate` walks every page so the
	// activity sees ALL inline comments before filtering — without it,
	// large reviews would silently drop comments past the first 30.
	commentsPath := fmt.Sprintf("repos/%s/pulls/%d/comments?per_page=100", repo, prNumber)
	commentsRaw, err := ghApiPaginated(ctx, commentsPath)
	if err != nil {
		return rc, fmt.Errorf("fetch pr comments: %w", err)
	}
	var allComments []struct {
		ID                  int64  `json:"id"`
		Path                string `json:"path"`
		Line                int    `json:"line"`
		Body                string `json:"body"`
		PullRequestReviewID int64  `json:"pull_request_review_id"`
	}
	if err := json.Unmarshal(commentsRaw, &allComments); err != nil {
		return rc, fmt.Errorf("decode pr comments: %w", err)
	}
	for _, c := range allComments {
		if c.PullRequestReviewID != reviewID {
			continue
		}
		rc.LineComments = append(rc.LineComments, reviewLineComment{
			ID:   c.ID,
			Path: c.Path,
			Line: c.Line,
			Body: c.Body,
		})
	}
	return rc, nil
}

// BuildIterationPrompt assembles the re-prompt passed to the driver. Pure
// function — exported so tests (and future spec-author iteration prompts)
// can assert on the shape.
//
// The orchestrator authors the commit message itself (see Execute below),
// so this prompt does NOT ask the driver to write one — only to make
// file changes. Comment-thread reply API integration is deferred to a
// follow-up; for now an intentional no-fix decision shows up as the
// driver simply not modifying the relevant file (the orchestrator's
// "driver returned success but produced no changes" branch then records
// the round as a no-op completion).
func BuildIterationPrompt(in IteratePRReviewInput, rc reviewContext) string {
	var b strings.Builder
	fmt.Fprintf(&b,
		"You are addressing review feedback on PR #%d (round %d).\n\n",
		in.PRNumber, in.Round)
	b.WriteString("You have a fresh worktree on the PR's branch. ")
	b.WriteString("For each comment below, EITHER apply the smallest reasonable fix ")
	b.WriteString("OR leave the file unchanged (which is recorded as an intentional decline). ")
	b.WriteString("Do not refactor unrelated code. Do not change task scope.\n\n")
	if strings.TrimSpace(rc.Body) != "" {
		fmt.Fprintf(&b, "REVIEW BODY:\n%s\n\n", strings.TrimSpace(rc.Body))
	}
	if len(rc.LineComments) > 0 {
		b.WriteString("LINE COMMENTS:\n")
		for i, c := range rc.LineComments {
			fmt.Fprintf(&b, "  [%d] %s:%d\n      %s\n",
				i+1, c.Path, c.Line, strings.TrimSpace(c.Body))
		}
		b.WriteString("\n")
	}
	b.WriteString("After making changes, exit. Do not run tests, do not write commit messages. ")
	b.WriteString("The orchestrator will commit + push your changes as a single fixup commit.\n")
	return b.String()
}

// gitInWorktree runs `git <args...>` in dir and returns trimmed stdout.
// Mirrors the helper in activities/sibling_rebase.go (spec 112 US2);
// duplicated rather than shared to avoid the package-private collision.
func gitInWorktree(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w: %s",
			strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(stdout.String()), nil
}

// ghApi runs `gh api <path>` and returns the raw JSON body. Errors out on
// non-zero exit. A future improvement: cap with a context timeout.
func ghApi(ctx context.Context, path string) ([]byte, error) {
	if _, err := exec.LookPath("gh"); err != nil {
		return nil, fmt.Errorf("gh CLI not available: %w", err)
	}
	cmd := exec.CommandContext(ctx, "gh", "api", path)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("gh api %s: %w: %s", path, err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}

// ghApiPaginated runs `gh api --paginate <path>` and returns the merged
// JSON array body. gh's --paginate flag walks every Link-header page and
// emits a single concatenated JSON array on stdout, so callers can
// Unmarshal the result into a `[]T` directly. Necessary for list
// endpoints like `/pulls/N/comments` where a large review would otherwise
// silently drop comments past the first page.
func ghApiPaginated(ctx context.Context, path string) ([]byte, error) {
	if _, err := exec.LookPath("gh"); err != nil {
		return nil, fmt.Errorf("gh CLI not available: %w", err)
	}
	cmd := exec.CommandContext(ctx, "gh", "api", "--paginate", path)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("gh api --paginate %s: %w: %s", path, err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}

// ghApiPostJSON runs `gh api --method POST --input - <path>` with body
// piped on stdin and returns the response stdout. Used by activities
// that POST non-scalar payloads (nested arrays/objects) where gh's `-F`
// flag falls short — e.g. spec-115 PostLintViolations posting a review
// with multiple inline comments.
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

// shortHex returns the first 8 hex chars of a SHA for human log lines.
func shortHex(sha string) string {
	if len(sha) <= 8 {
		return sha
	}
	return sha[:8]
}

// emitPRIterationEvent writes one spec-113 chain event via the kernel emit
// subcommand. Mirrors spec 112 US2's emitSiblingRebaseEvent pattern — temp
// file + `chitin-kernel emit -event-file <path>`, fail-soft via stderr
// warning (the round outcome is the load-bearing signal; the chain entry
// is supplementary audit). Honours CHITIN_DISABLE_CHAIN_EMIT=1 for tests.
func emitPRIterationEvent(ctx context.Context, eventType string, in IteratePRReviewInput, res IteratePRReviewResult) {
	if os.Getenv("CHITIN_DISABLE_CHAIN_EMIT") == "1" {
		return
	}
	binPath := os.Getenv("CHITIN_KERNEL_BIN")
	if binPath == "" {
		binPath = "chitin-kernel"
	}
	payload := map[string]any{
		"pr_number":     in.PRNumber,
		"pr_branch":     in.PRBranch,
		"review_id":     in.ReviewID,
		"round":         in.Round,
		"driver_id":     in.DriverID,
		"comment_count": res.CommentCount,
		"pushed_fixup":  res.PushedFixup,
		"fixup_sha":     res.FixupSHA,
		"explanation":   res.Explanation,
	}
	envelope := map[string]any{
		"schema_version":    "2",
		"event_type":        eventType,
		"run_id":            fmt.Sprintf("pr-iteration-%d-%d-r%d", in.PRNumber, in.ReviewID, in.Round),
		"session_id":        fmt.Sprintf("chitin-orchestrator-iterate-%d", in.PRNumber),
		"surface":           "chitin-orchestrator",
		"agent_instance_id": "chitin-orchestrator",
		"chain_type":        "pr-iteration",
		"ts":                time.Now().UTC().Format(time.RFC3339Nano),
		"payload":           payload,
	}
	body, err := json.Marshal(envelope)
	if err != nil {
		warnIteration("marshal: %v — %s recorded only in workflow result", err, eventType)
		return
	}
	tmp, err := os.CreateTemp("", "chitin-iterate-emit-*.json")
	if err != nil {
		warnIteration("temp file: %v — %s recorded only in workflow result", err, eventType)
		return
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.Write(body); err != nil {
		tmp.Close()
		warnIteration("temp write: %v — %s recorded only in workflow result", err, eventType)
		return
	}
	if err := tmp.Close(); err != nil {
		warnIteration("temp close: %v — %s recorded only in workflow result", err, eventType)
		return
	}
	chitinDir := os.Getenv("CHITIN_DIR")
	if chitinDir == "" {
		if home, err := os.UserHomeDir(); err == nil {
			chitinDir = home + "/.chitin"
		} else {
			chitinDir = ".chitin"
		}
	}
	emitCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(emitCtx, binPath, "emit", "-dir", chitinDir, "-event-file", tmpPath)
	var stderrBuf strings.Builder
	cmd.Stderr = &stderrBuf
	if err := cmd.Run(); err != nil {
		tail := strings.TrimSpace(stderrBuf.String())
		if len(tail) > 200 {
			tail = tail[len(tail)-200:]
		}
		warnIteration("kernel emit failed: %v (stderr: %s) — %s recorded only in workflow result", err, tail, eventType)
	}
}

// warnIteration logs a chain-emit warning. Goes to stderr so the worker
// host's journald entry captures it; the round outcome never depends on it.
func warnIteration(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "warning: pr-iteration chain emit: "+format+"\n", args...)
}
