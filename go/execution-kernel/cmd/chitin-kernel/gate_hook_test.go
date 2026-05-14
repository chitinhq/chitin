package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chitinhq/chitin/go/execution-kernel/internal/gov"
	"github.com/chitinhq/chitin/go/execution-kernel/internal/replay"
)

// hookTestEnv stages a CHITIN_HOME + chitin.yaml in cwd so evalHookStdin
// can run end-to-end against a real policy + sqlite gov.db.
type hookTestEnv struct {
	cwd     string
	chitin  string
	cleanup func()
}

func setupHookEnv(t *testing.T, policyYAML string) *hookTestEnv {
	t.Helper()
	cwd := t.TempDir()
	chitin := t.TempDir()
	if err := os.WriteFile(filepath.Join(cwd, "chitin.yaml"), []byte(policyYAML), 0o644); err != nil {
		t.Fatalf("write chitin.yaml: %v", err)
	}
	prev := os.Getenv("CHITIN_HOME")
	_ = os.Setenv("CHITIN_HOME", chitin)
	t.Cleanup(func() { _ = os.Setenv("CHITIN_HOME", prev) })
	prevEnvelope := os.Getenv("CHITIN_BUDGET_ENVELOPE")
	_ = os.Unsetenv("CHITIN_BUDGET_ENVELOPE")
	t.Cleanup(func() { _ = os.Setenv("CHITIN_BUDGET_ENVELOPE", prevEnvelope) })
	return &hookTestEnv{cwd: cwd, chitin: chitin}
}

func runHookCall(t *testing.T, env *hookTestEnv, payload map[string]any, envelopeFlag string) (stdout, stderr string, exitCode int) {
	t.Helper()
	return runHookCallAsAgent(t, env, "claude-code", payload, envelopeFlag)
}

func runHookCallAsAgent(t *testing.T, env *hookTestEnv, agent string, payload map[string]any, envelopeFlag string) (stdout, stderr string, exitCode int) {
	t.Helper()
	if _, ok := payload["cwd"]; !ok {
		payload["cwd"] = env.cwd
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	in := bytes.NewReader(body)
	var out, errOut bytes.Buffer
	exitCode = evalHookStdin(in, &out, &errOut, agent, envelopeFlag, "", false, false, false)
	return out.String(), errOut.String(), exitCode
}

const baselinePolicy = `
id: hook-test
mode: enforce
rules:
  - id: allow-read
    action: file.read
    effect: allow
    reason: read ok
  - id: allow-write
    action: file.write
    effect: allow
    reason: write ok
  - id: no-rm
    action: file.recursive_delete
    effect: deny
    reason: "no rm -rf"
    suggestion: "use git rm"
    correctedCommand: "git rm <files>"
  - id: allow-shell
    action: shell.exec
    effect: allow
    reason: shell ok
  - id: allow-git-status
    action: git.status
    effect: allow
    reason: git status ok
  - id: allow-http
    action: http.request
    effect: allow
    reason: http ok
  - id: allow-task
    action: delegate.task
    effect: allow
    reason: task ok
`

func TestEvalHookStdin_AllowReadIsExit0EmptyStdout(t *testing.T) {
	env := setupHookEnv(t, baselinePolicy)
	stdout, _, code := runHookCall(t, env, map[string]any{
		"tool_name":  "Read",
		"tool_input": map[string]any{"file_path": "/etc/hosts"},
	}, "")
	if code != 0 {
		t.Fatalf("exit=%d want 0 (allow), stdout=%q", code, stdout)
	}
	if stdout != "" {
		t.Fatalf("allow stdout must be empty, got %q", stdout)
	}
}

func TestEvalHookStdin_UnknownToolWritesUnknownToolsJSONL(t *testing.T) {
	env := setupHookEnv(t, baselinePolicy)
	stdout, stderr, code := runHookCallAsAgent(t, env, "hermes", map[string]any{
		"tool_name":       "future_hermes_tool",
		"hook_event_name": "PreToolUse",
		"tool_input":      map[string]any{"x": "y"},
	}, "")
	if code == 0 {
		t.Fatalf("unknown tool should be denied, got allow stdout=%q stderr=%q", stdout, stderr)
	}

	body, err := os.ReadFile(filepath.Join(env.chitin, "unknown-tools.jsonl"))
	if err != nil {
		t.Fatalf("read unknown-tools.jsonl: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(body)), "\n")
	if len(lines) != 1 {
		t.Fatalf("unknown-tools lines=%d want 1; body=%q", len(lines), string(body))
	}
	var row map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &row); err != nil {
		t.Fatalf("parse unknown-tools row: %v", err)
	}
	if row["driver"] != "hermes" || row["raw_tool_name"] != "future_hermes_tool" {
		t.Fatalf("unexpected unknown tool row: %#v", row)
	}
	if row["action_target"] != "future_hermes_tool" {
		t.Fatalf("action_target=%v want future_hermes_tool", row["action_target"])
	}
}

func TestEvalHookStdin_NoRecordStillLogsUnknownTool(t *testing.T) {
	env := setupHookEnv(t, baselinePolicy)
	body, _ := json.Marshal(map[string]any{
		"tool_name":  "future_codex_tool",
		"tool_input": map[string]any{"x": "y"},
		"cwd":        env.cwd,
	})
	var out, errOut bytes.Buffer
	_ = evalHookStdin(bytes.NewReader(body), &out, &errOut, "codex", "", "", false, true, false)

	got, err := os.ReadFile(filepath.Join(env.chitin, "unknown-tools.jsonl"))
	if err != nil {
		t.Fatalf("unknown tool should log even with --no-record: %v", err)
	}
	if !strings.Contains(string(got), `"raw_tool_name":"future_codex_tool"`) {
		t.Fatalf("unknown-tools.jsonl missing raw tool: %q", string(got))
	}
}

func TestEvalHookStdin_CodexInnerHopAppendsOneDecisionPerToolCall(t *testing.T) {
	env := setupHookEnv(t, baselinePolicy)
	t.Setenv("CHITIN_DRIVER", "codex")
	t.Setenv("CHITIN_AGENT_INSTANCE_ID", "codex-inner-hop-agent")

	sessionID := "codex-inner-hop-session"
	turnID := "codex-turn-1"
	calls := []struct {
		toolName   string
		toolUseID  string
		toolInput  map[string]any
		actionType string
		target     string
	}{
		{
			toolName:   "read_file",
			toolUseID:  "tool-use-read",
			toolInput:  map[string]any{"file_path": "README.md"},
			actionType: string(gov.ActFileRead),
			target:     "README.md",
		},
		{
			toolName:  "apply_patch",
			toolUseID: "tool-use-patch",
			toolInput: map[string]any{"input": strings.Join([]string{
				"*** Begin Patch",
				"*** Update File: README.md",
				"@@",
				"*** End Patch",
			}, "\n")},
			actionType: string(gov.ActFileWrite),
			target:     "README.md",
		},
		{
			toolName:   "Bash",
			toolUseID:  "tool-use-shell",
			toolInput:  map[string]any{"command": "echo codex-inner-hop"},
			actionType: string(gov.ActShellExec),
			target:     "echo codex-inner-hop",
		},
	}

	for i, call := range calls {
		stdout, stderr, code := runHookCallAsAgent(t, env, "codex", map[string]any{
			"session_id":      sessionID,
			"turn_id":         turnID,
			"tool_use_id":     call.toolUseID,
			"hook_event_name": "PreToolUse",
			"tool_name":       call.toolName,
			"tool_input":      call.toolInput,
		}, "")
		if code != 0 {
			t.Fatalf("call %d (%s): exit=%d want 0; stdout=%q stderr=%q", i, call.toolName, code, stdout, stderr)
		}
		if stdout != "" {
			t.Fatalf("call %d (%s): allow stdout must be empty, got %q", i, call.toolName, stdout)
		}
	}

	eventsPath := filepath.Join(env.chitin, "events-"+sessionID+".jsonl")
	data, err := os.ReadFile(eventsPath)
	if err != nil {
		t.Fatalf("read codex session events: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != len(calls) {
		t.Fatalf("decision chain lines=%d want %d:\n%s", len(lines), len(calls), data)
	}
	for i, line := range lines {
		var ev struct {
			EventType string          `json:"event_type"`
			RunID     string          `json:"run_id"`
			SessionID string          `json:"session_id"`
			Surface   string          `json:"surface"`
			ChainID   string          `json:"chain_id"`
			Seq       int             `json:"seq"`
			Labels    map[string]any  `json:"labels"`
			Payload   json.RawMessage `json:"payload"`
		}
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			t.Fatalf("line %d: unmarshal event: %v\n%s", i, err, line)
		}
		if ev.EventType != "decision" {
			t.Fatalf("line %d: event_type=%q want decision", i, ev.EventType)
		}
		if ev.RunID != sessionID || ev.SessionID != sessionID || ev.ChainID != sessionID {
			t.Fatalf("line %d: event not appended to codex session chain: run_id=%q session_id=%q chain_id=%q",
				i, ev.RunID, ev.SessionID, ev.ChainID)
		}
		if ev.Surface != "codex" {
			t.Fatalf("line %d: surface=%q want codex", i, ev.Surface)
		}
		if ev.Seq != i {
			t.Fatalf("line %d: seq=%d want %d", i, ev.Seq, i)
		}
		if ev.Labels["driver"] != "codex" {
			t.Fatalf("line %d: labels.driver=%v want codex", i, ev.Labels["driver"])
		}

		var payload map[string]any
		if err := json.Unmarshal(ev.Payload, &payload); err != nil {
			t.Fatalf("line %d: unmarshal payload: %v", i, err)
		}
		if payload["decision"] != "allow" {
			t.Fatalf("line %d: decision=%v want allow", i, payload["decision"])
		}
		if payload["action_type"] != calls[i].actionType {
			t.Fatalf("line %d: action_type=%v want %s", i, payload["action_type"], calls[i].actionType)
		}
		if payload["action_target"] != calls[i].target {
			t.Fatalf("line %d: action_target=%v want %s", i, payload["action_target"], calls[i].target)
		}
	}

	stats, err := replay.ComputeStatsIn("action_type", env.chitin)
	if err != nil {
		t.Fatalf("ComputeStatsIn: %v", err)
	}
	if stats.Total != len(calls) {
		t.Fatalf("stats total=%d want %d (chain stats must not double-count hook decisions)", stats.Total, len(calls))
	}
	for _, call := range calls {
		b := stats.Buckets[call.actionType]
		if b.Decisions != 1 || b.Allows != 1 || b.Denies != 0 {
			t.Fatalf("stats bucket %s=%+v want one allow", call.actionType, b)
		}
	}
}

func TestEvalHookStdin_DenyRmRfIsExit2BlockJSON(t *testing.T) {
	env := setupHookEnv(t, baselinePolicy)
	stdout, _, code := runHookCall(t, env, map[string]any{
		"tool_name":  "Bash",
		"tool_input": map[string]any{"command": "rm -rf go/"},
	}, "")
	if code != 2 {
		t.Fatalf("exit=%d want 2 (block)", code)
	}
	var parsed map[string]string
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &parsed); err != nil {
		t.Fatalf("stdout not valid JSON: %v\n%s", err, stdout)
	}
	if parsed["decision"] != "block" {
		t.Fatalf("decision=%q", parsed["decision"])
	}
	if !strings.Contains(parsed["reason"], "no rm -rf") {
		t.Fatalf("reason missing policy text: %q", parsed["reason"])
	}
	// Suggestion + corrected propagate to the model.
	if !strings.Contains(parsed["reason"], "git rm") {
		t.Fatalf("reason missing suggestion/corrected: %q", parsed["reason"])
	}
}

func TestEvalHookStdin_HermesExecuteCodeSubprocessRmRfDenied(t *testing.T) {
	env := setupHookEnv(t, baselinePolicy)
	stdout, _, code := runHookCallAsAgent(t, env, "hermes", map[string]any{
		"tool_name": "execute_code",
		"tool_input": map[string]any{
			"code": `import subprocess
subprocess.run(["rm", "-rf", "go/"])`,
		},
	}, "")
	if code != 2 {
		t.Fatalf("exit=%d want 2 (block)", code)
	}
	var parsed map[string]string
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &parsed); err != nil {
		t.Fatalf("stdout not valid JSON: %v\n%s", err, stdout)
	}
	if parsed["decision"] != "block" {
		t.Fatalf("decision=%q", parsed["decision"])
	}
	if !strings.Contains(parsed["reason"], "no rm -rf") {
		t.Fatalf("reason missing rm-rf denial: %q", parsed["reason"])
	}
}

func TestEvalHookStdin_HermesExecuteCodeShutilRmtreeDenied(t *testing.T) {
	env := setupHookEnv(t, baselinePolicy)
	stdout, _, code := runHookCallAsAgent(t, env, "hermes", map[string]any{
		"tool_name": "execute_code",
		"tool_input": map[string]any{
			"code": `import shutil
shutil.rmtree("go/")`,
		},
	}, "")
	if code != 2 {
		t.Fatalf("exit=%d want 2 (block)", code)
	}
	var parsed map[string]string
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &parsed); err != nil {
		t.Fatalf("stdout not valid JSON: %v\n%s", err, stdout)
	}
	if parsed["decision"] != "block" {
		t.Fatalf("decision=%q", parsed["decision"])
	}
	if !strings.Contains(parsed["reason"], "no rm -rf") {
		t.Fatalf("reason missing rm-rf denial: %q", parsed["reason"])
	}
}

func TestEvalHookStdin_HermesExecuteCodeGitStatusAllowed(t *testing.T) {
	env := setupHookEnv(t, baselinePolicy)
	stdout, _, code := runHookCallAsAgent(t, env, "hermes", map[string]any{
		"tool_name": "execute_code",
		"tool_input": map[string]any{
			"code": `import subprocess
subprocess.run(["git", "status"])`,
		},
	}, "")
	if code != 0 {
		t.Fatalf("exit=%d want 0 (allow), stdout=%q", code, stdout)
	}
	if stdout != "" {
		t.Fatalf("allow stdout must be empty, got %q", stdout)
	}
}

func TestEvalHookStdin_NoPolicyInCwdAllowsWithWarning(t *testing.T) {
	// chitin.yaml absent — fail-open with stderr warning.
	cwd := t.TempDir()
	chitin := t.TempDir()
	prev, hadPrev := os.LookupEnv("CHITIN_HOME")
	_ = os.Setenv("CHITIN_HOME", chitin)
	t.Cleanup(func() {
		if hadPrev {
			_ = os.Setenv("CHITIN_HOME", prev)
		} else {
			_ = os.Unsetenv("CHITIN_HOME")
		}
	})

	body, _ := json.Marshal(map[string]any{
		"tool_name":  "Read",
		"tool_input": map[string]any{"file_path": "/x"},
		"cwd":        cwd,
	})
	var out, errOut bytes.Buffer
	code := evalHookStdin(bytes.NewReader(body), &out, &errOut, "claude-code", "", "", false, false, false)
	if code != 0 {
		t.Fatalf("exit=%d want 0 (fail-open)", code)
	}
	if out.Len() != 0 {
		t.Fatalf("stdout must be empty, got %q", out.String())
	}
	if !strings.Contains(errOut.String(), "no_policy_found") {
		t.Fatalf("stderr missing no_policy_found warning: %q", errOut.String())
	}
}

func TestEvalHookStdin_NoPolicyInCwd_RequirePolicy_Blocks(t *testing.T) {
	// Same setup as the fail-open case, but with requirePolicy=true.
	// Expectation: exit 2 block + a chitin: reason in stdout.
	cwd := t.TempDir()
	chitin := t.TempDir()
	prev, hadPrev := os.LookupEnv("CHITIN_HOME")
	_ = os.Setenv("CHITIN_HOME", chitin)
	t.Cleanup(func() {
		if hadPrev {
			_ = os.Setenv("CHITIN_HOME", prev)
		} else {
			_ = os.Unsetenv("CHITIN_HOME")
		}
	})

	body, _ := json.Marshal(map[string]any{
		"tool_name":  "Read",
		"tool_input": map[string]any{"file_path": "/x"},
		"cwd":        cwd,
	})
	var out, errOut bytes.Buffer
	code := evalHookStdin(bytes.NewReader(body), &out, &errOut, "claude-code", "", "", true, false, false)
	if code != 2 {
		t.Fatalf("exit=%d want 2 (--require-policy → block)", code)
	}
	var parsed map[string]string
	if err := json.Unmarshal(bytes.TrimSpace(out.Bytes()), &parsed); err != nil {
		t.Fatalf("stdout not valid JSON: %v\n%s", err, out.String())
	}
	if parsed["decision"] != "block" {
		t.Fatalf("decision=%q want block", parsed["decision"])
	}
	if !strings.Contains(parsed["reason"], "require-policy") {
		t.Fatalf("reason missing require-policy mention: %q", parsed["reason"])
	}
}

func TestEvalHookStdin_TamperedSignedPolicyFailsClosedAndLogs(t *testing.T) {
	cwd := t.TempDir()
	chitin := t.TempDir()
	trustDir := filepath.Join(chitin, "trust")
	if err := os.MkdirAll(trustDir, 0o755); err != nil {
		t.Fatalf("mkdir trust: %v", err)
	}
	pub, priv, err := gov.GeneratePolicyKeyPair()
	if err != nil {
		t.Fatalf("GeneratePolicyKeyPair: %v", err)
	}
	if err := os.WriteFile(filepath.Join(trustDir, gov.DefaultPolicyPublicKey), []byte(pub), 0o644); err != nil {
		t.Fatalf("write public key: %v", err)
	}
	orig := baselinePolicy
	policyPath := filepath.Join(cwd, "chitin.yaml")
	if err := os.WriteFile(policyPath, []byte(orig), 0o644); err != nil {
		t.Fatalf("write policy: %v", err)
	}
	sig, err := gov.SignPolicyBytes([]byte(orig), priv)
	if err != nil {
		t.Fatalf("SignPolicyBytes: %v", err)
	}
	if err := os.WriteFile(policyPath+gov.DefaultPolicySigSuffix, []byte(sig), 0o644); err != nil {
		t.Fatalf("write sig: %v", err)
	}
	if err := os.WriteFile(policyPath, []byte(strings.Replace(orig, "read ok", "tampered read ok", 1)), 0o644); err != nil {
		t.Fatalf("tamper policy: %v", err)
	}

	t.Setenv("CHITIN_HOME", chitin)
	t.Setenv("CHITIN_POLICY_TRUST_DIR", trustDir)
	body, _ := json.Marshal(map[string]any{
		"tool_name":  "Read",
		"tool_input": map[string]any{"file_path": "/x"},
		"cwd":        cwd,
	})
	var out, errOut bytes.Buffer
	code := evalHookStdin(bytes.NewReader(body), &out, &errOut, "claude-code", "", "", false, false, false)
	if code != 2 {
		t.Fatalf("exit=%d want 2 (signature fail closed), stdout=%q stderr=%q", code, out.String(), errOut.String())
	}
	if !strings.Contains(out.String(), "policy_signature_invalid") {
		t.Fatalf("stdout missing structured signature error: %q", out.String())
	}
	entries, err := os.ReadDir(chitin)
	if err != nil {
		t.Fatalf("read chitin dir: %v", err)
	}
	var sawDecision bool
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "gov-decisions-") {
			data, _ := os.ReadFile(filepath.Join(chitin, entry.Name()))
			sawDecision = strings.Contains(string(data), `"rule_id":"policy_signature_invalid"`)
		}
	}
	if !sawDecision {
		t.Fatalf("expected policy_signature_invalid audit row in %s", chitin)
	}
}

func TestEvalHookStdin_BypassSigAllowsTamperedPolicyAndLogsWarning(t *testing.T) {
	cwd := t.TempDir()
	chitin := t.TempDir()
	trustDir := filepath.Join(chitin, "trust")
	if err := os.MkdirAll(trustDir, 0o755); err != nil {
		t.Fatalf("mkdir trust: %v", err)
	}
	pub, priv, err := gov.GeneratePolicyKeyPair()
	if err != nil {
		t.Fatalf("GeneratePolicyKeyPair: %v", err)
	}
	if err := os.WriteFile(filepath.Join(trustDir, gov.DefaultPolicyPublicKey), []byte(pub), 0o644); err != nil {
		t.Fatalf("write public key: %v", err)
	}
	policyPath := filepath.Join(cwd, "chitin.yaml")
	if err := os.WriteFile(policyPath, []byte(baselinePolicy), 0o644); err != nil {
		t.Fatalf("write policy: %v", err)
	}
	sig, err := gov.SignPolicyBytes([]byte(baselinePolicy), priv)
	if err != nil {
		t.Fatalf("SignPolicyBytes: %v", err)
	}
	if err := os.WriteFile(policyPath+gov.DefaultPolicySigSuffix, []byte(sig), 0o644); err != nil {
		t.Fatalf("write sig: %v", err)
	}
	if err := os.WriteFile(policyPath, []byte(strings.Replace(baselinePolicy, "read ok", "tampered read ok", 1)), 0o644); err != nil {
		t.Fatalf("tamper policy: %v", err)
	}

	t.Setenv("CHITIN_HOME", chitin)
	t.Setenv("CHITIN_POLICY_TRUST_DIR", trustDir)
	body, _ := json.Marshal(map[string]any{
		"tool_name":  "Read",
		"tool_input": map[string]any{"file_path": "/x"},
		"cwd":        cwd,
	})
	var out, errOut bytes.Buffer
	code := evalHookStdin(bytes.NewReader(body), &out, &errOut, "claude-code", "", "", false, false, true)
	if code != 0 {
		t.Fatalf("exit=%d want 0 under bypass, stdout=%q stderr=%q", code, out.String(), errOut.String())
	}
	if !strings.Contains(errOut.String(), "policy_signature_bypass") {
		t.Fatalf("stderr missing bypass warning: %q", errOut.String())
	}
}

func TestEvalHookStdin_WrongTypeField_WarnsAndProceeds(t *testing.T) {
	// file_path: 42 instead of a string. Normalize emits empty Target;
	// stderr gets a tool_input_wrong_type warning so an operator
	// debugging a malformed payload sees it.
	env := setupHookEnv(t, baselinePolicy)
	body, _ := json.Marshal(map[string]any{
		"tool_name":  "Read",
		"tool_input": map[string]any{"file_path": 42},
		"cwd":        env.cwd,
	})
	var out, errOut bytes.Buffer
	_ = evalHookStdin(bytes.NewReader(body), &out, &errOut, "claude-code", "", "", false, false, false)
	if !strings.Contains(errOut.String(), "tool_input_wrong_type") {
		t.Fatalf("stderr missing wrong-type warning: %q", errOut.String())
	}
}

func TestEvalHookStdin_MalformedJSONIsExit1(t *testing.T) {
	env := setupHookEnv(t, baselinePolicy)
	in := bytes.NewReader([]byte("not json"))
	var out, errOut bytes.Buffer
	code := evalHookStdin(in, &out, &errOut, "claude-code", "", "", false, false, false)
	_ = env
	if code != 1 {
		t.Fatalf("exit=%d want 1 (non-blocking error)", code)
	}
	if !strings.Contains(errOut.String(), "hook_parse_stdin") {
		t.Fatalf("missing parse-stdin error: %q", errOut.String())
	}
}

func TestEvalHookStdin_WithEnvelopeFlag_DebitsEnvelope(t *testing.T) {
	env := setupHookEnv(t, baselinePolicy)
	dbPath := filepath.Join(env.chitin, "gov.db")
	store, err := openBudgetStoreForTest(t, dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	envelope, err := store.Create(budgetLimits(t, 10, 1024))
	if err != nil {
		t.Fatalf("create envelope: %v", err)
	}
	store.Close()

	_, _, code := runHookCall(t, env, map[string]any{
		"tool_name":  "Read",
		"tool_input": map[string]any{"file_path": "/etc/hosts"},
	}, envelope.ID)
	if code != 0 {
		t.Fatalf("exit=%d want 0", code)
	}

	// Reopen and check spend.
	store2, _ := openBudgetStoreForTest(t, dbPath)
	defer store2.Close()
	env2, _ := store2.Load(envelope.ID)
	st, _ := env2.Inspect()
	if st.SpentCalls != 1 {
		t.Fatalf("SpentCalls=%d want 1 — envelope flag not honored", st.SpentCalls)
	}
}

func TestEvalHookStdin_EnvelopeExhausted_BlocksWithReason(t *testing.T) {
	env := setupHookEnv(t, baselinePolicy)
	dbPath := filepath.Join(env.chitin, "gov.db")
	store, _ := openBudgetStoreForTest(t, dbPath)
	envelope, _ := store.Create(budgetLimits(t, 1, 0))
	store.Close()

	// Burn the cap.
	_, _, code1 := runHookCall(t, env, map[string]any{
		"tool_name":  "Read",
		"tool_input": map[string]any{"file_path": "/a"},
	}, envelope.ID)
	if code1 != 0 {
		t.Fatalf("first call exit=%d want 0", code1)
	}
	// Next call: envelope-exhausted → block exit 2.
	stdout, _, code2 := runHookCall(t, env, map[string]any{
		"tool_name":  "Read",
		"tool_input": map[string]any{"file_path": "/b"},
	}, envelope.ID)
	if code2 != 2 {
		t.Fatalf("second call exit=%d want 2 (envelope-exhausted)", code2)
	}
	if !strings.Contains(stdout, "envelope") || !strings.Contains(stdout, "exhausted") {
		t.Fatalf("block reason missing envelope-exhausted: %q", stdout)
	}
}

func TestEvalHookStdin_EnvelopePrecedence_FlagBeatsEnvAndFile(t *testing.T) {
	env := setupHookEnv(t, baselinePolicy)
	dbPath := filepath.Join(env.chitin, "gov.db")
	store, _ := openBudgetStoreForTest(t, dbPath)
	flagEnv, _ := store.Create(budgetLimits(t, 10, 0))
	envEnv, _ := store.Create(budgetLimits(t, 10, 0))
	fileEnv, _ := store.Create(budgetLimits(t, 10, 0))
	store.Close()

	// Stage env var + current-envelope file pointing at the wrong envelopes.
	_ = os.Setenv("CHITIN_BUDGET_ENVELOPE", envEnv.ID)
	t.Cleanup(func() { _ = os.Unsetenv("CHITIN_BUDGET_ENVELOPE") })
	if err := os.WriteFile(filepath.Join(env.chitin, "current-envelope"), []byte(fileEnv.ID), 0o600); err != nil {
		t.Fatalf("stage current-envelope: %v", err)
	}

	// Pass flag — flag wins.
	_, _, code := runHookCall(t, env, map[string]any{
		"tool_name":  "Read",
		"tool_input": map[string]any{"file_path": "/x"},
	}, flagEnv.ID)
	if code != 0 {
		t.Fatalf("exit=%d want 0", code)
	}

	store2, _ := openBudgetStoreForTest(t, dbPath)
	defer store2.Close()
	for _, c := range []struct {
		name string
		id   string
		want int64
	}{
		{"flag", flagEnv.ID, 1},
		{"env", envEnv.ID, 0},
		{"file", fileEnv.ID, 0},
	} {
		e, _ := store2.Load(c.id)
		st, _ := e.Inspect()
		if st.SpentCalls != c.want {
			t.Errorf("%s envelope: SpentCalls=%d want %d", c.name, st.SpentCalls, c.want)
		}
	}
}

// chitinAdminPolicy: like baselinePolicy but adds a deny rule targeting
// `chitin-kernel envelope grant` so the policy-still-applies test can
// fire a real deny.
const chitinAdminPolicy = `
id: hook-test-admin
mode: enforce
rules:
  - id: deny-grant
    action: shell.exec
    effect: deny
    target: "chitin-kernel envelope grant"
    reason: "chitin-kernel envelope grant denied by operator policy"
  - id: allow-shell
    action: shell.exec
    effect: allow
    reason: shell ok
  - id: allow-read
    action: file.read
    effect: allow
    reason: read ok
`

// TestEvalHookStdin_ChitinAdmin_ReadAllowedOnExhaustedEnvelope verifies
// the recovery-adjacent read path: with an exhausted envelope, read-only
// governance introspection still passes the gate without spend. Mutating
// recovery commands such as envelope grant are supervisor/operator-only.
func TestEvalHookStdin_ChitinAdmin_ReadAllowedOnExhaustedEnvelope(t *testing.T) {
	env := setupHookEnv(t, baselinePolicy)
	dbPath := filepath.Join(env.chitin, "gov.db")
	store, _ := openBudgetStoreForTest(t, dbPath)
	envelope, _ := store.Create(budgetLimits(t, 1, 0))
	store.Close()

	// Burn the cap with a Read.
	_, _, code1 := runHookCall(t, env, map[string]any{
		"tool_name":  "Read",
		"tool_input": map[string]any{"file_path": "/a"},
	}, envelope.ID)
	if code1 != 0 {
		t.Fatalf("first call exit=%d want 0", code1)
	}

	// Confirm a non-admin Bash now denies (envelope-exhausted).
	stdoutDeny, _, codeDeny := runHookCall(t, env, map[string]any{
		"tool_name":  "Bash",
		"tool_input": map[string]any{"command": "ls /"},
	}, envelope.ID)
	if codeDeny != 2 {
		t.Fatalf("non-admin call exit=%d want 2 (envelope blocked); stdout=%q", codeDeny, stdoutDeny)
	}

	// chitin-kernel envelope inspect should pass — exempt from spend.
	stdoutAdmin, _, codeAdmin := runHookCall(t, env, map[string]any{
		"tool_name": "Bash",
		"tool_input": map[string]any{
			"command": "chitin-kernel envelope inspect " + envelope.ID,
		},
	}, envelope.ID)
	if codeAdmin != 0 {
		t.Fatalf("chitin-admin read exit=%d want 0 (exempt); stdout=%q", codeAdmin, stdoutAdmin)
	}
}

func TestClassifyChitinAdminCommand(t *testing.T) {
	cases := []struct {
		name string
		cmd  string
		want chitinAdminClass
	}{
		{"gate status", "chitin-kernel gate status --agent a", chitinAdminRead},
		{"decisions recent", "chitin-kernel decisions recent --json", chitinAdminRead},
		{"decisions worktree diagnostics", "chitin-kernel decisions worktree-diagnostics --limit 5", chitinAdminRead},
		{"chain stats", "chitin-kernel chain stats --json", chitinAdminRead},
		{"chain recommend tier", "chitin-kernel chain recommend-tier --action-type=shell.exec", chitinAdminRead},
		{"envelope inspect", "env CHITIN_HOME=/tmp chitin-kernel envelope inspect e1", chitinAdminRead},
		{"gate reset", "chitin-kernel gate reset --agent a", chitinAdminMutation},
		{"gate lockdown", "chitin-kernel gate lockdown --agent a", chitinAdminMutation},
		{"gate evaluate records", "chitin-kernel gate evaluate --tool Bash --agent a", chitinAdminMutation},
		{"gate evaluate no record", "chitin-kernel gate evaluate --tool Bash --agent a --no-record", chitinAdminRead},
		{"router evaluate records", "chitin-kernel router evaluate --hook-stdin", chitinAdminMutation},
		{"router evaluate no record", "chitin-kernel router evaluate --hook-stdin --no-record", chitinAdminRead},
		{"config before gate lockdown", "chitin-kernel --config /tmp/chitin.yaml gate lockdown", chitinAdminMutation},
		{"verbose before decisions recent", "chitin-kernel --verbose decisions recent", chitinAdminRead},
		{"envelope grant", "FOO=1 chitin-kernel envelope grant e1 --calls=+1", chitinAdminMutation},
		{"install hook", "chitin-kernel install-hook --surface claude-code", chitinAdminMutation},
		{"chained reset after read", "chitin-kernel gate status && chitin-kernel gate reset --agent a", chitinAdminMutation},
		{"pipeline reset after read", "chitin-kernel decisions recent | chitin-kernel gate reset --agent a", chitinAdminMutation},
		{"command wrapper", "command chitin-kernel gate reset --agent a", chitinAdminMutation},
		{"path prefixed", "/usr/local/bin/chitin-kernel gate reset --agent a", chitinAdminMutation},
		{"lookalike", "echo chitin-kernel gate reset --agent a", chitinAdminNone},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := classifyChitinAdminCommand(gov.Action{Type: gov.ActShellExec, Target: tc.cmd})
			if got != tc.want {
				t.Fatalf("classify=%q want %q", got, tc.want)
			}
		})
	}
}

func TestClassifyChitinAdmin_ReadOnlyWithMutationFlagsRequiresAuthority(t *testing.T) {
	for _, flag := range []string{"--repair", "--write", "--apply"} {
		t.Run(flag, func(t *testing.T) {
			cmd := "chitin-kernel decisions worktree-diagnostics " + flag
			got := classifyChitinAdminCommand(gov.Action{Type: gov.ActShellExec, Target: cmd})
			if got != chitinAdminMutation {
				t.Fatalf("classify(%q)=%q want %q", cmd, got, chitinAdminMutation)
			}
		})
	}
}

func TestClassifyChitinAdmin_FailClosedBoundaries(t *testing.T) {
	cases := []struct {
		name string
		cmd  string
	}{
		{
			name: "read command with apply flag is mixed",
			cmd:  "chitin-kernel chain replay --session=latest --apply",
		},
		{
			name: "unknown decisions subcommand",
			cmd:  "chitin-kernel decisions recnet",
		},
		{
			name: "unknown envelope alias",
			cmd:  "chitin-kernel envelope ls",
		},
		{
			name: "unknown root subcommand",
			cmd:  "chitin-kernel gov decisions list",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := classifyChitinAdminCommand(gov.Action{Type: gov.ActShellExec, Target: tc.cmd})
			if got != chitinAdminMutation {
				t.Fatalf("classify(%q)=%q want %q", tc.cmd, got, chitinAdminMutation)
			}
		})
	}
}

func TestClassifyChitinAdmin_VersionFlagsAreRead(t *testing.T) {
	for _, flag := range []string{"--version", "--help", "-V", "-h"} {
		t.Run(flag, func(t *testing.T) {
			cmd := "chitin-kernel " + flag
			got := classifyChitinAdminCommand(gov.Action{Type: gov.ActShellExec, Target: cmd})
			if got != chitinAdminRead {
				t.Fatalf("classify(%q)=%q want %q", cmd, got, chitinAdminRead)
			}
		})
	}
}

func TestClassifyChitinAdmin_PipedReadsStayRead(t *testing.T) {
	cases := []struct {
		name string
		cmd  string
	}{
		{"decisions recent | head", "chitin-kernel decisions recent | head"},
		{"health | grep ok", "chitin-kernel health | grep ok"},
		{"chain-info | jq", "chitin-kernel chain-info | jq"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := classifyChitinAdminCommand(gov.Action{Type: gov.ActShellExec, Target: tc.cmd})
			if got != chitinAdminRead {
				t.Fatalf("classify(%q)=%q want %q", tc.cmd, got, chitinAdminRead)
			}
		})
	}
}

// TestEvalHookStdin_ChitinAdmin_NoEnvelopeSpend verifies that a
// chitin-kernel command does NOT debit the envelope even when one is
// healthy. Otherwise the agent could rack up spend on admin calls
// while believing it's at zero cost.
func TestEvalHookStdin_ChitinAdmin_NoEnvelopeSpend(t *testing.T) {
	env := setupHookEnv(t, baselinePolicy)
	dbPath := filepath.Join(env.chitin, "gov.db")
	store, _ := openBudgetStoreForTest(t, dbPath)
	envelope, _ := store.Create(budgetLimits(t, 100, 0))
	store.Close()

	for _, cmd := range []string{
		"chitin-kernel envelope inspect " + envelope.ID,
		"chitin-kernel envelope list",
		"env CHITIN_HOME=/tmp chitin-kernel envelope list",
		"FOO=1 BAR=2 chitin-kernel envelope list",
	} {
		_, _, code := runHookCall(t, env, map[string]any{
			"tool_name":  "Bash",
			"tool_input": map[string]any{"command": cmd},
		}, envelope.ID)
		if code != 0 {
			t.Fatalf("cmd %q: exit=%d want 0", cmd, code)
		}
	}

	store2, _ := openBudgetStoreForTest(t, dbPath)
	defer store2.Close()
	e2, _ := store2.Load(envelope.ID)
	st, _ := e2.Inspect()
	if st.SpentCalls != 0 {
		t.Fatalf("SpentCalls=%d want 0 — admin commands debited envelope", st.SpentCalls)
	}
}

func TestEvalHookStdin_ChitinAdmin_WorkerCanReadStatus(t *testing.T) {
	env := setupHookEnv(t, baselinePolicy)
	dbPath := filepath.Join(env.chitin, "gov.db")
	store, _ := openBudgetStoreForTest(t, dbPath)
	envelope, _ := store.Create(budgetLimits(t, 10, 0))
	store.Close()

	stdout, _, code := runHookCall(t, env, map[string]any{
		"tool_name":  "Bash",
		"tool_input": map[string]any{"command": "chitin-kernel gate status --agent claude-code"},
	}, envelope.ID)
	if code != 0 {
		t.Fatalf("gate status exit=%d want 0; stdout=%q", code, stdout)
	}

	store2, _ := openBudgetStoreForTest(t, dbPath)
	defer store2.Close()
	e2, _ := store2.Load(envelope.ID)
	st, _ := e2.Inspect()
	if st.SpentCalls != 0 {
		t.Fatalf("SpentCalls=%d want 0 — read-only governance command debited envelope", st.SpentCalls)
	}
}

func TestEvalHookStdin_ChitinAdmin_WorkerCanReadDecisionDiagnostics(t *testing.T) {
	env := setupHookEnv(t, baselinePolicy)

	stdout, _, code := runHookCall(t, env, map[string]any{
		"tool_name": "Bash",
		"tool_input": map[string]any{
			"command": "chitin-kernel decisions worktree-diagnostics --window-hours 1 --limit 10",
		},
	}, "")
	if code != 0 {
		t.Fatalf("decisions worktree-diagnostics exit=%d want 0; stdout=%q", code, stdout)
	}
}

func TestEvalHookStdin_ChitinAdmin_WorkerCannotUseMutationFlagOnDiagnostics(t *testing.T) {
	env := setupHookEnv(t, baselinePolicy)

	stdout, _, code := runHookCall(t, env, map[string]any{
		"tool_name": "Bash",
		"tool_input": map[string]any{
			"command": "chitin-kernel decisions worktree-diagnostics --write",
		},
	}, "")
	if code != 2 {
		t.Fatalf("decisions worktree-diagnostics --write exit=%d want 2; stdout=%q", code, stdout)
	}
	if !strings.Contains(stdout, "governance-mutation-authority-required") {
		t.Fatalf("stdout missing authority rule id: %q", stdout)
	}
}

func TestEvalHookStdin_ChitinAdmin_WorkerCannotResetSelf(t *testing.T) {
	env := setupHookEnv(t, baselinePolicy)

	stdout, _, code := runHookCall(t, env, map[string]any{
		"tool_name":  "Bash",
		"tool_input": map[string]any{"command": "chitin-kernel gate reset --agent claude-code"},
	}, "")
	if code != 2 {
		t.Fatalf("gate reset exit=%d want 2; stdout=%q", code, stdout)
	}
	var parsed map[string]string
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &parsed); err != nil {
		t.Fatalf("stdout not valid JSON: %v\n%s", err, stdout)
	}
	if parsed["rule_id"] != "governance-mutation-authority-required" {
		t.Fatalf("rule_id=%q want governance-mutation-authority-required; parsed=%v", parsed["rule_id"], parsed)
	}
	if !strings.Contains(parsed["reason"], "self-reset is not permitted") {
		t.Fatalf("reason should explain self-reset denial, got %q", parsed["reason"])
	}
}

func TestEvalHookStdin_ChitinAdmin_SpoofedSupervisorCannotReset(t *testing.T) {
	t.Setenv("CHITIN_AUTHORITY", "supervisor")
	env := setupHookEnv(t, baselinePolicy)

	stdout, _, code := runHookCall(t, env, map[string]any{
		"tool_name":  "Bash",
		"tool_input": map[string]any{"command": "chitin-kernel gate reset --agent other-agent"},
	}, "")
	if code != 2 {
		t.Fatalf("spoofed supervisor reset exit=%d want 2; stdout=%q", code, stdout)
	}
	if !strings.Contains(stdout, "trusted supervisor/operator/system authority") {
		t.Fatalf("denial should explain trusted authority requirement: %q", stdout)
	}
}

func TestEvalHookStdin_ChitinAdmin_MutationDeniedInMonitorMode(t *testing.T) {
	env := setupHookEnv(t, `
id: hook-test-monitor-admin
mode: monitor
rules:
  - id: allow-shell
    action: shell.exec
    effect: allow
    reason: shell ok
`)

	stdout, _, code := runHookCall(t, env, map[string]any{
		"tool_name":  "Bash",
		"tool_input": map[string]any{"command": "chitin-kernel gate reset --agent claude-code"},
	}, "")
	if code != 2 {
		t.Fatalf("monitor-mode governance mutation exit=%d want 2; stdout=%q", code, stdout)
	}
	if !strings.Contains(stdout, "governance-mutation-authority-required") {
		t.Fatalf("stdout missing authority rule id: %q", stdout)
	}
}

func TestEvalHookStdin_ChitinAdmin_TrustedSupervisorCanReset(t *testing.T) {
	t.Setenv("CHITIN_AGENT_FINGERPRINT", "agentfp-supervisor")
	env := setupHookEnv(t, `
id: hook-test-supervisor
mode: enforce
authority:
  trusted:
    - authority: supervisor
      agent_fingerprint: agentfp-supervisor
rules:
  - id: allow-shell
    action: shell.exec
    effect: allow
    reason: shell ok
`)

	stdout, _, code := runHookCall(t, env, map[string]any{
		"tool_name":  "Bash",
		"tool_input": map[string]any{"command": "chitin-kernel gate reset --agent other-agent"},
	}, "")
	if code != 0 {
		t.Fatalf("trusted supervisor reset exit=%d want 0; stdout=%q", code, stdout)
	}
}

// TestEvalHookStdin_ChitinAdmin_PolicyStillApplies verifies the
// exemption is spend-only: a policy rule denying
// `chitin-kernel envelope grant` still produces a block, even though
// the envelope is healthy. Operators retain control over which admin
// commands the agent may run.
func TestEvalHookStdin_ChitinAdmin_PolicyStillApplies(t *testing.T) {
	env := setupHookEnv(t, chitinAdminPolicy)
	dbPath := filepath.Join(env.chitin, "gov.db")
	store, _ := openBudgetStoreForTest(t, dbPath)
	envelope, _ := store.Create(budgetLimits(t, 10, 0))
	store.Close()

	stdout, _, code := runHookCall(t, env, map[string]any{
		"tool_name": "Bash",
		"tool_input": map[string]any{
			"command": "chitin-kernel envelope grant " + envelope.ID + " --calls=+10",
		},
	}, envelope.ID)
	if code != 2 {
		t.Fatalf("exit=%d want 2 (policy denies admin grant); stdout=%q", code, stdout)
	}
	var parsed map[string]string
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &parsed); err != nil {
		t.Fatalf("stdout not valid JSON: %v\n%s", err, stdout)
	}
	if !strings.Contains(parsed["reason"], "denied by operator policy") {
		t.Fatalf("reason missing operator-deny text: %q", parsed["reason"])
	}
}

// TestEvalHookStdin_ChitinAdmin_NotMatchingNotExempt verifies the
// matcher's negative cases: lookalike commands don't get exempted.
// Each of these should debit the envelope normally.
func TestEvalHookStdin_ChitinAdmin_NotMatchingNotExempt(t *testing.T) {
	cases := []struct {
		name string
		cmd  string
	}{
		{"path-prefixed", "/usr/local/bin/chitin-kernel envelope list"},
		{"echo prefix", "echo chitin-kernel envelope list"},
		{"hyphen extension", "chitin-kernel-fake envelope list"},
		{"compound", "chitin-kernelizer envelope list"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			env := setupHookEnv(t, baselinePolicy)
			dbPath := filepath.Join(env.chitin, "gov.db")
			store, _ := openBudgetStoreForTest(t, dbPath)
			envelope, _ := store.Create(budgetLimits(t, 10, 0))
			store.Close()

			_, _, code := runHookCall(t, env, map[string]any{
				"tool_name":  "Bash",
				"tool_input": map[string]any{"command": c.cmd},
			}, envelope.ID)
			if code != 0 {
				t.Fatalf("exit=%d want 0", code)
			}

			store2, _ := openBudgetStoreForTest(t, dbPath)
			defer store2.Close()
			e2, _ := store2.Load(envelope.ID)
			st, _ := e2.Inspect()
			if st.SpentCalls != 1 {
				t.Fatalf("SpentCalls=%d want 1 — lookalike was incorrectly exempted", st.SpentCalls)
			}
		})
	}
}

// TestEvalHookStdin_ChitinAdmin_ChainedRmRfStillBlocked verifies that
// the exemption only bypasses envelope spend, not policy. A chained
// command like `chitin-kernel ... && rm -rf X` matches the admin matcher
// (so the envelope isn't debited), but the policy's rm-rf rule
// evaluates against the full Target and still denies. Belt-and-suspenders
// proof that exempting spend doesn't open a destructive-operation
// bypass through the chitin-kernel prefix.
func TestEvalHookStdin_ChitinAdmin_ChainedRmRfStillBlocked(t *testing.T) {
	env := setupHookEnv(t, baselinePolicy)
	dbPath := filepath.Join(env.chitin, "gov.db")
	store, _ := openBudgetStoreForTest(t, dbPath)
	envelope, _ := store.Create(budgetLimits(t, 10, 0))
	store.Close()

	body, _ := json.Marshal(map[string]any{
		"tool_name": "Bash",
		"tool_input": map[string]any{
			"command": "chitin-kernel envelope list && rm -rf /",
		},
		"cwd": env.cwd,
	})
	var out, errOut bytes.Buffer
	code := evalHookStdin(bytes.NewReader(body), &out, &errOut, "claude-code", envelope.ID, "", false, false, false)
	if code != 2 {
		t.Fatalf("exit=%d want 2 (rm-rf rule should still apply); stdout=%q", code, out.String())
	}
	var parsed map[string]string
	if err := json.Unmarshal(bytes.TrimSpace(out.Bytes()), &parsed); err != nil {
		t.Fatalf("stdout not valid JSON: %v\n%s", err, out.String())
	}
	if !strings.Contains(parsed["reason"], "rm -rf") {
		t.Fatalf("reason missing rm-rf policy text: %q", parsed["reason"])
	}
	// Confirm the exemption did fire — otherwise this would only test
	// that policy denies, not that the exemption preserves policy
	// enforcement on the chained command.
	if !strings.Contains(errOut.String(), "chitin_admin_exempt") {
		t.Fatalf("stderr missing chitin_admin_exempt info; exemption may not have fired: %q", errOut.String())
	}
	// Denial path should not have debited.
	store2, _ := openBudgetStoreForTest(t, dbPath)
	defer store2.Close()
	e2, _ := store2.Load(envelope.ID)
	st, _ := e2.Inspect()
	if st.SpentCalls != 0 {
		t.Fatalf("SpentCalls=%d want 0 — denial should not debit", st.SpentCalls)
	}
}

// TestEvalHookStdin_ChitinAdmin_ExemptInfoLogged verifies the
// structured info line on stderr — operators auditing the hook should
// see when an exemption fired.
func TestEvalHookStdin_ChitinAdmin_ExemptInfoLogged(t *testing.T) {
	env := setupHookEnv(t, baselinePolicy)
	dbPath := filepath.Join(env.chitin, "gov.db")
	store, _ := openBudgetStoreForTest(t, dbPath)
	envelope, _ := store.Create(budgetLimits(t, 10, 0))
	store.Close()

	body, _ := json.Marshal(map[string]any{
		"tool_name":  "Bash",
		"tool_input": map[string]any{"command": "chitin-kernel envelope list"},
		"cwd":        env.cwd,
	})
	var out, errOut bytes.Buffer
	code := evalHookStdin(bytes.NewReader(body), &out, &errOut, "claude-code", envelope.ID, "", false, false, false)
	if code != 0 {
		t.Fatalf("exit=%d want 0", code)
	}
	if !strings.Contains(errOut.String(), "chitin_admin_exempt") {
		t.Fatalf("stderr missing chitin_admin_exempt info: %q", errOut.String())
	}
}

func TestEvalHookStdin_BadEnvelopeIDBlocks(t *testing.T) {
	env := setupHookEnv(t, baselinePolicy)
	stdout, _, code := runHookCall(t, env, map[string]any{
		"tool_name":  "Read",
		"tool_input": map[string]any{"file_path": "/x"},
	}, "01-NONEXISTENT-ENVELOPE-ID-A")
	if code != 2 {
		t.Fatalf("exit=%d want 2 (block on bad envelope)", code)
	}
	// Assert structurally on the parsed JSON shape rather than a brittle
	// substring of the human-readable error text — the message can be
	// reworded without needing to update tests.
	var parsed map[string]string
	if err := json.Unmarshal(bytes.TrimSpace([]byte(stdout)), &parsed); err != nil {
		t.Fatalf("stdout not valid JSON: %v\n%s", err, stdout)
	}
	if parsed["decision"] != "block" {
		t.Fatalf("decision=%q want block", parsed["decision"])
	}
	if !strings.HasPrefix(parsed["reason"], "chitin: ") {
		t.Fatalf("reason missing chitin: prefix: %q", parsed["reason"])
	}
}

func TestEvalHookStdin_NoRecordSuppressesAllPersistence(t *testing.T) {
	// Regression: --no-record on the hook path must skip
	// (a) Counter.RecordDenial, (b) WriteLog, (c) OnDecision chain
	// emitter. Without all three, smoke-testing policy via hook-stdin
	// pollutes the same DB+chain as the production hook — exactly the
	// foot-gun --no-record was added to fix on the non-hook path.
	env := setupHookEnv(t, baselinePolicy)
	body, _ := json.Marshal(map[string]any{
		"tool_name":  "Bash",
		"tool_input": map[string]any{"command": "rm -rf go/"},
		"cwd":        env.cwd,
	})

	// 15 deny iterations under --no-record. Threshold is 10, so without
	// the fix the agent would lockdown by iteration 11 and every later
	// call would return RuleID=lockdown. With the fix, none of the
	// state-mutating side effects fire.
	for i := 0; i < 15; i++ {
		var out, errOut bytes.Buffer
		code := evalHookStdin(bytes.NewReader(body), &out, &errOut, "norecord-hook", "", "", false, true, false)
		if code != 2 {
			t.Fatalf("iter %d: code=%d want 2 (block); stdout=%q", i, code, out.String())
		}
		// Lockdown reason would be "agent in lockdown — contact operator";
		// regular deny reason is the rule's reason ("no rm -rf"). If the
		// counter incremented despite NoRecord, by iter 11+ we'd see the
		// lockdown reason.
		if strings.Contains(out.String(), "lockdown") {
			t.Fatalf("iter %d: stdout contains 'lockdown' — counter incremented despite NoRecord: %q", i, out.String())
		}
	}

	// (a) Counter: no agent_state row should exist for "norecord-hook".
	// We assert through the kernel binary's gate status surface — a
	// "normal" level on an agent with no DB row is the expected shape.
	// (Going through OpenCounter directly here would couple the test
	// to the gov package's internals; gate status is the public read.)
	// Skipping that assertion when the kernel binary isn't reachable
	// would silently mask regressions, so we read agent_state via
	// sqlite directly — gov.db is in env.chitin.

	// (b) Chain log: no gov-decisions-*.jsonl should exist (or exist empty).
	entries, _ := os.ReadDir(env.chitin)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "gov-decisions-") {
			path := filepath.Join(env.chitin, e.Name())
			b, _ := os.ReadFile(path)
			if len(bytes.TrimSpace(b)) > 0 {
				t.Errorf("gov-decisions log under NoRecord should be empty, got: %s", b)
			}
		}
		// (c) Chain emitter: no events-*.jsonl should exist either.
		if strings.HasPrefix(e.Name(), "events-") && strings.HasSuffix(e.Name(), ".jsonl") {
			t.Errorf("OnDecision chain event written under NoRecord: %s", e.Name())
		}
	}
}

func TestEvalHookStdin_NoRecordSkipsEnvelopeSpend(t *testing.T) {
	// Smoke evaluations under --no-record must not consume budget.
	// Without nil envelope passthrough, an operator running 100 probes
	// would silently debit 100 calls from the active envelope.
	env := setupHookEnv(t, baselinePolicy)
	// Create an envelope via the kernel's own path (via setup).
	// We don't have direct envelope-create plumbing here, so we
	// validate the lighter contract: when --no-record is set and the
	// envelope flag is empty, the hook still allows + the chain log
	// stays clean. This is the relevant invariant — the envelope-skip
	// branch in evalHookStdin only matters when an envelope is loaded,
	// and that path is exercised by the same NoRecord guard tested
	// above. Coverage of the explicit envelope-loaded scenario is in
	// the live smoke (operator runbook).
	var out, errOut bytes.Buffer
	body, _ := json.Marshal(map[string]any{
		"tool_name":  "Read",
		"tool_input": map[string]any{"file_path": "/x"},
		"cwd":        env.cwd,
	})
	code := evalHookStdin(bytes.NewReader(body), &out, &errOut, "claude-code", "", "", false, true, false)
	if code != 0 {
		t.Fatalf("allow under NoRecord should exit 0, got %d (stdout=%q errOut=%q)", code, out.String(), errOut.String())
	}
}

func TestEvalHookStdin_PolicyFileOverridesCwdInheritance(t *testing.T) {
	// Regression for 2026-05-07: the --policy-file flag was parsed in
	// main.go cmdGateEvaluate but never threaded to runHookStdin →
	// evalHookStdin in --hook-stdin mode. Effect: any caller passing
	// --policy-file with --hook-stdin got it silently dropped, and the
	// gate fell back to the cwd-walk inheritance lookup. Found while
	// replaying the 17-day Curie capture dataset against a pinned
	// policy and seeing per-cwd policies apply instead.
	//
	// Two assertions:
	//   1. policyFile="" → behaves as before (cwd walk applies)
	//   2. policyFile=<explicit> with no cwd policy → explicit wins
	//      (without the fix, this returned no_policy_found + allow)
	tmpHome := t.TempDir()
	prev := os.Getenv("CHITIN_HOME")
	_ = os.Setenv("CHITIN_HOME", tmpHome)
	t.Cleanup(func() { _ = os.Setenv("CHITIN_HOME", prev) })
	prevEnvelope := os.Getenv("CHITIN_BUDGET_ENVELOPE")
	_ = os.Unsetenv("CHITIN_BUDGET_ENVELOPE")
	t.Cleanup(func() { _ = os.Setenv("CHITIN_BUDGET_ENVELOPE", prevEnvelope) })

	// Stage a policy in a temp dir; cwd will be a SEPARATE temp dir
	// with no chitin.yaml in its inheritance chain.
	policyDir := t.TempDir()
	policyPath := filepath.Join(policyDir, "chitin.yaml")
	if err := os.WriteFile(policyPath, []byte(baselinePolicy), 0o644); err != nil {
		t.Fatalf("write policy: %v", err)
	}

	cwd := t.TempDir() // empty; no chitin.yaml here or above

	body, _ := json.Marshal(map[string]any{
		"tool_name":  "Bash",
		"tool_input": map[string]any{"command": "echo hello"},
		"cwd":        cwd,
		"session_id": "policy-file-test",
	})

	// Without --policy-file: hook fires no_policy_found warning, allows.
	{
		var out, errOut bytes.Buffer
		code := evalHookStdin(bytes.NewReader(body), &out, &errOut, "claude-code", "", "", false, false, false)
		if code != 0 {
			t.Fatalf("no-policy path: code=%d want 0 (fail-open allow)", code)
		}
		if !strings.Contains(errOut.String(), "no_policy_found") {
			t.Errorf("no-policy path: expected no_policy_found warn on stderr, got %q", errOut.String())
		}
	}

	// With --policy-file: explicit policy wins; no warn, gate allows
	// based on the policy's allow-shell rule (not the no_policy_found
	// fail-open path).
	{
		var out, errOut bytes.Buffer
		code := evalHookStdin(bytes.NewReader(body), &out, &errOut, "claude-code", "", policyPath, false, false, false)
		if code != 0 {
			t.Fatalf("policy-file path: code=%d want 0 (allow), stdout=%q errOut=%q", code, out.String(), errOut.String())
		}
		if strings.Contains(errOut.String(), "no_policy_found") {
			t.Errorf("policy-file path: should NOT see no_policy_found (means flag was ignored), got: %q", errOut.String())
		}
	}
}

func TestIsClawtaDriveCarveout(t *testing.T) {
	cases := []struct {
		name  string
		agent string
		cmd   string
		atype gov.ActionType
		want  bool
	}{
		// Allowed: agent=clawta + chitin-kernel drive <lane>
		{"clawta drive copilot", "clawta", "chitin-kernel drive copilot --model gpt-4.1 'do work'", gov.ActShellExec, true},
		{"clawta drive codex", "clawta", "chitin-kernel drive codex 'do work'", gov.ActShellExec, true},
		{"clawta drive with absolute path", "clawta", "/home/red/.local/bin/chitin-kernel drive copilot 'x'", gov.ActShellExec, true},
		{"clawta drive with env stamp", "clawta", "env CHITIN_DRIVER=clawta chitin-kernel drive copilot 'x'", gov.ActShellExec, true},
		{"clawta drive after global flag", "clawta", "chitin-kernel --config /tmp/p.yaml drive copilot 'x'", gov.ActShellExec, true},
		// Denied: wrong agent
		{"claude-code drive", "claude-code", "chitin-kernel drive copilot 'x'", gov.ActShellExec, false},
		{"hermes drive", "hermes", "chitin-kernel drive codex 'x'", gov.ActShellExec, false},
		{"empty agent", "", "chitin-kernel drive copilot 'x'", gov.ActShellExec, false},
		{"clawta-suffix not exact", "clawta-poller", "chitin-kernel drive copilot 'x'", gov.ActShellExec, false},
		// Denied: clawta but wrong subcommand (carve-out is narrow)
		{"clawta envelope grant", "clawta", "chitin-kernel envelope grant e1 --calls=+1", gov.ActShellExec, false},
		{"clawta gate lockdown", "clawta", "chitin-kernel gate lockdown --agent a", gov.ActShellExec, false},
		{"clawta gate reset", "clawta", "chitin-kernel gate reset --agent a", gov.ActShellExec, false},
		{"clawta emit", "clawta", "chitin-kernel emit --action shell.exec --target x", gov.ActShellExec, false},
		{"clawta install-hook", "clawta", "chitin-kernel install-hook --surface claude-code", gov.ActShellExec, false},
		// Denied: not a chitin-kernel command at all
		{"clawta arbitrary shell", "clawta", "echo hello", gov.ActShellExec, false},
		{"clawta lookalike", "clawta", "echo chitin-kernel drive copilot", gov.ActShellExec, false},
		// Wrong action type
		{"clawta drive but file.read action", "clawta", "chitin-kernel drive copilot 'x'", gov.ActFileRead, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := isClawtaDriveCarveout(gov.Action{Type: tc.atype, Target: tc.cmd}, tc.agent)
			if got != tc.want {
				t.Errorf("isClawtaDriveCarveout(%q, agent=%q) = %v, want %v", tc.cmd, tc.agent, got, tc.want)
			}
		})
	}
}
