package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
	return &hookTestEnv{cwd: cwd, chitin: chitin}
}

func runHookCall(t *testing.T, env *hookTestEnv, payload map[string]any, envelopeFlag string) (stdout, stderr string, exitCode int) {
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
	exitCode = evalHookStdin(in, &out, &errOut, "claude-code", envelopeFlag, false)
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
    action: shell.exec
    effect: deny
    target: "rm -rf"
    reason: "no rm -rf"
    suggestion: "use git rm"
    correctedCommand: "git rm <files>"
  - id: allow-shell
    action: shell.exec
    effect: allow
    reason: shell ok
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
	code := evalHookStdin(bytes.NewReader(body), &out, &errOut, "claude-code", "", false)
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
	code := evalHookStdin(bytes.NewReader(body), &out, &errOut, "claude-code", "", true)
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
	_ = evalHookStdin(bytes.NewReader(body), &out, &errOut, "claude-code", "", false)
	if !strings.Contains(errOut.String(), "tool_input_wrong_type") {
		t.Fatalf("stderr missing wrong-type warning: %q", errOut.String())
	}
}

func TestEvalHookStdin_MalformedJSONIsExit1(t *testing.T) {
	env := setupHookEnv(t, baselinePolicy)
	in := bytes.NewReader([]byte("not json"))
	var out, errOut bytes.Buffer
	code := evalHookStdin(in, &out, &errOut, "claude-code", "", false)
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

// TestEvalHookStdin_ChitinAdmin_AllowedOnExhaustedEnvelope verifies the
// recovery path: with a 1-call envelope already exhausted by a prior
// Read, a chitin-kernel admin command still passes the gate. Without
// the exemption, the hook would deny on envelope-closed and the
// operator would have to leave the gated session to recover.
func TestEvalHookStdin_ChitinAdmin_AllowedOnExhaustedEnvelope(t *testing.T) {
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

	// chitin-kernel envelope grant should pass — exempt from spend.
	stdoutAdmin, _, codeAdmin := runHookCall(t, env, map[string]any{
		"tool_name": "Bash",
		"tool_input": map[string]any{
			"command": "chitin-kernel envelope grant " + envelope.ID + " --calls=+10",
		},
	}, envelope.ID)
	if codeAdmin != 0 {
		t.Fatalf("chitin-admin call exit=%d want 0 (exempt); stdout=%q", codeAdmin, stdoutAdmin)
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
	code := evalHookStdin(bytes.NewReader(body), &out, &errOut, "claude-code", envelope.ID, false)
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
