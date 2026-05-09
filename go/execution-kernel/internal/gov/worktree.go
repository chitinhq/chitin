package gov

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

func CheckWorktreeRequirement(a Action, p Policy, cwd string) Decision {
	if len(p.Worktree.RequireFor) == 0 || !p.Worktree.RequireFor.Matches(a.Type) {
		return Decision{Allowed: true, RuleID: "worktree:not-required", Action: a}
	}
	mode := p.Worktree.Mode
	if mode == "" {
		mode = "guide"
	}
	if insideProtectedRoot(cwd, p.Worktree.ProtectedRoots) {
		return worktreeDenied(a, mode, "cwd is inside a protected primary checkout")
	}
	linked, err := isLinkedGitWorktree(cwd)
	if err != nil {
		return worktreeDenied(a, mode, fmt.Sprintf("unable to verify linked git worktree: %v", err))
	}
	if !linked {
		return worktreeDenied(a, mode, "cwd is the primary git checkout, not a linked worktree")
	}
	return Decision{Allowed: true, RuleID: "worktree:linked", Action: a}
}

func worktreeDenied(a Action, mode, reason string) Decision {
	return Decision{
		Allowed: false,
		Mode:    mode,
		RuleID:  "worktree-required",
		Reason:  reason,
		Suggestion: "Create a task branch in a linked worktree, then retry from there: " +
			"git worktree add ../<repo>-<task> -b <branch>",
		Action: a,
	}
}

func isLinkedGitWorktree(cwd string) (bool, error) {
	out, err := gitOutput(cwd, "rev-parse", "--is-inside-work-tree", "--git-dir", "--git-common-dir")
	if err != nil {
		return false, err
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 3 {
		return false, fmt.Errorf("unexpected git rev-parse output: got %d lines", len(lines))
	}
	if strings.TrimSpace(lines[0]) != "true" {
		return false, fmt.Errorf("not inside a git worktree")
	}
	absGitDir, err := absGitPath(cwd, strings.TrimSpace(lines[1]))
	if err != nil {
		return false, err
	}
	absCommonDir, err := absGitPath(cwd, strings.TrimSpace(lines[2]))
	if err != nil {
		return false, err
	}
	return absGitDir != absCommonDir, nil
}

func gitOutput(cwd string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", cwd}, args...)...)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return string(out), nil
}

func absGitPath(cwd, p string) (string, error) {
	if p == "" {
		return "", fmt.Errorf("empty git path")
	}
	if !filepath.IsAbs(p) {
		p = filepath.Join(cwd, p)
	}
	return filepath.Abs(p)
}

func insideProtectedRoot(cwd string, roots []string) bool {
	if len(roots) == 0 {
		return false
	}
	absCwd, err := filepath.Abs(cwd)
	if err != nil {
		return false
	}
	for _, root := range roots {
		absRoot, err := filepath.Abs(root)
		if err != nil {
			continue
		}
		rel, err := filepath.Rel(absRoot, absCwd)
		if err != nil {
			continue
		}
		if rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))) {
			return true
		}
	}
	return false
}
