package gov

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const worktreeDiagnosticRuleID = "worktree-required-diagnostic"

func stampWorktreeDiagnostic(d *Decision, a Action, cwd string) {
	cwd = firstNonEmpty(cwd, a.Path)
	if !isSideEffectAction(a) || cwd == "" {
		return
	}
	status := detectGitWorktreeStatus(cwd)
	if status != "primary" {
		return
	}
	d.WorktreeDiagnosticRuleID = worktreeDiagnosticRuleID
	d.WorktreeStatus = status
	d.WorktreeReason = "side-effect action evaluated from primary git checkout; use a linked worktree for autonomous agent changes"
}

func isSideEffectAction(a Action) bool {
	return !isReadOnlyAction(a)
}

func isReadOnlyAction(a Action) bool {
	switch a.Type {
	case ActFileRead,
		ActGitDiff,
		ActGitLog,
		ActGitStatus,
		ActGitWorktreeList,
		ActGithubPRView,
		ActGithubPRList,
		ActGithubIssueView,
		ActGithubIssueList,
		ActHTTPRequest:
		return true
	case ActShellExec:
		subAction, _ := a.Params["sub_action"].(string)
		return subAction == "shell.cat"
	default:
		return false
	}
}

func detectGitWorktreeStatus(cwd string) string {
	list, err := gitOutput(cwd, "worktree", "list", "--porcelain")
	if err != nil || list == "" {
		return ""
	}
	worktrees := parseWorktreePorcelain(list)
	if len(worktrees) == 0 {
		return ""
	}
	if pathUnderOrEqual(cwd, worktrees[0]) {
		return "primary"
	}
	for _, wt := range worktrees[1:] {
		if pathUnderOrEqual(cwd, wt) {
			return "linked"
		}
	}
	return ""
}

func pathUnderOrEqual(path, root string) bool {
	pathAbs, err := filepath.Abs(path)
	if err != nil {
		pathAbs = path
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		rootAbs = root
	}
	pathClean := filepath.Clean(pathAbs)
	rootClean := filepath.Clean(rootAbs)
	if pathClean == rootClean {
		return true
	}
	rel, err := filepath.Rel(rootClean, pathClean)
	if err != nil {
		return false
	}
	return rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func gitOutput(cwd string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", cwd}, args...)...)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func parseWorktreePorcelain(out string) []string {
	var worktrees []string
	for _, line := range strings.Split(out, "\n") {
		if path, ok := strings.CutPrefix(line, "worktree "); ok {
			worktrees = append(worktrees, path)
		}
	}
	return worktrees
}
