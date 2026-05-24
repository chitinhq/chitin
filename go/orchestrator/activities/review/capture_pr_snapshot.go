package review

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// CapturePRSnapshotActivityName is the stable Temporal activity name —
// must match the string PRReviewWorkflow uses to dispatch this activity
// (see workflows/pr_review.go:126 ExecuteActivity call).
const CapturePRSnapshotActivityName = "CapturePRSnapshot"

// GhRunner is the abstraction over `gh` CLI invocation. It exists so tests
// can inject a fake without spawning the real CLI; production binds the
// defaultGhRunner which shells out to the operator's authenticated `gh`.
//
// Run takes the gh-subcommand args (no leading "gh") and returns stdout
// or a wrapped error including stderr. The runner MUST honor ctx
// cancellation — long-hung `gh` calls would otherwise stall the activity
// past its StartToCloseTimeout.
type GhRunner interface {
	Run(ctx context.Context, args ...string) ([]byte, error)
}

type defaultGhRunner struct{}

// Run shells out to `gh` and returns stdout. exec.CommandContext honors
// ctx — if the workflow's activity timeout fires, gh is SIGKILL'd.
func (defaultGhRunner) Run(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "gh", args...)
	out, err := cmd.Output()
	if err != nil {
		// Surface stderr so the operator can see why gh failed (auth
		// expired, network down, repo not found, PR closed, etc.).
		var stderr string
		if ee, ok := err.(*exec.ExitError); ok {
			stderr = strings.TrimSpace(string(ee.Stderr))
		}
		if stderr != "" {
			return nil, fmt.Errorf("gh %s: %w; stderr=%s", args[0], err, stderr)
		}
		return nil, fmt.Errorf("gh %s: %w", args[0], err)
	}
	return out, nil
}

// CapturePRSnapshot is the FIRST activity PRReviewWorkflow dispatches
// (spec 094 R-SNAP). It captures the PR state — title, body, files,
// diffs, and any in-repo spec-kit artifacts the PR is bound to — at the
// moment the workflow starts, so later PR head movement does not
// invalidate in-flight reviewer verdicts.
//
// Side effects: three `gh` CLI invocations (one for metadata, one for
// the unified diff, and one HTTP fetch per spec-kit artifact). All I/O
// must run as an activity, never in workflow code (workflow determinism).
//
// The activity is bound to a GhRunner at worker-host startup; tests
// inject a fake. Production binds defaultGhRunner.
type CapturePRSnapshot struct {
	gh GhRunner
}

// NewCapturePRSnapshot builds a CapturePRSnapshot activity. A nil runner
// uses the default gh shell-out; production always passes nil.
func NewCapturePRSnapshot(gh GhRunner) *CapturePRSnapshot {
	if gh == nil {
		gh = defaultGhRunner{}
	}
	return &CapturePRSnapshot{gh: gh}
}

// ActivityName returns the activity's registered name for symmetry with
// the other review activities (they all expose ActivityName so the
// Register function can wire them by their declared name).
func (a *CapturePRSnapshot) ActivityName() string { return CapturePRSnapshotActivityName }

// ghPRView is the JSON shape returned by `gh pr view --json
// title,body,headRefOid,baseRefName,author,files`. Only the fields the
// activity uses are declared.
type ghPRView struct {
	Title       string `json:"title"`
	Body        string `json:"body"`
	HeadRefOid  string `json:"headRefOid"`
	BaseRefName string `json:"baseRefName"`
	Author      struct {
		Login string `json:"login"`
	} `json:"author"`
	Files []struct {
		Path      string `json:"path"`
		Additions int    `json:"additions"`
		Deletions int    `json:"deletions"`
	} `json:"files"`
}

// specArtifactPathRe matches paths under `.specify/specs/<NN-name>/`.
// Anchored at start so it matches only top-level spec-kit artifacts, not
// nested copies under worktrees or docs. The numeric prefix is required
// so the regex doesn't sweep in arbitrary `.specify/` config files.
var specArtifactPathRe = regexp.MustCompile(`^\.specify/specs/[0-9]+[A-Za-z0-9_\-]*(?:/.+)?$`)

// Execute captures the PR snapshot. Algorithm:
//
//  1. `gh pr view <PR> --repo <repo> --json title,body,headRefOid,baseRefName,author,files`
//     to read metadata + the file-touched list.
//  2. `gh pr diff <PR> --repo <repo>` to read the unified diff, split
//     per-file by the `diff --git a/... b/...` header convention.
//  3. For every file path under `.specify/specs/<NN-name>/`, fetch the
//     PR's post-merge content via `gh api repos/<repo>/contents/<path>?ref=<head>`
//     with `Accept: application/vnd.github.raw` so gh returns raw bytes
//     (not base64). Fetch failures are best-effort: log and continue, so
//     a transient API blip does not halt the entire dialectic.
//
// CapturedAt is RFC3339-stable (UTC), set once after all I/O so the field
// reflects when the snapshot was COMPLETED, not when it was started.
//
// Failure modes:
//   - gh pr view fails → return error; workflow halts the gate per FR-026.
//   - gh pr diff fails → return error; the diff is load-bearing.
//   - per-file spec-artifact fetch fails → silently skipped; the file's
//     Path still appears in Files[] with an empty Diff slot. Reviewer
//     drivers may consult Files[].Path even when SpecArtifacts is incomplete.
func (a *CapturePRSnapshot) Execute(ctx context.Context, in PRReviewInput) (PRSnapshot, error) {
	if in.Repo == "" {
		return PRSnapshot{}, fmt.Errorf("activities/review: CapturePRSnapshot requires Repo")
	}
	if in.PRNumber <= 0 {
		return PRSnapshot{}, fmt.Errorf("activities/review: CapturePRSnapshot requires PRNumber > 0 (got %d)", in.PRNumber)
	}

	prArg := strconv.Itoa(in.PRNumber)

	// Step 1 — metadata + files list.
	viewOut, err := a.gh.Run(ctx, "pr", "view", prArg,
		"--repo", in.Repo,
		"--json", "title,body,headRefOid,baseRefName,author,files")
	if err != nil {
		return PRSnapshot{}, fmt.Errorf("CapturePRSnapshot: gh pr view: %w", err)
	}
	var view ghPRView
	if err := json.Unmarshal(viewOut, &view); err != nil {
		return PRSnapshot{}, fmt.Errorf("CapturePRSnapshot: unmarshal gh pr view JSON: %w", err)
	}

	// Step 2 — full unified diff, split per file.
	diffOut, err := a.gh.Run(ctx, "pr", "diff", prArg, "--repo", in.Repo)
	if err != nil {
		return PRSnapshot{}, fmt.Errorf("CapturePRSnapshot: gh pr diff: %w", err)
	}
	perFile := splitUnifiedDiff(string(diffOut))

	// Per-file diff caps protect against Temporal's 2 MB activity-output
	// payload limit. A large refactor PR can blow past 2 MB easily —
	// 100 files × 30 KB diff each is already 3 MB. We cap each file's
	// diff at MaxPerFileDiffBytes and the total Files[].Diff budget at
	// MaxTotalDiffBytes; reviewer drivers see a "[diff truncated]" marker
	// where content was elided so they know to fetch via gh themselves
	// when they need the full hunks.
	files := make([]PRFile, len(view.Files))
	var totalDiffBytes int
	for i, f := range view.Files {
		raw := perFile[f.Path] // empty string is fine for pure rename/binary
		diff, originalSize := capDiff(raw, totalDiffBytes)
		totalDiffBytes += len(diff)
		_ = originalSize // could surface in a future Warnings field
		files[i] = PRFile{
			Path:      f.Path,
			Additions: f.Additions,
			Deletions: f.Deletions,
			Diff:      diff,
		}
	}

	// Step 3 — spec-kit artifact content at PR head, best-effort.
	//
	// The path is URL-encoded per-segment so paths containing reserved
	// characters (?, #, %, &, space, {, } …) round-trip correctly through
	// the gh api call. url.PathEscape on the whole path would over-encode
	// the segment separators '/'; we split, escape each segment, then
	// rejoin. Per-artifact failures are logged through the stdlib log
	// package (the same stream the orchestrator service writes to) so
	// operators have a debug trail without halting the dialectic.
	// stdlib log is preferred over activity.GetLogger here because the
	// latter panics outside a real activity context — making the
	// activity unusable in unit tests that drive Execute directly with
	// a plain context.Background().
	var artifacts []SpecArtifact
	for _, f := range view.Files {
		if !specArtifactPathRe.MatchString(f.Path) {
			continue
		}
		encodedPath := encodeURLPath(f.Path)
		raw, err := a.gh.Run(ctx, "api",
			"-H", "Accept: application/vnd.github.raw",
			fmt.Sprintf("repos/%s/contents/%s?ref=%s", in.Repo, encodedPath, view.HeadRefOid))
		if err != nil {
			// Best-effort — a transient API failure on one artifact must
			// not halt the dialectic, but it MUST be visible so the
			// operator can debug "why is the reviewer's view incomplete".
			log.Printf("CapturePRSnapshot: skipping spec artifact path=%q head_oid=%q err=%v",
				f.Path, view.HeadRefOid, err)
			continue
		}
		artifacts = append(artifacts, SpecArtifact{
			Path:    f.Path,
			Content: string(raw),
		})
	}

	return PRSnapshot{
		Repo:          in.Repo,
		PRNumber:      in.PRNumber,
		HeadOID:       view.HeadRefOid,
		Title:         view.Title,
		Body:          view.Body,
		Author:        view.Author.Login,
		BaseRef:       view.BaseRefName,
		Files:         files,
		SpecArtifacts: artifacts,
		CapturedAt:    time.Now().UTC(),
	}, nil
}

// MaxPerFileDiffBytes is the cap on a single PRFile.Diff field, in bytes.
// Above this, the file's diff is truncated with a marker. 32 KB is large
// enough to comfortably hold the typical code-review diff (a few hundred
// lines of unified diff) and small enough that 60+ such files still fit
// under MaxTotalDiffBytes.
const MaxPerFileDiffBytes = 32 * 1024

// MaxTotalDiffBytes is the cap on the SUM of Files[].Diff across the whole
// snapshot. Temporal's default activity-output payload limit is 2 MiB;
// 1.5 MiB leaves room for metadata + SpecArtifacts + ULID/timestamp
// fields without crossing the limit. Above this, subsequent files get
// an empty Diff with a marker.
const MaxTotalDiffBytes = 1536 * 1024 // 1.5 MiB

// truncatedMarker is the trailing line appended to a per-file diff that
// was truncated by the per-file cap. The marker is human-readable and
// deterministic so reviewer prompts can detect it without parsing.
const truncatedMarker = "\n[diff truncated by chitin snapshot cap; fetch full diff via `gh pr diff <PR>`]\n"

// capDiff applies the per-file and total-budget caps. Returns the
// possibly-truncated diff text and the original (uncapped) byte length.
// `usedBytes` is the running total of Diff bytes already added to the
// snapshot's Files[] (NOT including this file's contribution).
//
// Post-budget behavior: returns an empty Diff. The Files[] entry still
// retains Path + Additions + Deletions so the reviewer driver knows
// the file changed; only the diff text is omitted. The first file that
// triggered truncation carries the truncatedMarker, signaling the
// reviewer that subsequent empty Diffs are intentional (not the
// pure-rename / binary-file case).
func capDiff(raw string, usedBytes int) (capped string, originalSize int) {
	originalSize = len(raw)
	remaining := MaxTotalDiffBytes - usedBytes
	if remaining <= 0 {
		// Total budget exhausted — emit nothing further to keep the
		// snapshot bounded. The activity logs each skip so operators
		// can see the truncation footprint in worker-host logs.
		log.Printf("CapturePRSnapshot: diff budget exhausted, omitting %d bytes", originalSize)
		return "", originalSize
	}
	if originalSize <= MaxPerFileDiffBytes && originalSize <= remaining {
		return raw, originalSize
	}
	// Need to truncate. Take the smaller of the two caps.
	max := MaxPerFileDiffBytes
	if remaining < max {
		max = remaining
	}
	// Reserve room for the marker so the FINAL length still fits.
	if max <= len(truncatedMarker) {
		// Too little room left for even the marker; emit empty.
		log.Printf("CapturePRSnapshot: diff budget too small for marker, omitting %d bytes", originalSize)
		return "", originalSize
	}
	cut := max - len(truncatedMarker)
	return raw[:cut] + truncatedMarker, originalSize
}

// encodeURLPath URL-encodes a file path per-segment for use in a GitHub
// contents-API URL. Splitting on '/' before escaping preserves segment
// boundaries (url.PathEscape on the whole path would encode '/' itself
// to '%2F', which GitHub's API does not accept as a path separator).
//
// Examples:
//   .specify/specs/100-x/spec.md           → .specify/specs/100-x/spec.md
//   .specify/specs/100-x/contracts/{a}.md  → .specify/specs/100-x/contracts/%7Ba%7D.md
//   path/with space/file.md                → path/with%20space/file.md
func encodeURLPath(p string) string {
	segments := strings.Split(p, "/")
	for i, s := range segments {
		segments[i] = url.PathEscape(s)
	}
	return strings.Join(segments, "/")
}

// diffHeaderRe matches the per-file boundary line in a unified diff
// produced by `git diff` or `gh pr diff`. Both sides may quote paths
// when they contain spaces, but `gh pr diff` does not quote, matching
// vanilla git behavior on unquoted-safe paths. We accept the simple
// form `diff --git a/<path> b/<path>` and extract the "b" side as the
// canonical post-image path that `gh pr view --json files` reports.
var diffHeaderRe = regexp.MustCompile(`^diff --git a/(.+) b/(.+)$`)

// splitUnifiedDiff partitions a unified diff into per-file sections,
// keyed by the post-image path (the "b/" side of the `diff --git` header).
// The map's value is the full section including the header line and all
// trailing hunk lines up to the next file boundary or EOF.
//
// A diff with no `diff --git` headers (e.g., empty output for a PR that
// only touches binary or pure-rename files) returns an empty map; the
// caller's per-file lookup yields "" for each path, which is fine.
func splitUnifiedDiff(diff string) map[string]string {
	out := map[string]string{}
	if diff == "" {
		return out
	}
	lines := strings.Split(diff, "\n")
	var currentPath string
	var currentBuf strings.Builder
	flush := func() {
		if currentPath != "" {
			out[currentPath] = currentBuf.String()
		}
	}
	for _, line := range lines {
		if m := diffHeaderRe.FindStringSubmatch(line); m != nil {
			flush()
			currentPath = m[2] // post-image (b/) path
			currentBuf.Reset()
		}
		if currentPath == "" {
			// Pre-header preamble (rare); drop it.
			continue
		}
		currentBuf.WriteString(line)
		currentBuf.WriteByte('\n')
	}
	flush()
	return out
}
