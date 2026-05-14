package gov

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"github.com/chitinhq/chitin/go/execution-kernel/internal/canon"
)

// CheckBounds fires only for push-shaped actions (git.push, github.pr.create).
// For git.push it shells out to `git diff --stat origin/main...HEAD` in cwd.
// For github.pr.create it resolves the repo root and measures
// `git diff --stat <base>...<head>` from the gh command line so the bounds
// check matches the PR page's merge-base diff regardless of the caller's cwd.
// Rejects if the per-action ceiling in policy.Bounds is exceeded. Fail-closed:
// if git diff fails or returns unparseable output, treat as over-bounds.
//
// Per-action overrides (Bounds.PerAction[<action_type>]) close #70: doc-batch
// pushes via git.push can be allowed a higher ceiling than code commits via
// github.pr.create without widening either globally.
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

	files, ins, del, err := collectDiffStats(a, cwd)
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

func collectDiffStats(a Action, cwd string) (files, ins, del int, err error) {
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
	cmd := exec.Command("git", "-C", repoPath, "diff", "--stat", diffRange)
	out, runErr := cmd.Output()
	if runErr != nil {
		return 0, 0, 0, fmt.Errorf("git diff --stat %s: %w", diffRange, runErr)
	}
	trimmed := strings.TrimSpace(string(out))
	if trimmed == "" {
		// Empty diff (no changes vs. base). All zeros pass bounds.
		return 0, 0, 0, nil
	}
	lines := strings.Split(trimmed, "\n")
	last := lines[len(lines)-1]
	f, ip, dp := parseDiffStatLine(last)
	// Non-empty output but the summary line didn't match the expected
	// shape — fail-closed rather than let bounds silently pass on
	// unparseable input.
	if f == 0 && ip == 0 && dp == 0 && !strings.Contains(last, "files changed") && !strings.Contains(last, "file changed") {
		return 0, 0, 0, fmt.Errorf("unparseable git diff --stat summary: %q", last)
	}
	return f, ip, dp, nil
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
		base = strings.TrimPrefix(defaultBase, "origin/")
	}
	return base + "..." + head, nil
}

func parsePRCreateBaseHead(raw string) (base, head string) {
	cmd := canon.ParseOne(raw)
	if cmd.Tool != "gh" || cmd.Action != "pr" || len(cmd.Args) == 0 || cmd.Args[0] != "create" {
		return "", ""
	}
	return cmd.Flags["base"], cmd.Flags["head"]
}

func boundsGitOutput(repoPath string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", repoPath}, args...)...)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// parseDiffStatLine parses the summary line printed at the bottom of
// `git diff --stat`, e.g.:
//
//	" 3 files changed, 10 insertions(+), 5 deletions(-)"
//
// Returns (files, insertions, deletions). Any field missing returns 0
// for that field (e.g. a diff with only insertions lacks deletions).
func parseDiffStatLine(s string) (files, ins, del int) {
	reFiles := regexp.MustCompile(`(\d+)\s+files?\s+changed`)
	reIns := regexp.MustCompile(`(\d+)\s+insertions?\(\+\)`)
	reDel := regexp.MustCompile(`(\d+)\s+deletions?\(-\)`)
	if m := reFiles.FindStringSubmatch(s); len(m) > 1 {
		files, _ = strconv.Atoi(m[1])
	}
	if m := reIns.FindStringSubmatch(s); len(m) > 1 {
		ins, _ = strconv.Atoi(m[1])
	}
	if m := reDel.FindStringSubmatch(s); len(m) > 1 {
		del, _ = strconv.Atoi(m[1])
	}
	return files, ins, del
}
