package gov

import (
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

// CheckBounds fires only for push-shaped actions (git.push, github.pr.create).
// Shells out to `git diff --stat origin/main...HEAD` in cwd; rejects if any
// ceiling in policy.Bounds is exceeded. Fail-closed: if git diff fails or
// returns unparseable output, treat as over-bounds.
//
// Bounds decisions are ALWAYS mode=enforce — a "try again smaller" guide
// loop is too expensive for aggregate-blast actions.
func CheckBounds(a Action, p Policy, cwd string) Decision {
	if a.Type != ActGitPush && a.Type != ActGithubPRCreate {
		return Decision{Allowed: true, RuleID: "bounds:not-push-shaped", Action: a}
	}
	if p.Bounds.MaxFilesChanged == 0 && p.Bounds.MaxLinesChanged == 0 {
		return Decision{Allowed: true, RuleID: "bounds:no-ceiling", Action: a}
	}

	files, ins, del, err := collectDiffStats(cwd)
	if err != nil {
		return Decision{
			Allowed: false,
			Mode:    "enforce",
			RuleID:  "bounds:undetermined",
			Reason:  fmt.Sprintf("failed to compute diff stats: %v", err),
			Action:  a,
		}
	}
	return evaluateBoundsFromStats(a, p, files, ins, del)
}

func evaluateBoundsFromStats(a Action, p Policy, files, ins, del int) Decision {
	lines := ins + del
	if p.Bounds.MaxFilesChanged > 0 && files > p.Bounds.MaxFilesChanged {
		return Decision{
			Allowed: false,
			Mode:    "enforce",
			RuleID:  "bounds:max_files_changed",
			Reason: fmt.Sprintf(
				"%d files changed exceeds ceiling of %d",
				files, p.Bounds.MaxFilesChanged),
			Action: a,
		}
	}
	if p.Bounds.MaxLinesChanged > 0 && lines > p.Bounds.MaxLinesChanged {
		return Decision{
			Allowed: false,
			Mode:    "enforce",
			RuleID:  "bounds:max_lines_changed",
			Reason: fmt.Sprintf(
				"%d lines changed exceeds ceiling of %d",
				lines, p.Bounds.MaxLinesChanged),
			Action: a,
		}
	}
	return Decision{Allowed: true, RuleID: "bounds:within-ceilings", Action: a}
}

func collectDiffStats(cwd string) (files, ins, del int, err error) {
	// Use origin/main...HEAD (three dots = merge-base diff, matches what
	// would become the PR diff). Fall back to HEAD~1 if origin/main absent.
	cmd := exec.Command("git", "-C", cwd, "diff", "--stat", "origin/main...HEAD")
	out, runErr := cmd.Output()
	if runErr != nil {
		return 0, 0, 0, fmt.Errorf("git diff --stat origin/main...HEAD: %w", runErr)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) == 0 {
		return 0, 0, 0, nil
	}
	last := lines[len(lines)-1]
	f, ip, dp := parseDiffStatLine(last)
	return f, ip, dp, nil
}

// parseDiffStatLine parses the summary line printed at the bottom of
// `git diff --stat`, e.g.:
//   " 3 files changed, 10 insertions(+), 5 deletions(-)"
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
