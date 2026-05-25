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

	"github.com/chitinhq/chitin/go/orchestrator/worktree"
)

// RebaseSiblingPRInput is the typed input to the RebaseSiblingPR activity —
// one open chitin-authored pull request whose sibling just merged to the base
// branch (spec 112 US2, FR-004/005/006). The activity rebases the sibling's
// branch onto the freshly-merged base; on conflict it aborts cleanly and
// leaves the PR in the operator's queue for manual resolution.
type RebaseSiblingPRInput struct {
	// PRNumber is the open sibling pull request to rebase. Carried for
	// telemetry and chain correlation; the rebase itself operates on
	// PRBranch.
	PRNumber int `json:"pr_number"`
	// PRBranch is the head branch of the open sibling pull request — the
	// branch the rebase rewrites and force-pushes.
	PRBranch string `json:"pr_branch"`
	// TargetRepo is the absolute path to the repository the rebase runs in.
	// The worktree Manager mints a dedicated checkout of PRBranch under it.
	TargetRepo string `json:"target_repo"`
	// BaseBranch is the branch the rebase targets — almost always "main".
	// Explicit so a non-main base (e.g., a release line) is supported.
	BaseBranch string `json:"base_branch"`
	// SchedulerRunID is the scheduler run that originally dispatched both
	// siblings. Carried into the chain event payload so the operator can
	// correlate the rebase outcome to the spec dispatch.
	SchedulerRunID string `json:"scheduler_run_id"`
	// SourcePRNumber is the sibling PR whose merge to BaseBranch triggered
	// this rebase. Carried into the chain event payload for the same
	// correlation reason.
	SourcePRNumber int `json:"source_pr_number"`
	// WorkUnitID is the orchestration handle for this rebase. Used to slug
	// the worktree directory and the chain event correlation id; usually
	// derived from PRNumber.
	WorkUnitID string `json:"work_unit_id"`
	// Repo is the GitHub owner/name pair (e.g. "chitinhq/chitin") used to
	// build operator-facing links (Discord escalation notices). Optional —
	// when empty, escalation notices fall back to a URL-less reference and
	// the helper logs a warning.
	Repo string `json:"repo,omitempty"`
}

// RebaseSiblingPRResult is the typed outcome of one RebaseSiblingPR
// invocation. The activity always returns a nil error and folds every
// outcome — including a conflict that left the branch untouched — into the
// result, so the workflow settles on the rebase outcome rather than retrying
// a non-transient git fault.
type RebaseSiblingPRResult struct {
	// Rebased is true iff the branch was rewritten and force-pushed.
	Rebased bool `json:"rebased"`
	// NewHeadSHA is the rewritten branch tip after a successful rebase,
	// empty when Rebased is false.
	NewHeadSHA string `json:"new_head_sha"`
	// NewBaseSHA is the BaseBranch commit the rebase landed onto, populated
	// whenever the activity successfully fetched the base — even on a
	// conflict outcome — so the operator sees what the rebase was aiming at.
	NewBaseSHA string `json:"new_base_sha"`
	// ConflictFiles lists paths git's rebase marked unmerged (UU / AA / DD
	// in `git status --porcelain`). Populated only on the conflict path.
	ConflictFiles []string `json:"conflict_files"`
	// Explanation is a human-readable account of how far the rebase reached.
	Explanation string `json:"explanation"`
}

// RebaseSiblingPR is the spec 112 US2 auto-rebase activity (FR-005, FR-006).
// On a sibling PR merge to main, the activity rebases each in-flight sibling
// PR onto the new base, force-pushes the rewritten branch on success, and
// surfaces the outcome via a chain event so the operator can audit which
// rebases the orchestrator applied.
//
// The activity carries one startup-bound dependency — the worktree Manager —
// so the dedicated checkout, teardown, and GC integration match the per-work-
// unit lifecycle. On any path the worktree is reclaimed by a deferred
// Teardown, so a faulted rebase never leaks its checkout.
//
// Fail-soft per FR-005: a rebase conflict is a real outcome, not an activity
// fault. Execute always returns a nil error; the workflow reads Rebased to
// settle the sibling-rebase result done or conflict.
type RebaseSiblingPR struct {
	// manager is the orchestrator's worktree Manager, used to mint a
	// dedicated checkout of the PR's branch.
	manager *worktree.Manager
}

// NewRebaseSiblingPR returns a RebaseSiblingPR activity bound to mgr.
func NewRebaseSiblingPR(mgr *worktree.Manager) *RebaseSiblingPR {
	return &RebaseSiblingPR{manager: mgr}
}

// ActivityName is the stable Temporal activity name RebaseSiblingPR
// registers under.
func (a *RebaseSiblingPR) ActivityName() string { return "RebaseSiblingPR" }

// Execute rebases the sibling PR's branch onto BaseBranch. It is the
// activity function registered with the Temporal worker.
//
// On success it force-pushes the rewritten branch and emits a
// `sibling_rebase_dispatched` chain event. On conflict it aborts the rebase
// cleanly (no in-flight rebase state left on the branch), records the
// conflicting files, and emits a `sibling_rebase_failed` chain event. Both
// paths return a populated result with a nil error.
func (a *RebaseSiblingPR) Execute(ctx context.Context, in RebaseSiblingPRInput) (RebaseSiblingPRResult, error) {
	if a.manager == nil {
		return RebaseSiblingPRResult{
			Explanation: "no worktree Manager bound — sibling rebase not attempted",
		}, nil
	}
	if in.PRBranch == "" || in.TargetRepo == "" {
		return RebaseSiblingPRResult{
			Explanation: "missing PRBranch or TargetRepo — sibling rebase not attempted",
		}, nil
	}
	base := in.BaseBranch
	if base == "" {
		base = "main"
	}
	workUnitID := in.WorkUnitID
	if workUnitID == "" {
		workUnitID = fmt.Sprintf("rebase-pr-%d", in.PRNumber)
	}

	// Mint a dedicated checkout of the PR branch. The Manager.Checkout
	// fetches origin first, so origin/<base> is also fresh.
	wt, err := a.manager.Checkout(in.TargetRepo, in.PRBranch, workUnitID)
	if err != nil {
		return RebaseSiblingPRResult{
			Explanation: fmt.Sprintf("worktree checkout failed: %v", err),
		}, nil
	}
	// Always reclaim the worktree, success or failure.
	defer func() {
		_ = a.manager.Teardown(wt)
	}()

	res := RebaseSiblingPRResult{}

	// Resolve the new base SHA so the operator sees what the rebase aimed
	// at even on the conflict path.
	if sha, err := gitOutput(ctx, wt, "rev-parse", "--verify", "--quiet", "origin/"+base+"^{commit}"); err == nil {
		res.NewBaseSHA = sha
	}

	// Attempt the rebase. `git rebase origin/<base>` rewrites the current
	// branch onto the freshly fetched remote base. A non-zero exit can mean
	// EITHER a merge conflict (the case auto-rebase exists to detect) OR an
	// underlying git fault (missing origin/<base>, detached HEAD, repo
	// corruption). The two paths surface differently: a real conflict has
	// non-empty conflict files in `git status --porcelain`; a non-conflict
	// fault has none. Both still emit `sibling_rebase_failed` so the operator
	// sees something, but the Explanation carries the underlying git stderr
	// so a non-conflict fault is not silently mislabeled as "0 conflicts".
	if _, rebaseErr := gitOutput(ctx, wt, "rebase", "origin/"+base); rebaseErr != nil {
		conflicts := readConflictFiles(ctx, wt)
		// `git rebase --abort` returns the worktree to pre-rebase state. A
		// failed abort is non-fatal — the worktree is going away on teardown.
		_, _ = gitOutput(ctx, wt, "rebase", "--abort")
		res.ConflictFiles = conflicts
		switch len(conflicts) {
		case 0:
			res.Explanation = fmt.Sprintf(
				"rebase onto origin/%s failed without conflict files (likely git fault): %v; branch left untouched",
				base, rebaseErr)
		default:
			res.Explanation = fmt.Sprintf(
				"rebase onto origin/%s produced %d conflicting file(s); branch left untouched for manual resolution",
				base, len(conflicts))
		}
		emitSiblingRebaseEvent(ctx, "sibling_rebase_failed", in, res)
		return res, nil
	}

	// Rebase succeeded — capture the new head, then force-push with lease so
	// a concurrent operator push isn't silently clobbered.
	if sha, err := gitOutput(ctx, wt, "rev-parse", "HEAD"); err == nil {
		res.NewHeadSHA = sha
	}
	if _, err := gitOutput(ctx, wt, "push", "--force-with-lease", "origin", in.PRBranch); err != nil {
		// Rebase rewrote history locally but the push failed (lease lost to a
		// concurrent push, or network). The local worktree is going away on
		// teardown; the remote branch is untouched. Surface as a conflict-style
		// outcome so the operator sees it — same chain event, populated detail.
		res.Rebased = false
		res.Explanation = fmt.Sprintf(
			"rebase onto origin/%s succeeded locally but force-push failed: %v",
			base, err)
		emitSiblingRebaseEvent(ctx, "sibling_rebase_failed", in, res)
		return res, nil
	}

	res.Rebased = true
	res.Explanation = fmt.Sprintf(
		"rebased PR #%d branch %s onto origin/%s (new head %s)",
		in.PRNumber, in.PRBranch, base, shortSHA(res.NewHeadSHA))
	emitSiblingRebaseEvent(ctx, "sibling_rebase_dispatched", in, res)
	return res, nil
}

// readConflictFiles returns the paths git's last rebase marked unmerged.
// `git status --porcelain` lists them with two-letter codes UU, AA, DD, UA,
// AU, DU, UD — the "U" or matched letter-pair patterns. Returns sorted
// deduplicated paths.
func readConflictFiles(ctx context.Context, worktreePath string) []string {
	out, err := gitOutput(ctx, worktreePath, "status", "--porcelain")
	if err != nil {
		return nil
	}
	seen := map[string]struct{}{}
	for _, line := range strings.Split(out, "\n") {
		if len(line) < 3 {
			continue
		}
		code := line[:2]
		if !isConflictCode(code) {
			continue
		}
		path := strings.TrimSpace(line[3:])
		if path == "" {
			continue
		}
		seen[path] = struct{}{}
	}
	if len(seen) == 0 {
		return nil
	}
	paths := make([]string, 0, len(seen))
	for p := range seen {
		paths = append(paths, p)
	}
	// Sort for deterministic output — operators and tests compare on it.
	sortStrings(paths)
	return paths
}

// isConflictCode reports whether a porcelain two-letter status code marks an
// unmerged path. Per git-status(1): DD, AU, UD, UA, DU, AA, UU.
func isConflictCode(code string) bool {
	switch code {
	case "DD", "AU", "UD", "UA", "DU", "AA", "UU":
		return true
	}
	return false
}

// sortStrings sorts s in place. Tiny insertion sort — the path count for a
// single rebase conflict is always small.
func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j-1] > s[j]; j-- {
			s[j-1], s[j] = s[j], s[j-1]
		}
	}
}

// shortSHA returns the first 8 hex characters of a SHA, or the SHA itself if
// it is already short. Empty input yields empty output. Used only for human
// log lines — the full SHA travels in NewHeadSHA.
func shortSHA(sha string) string {
	if len(sha) <= 8 {
		return sha
	}
	return sha[:8]
}

// gitOutput runs `git <args...>` in dir and returns trimmed stdout. A non-zero
// exit yields an error carrying git's stderr. Mirrors the activities/deliver.go
// helper but lives here so the sibling-rebase activity doesn't depend on the
// deliver activity's package-private function.
func gitOutput(ctx context.Context, dir string, args ...string) (string, error) {
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

// emitSiblingRebaseEvent writes one spec-112 chain event via the kernel's
// emit subcommand. Mirrors cmd/chitin-orchestrator/emit.go's pattern — temp
// file + `chitin-kernel emit -event-file <path>` — but lives here so the
// activity has no dependency on the orchestrator CLI package.
//
// Fail-soft: a missing kernel binary, a failed write, or a non-zero exit
// only logs a warning to stderr. The rebase outcome carried in res is the
// load-bearing signal; the chain entry is supplementary audit.
//
// ALSO fires a Discord escalation notice on the failure path
// (sibling_rebase_failed) — the operator's only signal that the
// auto-rebase couldn't resolve a cascade and manual intervention is
// needed. The success path (sibling_rebase_dispatched) does NOT ping
// Discord since the autopilot completed cleanly.
func emitSiblingRebaseEvent(ctx context.Context, eventType string, in RebaseSiblingPRInput, res RebaseSiblingPRResult) {
	if eventType == "sibling_rebase_failed" {
		notifyDiscordEscalation(ctx, EscalationNotice{
			EventType: eventType,
			Severity:  SeverityAlert,
			PRNumber:  in.PRNumber,
			PRURL:     siblingRebasePRURL(in),
			Reason:    "sibling_rebase_failed",
			Detail:    res.Explanation,
		})
	}

	// Allow tests and sandboxed environments to opt out of the shell-out.
	if os.Getenv("CHITIN_DISABLE_CHAIN_EMIT") == "1" {
		return
	}

	binPath := os.Getenv("CHITIN_KERNEL_BIN")
	if binPath == "" {
		binPath = "chitin-kernel"
	}

	payload := map[string]any{
		"pr_number":        in.PRNumber,
		"pr_branch":        in.PRBranch,
		"base_branch":      stringOrDefault(in.BaseBranch, "main"),
		"scheduler_run_id": in.SchedulerRunID,
		"source_pr_number": in.SourcePRNumber,
		"new_base_sha":     res.NewBaseSHA,
		"new_head_sha":     res.NewHeadSHA,
		"conflict_files":   res.ConflictFiles,
		"rebased":          res.Rebased,
		"explanation":      res.Explanation,
	}
	envelope := map[string]any{
		"schema_version":    "2",
		"event_type":        eventType,
		"run_id":            in.SchedulerRunID,
		"session_id":        fmt.Sprintf("chitin-orchestrator-rebase-%d", in.PRNumber),
		"surface":           "chitin-orchestrator",
		"agent_instance_id": "chitin-orchestrator",
		"chain_type":        "scheduler",
		"ts":                time.Now().UTC().Format(time.RFC3339Nano),
		"payload":           payload,
	}
	body, err := json.Marshal(envelope)
	if err != nil {
		warnEmit("marshal: %v — %s recorded only in workflow result", err, eventType)
		return
	}

	tmp, err := os.CreateTemp("", "chitin-rebase-emit-*.json")
	if err != nil {
		warnEmit("temp file: %v — %s recorded only in workflow result", err, eventType)
		return
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.Write(body); err != nil {
		tmp.Close()
		warnEmit("temp write: %v — %s recorded only in workflow result", err, eventType)
		return
	}
	if err := tmp.Close(); err != nil {
		warnEmit("temp close: %v — %s recorded only in workflow result", err, eventType)
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
		warnEmit("kernel emit failed: %v (stderr: %s) — %s recorded only in workflow result", err, tail, eventType)
	}
}

// stringOrDefault returns s when non-empty, otherwise def.
func stringOrDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

// siblingRebasePRURL builds the github.com PR link for the input's
// Repo + PRNumber. Returns "" when Repo is empty so the Discord
// helper drops the notice with a clear warning rather than posting
// a broken link.
func siblingRebasePRURL(in RebaseSiblingPRInput) string {
	if in.Repo == "" || in.PRNumber == 0 {
		return ""
	}
	return fmt.Sprintf("https://github.com/%s/pull/%d", in.Repo, in.PRNumber)
}

// warnEmit logs a chain-emit warning. Goes to stderr so the worker host's
// journald entry captures it; the rebase outcome itself never depends on it.
func warnEmit(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "warning: sibling-rebase chain emit: "+format+"\n", args...)
}
