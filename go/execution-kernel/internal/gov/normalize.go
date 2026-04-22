package gov

import (
	"regexp"
	"strings"
)

// Normalize maps a raw tool call to a canonical Action. Closed enum:
// unknown tools produce ActUnknown (fail-closed at the policy layer).
//
// The critical invariant: a destructive operation expressed as
// terminal "rm -rf X", execute_code "subprocess.run([rm,-rf,X])", or
// execute_code "shutil.rmtree(X)" must all produce the same Action.Type.
// This is the bypass closure — one policy rule catches all routes.
func Normalize(toolName string, args map[string]any) (Action, error) {
	switch toolName {
	case "terminal", "bash", "shell":
		return normalizeShell(args), nil
	case "execute_code":
		return normalizeExecuteCode(args), nil
	case "write_file", "patch":
		return normalizeWriteFile(args), nil
	case "read_file":
		return Action{Type: ActFileRead, Target: stringArg(args, "path")}, nil
	case "delegate_task":
		return Action{Type: ActDelegateTask, Target: stringArg(args, "goal")}, nil
	case "search_files":
		return Action{Type: ActFileRead, Target: stringArg(args, "query")}, nil
	case "skill_view":
		return Action{Type: ActFileRead, Target: stringArg(args, "skill")}, nil
	case "todo":
		return Action{Type: ActFileWrite, Target: "todo"}, nil
	}
	return Action{Type: ActUnknown, Target: toolName, Params: args}, nil
}

func normalizeShell(args map[string]any) Action {
	cmd := stringArg(args, "command")
	if cmd == "" {
		cmd = stringArg(args, "cmd")
	}
	return classifyShellCommand(cmd)
}

func normalizeExecuteCode(args map[string]any) Action {
	code := stringArg(args, "code")
	// Inspect code for shell-out patterns. If any match, treat as shell.exec
	// with the intent extracted — closes the execute_code bypass class.
	if shellOutIntent := extractShellIntent(code); shellOutIntent != "" {
		return classifyShellCommand(shellOutIntent)
	}
	// Pure Python execute_code (no shell-out) is treated as file write
	// because it can still modify files via open(..., "w") etc.
	return Action{Type: ActFileWrite, Target: "execute_code"}
}

func normalizeWriteFile(args map[string]any) Action {
	path := stringArg(args, "path")
	if path == "" {
		path = stringArg(args, "file_path")
	}
	return Action{Type: ActFileWrite, Target: path}
}

// classifyShellCommand inspects a shell command string and returns
// the most specific canonical Action. Ordering matters: check for
// destructive / dangerous patterns before generic categories.
func classifyShellCommand(cmd string) Action {
	trimmed := strings.TrimSpace(cmd)

	// git force-push before git push (force-push is a git.push superset)
	if matched, _ := regexp.MatchString(`\bgit\s+push\b.*--force(-with-lease)?\b`, trimmed); matched {
		return Action{Type: ActGitForcePush, Target: trimmed}
	}
	if matched, _ := regexp.MatchString(`\bgit\s+push\s+-f\b`, trimmed); matched {
		return Action{Type: ActGitForcePush, Target: trimmed}
	}

	// git push — capture branch if present
	if matched, _ := regexp.MatchString(`\bgit\s+push\b`, trimmed); matched {
		branch := extractPushBranch(trimmed)
		return Action{Type: ActGitPush, Target: branch}
	}

	// Specific git read commands
	if matched, _ := regexp.MatchString(`\bgit\s+status\b`, trimmed); matched {
		return Action{Type: ActGitStatus, Target: trimmed}
	}
	if matched, _ := regexp.MatchString(`\bgit\s+log\b`, trimmed); matched {
		return Action{Type: ActGitLog, Target: trimmed}
	}
	if matched, _ := regexp.MatchString(`\bgit\s+diff\b`, trimmed); matched {
		return Action{Type: ActGitDiff, Target: trimmed}
	}
	if matched, _ := regexp.MatchString(`\bgit\s+commit\b`, trimmed); matched {
		return Action{Type: ActGitCommit, Target: trimmed}
	}
	if matched, _ := regexp.MatchString(`\bgit\s+checkout\b`, trimmed); matched {
		return Action{Type: ActGitCheckout, Target: trimmed}
	}
	if matched, _ := regexp.MatchString(`\bgit\s+worktree\s+list\b`, trimmed); matched {
		return Action{Type: ActGitWorktreeList, Target: trimmed}
	}
	if matched, _ := regexp.MatchString(`\bgit\s+worktree\s+add\b`, trimmed); matched {
		return Action{Type: ActGitWorktreeAdd, Target: trimmed}
	}
	if matched, _ := regexp.MatchString(`\bgit\s+worktree\s+remove\b`, trimmed); matched {
		return Action{Type: ActGitWorktreeRemove, Target: trimmed}
	}

	// gh CLI — PR / issue operations
	if matched, _ := regexp.MatchString(`\bgh\s+pr\s+create\b`, trimmed); matched {
		return Action{Type: ActGithubPRCreate, Target: trimmed}
	}
	if matched, _ := regexp.MatchString(`\bgh\s+pr\s+view\b`, trimmed); matched {
		return Action{Type: ActGithubPRView, Target: trimmed}
	}
	if matched, _ := regexp.MatchString(`\bgh\s+pr\s+list\b`, trimmed); matched {
		return Action{Type: ActGithubPRList, Target: trimmed}
	}
	if matched, _ := regexp.MatchString(`\bgh\s+pr\s+merge\b`, trimmed); matched {
		return Action{Type: ActGithubPRMerge, Target: trimmed}
	}
	if matched, _ := regexp.MatchString(`\bgh\s+pr\s+close\b`, trimmed); matched {
		return Action{Type: ActGithubPRClose, Target: trimmed}
	}
	if matched, _ := regexp.MatchString(`\bgh\s+issue\s+view\b`, trimmed); matched {
		return Action{Type: ActGithubIssueView, Target: trimmed}
	}
	if matched, _ := regexp.MatchString(`\bgh\s+issue\s+list\b`, trimmed); matched {
		return Action{Type: ActGithubIssueList, Target: trimmed}
	}
	if matched, _ := regexp.MatchString(`\bgh\s+issue\s+create\b`, trimmed); matched {
		return Action{Type: ActGithubIssueCreate, Target: trimmed}
	}
	if matched, _ := regexp.MatchString(`\bgh\s+issue\s+close\b`, trimmed); matched {
		return Action{Type: ActGithubIssueClose, Target: trimmed}
	}
	if matched, _ := regexp.MatchString(`\bgh\s+api\b`, trimmed); matched {
		return Action{Type: ActGithubAPI, Target: trimmed}
	}

	// Default: generic shell.exec — all other commands (including rm -rf)
	return Action{Type: ActShellExec, Target: trimmed}
}

// extractShellIntent scans Python code for subprocess.run/call/Popen with
// a command list, or shutil.rmtree. Returns the reconstructed shell
// command if found, or "" otherwise.
//
// This is the core of the execute_code bypass closure: whatever an agent
// writes in Python that shells out gets mapped back to its shell equivalent.
func extractShellIntent(code string) string {
	// subprocess.run(["rm", "-rf", "x"]) or subprocess.run(['rm','-rf','x'])
	subRE := regexp.MustCompile(`subprocess\.(?:run|call|Popen|check_call|check_output)\s*\(\s*\[([^\]]+)\]`)
	if m := subRE.FindStringSubmatch(code); len(m) > 1 {
		return joinQuotedList(m[1])
	}
	// shutil.rmtree("x") — map to rm -rf <x>
	rmtreeRE := regexp.MustCompile(`shutil\.rmtree\s*\(\s*['"]([^'"]+)['"]`)
	if m := rmtreeRE.FindStringSubmatch(code); len(m) > 1 {
		return "rm -rf " + m[1]
	}
	// os.remove("x") / os.unlink("x") — map to rm <x>
	rmRE := regexp.MustCompile(`os\.(?:remove|unlink)\s*\(\s*['"]([^'"]+)['"]`)
	if m := rmRE.FindStringSubmatch(code); len(m) > 1 {
		return "rm " + m[1]
	}
	return ""
}

// joinQuotedList takes the inside of a Python list literal like:
//   "rm", "-rf", "go/"
// and returns the space-joined unquoted string: `rm -rf go/`
func joinQuotedList(inside string) string {
	parts := []string{}
	re := regexp.MustCompile(`['"]([^'"]*)['"]`)
	for _, m := range re.FindAllStringSubmatch(inside, -1) {
		parts = append(parts, m[1])
	}
	return strings.Join(parts, " ")
}

// extractPushBranch parses `git push [remote] [branch|HEAD:branch]`
// and returns the destination branch name, or "" if it can't be parsed.
func extractPushBranch(cmd string) string {
	// Match "git push origin branch" or "git push origin HEAD:branch"
	re := regexp.MustCompile(`\bgit\s+push\s+\S+\s+(?:HEAD:)?([A-Za-z0-9_./\-]+)`)
	if m := re.FindStringSubmatch(cmd); len(m) > 1 {
		return m[1]
	}
	return ""
}

func stringArg(args map[string]any, key string) string {
	if v, ok := args[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}
