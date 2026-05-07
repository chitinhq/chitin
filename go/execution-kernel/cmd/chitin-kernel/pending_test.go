package main

import (
	"bytes"
	"encoding/json"
	"path/filepath"
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
