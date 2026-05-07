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
