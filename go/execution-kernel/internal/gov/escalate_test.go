package gov

import (
	"path/filepath"
	"testing"
)

// TestOpenEscalateStore_CreatesTablesAndIndexes verifies the store
// initializes its sqlite schema (pending_approvals + remember_grants
// + the unresolved index) on first open, and is idempotent on re-open.
func TestOpenEscalateStore_CreatesTablesAndIndexes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pending_approvals.sqlite")

	store, err := OpenEscalateStore(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer store.Close()

	// Verify both tables exist.
	for _, table := range []string{"pending_approvals", "remember_grants"} {
		var count int
		if err := store.db.QueryRow(
			"SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?", table,
		).Scan(&count); err != nil {
			t.Fatalf("query for %s: %v", table, err)
		}
		if count != 1 {
			t.Errorf("table %s: expected 1, got %d", table, count)
		}
	}

	// Verify the partial index for unresolved rows.
	var indexCount int
	_ = store.db.QueryRow(
		"SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name='idx_unresolved'",
	).Scan(&indexCount)
	if indexCount != 1 {
		t.Errorf("idx_unresolved: expected 1, got %d", indexCount)
	}

	// Re-open should be idempotent (no error, schema unchanged).
	store.Close()
	store2, err := OpenEscalateStore(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	store2.Close()
}

func TestPendingApprovals_InsertAndGet(t *testing.T) {
	store := mustOpenStore(t)
	defer store.Close()

	row := PendingApproval{
		ID: "01TEST00000000000000000001", Agent: "claude-code",
		RuleID: "test-rule", ActionType: "shell.exec",
		ActionTarget: "echo hi", Cwd: "/tmp", Reason: "test reason",
		Channel: "hermes", TimeoutSeconds: 600, RememberWindowSeconds: 300,
		CreatedTs: 1700000000,
	}
	if err := store.InsertPending(row); err != nil {
		t.Fatalf("insert: %v", err)
	}

	got, err := store.GetPending(row.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.ID != row.ID || got.Agent != row.Agent || got.ActionTarget != row.ActionTarget {
		t.Errorf("got %+v, want %+v", got, row)
	}
	if got.ResolvedTs != nil {
		t.Errorf("freshly-inserted row should have ResolvedTs=nil, got %v", got.ResolvedTs)
	}
}

// mustOpenStore is a tiny test helper used across escalate_test.go.
func mustOpenStore(t *testing.T) *EscalateStore {
	t.Helper()
	store, err := OpenEscalateStore(filepath.Join(t.TempDir(), "p.sqlite"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	return store
}

func TestPendingApprovals_Resolve(t *testing.T) {
	store := mustOpenStore(t)
	defer store.Close()

	insert := func(id string) {
		t.Helper()
		err := store.InsertPending(PendingApproval{
			ID: id, Agent: "a", RuleID: "r", ActionType: "shell.exec",
			ActionTarget: "x", Cwd: "/tmp", Reason: "x",
			Channel: "cli-only", TimeoutSeconds: 60, RememberWindowSeconds: 0,
			CreatedTs: 1700000000,
		})
		if err != nil {
			t.Fatalf("insert %s: %v", id, err)
		}
	}

	// Approve path.
	insert("01A")
	if err := store.ResolveApprove("01A", "operator-cli", 300); err != nil {
		t.Fatalf("approve: %v", err)
	}
	got, _ := store.GetPending("01A")
	if got.Resolution != "approved" || got.ResolutionBy != "operator-cli" {
		t.Errorf("approve fields wrong: %+v", got)
	}
	if got.RememberGrantSeconds == nil || *got.RememberGrantSeconds != 300 {
		t.Errorf("remember_grant_seconds want 300, got %v", got.RememberGrantSeconds)
	}

	// Deny path.
	insert("01B")
	if err := store.ResolveDeny("01B", "hermes-reply", "operator says no"); err != nil {
		t.Fatalf("deny: %v", err)
	}
	got, _ = store.GetPending("01B")
	if got.Resolution != "denied" || got.ResolutionReason != "operator says no" {
		t.Errorf("deny fields wrong: %+v", got)
	}

	// Timeout path.
	insert("01C")
	if err := store.ResolveTimeout("01C"); err != nil {
		t.Fatalf("timeout: %v", err)
	}
	got, _ = store.GetPending("01C")
	if got.Resolution != "timeout" || got.ResolutionBy != "timeout-watcher" {
		t.Errorf("timeout fields wrong: %+v", got)
	}

	// Re-resolution refused.
	if err := store.ResolveApprove("01A", "operator-cli", 0); err == nil {
		t.Error("expected re-resolve to error, got nil")
	}
}
