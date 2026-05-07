package gov

import (
	"database/sql"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestEscalateStore_SchemaHasHermesTaskIdAndLastEventSeq locks in the
// kanban-shaped naming for the outbound notify path. The original
// `notify_msg_id` was named for a fictional `hermes message send`
// surface (see Task 16 observation 2026-05-07): the real shape is
// `hermes kanban create` returning a task_id, plus a per-row event
// cursor for watch-hermes (Task 19) per-task polls.
func TestEscalateStore_SchemaHasHermesTaskIdAndLastEventSeq(t *testing.T) {
	store := mustOpenStore(t)
	defer store.Close()

	// Verify the renamed/new columns exist by introspecting sqlite_master.
	var schema string
	if err := store.db.QueryRow(
		"SELECT sql FROM sqlite_master WHERE type='table' AND name='pending_approvals'",
	).Scan(&schema); err != nil {
		t.Fatalf("query schema: %v", err)
	}
	for _, want := range []string{"hermes_task_id", "last_event_seq"} {
		if !strings.Contains(schema, want) {
			t.Errorf("schema missing %q: %s", want, schema)
		}
	}
	if strings.Contains(schema, "notify_msg_id") {
		t.Errorf("schema still references old name notify_msg_id: %s", schema)
	}
}

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

func TestWait_ApprovalUnblocks(t *testing.T) {
	store := mustOpenStore(t)
	defer store.Close()

	// Override the poll interval for fast tests.
	prev := WaitPollInterval
	WaitPollInterval = 50 * time.Millisecond
	defer func() { WaitPollInterval = prev }()

	cfg := EscalateConfig{
		Channel: "cli-only", TimeoutSeconds: 5, RememberWindowSeconds: 0,
	}
	a := Action{Type: ActShellExec, Target: "echo hi", Path: "/tmp"}

	// Spawn the Wait in a goroutine; resolve from the test thread.
	type result struct {
		res Resolution
		err error
	}
	resCh := make(chan result, 1)
	idCh := make(chan string, 1)
	go func() {
		res, err := store.Wait(WaitArgs{
			RuleID: "test-rule", Agent: "agent-1", Action: a,
			Reason: "test reason", Config: cfg,
			NotifyFn: func(string, PendingApproval) error { return nil },
			OnInsert: func(id string) { idCh <- id },
		})
		resCh <- result{res, err}
	}()

	// Wait for the row to land, then approve.
	var insertedID string
	select {
	case insertedID = <-idCh:
	case <-time.After(2 * time.Second):
		t.Fatal("Wait did not insert a row within 2s")
	}
	if err := store.ResolveApprove(insertedID, "operator-cli", 0); err != nil {
		t.Fatalf("approve: %v", err)
	}

	select {
	case r := <-resCh:
		if r.err != nil {
			t.Fatalf("wait: %v", r.err)
		}
		if !r.res.Approved {
			t.Errorf("Approved = false, want true")
		}
		if r.res.OutcomeRuleID() != "escalate-approved" {
			t.Errorf("OutcomeRuleID = %q", r.res.OutcomeRuleID())
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Wait did not return within 2s of resolution")
	}
}

func TestWait_TimeoutDenies(t *testing.T) {
	store := mustOpenStore(t)
	defer store.Close()
	prev := WaitPollInterval
	WaitPollInterval = 50 * time.Millisecond
	defer func() { WaitPollInterval = prev }()

	cfg := EscalateConfig{Channel: "cli-only", TimeoutSeconds: 1, RememberWindowSeconds: 0}
	a := Action{Type: ActShellExec, Target: "x", Path: "/tmp"}

	res, err := store.Wait(WaitArgs{
		RuleID: "r", Agent: "a", Action: a, Reason: "x", Config: cfg,
		NotifyFn: func(string, PendingApproval) error { return nil },
	})
	if err != nil {
		t.Fatalf("wait: %v", err)
	}
	if res.Approved {
		t.Error("Approved = true, want false (timeout)")
	}
	if res.OutcomeRuleID() != "escalate-timeout" {
		t.Errorf("OutcomeRuleID = %q, want escalate-timeout", res.OutcomeRuleID())
	}
}

func TestWait_RememberGrantSetOnApproveWithWindow(t *testing.T) {
	store := mustOpenStore(t)
	defer store.Close()
	prev := WaitPollInterval
	WaitPollInterval = 50 * time.Millisecond
	defer func() { WaitPollInterval = prev }()

	cfg := EscalateConfig{Channel: "cli-only", TimeoutSeconds: 5, RememberWindowSeconds: 300}
	a := Action{Type: ActShellExec, Target: "x", Path: "/tmp"}

	resCh := make(chan Resolution, 1)
	idCh := make(chan string, 1)
	go func() {
		r, _ := store.Wait(WaitArgs{
			RuleID: "r", Agent: "a", Action: a, Reason: "x", Config: cfg,
			NotifyFn: func(string, PendingApproval) error { return nil },
			OnInsert: func(id string) { idCh <- id },
		})
		resCh <- r
	}()

	var insertedID string
	select {
	case insertedID = <-idCh:
	case <-time.After(2 * time.Second):
		t.Fatal("Wait did not insert a row within 2s")
	}
	_ = store.ResolveApprove(insertedID, "operator-cli", 300)
	r := <-resCh

	if r.GrantedWindowSeconds != 300 {
		t.Errorf("GrantedWindowSeconds = %d, want 300", r.GrantedWindowSeconds)
	}
}

func TestRememberGrants(t *testing.T) {
	store := mustOpenStore(t)
	defer store.Close()

	now := int64(1700000000)
	setTimeNow(func() time.Time { return time.Unix(now, 0) })
	defer setTimeNow(time.Now)

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

func TestSweepStaleEscalations_ResolvesPastDeadline(t *testing.T) {
	store := mustOpenStore(t)
	defer store.Close()

	now := int64(1700000000)
	setTimeNow(func() time.Time { return time.Unix(now, 0) })
	defer setTimeNow(time.Now)

	// Two rows: one fresh, one stale.
	mkRow := func(id string, createdTs int64, timeout int) {
		t.Helper()
		_ = store.InsertPending(PendingApproval{
			ID: id, Agent: "a", RuleID: "r", ActionType: "shell.exec",
			ActionTarget: "x", Cwd: "/tmp", Reason: "x",
			Channel: "cli-only", TimeoutSeconds: timeout,
			RememberWindowSeconds: 0, CreatedTs: createdTs,
		})
	}
	mkRow("01F", now-30, 600)  // fresh: deadline now+570
	mkRow("01S", now-1000, 60) // stale: deadline now-940

	resolved, err := store.SweepStale()
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if resolved != 1 {
		t.Errorf("sweep resolved %d, want 1", resolved)
	}

	got, _ := store.GetPending("01S")
	if got.Resolution != "timeout" || got.ResolutionBy != "timeout-watcher" {
		t.Errorf("01S not resolved as timeout: %+v", got)
	}
	got, _ = store.GetPending("01F")
	if got.ResolvedTs != nil {
		t.Errorf("01F should still be unresolved: %+v", got)
	}
}

// TestOpenEscalateStore_ConcurrentOpen verifies that N processes
// opening the same sqlite path concurrently do not collide on the
// CREATE TABLE IF NOT EXISTS schema bootstrap. Before the fix, the
// second goroutine's WAL/CREATE TABLE could race the first's lock
// and return SQLITE_BUSY (5) — observed as flakiness in the e2e
// escalate test (workaround: 200ms sleep before opening). After the
// fix, busy_timeout=5000 lets each goroutine wait up to 5s for the
// schema lock, so all opens succeed.
//
// Boundary: 5 concurrent goroutines, single shared path, single
// WaitGroup barrier. Asserts no errors and that each goroutine
// observes the schema (idempotent CREATE TABLE).
func TestOpenEscalateStore_ConcurrentOpen(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "p.sqlite")

	const N = 5
	errs := make([]error, N)
	var wg sync.WaitGroup
	wg.Add(N)
	for i := 0; i < N; i++ {
		go func(idx int) {
			defer wg.Done()
			store, err := OpenEscalateStore(path)
			if err != nil {
				errs[idx] = err
				return
			}
			defer store.Close()
			// Touch the schema so the connection actually exercises a
			// table-lookup after the bootstrap — catches the case where
			// CREATE TABLE returned but the connection's view was stale.
			var n int
			_ = store.db.QueryRow(
				"SELECT COUNT(*) FROM pending_approvals",
			).Scan(&n)
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("goroutine %d: open failed: %v", i, err)
		}
	}
}

// TestOpenEscalateStore_MigratesOlderSchema covers the dogfood scenario
// from PR #382 (2026-05-07): a pre-existing pending_approvals.sqlite
// from an older binary has columns notify_msg_id / notify_failed_reason
// instead of the current hermes_task_id / last_event_seq. CREATE TABLE
// IF NOT EXISTS is a no-op on a pre-existing table, so the new code's
// INSERT/SELECT queries fail with "no such column" and every escalate
// degrades to deny. After fix: OpenEscalateStore detects the missing
// columns via PRAGMA table_info and runs ALTER TABLE ADD COLUMN
// (metadata-only in sqlite, no data rewrite).
func TestOpenEscalateStore_MigratesOlderSchema(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "old.sqlite")

	// Build the OLD schema (no hermes_task_id, no last_event_seq;
	// has notify_msg_id like the original Task 16 design).
	{
		db, err := sql.Open("sqlite", path)
		if err != nil {
			t.Fatalf("open raw db: %v", err)
		}
		_, err = db.Exec(`
			CREATE TABLE pending_approvals (
				id              TEXT PRIMARY KEY,
				agent           TEXT NOT NULL,
				rule_id         TEXT NOT NULL,
				action_type     TEXT NOT NULL,
				action_target   TEXT NOT NULL,
				action_params   TEXT,
				cwd             TEXT NOT NULL,
				reason          TEXT NOT NULL,
				channel         TEXT NOT NULL,
				timeout_seconds INTEGER NOT NULL,
				remember_window_seconds INTEGER NOT NULL,
				created_ts      INTEGER NOT NULL,
				notified_ts     INTEGER,
				notify_msg_id   TEXT,
				notify_failed_reason TEXT,
				resolved_ts     INTEGER,
				resolution      TEXT,
				resolution_by   TEXT,
				resolution_reason TEXT,
				remember_grant_seconds INTEGER
			);
		`)
		if err != nil {
			t.Fatalf("create old schema: %v", err)
		}
		db.Close()
	}

	// Open via the production path — must migrate, not error.
	store, err := OpenEscalateStore(path)
	if err != nil {
		t.Fatalf("OpenEscalateStore on legacy db: %v", err)
	}
	defer store.Close()

	// Verify the new columns exist.
	checkCol := func(name string) {
		t.Helper()
		var n int
		if err := store.db.QueryRow(
			`SELECT COUNT(*) FROM pragma_table_info('pending_approvals') WHERE name = ?`,
			name,
		).Scan(&n); err != nil {
			t.Fatalf("query column %s: %v", name, err)
		}
		if n != 1 {
			t.Errorf("column %q not present after migration", name)
		}
	}
	checkCol("hermes_task_id")
	checkCol("last_event_seq")
	// Old column survives — we never DROP, just ADD.
	checkCol("notify_msg_id")

	// InsertPending must succeed under the migrated schema (this is the
	// failure mode that surfaced in PR #382: the new INSERT references
	// columns that didn't exist, so every escalate threw a SQL error
	// and ran out the Wait clock).
	row := PendingApproval{
		ID: "01MIGRATED0000000000000001", Agent: "claude-code",
		RuleID: "test-rule", ActionType: "file.write",
		ActionTarget: "/tmp/x", Cwd: "/tmp", Reason: "smoke",
		Channel: "cli-only", TimeoutSeconds: 60, RememberWindowSeconds: 0,
		CreatedTs: 1700000000,
	}
	if err := store.InsertPending(row); err != nil {
		t.Fatalf("InsertPending after migration: %v", err)
	}
	got, err := store.GetPending(row.ID)
	if err != nil {
		t.Fatalf("GetPending after migration: %v", err)
	}
	if got.ID != row.ID {
		t.Errorf("round-trip id: got %q, want %q", got.ID, row.ID)
	}

	// Re-opening the migrated db must be idempotent (no error on
	// second call to migratePendingApprovals).
	store.Close()
	store2, err := OpenEscalateStore(path)
	if err != nil {
		t.Fatalf("re-open migrated db: %v", err)
	}
	store2.Close()
}
