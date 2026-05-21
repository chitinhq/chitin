package gov

import (
	"fmt"
	"os"
	"os/exec"
	"path"
	"strconv"
	"strings"

	"github.com/chitinhq/chitin/go/execution-kernel/internal/canon"
)

// CheckBounds fires only for push-shaped actions (git.push, github.pr.create).
// For git.push it shells out to `git diff --numstat origin/main...HEAD` in cwd.
// For github.pr.create it resolves the repo root and measures
// `git diff --numstat <base>...<head>` from the gh command line so the bounds
// check matches the PR page's merge-base diff regardless of the caller's cwd.
// Rejects if the per-action ceiling in policy.Bounds is exceeded. Fail-closed:
// if git diff fails or returns unparseable output, treat as over-bounds.
//
// Per-action overrides (Bounds.PerAction[<action_type>]) close #70: doc-batch
// pushes via git.push can be allowed a higher ceiling than code commits via
// github.pr.create without widening either globally.
//
// Bounds.ExcludePaths drops generated artifacts (lockfiles, vendored trees)
// from the files/lines totals before they are measured against the ceilings.
// An entirely-excluded diff measures as 0 files / 0 lines and passes.
//
// Bounds decisions default to mode=enforce — bounds are intended as hard
// kill-switches even when policy is in monitor — but the operator can opt
// out per-rule via invariantModes (e.g. invariantModes."bounds:max_files
// _changed": monitor) for a documented soft-kill workflow. The InvariantModes
// override path matches every other rule's mode resolution; it's just
// applied to bounds rules too instead of being hardcoded.
func CheckBounds(a Action, p Policy, cwd string) Decision {
	if a.Type != ActGitPush && a.Type != ActGithubPRCreate {
		return Decision{Allowed: true, RuleID: "bounds:not-push-shaped", Action: a}
	}
	eff := p.Bounds.effectiveBounds(string(a.Type))
	if eff.MaxFilesChanged == 0 && eff.MaxLinesChanged == 0 {
		return Decision{Allowed: true, RuleID: "bounds:no-ceiling", Action: a}
	}

	files, ins, del, err := collectDiffStats(a, cwd, p.Bounds.ExcludePaths)
	if err != nil {
		return Decision{
			Allowed: false,
			Mode:    boundsModeFor(p, "bounds:undetermined"),
			RuleID:  "bounds:undetermined",
			Reason:  fmt.Sprintf("failed to compute diff stats: %v", err),
			Action:  a,
		}
	}
	return evaluateBoundsFromStats(a, p, eff, files, ins, del)
}

func evaluateBoundsFromStats(a Action, p Policy, eff ActionBounds, files, ins, del int) Decision {
	lines := ins + del
	if eff.MaxFilesChanged > 0 && files > eff.MaxFilesChanged {
		return Decision{
			Allowed: false,
			Mode:    boundsModeFor(p, "bounds:max_files_changed"),
			RuleID:  "bounds:max_files_changed",
			Reason: fmt.Sprintf(
				"%d files changed exceeds ceiling of %d",
				files, eff.MaxFilesChanged),
			Action: a,
		}
	}
	if eff.MaxLinesChanged > 0 && lines > eff.MaxLinesChanged {
		return Decision{
			Allowed: false,
			Mode:    boundsModeFor(p, "bounds:max_lines_changed"),
			RuleID:  "bounds:max_lines_changed",
			Reason: fmt.Sprintf(
				"%d lines changed exceeds ceiling of %d",
				lines, eff.MaxLinesChanged),
			Action: a,
		}
	}
	return Decision{Allowed: true, RuleID: "bounds:within-ceilings", Action: a}
}

// boundsModeFor resolves the effective Mode for a bounds rule. Defaults
// to enforce (bounds are kill-switches), but honors invariantModes for
// per-rule overrides — closes #70 for the "I want doc-batch pushes
// to be soft-killed not hard-blocked" use case. Operator opts out via:
//
//	invariantModes:
//	  "bounds:max_files_changed": monitor
//	  "bounds:max_lines_changed": guide
func boundsModeFor(p Policy, ruleID string) string {
	if m, ok := p.InvariantModes[ruleID]; ok {
		return m
	}
	return "enforce"
}

func collectDiffStats(a Action, cwd string, excludePaths []string) (files, ins, del int, err error) {
	diffRange := "origin/main...HEAD"
	repoPath := cwd

	if a.Type == ActGithubPRCreate {
		repoRoot, rootErr := boundsGitOutput(cwd, "rev-parse", "--show-toplevel")
		if rootErr != nil {
			return 0, 0, 0, fmt.Errorf("git rev-parse --show-toplevel: %w", rootErr)
		}
		repoPath = repoRoot
		diffRange, err = prDiffRange(a, repoRoot)
		if err != nil {
			return 0, 0, 0, err
		}
	}

	// origin/main...HEAD (three dots = merge-base diff, matches what the PR
	// diff would be). If origin/main isn't available (detached HEAD, no
	// remote), fail-closed upstream treats bounds:undetermined as a deny.
	// No silent HEAD~1 fallback: a single-commit diff doesn't tell us the
	// PR-level blast radius, and getting bounds wrong in the permissive
	// direction defeats the point.
	//
	// --numstat gives one machine-readable line per file:
	//   <added>\t<deleted>\t<path>
	// Binary files render as `-\t-\t<path>`; they count as a changed file
	// with zero lines. This is parsed per file (not via a summary line) so
	// ExcludePaths can drop generated artifacts before totals are summed.
	cmd := exec.Command("git", "-C", repoPath, "diff", "--numstat", diffRange)
	out, runErr := cmd.Output()
	if runErr != nil {
		return 0, 0, 0, fmt.Errorf("git diff --numstat %s: %w", diffRange, runErr)
	}
	return parseNumstat(string(out), excludePaths)
}

// parseNumstat sums files/insertions/deletions over `git diff --numstat`
// output, skipping any file whose path matches an ExcludePaths glob.
// Empty output means no changes vs. base — all zeros, which passes
// bounds. A line that does not have three tab-separated fields is
// treated fail-closed: an error, not a silent skip.
func parseNumstat(out string, excludePaths []string) (files, ins, del int, err error) {
	trimmed := strings.TrimSpace(out)
	if trimmed == "" {
		return 0, 0, 0, nil
	}
	for _, line := range strings.Split(trimmed, "\n") {
		if line == "" {
			continue
		}
		fields := strings.SplitN(line, "\t", 3)
		if len(fields) != 3 {
			return 0, 0, 0, fmt.Errorf("unparseable git diff --numstat line: %q", line)
		}
		addStr, delStr, pathField := fields[0], fields[1], fields[2]
		if pathMatchesAny(pathField, excludePaths) {
			continue
		}
		// Binary files render added/deleted as "-"; count the file but
		// contribute zero lines (there is no textual line count).
		a := 0
		if addStr != "-" {
			a, err = strconv.Atoi(addStr)
			if err != nil {
				return 0, 0, 0, fmt.Errorf("unparseable added count %q in %q", addStr, line)
			}
		}
		d := 0
		if delStr != "-" {
			d, err = strconv.Atoi(delStr)
			if err != nil {
				return 0, 0, 0, fmt.Errorf("unparseable deleted count %q in %q", delStr, line)
			}
		}
		files++
		ins += a
		del += d
	}
	return files, ins, del, nil
}

// pathMatchesAny reports whether p matches any of the glob patterns.
// The path field from `git diff --numstat` is matched as-is; rename
// entries (which contain `=>`) will not match a clean glob and so are
// counted — the conservative direction for a ceiling check.
func pathMatchesAny(p string, patterns []string) bool {
	for _, pattern := range patterns {
		if matchGlob(pattern, p) {
			return true
		}
	}
	return false
}

// matchGlob matches a slash-separated path against a glob pattern.
// Each non-`**` segment is matched with path.Match (so `*` and `?`
// work within a segment but do not cross `/`). A `**` segment matches
// zero or more whole path segments, so `**/pnpm-lock.yaml` matches both
// `pnpm-lock.yaml` and `apps/x/pnpm-lock.yaml`, and `vendor/**` matches
// everything under `vendor/`.
func matchGlob(pattern, p string) bool {
	if pattern == "" {
		return false
	}
	return matchSegments(strings.Split(pattern, "/"), strings.Split(p, "/"))
}

func matchSegments(pat, segs []string) bool {
	if len(pat) == 0 {
		return len(segs) == 0
	}
	if pat[0] == "**" {
		// `**` matches zero or more whole segments, EXCEPT a trailing
		// `**` (e.g. `vendor/**`) which requires at least one segment
		// after the separator — so `vendor/**` matches `vendor/x` but
		// not `vendor` itself. A leading/middle `**` (e.g.
		// `**/pnpm-lock.yaml`) still matches zero segments, so the
		// pattern also matches `pnpm-lock.yaml` at the repo root.
		start := 0
		if len(pat) == 1 {
			start = 1
		}
		for i := start; i <= len(segs); i++ {
			if matchSegments(pat[1:], segs[i:]) {
				return true
			}
		}
		return false
	}
	if len(segs) == 0 {
		return false
	}
	ok, matchErr := path.Match(pat[0], segs[0])
	if matchErr != nil || !ok {
		return false
	}
	return matchSegments(pat[1:], segs[1:])
}

func prDiffRange(a Action, repoRoot string) (string, error) {
	base, head := parsePRCreateBaseHead(a.Target)
	if head == "" {
		fmt.Fprintln(os.Stderr, "bounds: using cwd HEAD; pass --head/--base for accurate PR-size measurement")
		return "origin/main...HEAD", nil
	}
	if base == "" {
		defaultBase, err := boundsGitOutput(repoRoot, "symbolic-ref", "--short", "refs/remotes/origin/HEAD")
		if err != nil {
			return "", fmt.Errorf("git symbolic-ref --short refs/remotes/origin/HEAD: %w", err)
		}
		base = defaultBase
	}
	return remoteBaseRef(base) + "..." + head, nil
}

func parsePRCreateBaseHead(raw string) (base, head string) {
	cmd := canon.ParseOne(raw)
	if cmd.Tool != "gh" || cmd.Action != "pr" || len(cmd.Args) == 0 || cmd.Args[0] != "create" {
		return "", ""
	}
	return cmd.Flags["base"], cmd.Flags["head"]
}

func remoteBaseRef(base string) string {
	base = strings.TrimSpace(base)
	base = strings.TrimPrefix(base, "refs/remotes/")
	base = strings.TrimPrefix(base, "refs/heads/")
	if base == "" || strings.HasPrefix(base, "origin/") {
		return base
	}
	return "origin/" + base
}

func boundsGitOutput(repoPath string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", repoPath}, args...)...)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
