// Package queue powers the operator escalation surface (spec 114).
//
// live.go fetches the open-PR live state from GitHub via the `gh` CLI and
// decorates each PR with two derived fields the filter (spec 114 FR-003)
// needs but `gh pr list --json` cannot return directly:
//
//  1. SpecRef — the scheduler-run id parsed from a `sched/run/<id>` label
//     (spec 112 US2 convention).
//  2. LastAutomatedCommit — the most recent commit on the PR's head branch
//     authored by the orchestrator identity. Used by the
//     `stale_no_automation` rule.
//
// The package is a pure reader of GitHub state; it never mutates a PR.
package queue

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/chitinhq/chitin/go/orchestrator/activities"
)

// Label is the subset of `gh pr list --json labels` we consume.
type Label struct {
	Name string `json:"name"`
}

// Review is the subset of `gh pr list --json reviews` we consume.
// State is one of GitHub's review-state strings: APPROVED, CHANGES_REQUESTED,
// COMMENTED, DISMISSED, PENDING.
type Review struct {
	Author struct {
		Login string `json:"login"`
	} `json:"author"`
	AuthorAssociation string    `json:"authorAssociation"`
	State             string    `json:"state"`
	SubmittedAt       time.Time `json:"submittedAt"`
}

// LivePR is one decorated entry from `gh pr list --json ...`.
//
// The first seven fields mirror the JSON shape returned by gh; SpecRef and
// LastAutomatedCommit are the two decorations applied by ListLive.
type LivePR struct {
	Number      int       `json:"number"`
	Title       string    `json:"title"`
	HeadRefName string    `json:"headRefName"`
	Labels      []Label   `json:"labels"`
	Mergeable   string    `json:"mergeable"`
	UpdatedAt   time.Time `json:"updatedAt"`
	Reviews     []Review  `json:"reviews"`

	// SpecRef is the scheduler-run id parsed from the first sched/run/<id>
	// label on the PR, or "" when no such label is present.
	SpecRef string `json:"-"`

	// LastAutomatedCommit is the committedDate of the most recent commit on
	// the PR's head branch authored by the orchestrator identity
	// (AutomatedAuthorEmail). Zero when no automated commit exists, or when
	// the per-PR lookup faulted — the queue surfaces what it can.
	LastAutomatedCommit time.Time `json:"-"`
}

// GhRunner is the seam tests stub to avoid spawning a real gh process.
// Production callers use [ListLive]; tests call [ListLiveWith] with a fake.
type GhRunner func(ctx context.Context, args ...string) ([]byte, error)

// ListLive runs the standard `gh pr list --json ...` query for open PRs in
// repo (or the gh-detected default when repo == "") and decorates each entry
// with SpecRef and LastAutomatedCommit. See package doc for the contract.
func ListLive(ctx context.Context, repo string) ([]LivePR, error) {
	return ListLiveWith(ctx, repo, runGH)
}

// ListLiveWith is [ListLive] with an injected gh runner — exists only so
// unit tests can run hermetically.
func ListLiveWith(ctx context.Context, repo string, gh GhRunner) ([]LivePR, error) {
	if gh == nil {
		gh = runGH
	}
	args := []string{
		"pr", "list",
		"--search", "is:open",
		"--limit", "100",
		"--json", "number,title,headRefName,labels,mergeable,updatedAt,reviews",
	}
	if repo != "" {
		args = append(args, "--repo", repo)
	}
	out, err := gh(ctx, args...)
	if err != nil {
		return nil, fmt.Errorf("gh pr list: %w", err)
	}
	var prs []LivePR
	if err := json.Unmarshal(out, &prs); err != nil {
		return nil, fmt.Errorf("gh pr list json decode: %w", err)
	}
	for i := range prs {
		prs[i].SpecRef = SpecRefFromLabels(prs[i].Labels)
	}
	decorateAutomatedCommit(ctx, repo, prs, gh)
	return prs, nil
}

// SpecRefFromLabels returns the scheduler-run id encoded in the first
// `sched/run/<id>` label, or "" when no such label is present.
func SpecRefFromLabels(labels []Label) string {
	for _, l := range labels {
		if !strings.HasPrefix(l.Name, activities.SchedRunLabelPrefix) {
			continue
		}
		ref := strings.TrimPrefix(l.Name, activities.SchedRunLabelPrefix)
		if ref != "" {
			return ref
		}
	}
	return ""
}

// AutomatedAuthorEmail is the git identity the orchestrator commits with
// (set in activities/deliver.go via `git -c user.email=...`). The
// stale_no_automation rule (FR-003) treats a PR as automated iff its head
// branch carries a commit authored by this identity.
const AutomatedAuthorEmail = "orchestrator@chitin.local"

// commitAuthor mirrors the per-author entry under `gh pr view --json commits`
// — each commit may have one or more authors (a co-authored-by trailer fans
// out into the authors list).
type commitAuthor struct {
	Email string `json:"email"`
	Name  string `json:"name"`
}

// commitsResponse is the subset of `gh pr view --json commits` output we
// consume — only the per-commit authors + committedDate.
type commitsResponse struct {
	Commits []struct {
		Authors       []commitAuthor `json:"authors"`
		CommittedDate time.Time      `json:"committedDate"`
	} `json:"commits"`
}

// decorateAutomatedCommit fills LastAutomatedCommit on every entry by calling
// `gh pr view <n> --json commits` with bounded parallelism. Per-PR lookup
// faults are tolerated — LastAutomatedCommit stays zero and the filter
// treats the PR as having no automation, which is the correct conservative
// behavior under SC-002's 2-second budget.
func decorateAutomatedCommit(ctx context.Context, repo string, prs []LivePR, gh GhRunner) {
	if len(prs) == 0 {
		return
	}
	const workers = 8
	sem := make(chan struct{}, workers)
	var wg sync.WaitGroup
	for i := range prs {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int) {
			defer wg.Done()
			defer func() { <-sem }()
			ts, err := mostRecentAutomatedCommit(ctx, repo, prs[i].Number, gh)
			if err != nil {
				return
			}
			prs[i].LastAutomatedCommit = ts
		}(i)
	}
	wg.Wait()
}

// mostRecentAutomatedCommit returns the latest committedDate among commits
// authored by AutomatedAuthorEmail on pr's head branch. Zero time when no
// automated commit is present.
func mostRecentAutomatedCommit(ctx context.Context, repo string, pr int, gh GhRunner) (time.Time, error) {
	args := []string{"pr", "view", strconv.Itoa(pr), "--json", "commits"}
	if repo != "" {
		args = append(args, "--repo", repo)
	}
	out, err := gh(ctx, args...)
	if err != nil {
		return time.Time{}, fmt.Errorf("gh pr view %d: %w", pr, err)
	}
	var resp commitsResponse
	if err := json.Unmarshal(out, &resp); err != nil {
		return time.Time{}, fmt.Errorf("gh pr view %d commits decode: %w", pr, err)
	}
	var latest time.Time
	for _, c := range resp.Commits {
		if !commitIsAutomated(c.Authors) {
			continue
		}
		if c.CommittedDate.After(latest) {
			latest = c.CommittedDate
		}
	}
	return latest, nil
}

func commitIsAutomated(authors []commitAuthor) bool {
	for _, a := range authors {
		if strings.EqualFold(a.Email, AutomatedAuthorEmail) {
			return true
		}
	}
	return false
}

// runGH is the production shell-out. Tests inject a stub via [ListLiveWith].
func runGH(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "gh", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}
