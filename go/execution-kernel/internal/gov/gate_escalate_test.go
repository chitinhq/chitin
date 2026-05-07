package gov

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestGate_EscalateApprovedReturnsAllow exercises the integration:
// a rule with Effect=escalate causes Evaluate to insert a pending row
// and block in Wait; once the test approves the row, Evaluate returns
// with Allowed=true, RuleID="escalate-approved", and a non-empty
// EscalationID stamped on the Decision.
func TestGate_EscalateApprovedReturnsAllow(t *testing.T) {
	g, dir := newTestGate(t)
	store, err := OpenEscalateStore(filepath.Join(dir, "p.sqlite"))
	if err != nil {
		t.Fatalf("escalate store: %v", err)
	}
	defer store.Close()
	g.EscalateStore = store

	prevPoll := WaitPollInterval
	WaitPollInterval = 50 * time.Millisecond
	defer func() { WaitPollInterval = prevPoll }()

	// Override policy with an escalate rule.
	g.Policy.Rules = []Rule{{
		ID: "test-escalate", Action: ActionMatcher{"shell.exec"}, Effect: EffectEscalate,
		Reason: "needs operator",
		Escalation: &EscalateConfig{
			Channel: "cli-only", TimeoutSeconds: 5, RememberWindowSeconds: 0,
		},
	}}

	type result struct{ d Decision }
	resCh := make(chan result, 1)
	go func() {
		d := g.Evaluate(Action{Type: ActShellExec, Target: "echo"}, "agent-1", nil)
		resCh <- result{d}
	}()

	// Find the inserted pending row, approve it.
	deadline := time.Now().Add(2 * time.Second)
	var pid string
	for time.Now().Before(deadline) && pid == "" {
		rows, _ := store.ListUnresolved()
		if len(rows) > 0 {
			pid = rows[0].ID
		}
		time.Sleep(20 * time.Millisecond)
	}
	if pid == "" {
		t.Fatal("no pending row appeared within 2s")
	}
	_ = store.ResolveApprove(pid, "operator-cli", 0)

	select {
	case r := <-resCh:
		if !r.d.Allowed {
			t.Errorf("Allowed = false, want true")
		}
		if r.d.RuleID != "escalate-approved" {
			t.Errorf("RuleID = %q, want escalate-approved", r.d.RuleID)
		}
		if r.d.EscalationID == "" {
			t.Error("EscalationID empty, want set")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Evaluate did not return within 2s")
	}
}

// TestGate_EscalateRememberGrantShortCircuits verifies the
// remember_grants short-circuit path: when an unexpired grant exists
// for (rule_id, agent), Evaluate must NOT call Wait — it returns
// Allowed=true with RuleID="escalate-remember-grant" immediately. This
// is what makes the operator-approval flow tolerable for repeated
// actions in the same session.
func TestGate_EscalateRememberGrantShortCircuits(t *testing.T) {
	g, dir := newTestGate(t)
	store, err := OpenEscalateStore(filepath.Join(dir, "p.sqlite"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	defer store.Close()
	g.EscalateStore = store

	g.Policy.Rules = []Rule{{
		ID: "test-escalate", Action: ActionMatcher{"shell.exec"}, Effect: EffectEscalate,
		Reason: "needs operator",
		Escalation: &EscalateConfig{
			Channel: "cli-only", TimeoutSeconds: 60, RememberWindowSeconds: 300,
		},
	}}

	// Pre-seed a remember grant.
	_ = store.InsertGrant("test-escalate", "agent-1", 300)

	d := g.Evaluate(Action{Type: ActShellExec, Target: "echo"}, "agent-1", nil)
	if !d.Allowed {
		t.Errorf("Allowed = false, want true (grant should short-circuit)")
	}
	if d.RuleID != "escalate-remember-grant" {
		t.Errorf("RuleID = %q, want escalate-remember-grant", d.RuleID)
	}
}

// TestGate_EscalateRememberGrant_WritesAuditRow covers Bug B from PR
// #382's dogfood (2026-05-07): the remember-grant short-circuit must
// produce a gov-decisions JSONL row, not silently allow without audit.
// This is the path the operator hit after approving the first edit:
// every subsequent edit within the grant window flowed through here,
// and the operator could not find the corresponding row in the daily
// JSONL — the worry was that the short-circuit skipped step-8 logging.
// The fix is the unconditional WriteLog at step 8 (which already
// existed but is now exercised by this test) and a defense-in-depth
// "escalate-pending" row written before Wait blocks (covered by the
// EscalateApproved test below).
func TestGate_EscalateRememberGrant_WritesAuditRow(t *testing.T) {
	g, dir := newTestGate(t)
	store, err := OpenEscalateStore(filepath.Join(dir, "p.sqlite"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	defer store.Close()
	g.EscalateStore = store

	g.Policy.Rules = []Rule{{
		ID: "test-escalate", Action: ActionMatcher{"shell.exec"}, Effect: EffectEscalate,
		Reason: "needs operator",
		Escalation: &EscalateConfig{
			Channel: "cli-only", TimeoutSeconds: 60, RememberWindowSeconds: 300,
		},
	}}
	_ = store.InsertGrant("test-escalate", "agent-1", 300)

	_ = g.Evaluate(Action{Type: ActShellExec, Target: "echo"}, "agent-1", nil)

	rows := readAllDecisionRows(t, g.LogDir)
	if len(rows) == 0 {
		t.Fatalf("no audit rows written for remember-grant short-circuit (Bug B)")
	}
	// Find the escalate-remember-grant row.
	found := false
	for _, r := range rows {
		if r["rule_id"] == "escalate-remember-grant" {
			if r["allowed"] != true {
				t.Errorf("escalate-remember-grant row has allowed=%v, want true", r["allowed"])
			}
			found = true
			break
		}
	}
	if !found {
		t.Errorf("no audit row with rule_id=escalate-remember-grant; got rows: %+v", rows)
	}
}

// TestGate_EscalateApproved_WritesPendingThenOutcome covers the
// audit-gap defense for the Wait path (Bug B + Bug C interaction):
// before Wait blocks, the gate writes an "escalate-pending" row so the
// attempt is captured even if the harness kills the kernel mid-Wait.
// On approval, Wait returns and step 8 writes a second row with
// rule_id=escalate-approved. Both rows must be present in the JSONL.
func TestGate_EscalateApproved_WritesPendingThenOutcome(t *testing.T) {
	g, dir := newTestGate(t)
	store, err := OpenEscalateStore(filepath.Join(dir, "p.sqlite"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	defer store.Close()
	g.EscalateStore = store

	prevPoll := WaitPollInterval
	WaitPollInterval = 50 * time.Millisecond
	defer func() { WaitPollInterval = prevPoll }()

	g.Policy.Rules = []Rule{{
		ID: "test-escalate", Action: ActionMatcher{"shell.exec"}, Effect: EffectEscalate,
		Reason: "needs operator",
		Escalation: &EscalateConfig{
			Channel: "cli-only", TimeoutSeconds: 30, RememberWindowSeconds: 0,
		},
	}}

	done := make(chan struct{})
	go func() {
		_ = g.Evaluate(Action{Type: ActShellExec, Target: "echo"}, "agent-1", nil)
		close(done)
	}()

	// Wait for the pending row, approve.
	deadline := time.Now().Add(2 * time.Second)
	var pid string
	for time.Now().Before(deadline) && pid == "" {
		rows, _ := store.ListUnresolved()
		if len(rows) > 0 {
			pid = rows[0].ID
		}
		time.Sleep(20 * time.Millisecond)
	}
	if pid == "" {
		t.Fatal("no pending row appeared")
	}
	_ = store.ResolveApprove(pid, "operator-cli", 0)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Evaluate did not return")
	}

	rows := readAllDecisionRows(t, g.LogDir)
	sawPending := false
	sawApproved := false
	for _, r := range rows {
		switch r["rule_id"] {
		case "escalate-pending":
			sawPending = true
		case "escalate-approved":
			sawApproved = true
			if r["allowed"] != true {
				t.Errorf("escalate-approved allowed=%v, want true", r["allowed"])
			}
		}
	}
	if !sawPending {
		t.Errorf("missing escalate-pending audit row (audit-gap defense); rows: %+v", rows)
	}
	if !sawApproved {
		t.Errorf("missing escalate-approved audit row; rows: %+v", rows)
	}
}

// readAllDecisionRows reads every gov-decisions-*.jsonl in dir and
// returns the rows as map[string]any (decoded JSON objects). Returns
// an empty slice if dir doesn't exist (no rows written).
func readAllDecisionRows(t *testing.T, dir string) []map[string]any {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatalf("read log dir: %v", err)
	}
	var out []map[string]any
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), "gov-decisions-") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", e.Name(), err)
		}
		for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
			if line == "" {
				continue
			}
			var row map[string]any
			if err := json.Unmarshal([]byte(line), &row); err != nil {
				t.Fatalf("unmarshal line %q: %v", line, err)
			}
			out = append(out, row)
		}
	}
	return out
}
