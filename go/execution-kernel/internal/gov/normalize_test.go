package gov

import (
	"strings"
	"testing"
)

func TestNormalize_TerminalRmRf(t *testing.T) {
	a, err := Normalize("terminal", map[string]any{"command": "rm -rf go/"})
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	if a.Type != ActShellExec {
		t.Errorf("Type: got %q want shell.exec", a.Type)
	}
	if a.Target != "rm -rf go/" {
		t.Errorf("Target: got %q", a.Target)
	}
}

func TestNormalize_ExecuteCodeSubprocessRm(t *testing.T) {
	// Critical bypass closure: execute_code that shells out to rm -rf
	// must produce the same Action as direct terminal rm -rf.
	code := `import subprocess
subprocess.run(["rm", "-rf", "go/"])`
	a, err := Normalize("execute_code", map[string]any{"code": code})
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	if a.Type != ActShellExec {
		t.Errorf("Type: got %q want shell.exec (bypass closure failed)", a.Type)
	}
	if a.Target == "" {
		t.Errorf("Target should be non-empty for shell.exec")
	}
}

func TestNormalize_ExecuteCodeShutilRmtree(t *testing.T) {
	code := `import shutil
shutil.rmtree("go/")`
	a, err := Normalize("execute_code", map[string]any{"code": code})
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	if a.Type != ActShellExec && a.Type != ActFileDelete {
		t.Errorf("shutil.rmtree should map to shell.exec or file.delete, got %q", a.Type)
	}
}

func TestNormalize_TerminalGitPush(t *testing.T) {
	a, _ := Normalize("terminal", map[string]any{"command": "git push origin main"})
	if a.Type != ActGitPush {
		t.Errorf("Type: got %q want git.push", a.Type)
	}
	if a.Target != "main" {
		t.Errorf("Target (branch): got %q want main", a.Target)
	}
}

func TestNormalize_TerminalGitForcePush(t *testing.T) {
	a, _ := Normalize("terminal", map[string]any{"command": "git push --force origin main"})
	if a.Type != ActGitForcePush {
		t.Errorf("Type: got %q want git.force-push", a.Type)
	}
}

// extractPushBranch must skip leading flag tokens (-u, --set-upstream, -q, etc.)
// before consuming the remote arg. Closes #52 — agent that adds -u silently
// shifts the branch capture onto the remote name.
func TestNormalize_GitPushFlagPrefixDoesNotShiftBranch(t *testing.T) {
	cases := []struct {
		cmd  string
		want string
	}{
		{"git push origin main", "main"},
		{"git push -u origin main", "main"},
		{"git push --set-upstream origin main", "main"},
		{"git push -q origin feature/x", "feature/x"},
		{"git push -u origin HEAD:main", "main"},
		{"git push origin HEAD:main", "main"},
		{"git push --no-verify origin main", "main"},
	}
	for _, tc := range cases {
		t.Run(tc.cmd, func(t *testing.T) {
			a, _ := Normalize("terminal", map[string]any{"command": tc.cmd})
			if a.Type != ActGitPush {
				t.Errorf("Type: got %q want git.push", a.Type)
			}
			if a.Target != tc.want {
				t.Errorf("Target: got %q want %q", a.Target, tc.want)
			}
		})
	}
}

// Bare `git push` (no remote, no branch) and `git push origin` (no branch)
// produce Target="". The driver layer is responsible for resolving the
// current branch from cwd. Closes #62 — without this contract, the gov
// parser silently mis-parses these forms.
func TestNormalize_GitPushNoBranchArgReturnsEmptyTarget(t *testing.T) {
	cases := []string{
		"git push",
		"git push origin",
		"git push -u origin",
		"git push --set-upstream",
	}
	for _, cmd := range cases {
		t.Run(cmd, func(t *testing.T) {
			a, _ := Normalize("terminal", map[string]any{"command": cmd})
			if a.Type != ActGitPush {
				t.Errorf("Type: got %q want git.push", a.Type)
			}
			if a.Target != "" {
				t.Errorf("Target: got %q want \"\" (driver resolves)", a.Target)
			}
		})
	}
}

func TestNormalize_TerminalGhPRCreate(t *testing.T) {
	a, _ := Normalize("terminal", map[string]any{"command": `gh pr create --title "x"`})
	if a.Type != ActGithubPRCreate {
		t.Errorf("Type: got %q want github.pr.create", a.Type)
	}
}

func TestNormalize_WriteFileEnv(t *testing.T) {
	a, _ := Normalize("write_file", map[string]any{"path": ".env", "content": "X=1"})
	if a.Type != ActFileWrite {
		t.Errorf("Type: got %q want file.write", a.Type)
	}
	if a.Target != ".env" {
		t.Errorf("Target: got %q want .env", a.Target)
	}
}

func TestNormalize_WriteFileGovernanceSelfMod(t *testing.T) {
	a, _ := Normalize("write_file", map[string]any{"path": "chitin.yaml", "content": "..."})
	if a.Type != ActFileWrite {
		t.Errorf("Type: got %q want file.write", a.Type)
	}
	if a.Target != "chitin.yaml" {
		t.Errorf("Target: got %q want chitin.yaml", a.Target)
	}
}

func TestNormalize_DelegateTask(t *testing.T) {
	a, _ := Normalize("delegate_task", map[string]any{"goal": "review"})
	if a.Type != ActDelegateTask {
		t.Errorf("Type: got %q want delegate.task", a.Type)
	}
}

func TestNormalize_ExecuteCodeSubprocessString(t *testing.T) {
	// Regression for C3: subprocess.run("rm -rf X", shell=True) — string form
	// (not a list literal) must also route through shell.exec so policy
	// regexes can match. Previous extractShellIntent only handled list form.
	code := `import subprocess
subprocess.run("rm -rf /tmp/x", shell=True)`
	a, _ := Normalize("execute_code", map[string]any{"code": code})
	if a.Type != ActShellExec {
		t.Errorf("subprocess.run string-form must map to shell.exec, got %q", a.Type)
	}
	if a.Target == "" || a.Target == "execute_code" {
		t.Errorf("Target should be the extracted command, got %q", a.Target)
	}
}

func TestNormalize_ExecuteCodeOsSystem(t *testing.T) {
	// Regression for C3: os.system("rm -rf X") is a pure shell call.
	code := `import os
os.system("rm -rf /tmp/x")`
	a, _ := Normalize("execute_code", map[string]any{"code": code})
	if a.Type != ActShellExec {
		t.Errorf("os.system must map to shell.exec, got %q", a.Type)
	}
}

func TestNormalize_ExecuteCodeRawRmFallback(t *testing.T) {
	// Regression for C3: if we can't parse the specific call pattern but
	// the code contains "rm -rf" anywhere (f-strings, pathlib.unlink,
	// obfuscated variants), fall back to treating the whole code as a
	// shell.exec so the no-destructive-rm target:"rm -rf" substring match
	// still fires.
	code := `import subprocess
cmd = "rm -rf " + target_dir
subprocess.run(cmd, shell=True)`
	a, _ := Normalize("execute_code", map[string]any{"code": code})
	if a.Type != ActShellExec {
		t.Errorf("code containing 'rm -rf' should map to shell.exec, got %q", a.Type)
	}
	if !strings.Contains(a.Target, "rm -rf") {
		t.Errorf("Target should contain 'rm -rf' so target: rule still matches, got %q", a.Target)
	}
}

func TestNormalize_TerraformDestroy(t *testing.T) {
	cases := []struct {
		name     string
		command  string
		wantType ActionType
		wantTool string
	}{
		{"basic", "terraform destroy", ActInfraDestroy, "terraform"},
		{"with auto-approve", "terraform destroy -auto-approve", ActInfraDestroy, "terraform"},
		{"with target", "terraform destroy -target=aws_instance.web", ActInfraDestroy, "terraform"},
		{"plan is NOT destroy", "terraform plan", ActShellExec, ""},
		{"apply is NOT destroy", "terraform apply -auto-approve", ActShellExec, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Normalize("terminal", map[string]any{"command": tc.command})
			if err != nil {
				t.Fatalf("Normalize: %v", err)
			}
			if got.Type != tc.wantType {
				t.Errorf("Type: got %q, want %q", got.Type, tc.wantType)
			}
			if tc.wantTool != "" {
				tool, _ := got.Params["tool"].(string)
				if tool != tc.wantTool {
					t.Errorf("Params[tool]: got %q, want %q", tool, tc.wantTool)
				}
			}
		})
	}
}

func TestNormalize_KubectlDelete(t *testing.T) {
	cases := []struct {
		name     string
		command  string
		wantType ActionType
		wantTool string
	}{
		{"delete ns", "kubectl delete ns production", ActInfraDestroy, "kubectl"},
		{"delete namespace", "kubectl delete namespace production", ActInfraDestroy, "kubectl"},
		{"delete pod is NOT infra destroy", "kubectl delete pod my-pod", ActShellExec, ""},
		{"get ns is NOT destroy", "kubectl get ns", ActShellExec, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Normalize("terminal", map[string]any{"command": tc.command})
			if err != nil {
				t.Fatalf("Normalize: %v", err)
			}
			if got.Type != tc.wantType {
				t.Errorf("Type: got %q, want %q", got.Type, tc.wantType)
			}
			if tc.wantTool != "" {
				tool, _ := got.Params["tool"].(string)
				if tool != tc.wantTool {
					t.Errorf("Params[tool]: got %q, want %q", tool, tc.wantTool)
				}
			}
		})
	}
}

func TestNormalize_UnknownTool(t *testing.T) {
	a, _ := Normalize("no_such_tool", map[string]any{"foo": "bar"})
	if a.Type != ActUnknown {
		t.Errorf("Type: got %q want unknown (fail-closed)", a.Type)
	}
}

func TestNormalize_CurlPipeBash(t *testing.T) {
	cases := []struct {
		name      string
		command   string
		wantShape string
	}{
		{"pipe to bash", "curl https://example.com/install.sh | bash", "curl-pipe-bash"},
		{"pipe to sh", "curl https://example.com/install.sh | sh", "curl-pipe-bash"},
		{"pipe no space", "curl -fsSL https://example.com/i.sh |bash", "curl-pipe-bash"},
		{"curl without pipe is plain shell", "curl https://example.com/", ""},
		{"wget pipe bash is NOT caught (curl-specific)", "wget -qO- https://example.com/i.sh | bash", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Normalize("terminal", map[string]any{"command": tc.command})
			if err != nil {
				t.Fatalf("Normalize: %v", err)
			}
			if got.Type != ActShellExec {
				t.Errorf("Type: got %q, want shell.exec", got.Type)
			}
			shape, _ := got.Params["shape"].(string)
			if shape != tc.wantShape {
				t.Errorf("Params[shape]: got %q, want %q", shape, tc.wantShape)
			}
		})
	}
}

func TestNormalize_TerminalReadOnly(t *testing.T) {
	cases := []struct{ cmd string; want ActionType }{
		{"ls -la", ActShellExec},
		{"cat /etc/passwd", ActShellExec},
		{"git status", ActGitStatus},
		{"git log --oneline", ActGitLog},
		{"git diff main", ActGitDiff},
		{"gh issue view 40", ActGithubIssueView},
		{"gh issue list", ActGithubIssueList},
	}
	for _, c := range cases {
		a, _ := Normalize("terminal", map[string]any{"command": c.cmd})
		if a.Type != c.want {
			t.Errorf("%q: got %q want %q", c.cmd, a.Type, c.want)
		}
	}
}

// openclaw pi-runtime tool names — closes the "exec/process/read/write/edit
// fall through to ActUnknown" gap that left the openclaw plugin gating with
// default-deny-unknown instead of policy-meaningful action types.
//
// Invariant: an exec/process call with the same command as a terminal call
// produces the same Action — one rule catches all routes (bypass closure).

func TestNormalize_OpenclawExec(t *testing.T) {
	// Same shape as terminal: passes cmd, lands on shell.exec.
	a, _ := Normalize("exec", map[string]any{"cmd": "echo gate-fired"})
	if a.Type != ActShellExec {
		t.Errorf("Type: got %q want shell.exec", a.Type)
	}
	if a.Target != "echo gate-fired" {
		t.Errorf("Target: got %q want %q", a.Target, "echo gate-fired")
	}
}

func TestNormalize_OpenclawExec_BypassClosure(t *testing.T) {
	// Bypass closure: openclaw `exec` and `terminal` must produce the same
	// Action (Type + Fingerprint) for the same command, so one policy rule
	// catches both surfaces. This test does NOT assert the resulting type
	// is "dangerous" — `rm -rf` is just a representative command. The
	// invariant under test is parity, not classification.
	terminalA, _ := Normalize("terminal", map[string]any{"command": "rm -rf go/"})
	execA, _ := Normalize("exec", map[string]any{"cmd": "rm -rf go/"})
	if terminalA.Type != execA.Type {
		t.Errorf("terminal/exec divergence: terminal=%q exec=%q", terminalA.Type, execA.Type)
	}
	if terminalA.Fingerprint() != execA.Fingerprint() {
		t.Errorf("fingerprint divergence: terminal=%s exec=%s",
			terminalA.Fingerprint(), execA.Fingerprint())
	}
}

func TestNormalize_OpenclawProcess(t *testing.T) {
	// `process` is openclaw's long-running/backgrounded shell tool —
	// same command shape, same gate decision.
	a, _ := Normalize("process", map[string]any{"cmd": "tail -f /var/log/syslog"})
	if a.Type != ActShellExec {
		t.Errorf("Type: got %q want shell.exec", a.Type)
	}
}

func TestNormalize_OpenclawWrite(t *testing.T) {
	a, _ := Normalize("write", map[string]any{"path": "/tmp/foo.txt", "content": "hi"})
	if a.Type != ActFileWrite {
		t.Errorf("Type: got %q want file.write", a.Type)
	}
	if a.Target != "/tmp/foo.txt" {
		t.Errorf("Target: got %q", a.Target)
	}
}

func TestNormalize_OpenclawEdit(t *testing.T) {
	// `edit` is in-place mutation; same policy class as `write`.
	a, _ := Normalize("edit", map[string]any{"path": "/etc/hosts"})
	if a.Type != ActFileWrite {
		t.Errorf("Type: got %q want file.write (edit is mutation)", a.Type)
	}
	if a.Target != "/etc/hosts" {
		t.Errorf("Target: got %q", a.Target)
	}
}

func TestNormalize_OpenclawRead(t *testing.T) {
	a, _ := Normalize("read", map[string]any{"path": "/etc/passwd"})
	if a.Type != ActFileRead {
		t.Errorf("Type: got %q want file.read", a.Type)
	}
	if a.Target != "/etc/passwd" {
		t.Errorf("Target: got %q", a.Target)
	}
}

func TestNormalize_OpenclawRead_FilePathAlias(t *testing.T) {
	// Some openclaw schemas use file_path; both should be accepted.
	a, _ := Normalize("read", map[string]any{"file_path": "/etc/hostname"})
	if a.Target != "/etc/hostname" {
		t.Errorf("file_path alias not honored: got %q", a.Target)
	}
}

func TestNormalize_OpenclawTools_NoLongerUnknown(t *testing.T) {
	// Regression: each of these used to fall into ActUnknown and trip
	// default-deny-unknown. After the openclaw mappings, none should be
	// ActUnknown — they get policy-meaningful types.
	for _, tool := range []string{"exec", "process", "read", "write", "edit"} {
		a, _ := Normalize(tool, map[string]any{"cmd": "noop", "path": "/tmp/x"})
		if a.Type == ActUnknown {
			t.Errorf("%s still maps to ActUnknown — normalizer mapping missing", tool)
		}
	}
}

// openclaw chat-domain tools — slice 3 normalizer coverage. These are the
// tools openclaw's pi-runtime registers via memory-core, sessions, image,
// ollama-web, and cron extensions. Without these mappings, they fall into
// ActUnknown and trip default-deny-unknown — which would make `mode=enforce`
// on the openclaw-governance plugin deadlock the agent on every tool call.
//
// The openclaw plugin's default-mode flip from `observe` → `enforce` is
// only safe once these mappings exist AND the policy has rules covering
// the resulting action types (file.read, delegate.task, http.request —
// all already in chitin.yaml as default-allow-* rules).

func TestNormalize_OpenclawMemorySearch(t *testing.T) {
	a, _ := Normalize("memory_search", map[string]any{"query": "chitin governance"})
	if a.Type != ActFileRead {
		t.Errorf("Type: got %q want file.read", a.Type)
	}
	if a.Target != "chitin governance" {
		t.Errorf("Target: got %q", a.Target)
	}
}

func TestNormalize_OpenclawMemoryGet(t *testing.T) {
	a, _ := Normalize("memory_get", map[string]any{"path": "MEMORY.md"})
	if a.Type != ActFileRead {
		t.Errorf("Type: got %q want file.read", a.Type)
	}
	if a.Target != "MEMORY.md" {
		t.Errorf("Target: got %q", a.Target)
	}
}

func TestNormalize_OpenclawSessionReads(t *testing.T) {
	// All session-read tools map to file.read with toolName as target.
	for _, tool := range []string{"sessions_list", "sessions_history", "sessions_yield", "session_status"} {
		a, _ := Normalize(tool, map[string]any{})
		if a.Type != ActFileRead {
			t.Errorf("%s: Type got %q want file.read", tool, a.Type)
		}
		if a.Target != tool {
			t.Errorf("%s: Target got %q want %q", tool, a.Target, tool)
		}
	}
}

func TestNormalize_OpenclawSessionMutates(t *testing.T) {
	// All session-mutate tools map to delegate.task.
	for _, tool := range []string{"sessions_send", "sessions_spawn", "subagents", "cron"} {
		a, _ := Normalize(tool, map[string]any{})
		if a.Type != ActDelegateTask {
			t.Errorf("%s: Type got %q want delegate.task", tool, a.Type)
		}
	}
}

func TestNormalize_OpenclawSessionsSpawn_TargetExtracted(t *testing.T) {
	// Spawn carries the target agent id. Surface it for policy targeting.
	a, _ := Normalize("sessions_spawn", map[string]any{"agentId": "qwen-agent"})
	if a.Type != ActDelegateTask {
		t.Errorf("Type: got %q", a.Type)
	}
	if a.Target != "qwen-agent" {
		t.Errorf("Target: got %q want qwen-agent", a.Target)
	}
}

func TestNormalize_OpenclawDelegateBypassClosure(t *testing.T) {
	// Bypass closure with the existing `delegate_task` mapping: any rule
	// matching delegate.task catches sessions_spawn, subagents, cron
	// equivalently. Verify type identity (not target — different schemas
	// surface different fields).
	canonical, _ := Normalize("delegate_task", map[string]any{"goal": "build"})
	for _, tool := range []string{"sessions_send", "sessions_spawn", "subagents", "cron"} {
		a, _ := Normalize(tool, map[string]any{})
		if a.Type != canonical.Type {
			t.Errorf("%s diverges from delegate_task: got %q want %q",
				tool, a.Type, canonical.Type)
		}
	}
}

func TestNormalize_OpenclawImageTools(t *testing.T) {
	for _, tool := range []string{"image", "image_generate"} {
		a, _ := Normalize(tool, map[string]any{})
		if a.Type != ActHTTPRequest {
			t.Errorf("%s: Type got %q want http.request", tool, a.Type)
		}
	}
}

func TestNormalize_OpenclawOllamaWebSearch(t *testing.T) {
	a, _ := Normalize("ollama_web_search", map[string]any{"query": "chitin governance"})
	if a.Type != ActHTTPRequest {
		t.Errorf("Type: got %q want http.request", a.Type)
	}
	if a.Target != "chitin governance" {
		t.Errorf("Target: got %q", a.Target)
	}
}

func TestNormalize_OpenclawOllamaWebFetch(t *testing.T) {
	a, _ := Normalize("ollama_web_fetch", map[string]any{"url": "https://example.com"})
	if a.Type != ActHTTPRequest {
		t.Errorf("Type: got %q want http.request", a.Type)
	}
	if a.Target != "https://example.com" {
		t.Errorf("Target: got %q", a.Target)
	}
}

func TestNormalize_OpenclawWebTools_PlainAndPrefixed(t *testing.T) {
	// Openclaw registers both plain (web_search, web_fetch) and
	// provider-prefixed (ollama_web_*) names. Both must produce the same
	// Action so policy rules don't depend on which provider is wired.
	plainSearch, _ := Normalize("web_search", map[string]any{"query": "q"})
	prefixedSearch, _ := Normalize("ollama_web_search", map[string]any{"query": "q"})
	if plainSearch.Type != prefixedSearch.Type || plainSearch.Target != prefixedSearch.Target {
		t.Errorf("web_search divergence: plain=%v prefixed=%v", plainSearch, prefixedSearch)
	}
	plainFetch, _ := Normalize("web_fetch", map[string]any{"url": "https://x"})
	prefixedFetch, _ := Normalize("ollama_web_fetch", map[string]any{"url": "https://x"})
	if plainFetch.Type != prefixedFetch.Type || plainFetch.Target != prefixedFetch.Target {
		t.Errorf("web_fetch divergence: plain=%v prefixed=%v", plainFetch, prefixedFetch)
	}
	if plainSearch.Type != ActHTTPRequest {
		t.Errorf("web_search: got %q want http.request", plainSearch.Type)
	}
}

func TestNormalize_OpenclawChatDomain_NoneUnknown(t *testing.T) {
	// Regression: every chat-domain tool the pi-runtime exposes for the
	// `main` openclaw agent today must produce a non-Unknown action so
	// the plugin can run in mode=enforce without deadlocking.
	chatDomain := []string{
		"memory_search", "memory_get",
		"sessions_list", "sessions_history", "sessions_send", "sessions_spawn",
		"sessions_yield", "subagents", "session_status",
		"image", "image_generate",
		"cron",
		"web_search", "web_fetch",
		"ollama_web_search", "ollama_web_fetch",
	}
	for _, tool := range chatDomain {
		a, _ := Normalize(tool, map[string]any{})
		if a.Type == ActUnknown {
			t.Errorf("%s still maps to ActUnknown — normalizer mapping missing", tool)
		}
	}
}
