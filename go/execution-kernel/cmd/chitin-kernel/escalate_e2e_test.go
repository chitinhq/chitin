package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/chitinhq/chitin/go/execution-kernel/internal/gov"
)

// TestE2E_EscalateThenApprove_ReturnsAllow simulates a real PreToolUse
// hook payload that hits an escalate rule, then approves it via the
// CLI handler, then verifies evalHookStdin returns ExitAllow.
//
// This exercises the full chain: evalHookStdin -> policy match ->
// gate.Evaluate's escalate branch (Task 10) -> EscalateStore.Wait
// (Task 9) -> ListUnresolved -> ResolveApprove -> Decision returned.
func TestE2E_EscalateThenApprove_ReturnsAllow(t *testing.T) {
	cwd := t.TempDir()
	chitin := t.TempDir()

	// Stage a chitin.yaml in cwd with a rule that escalates on shell.exec.
	policy := `
id: e2e-test
mode: enforce
rules:
  - id: shell-needs-approval
    action: shell.exec
    effect: escalate
    timeout_seconds: 30
    remember_window_seconds: 0
    channel: cli-only
`
	if err := os.WriteFile(filepath.Join(cwd, "chitin.yaml"), []byte(policy), 0o644); err != nil {
		t.Fatalf("write policy: %v", err)
	}

	prevHome := os.Getenv("CHITIN_HOME")
	_ = os.Setenv("CHITIN_HOME", chitin)
	defer os.Setenv("CHITIN_HOME", prevHome)

	prevPoll := gov.WaitPollInterval
	gov.WaitPollInterval = 50 * time.Millisecond
	defer func() { gov.WaitPollInterval = prevPoll }()

	body, _ := json.Marshal(map[string]any{
		"hook_event_name": "PreToolUse",
		"tool_name":       "Bash",
		"tool_input":      map[string]any{"command": "echo hi"},
		"cwd":             cwd,
		"session_id":      "e2e-1",
	})

	type result struct {
		stdout string
		code   int
	}
	resCh := make(chan result, 1)
	go func() {
		var out, errOut bytes.Buffer
		// evalHookStdin signature: (in, out, errOut, agent, envelopeFlag,
		// policyFile, requirePolicy, noRecord). Pass empty/false for
		// optional flags so we exercise the real production path.
		code := evalHookStdin(bytes.NewReader(body), &out, &errOut, "claude-code", "", "", false, false)
		resCh <- result{out.String(), code}
	}()

	// Poll for the pending row. OpenEscalateStore now sets a 5s
	// busy_timeout before WAL/CREATE TABLE so the test thread's open
	// no longer races the kernel's schema bootstrap (replaced the
	// 200ms warmup that used to live here).
	dbPath := filepath.Join(chitin, "pending_approvals.sqlite")
	var pid string
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && pid == "" {
		store, err := gov.OpenEscalateStore(dbPath)
		if err != nil {
			time.Sleep(50 * time.Millisecond)
			continue
		}
		rows, _ := store.ListUnresolved()
		store.Close()
		if len(rows) > 0 {
			pid = rows[0].ID
		} else {
			time.Sleep(50 * time.Millisecond)
		}
	}
	if pid == "" {
		t.Fatal("no pending row appeared within 3s")
	}

	// Approve via the store directly (production would shell out to
	// `chitin-kernel pending approve`).
	store, _ := gov.OpenEscalateStore(dbPath)
	if err := store.ResolveApprove(pid, "operator-cli", 0); err != nil {
		t.Fatalf("approve: %v", err)
	}
	store.Close()

	select {
	case r := <-resCh:
		if r.code != 0 {
			t.Errorf("exit=%d want 0 (allow), stdout=%q", r.code, r.stdout)
		}
		if r.stdout != "" {
			t.Errorf("allow stdout must be empty, got %q", r.stdout)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("evalHookStdin did not return within 3s of resolution")
	}
}

// TestE2E_EscalateTimeoutDenies covers the no-response path.
func TestE2E_EscalateTimeoutDenies(t *testing.T) {
	cwd := t.TempDir()
	chitin := t.TempDir()

	policy := `
id: e2e-test
mode: enforce
rules:
  - id: shell-needs-approval
    action: shell.exec
    effect: escalate
    timeout_seconds: 30
    remember_window_seconds: 0
    channel: cli-only
`
	_ = os.WriteFile(filepath.Join(cwd, "chitin.yaml"), []byte(policy), 0o644)

	prevHome := os.Getenv("CHITIN_HOME")
	_ = os.Setenv("CHITIN_HOME", chitin)
	defer os.Setenv("CHITIN_HOME", prevHome)

	prevPoll := gov.WaitPollInterval
	gov.WaitPollInterval = 50 * time.Millisecond
	defer func() { gov.WaitPollInterval = prevPoll }()

	body, _ := json.Marshal(map[string]any{
		"hook_event_name": "PreToolUse",
		"tool_name":       "Bash",
		"tool_input":      map[string]any{"command": "echo hi"},
		"cwd":             cwd,
		"session_id":      "e2e-2",
	})

	// Override timeNow to simulate fast time passage past the deadline.
	prevTimeNow := gov.TimeNowForTest()
	defer gov.RestoreTimeNow(prevTimeNow)

	type result struct {
		stdout string
		code   int
	}
	resCh := make(chan result, 1)
	go func() {
		var out, errOut bytes.Buffer
		code := evalHookStdin(bytes.NewReader(body), &out, &errOut, "claude-code", "", "", false, false)
		resCh <- result{out.String(), code}
	}()

	// Let Wait insert the row + start polling, then advance simulated
	// time past the 30s deadline.
	time.Sleep(150 * time.Millisecond)
	gov.SetTimeNowForTest(time.Now().Add(60 * time.Second))

	select {
	case r := <-resCh:
		// Escalate timeout -> deny -> exit 2 (block). Stdout has the
		// chitin: ... block JSON.
		if r.code != 2 {
			t.Errorf("exit=%d want 2 (block on timeout); stdout=%q", r.code, r.stdout)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("evalHookStdin did not return within 3s of timeout")
	}
}
