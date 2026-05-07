package main

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chitinhq/chitin/go/execution-kernel/internal/gov"
)

func TestPendingList_OrdersOldestFirst(t *testing.T) {
	dir := t.TempDir()
	store, _ := gov.OpenEscalateStore(filepath.Join(dir, "p.sqlite"))
	defer store.Close()

	mk := func(id string, ts int64) {
		_ = store.InsertPending(gov.PendingApproval{
			ID: id, Agent: "a", RuleID: "r", ActionType: "shell.exec",
			ActionTarget: "x", Cwd: "/tmp", Reason: "r",
			Channel: "cli-only", TimeoutSeconds: 600,
			RememberWindowSeconds: 0, CreatedTs: ts,
		})
	}
	mk("01C", 1700000300)
	mk("01A", 1700000100)
	mk("01B", 1700000200)

	var buf bytes.Buffer
	if err := pendingList(store, &buf, true /* json */); err != nil {
		t.Fatalf("pendingList: %v", err)
	}

	var out []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, buf.String())
	}
	if len(out) != 3 {
		t.Fatalf("got %d rows, want 3", len(out))
	}
	want := []string{"01A", "01B", "01C"}
	for i, w := range want {
		if out[i]["id"] != w {
			t.Errorf("row %d id = %v, want %s", i, out[i]["id"], w)
		}
	}
}

func TestPendingApprove_WritesResolution(t *testing.T) {
	dir := t.TempDir()
	store, _ := gov.OpenEscalateStore(filepath.Join(dir, "p.sqlite"))
	defer store.Close()

	_ = store.InsertPending(gov.PendingApproval{
		ID: "01X", Agent: "a", RuleID: "r", ActionType: "shell.exec",
		ActionTarget: "x", Cwd: "/tmp", Reason: "x",
		Channel: "cli-only", TimeoutSeconds: 60, RememberWindowSeconds: 0,
		CreatedTs: 1700000000,
	})

	if err := pendingApprove(store, "01X", 300); err != nil {
		t.Fatalf("approve: %v", err)
	}

	got, _ := store.GetPending("01X")
	if got.Resolution != "approved" {
		t.Errorf("resolution = %q, want approved", got.Resolution)
	}
	if got.ResolutionBy != "operator-cli" {
		t.Errorf("resolution_by = %q, want operator-cli", got.ResolutionBy)
	}
	if got.RememberGrantSeconds == nil || *got.RememberGrantSeconds != 300 {
		t.Errorf("remember_grant_seconds = %v, want 300", got.RememberGrantSeconds)
	}
}

func TestPendingDeny_WritesReason(t *testing.T) {
	dir := t.TempDir()
	store, _ := gov.OpenEscalateStore(filepath.Join(dir, "p.sqlite"))
	defer store.Close()
	_ = store.InsertPending(gov.PendingApproval{
		ID: "01Y", Agent: "a", RuleID: "r", ActionType: "shell.exec",
		ActionTarget: "x", Cwd: "/tmp", Reason: "x",
		Channel: "cli-only", TimeoutSeconds: 60, RememberWindowSeconds: 0,
		CreatedTs: 1700000000,
	})

	if err := pendingDeny(store, "01Y", "no thank you"); err != nil {
		t.Fatalf("deny: %v", err)
	}

	got, _ := store.GetPending("01Y")
	if got.Resolution != "denied" {
		t.Errorf("resolution = %q, want denied", got.Resolution)
	}
	if got.ResolutionReason != "no thank you" {
		t.Errorf("reason = %q", got.ResolutionReason)
	}
}

func TestPendingApprove_RefusesAlreadyResolved(t *testing.T) {
	dir := t.TempDir()
	store, _ := gov.OpenEscalateStore(filepath.Join(dir, "p.sqlite"))
	defer store.Close()
	_ = store.InsertPending(gov.PendingApproval{
		ID: "01Z", Agent: "a", RuleID: "r", ActionType: "shell.exec",
		ActionTarget: "x", Cwd: "/tmp", Reason: "x",
		Channel: "cli-only", TimeoutSeconds: 60, RememberWindowSeconds: 0,
		CreatedTs: 1700000000,
	})
	_ = pendingApprove(store, "01Z", 0)
	err := pendingApprove(store, "01Z", 0)
	if err == nil {
		t.Error("expected error on re-approve, got nil")
	}
}

func TestPendingAuth_RejectsWrongUID(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "p.sqlite")
	store, _ := gov.OpenEscalateStore(dbPath)
	store.Close()

	// chmod the file to root-owned (or another uid). On macOS/linux
	// CI we can't actually chown to a different uid without root; this
	// test relies on the auth function being called and using a hook
	// for the stat call.
	prev := statOwnerUID
	statOwnerUID = func(string) (uint32, error) { return 999, nil }
	defer func() { statOwnerUID = prev }()

	prevSelf := selfUID
	selfUID = func() uint32 { return 1000 }
	defer func() { selfUID = prevSelf }()

	if err := authPendingFile(dbPath); err == nil {
		t.Error("expected pending_unauthorized error, got nil")
	} else if !strings.Contains(err.Error(), "pending_unauthorized") {
		t.Errorf("err = %v, want substring pending_unauthorized", err)
	}
}

// TestCLI_PendingWatchHermes_GracefulNoopWhenOperatorConfigMissing pins
// the fix for the chitin-pending-watch.timer failure observed
// 2026-05-07: with no rule using channel: hermes, ~/.chitin/operator.yaml
// doesn't exist (the cli-only escalate flow doesn't need it), and the
// timer was failing every 30s with "operator_config_missing" — burying
// real signals under hours of noise. Now the watch-hermes command
// returns a structured noop instead, so the timer stays green and the
// log line names what's missing.
func TestCLI_PendingWatchHermes_GracefulNoopWhenOperatorConfigMissing(t *testing.T) {
	stdout, stderr, code := runCLIWithEnv(t, t.TempDir(), nil, "pending", "watch-hermes")
	if code != 0 {
		t.Fatalf("exit code = %d (stderr=%q stdout=%q)", code, stderr, stdout)
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(stdout), &out); err != nil {
		t.Fatalf("unmarshal stdout %q: %v", stdout, err)
	}
	if ok, _ := out["ok"].(bool); !ok {
		t.Errorf(`stdout missing "ok":true: %q`, stdout)
	}
	if skipped, _ := out["skipped"].(string); skipped != "operator_config_missing" {
		t.Errorf(`expected "skipped":"operator_config_missing", got %q`, skipped)
	}
	// resolved must be 0 — nothing to do.
	if resolved, _ := out["resolved"].(float64); resolved != 0 {
		t.Errorf("resolved = %v, want 0", resolved)
	}
}
