package gov

import (
	"regexp"
	"strings"
)

// Package-level regexes for infra.destroy detection and shape annotation.
// Compiled once at init time; not recompiled per call.
var (
	reTerraformDestroy = regexp.MustCompile(`^terraform\s+destroy\b`)
	reKubectlDeleteNS  = regexp.MustCompile(`^kubectl\s+delete\s+(ns|namespace)\b`)
	reCurlPipeBash     = regexp.MustCompile(`\bcurl\b[^|]*\|\s*(bash|sh)\b`)
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

	// Infra-destroy patterns: re-tag from shell.exec to infra.destroy so
	// policy rules can match the intent-class, not the shell string.
	// Invariant: every command matching these patterns produces exactly one
	// ActInfraDestroy action with Params["tool"] naming the CLI tool.
	if reTerraformDestroy.MatchString(trimmed) {
		return Action{Type: ActInfraDestroy, Target: trimmed, Params: map[string]any{"tool": "terraform"}}
	}
	if reKubectlDeleteNS.MatchString(trimmed) {
		return Action{Type: ActInfraDestroy, Target: trimmed, Params: map[string]any{"tool": "kubectl"}}
	}

	// Default: generic shell.exec — all other commands (including rm -rf).
	// Annotate curl-pipe-bash shape: action stays shell.exec, but policy can
	// target Params["shape"] = "curl-pipe-bash" to match this dangerous pattern.
	// Invariant: every curl ... | bash/sh command produces exactly one
	// ActShellExec action with Params["shape"] = "curl-pipe-bash".
	// wget pipe bash and curl without pipe intentionally do not match.
	action := Action{Type: ActShellExec, Target: trimmed}
	if reCurlPipeBash.MatchString(trimmed) {
		action.Params = map[string]any{"shape": "curl-pipe-bash"}
	}
	return action
}

// extractShellIntent scans Python code for anything that would execute
// a shell command or delete files, and returns the reconstructed shell
// command. Returns "" if no dangerous pattern is found.
//
// This is the core of the execute_code bypass closure: whatever an agent
// writes in Python that shells out gets mapped back to its shell
// equivalent for policy evaluation.
//
// Patterns matched (ordered by specificity):
//  1. subprocess.run/call/Popen/check_* with a LIST argument:
//        subprocess.run(["rm", "-rf", "go/"])
//  2. subprocess.run/call/Popen/check_* with a STRING argument (typically
//     shell=True, but matched regardless — string form is shell semantics):
//        subprocess.run("rm -rf go/", shell=True)
//  3. os.system("rm -rf go/") — always shell
//  4. shutil.rmtree("go/") → rm -rf go/
//  5. os.remove/unlink("x") → rm x
//  6. Last-resort pattern match: if the code contains a bare "rm -rf"
//     or "rm -r" substring anywhere (e.g. in a non-obvious helper, f-string,
//     or pathlib.Path.unlink call we didn't catch), treat the whole code
//     as a shell exec of that fragment so the policy engine's target_regex
//     can still match it.
func extractShellIntent(code string) string {
	// 1. List-form subprocess
	subListRE := regexp.MustCompile(`subprocess\.(?:run|call|Popen|check_call|check_output)\s*\(\s*\[([^\]]+)\]`)
	if m := subListRE.FindStringSubmatch(code); len(m) > 1 {
		return joinQuotedList(m[1])
	}
	// 2. String-form subprocess (shell=True or default on some platforms)
	subStrRE := regexp.MustCompile(`subprocess\.(?:run|call|Popen|check_call|check_output)\s*\(\s*['"]([^'"]+)['"]`)
	if m := subStrRE.FindStringSubmatch(code); len(m) > 1 {
		return m[1]
	}
	// 3. os.system
	osSysRE := regexp.MustCompile(`os\.system\s*\(\s*['"]([^'"]+)['"]`)
	if m := osSysRE.FindStringSubmatch(code); len(m) > 1 {
		return m[1]
	}
	// 4. shutil.rmtree
	rmtreeRE := regexp.MustCompile(`shutil\.rmtree\s*\(\s*['"]([^'"]+)['"]`)
	if m := rmtreeRE.FindStringSubmatch(code); len(m) > 1 {
		return "rm -rf " + m[1]
	}
	// 5. os.remove / os.unlink
	rmRE := regexp.MustCompile(`os\.(?:remove|unlink)\s*\(\s*['"]([^'"]+)['"]`)
	if m := rmRE.FindStringSubmatch(code); len(m) > 1 {
		return "rm " + m[1]
	}
	// 6. Last-resort: bare "rm -rf" substring. Catches f-strings,
	// pathlib.Path(...).unlink(missing_ok=True), and other forms we
	// don't explicitly model. Target becomes the full code fragment so
	// policy regexes can still match it (the raw "rm -rf" literal
	// triggers the no-destructive-rm target: "rm -rf" substring match).
	rawRmRE := regexp.MustCompile(`\brm\s+-r[f]?\b`)
	if rawRmRE.MatchString(code) {
		return code
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
