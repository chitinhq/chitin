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
)

// DeliverWorkProductInput is the typed input to the DeliverWorkProduct
// activity: a completed agent work unit's worktree plus the metadata needed to
// commit its changes, push the dedicated branch, and open a pull request — the
// spec-070 PR-out gate.
type DeliverWorkProductInput struct {
	// WorkUnitID is the DAG node the work unit ran — woven into the commit and
	// PR text for correlation.
	WorkUnitID string `json:"work_unit_id"`
	// SpecRef is the source spec the work derives from.
	SpecRef string `json:"spec_ref"`
	// TaskRef is the task within the spec.
	TaskRef string `json:"task_ref"`
	// Description is the task instruction — the commit subject and PR title.
	Description string `json:"description"`
	// WorktreePath is the dedicated worktree the agent worked in. Its current
	// branch is the work product; delivery commits, pushes, and PRs it.
	WorktreePath string `json:"worktree_path"`
	// BaseRef is the branch the pull request targets.
	BaseRef string `json:"base_ref"`
	// SchedulerRunID is the scheduler run this work unit belongs to. Delivery
	// stamps it onto the opened PR as a `sched/run/<id>` label so the
	// spec 112 US2 auto-rebase path can list every sibling PR by label when a
	// chitin-authored PR merges to main. Empty leaves the PR unlabeled —
	// auto-rebase then skips it (correct fallback for non-scheduler deliveries).
	SchedulerRunID string `json:"scheduler_run_id"`
}

const WorkUnitCompletedWithoutDeliverableEventType = "work_unit_completed_without_deliverable"

const (
	DeliverableKindPR         = "pr"
	DeliverableKindFile       = "file"
	DeliverableKindChainEvent = "chain_event"

	WorkUnitNoChangesToCommitReason              = "no_changes_to_commit"
	WorkUnitGitPushFailedReason                  = "git_push_failed"
	WorkUnitGHPRCreateFailedReason               = "gh_pr_create_failed"
	WorkUnitActivityDeclinedWithoutFailureReason = "activity_declined_without_failure"
)

// WorkUnitCompletedWithoutDeliverablePayload is the chain payload emitted
// when an activity reports nominal success without producing its deliverable.
type WorkUnitCompletedWithoutDeliverablePayload struct {
	WorkUnitID      string `json:"work_unit_id"`
	TaskID          string `json:"task_id"`
	SpecRef         string `json:"spec_ref"`
	DeliverableKind string `json:"deliverable_kind"`
	Reason          string `json:"reason"`
}

// DeliverWorkProductResult is the typed output of the DeliverWorkProduct
// activity — how far delivery progressed and the reference to the work product.
// Every field reflects an outcome, never a fault: a delivery that cannot push
// or PR still reports Committed with the reason in Explanation, so the work
// unit records exactly how far the work reached.
type DeliverWorkProductResult struct {
	// Branch is the worktree's dedicated branch — the work product.
	Branch string `json:"branch"`
	// Committed is true once the agent's changes are committed to Branch.
	Committed bool `json:"committed"`
	// CommitSHA is the delivered commit, empty when nothing was committed.
	CommitSHA string `json:"commit_sha"`
	// Pushed is true once Branch is pushed to the target repo's origin remote.
	Pushed bool `json:"pushed"`
	// PRURL is the opened pull request, empty when no PR could be opened.
	PRURL string `json:"pr_url"`
	// Explanation is a human-readable account of how far delivery reached.
	Explanation string `json:"explanation"`
}

// DeliverWorkProduct is the PR-out-gate activity (spec 070): after an agent
// work unit succeeds it commits the worktree, pushes the dedicated branch, and
// opens a pull request, so the orchestrator SHIPS reviewable work rather than
// leaving it to be reclaimed with the worktree.
//
// Committing, pushing, and opening a PR are side effects — git and gh
// subprocess I/O — so this MUST run in an activity, never in workflow code. It
// carries no startup-bound dependency: delivery is a self-contained sequence
// over the worktree, so a zero-value DeliverWorkProduct is usable.
//
// Delivery degrades gracefully and step by step. No agent changes → nothing is
// committed. No `origin` remote → the commit lands locally but is not pushed.
// No `gh` CLI, or a `gh` fault → the branch is pushed but no PR opens. Each
// outcome is reported in the Result, never as an activity error — the agent's
// work already succeeded, and only its delivery degraded.
type DeliverWorkProduct struct{}

// NewDeliverWorkProduct returns a DeliverWorkProduct activity. It takes no
// dependencies — delivery is a self-contained sequence over the worktree.
func NewDeliverWorkProduct() *DeliverWorkProduct { return &DeliverWorkProduct{} }

// ActivityName is the stable Temporal activity name DeliverWorkProduct
// registers under and WorkUnitWorkflow dispatches to.
func (a *DeliverWorkProduct) ActivityName() string { return "DeliverWorkProduct" }

// Execute commits, pushes, and opens a PR for one completed work unit's
// worktree. It is the activity function registered with the Temporal worker.
//
// It always returns a nil error: every outcome — including a failed push or a
// missing gh CLI — is carried in the Result, so the work unit settles on the
// agent's success while recording exactly how far delivery reached. The error
// return is unused, reserved for a future fault the caller must retry.
func (a *DeliverWorkProduct) Execute(ctx context.Context, in DeliverWorkProductInput) (DeliverWorkProductResult, error) {
	wt := strings.TrimSpace(in.WorktreePath)
	if wt == "" {
		return DeliverWorkProductResult{Explanation: "no worktree path — nothing to deliver"}, nil
	}

	// Resolve the worktree's dedicated branch — the work product.
	branch, err := git(ctx, wt, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil || branch == "" || branch == "HEAD" {
		return DeliverWorkProductResult{
			Explanation: fmt.Sprintf("cannot resolve the worktree branch to deliver: %v", err),
		}, nil
	}
	res := DeliverWorkProductResult{Branch: branch}

	// Are there any agent changes to deliver?
	status, err := git(ctx, wt, "status", "--porcelain")
	if err != nil {
		res.Explanation = fmt.Sprintf("cannot read worktree status: %v", err)
		return res, nil
	}
	if strings.TrimSpace(status) == "" {
		emitWorkUnitCompletedWithoutDeliverable(ctx, in, WorkUnitNoChangesToCommitReason)
		res.Explanation = "agent produced no changes — missing deliverable pr: nothing to deliver"
		return res, nil
	}

	// Commit. A fixed orchestrator identity is passed explicitly so the commit
	// never depends on the worktree's git config being set.
	if _, err := git(ctx, wt, "add", "-A"); err != nil {
		res.Explanation = fmt.Sprintf("git add failed: %v", err)
		return res, nil
	}
	subject, body := commitMessage(in)
	if _, err := git(ctx, wt,
		"-c", "user.name=Chitin Orchestrator",
		"-c", "user.email=orchestrator@chitin.local",
		"commit", "-m", subject, "-m", body,
	); err != nil {
		res.Explanation = fmt.Sprintf("git commit failed: %v", err)
		return res, nil
	}
	res.Committed = true
	if sha, shaErr := git(ctx, wt, "rev-parse", "HEAD"); shaErr == nil {
		res.CommitSHA = sha
	}

	// Push — only if the target repo has an `origin` remote.
	if _, err := git(ctx, wt, "remote", "get-url", "origin"); err != nil {
		emitWorkUnitCompletedWithoutDeliverable(ctx, in, WorkUnitActivityDeclinedWithoutFailureReason)
		res.Explanation = "committed locally; missing deliverable pr: the target repo has no 'origin' remote — branch not pushed"
		return res, nil
	}
	if _, err := git(ctx, wt, "push", "-u", "origin", branch); err != nil {
		emitWorkUnitCompletedWithoutDeliverable(ctx, in, WorkUnitGitPushFailedReason)
		res.Explanation = fmt.Sprintf("committed; missing deliverable pr: pushing branch %s to origin failed: %v", branch, err)
		return res, nil
	}
	res.Pushed = true

	// Open a pull request — only if the gh CLI is available.
	if _, err := exec.LookPath("gh"); err != nil {
		emitWorkUnitCompletedWithoutDeliverable(ctx, in, WorkUnitActivityDeclinedWithoutFailureReason)
		res.Explanation = fmt.Sprintf(
			"committed and pushed branch %s; missing deliverable pr: gh CLI not available — no PR opened", branch)
		return res, nil
	}
	prURL, err := openPR(ctx, wt, in, branch, subject)
	if err != nil {
		emitWorkUnitCompletedWithoutDeliverable(ctx, in, WorkUnitGHPRCreateFailedReason)
		res.Explanation = fmt.Sprintf(
			"committed and pushed branch %s; missing deliverable pr: opening the PR failed: %v", branch, err)
		return res, nil
	}
	res.PRURL = prURL

	// Stamp the scheduler run id onto the PR as a sched/run/<id> label so the
	// spec 112 US2 auto-rebase path can list every sibling PR by label when a
	// chitin-authored PR merges to main. A label failure is non-fatal — the
	// PR is already open; the operator can manually rebase if auto-rebase
	// cannot find the sibling. An empty SchedulerRunID skips the label
	// entirely (delivery from a non-scheduler context).
	label := siblingLabelFor(in.SchedulerRunID)
	if label != "" {
		if _, lblErr := applyPRLabel(ctx, wt, prURL, label); lblErr != nil {
			res.Explanation = fmt.Sprintf(
				"delivered: committed, pushed branch %s, opened PR %s; sched-run label apply failed: %v",
				branch, prURL, lblErr)
			return res, nil
		}
	}
	res.Explanation = fmt.Sprintf("delivered: committed, pushed branch %s, opened PR %s", branch, prURL)
	return res, nil
}

func emitWorkUnitCompletedWithoutDeliverable(ctx context.Context, in DeliverWorkProductInput, reason string) {
	payload := WorkUnitCompletedWithoutDeliverablePayload{
		WorkUnitID:      in.WorkUnitID,
		TaskID:          in.TaskRef,
		SpecRef:         in.SpecRef,
		DeliverableKind: DeliverableKindPR,
		Reason:          reason,
	}
	emitDeliverChainEvent(ctx, WorkUnitCompletedWithoutDeliverableEventType, in.WorkUnitID, payload)
}

func emitDeliverChainEvent(ctx context.Context, eventType, runID string, payload any) {
	if os.Getenv("CHITIN_DISABLE_CHAIN_EMIT") == "1" {
		return
	}
	binPath := os.Getenv("CHITIN_KERNEL_BIN")
	if binPath == "" {
		binPath = "chitin-kernel"
	}
	envelope := map[string]any{
		"schema_version":    "2",
		"event_type":        eventType,
		"run_id":            runID,
		"session_id":        fmt.Sprintf("chitin-orchestrator-deliver-%s", runID),
		"surface":           "chitin-orchestrator",
		"agent_instance_id": "chitin-orchestrator",
		"chain_type":        "scheduler",
		"ts":                time.Now().UTC().Format(time.RFC3339Nano),
		"payload":           payload,
	}
	body, err := json.Marshal(envelope)
	if err != nil {
		warnDeliver("marshal: %v — %s recorded only in activity result", err, eventType)
		return
	}
	tmp, err := os.CreateTemp("", "chitin-deliver-emit-*.json")
	if err != nil {
		warnDeliver("temp file: %v — %s recorded only in activity result", err, eventType)
		return
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.Write(body); err != nil {
		tmp.Close()
		warnDeliver("temp write: %v — %s recorded only in activity result", err, eventType)
		return
	}
	if err := tmp.Close(); err != nil {
		warnDeliver("temp close: %v — %s recorded only in activity result", err, eventType)
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
		warnDeliver("kernel emit failed: %v (stderr: %s) — %s recorded only in activity result", err, tail, eventType)
	}
}

func warnDeliver(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "warning: deliver chain emit: "+format+"\n", args...)
}

// siblingLabelFor returns the sched/run/<id> label string for one scheduler
// run, or "" when runID is empty. Exported logic (lowercase function name but
// kept in this file because deliver and sibling-rebase share the same label
// convention — the dispatcher in factory-listen reads the same prefix.
func siblingLabelFor(runID string) string {
	if runID == "" {
		return ""
	}
	return SchedRunLabelPrefix + runID
}

// SchedRunLabelPrefix is the chitin scheduler-run PR label prefix (spec 112
// US2). Every chitin-authored PR opened by the scheduler is labeled
// SchedRunLabelPrefix+runID so the auto-rebase path can list every sibling
// PR by label when one merges to main.
const SchedRunLabelPrefix = "sched/run/"

// applyPRLabel adds one label to an open pull request. CONTRARY TO an earlier
// comment in this file's history, `gh pr edit --add-label <name>` does NOT
// auto-create the label — it returns `'name' not found` if the label is
// absent. Per-(scheduler-run-id) labels like `sched/run/<uuid>` are by
// definition new on every dispatch, so this helper FIRST runs
// `gh label create <name> --force` (idempotent — `--force` updates color +
// description if the label already exists) and ONLY THEN runs
// `gh pr edit --add-label`. Failing to do the create step silently leaves
// the PR unlabeled and breaks the spec 112 US2 auto-rebase lister — observed
// in production after PR #1038, fixed here.
//
// A label-create failure is treated as fatal to this helper so the caller
// (DeliverWorkProduct.Execute) can record the shortfall. A successful create
// followed by a failed add still surfaces as failure for the same reason.
func applyPRLabel(ctx context.Context, worktreePath, prURL, label string) (string, error) {
	if _, err := exec.LookPath("gh"); err != nil {
		return "", fmt.Errorf("gh CLI not available: %w", err)
	}
	if err := ensureLabelExists(ctx, worktreePath, label); err != nil {
		return "", fmt.Errorf("ensure label %q exists: %w", label, err)
	}
	cmd := exec.CommandContext(ctx, "gh", "pr", "edit", prURL, "--add-label", label)
	cmd.Dir = worktreePath
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(stdout.String()), nil
}

// ensureLabelExists runs `gh label create <name> --force --color <hex>
// --description <text>`. `--force` makes the call idempotent: if the label
// already exists it updates color + description; if not it creates fresh.
// Color and description are best-effort stable conventions so operators
// recognise chitin-authored labels in the GitHub label list. The repo is
// inferred from the worktree's `origin` remote.
func ensureLabelExists(ctx context.Context, worktreePath, label string) error {
	cmd := exec.CommandContext(ctx, "gh", "label", "create", label,
		"--force",
		"--color", chitinLabelColor,
		"--description", chitinLabelDescription(label),
	)
	cmd.Dir = worktreePath
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("gh label create: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// chitinLabelColor is the standard fill color (hex, no #) used for every
// chitin-authored PR label. Picked to be visually distinct from the GitHub
// default palette without resembling an existing "type:" or "status:" badge.
const chitinLabelColor = "8b5cf6"

// chitinLabelDescription returns a human-readable description for the named
// chitin label. Stable text per prefix so updates remain idempotent.
func chitinLabelDescription(label string) string {
	if strings.HasPrefix(label, SchedRunLabelPrefix) {
		runID := strings.TrimPrefix(label, SchedRunLabelPrefix)
		return "Chitin scheduler run " + runID + " (spec 112 US2 sibling tracking)"
	}
	return "Applied by the Chitin orchestrator"
}

// openPR runs `gh pr create` from the worktree and returns the new PR's URL.
// gh detects the repository from the worktree's git remote; the PR targets the
// work unit's BaseRef.
func openPR(ctx context.Context, worktreePath string, in DeliverWorkProductInput, branch, title string) (string, error) {
	body := fmt.Sprintf(
		"Work unit `%s` (spec %s, task %s), delivered by the Chitin Orchestrator.\n\n%s",
		in.WorkUnitID, in.SpecRef, in.TaskRef, strings.TrimSpace(in.Description))
	cmd := exec.CommandContext(ctx, "gh", "pr", "create",
		"--base", in.BaseRef, "--head", branch, "--title", title, "--body", body)
	cmd.Dir = worktreePath
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(stdout.String()), nil
}

// commitMessage builds the commit subject and body for a delivered work unit.
// The subject is the task description, truncated by rune (never mid-rune) to a
// conventional length; the body records the work unit's provenance.
func commitMessage(in DeliverWorkProductInput) (subject, body string) {
	subject = strings.TrimSpace(in.Description)
	if subject == "" {
		subject = "chitin work unit " + in.WorkUnitID
	}
	if r := []rune(subject); len(r) > 72 {
		subject = string(r[:69]) + "..."
	}
	body = fmt.Sprintf(
		"Work unit %s (spec %s, task %s), produced by the Chitin Orchestrator.",
		in.WorkUnitID, in.SpecRef, in.TaskRef)
	return subject, body
}

// git runs `git <args...>` in dir and returns its trimmed stdout. A non-zero
// exit yields an error carrying git's stderr.
func git(ctx context.Context, dir string, args ...string) (string, error) {
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
