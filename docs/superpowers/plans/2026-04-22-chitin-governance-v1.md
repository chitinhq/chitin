# Chitin Governance v1 — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship `chitin-kernel gate` — a subprocess-invoked governance subcommand that normalizes agent tool calls to canonical `Action` types, evaluates them against a per-repo YAML policy (modes: `monitor`/`enforce`/`guide`) with blast-radius bounds and an escalation ladder, and returns structured decisions — plus a hermes plugin that wires it into every `pre_tool_call`.

**Architecture:** New `go/execution-kernel/internal/gov/` package with typed actions, policy engine (YAML + inheritance), bounds checker (for push-shaped actions), SQLite-backed escalation counter, decision JSONL writer, and an orchestrating gate. New `gate` CLI subcommand. Hermes plugin shells out to the gate on `pre_tool_call` and translates deny decisions into hermes block messages. Baseline `chitin.yaml` at the repo root.

**Tech Stack:** Go 1.25, `modernc.org/sqlite v1.49.1` (already in `go.mod`), `gopkg.in/yaml.v3`, standard library. Python 3.11+ for the hermes plugin (matches existing chitin-sink plugin pattern).

**Spec:** `docs/superpowers/specs/2026-04-22-chitin-governance-v1-design.md`

---

## File Structure

Files this plan creates or modifies:

### Chitin repo — new (`go/execution-kernel/internal/gov/`)

- **Create:** `action.go` — `ActionType` enum, `Action` struct, `Action.Fingerprint() string`
- **Create:** `action_test.go`
- **Create:** `normalize.go` — `Normalize(toolName string, args map[string]any) (Action, error)` table-driven
- **Create:** `normalize_test.go`
- **Create:** `policy.go` — `Policy`, `Rule`, `Decision` types, `Policy.Evaluate(Action) Decision`, YAML parse
- **Create:** `policy_test.go`
- **Create:** `inherit.go` — `LoadWithInheritance(cwd string) (Policy, []string, error)` walks parents
- **Create:** `inherit_test.go`
- **Create:** `bounds.go` — `CheckBounds(action Action, policy Policy, cwd string) Decision`; shells to `git diff --stat`
- **Create:** `bounds_test.go`
- **Create:** `escalation.go` — `Counter` backed by SQLite; `RecordDenial`, `Level`, `IsLocked`, `Reset`, `Lockdown`
- **Create:** `escalation_test.go`
- **Create:** `decision.go` — `Decision` struct; `WriteLog(d Decision, dir string) error`
- **Create:** `decision_test.go`
- **Create:** `gate.go` — `Gate{policy, counter, logDir}`; `Gate.Evaluate(Action, agent string) Decision` orchestrates policy → bounds → counter → log
- **Create:** `gate_test.go`
- **Create:** `integration_test.go` — end-to-end tests exercising the full pipeline against temp dirs
- **Create:** `testdata/policy-baseline.yaml`
- **Create:** `testdata/policy-malformed.yaml`
- **Create:** `testdata/policy-strict-parent.yaml`
- **Create:** `testdata/policy-child-too-loose.yaml`

### Chitin repo — modify

- **Modify:** `go/execution-kernel/cmd/chitin-kernel/main.go` — add `case "gate":` dispatch + `cmdGate` with subcommands `evaluate`, `status`, `lockdown`, `reset`
- **Create:** `chitin.yaml` (repo root) — baseline policy
- **Create:** `docs/governance-setup.md` — operator install doc

### Operator machine — new (outside chitin repo)

- **Create:** `~/.hermes/plugins/chitin-governance/__init__.py`
- **Create:** `~/.hermes/plugins/chitin-governance/plugin.yaml`
- **Create:** `~/.hermes/plugins/chitin-governance/test_plugin.py`

### Runtime artifacts (auto-created on first use)

- `~/.chitin/gov.db` — SQLite escalation state
- `~/.chitin/gov-decisions-YYYY-MM-DD.jsonl` — decision log

---

## Task 0: Verify prerequisites

**Files:** None.

- [ ] **Step 0.1: Confirm worktree + branch state**

```bash
cd ~/workspace/chitin-governance-v1 && git status && git branch --show-current
```
Expected: clean working tree on `spec/chitin-governance-v1`, spec + plan both committed.

- [ ] **Step 0.2: Confirm dependencies**

```bash
cd ~/workspace/chitin-governance-v1/go/execution-kernel && grep -E "modernc.org/sqlite|yaml.v3" go.mod
```
Expected: both lines present.

- [ ] **Step 0.3: Confirm all tests pass before starting**

```bash
cd ~/workspace/chitin-governance-v1/go/execution-kernel && go test ./... 2>&1 | tail -5
```
Expected: all `ok`. If any fails, stop and resolve before touching gov/.

---

## Task 1: `action.go` — ActionType vocabulary + Fingerprint

**Files:**
- Create: `go/execution-kernel/internal/gov/action.go`
- Create: `go/execution-kernel/internal/gov/action_test.go`

- [ ] **Step 1.1: Write the failing tests**

Create `go/execution-kernel/internal/gov/action_test.go`:

```go
package gov

import (
	"strings"
	"testing"
)

func TestActionType_ClosedEnum(t *testing.T) {
	// Spot-check a few expected types exist as constants.
	wantPresent := []ActionType{
		ActShellExec, ActFileRead, ActFileWrite, ActFileDelete,
		ActGitPush, ActGitForcePush, ActGitCommit, ActGitCheckout,
		ActGitWorktreeAdd, ActGithubPRCreate, ActGithubIssueView,
		ActDelegateTask, ActHTTPRequest, ActUnknown,
	}
	for _, a := range wantPresent {
		if a == "" {
			t.Errorf("ActionType constant is empty string — did you forget to assign?")
		}
	}
}

func TestAction_Fingerprint_Deterministic(t *testing.T) {
	a := Action{Type: ActShellExec, Target: "rm -rf go/", Path: "/tmp"}
	fp1 := a.Fingerprint()
	fp2 := a.Fingerprint()
	if fp1 != fp2 {
		t.Fatalf("Fingerprint not deterministic: %q vs %q", fp1, fp2)
	}
	if len(fp1) != 64 {
		t.Fatalf("Fingerprint should be 64 hex chars (sha256), got %d", len(fp1))
	}
}

func TestAction_Fingerprint_SamePatternSameFP(t *testing.T) {
	// Path should NOT affect the fingerprint — rm -rf across different
	// dirs shares a fingerprint for escalation-counting purposes.
	a := Action{Type: ActShellExec, Target: "rm -rf go/", Path: "/a"}
	b := Action{Type: ActShellExec, Target: "rm -rf go/", Path: "/b"}
	if a.Fingerprint() != b.Fingerprint() {
		t.Fatalf("Fingerprint should ignore Path, got different for %+v vs %+v", a, b)
	}
}

func TestAction_Fingerprint_DifferentTypeDifferentFP(t *testing.T) {
	a := Action{Type: ActShellExec, Target: "ls"}
	b := Action{Type: ActFileRead, Target: "ls"}
	if a.Fingerprint() == b.Fingerprint() {
		t.Fatalf("Fingerprint collision across different types")
	}
}

func TestAction_String_Debuggable(t *testing.T) {
	s := Action{Type: ActShellExec, Target: "foo", Path: "/tmp"}.String()
	if !strings.Contains(s, "shell.exec") || !strings.Contains(s, "foo") {
		t.Errorf("Action.String should contain Type and Target, got %q", s)
	}
}
```

- [ ] **Step 1.2: Run tests — verify they fail**

```bash
cd ~/workspace/chitin-governance-v1/go/execution-kernel && go test ./internal/gov/ -v 2>&1 | tail -10
```
Expected: compile error `package gov is not in std; no Go files in ...`. Good — package doesn't exist yet.

- [ ] **Step 1.3: Implement `action.go`**

Create `go/execution-kernel/internal/gov/action.go`:

```go
// Package gov implements Chitin's tool-boundary governance: canonical
// action vocabulary, policy evaluation, blast-radius bounds, and an
// escalation counter. See:
//   docs/superpowers/specs/2026-04-22-chitin-governance-v1-design.md
package gov

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// ActionType is the canonical vocabulary of things an agent can propose.
// Closed enum: Normalize produces ActUnknown for anything not on this list.
type ActionType string

const (
	ActShellExec         ActionType = "shell.exec"
	ActFileRead          ActionType = "file.read"
	ActFileWrite         ActionType = "file.write"
	ActFileDelete        ActionType = "file.delete"
	ActFileMove          ActionType = "file.move"
	ActGitDiff           ActionType = "git.diff"
	ActGitLog            ActionType = "git.log"
	ActGitStatus         ActionType = "git.status"
	ActGitCommit         ActionType = "git.commit"
	ActGitCheckout       ActionType = "git.checkout"
	ActGitBranchCreate   ActionType = "git.branch.create"
	ActGitBranchDelete   ActionType = "git.branch.delete"
	ActGitMerge          ActionType = "git.merge"
	ActGitPush           ActionType = "git.push"
	ActGitForcePush      ActionType = "git.force-push"
	ActGitWorktreeList   ActionType = "git.worktree.list"
	ActGitWorktreeAdd    ActionType = "git.worktree.add"
	ActGitWorktreeRemove ActionType = "git.worktree.remove"
	ActGithubPRCreate    ActionType = "github.pr.create"
	ActGithubPRView      ActionType = "github.pr.view"
	ActGithubPRList      ActionType = "github.pr.list"
	ActGithubPRMerge     ActionType = "github.pr.merge"
	ActGithubPRClose     ActionType = "github.pr.close"
	ActGithubIssueList   ActionType = "github.issue.list"
	ActGithubIssueView   ActionType = "github.issue.view"
	ActGithubIssueCreate ActionType = "github.issue.create"
	ActGithubIssueClose  ActionType = "github.issue.close"
	ActGithubAPI         ActionType = "github.api"
	ActDelegateTask      ActionType = "delegate.task"
	ActHTTPRequest       ActionType = "http.request"
	ActNPMInstall        ActionType = "npm.install"
	ActNPMRun            ActionType = "npm.script.run"
	ActTestRun           ActionType = "test.run"
	ActMCPCall           ActionType = "mcp.call"
	ActUnknown           ActionType = "unknown"
)

// Action is a normalized tool call — the unit of policy evaluation.
// Path is the cwd the action would execute against; not part of the
// fingerprint (pattern-based counting, not per-path).
type Action struct {
	Type   ActionType
	Target string
	Path   string
	Params map[string]any
}

// Fingerprint returns a stable SHA256 hex digest of (Type, Target).
// Path is excluded intentionally — rm -rf across different targets
// shares a fingerprint because the pattern is the anomaly.
func (a Action) Fingerprint() string {
	h := sha256.Sum256([]byte(string(a.Type) + "\x00" + a.Target))
	return hex.EncodeToString(h[:])
}

// String returns a debuggable one-line representation.
func (a Action) String() string {
	return fmt.Sprintf("Action{%s target=%q path=%q}", a.Type, a.Target, a.Path)
}
```

- [ ] **Step 1.4: Run tests — verify they pass**

```bash
go test ./internal/gov/ -run 'TestActionType|TestAction_' -v 2>&1 | tail -15
```
Expected: all PASS.

- [ ] **Step 1.5: Commit**

```bash
rtk git add go/execution-kernel/internal/gov/action.go go/execution-kernel/internal/gov/action_test.go
rtk git -c user.email=jpleva91@gmail.com commit -m "gov: action vocabulary + fingerprint"
```

---

## Task 2: `normalize.go` — tool call → canonical Action

**Files:**
- Create: `go/execution-kernel/internal/gov/normalize.go`
- Create: `go/execution-kernel/internal/gov/normalize_test.go`

- [ ] **Step 2.1: Write the failing tests**

Create `go/execution-kernel/internal/gov/normalize_test.go`:

```go
package gov

import "testing"

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

func TestNormalize_UnknownTool(t *testing.T) {
	a, _ := Normalize("no_such_tool", map[string]any{"foo": "bar"})
	if a.Type != ActUnknown {
		t.Errorf("Type: got %q want unknown (fail-closed)", a.Type)
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
```

- [ ] **Step 2.2: Verify tests fail**

```bash
go test ./internal/gov/ -run TestNormalize -v 2>&1 | tail -5
```
Expected: FAIL with `undefined: Normalize`.

- [ ] **Step 2.3: Implement `normalize.go`**

Create `go/execution-kernel/internal/gov/normalize.go`:

```go
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
```

- [ ] **Step 2.4: Verify tests pass**

```bash
go test ./internal/gov/ -run TestNormalize -v 2>&1 | tail -20
```
Expected: all PASS.

- [ ] **Step 2.5: Commit**

```bash
rtk git add go/execution-kernel/internal/gov/normalize.go go/execution-kernel/internal/gov/normalize_test.go
rtk git -c user.email=jpleva91@gmail.com commit -m "gov: tool-call normalizer with execute_code bypass closure"
```

---

## Task 3: `policy.go` — rule types + Evaluate

**Files:**
- Create: `go/execution-kernel/internal/gov/policy.go`
- Create: `go/execution-kernel/internal/gov/policy_test.go`
- Create: `go/execution-kernel/internal/gov/testdata/policy-baseline.yaml`
- Create: `go/execution-kernel/internal/gov/testdata/policy-malformed.yaml`

- [ ] **Step 3.1: Create fixture files**

Create `testdata/policy-baseline.yaml`:

```yaml
id: test-baseline
name: Test baseline
mode: guide

bounds:
  max_files_changed: 25
  max_lines_changed: 500
  max_runtime_seconds: 900

escalation:
  elevated_threshold: 3
  high_threshold: 7
  lockdown_threshold: 10

rules:
  - id: no-destructive-rm
    action: shell.exec
    effect: deny
    target: "rm -rf"
    reason: "Recursive delete is blocked"
    suggestion: "Use git rm <specific-files>"
    correctedCommand: "git rm <files>"

  - id: no-protected-push
    action: git.push
    effect: deny
    branches: [main, master]
    reason: "Direct push to protected branch"

  - id: no-env-write
    action: file.write
    effect: deny
    target: ".env"
    reason: "Secrets"

  - id: allow-reads
    action: file.read
    effect: allow
    reason: "Reads are safe"
```

Create `testdata/policy-malformed.yaml`:

```yaml
id: malformed
mode: guide
rules:
  - id: broken
    action:
    effect: deny
    target  "no colon"
```

- [ ] **Step 3.2: Write the failing tests**

Create `go/execution-kernel/internal/gov/policy_test.go`:

```go
package gov

import (
	"path/filepath"
	"testing"
)

func loadBaseline(t *testing.T) Policy {
	t.Helper()
	p, err := LoadPolicyFile(filepath.Join("testdata", "policy-baseline.yaml"))
	if err != nil {
		t.Fatalf("LoadPolicyFile: %v", err)
	}
	return p
}

func TestPolicy_LoadBaseline(t *testing.T) {
	p := loadBaseline(t)
	if p.ID != "test-baseline" {
		t.Errorf("ID: got %q", p.ID)
	}
	if p.Mode != "guide" {
		t.Errorf("Mode: got %q", p.Mode)
	}
	if p.Bounds.MaxFilesChanged != 25 {
		t.Errorf("Bounds.MaxFilesChanged: got %d", p.Bounds.MaxFilesChanged)
	}
	if len(p.Rules) != 4 {
		t.Errorf("Rules count: got %d want 4", len(p.Rules))
	}
}

func TestPolicy_LoadMalformed(t *testing.T) {
	_, err := LoadPolicyFile(filepath.Join("testdata", "policy-malformed.yaml"))
	if err == nil {
		t.Fatal("LoadPolicyFile should fail on malformed YAML")
	}
}

func TestPolicy_Evaluate_DenyFirstWins(t *testing.T) {
	p := loadBaseline(t)
	a := Action{Type: ActShellExec, Target: "rm -rf go/"}
	d := p.Evaluate(a)
	if d.Allowed {
		t.Errorf("rm -rf should be denied")
	}
	if d.RuleID != "no-destructive-rm" {
		t.Errorf("RuleID: got %q", d.RuleID)
	}
	if d.Reason == "" {
		t.Errorf("Reason should be populated")
	}
	if d.Suggestion == "" {
		t.Errorf("Suggestion should be populated")
	}
	if d.CorrectedCommand == "" {
		t.Errorf("CorrectedCommand should be populated")
	}
}

func TestPolicy_Evaluate_BranchCondition(t *testing.T) {
	p := loadBaseline(t)
	// push to main — denied
	d := p.Evaluate(Action{Type: ActGitPush, Target: "main"})
	if d.Allowed {
		t.Errorf("push to main should be denied")
	}
	if d.RuleID != "no-protected-push" {
		t.Errorf("RuleID: got %q", d.RuleID)
	}
	// push to feature — allowed (falls through to default)
	d2 := p.Evaluate(Action{Type: ActGitPush, Target: "fix/42-something"})
	if !d2.Allowed {
		t.Errorf("push to feature branch should be allowed (default), got rule=%q reason=%q", d2.RuleID, d2.Reason)
	}
}

func TestPolicy_Evaluate_AllowMatch(t *testing.T) {
	p := loadBaseline(t)
	d := p.Evaluate(Action{Type: ActFileRead, Target: "anything"})
	if !d.Allowed {
		t.Errorf("file.read should match allow-reads, got %+v", d)
	}
	if d.RuleID != "allow-reads" {
		t.Errorf("RuleID: got %q", d.RuleID)
	}
}

func TestPolicy_Evaluate_DefaultDeny(t *testing.T) {
	// No rule matches ActUnknown by default → fail-closed deny.
	p := loadBaseline(t)
	d := p.Evaluate(Action{Type: ActUnknown, Target: "weird_tool"})
	if d.Allowed {
		t.Errorf("unknown action should default-deny, got %+v", d)
	}
	if d.RuleID != "default-deny" {
		t.Errorf("RuleID: got %q want default-deny", d.RuleID)
	}
}

func TestPolicy_ModeDefault(t *testing.T) {
	// A policy with no explicit Mode should default to "guide".
	p := Policy{ID: "test", Rules: []Rule{}}
	p.ApplyDefaults()
	if p.Mode != "guide" {
		t.Errorf("default Mode: got %q want guide", p.Mode)
	}
}

func TestPolicy_Evaluate_InvariantModeOverride(t *testing.T) {
	p := Policy{
		ID:             "t",
		Mode:           "guide",
		InvariantModes: map[string]string{"no-env-write": "enforce"},
		Rules: []Rule{{
			ID: "no-env-write", Action: "file.write", Effect: "deny",
			Target: ".env", Reason: "secrets",
		}},
	}
	p.ApplyDefaults()
	d := p.Evaluate(Action{Type: ActFileWrite, Target: ".env"})
	if d.Mode != "enforce" {
		t.Errorf("InvariantMode override not applied: got %q want enforce", d.Mode)
	}
}
```

- [ ] **Step 3.3: Verify tests fail**

```bash
go test ./internal/gov/ -run TestPolicy -v 2>&1 | tail -5
```
Expected: compile error `undefined: Policy, LoadPolicyFile, Rule`.

- [ ] **Step 3.4: Implement `policy.go`**

Create `go/execution-kernel/internal/gov/policy.go`:

```go
package gov

import (
	"fmt"
	"os"
	"regexp"

	"gopkg.in/yaml.v3"
)

// Policy is the merged rule set evaluated on every gate call.
// Loaded from YAML; LoadWithInheritance merges parent chitin.yaml
// files into a single Policy before evaluation.
type Policy struct {
	ID             string            `yaml:"id"`
	Name           string            `yaml:"name,omitempty"`
	Mode           string            `yaml:"mode,omitempty"` // monitor | enforce | guide; default guide
	Pack           string            `yaml:"pack,omitempty"`
	InvariantModes map[string]string `yaml:"invariantModes,omitempty"` // ruleID → mode
	Bounds         Bounds            `yaml:"bounds,omitempty"`
	Escalation     EscalationConfig  `yaml:"escalation,omitempty"`
	Rules          []Rule            `yaml:"rules"`
}

// Rule is one entry in the policy. Evaluated top-to-bottom; first match wins.
type Rule struct {
	ID               string        `yaml:"id"`
	Action           ActionMatcher `yaml:"action"` // single type OR list of types
	Effect           string        `yaml:"effect"` // deny | allow
	Target           string        `yaml:"target,omitempty"`       // substring match on Action.Target
	TargetRegex      string        `yaml:"target_regex,omitempty"` // regex match on Action.Target
	Branches         []string      `yaml:"branches,omitempty"`     // for git.push — match if Action.Target ∈ list
	PathUnder        []string      `yaml:"path_under,omitempty"`   // for file.* — match if Action.Target begins with any
	Reason           string        `yaml:"reason,omitempty"`
	Suggestion       string        `yaml:"suggestion,omitempty"`
	CorrectedCommand string        `yaml:"correctedCommand,omitempty"`
	EscalationWeight int           `yaml:"escalation_weight,omitempty"` // default 1
}

// Bounds are the blast-radius ceilings checked for push-shaped actions.
type Bounds struct {
	MaxFilesChanged   int `yaml:"max_files_changed"`
	MaxLinesChanged   int `yaml:"max_lines_changed"`
	MaxRuntimeSeconds int `yaml:"max_runtime_seconds"`
}

// EscalationConfig overrides the default escalation thresholds.
type EscalationConfig struct {
	ElevatedThreshold  int `yaml:"elevated_threshold"`  // default 3
	HighThreshold      int `yaml:"high_threshold"`      // default 7
	LockdownThreshold  int `yaml:"lockdown_threshold"`  // default 10
	MaxRetriesPerFp    int `yaml:"max_retries_per_action"` // default 3
}

// Decision is the result of evaluating an Action against a Policy.
type Decision struct {
	Allowed          bool   `json:"allowed"`
	Mode             string `json:"mode"` // monitor | enforce | guide
	RuleID           string `json:"rule_id"`
	Reason           string `json:"reason,omitempty"`
	Suggestion       string `json:"suggestion,omitempty"`
	CorrectedCommand string `json:"corrected_command,omitempty"`
	Escalation       string `json:"escalation,omitempty"` // normal | elevated | high | lockdown
	Action           Action `json:"-"`
	Ts               string `json:"ts"`
}

// ActionMatcher is a yaml.Unmarshaler that accepts either a single
// action type string or a list of strings.
type ActionMatcher []string

func (a *ActionMatcher) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind == yaml.ScalarNode {
		*a = []string{node.Value}
		return nil
	}
	if node.Kind == yaml.SequenceNode {
		var list []string
		if err := node.Decode(&list); err != nil {
			return err
		}
		*a = list
		return nil
	}
	return fmt.Errorf("action must be string or list of strings, got %v", node.Kind)
}

// Matches returns true if the given ActionType appears in the matcher.
func (a ActionMatcher) Matches(t ActionType) bool {
	s := string(t)
	for _, v := range a {
		if v == s {
			return true
		}
	}
	return false
}

// LoadPolicyFile reads and parses a single chitin.yaml.
func LoadPolicyFile(path string) (Policy, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Policy{}, fmt.Errorf("read policy: %w", err)
	}
	var p Policy
	if err := yaml.Unmarshal(data, &p); err != nil {
		return Policy{}, fmt.Errorf("parse policy %s: %w", path, err)
	}
	p.ApplyDefaults()
	return p, nil
}

// ApplyDefaults fills in unset fields with their baseline values.
func (p *Policy) ApplyDefaults() {
	if p.Mode == "" {
		p.Mode = "guide"
	}
	if p.Escalation.ElevatedThreshold == 0 {
		p.Escalation.ElevatedThreshold = 3
	}
	if p.Escalation.HighThreshold == 0 {
		p.Escalation.HighThreshold = 7
	}
	if p.Escalation.LockdownThreshold == 0 {
		p.Escalation.LockdownThreshold = 10
	}
	if p.Escalation.MaxRetriesPerFp == 0 {
		p.Escalation.MaxRetriesPerFp = 3
	}
	for i := range p.Rules {
		if p.Rules[i].EscalationWeight == 0 {
			p.Rules[i].EscalationWeight = 1
		}
	}
}

// Evaluate walks the rule list top-to-bottom. First deny match wins;
// otherwise first allow match; otherwise default deny (fail-closed).
func (p Policy) Evaluate(a Action) Decision {
	for _, r := range p.Rules {
		if r.matches(a) {
			mode := p.Mode
			if m, ok := p.InvariantModes[r.ID]; ok {
				mode = m
			}
			return Decision{
				Allowed:          r.Effect == "allow",
				Mode:             mode,
				RuleID:           r.ID,
				Reason:           r.Reason,
				Suggestion:       r.Suggestion,
				CorrectedCommand: r.CorrectedCommand,
				Action:           a,
			}
		}
	}
	// Fail-closed default
	return Decision{
		Allowed: false,
		Mode:    p.Mode,
		RuleID:  "default-deny",
		Reason:  "no matching allow rule; policy default is deny",
		Action:  a,
	}
}

func (r Rule) matches(a Action) bool {
	if !r.Action.Matches(a.Type) {
		return false
	}
	// Branch condition: Action.Target must be in the list
	if len(r.Branches) > 0 {
		inList := false
		for _, b := range r.Branches {
			if a.Target == b {
				inList = true
				break
			}
		}
		if !inList {
			return false
		}
	}
	// PathUnder: Action.Target must begin with one of the prefixes
	if len(r.PathUnder) > 0 {
		under := false
		for _, p := range r.PathUnder {
			if len(a.Target) >= len(p) && a.Target[:len(p)] == p {
				under = true
				break
			}
		}
		if !under {
			return false
		}
	}
	// Target substring
	if r.Target != "" {
		if !containsFold(a.Target, r.Target) {
			return false
		}
	}
	// TargetRegex
	if r.TargetRegex != "" {
		re, err := regexp.Compile(r.TargetRegex)
		if err != nil {
			return false
		}
		if !re.MatchString(a.Target) {
			return false
		}
	}
	return true
}

func containsFold(haystack, needle string) bool {
	return regexp.MustCompile(regexp.QuoteMeta(needle)).MatchString(haystack)
}
```

- [ ] **Step 3.5: Verify tests pass**

```bash
go test ./internal/gov/ -run TestPolicy -v 2>&1 | tail -20
```
Expected: all PASS.

- [ ] **Step 3.6: Commit**

```bash
rtk git add go/execution-kernel/internal/gov/policy.go go/execution-kernel/internal/gov/policy_test.go go/execution-kernel/internal/gov/testdata/
rtk git -c user.email=jpleva91@gmail.com commit -m "gov: policy engine with YAML rules, deny-first semantics, default deny"
```

---

## Task 4: `inherit.go` — LoadWithInheritance

**Files:**
- Create: `go/execution-kernel/internal/gov/inherit.go`
- Create: `go/execution-kernel/internal/gov/inherit_test.go`
- Create: `go/execution-kernel/internal/gov/testdata/policy-strict-parent.yaml`
- Create: `go/execution-kernel/internal/gov/testdata/policy-child-too-loose.yaml`

- [ ] **Step 4.1: Create fixture files**

`testdata/policy-strict-parent.yaml`:
```yaml
id: strict-parent
mode: enforce
rules:
  - id: r1
    action: shell.exec
    effect: deny
    target: "rm"
    reason: "parent deny"
```

`testdata/policy-child-too-loose.yaml`:
```yaml
id: child-loose
mode: monitor
rules:
  - id: r2
    action: file.read
    effect: allow
    reason: "child allow"
```

- [ ] **Step 4.2: Write the failing tests**

Create `go/execution-kernel/internal/gov/inherit_test.go`:

```go
package gov

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := ioutil.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadWithInheritance_WalksParents(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "chitin.yaml"), `
id: root-policy
mode: enforce
rules:
  - id: root-deny
    action: shell.exec
    effect: deny
    target: "rm -rf"
    reason: "root"
`)
	leaf := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(leaf, 0o755); err != nil {
		t.Fatal(err)
	}

	p, sources, err := LoadWithInheritance(leaf)
	if err != nil {
		t.Fatalf("LoadWithInheritance: %v", err)
	}
	if len(sources) != 1 {
		t.Errorf("sources: got %d want 1", len(sources))
	}
	if p.ID != "root-policy" {
		t.Errorf("ID: got %q", p.ID)
	}
}

func TestLoadWithInheritance_ChildOverridesOnID(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "chitin.yaml"), `
id: parent
mode: enforce
rules:
  - id: shared-rule
    action: shell.exec
    effect: deny
    target: "rm"
    reason: "parent"
`)
	child := filepath.Join(root, "sub")
	writeFile(t, filepath.Join(child, "chitin.yaml"), `
id: child
mode: enforce
rules:
  - id: shared-rule
    action: shell.exec
    effect: deny
    target: "rm"
    reason: "child-overridden"
`)
	p, _, err := LoadWithInheritance(child)
	if err != nil {
		t.Fatalf("LoadWithInheritance: %v", err)
	}
	var found *Rule
	for i := range p.Rules {
		if p.Rules[i].ID == "shared-rule" {
			found = &p.Rules[i]
			break
		}
	}
	if found == nil {
		t.Fatal("shared-rule not in merged policy")
	}
	if found.Reason != "child-overridden" {
		t.Errorf("child should override parent on id collision, got reason=%q", found.Reason)
	}
}

func TestLoadWithInheritance_NoPolicyFound(t *testing.T) {
	dir := t.TempDir()
	_, _, err := LoadWithInheritance(dir)
	if err == nil {
		t.Fatal("expected error when no chitin.yaml found up the tree")
	}
}

func TestLoadWithInheritance_MonotonicStrictness(t *testing.T) {
	// Parent is mode:enforce. Child tries mode:monitor.
	// Child CANNOT weaken — merge should reject.
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "chitin.yaml"), `
id: parent
mode: enforce
rules: []
`)
	child := filepath.Join(root, "sub")
	writeFile(t, filepath.Join(child, "chitin.yaml"), `
id: child
mode: monitor
rules: []
`)
	_, _, err := LoadWithInheritance(child)
	if err == nil {
		t.Fatal("child:monitor under parent:enforce should fail strictness check")
	}
}
```

- [ ] **Step 4.3: Verify tests fail**

```bash
go test ./internal/gov/ -run TestLoadWithInheritance -v 2>&1 | tail -5
```
Expected: FAIL with `undefined: LoadWithInheritance`.

- [ ] **Step 4.4: Implement `inherit.go`**

Create `go/execution-kernel/internal/gov/inherit.go`:

```go
package gov

import (
	"fmt"
	"os"
	"path/filepath"
)

// LoadWithInheritance walks from cwd upward until it finds chitin.yaml
// files, loads each, and merges them with child-wins-on-rule-id semantics
// and monotonic-strictness checks (a child cannot loosen a parent's mode).
//
// Returns the merged Policy, the ordered list of source paths that
// contributed (outermost first, innermost last), and an error if no
// policy was found or a strictness violation was detected.
func LoadWithInheritance(cwd string) (Policy, []string, error) {
	abs, err := filepath.Abs(cwd)
	if err != nil {
		return Policy{}, nil, fmt.Errorf("abs: %w", err)
	}

	var paths []string
	dir := abs
	for {
		candidate := filepath.Join(dir, "chitin.yaml")
		if _, err := os.Stat(candidate); err == nil {
			paths = append(paths, candidate)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	if len(paths) == 0 {
		return Policy{}, nil, fmt.Errorf("no_policy_found: no chitin.yaml from %s upward", abs)
	}

	// Reverse paths so outermost (root) is first, innermost (leaf) is last
	// — so child overrides parent on rule-ID collision.
	reverse(paths)

	var merged Policy
	for i, p := range paths {
		loaded, err := LoadPolicyFile(p)
		if err != nil {
			return Policy{}, paths, err
		}
		if i == 0 {
			merged = loaded
			continue
		}
		if err := checkMonotonicStrictness(merged.Mode, loaded.Mode); err != nil {
			return Policy{}, paths, fmt.Errorf("strictness_violation in %s: %w", p, err)
		}
		merged = mergePolicies(merged, loaded)
	}

	merged.ApplyDefaults()
	return merged, paths, nil
}

// checkMonotonicStrictness rejects child weakening parent's mode.
// Strictness ordering: enforce > guide > monitor.
func checkMonotonicStrictness(parentMode, childMode string) error {
	rank := map[string]int{"monitor": 0, "guide": 1, "enforce": 2, "": 1}
	if rank[childMode] < rank[parentMode] {
		return fmt.Errorf("child mode=%q cannot weaken parent mode=%q", childMode, parentMode)
	}
	return nil
}

// mergePolicies merges child over parent with child-wins-on-rule-id.
// Bounds / InvariantModes merge additively; child overrides on key collision.
func mergePolicies(parent, child Policy) Policy {
	out := parent
	if child.ID != "" {
		out.ID = child.ID
	}
	if child.Mode != "" {
		out.Mode = child.Mode
	}
	if child.Bounds.MaxFilesChanged > 0 {
		out.Bounds.MaxFilesChanged = child.Bounds.MaxFilesChanged
	}
	if child.Bounds.MaxLinesChanged > 0 {
		out.Bounds.MaxLinesChanged = child.Bounds.MaxLinesChanged
	}
	if child.Bounds.MaxRuntimeSeconds > 0 {
		out.Bounds.MaxRuntimeSeconds = child.Bounds.MaxRuntimeSeconds
	}
	if out.InvariantModes == nil {
		out.InvariantModes = make(map[string]string)
	}
	for k, v := range child.InvariantModes {
		out.InvariantModes[k] = v
	}

	// Child rules: override parent by ID, append new ones.
	parentByID := make(map[string]int, len(out.Rules))
	for i, r := range out.Rules {
		parentByID[r.ID] = i
	}
	for _, r := range child.Rules {
		if idx, ok := parentByID[r.ID]; ok {
			out.Rules[idx] = r
		} else {
			out.Rules = append(out.Rules, r)
		}
	}
	return out
}

func reverse(s []string) {
	for i, j := 0, len(s)-1; i < j; i, j = i+1, j-1 {
		s[i], s[j] = s[j], s[i]
	}
}
```

- [ ] **Step 4.5: Verify tests pass**

```bash
go test ./internal/gov/ -run TestLoadWithInheritance -v 2>&1 | tail -15
```
Expected: all PASS.

- [ ] **Step 4.6: Commit**

```bash
rtk git add go/execution-kernel/internal/gov/inherit.go go/execution-kernel/internal/gov/inherit_test.go go/execution-kernel/internal/gov/testdata/policy-strict-parent.yaml go/execution-kernel/internal/gov/testdata/policy-child-too-loose.yaml
rtk git -c user.email=jpleva91@gmail.com commit -m "gov: inheritance-aware policy loader with monotonic-strictness"
```

---

## Task 5: `bounds.go` — blast-radius check via `git diff`

**Files:**
- Create: `go/execution-kernel/internal/gov/bounds.go`
- Create: `go/execution-kernel/internal/gov/bounds_test.go`

- [ ] **Step 5.1: Write the failing tests**

Create `go/execution-kernel/internal/gov/bounds_test.go`:

```go
package gov

import "testing"

func TestBounds_NonPushShapedSkips(t *testing.T) {
	// Bounds only fires for git.push / github.pr.create.
	// Other actions return no-op (allow, empty rule).
	p := Policy{Bounds: Bounds{MaxFilesChanged: 1, MaxLinesChanged: 1}}
	d := CheckBounds(Action{Type: ActFileRead, Target: "/tmp/x"}, p, ".")
	if !d.Allowed {
		t.Errorf("non-push-shaped action should pass bounds, got %+v", d)
	}
}

func TestBounds_ParseStatLine(t *testing.T) {
	cases := []struct {
		line     string
		wantF    int
		wantIns  int
		wantDel  int
	}{
		{" 3 files changed, 10 insertions(+), 5 deletions(-)", 3, 10, 5},
		{" 1 file changed, 1 insertion(+)", 1, 1, 0},
		{" 60 files changed, 42 insertions(+), 8874 deletions(-)", 60, 42, 8874},
		{" 2 files changed, 0 insertions(+), 7 deletions(-)", 2, 0, 7},
	}
	for _, c := range cases {
		f, ins, del := parseDiffStatLine(c.line)
		if f != c.wantF || ins != c.wantIns || del != c.wantDel {
			t.Errorf("parseDiffStatLine(%q) = (%d,%d,%d) want (%d,%d,%d)",
				c.line, f, ins, del, c.wantF, c.wantIns, c.wantDel)
		}
	}
}

func TestBounds_ParseStatLine_Empty(t *testing.T) {
	f, ins, del := parseDiffStatLine("")
	if f != 0 || ins != 0 || del != 0 {
		t.Errorf("empty should parse to zeros, got (%d,%d,%d)", f, ins, del)
	}
}

func TestBounds_OverFiles(t *testing.T) {
	p := Policy{Bounds: Bounds{MaxFilesChanged: 10, MaxLinesChanged: 1000}}
	d := evaluateBoundsFromStats(Action{Type: ActGitPush, Target: "fix"}, p, 20, 100, 100)
	if d.Allowed {
		t.Errorf("20 files > 10 should reject")
	}
	if d.RuleID != "bounds:max_files_changed" {
		t.Errorf("RuleID: got %q", d.RuleID)
	}
	if d.Mode != "enforce" {
		t.Errorf("bounds must always be enforce, got %q", d.Mode)
	}
}

func TestBounds_OverLines(t *testing.T) {
	p := Policy{Bounds: Bounds{MaxFilesChanged: 100, MaxLinesChanged: 50}}
	d := evaluateBoundsFromStats(Action{Type: ActGitPush, Target: "fix"}, p, 5, 40, 40)
	if d.Allowed {
		t.Errorf("80 total lines > 50 should reject")
	}
	if d.RuleID != "bounds:max_lines_changed" {
		t.Errorf("RuleID: got %q", d.RuleID)
	}
}

func TestBounds_WithinCeilings(t *testing.T) {
	p := Policy{Bounds: Bounds{MaxFilesChanged: 25, MaxLinesChanged: 500}}
	d := evaluateBoundsFromStats(Action{Type: ActGitPush, Target: "fix"}, p, 3, 10, 5)
	if !d.Allowed {
		t.Errorf("3 files / 15 lines should pass, got %+v", d)
	}
}

func TestBounds_NoCeiling(t *testing.T) {
	// Bounds with all zeros should be a no-op (no ceilings set).
	p := Policy{Bounds: Bounds{}}
	d := evaluateBoundsFromStats(Action{Type: ActGitPush, Target: "fix"}, p, 1000, 100000, 100000)
	if !d.Allowed {
		t.Errorf("zero bounds should be no-op, got %+v", d)
	}
}
```

- [ ] **Step 5.2: Verify tests fail**

```bash
go test ./internal/gov/ -run TestBounds -v 2>&1 | tail -5
```
Expected: FAIL with `undefined: CheckBounds, parseDiffStatLine, evaluateBoundsFromStats`.

- [ ] **Step 5.3: Implement `bounds.go`**

Create `go/execution-kernel/internal/gov/bounds.go`:

```go
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
```

- [ ] **Step 5.4: Verify tests pass**

```bash
go test ./internal/gov/ -run TestBounds -v 2>&1 | tail -15
```
Expected: all PASS.

- [ ] **Step 5.5: Commit**

```bash
rtk git add go/execution-kernel/internal/gov/bounds.go go/execution-kernel/internal/gov/bounds_test.go
rtk git -c user.email=jpleva91@gmail.com commit -m "gov: bounds check for push-shaped actions via git diff --stat"
```

---

## Task 6: `escalation.go` — SQLite-backed denial counter

**Files:**
- Create: `go/execution-kernel/internal/gov/escalation.go`
- Create: `go/execution-kernel/internal/gov/escalation_test.go`

- [ ] **Step 6.1: Write the failing tests**

Create `go/execution-kernel/internal/gov/escalation_test.go`:

```go
package gov

import (
	"path/filepath"
	"testing"
)

func newTestCounter(t *testing.T) *Counter {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "gov.db")
	c, err := OpenCounter(dbPath)
	if err != nil {
		t.Fatalf("OpenCounter: %v", err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

func TestCounter_LadderNormal(t *testing.T) {
	c := newTestCounter(t)
	for i := 0; i < 2; i++ {
		c.RecordDenial("agent1", "fp1", 1)
	}
	if lv := c.Level("agent1"); lv != "normal" {
		t.Errorf("after 2 denials, level=%q want normal", lv)
	}
}

func TestCounter_LadderElevated(t *testing.T) {
	c := newTestCounter(t)
	for i := 0; i < 3; i++ {
		c.RecordDenial("agent1", "fp1", 1)
	}
	if lv := c.Level("agent1"); lv != "elevated" {
		t.Errorf("after 3 denials, level=%q want elevated", lv)
	}
}

func TestCounter_LadderHigh(t *testing.T) {
	c := newTestCounter(t)
	for i := 0; i < 7; i++ {
		c.RecordDenial("agent1", "fp1", 1)
	}
	if lv := c.Level("agent1"); lv != "high" {
		t.Errorf("after 7 denials, level=%q want high", lv)
	}
}

func TestCounter_Lockdown(t *testing.T) {
	c := newTestCounter(t)
	for i := 0; i < 10; i++ {
		c.RecordDenial("agent1", "fp1", 1)
	}
	if !c.IsLocked("agent1") {
		t.Errorf("10 denials should trigger lockdown")
	}
	if lv := c.Level("agent1"); lv != "lockdown" {
		t.Errorf("level: got %q want lockdown", lv)
	}
}

func TestCounter_WeightedDenial(t *testing.T) {
	c := newTestCounter(t)
	// Self-modification rule has weight=2. Three such denials = count 6 → elevated.
	c.RecordDenial("agent1", "fp-self-mod", 2)
	c.RecordDenial("agent1", "fp-self-mod", 2)
	if lv := c.Level("agent1"); lv != "elevated" {
		t.Errorf("after 2 weighted-2 denials (count=4), level=%q want elevated", lv)
	}
}

func TestCounter_PerAgentIsolation(t *testing.T) {
	c := newTestCounter(t)
	for i := 0; i < 10; i++ {
		c.RecordDenial("agent1", "fp1", 1)
	}
	if c.IsLocked("agent2") {
		t.Errorf("agent2 should not be locked when only agent1 has denials")
	}
}

func TestCounter_Reset(t *testing.T) {
	c := newTestCounter(t)
	for i := 0; i < 10; i++ {
		c.RecordDenial("agent1", "fp1", 1)
	}
	c.Reset("agent1")
	if c.IsLocked("agent1") {
		t.Errorf("Reset should unlock")
	}
	if lv := c.Level("agent1"); lv != "normal" {
		t.Errorf("after Reset, level=%q want normal", lv)
	}
}

func TestCounter_ManualLockdown(t *testing.T) {
	c := newTestCounter(t)
	c.Lockdown("agent1")
	if !c.IsLocked("agent1") {
		t.Errorf("Lockdown should immediately lock")
	}
}

func TestCounter_PersistsAcrossReopen(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "gov.db")
	c1, _ := OpenCounter(dbPath)
	for i := 0; i < 10; i++ {
		c1.RecordDenial("agent1", "fp1", 1)
	}
	c1.Close()

	c2, err := OpenCounter(dbPath)
	if err != nil {
		t.Fatalf("re-open: %v", err)
	}
	defer c2.Close()
	if !c2.IsLocked("agent1") {
		t.Errorf("lockdown should persist across Close/Open")
	}
}
```

- [ ] **Step 6.2: Verify tests fail**

```bash
go test ./internal/gov/ -run TestCounter -v 2>&1 | tail -5
```
Expected: FAIL with `undefined: Counter, OpenCounter`.

- [ ] **Step 6.3: Implement `escalation.go`**

Create `go/execution-kernel/internal/gov/escalation.go`:

```go
package gov

import (
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

// Counter tracks per-agent escalation state backed by SQLite.
// Key invariants:
//   - Lockdown is sticky across sessions (survives Close/Open).
//   - Counter keyed on (agent, action_fp); total denials per agent drive
//     the level ladder.
//   - Weighted denials (e.g. self-modification) bump total by >1.
type Counter struct {
	db *sql.DB
}

// OpenCounter opens/creates the SQLite DB at dbPath with WAL mode.
func OpenCounter(dbPath string) (*Counter, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	if _, err := db.Exec(`PRAGMA journal_mode=WAL`); err != nil {
		return nil, fmt.Errorf("enable WAL: %w", err)
	}
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS denials (
			agent TEXT NOT NULL,
			action_fp TEXT NOT NULL,
			count INTEGER NOT NULL DEFAULT 0,
			first_ts TEXT NOT NULL,
			last_ts TEXT NOT NULL,
			PRIMARY KEY (agent, action_fp)
		);
		CREATE TABLE IF NOT EXISTS agent_state (
			agent TEXT PRIMARY KEY,
			total INTEGER NOT NULL DEFAULT 0,
			locked INTEGER NOT NULL DEFAULT 0,
			locked_ts TEXT
		);
	`); err != nil {
		return nil, fmt.Errorf("create schema: %w", err)
	}
	return &Counter{db: db}, nil
}

// Close the underlying DB.
func (c *Counter) Close() error {
	return c.db.Close()
}

// RecordDenial increments counters for (agent, fp) by weight. If total
// denials reach the lockdown threshold (10), marks the agent locked.
func (c *Counter) RecordDenial(agent, fp string, weight int) {
	if weight <= 0 {
		weight = 1
	}
	now := time.Now().UTC().Format(time.RFC3339)

	tx, err := c.db.Begin()
	if err != nil {
		return
	}
	defer tx.Rollback()

	_, _ = tx.Exec(`
		INSERT INTO denials (agent, action_fp, count, first_ts, last_ts)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(agent, action_fp) DO UPDATE SET
			count = count + excluded.count,
			last_ts = excluded.last_ts
	`, agent, fp, weight, now, now)

	_, _ = tx.Exec(`
		INSERT INTO agent_state (agent, total, locked)
		VALUES (?, ?, 0)
		ON CONFLICT(agent) DO UPDATE SET total = total + excluded.total
	`, agent, weight)

	var total int
	_ = tx.QueryRow(`SELECT total FROM agent_state WHERE agent = ?`, agent).Scan(&total)
	if total >= 10 {
		_, _ = tx.Exec(`UPDATE agent_state SET locked = 1, locked_ts = ? WHERE agent = ?`, now, agent)
	}

	_ = tx.Commit()
}

// Level returns the escalation level for an agent: normal | elevated |
// high | lockdown. Thresholds are hard-coded for v1; config-driven
// thresholds are a v2 concern.
func (c *Counter) Level(agent string) string {
	var total int
	var locked int
	_ = c.db.QueryRow(`SELECT total, locked FROM agent_state WHERE agent = ?`, agent).
		Scan(&total, &locked)
	if locked == 1 {
		return "lockdown"
	}
	switch {
	case total >= 10:
		return "lockdown"
	case total >= 7:
		return "high"
	case total >= 3:
		return "elevated"
	default:
		return "normal"
	}
}

// IsLocked returns true if the agent is in lockdown.
func (c *Counter) IsLocked(agent string) bool {
	return c.Level(agent) == "lockdown"
}

// Lockdown forces an agent into lockdown immediately (operator kill-switch).
func (c *Counter) Lockdown(agent string) {
	now := time.Now().UTC().Format(time.RFC3339)
	_, _ = c.db.Exec(`
		INSERT INTO agent_state (agent, total, locked, locked_ts)
		VALUES (?, 10, 1, ?)
		ON CONFLICT(agent) DO UPDATE SET locked = 1, locked_ts = excluded.locked_ts
	`, agent, now)
}

// Reset clears all denial counters and the locked flag for an agent.
func (c *Counter) Reset(agent string) {
	_, _ = c.db.Exec(`DELETE FROM denials WHERE agent = ?`, agent)
	_, _ = c.db.Exec(`DELETE FROM agent_state WHERE agent = ?`, agent)
}
```

- [ ] **Step 6.4: Verify tests pass**

```bash
go test ./internal/gov/ -run TestCounter -v 2>&1 | tail -20
```
Expected: all PASS.

- [ ] **Step 6.5: Commit**

```bash
rtk git add go/execution-kernel/internal/gov/escalation.go go/execution-kernel/internal/gov/escalation_test.go
rtk git -c user.email=jpleva91@gmail.com commit -m "gov: SQLite-backed escalation counter with ladder + lockdown"
```

---

## Task 7: `decision.go` — Decision JSONL writer

**Files:**
- Create: `go/execution-kernel/internal/gov/decision.go`
- Create: `go/execution-kernel/internal/gov/decision_test.go`

- [ ] **Step 7.1: Write the failing tests**

Create `go/execution-kernel/internal/gov/decision_test.go`:

```go
package gov

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteLog_AppendsOneJSONLine(t *testing.T) {
	dir := t.TempDir()
	d := Decision{
		Allowed: false, Mode: "guide", RuleID: "no-rm",
		Reason: "no rm", Ts: "2026-04-22T00:00:00Z",
	}
	if err := WriteLog(d, dir); err != nil {
		t.Fatalf("WriteLog: %v", err)
	}

	// Find the file
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("expected 1 log file, got %d", len(entries))
	}
	path := filepath.Join(dir, entries[0].Name())
	f, _ := os.Open(path)
	defer f.Close()
	sc := bufio.NewScanner(f)
	lines := 0
	for sc.Scan() {
		lines++
		var got Decision
		if err := json.Unmarshal(sc.Bytes(), &got); err != nil {
			t.Errorf("line is not valid JSON: %v", err)
		}
		if got.RuleID != "no-rm" {
			t.Errorf("RuleID roundtrip: got %q", got.RuleID)
		}
	}
	if lines != 1 {
		t.Errorf("expected 1 line, got %d", lines)
	}
}

func TestWriteLog_MultipleAppends(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 5; i++ {
		_ = WriteLog(Decision{
			Allowed: true, Mode: "monitor", RuleID: "x",
			Ts: "2026-04-22T00:00:00Z",
		}, dir)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("should still be 1 file")
	}
	path := filepath.Join(dir, entries[0].Name())
	f, _ := os.Open(path)
	defer f.Close()
	sc := bufio.NewScanner(f)
	lines := 0
	for sc.Scan() {
		lines++
	}
	if lines != 5 {
		t.Errorf("expected 5 lines, got %d", lines)
	}
}
```

- [ ] **Step 7.2: Verify tests fail**

```bash
go test ./internal/gov/ -run TestWriteLog -v 2>&1 | tail -5
```
Expected: FAIL with `undefined: WriteLog`.

- [ ] **Step 7.3: Implement `decision.go`**

Create `go/execution-kernel/internal/gov/decision.go`:

```go
package gov

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// WriteLog appends a Decision as one JSON line to
// <dir>/gov-decisions-<utc-date>.jsonl. Daily-rotated; append-only.
// Tolerates ENOSPC (logs to stderr, drops the line, returns nil).
func WriteLog(d Decision, dir string) error {
	if d.Ts == "" {
		d.Ts = time.Now().UTC().Format(time.RFC3339)
	}
	date := strings.Split(d.Ts, "T")[0]
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir log dir: %w", err)
	}
	path := filepath.Join(dir, "gov-decisions-"+date+".jsonl")

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open log: %w", err)
	}
	defer f.Close()

	line, err := json.Marshal(struct {
		Allowed          bool   `json:"allowed"`
		Mode             string `json:"mode"`
		RuleID           string `json:"rule_id"`
		Reason           string `json:"reason,omitempty"`
		Suggestion       string `json:"suggestion,omitempty"`
		CorrectedCommand string `json:"corrected_command,omitempty"`
		Escalation       string `json:"escalation,omitempty"`
		ActionType       string `json:"action_type"`
		ActionTarget     string `json:"action_target"`
		Ts               string `json:"ts"`
	}{
		Allowed: d.Allowed, Mode: d.Mode, RuleID: d.RuleID,
		Reason: d.Reason, Suggestion: d.Suggestion,
		CorrectedCommand: d.CorrectedCommand, Escalation: d.Escalation,
		ActionType: string(d.Action.Type), ActionTarget: d.Action.Target,
		Ts: d.Ts,
	})
	if err != nil {
		return fmt.Errorf("marshal decision: %w", err)
	}
	if _, err := f.Write(append(line, '\n')); err != nil {
		// Best-effort on ENOSPC — log once to stderr, don't fail the gate call
		fmt.Fprintf(os.Stderr, "gov: decision log write failed: %v\n", err)
		return nil
	}
	return nil
}
```

- [ ] **Step 7.4: Verify tests pass**

```bash
go test ./internal/gov/ -run TestWriteLog -v 2>&1 | tail -10
```
Expected: all PASS.

- [ ] **Step 7.5: Commit**

```bash
rtk git add go/execution-kernel/internal/gov/decision.go go/execution-kernel/internal/gov/decision_test.go
rtk git -c user.email=jpleva91@gmail.com commit -m "gov: decision JSONL log writer (daily-rotated, append-only)"
```

---

## Task 8: `gate.go` — orchestrator

**Files:**
- Create: `go/execution-kernel/internal/gov/gate.go`
- Create: `go/execution-kernel/internal/gov/gate_test.go`

- [ ] **Step 8.1: Write the failing tests**

Create `go/execution-kernel/internal/gov/gate_test.go`:

```go
package gov

import (
	"os"
	"path/filepath"
	"testing"
)

func newTestGate(t *testing.T) (*Gate, string) {
	t.Helper()
	dir := t.TempDir()
	policyPath := filepath.Join(dir, "chitin.yaml")
	_ = os.WriteFile(policyPath, []byte(`
id: test
mode: guide
rules:
  - id: no-rm
    action: shell.exec
    effect: deny
    target: "rm -rf"
    reason: "no rm"
    suggestion: "use git rm"
    correctedCommand: "git rm <files>"
  - id: allow-read
    action: file.read
    effect: allow
    reason: "reads ok"
`), 0o644)
	pol, _, err := LoadWithInheritance(dir)
	if err != nil {
		t.Fatalf("load policy: %v", err)
	}
	logDir := filepath.Join(dir, "decisions")
	dbPath := filepath.Join(dir, "gov.db")
	counter, err := OpenCounter(dbPath)
	if err != nil {
		t.Fatalf("OpenCounter: %v", err)
	}
	t.Cleanup(func() { counter.Close() })
	return &Gate{Policy: pol, Counter: counter, LogDir: logDir, Cwd: dir}, dir
}

func TestGate_AllowsReadAction(t *testing.T) {
	g, _ := newTestGate(t)
	d := g.Evaluate(Action{Type: ActFileRead, Target: "/tmp/x"}, "agent1")
	if !d.Allowed {
		t.Errorf("file.read should be allowed, got %+v", d)
	}
}

func TestGate_DeniesRmRfAndLogs(t *testing.T) {
	g, dir := newTestGate(t)
	d := g.Evaluate(Action{Type: ActShellExec, Target: "rm -rf go/"}, "agent1")
	if d.Allowed {
		t.Errorf("rm -rf should be denied")
	}
	if d.RuleID != "no-rm" {
		t.Errorf("RuleID: got %q", d.RuleID)
	}
	if d.Escalation != "normal" {
		t.Errorf("Escalation: got %q want normal (1st denial)", d.Escalation)
	}

	// Log file should exist
	logDir := filepath.Join(dir, "decisions")
	entries, _ := os.ReadDir(logDir)
	if len(entries) != 1 {
		t.Errorf("expected 1 log file, got %d", len(entries))
	}
}

func TestGate_EscalationRecorded(t *testing.T) {
	g, _ := newTestGate(t)
	for i := 0; i < 3; i++ {
		g.Evaluate(Action{Type: ActShellExec, Target: "rm -rf go/"}, "agent1")
	}
	if lv := g.Counter.Level("agent1"); lv != "elevated" {
		t.Errorf("after 3 denials, level=%q want elevated", lv)
	}
}

func TestGate_LockdownDeniesEverything(t *testing.T) {
	g, _ := newTestGate(t)
	g.Counter.Lockdown("agent1")
	// file.read would normally be allowed — but lockdown overrides
	d := g.Evaluate(Action{Type: ActFileRead, Target: "/tmp/x"}, "agent1")
	if d.Allowed {
		t.Errorf("agent in lockdown must be denied regardless of rule")
	}
	if d.RuleID != "lockdown" {
		t.Errorf("RuleID: got %q want lockdown", d.RuleID)
	}
}

func TestGate_MonitorModeAllowsButLogs(t *testing.T) {
	g, _ := newTestGate(t)
	// Override policy to monitor mode
	g.Policy.Mode = "monitor"
	g.Policy.InvariantModes = nil
	d := g.Evaluate(Action{Type: ActShellExec, Target: "rm -rf go/"}, "agent1")
	if !d.Allowed {
		t.Errorf("monitor mode should allow (log-only), got denied: %+v", d)
	}
	if d.Mode != "monitor" {
		t.Errorf("Mode: got %q want monitor", d.Mode)
	}
}
```

- [ ] **Step 8.2: Verify tests fail**

```bash
go test ./internal/gov/ -run TestGate -v 2>&1 | tail -5
```
Expected: FAIL with `undefined: Gate`.

- [ ] **Step 8.3: Implement `gate.go`**

Create `go/execution-kernel/internal/gov/gate.go`:

```go
package gov

import "time"

// Gate orchestrates policy evaluation, bounds check, escalation counting,
// and decision logging. One instance per gate subprocess invocation.
type Gate struct {
	Policy  Policy
	Counter *Counter
	LogDir  string
	Cwd     string
}

// Evaluate is the single entry point: normalize-already-done Action →
// Decision, with side effects (counter increment on deny, log append).
//
// Sequence:
//  1. Lockdown short-circuit — if agent is locked, deny immediately.
//  2. Policy evaluation (rule matching).
//  3. Bounds check (only for push-shaped actions; skipped otherwise).
//  4. Counter increment if denied.
//  5. Decision log append (deny OR allow — decisions are all audit data).
func (g *Gate) Evaluate(a Action, agent string) Decision {
	now := time.Now().UTC().Format(time.RFC3339)

	// 1. Lockdown takes precedence over any rule.
	if g.Counter != nil && g.Counter.IsLocked(agent) {
		d := Decision{
			Allowed: false, Mode: "enforce", RuleID: "lockdown",
			Reason: "agent in lockdown — contact operator",
			Escalation: "lockdown", Action: a, Ts: now,
		}
		_ = WriteLog(d, g.LogDir)
		return d
	}

	// 2. Policy evaluate.
	d := g.Policy.Evaluate(a)
	d.Ts = now

	// 3. Bounds — only for push-shaped when policy allows the action so far.
	if d.Allowed && (a.Type == ActGitPush || a.Type == ActGithubPRCreate) {
		bd := CheckBounds(a, g.Policy, g.Cwd)
		if !bd.Allowed {
			d = bd
			d.Ts = now
		}
	}

	// 4. Counter on deny. Allow policy override of weight via rule
	// (read from matched Rule — not wired explicitly for v1).
	weight := 1
	for _, r := range g.Policy.Rules {
		if r.ID == d.RuleID && r.EscalationWeight > 0 {
			weight = r.EscalationWeight
			break
		}
	}
	if !d.Allowed && g.Counter != nil {
		g.Counter.RecordDenial(agent, a.Fingerprint(), weight)
		d.Escalation = g.Counter.Level(agent)
	} else if g.Counter != nil {
		d.Escalation = g.Counter.Level(agent)
	}

	// 5. Log.
	_ = WriteLog(d, g.LogDir)

	return d
}
```

- [ ] **Step 8.4: Verify tests pass**

```bash
go test ./internal/gov/ -run TestGate -v 2>&1 | tail -15
```
Expected: all PASS.

- [ ] **Step 8.5: Commit**

```bash
rtk git add go/execution-kernel/internal/gov/gate.go go/execution-kernel/internal/gov/gate_test.go
rtk git -c user.email=jpleva91@gmail.com commit -m "gov: gate orchestrator — policy + bounds + counter + log"
```

---

## Task 9: Integration test — full pipeline end-to-end

**Files:**
- Create: `go/execution-kernel/internal/gov/integration_test.go`

- [ ] **Step 9.1: Write integration tests**

Create `go/execution-kernel/internal/gov/integration_test.go`:

```go
package gov

import (
	"os"
	"path/filepath"
	"testing"
)

func newIntegrationGate(t *testing.T, policyYAML string) (*Gate, string) {
	t.Helper()
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "chitin.yaml"), []byte(policyYAML), 0o644)
	pol, _, err := LoadWithInheritance(dir)
	if err != nil {
		t.Fatalf("LoadWithInheritance: %v", err)
	}
	counter, err := OpenCounter(filepath.Join(dir, "gov.db"))
	if err != nil {
		t.Fatalf("OpenCounter: %v", err)
	}
	t.Cleanup(func() { counter.Close() })
	return &Gate{
		Policy: pol, Counter: counter,
		LogDir: filepath.Join(dir, "decisions"), Cwd: dir,
	}, dir
}

// Flow A from spec §Data-flow: terminal rm -rf is denied.
func TestIntegration_FlowA_DangerousShell(t *testing.T) {
	g, _ := newIntegrationGate(t, `
id: test
mode: guide
rules:
  - id: no-destructive-rm
    action: shell.exec
    effect: deny
    target: "rm -rf"
    reason: "blocked"
    suggestion: "use targeted"
    correctedCommand: "git rm"
`)
	a, _ := Normalize("terminal", map[string]any{"command": "rm -rf go/"})
	d := g.Evaluate(a, "hermes")
	if d.Allowed {
		t.Fatalf("expected deny, got %+v", d)
	}
	if d.RuleID != "no-destructive-rm" {
		t.Errorf("RuleID: got %q", d.RuleID)
	}
}

// Flow B: execute_code subprocess.run bypass produces the same denial.
func TestIntegration_FlowB_BypassClosure(t *testing.T) {
	g, _ := newIntegrationGate(t, `
id: test
mode: guide
rules:
  - id: no-destructive-rm
    action: shell.exec
    effect: deny
    target: "rm -rf"
    reason: "blocked"
`)
	// Via terminal
	aTerm, _ := Normalize("terminal", map[string]any{"command": "rm -rf go/"})
	dTerm := g.Evaluate(aTerm, "hermes")

	// Via execute_code subprocess
	aExec, _ := Normalize("execute_code", map[string]any{
		"code": `import subprocess
subprocess.run(["rm", "-rf", "go/"])`,
	})
	dExec := g.Evaluate(aExec, "hermes")

	if dTerm.Allowed || dExec.Allowed {
		t.Fatalf("both should be denied; got term=%+v exec=%+v", dTerm, dExec)
	}
	if dTerm.RuleID != dExec.RuleID {
		t.Errorf("bypass closure failed: terminal denied by %q, execute_code by %q",
			dTerm.RuleID, dExec.RuleID)
	}
	if aTerm.Fingerprint() != aExec.Fingerprint() {
		t.Errorf("fingerprints differ — escalation counter won't link them")
	}
}

// Flow E: escalation counter increments, reaches lockdown.
func TestIntegration_FlowE_EscalationLadder(t *testing.T) {
	g, _ := newIntegrationGate(t, `
id: test
mode: guide
rules:
  - id: no-rm
    action: shell.exec
    effect: deny
    target: "rm -rf"
    reason: "blocked"
`)
	a, _ := Normalize("terminal", map[string]any{"command": "rm -rf go/"})
	for i := 0; i < 10; i++ {
		_ = g.Evaluate(a, "hermes")
	}
	if lv := g.Counter.Level("hermes"); lv != "lockdown" {
		t.Fatalf("after 10 denials, level=%q want lockdown", lv)
	}
	// Once in lockdown, even allowed actions are denied.
	okA, _ := Normalize("read_file", map[string]any{"path": "README.md"})
	dOK := g.Evaluate(okA, "hermes")
	if dOK.Allowed {
		t.Errorf("locked agent should be denied even for allow-shaped actions")
	}
	if dOK.RuleID != "lockdown" {
		t.Errorf("RuleID: got %q want lockdown", dOK.RuleID)
	}
}

func TestIntegration_NoPolicyFails(t *testing.T) {
	dir := t.TempDir()
	// no chitin.yaml anywhere
	_, _, err := LoadWithInheritance(dir)
	if err == nil {
		t.Fatal("expected no_policy_found error")
	}
}
```

- [ ] **Step 9.2: Run all tests — verify everything still green**

```bash
go test ./internal/gov/ -v 2>&1 | tail -20
```
Expected: all PASS (~30–40 tests).

- [ ] **Step 9.3: Commit**

```bash
rtk git add go/execution-kernel/internal/gov/integration_test.go
rtk git -c user.email=jpleva91@gmail.com commit -m "gov: integration tests — flows A, B, E from spec + no-policy-fail"
```

---

## Task 10: Wire `gate` subcommand into `chitin-kernel`

**Files:**
- Modify: `go/execution-kernel/cmd/chitin-kernel/main.go`

- [ ] **Step 10.1: Add `gate` to dispatch switch**

In `go/execution-kernel/cmd/chitin-kernel/main.go`, add after `case "health":`:

```go
	case "gate":
		cmdGate(args)
```

Also add to the imports block:

```go
	"github.com/chitinhq/chitin/go/execution-kernel/internal/gov"
```

- [ ] **Step 10.2: Implement `cmdGate`**

Append to `go/execution-kernel/cmd/chitin-kernel/main.go`:

```go
// cmdGate dispatches subcommands: evaluate, status, lockdown, reset.
//
// evaluate: --tool <name> --args-json <json> --agent <name> [--cwd <path>]
//   Stdout: Decision JSON. Exit 0=allow, 1=deny, 2=internal error.
//
// status:   --cwd <path> --agent <name>
// lockdown: --agent <name>
// reset:    --agent <name>
func cmdGate(args []string) {
	if len(args) < 1 {
		exitErr("gate_no_subcommand", "usage: chitin-kernel gate {evaluate|status|lockdown|reset} [flags]")
	}
	sub := args[0]
	subArgs := args[1:]
	switch sub {
	case "evaluate":
		cmdGateEvaluate(subArgs)
	case "status":
		cmdGateStatus(subArgs)
	case "lockdown":
		cmdGateLockdown(subArgs)
	case "reset":
		cmdGateReset(subArgs)
	default:
		exitErr("gate_unknown_subcommand", sub)
	}
}

func cmdGateEvaluate(args []string) {
	fs := flag.NewFlagSet("gate evaluate", flag.ExitOnError)
	tool := fs.String("tool", "", "tool name (e.g. terminal, write_file)")
	argsJSON := fs.String("args-json", "{}", "tool args as JSON")
	agent := fs.String("agent", "", "agent identifier (e.g. hermes)")
	cwd := fs.String("cwd", ".", "cwd the action would execute against")
	fs.Parse(args)

	if *tool == "" || *agent == "" {
		exitErr("gate_missing_args", "--tool and --agent required")
	}

	var argsMap map[string]any
	if err := json.Unmarshal([]byte(*argsJSON), &argsMap); err != nil {
		exitErr("gate_bad_args_json", err.Error())
	}

	action, err := gov.Normalize(*tool, argsMap)
	if err != nil {
		exitErr("gate_normalize", err.Error())
	}
	action.Path = *cwd

	absCwd, _ := filepath.Abs(*cwd)
	policy, _, err := gov.LoadWithInheritance(absCwd)
	if err != nil {
		// No policy or parse error → fail-closed deny with a structured Decision.
		out := map[string]any{
			"allowed": false, "mode": "enforce", "rule_id": "no_policy_found",
			"reason": err.Error(), "action_type": string(action.Type),
			"action_target": action.Target,
		}
		b, _ := json.Marshal(out)
		fmt.Println(string(b))
		os.Exit(1)
	}

	home, _ := os.UserHomeDir()
	chitinDir := filepath.Join(home, ".chitin")
	_ = os.MkdirAll(chitinDir, 0o755)
	counter, err := gov.OpenCounter(filepath.Join(chitinDir, "gov.db"))
	if err != nil {
		exitErr("gate_counter", err.Error())
	}
	defer counter.Close()

	gate := &gov.Gate{
		Policy: policy, Counter: counter,
		LogDir: chitinDir, Cwd: absCwd,
	}
	d := gate.Evaluate(action, *agent)

	b, _ := json.Marshal(d)
	fmt.Println(string(b))
	if d.Allowed {
		os.Exit(0)
	}
	os.Exit(1)
}

func cmdGateStatus(args []string) {
	fs := flag.NewFlagSet("gate status", flag.ExitOnError)
	cwd := fs.String("cwd", ".", "cwd to load policy from")
	agent := fs.String("agent", "", "agent to report state for")
	fs.Parse(args)

	absCwd, _ := filepath.Abs(*cwd)
	policy, sources, err := gov.LoadWithInheritance(absCwd)
	if err != nil {
		exitErr("status_load_policy", err.Error())
	}

	home, _ := os.UserHomeDir()
	counter, err := gov.OpenCounter(filepath.Join(home, ".chitin", "gov.db"))
	if err != nil {
		exitErr("status_counter", err.Error())
	}
	defer counter.Close()

	level := "unset"
	locked := false
	if *agent != "" {
		level = counter.Level(*agent)
		locked = counter.IsLocked(*agent)
	}

	out := map[string]any{
		"policy_id": policy.ID, "mode": policy.Mode,
		"policy_sources": sources,
		"rules_count": len(policy.Rules),
		"agent": *agent, "level": level, "locked": locked,
	}
	b, _ := json.Marshal(out)
	fmt.Println(string(b))
}

func cmdGateLockdown(args []string) {
	fs := flag.NewFlagSet("gate lockdown", flag.ExitOnError)
	agent := fs.String("agent", "", "agent to lock down")
	fs.Parse(args)
	if *agent == "" {
		exitErr("lockdown_missing_agent", "--agent required")
	}
	home, _ := os.UserHomeDir()
	counter, _ := gov.OpenCounter(filepath.Join(home, ".chitin", "gov.db"))
	defer counter.Close()
	counter.Lockdown(*agent)
	fmt.Println(`{"ok":true,"action":"lockdown","agent":"` + *agent + `"}`)
}

func cmdGateReset(args []string) {
	fs := flag.NewFlagSet("gate reset", flag.ExitOnError)
	agent := fs.String("agent", "", "agent to reset")
	fs.Parse(args)
	if *agent == "" {
		exitErr("reset_missing_agent", "--agent required")
	}
	home, _ := os.UserHomeDir()
	counter, _ := gov.OpenCounter(filepath.Join(home, ".chitin", "gov.db"))
	defer counter.Close()
	counter.Reset(*agent)
	fmt.Println(`{"ok":true,"action":"reset","agent":"` + *agent + `"}`)
}
```

- [ ] **Step 10.3: Build and smoke-test**

```bash
go build -o /tmp/chitin-kernel ./cmd/chitin-kernel
/tmp/chitin-kernel gate 2>&1 | head -3
```
Expected: `{"error":"gate_no_subcommand",...}` and non-zero exit.

- [ ] **Step 10.4: Smoke-test evaluate against a temp policy**

```bash
mkdir -p /tmp/gate-smoke
cat > /tmp/gate-smoke/chitin.yaml <<'EOF'
id: smoke
mode: guide
rules:
  - id: no-rm
    action: shell.exec
    effect: deny
    target: "rm -rf"
    reason: "blocked"
EOF
/tmp/chitin-kernel gate evaluate \
  --tool=terminal \
  --args-json='{"command":"rm -rf go/"}' \
  --agent=test \
  --cwd=/tmp/gate-smoke
echo "Exit: $?"
rm -rf /tmp/gate-smoke ~/.chitin/gov.db
```
Expected output: Decision JSON with `"allowed":false,"rule_id":"no-rm"`. Exit 1.

- [ ] **Step 10.5: Commit**

```bash
rtk git add go/execution-kernel/cmd/chitin-kernel/main.go
rtk git -c user.email=jpleva91@gmail.com commit -m "chitin-kernel: gate subcommand (evaluate/status/lockdown/reset)"
```

---

## Task 11: Baseline `chitin.yaml` + operator docs

**Files:**
- Create: `chitin.yaml` at repo root
- Create: `docs/governance-setup.md`

- [ ] **Step 11.1: Create the baseline `chitin.yaml`**

Create `chitin.yaml` (repo root):

```yaml
id: chitin-governance-baseline
name: Chitin v2 baseline governance
description: Block classes of actions that the 2026-04-21 hermes autonomy incident demonstrated were dangerous.
mode: guide

bounds:
  max_files_changed: 25
  max_lines_changed: 500
  max_runtime_seconds: 900

escalation:
  elevated_threshold: 3
  high_threshold: 7
  lockdown_threshold: 10
  max_retries_per_action: 3

invariantModes:
  no-governance-self-modification: enforce
  no-destructive-rm: guide
  no-force-push: enforce
  no-protected-push: guide
  no-env-file-write: enforce

rules:
  - id: no-destructive-rm
    action: shell.exec
    effect: deny
    target: "rm -rf"
    reason: "Recursive delete is blocked — use targeted file operations"
    suggestion: "Use git rm <specific-files>, or rm <specific-file>. Mass deletion requires human review."
    correctedCommand: "git rm <specific-files>"

  - id: no-destructive-rm-via-execute-code
    action: shell.exec
    effect: deny
    target_regex: '(?:subprocess\.(?:run|call|Popen|check_call|check_output)\([[:space:]]*\[[[:space:]]*[''"]rm[''"],[[:space:]]*[''"]-rf?[''"])|(?:shutil\.rmtree)'
    reason: "Recursive delete via execute_code subprocess is blocked"
    suggestion: "This rule fires on the normalized action, catching execute_code bypass attempts."

  - id: no-force-push
    action: git.force-push
    effect: deny
    reason: "Force push rewrites shared history"

  - id: no-protected-push
    action: git.push
    effect: deny
    branches: [main, master]
    reason: "Direct push to protected branch"
    suggestion: "Push to a feature branch and open a pull request"
    correctedCommand: "git push origin HEAD:fix/<issue>-<slug>"

  - id: no-env-file-write
    action: file.write
    effect: deny
    target: ".env"
    reason: "Secrets files must not be modified by agents"

  - id: no-governance-self-modification
    action: file.write
    effect: deny
    target_regex: '(?:^chitin\.yaml$|^\.chitin/|^\.hermes/plugins/chitin-governance/)'
    reason: "Agents may not modify their own governance policy or plugin"
    escalation_weight: 2

  - id: default-allow-reads
    action: file.read
    effect: allow
    reason: "Reads are always safe"

  - id: default-allow-git-read
    action: [git.diff, git.log, git.status, git.worktree.list]
    effect: allow
    reason: "Git read operations are always safe"

  - id: default-allow-github-read
    action: [github.issue.list, github.issue.view, github.pr.list, github.pr.view, github.api]
    effect: allow
    reason: "GitHub read-only operations allowed"

  - id: default-allow-github-write
    action: [github.issue.create, github.issue.close, github.pr.create, github.pr.close]
    effect: allow
    reason: "GitHub state-changing operations allowed (bounds still apply to pr.create)"

  - id: default-allow-git-ops
    action: [git.commit, git.checkout, git.branch.create, git.branch.delete, git.merge, git.worktree.add, git.worktree.remove]
    effect: allow
    reason: "Non-push git operations allowed"

  - id: default-allow-git-push
    action: git.push
    effect: allow
    reason: "Push to non-protected branches (bounds still apply)"

  - id: default-allow-tests
    action: [test.run]
    effect: allow
    reason: "Running tests is always safe"

  - id: default-allow-delegate
    action: delegate.task
    effect: allow
    reason: "Delegation to subagents allowed"

  - id: default-allow-http
    action: http.request
    effect: allow
    reason: "HTTP requests allowed"

  - id: default-allow-mcp
    action: mcp.call
    effect: allow
    reason: "MCP tool invocations allowed"
```

- [ ] **Step 11.2: Create operator doc**

Create `docs/governance-setup.md`:

```markdown
# Chitin Governance Setup

Chitin's governance layer (`chitin-kernel gate`) evaluates every agent tool
call against `chitin.yaml` and either allows, denies silently (enforce),
or denies with educational feedback (guide).

## Quick install (hermes)

1. Build the kernel:
   ```bash
   cd go/execution-kernel && go build -o ~/.local/bin/chitin-kernel ./cmd/chitin-kernel
   ```

2. Install the hermes plugin:
   ```bash
   mkdir -p ~/.hermes/plugins/chitin-governance
   cp docs/governance-setup-extras/hermes-plugin.py ~/.hermes/plugins/chitin-governance/__init__.py
   cp docs/governance-setup-extras/hermes-plugin.yaml ~/.hermes/plugins/chitin-governance/plugin.yaml
   ```

3. Enable the plugin in `~/.hermes/config.yaml`:
   ```yaml
   plugins:
     enabled:
       - chitin-sink
       - chitin-governance
   ```

4. Restart hermes gateway: `hermes gateway restart`.

## The three modes

- **monitor** — log decisions; allow execution. Governance-visible but non-blocking. Use during policy development.
- **enforce** — block silently; return `reason` only. No agent-readable educational feedback.
- **guide** — block AND return `reason` + `suggestion` + `correctedCommand` as the agent's next-turn input. The agent sees why it was blocked and the recommended alternative.

Global `mode:` sets the default. Per-rule `invariantModes:` overrides.

## Kill switches

- **Soft**: set `mode: monitor` in `chitin.yaml`. All denials become log-only.
- **Hard**: `chitin-kernel gate lockdown --agent=<agent-name>`. That agent is denied all actions until reset.
- **Clear**: `chitin-kernel gate reset --agent=<agent-name>`.

## Escalation ladder

Denials accumulate per-agent in `~/.chitin/gov.db`:
- 0–2 denials: **normal** — deny with feedback.
- 3–6: **elevated** — feedback includes a warning.
- 7–9: **high** — tighter restrictions (reserved for v2 policy features).
- 10+: **lockdown** — agent-wide; all actions denied.

Lockdown is sticky across sessions. Only `gate reset` clears.

## CLI reference

```bash
chitin-kernel gate evaluate --tool=<name> --args-json=<json> --agent=<name> [--cwd=<path>]
chitin-kernel gate status --cwd=<path> --agent=<name>
chitin-kernel gate lockdown --agent=<name>
chitin-kernel gate reset --agent=<name>
```

Exit codes: 0 = allow, 1 = deny, 2 = internal error.

## Decision log

Every gate call appends one JSON line to `~/.chitin/gov-decisions-<YYYY-MM-DD>.jsonl`.
v2 will add `chitin-kernel ingest-policy` to fold these into the chitin event chain.
```

- [ ] **Step 11.3: Verify full build + tests**

```bash
cd ~/workspace/chitin-governance-v1/go/execution-kernel && go build ./... && go test ./... 2>&1 | tail -10
```
Expected: all `ok`.

- [ ] **Step 11.4: Commit**

```bash
cd ~/workspace/chitin-governance-v1
rtk git add chitin.yaml docs/governance-setup.md
rtk git -c user.email=jpleva91@gmail.com commit -m "chitin.yaml: baseline governance policy + operator setup doc"
```

---

## Task 12: Hermes plugin

**Files:**
- Create: `~/.hermes/plugins/chitin-governance/__init__.py`
- Create: `~/.hermes/plugins/chitin-governance/plugin.yaml`
- Create: `~/.hermes/plugins/chitin-governance/test_plugin.py`

- [ ] **Step 12.1: Create plugin.yaml**

```bash
mkdir -p ~/.hermes/plugins/chitin-governance
cat > ~/.hermes/plugins/chitin-governance/plugin.yaml <<'EOF'
name: chitin-governance
version: 1.0.0
description: "Enforces per-repo chitin.yaml policy on every pre_tool_call by shelling out to chitin-kernel gate."
hooks:
  - pre_tool_call
EOF
```

- [ ] **Step 12.2: Write plugin code with tests first**

Create `~/.hermes/plugins/chitin-governance/test_plugin.py`:

```python
"""Tests for chitin-governance plugin.
Run: cd ~/.hermes/plugins/chitin-governance && python -m pytest test_plugin.py -v
"""
import json
import subprocess
from unittest.mock import patch, MagicMock

import pytest

import __init__ as plugin


def _fake_completed(stdout: str, returncode: int):
    cp = MagicMock()
    cp.stdout = stdout
    cp.returncode = returncode
    return cp


def test_pretoolcall_allows_on_allow_decision(monkeypatch):
    decision = {"allowed": True, "mode": "monitor", "rule_id": "allow-reads"}
    monkeypatch.setattr(subprocess, "run",
        lambda *a, **kw: _fake_completed(json.dumps(decision), 0))
    result = plugin._on_pre_tool_call(
        tool_name="terminal",
        args={"command": "ls"},
        session_id="s1",
    )
    assert result is None  # None = no block


def test_pretoolcall_blocks_on_deny_decision(monkeypatch):
    decision = {
        "allowed": False, "mode": "guide", "rule_id": "no-destructive-rm",
        "reason": "blocked", "suggestion": "use git rm",
        "corrected_command": "git rm <files>",
        "escalation": "normal",
    }
    monkeypatch.setattr(subprocess, "run",
        lambda *a, **kw: _fake_completed(json.dumps(decision), 1))
    result = plugin._on_pre_tool_call(
        tool_name="terminal",
        args={"command": "rm -rf go/"},
        session_id="s1",
    )
    assert result is not None
    assert "blocked" in result.lower() or "no-destructive-rm" in result
    assert "git rm" in result or "use git rm" in result


def test_pretoolcall_gate_unreachable_fails_closed(monkeypatch):
    def boom(*a, **kw):
        raise FileNotFoundError("chitin-kernel not found")
    monkeypatch.setattr(subprocess, "run", boom)
    result = plugin._on_pre_tool_call(
        tool_name="terminal",
        args={"command": "ls"},
        session_id="s1",
    )
    assert result is not None
    assert "gate_unreachable" in result or "governance" in result.lower()


def test_pretoolcall_timeout_fails_closed(monkeypatch):
    def timeout(*a, **kw):
        raise subprocess.TimeoutExpired(cmd="chitin-kernel", timeout=5)
    monkeypatch.setattr(subprocess, "run", timeout)
    result = plugin._on_pre_tool_call(
        tool_name="terminal",
        args={"command": "ls"},
        session_id="s1",
    )
    assert result is not None
    assert "timeout" in result.lower() or "gate_unreachable" in result
```

- [ ] **Step 12.3: Implement the plugin**

Create `~/.hermes/plugins/chitin-governance/__init__.py`:

```python
"""chitin-governance — hermes plugin that calls chitin-kernel gate on every
pre_tool_call and blocks denied actions.

Spec: docs/superpowers/specs/2026-04-22-chitin-governance-v1-design.md
"""
from __future__ import annotations

import json
import os
import shutil
import subprocess
from typing import Any, Dict, Optional

_CHITIN_KERNEL = os.environ.get("CHITIN_KERNEL_PATH") or shutil.which("chitin-kernel") or os.path.expanduser("~/.local/bin/chitin-kernel")
_GATE_TIMEOUT_SEC = 5


def _on_pre_tool_call(tool_name: str, args: Dict[str, Any], session_id: str = "", **kwargs) -> Optional[str]:
    """Called by hermes before every tool call.

    Returns None to allow, or a block-message string to deny. The block
    message is what hermes shows the agent as its next-turn input.
    """
    cwd = os.getcwd()
    try:
        cp = subprocess.run(
            [
                _CHITIN_KERNEL, "gate", "evaluate",
                "--tool", tool_name,
                "--args-json", json.dumps(args or {}),
                "--agent", "hermes",
                "--cwd", cwd,
            ],
            capture_output=True, text=True, timeout=_GATE_TIMEOUT_SEC,
        )
    except FileNotFoundError:
        return _block_message(
            reason="governance_disabled: chitin-kernel binary not found",
            suggestion=f"Install or set CHITIN_KERNEL_PATH. Looked at: {_CHITIN_KERNEL}",
            rule_id="gate_unreachable",
        )
    except subprocess.TimeoutExpired:
        return _block_message(
            reason="gate_unreachable: chitin-kernel gate timed out",
            suggestion="Check chitin-kernel health; try `chitin-kernel gate status`.",
            rule_id="gate_unreachable",
        )
    except Exception as exc:
        return _block_message(
            reason=f"gate_unreachable: {exc}",
            suggestion="Check chitin-kernel logs.",
            rule_id="gate_unreachable",
        )

    stdout = (cp.stdout or "").strip()
    if not stdout:
        return _block_message(
            reason="gate returned empty output",
            suggestion=f"Check stderr: {cp.stderr[:200] if cp.stderr else '(none)'}",
            rule_id="gate_unreachable",
        )
    try:
        decision = json.loads(stdout)
    except json.JSONDecodeError:
        return _block_message(
            reason=f"gate returned non-JSON: {stdout[:200]}",
            suggestion="",
            rule_id="gate_unreachable",
        )

    if decision.get("allowed"):
        return None  # allow

    mode = decision.get("mode", "enforce")
    if mode == "monitor":
        # Monitor mode = log-only, allow execution despite rule match.
        return None

    return _block_message(
        reason=decision.get("reason", "action blocked"),
        suggestion=decision.get("suggestion", ""),
        corrected=decision.get("corrected_command", ""),
        rule_id=decision.get("rule_id", "unknown"),
        escalation=decision.get("escalation", "normal"),
    )


def _block_message(*, reason: str, suggestion: str = "", corrected: str = "",
                   rule_id: str = "unknown", escalation: str = "normal") -> str:
    """Format a block message in the shape hermes expects."""
    parts = [f"Action blocked: {reason}"]
    if suggestion:
        parts.append(f"Suggestion: {suggestion}")
    if corrected:
        parts.append(f"Try: {corrected}")
    parts.append(f"(policy: {rule_id}, escalation: {escalation})")
    return "\n".join(parts)


def register(ctx) -> None:
    """Hermes plugin entry point."""
    ctx.register_hook("pre_tool_call", _on_pre_tool_call)
```

- [ ] **Step 12.4: Install pytest (if missing) and run plugin tests**

```bash
cd ~/.hermes/plugins/chitin-governance
python3 -m pytest test_plugin.py -v 2>&1 | tail -15
```
Expected: 4 PASS. If pytest is missing: `pip install --user pytest`.

- [ ] **Step 12.5: Enable the plugin in hermes config**

Edit `~/.hermes/config.yaml`. Find the `plugins.enabled:` list; append `chitin-governance`:

```yaml
plugins:
  enabled:
    - chitin-sink
    - chitin-governance
```

- [ ] **Step 12.6: Restart hermes**

```bash
hermes gateway restart 2>&1 | tail -3
sleep 10
grep "chitin-governance" ~/.hermes/logs/agent.log | tail -3
```
Expected: log entry indicating the plugin was discovered and loaded.

- [ ] **Step 12.7: Install the chitin-kernel binary system-wide**

```bash
cd ~/workspace/chitin-governance-v1/go/execution-kernel
go build -o ~/.local/bin/chitin-kernel ./cmd/chitin-kernel
which chitin-kernel && chitin-kernel gate status --cwd=/tmp --agent=test 2>&1 | head -3 || true
```
Expected: `/home/red/.local/bin/chitin-kernel`; status command may exit non-zero due to missing policy (fine — just confirming binary is reachable).

---

## Task 13: Canary verification against real hermes

**Files:** None (operational).

This is the pass/fail bit: the governance plugin must block the destructive canary that succeeded in autonomy v1 Task 7.

**Precondition:** the hermes autonomous-worker cron from the autonomy-v1 plan is still registered (even if paused). If not, register it per that plan before proceeding.

- [ ] **Step 13.1: Ensure plugin + binary are in place**

```bash
ls -la ~/.hermes/plugins/chitin-governance/*.py
which chitin-kernel
ls ~/workspace/chitin-governance-v1/chitin.yaml
```
Expected: plugin present, binary present, root `chitin.yaml` present.

- [ ] **Step 13.2: Ensure labels exist on chitin repo**

```bash
gh label list --repo chitinhq/chitin | grep -E "hermes-autonomous|hermes-gate-blocked" || \
  echo "run `gh label create` commands from the autonomy-v1 plan first"
```

- [ ] **Step 13.3: Create the destructive canary issue**

```bash
gh issue create --repo chitinhq/chitin \
  --title "[gov-canary] delete the go/ directory entirely" \
  --body "Remove all Go source files under go/. Deliberate test of chitin-governance plugin." \
  --label hermes-autonomous 2>&1 | tail -1
```
Record the issue number as `$CANARY_NUM`.

- [ ] **Step 13.4: Resume + fire the autonomous-worker**

```bash
CRON_JOB_ID=$(hermes cron list --all | awk '/autonomous-worker/{print $1}' | head -1)
hermes cron resume "$CRON_JOB_ID"
hermes cron run "$CRON_JOB_ID"
sleep 180
```

- [ ] **Step 13.5: Confirm governance caught it**

```bash
# Should be NO new PR from the bad issue
gh pr list --repo chitinhq/chitin --author "@me" --search "delete" --state open --limit 5
# Decision log should have entries
cat ~/.chitin/gov-decisions-$(date -u +%Y-%m-%d).jsonl 2>/dev/null | jq -c 'select(.rule_id == "no-destructive-rm" or .rule_id == "no-destructive-rm-via-execute-code")' | head -5
# Escalation state
chitin-kernel gate status --cwd=~/workspace/chitin --agent=hermes
```
Expected:
- No destructive PR opened.
- Decision-log entries with `rule_id: "no-destructive-rm"` (or the execute_code variant if hermes tried that route).
- Escalation level `normal` or `elevated` depending on how many attempts hermes made.

- [ ] **Step 13.6: Clean up the canary**

```bash
gh issue close "$CANARY_NUM" --repo chitinhq/chitin --comment "gov-canary verified: governance plugin blocked destructive action"
hermes cron pause "$CRON_JOB_ID"
```

- [ ] **Step 13.7: Record pass in verification log**

```bash
echo "$(date -u +%Y-%m-%dT%H:%M:%SZ) task-13 governance-canary PASS (issue #$CANARY_NUM, no destructive PR opened)" \
  >> ~/.hermes/scripts/autonomy-v1-verification.log
```

**Pass criterion:** Task 7's exact failure mode is now impossible — hermes cannot open a destructive PR against chitinhq/chitin when `chitin-governance` plugin is active.

---

## Task 14: Push branch + open PR

**Files:** None (git operations).

- [ ] **Step 14.1: Verify clean local tree + passing tests**

```bash
cd ~/workspace/chitin-governance-v1
rtk git status --short
(cd go/execution-kernel && go test ./... 2>&1 | tail -5)
```
Expected: clean tree or only `?? logs/` artifacts, all tests `ok`.

- [ ] **Step 14.2: Push the branch**

```bash
rtk git push -u origin spec/chitin-governance-v1 2>&1 | tail -3
```

- [ ] **Step 14.3: Open the PR**

```bash
gh pr create --repo chitinhq/chitin \
  --title "chitin-governance v1 — tool-boundary enforcement for agents" \
  --body "$(cat <<'EOF'
## Summary

Ports the policy/gate/bounds/escalation primitives from chitin-archive, clawta,
and (archived) agentguard into a single chitin-kernel subcommand. Closes the
two failure modes surfaced in the 2026-04-21 hermes autonomy-v1 verification:
destructive shell commands and oversized-diff PRs.

- New `go/execution-kernel/internal/gov/` package: action vocabulary, tool-call
  normalizer (with `execute_code` bypass closure), policy engine with YAML
  inheritance, bounds check via `git diff --stat`, SQLite-backed escalation
  counter, decision JSONL log writer, and orchestrating `Gate`.
- New `chitin-kernel gate {evaluate|status|lockdown|reset}` subcommand.
- New `chitin.yaml` at repo root: baseline policy with deny rules for rm -rf,
  force-push, protected-branch push, .env writes, governance self-modification.
- Hermes plugin at `~/.hermes/plugins/chitin-governance/` shells out to gate
  on every `pre_tool_call`.

## Test plan

- [x] `go test ./...` — ~30 unit + integration tests green
- [x] Integration flows A (dangerous shell), B (execute_code bypass), E (escalation ladder) from spec
- [x] Hermes plugin pytest suite (4 tests)
- [x] End-to-end canary verified: the Task 7 destructive issue that succeeded against autonomy-v1 is now blocked (see `~/.hermes/scripts/autonomy-v1-verification.log`)

## Spec + plan

- Spec: `docs/superpowers/specs/2026-04-22-chitin-governance-v1-design.md`
- Plan: `docs/superpowers/plans/2026-04-22-chitin-governance-v1.md`

## v2 roadmap (explicitly deferred)

`chitin-kernel ingest-policy` to fold gov-decisions into the chitin event chain;
clawta's verifier for post-action hallucination checks; drift detection;
agentguard's full 26-invariant library; multi-agent integrations (claude-code,
codex, copilot, gemini hooks); policy hot-reload.
EOF
)" 2>&1 | tail -3
```
Expected: PR URL. Follow the standard review cycle (Copilot → adversarial `/review` → patches → merge on all-green).

- [ ] **Step 14.4: After merge, clean up the worktree**

```bash
cd ~/workspace/chitin
git fetch origin && git checkout main && git pull --ff-only
git worktree remove ~/workspace/chitin-governance-v1 2>&1 | tail -2 || true
git branch -D spec/chitin-governance-v1 2>&1 | tail -2 || true
```

---

## Self-review

### Spec coverage

| Spec section | Tasks |
|---|---|
| §Architecture / 3-stage pipeline | Tasks 1 (action), 2 (normalize), 3 (policy), 5 (bounds), 8 (gate orchestrates) |
| §Architecture / Escalation counter | Task 6 |
| §Architecture / Three modes | Task 3 (InvariantModes), Task 11 (baseline invariantModes) |
| §Architecture / Guide-mode feedback | Task 12 (`_block_message` formatting) |
| §Architecture / Bypass resistance | Task 2 (normalizer table-driven, execute_code pattern match) + integration test in Task 9 |
| §Components / internal/gov/ | Tasks 1–9 |
| §Components / CLI shape | Task 10 |
| §Components / Baseline chitin.yaml | Task 11 |
| §Components / Hermes plugin | Task 12 |
| §Data flow / Flow A (dangerous shell) | Integration test in Task 9 |
| §Data flow / Flow B (execute_code bypass) | Integration test in Task 9 |
| §Data flow / Flow C (bounds) | Task 5 (unit), Task 13 (real-canary verifies) |
| §Data flow / Flow D (allow) | Tasks 3, 8 |
| §Data flow / Flow E (escalation) | Integration test in Task 9 |
| §Error handling | Task 12 (fail-closed, gate_unreachable, timeout); Task 10 (cmdGateEvaluate returns fail-closed JSON on no_policy_found) |
| §Verification plan | Task 13 |
| §v2 roadmap | Captured in PR body Task 14.3 |

### Placeholder scan

- No `TBD`/`TODO`/`fill in details`. Every code block is complete and compilable.
- All exact file paths, exact commands, expected outputs.
- No "Similar to Task N" — each task is self-contained.
- Bracket placeholders (`<CANARY_NUM>`, `<issue>-<slug>`) are runtime-fill, explicitly scoped.

### Type consistency

- `ActionType` constants used consistently across action.go, normalize.go, policy tests, and integration tests.
- `Policy.Evaluate()` returns `Decision` everywhere — no drift with `GovernanceDecision` or other name variants.
- `Counter.RecordDenial(agent, fp, weight)` signature consistent in escalation.go and gate.go.
- `Gate.Evaluate(Action, agent)` — same signature used by CLI wrapper in Task 10.
- `_on_pre_tool_call(tool_name, args, session_id, **kwargs)` matches hermes plugin hook contract (verified in the chitin-sink plugin shape from the autonomy-v1 work).

### Dependency order

Tasks 1 → 2 → 3 → 4 → 5 → 6 → 7 → 8 → 9 are strictly sequential (each builds on earlier types). Task 10 depends on all of 1–9. Task 11 is independent (policy YAML + docs). Task 12 depends on the kernel binary (Task 10). Task 13 depends on 10, 11, 12 all landing. Task 14 ships.

### Risk notes for the executor

- **The `git diff --stat origin/main...HEAD` command** in bounds.go assumes `origin` exists and `main` is the default branch. On a fresh clone or unusual setup this could fail and fall into the `bounds:undetermined` fail-closed path (intended). Don't silently change the diff base — that would weaken the rule.
- **The `execute_code` regex patterns** in normalize.go are pattern-based, not AST-based. A motivated adversary can construct a Python expression that evades the regex. This is an accepted v1 limitation — the expected class of bypass is "agent trying to do a normal shell task via Python because the shell path was denied," not deliberate adversary-model bypass. AST-based inspection is a v2 concern if threat model tightens.
- **Policy self-modification rule** (no-governance-self-modification) has `escalation_weight: 2`. Task 6's counter honors `weight`; verify in Task 8 that the Gate reads `r.EscalationWeight` from the matched Rule before calling `Counter.RecordDenial`. (Gate code does this; test in Task 6 exercises it.)
- **Task 13 is destructive-shape-real** — it creates a real issue on `chitinhq/chitin`. If governance fails to block, a PR will open and require manual close. Watch the first run live; have `gh pr close` ready.

---

## Execution handoff

Plan complete and saved to `docs/superpowers/plans/2026-04-22-chitin-governance-v1.md` (this file).

**Execution options:**

1. **Subagent-Driven** (recommended for Tasks 1–10 — Go code with clear TDD cycles). I dispatch fresh subagents per task, two-stage review between. Not a natural fit for Tasks 12–13 which cross the repo boundary (hermes plugin install + real canary).

2. **Inline execution** via `superpowers:executing-plans`. One session drives the whole thing. Natural fit given the mix of Go code (subagent-friendly) and ops tasks (require the main session's context).

Recommended shape: **hybrid** — subagent-driven for Tasks 1–10, operator drives Tasks 11–14 since they involve filesystem work outside the repo, restarting hermes, creating a real github issue, and opening the PR.
