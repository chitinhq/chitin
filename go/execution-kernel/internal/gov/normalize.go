package gov

import (
	"regexp"
	"strings"

	"github.com/chitinhq/chitin/go/execution-kernel/internal/canon"
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
		return normalizeShell(args)
	case "exec", "process":
		return normalizeShell(args)
	case "execute_code":
		return normalizeExecuteCode(args), nil
	case "write_file", "patch":
		return normalizeWriteFile(args)
	// openclaw pi-runtime file tools. `write` creates/overwrites; `edit`
	// modifies in-place. Both end at file.write because both are mutations
	// from the policy's perspective — the existing `write_file` mapping
	// already encodes that policy.
	// "Write" is the Claude Code + Hermes tool name (capitalized); "write"
	// is the lower-level alias. Both normalize identically.
	case "write", "edit", "Write":
		return normalizeWriteFile(args)
	case "read_file":
		path := stringArg(args, "path")
		if path == "" {
			path = stringArg(args, "file_path")
		}
		if path == "" {
			return Action{}, ErrMissingTarget("read_file")
		}
		return Action{Type: ActFileRead, Target: path}, nil
	case "read":
		path := stringArg(args, "path")
		if path == "" {
			path = stringArg(args, "file_path")
		}
		if path == "" {
			return Action{}, ErrMissingTarget("read")
		}
		return Action{Type: ActFileRead, Target: path}, nil
	// openclaw chat-domain tools — slice 3 normalizer coverage.
	// Memory tools: read-only access to MEMORY.md / wiki content (memory-core
	// extension). The query/path is the operative target.
	case "memory_search":
		return Action{Type: ActFileRead, Target: stringArg(args, "query")}, nil
	case "memory_get":
		target := stringArg(args, "path")
		if target == "" {
			target = stringArg(args, "file")
		}
		return Action{Type: ActFileRead, Target: target}, nil
	// Session-read tools: list, transcript, status, end-turn. All side-
	// effect-free from the policy's perspective.
	case "sessions_list", "sessions_history", "sessions_yield", "session_status":
		return Action{Type: ActFileRead, Target: toolName}, nil
	// Session-mutate tools: spawn subagents, send to other sessions, manage
	// subagents, schedule cron jobs. Cross-agent communication and scheduling
	// → delegate.task. Bypass closure with `delegate_task`: any rule that
	// catches one catches all forms.
	//
	// sessions_send / sessions_spawn: side-effectful cross-session calls.
	// They take a target session id (sessions_send) or an agent id +
	// initial message (sessions_spawn). Use the most specific identifier
	// available so policy rules can target a specific peer.
	case "sessions_send", "sessions_spawn":
		target := stringArg(args, "agentId")
		if target == "" {
			target = stringArg(args, "sessionId")
		}
		if target == "" {
			target = stringArg(args, "target")
		}
		if target == "" {
			target = toolName
		}
		return Action{Type: ActDelegateTask, Target: target}, nil
	// cron: granular target is "<action>:<name>" so policy rules can
	// distinguish create vs delete vs trigger on a specific job. When the
	// payload omits action/name (older callers, defensive fallback), drop
	// to the prior target-then-name lookup so existing rules keep firing.
	case "cron":
		action := stringArg(args, "action")
		name := stringArg(args, "name")
		if action != "" && name != "" {
			return Action{Type: ActDelegateTask, Target: action + ":" + name}, nil
		}
		target := stringArg(args, "target")
		if target == "" {
			target = stringArg(args, "name")
		}
		if target == "" {
			target = toolName
		}
		return Action{Type: ActDelegateTask, Target: target}, nil
	// subagents: granular target is "<action>:<agentId>" so policy rules
	// can distinguish spawn vs kill vs message on a specific agent.
	// Falls back to target/agentId/toolName when either field is absent.
	case "subagents":
		action := stringArg(args, "action")
		agentId := stringArg(args, "agentId")
		if action != "" && agentId != "" {
			return Action{Type: ActDelegateTask, Target: action + ":" + agentId}, nil
		}
		target := stringArg(args, "target")
		if target == "" {
			target = stringArg(args, "agentId")
		}
		if target == "" {
			target = toolName
		}
		return Action{Type: ActDelegateTask, Target: target}, nil
	// External-call tools: image analysis/generation, web search/fetch.
	// All make network requests under the hood → http.request. Both the
	// plain forms (web_search, web_fetch) and the provider-prefixed forms
	// (ollama_web_*) get registered by openclaw; map them identically so
	// the policy doesn't depend on which provider is wired.
	//
	// image / image_generate: granular target = the path being analyzed
	// (image) or the prompt being rendered (image_generate). Lets rules
	// like "no image_generate with prompts containing 'X'" fire on the
	// actual content. Falls back to toolName when neither is present.
	case "image":
		target := stringArg(args, "path")
		if target == "" {
			target = stringArg(args, "url")
		}
		if target == "" {
			target = toolName
		}
		return Action{Type: ActHTTPRequest, Target: target}, nil
	case "image_generate":
		target := stringArg(args, "prompt")
		if target == "" {
			target = toolName
		}
		return Action{Type: ActHTTPRequest, Target: target}, nil
	case "web_search", "ollama_web_search":
		return Action{Type: ActHTTPRequest, Target: stringArg(args, "query")}, nil
	case "web_fetch", "ollama_web_fetch":
		return Action{Type: ActHTTPRequest, Target: stringArg(args, "url")}, nil
	case "delegate_task":
		return Action{Type: ActDelegateTask, Target: stringArg(args, "goal")}, nil
	// Agent is the Claude Code + Hermes agent-spawn tool. Maps to delegate.task
	// because spawning a subagent is the same shape as delegating work: cede
	// a tool budget to a subordinate agent. Target extraction follows the
	// driver-layer pattern: description > subagent_type > agent_id > toolName.
	case "Agent":
		target := stringArg(args, "description")
		if target == "" {
			target = stringArg(args, "subagent_type")
		}
		if target == "" {
			target = stringArg(args, "agent_id")
		}
		if target == "" {
			target = "Agent"
		}
		return Action{Type: ActDelegateTask, Target: target}, nil
	case "search_files":
		return Action{Type: ActFileRead, Target: stringArg(args, "query")}, nil
	case "skill_view":
		return Action{Type: ActFileRead, Target: stringArg(args, "skill")}, nil
	case "todo":
		return Action{Type: ActFileWrite, Target: "todo"}, nil
	}
	return Action{Type: ActUnknown, Target: toolName, Params: args}, nil
}

func normalizeShell(args map[string]any) (Action, error) {
	cmd := stringArg(args, "command")
	if cmd == "" {
		cmd = stringArg(args, "cmd")
	}
	if strings.TrimSpace(cmd) == "" {
		return Action{}, ErrMissingTarget("shell/exec/process/terminal")
	}
	return classifyShellCommand(cmd), nil
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

func normalizeWriteFile(args map[string]any) (Action, error) {
	path := stringArg(args, "path")
	if path == "" {
		path = stringArg(args, "file_path")
	}
	if path == "" {
		return Action{}, ErrMissingTarget("write_file/write/edit/patch")
	}
	return Action{Type: ActFileWrite, Target: path}, nil
}

// classifyShellCommand inspects a shell command string and returns the most
// specific canonical Action. Routes through the canon package so detection
// works against the *canonical* form of the command (env-prefix stripped,
// global flags walked past, quotes/whitespace normalized) rather than a
// raw-string substring/regex match. This closes the bypass-by-spelling
// family (#58/#59/#60/#61/#62) where anchored regexes were trivially
// evaded by leading flags, env-prefix tokens, double-spacing, quoted
// flags, or split-flag forms.
//
// Invariants:
//   - Every dangerous-pattern detection consults canon.Pipeline and the
//     bypass-class detectors in canon (IsRecursiveDelete, IsBareGitPush,
//     IsInfraDestroy, IsRemoteCodeExec, ContainsProcSubstFetch,
//     WriteDestinations) — never a raw-string regex.
//   - Each concrete dispatch (git.status, gh.pr.create, etc.) is keyed
//     on canon.Command{Tool, Action} for the FIRST segment of the
//     pipeline. Commands with no canon mapping fall through to ActShellExec.
//   - Per-segment shape annotations (curl-pipe-bash, remote-code-exec)
//     are set in Params for policy target_regex matching.
//
// Ordering matters: check destructive / re-tag-worthy patterns before
// generic per-tool dispatch so a `terraform destroy` doesn't get caught
// by a `terraform` pass-through rule first.
func classifyShellCommand(cmd string) Action {
	trimmed := strings.TrimSpace(cmd)
	// Use the AST-grade canon parser so subshells `(rm -rf /)`, command
	// substitution `$(rm -rf /)`, process substitution `bash <(curl)`,
	// heredoc destinations, and `bash -c "<string>"` re-parse all land
	// inner commands as their own pipeline segments. ParseAST auto-falls-
	// back to the tokenizer-grade Parse on parse failure, so we never
	// regress below the prior tokenizer behavior.
	pipeline := canon.ParseAST(trimmed)
	first := canon.Command{}
	if len(pipeline.Segments) > 0 {
		first = pipeline.Segments[0].Command
	}

	// === Destructive / re-tag patterns (consulted before per-tool dispatch) ===

	// rm -rf class — re-tag from shell.exec so the no-rm-recursive rule
	// matches regardless of flag spelling, whitespace, or quoting (#58).
	for _, seg := range pipeline.Segments {
		if canon.IsRecursiveDelete(seg.Command) {
			return Action{
				Type:   ActFileRecursiveDelete,
				Target: trimmed,
				Params: map[string]any{"tool": "rm"},
			}
		}
	}

	// Self-modification via shell — extract write destinations and re-tag
	// to file.write so the no-governance-self-modification rule fires
	// (#59). The target_regex on that rule already handles the path
	// patterns; we just need to set Action.Type=file.write and Target=path.
	for _, dest := range canon.WriteDestinations(trimmed) {
		if reSelfMod.MatchString(dest) {
			return Action{
				Type:   ActFileWrite,
				Target: dest,
				Params: map[string]any{"via": "shell-redirect"},
			}
		}
	}

	// Infra-destroy — terraform destroy / kubectl delete ns/namespace,
	// with leading global flags or env-prefix already stripped by canon (#62).
	for _, seg := range pipeline.Segments {
		if tool, ok := canon.IsInfraDestroy(seg.Command); ok {
			return Action{
				Type:   ActInfraDestroy,
				Target: trimmed,
				Params: map[string]any{"tool": tool},
			}
		}
	}

	// === Per-tool dispatch (keyed on canon's Tool/Action) ===

	switch first.Tool {
	case "git":
		return classifyGit(first, trimmed)
	case "gh":
		return classifyGh(first, trimmed)
	}

	// === Default: generic shell.exec with optional shape annotation ===

	action := Action{Type: ActShellExec, Target: trimmed}
	// IsRemoteCodeExec sees both `curl | bash` AND `bash <(curl)` because
	// ParseAST descends into ProcSubst. The previous regex band-aid
	// (ContainsProcSubstFetch) is no longer needed.
	if canon.IsRemoteCodeExec(pipeline) {
		action.Params = map[string]any{"shape": "remote-code-exec"}
	}
	return action
}

// classifyGit dispatches the git family (force-push, push, status, log,
// diff, commit, checkout, worktree list/add/remove) over canon's parsed
// action verb. Bare `git push` (no explicit branch) gets the sentinel
// Target value "<HEAD-implicit>" so the no-protected-push rule's
// branches list can match it without driver-side cwd resolution (#60).
func classifyGit(c canon.Command, raw string) Action {
	switch c.Action {
	case "push":
		// Force push first (it's a push superset).
		if _, force := c.Flags["force"]; force {
			return Action{Type: ActGitForcePush, Target: raw}
		}
		if _, force := c.Flags["force-with-lease"]; force {
			return Action{Type: ActGitForcePush, Target: raw}
		}
		if _, f := c.Flags["f"]; f {
			return Action{Type: ActGitForcePush, Target: raw}
		}
		// Bare push: sentinel target so the no-protected-push rule fires
		// even when the operator's branches list doesn't include "" (#60).
		if canon.IsBareGitPush(c) {
			return Action{Type: ActGitPush, Target: "<HEAD-implicit>"}
		}
		return Action{Type: ActGitPush, Target: extractPushBranch(raw)}
	case "status":
		return Action{Type: ActGitStatus, Target: raw}
	case "log":
		return Action{Type: ActGitLog, Target: raw}
	case "diff":
		return Action{Type: ActGitDiff, Target: raw}
	case "commit":
		return Action{Type: ActGitCommit, Target: raw}
	case "checkout":
		return Action{Type: ActGitCheckout, Target: raw}
	case "worktree":
		// canon doesn't sub-action worktree; inspect the raw string.
		if matched, _ := regexp.MatchString(`\bworktree\s+list\b`, raw); matched {
			return Action{Type: ActGitWorktreeList, Target: raw}
		}
		if matched, _ := regexp.MatchString(`\bworktree\s+add\b`, raw); matched {
			return Action{Type: ActGitWorktreeAdd, Target: raw}
		}
		if matched, _ := regexp.MatchString(`\bworktree\s+remove\b`, raw); matched {
			return Action{Type: ActGitWorktreeRemove, Target: raw}
		}
	}
	return Action{Type: ActShellExec, Target: raw}
}

// classifyGh dispatches `gh pr create/view/list/merge/close`,
// `gh issue create/view/list/close`, `gh api`. canon doesn't sub-action
// gh past the first verb (`pr`, `issue`, `api`), so we re-inspect args
// for the second-level verb.
func classifyGh(c canon.Command, raw string) Action {
	if c.Action == "api" {
		return Action{Type: ActGithubAPI, Target: raw}
	}
	if len(c.Args) == 0 {
		return Action{Type: ActShellExec, Target: raw}
	}
	verb := c.Args[0]
	switch c.Action {
	case "pr":
		switch verb {
		case "create":
			return Action{Type: ActGithubPRCreate, Target: raw}
		case "view":
			return Action{Type: ActGithubPRView, Target: raw}
		case "list":
			return Action{Type: ActGithubPRList, Target: raw}
		case "merge":
			return Action{Type: ActGithubPRMerge, Target: raw}
		case "close":
			return Action{Type: ActGithubPRClose, Target: raw}
		}
	case "issue":
		switch verb {
		case "create":
			return Action{Type: ActGithubIssueCreate, Target: raw}
		case "view":
			return Action{Type: ActGithubIssueView, Target: raw}
		case "list":
			return Action{Type: ActGithubIssueList, Target: raw}
		case "close":
			return Action{Type: ActGithubIssueClose, Target: raw}
		}
	}
	return Action{Type: ActShellExec, Target: raw}
}

// reSelfMod matches the same path patterns as the chitin.yaml
// no-governance-self-modification rule's target_regex. Compiled once.
var reSelfMod = regexp.MustCompile(
	`(?:(?:^|/)chitin\.yaml$|(?:^|/)\.chitin/|(?:^|/)\.hermes/plugins/chitin-governance/)`,
)

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
//     subprocess.run(["rm", "-rf", "go/"])
//  2. subprocess.run/call/Popen/check_* with a STRING argument (typically
//     shell=True, but matched regardless — string form is shell semantics):
//     subprocess.run("rm -rf go/", shell=True)
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
//
//	"rm", "-rf", "go/"
//
// and returns the space-joined unquoted string: `rm -rf go/`
func joinQuotedList(inside string) string {
	parts := []string{}
	re := regexp.MustCompile(`['"]([^'"]*)['"]`)
	for _, m := range re.FindAllStringSubmatch(inside, -1) {
		parts = append(parts, m[1])
	}
	return strings.Join(parts, " ")
}

// extractPushBranch parses `git push [flags...] [remote] [branch|HEAD:branch]`
// and returns the destination branch name, or "" if no branch arg is present
// (bare `git push`, `git push origin`, etc.).
//
// Invariant: for any input where `git push` is followed by a remote and a
// branch arg with any number of flag tokens (-x, --xxx, --xxx=val) anywhere
// before the remote, the return value is the branch name with HEAD: prefix
// stripped. For inputs without an explicit branch arg, returns "" — the
// caller is responsible for resolving the current branch from cwd if needed
// (driver/copilot/normalize.go does this for the bare-push case).
//
// Closes #52, #62.
func extractPushBranch(cmd string) string {
	after := regexp.MustCompile(`^\s*git\s+push\b\s*`).ReplaceAllString(strings.TrimSpace(cmd), "")
	var positional []string
	for _, tok := range strings.Fields(after) {
		if strings.HasPrefix(tok, "-") {
			continue
		}
		positional = append(positional, tok)
	}
	// Forms after dropping flags:
	//   []                       bare push: branch unknown, return ""
	//   ["origin"]               remote without branch: return ""
	//   ["origin", "main"]       standard: return "main"
	//   ["origin", "HEAD:main"]  HEAD prefix: strip, return "main"
	if len(positional) < 2 {
		return ""
	}
	target := positional[1]
	if strings.HasPrefix(target, "HEAD:") {
		return target[len("HEAD:"):]
	}
	return target
}

func stringArg(args map[string]any, key string) string {
	if v, ok := args[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}
