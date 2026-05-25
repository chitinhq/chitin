// Package queue implements the spec 114 operator escalation surface — the
// `chitin-orchestrator queue` subcommand and its scheduled-digest companion.
//
// This file (live.go) is the LIVE-PR reader: it shells out to `gh pr list`
// for every open PR in a repo and decorates each entry with the metadata
// the filter (T004) and renderers (T005-T007) consume:
//
//   - The scheduler run id parsed from the `sched/run/<id>` label
//     (spec 112 US2's tracking label, applied by activities.DeliverWorkProduct).
//   - The spec ref parsed from the head branch — chitin/wu/<NNN-slug>-tNNN-<suffix>
//     yields spec_ref=NNN-slug. The label-set itself does not carry the
//     spec ref directly; the work-unit branch convention does, and the
//     run-id label correlates the PR back to a chain-event row when the
//     scanner (T002) joins them.
//   - The "last automated action age" — the timestamp of the most-recent
//     commit authored by the chitin orchestrator identity
//     (orchestrator@chitin.local). Drives FR-005's last-automated-action-age
//     column and the FR-003 `stale_no_automation` rule.
//
// Per spec 114 FR-002 the queue is a READER ONLY — this file never mutates
// GitHub state. Per SC-002 the listing call is bounded (--limit 100) and the
// per-PR commit fetch is injected so tests don't shell out.
package queue

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/chitinhq/chitin/go/orchestrator/activities"
)

// Label is the JSON-mapped subset of `gh pr list --json labels` we read.
// The full GitHub label payload has color and description too — neither is
// needed for the queue surface.
type Label struct {
	Name string `json:"name"`
}

// Review is the JSON-mapped subset of `gh pr list --json reviews` we read.
// State + AuthorLogin are enough to drive the FR-003 `human_reviewer_present`
// rule without re-fetching review detail.
//
// SubmittedAt is a pointer because PENDING reviews carry `submittedAt: null`
// in the gh payload; a value-typed time.Time would fail the whole list decode.
type Review struct {
	State       string     `json:"state"`
	SubmittedAt *time.Time `json:"submittedAt"`
	Author      struct {
		Login string `json:"login"`
	} `json:"author"`
}

// LivePR is one open PR's worth of live state — the raw gh pr list fields
// plus the decorations FetchLive computes. Consumed by the filter (T004)
// and renderers (T005-T007); the latter format only what is set here.
type LivePR struct {
	// Raw gh pr list --json fields, per spec 114 T003's exact field set.
	Number      int       `json:"number"`
	Title       string    `json:"title"`
	HeadRefName string    `json:"headRefName"`
	Labels      []Label   `json:"labels"`
	Mergeable   string    `json:"mergeable"`
	UpdatedAt   time.Time `json:"updatedAt"`
	Reviews     []Review  `json:"reviews"`

	// Decorations added by FetchLive — populated AFTER the gh call returns.

	// SchedulerRunID is the scheduler run that opened this PR, parsed from
	// the first `sched/run/<id>` label. Empty when no such label exists —
	// the PR was opened outside the scheduler (human or external bot).
	SchedulerRunID string `json:"scheduler_run_id,omitempty"`
	// SpecRef is the work unit's spec directory name (e.g. "114-operator-escalation-surface"),
	// parsed from the head branch's chitin/wu/<slug>-<suffix> convention. Empty
	// when the head ref does not match the convention.
	SpecRef string `json:"spec_ref,omitempty"`
	// LastAutomatedCommitAt is the timestamp of the most-recent commit on
	// this PR authored by the chitin orchestrator identity. Nil when no
	// orchestrator-authored commit exists yet (the PR was just opened) or
	// the per-PR fetch failed (commitFetcher returned nil for that PR).
	LastAutomatedCommitAt *time.Time `json:"last_automated_commit_at,omitempty"`
}

// LastAutomatedCommitAge returns the duration since the most-recent
// orchestrator-authored commit, evaluated against `now`. Zero duration when
// LastAutomatedCommitAt is unset — callers MUST distinguish "no commit
// recorded" from "fresh commit" by inspecting LastAutomatedCommitAt first.
func (p LivePR) LastAutomatedCommitAge(now time.Time) time.Duration {
	if p.LastAutomatedCommitAt == nil {
		return 0
	}
	return now.Sub(*p.LastAutomatedCommitAt)
}

// PRLister is the seam FetchLive uses to issue the `gh pr list` call.
// Production binds it to listOpenPRsViaGH; tests inject a stub returning a
// fixed slice.
type PRLister func(ctx context.Context, repo string) ([]LivePR, error)

// CommitFetcher is the seam FetchLive uses to pull the most-recent
// orchestrator-authored commit timestamp for one PR. Production binds it
// to fetchLastAutomatedCommitViaGH; tests inject a stub.
//
// Returning (nil, nil) means "no orchestrator-authored commit found" — a
// valid steady state on a freshly-opened PR. Returning (nil, err) means the
// fetch faulted and the caller decides whether to surface or swallow it.
type CommitFetcher func(ctx context.Context, repo string, prNumber int) (*time.Time, error)

// fetchLiveConcurrency bounds how many per-PR `gh pr view` calls run in
// parallel. With `--limit 100` on the list call and a 15s per-fetch timeout,
// fully sequential fetches could wall-clock at ~25 minutes — too long for
// the daily digest path. A pool of 8 caps worst-case at ~3 minutes while
// keeping gh's rate-limit footprint modest.
const fetchLiveConcurrency = 8

// FetchLive returns every open PR in `repo`, decorated per spec 114 T003.
// Default seams shell out to `gh`; tests inject `lister` and `fetcher`.
//
// A commit-fetch fault for any single PR is swallowed (the decoration is
// left nil); the listing fault itself is fatal — a queue with no PRs is a
// real result, but a queue we could not enumerate is not.
//
// Per-PR commit fetches run concurrently with a bounded worker pool so the
// total wall-clock stays predictable even with 100 PRs.
func FetchLive(ctx context.Context, repo string, lister PRLister, fetcher CommitFetcher) ([]LivePR, error) {
	if lister == nil {
		lister = listOpenPRsViaGH
	}
	prs, err := lister(ctx, repo)
	if err != nil {
		return nil, fmt.Errorf("list open PRs: %w", err)
	}
	if fetcher == nil {
		fetcher = fetchLastAutomatedCommitViaGH
	}
	sem := make(chan struct{}, fetchLiveConcurrency)
	var wg sync.WaitGroup
	for i := range prs {
		decorateLabels(&prs[i])
		decorateSpecRef(&prs[i])
		wg.Add(1)
		sem <- struct{}{}
		go func(i int) {
			defer wg.Done()
			defer func() { <-sem }()
			if ts, err := fetcher(ctx, repo, prs[i].Number); err == nil && ts != nil {
				prs[i].LastAutomatedCommitAt = ts
			}
		}(i)
	}
	wg.Wait()
	return prs, nil
}

// decorateLabels extracts the SchedulerRunID from the first sched/run/<id>
// label on the PR. The label set is bounded (GitHub caps at 100 per PR),
// so a linear scan is fine; the first match wins because the scheduler
// stamps exactly one such label per PR by construction.
func decorateLabels(p *LivePR) {
	for _, l := range p.Labels {
		if strings.HasPrefix(l.Name, activities.SchedRunLabelPrefix) {
			runID := strings.TrimPrefix(l.Name, activities.SchedRunLabelPrefix)
			if runID != "" {
				p.SchedulerRunID = runID
				return
			}
		}
	}
}

// chitinWUSpecRefPattern matches the chitin/wu/<NNN-slug>-t<NNN>-<suffix>
// head-branch convention (worktree.Create + activities.DeliverWorkProduct).
// Capture group 1 is the spec ref: the NNN-slug portion before the -tNNN-
// task segment.
var chitinWUSpecRefPattern = regexp.MustCompile(`^chitin/wu/(\d+-[a-z0-9-]+?)-t\d+-[a-z0-9-]+$`)

// decorateSpecRef parses the spec ref out of a chitin/wu/* head branch
// name. Branches that do not match the convention leave SpecRef empty —
// the filter and renderers treat empty SpecRef as "unknown", which is the
// correct rendering for PRs opened outside the factory.
func decorateSpecRef(p *LivePR) {
	m := chitinWUSpecRefPattern.FindStringSubmatch(p.HeadRefName)
	if m == nil {
		return
	}
	p.SpecRef = m[1]
}

// DefaultPRLister returns the production PRLister that shells out to
// `gh pr list`. Separate from listOpenPRsViaGH so callers cleanly express
// "use the production seam" at the call site; tests still inject their
// own PRLister via FetchLive's parameter.
func DefaultPRLister() PRLister { return listOpenPRsViaGH }

// DefaultCommitFetcher returns the production CommitFetcher that shells
// out to `gh api /repos/<owner>/<name>/pulls/<n>/commits`. Mirrors
// DefaultPRLister's role as the named seam for production callers.
func DefaultCommitFetcher() CommitFetcher { return fetchLastAutomatedCommitViaGH }

// listOpenPRsViaGH is the production PRLister: shells out to
// `gh pr list --json number,title,headRefName,labels,mergeable,updatedAt,reviews
//
//	--search "is:open" --limit 100` and decodes the JSON array into LivePR.
//
// The `--search "is:open"` filter is redundant with the default state filter
// but per spec 114 T003's exact invocation — kept verbatim so the gh call
// matches the spec text and any future search-qualifier extension drops in
// without re-plumbing.
func listOpenPRsViaGH(ctx context.Context, repo string) ([]LivePR, error) {
	if _, err := exec.LookPath("gh"); err != nil {
		return nil, fmt.Errorf("gh CLI not available: %w", err)
	}
	args := []string{
		"pr", "list",
		"--json", "number,title,headRefName,labels,mergeable,updatedAt,reviews",
		"--search", "is:open",
		"--limit", "100",
	}
	if repo != "" {
		args = append(args, "--repo", repo)
	}
	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, "gh", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("gh pr list: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	var prs []LivePR
	if err := json.Unmarshal(stdout.Bytes(), &prs); err != nil {
		return nil, fmt.Errorf("gh pr list json decode: %w", err)
	}
	return prs, nil
}

// orchestratorCommitEmail is the git author email every chitin orchestrator
// commit carries (activities.DeliverWorkProduct + activities.PRIteration set
// it explicitly via `-c user.email`). Used as the identity filter for the
// last-automated-commit lookup.
const orchestratorCommitEmail = "orchestrator@chitin.local"

// ghCommitEntry is the JSON-mapped subset of `gh pr view --json commits` we
// read. Each commit carries an authors array (gh wraps both git and GitHub
// author identities into the same list); the most-recent entry whose author
// email matches orchestratorCommitEmail is the "last automated action".
type ghCommitEntry struct {
	CommittedDate time.Time `json:"committedDate"`
	AuthoredDate  time.Time `json:"authoredDate"`
	Authors       []struct {
		Email string `json:"email"`
		Login string `json:"login"`
		Name  string `json:"name"`
	} `json:"authors"`
}

// fetchLastAutomatedCommitViaGH is the production CommitFetcher: shells out
// to `gh pr view <number> --json commits` and returns the AuthoredDate of
// the latest commit whose author email is the chitin orchestrator identity.
//
// Returns (nil, nil) when no orchestrator commit exists — a valid state on
// a freshly-opened PR. A gh fault surfaces as (nil, err); FetchLive swallows
// it so a single broken PR cannot kill the whole queue listing.
func fetchLastAutomatedCommitViaGH(ctx context.Context, repo string, prNumber int) (*time.Time, error) {
	if _, err := exec.LookPath("gh"); err != nil {
		return nil, fmt.Errorf("gh CLI not available: %w", err)
	}
	args := []string{
		"pr", "view", fmt.Sprintf("%d", prNumber),
		"--json", "commits",
	}
	if repo != "" {
		args = append(args, "--repo", repo)
	}
	cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, "gh", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("gh pr view: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	var payload struct {
		Commits []ghCommitEntry `json:"commits"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		return nil, fmt.Errorf("gh pr view json decode: %w", err)
	}
	var latest *time.Time
	for i := range payload.Commits {
		c := &payload.Commits[i]
		if !commitAuthoredByOrchestrator(c) {
			continue
		}
		ts := commitTimestamp(c)
		if ts.IsZero() {
			continue
		}
		if latest == nil || ts.After(*latest) {
			tsCopy := ts
			latest = &tsCopy
		}
	}
	return latest, nil
}

// commitAuthoredByOrchestrator reports whether any author entry on the
// commit matches the chitin orchestrator identity. gh returns an authors
// array because a commit can carry both a git author and a GitHub-side
// attribution; matching ANY entry is correct — a Co-Authored-By trailer
// stamped by the orchestrator is still an automated action.
func commitAuthoredByOrchestrator(c *ghCommitEntry) bool {
	for _, a := range c.Authors {
		if strings.EqualFold(a.Email, orchestratorCommitEmail) {
			return true
		}
	}
	return false
}

// commitTimestamp returns the commit's effective time — AuthoredDate when
// present (the original work moment, preserved across rebases), falling
// back to CommittedDate. Both are populated by gh from the GraphQL
// schema; AuthoredDate is the right choice for the "when did the
// orchestrator last act" question.
func commitTimestamp(c *ghCommitEntry) time.Time {
	if !c.AuthoredDate.IsZero() {
		return c.AuthoredDate
	}
	return c.CommittedDate
}
