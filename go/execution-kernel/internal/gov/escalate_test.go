package gov

import (
	"path/filepath"
	"testing"
	"time"
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

func TestPendingApprovals_ListUnresolved(t *testing.T) {
	store := mustOpenStore(t)
	defer store.Close()

	// Three rows: one resolved, one unresolved-fresh, one unresolved-stale.
	now := int64(1700000000)
	mkRow := func(id string, createdTs int64, timeout int, resolved bool) {
		t.Helper()
		err := store.InsertPending(PendingApproval{
			ID: id, Agent: "a", RuleID: "r", ActionType: "shell.exec",
			ActionTarget: "x", Cwd: "/tmp", Reason: "x",
			Channel: "cli-only", TimeoutSeconds: timeout,
			RememberWindowSeconds: 0, CreatedTs: createdTs,
		})
		if err != nil {
			t.Fatal(err)
		}
		if resolved {
			_ = store.ResolveApprove(id, "operator-cli", 0)
		}
	}
	mkRow("01R", now-1000, 600, true)         // resolved
	mkRow("01F", now-30, 600, false)          // unresolved, fresh (deadline +570s)
	mkRow("01S", now-1000, 60, false)         // unresolved, stale (deadline -940s)

	all, err := store.ListUnresolved()
	if err != nil {
		t.Fatalf("list unresolved: %v", err)
	}
	if len(all) != 2 {
		t.Errorf("ListUnresolved: got %d rows, want 2 (skip resolved)", len(all))
	}

	stale, err := store.ListUnresolvedPastDeadline(now)
	if err != nil {
		t.Fatalf("list past deadline: %v", err)
	}
	if len(stale) != 1 || stale[0].ID != "01S" {
		t.Errorf("ListUnresolvedPastDeadline: got %d rows, want 1 (01S)", len(stale))
	}
}

func TestRememberGrants(t *testing.T) {
	store := mustOpenStore(t)
	defer store.Close()

	now := int64(1700000000)
	timeNow = func() time.Time { return time.Unix(now, 0) }
	defer func() { timeNow = time.Now }()

	// Empty store: HasUnexpired returns false.
	if store.HasUnexpiredGrant("rule-x", "agent-a") {
		t.Error("empty store should return false")
	}

	// Insert a 300s grant; HasUnexpired returns true while now is within window.
	if err := store.InsertGrant("rule-x", "agent-a", 300); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if !store.HasUnexpiredGrant("rule-x", "agent-a") {
		t.Error("inserted grant should be unexpired")
	}

	// Different (rule, agent) is independent.
	if store.HasUnexpiredGrant("rule-x", "agent-b") {
		t.Error("agent-b should have no grant")
	}
	if store.HasUnexpiredGrant("rule-y", "agent-a") {
		t.Error("rule-y should have no grant")
	}

	// Advance time past the window — grant expired.
	now += 301
	if store.HasUnexpiredGrant("rule-x", "agent-a") {
		t.Error("expired grant should return false")
	}

	// Sweep removes expired rows.
	removed, err := store.SweepExpiredGrants()
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if removed != 1 {
		t.Errorf("sweep removed %d, want 1", removed)
	}

	// Re-insert with same (rule, agent) replaces.
	now += 100
	if err := store.InsertGrant("rule-x", "agent-a", 600); err != nil {
		t.Fatalf("reinsert: %v", err)
	}
	if !store.HasUnexpiredGrant("rule-x", "agent-a") {
		t.Error("reinserted grant should be unexpired")
	}
}
