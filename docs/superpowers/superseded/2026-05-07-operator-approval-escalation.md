# Operator-Approval Escalation Implementation Plan

> **Superseded (2026-05-08):** This implementation was built (PRs #380–#396) and then culled (PRs #397–#400). Operator approvals are now handled by Hermes' `tools/approval.py`. See `docs/decisions/2026-05-08-cull-escalate-defer-to-hermes.md`.

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `effect: escalate` to the chitin policy DSL so any rule can request real-time operator approval (via hermes-gateway chat or CLI fallback) before allowing/denying a tool call.

**Architecture:** New `EscalationPending` internal state inside `gov.Gate.Evaluate`. SQLite (`~/.chitin/pending_approvals.sqlite`) is the source of truth. Polling loop every 2s blocks the agent's tool call until operator resolves or timeout fires (default 10 min). Approval grants a configurable per-(rule, agent) "remember window" so the operator isn't re-approving the same action repeatedly. Hermes-gateway notification is the convenience path; `chitin-kernel approve <id>` CLI is the always-works fallback.

**Tech Stack:** Go (kernel internals), SQLite WAL via `modernc.org/sqlite` (already in use for `gov.db`), `text/template` for notify message rendering, `os/exec` to shell out to `hermes message send`.

**Spec reference:** `docs/superpowers/specs/2026-05-07-operator-approval-escalation-design.md`.

**Branch:** This plan executes on `docs/operator-approval-escalation-spec` (which already has the spec committed). Companion branch `fix/hermes-internal-tool-names` ships independently and shouldn't block this.

---

## Important context for the implementer

### Dogfood paradox

The chitin gate has a `no-governance-self-modification` rule that blocks edits to `internal/gov/*` and `chitin.yaml`. **An agent executing this plan via the live gate will be blocked on most code-edit steps.** Two options:

1. **Operator-driven:** the operator (human maintainer) executes the plan locally; the gate fires on each Edit/Write tool call, the operator approves manually via the (yet-to-exist) approval flow OR temporarily relaxes the rule on a development branch with a project-local `chitin.yaml` override.
2. **Dev-mode bypass:** add a temporary local `chitin.yaml` rule allowing `file.write` to `internal/gov/` for the duration of execution; remove it before merge.

Either way, the very fact that this plan can't be cleanly auto-executed by the swarm is the motivation for the feature.

### Sibling branch

`fix/hermes-internal-tool-names` (separate PR) adds `ActKanbanCall` + `ActHermesProcess` + the hermes driver normalize cases. It does NOT block this plan, but its companion policy edits (the `default-allow-kanban-call` rule) are blocked by the same `no-governance-self-modification` rule and will land via this feature once shipped.

---

## File structure

### Files created

| Path | Responsibility |
|---|---|
| `internal/gov/escalate.go` | `PendingApprovalStore`, `RememberGrants`, `Wait()` poll loop, `Resolution` types |
| `internal/gov/escalate_test.go` | Unit tests for Wait, RememberGrants, Resolution |
| `internal/gov/policy_escalate_test.go` | Policy parse + validation tests for `effect: escalate` |
| `cmd/chitin-kernel/pending.go` | `pending list/approve/deny` CLI handler |
| `cmd/chitin-kernel/pending_test.go` | CLI handler unit tests |
| `cmd/chitin-kernel/notify_hermes.go` | Outbound `hermes message send` shell-out + inbound reply listener |
| `cmd/chitin-kernel/notify_hermes_test.go` | Notify shell-out + parser tests (mocked exec) |
| `cmd/chitin-kernel/escalate_e2e_test.go` | End-to-end integration: gate.Evaluate ↔ CLI approve ↔ resolution |
| `infra/systemd/chitin-pending-watch.service` | systemd one-shot for the hermes reply watcher |
| `infra/systemd/chitin-pending-watch.timer` | every-30s timer for the watcher |

### Files modified

| Path | Change |
|---|---|
| `internal/gov/action.go` | Add `EffectEscalate` constant; add `EscalationConfig` struct |
| `internal/gov/policy.go` | Parse + validate `effect: escalate` and its four optional fields |
| `internal/gov/decision.go` | Add `EscalationID string` + `Effect string` fields to `Decision` |
| `internal/gov/gate.go` | New escalate branch in `Evaluate` between policy + counter |
| `cmd/chitin-kernel/main.go` | Wire `cmdPending` dispatcher into top-level switch |

### External (operator-side, not in repo)

| Path | Schema |
|---|---|
| `~/.chitin/pending_approvals.sqlite` | created at first use, `chmod 600` |
| `~/.chitin/operator.yaml` | `{channel: <name>, hermes_bin: <path>}` |

---

## Task ordering

Foundation → Storage → Gate integration → CLI Handler A → Hermes investigation → Hermes Handler B → Operator config → Systemd → Integration tests.

A→B→C→D is the critical path; E (research) gates F (Handler B); G/H/I can run in parallel with the rest after C.

---

## Phase 1 — Foundation: action types + decision shape

### Task 1: Add `EffectEscalate` constant + `EscalationConfig` struct

**Files:**
- Modify: `internal/gov/action.go`

- [ ] **Step 1: Append to the existing const block in action.go**

After the `ActUnknown` line, add a new const block for effects:

```go
// Effect constants — match the YAML `effect:` field values.
type Effect string

const (
	EffectAllow    Effect = "allow"
	EffectDeny     Effect = "deny"
	EffectGuide    Effect = "guide"
	EffectMonitor  Effect = "monitor"
	// EffectEscalate pauses the agent's tool call and asks the operator
	// to approve via the configured channel. Resolution comes through
	// the pending_approvals sqlite table; the gate's Wait helper polls
	// for it. Spec: docs/superpowers/specs/2026-05-07-operator-approval-escalation-design.md
	EffectEscalate Effect = "escalate"
)

// EscalationConfig is the per-rule configuration for effect:escalate.
// All fields have sensible defaults (see policy.go's parser); only
// non-default values need to be set in chitin.yaml.
type EscalationConfig struct {
	// Channel: "hermes" (notify via hermes-gateway) | "cli-only" (no notify, queue for `chitin-kernel approve`)
	Channel string
	// TimeoutSeconds: deny resolution if no operator response within this window. Range [30, 86400].
	TimeoutSeconds int
	// RememberWindowSeconds: on approve, grant subsequent (rule_id, agent) calls for this window. 0 = single-call only.
	RememberWindowSeconds int
	// NotifyTemplate: optional Go text/template for the notification body. Empty = use built-in default.
	NotifyTemplate string
}
```

- [ ] **Step 2: Build to confirm no compile errors**

Run: `cd go/execution-kernel && go build ./internal/gov/...`
Expected: no output (success)

- [ ] **Step 3: Commit**

```bash
git add go/execution-kernel/internal/gov/action.go
git -c user.name=red -c user.email=jpleva91@gmail.com commit -m "feat(gov): add EffectEscalate constant + EscalationConfig struct

Foundation for the operator-approval escalation effect (spec
2026-05-07). No behavior change yet — just the type surface."
```

---

### Task 2: Add `EscalationID` + `Effect` fields to `Decision`

**Files:**
- Modify: `internal/gov/decision.go`

- [ ] **Step 1: Inspect current Decision struct**

Run: `grep -n "type Decision" go/execution-kernel/internal/gov/decision.go`
Read the file to find where the struct fields are defined.

- [ ] **Step 2: Add the two new fields to the Decision struct**

Locate the existing `type Decision struct { ... }` and append these fields immediately before the closing brace:

```go
	// EscalationID is the ULID of the pending_approvals row when
	// this decision came from an operator-approval flow. Lets
	// auditors join chain rows back to pending_approvals for the
	// full provenance (notification time, operator reply, etc.).
	// Empty for non-escalate decisions.
	EscalationID string `json:"escalation_id,omitempty"`

	// Effect is the rule's effect value as parsed from chitin.yaml
	// (allow|deny|guide|monitor|escalate). Internal to the gate's
	// flow control; not serialized to the chain (the chain only
	// cares about the resolved Allowed + RuleID).
	Effect Effect `json:"-"`
```

- [ ] **Step 3: Build to confirm**

Run: `cd go/execution-kernel && go build ./internal/gov/...`
Expected: no output (success)

- [ ] **Step 4: Commit**

```bash
git add go/execution-kernel/internal/gov/decision.go
git -c user.name=red -c user.email=jpleva91@gmail.com commit -m "feat(gov): add EscalationID + Effect fields to Decision

EscalationID is chain-serialized (omitempty) for audit join; Effect
is internal-only (json:\"-\") and drives the gate's escalate branch."
```

---

## Phase 2 — Storage: sqlite tables + CRUD

### Task 3: Write the failing test for `OpenEscalateStore`

**Files:**
- Create: `internal/gov/escalate_test.go`

- [ ] **Step 1: Write the failing test for the store opener**

Create `internal/gov/escalate_test.go` with:

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd go/execution-kernel && go test ./internal/gov/ -run TestOpenEscalateStore -v`
Expected: FAIL with `undefined: OpenEscalateStore`

- [ ] **Step 3: Implement `OpenEscalateStore` minimally to pass**

Create `internal/gov/escalate.go`:

```go
// Package gov: PendingApprovalStore + RememberGrants for the operator-
// approval escalation effect. Spec:
// docs/superpowers/specs/2026-05-07-operator-approval-escalation-design.md
package gov

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

type EscalateStore struct {
	db *sql.DB
}

func OpenEscalateStore(dbPath string) (*EscalateStore, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open: %w", err)
	}
	if _, err := db.Exec(`PRAGMA journal_mode=WAL`); err != nil {
		return nil, fmt.Errorf("WAL: %w", err)
	}
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS pending_approvals (
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
		CREATE INDEX IF NOT EXISTS idx_unresolved ON pending_approvals (resolved_ts) WHERE resolved_ts IS NULL;
		CREATE TABLE IF NOT EXISTS remember_grants (
			rule_id    TEXT NOT NULL,
			agent      TEXT NOT NULL,
			granted_ts INTEGER NOT NULL,
			expires_ts INTEGER NOT NULL,
			PRIMARY KEY (rule_id, agent)
		);
		CREATE INDEX IF NOT EXISTS idx_remember_unexpired ON remember_grants (expires_ts);
	`); err != nil {
		return nil, fmt.Errorf("create schema: %w", err)
	}
	return &EscalateStore{db: db}, nil
}

func (s *EscalateStore) Close() error { return s.db.Close() }
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd go/execution-kernel && go test ./internal/gov/ -run TestOpenEscalateStore -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add go/execution-kernel/internal/gov/escalate.go go/execution-kernel/internal/gov/escalate_test.go
git -c user.name=red -c user.email=jpleva91@gmail.com commit -m "feat(gov): EscalateStore — sqlite schema for pending_approvals + remember_grants

Schema per spec section 'Storage'. WAL mode (matches gov.db); idempotent open."
```

---

### Task 4: PendingApproval CRUD — Insert + Get

**Files:**
- Modify: `internal/gov/escalate.go`
- Modify: `internal/gov/escalate_test.go`

- [ ] **Step 1: Add the failing test**

Append to `escalate_test.go`:

```go
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
```

- [ ] **Step 2: Run to verify failure**

Run: `cd go/execution-kernel && go test ./internal/gov/ -run TestPendingApprovals_InsertAndGet -v`
Expected: FAIL with `undefined: PendingApproval` (or similar)

- [ ] **Step 3: Implement PendingApproval struct + Insert + Get**

Append to `escalate.go`:

```go
type PendingApproval struct {
	ID                    string
	Agent                 string
	RuleID                string
	ActionType            string
	ActionTarget          string
	ActionParams          string // JSON-encoded; "" when none
	Cwd                   string
	Reason                string
	Channel               string // "hermes" | "cli-only"
	TimeoutSeconds        int
	RememberWindowSeconds int
	CreatedTs             int64
	NotifiedTs            *int64
	NotifyMsgID           string
	NotifyFailedReason    string
	ResolvedTs            *int64
	Resolution            string // "approved" | "denied" | "timeout"
	ResolutionBy          string // "operator-cli" | "hermes-reply" | "timeout-watcher"
	ResolutionReason      string
	RememberGrantSeconds  *int
}

func (s *EscalateStore) InsertPending(p PendingApproval) error {
	_, err := s.db.Exec(`
		INSERT INTO pending_approvals (
			id, agent, rule_id, action_type, action_target, action_params,
			cwd, reason, channel, timeout_seconds, remember_window_seconds,
			created_ts
		) VALUES (?,?,?,?,?,?,?,?,?,?,?,?)
	`, p.ID, p.Agent, p.RuleID, p.ActionType, p.ActionTarget, p.ActionParams,
		p.Cwd, p.Reason, p.Channel, p.TimeoutSeconds, p.RememberWindowSeconds,
		p.CreatedTs)
	return err
}

func (s *EscalateStore) GetPending(id string) (PendingApproval, error) {
	var p PendingApproval
	var notifiedTs sql.NullInt64
	var resolvedTs sql.NullInt64
	var rememberGrant sql.NullInt64
	err := s.db.QueryRow(`
		SELECT id, agent, rule_id, action_type, action_target,
		       COALESCE(action_params, ''), cwd, reason, channel,
		       timeout_seconds, remember_window_seconds, created_ts,
		       notified_ts, COALESCE(notify_msg_id, ''),
		       COALESCE(notify_failed_reason, ''), resolved_ts,
		       COALESCE(resolution, ''), COALESCE(resolution_by, ''),
		       COALESCE(resolution_reason, ''), remember_grant_seconds
		FROM pending_approvals WHERE id = ?
	`, id).Scan(
		&p.ID, &p.Agent, &p.RuleID, &p.ActionType, &p.ActionTarget,
		&p.ActionParams, &p.Cwd, &p.Reason, &p.Channel,
		&p.TimeoutSeconds, &p.RememberWindowSeconds, &p.CreatedTs,
		&notifiedTs, &p.NotifyMsgID, &p.NotifyFailedReason,
		&resolvedTs, &p.Resolution, &p.ResolutionBy,
		&p.ResolutionReason, &rememberGrant,
	)
	if err != nil {
		return p, err
	}
	if notifiedTs.Valid {
		v := notifiedTs.Int64
		p.NotifiedTs = &v
	}
	if resolvedTs.Valid {
		v := resolvedTs.Int64
		p.ResolvedTs = &v
	}
	if rememberGrant.Valid {
		v := int(rememberGrant.Int64)
		p.RememberGrantSeconds = &v
	}
	return p, nil
}
```

- [ ] **Step 4: Run to verify pass**

Run: `cd go/execution-kernel && go test ./internal/gov/ -run TestPendingApprovals_InsertAndGet -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add go/execution-kernel/internal/gov/escalate.go go/execution-kernel/internal/gov/escalate_test.go
git -c user.name=red -c user.email=jpleva91@gmail.com commit -m "feat(gov): PendingApproval struct + Insert + Get"
```

---

### Task 5: Resolve operations — Approve, Deny, Timeout

**Files:**
- Modify: `internal/gov/escalate.go`
- Modify: `internal/gov/escalate_test.go`

- [ ] **Step 1: Failing test for Resolve operations**

Append to `escalate_test.go`:

```go
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
```

- [ ] **Step 2: Run to verify failure**

Run: `cd go/execution-kernel && go test ./internal/gov/ -run TestPendingApprovals_Resolve -v`
Expected: FAIL — `undefined: ResolveApprove`

- [ ] **Step 3: Implement Resolve methods + the re-resolve refusal**

Append to `escalate.go`:

```go
// ErrAlreadyResolved is returned when a Resolve* call targets a row
// whose resolved_ts is already set. Caller (CLI / hermes-reply parser)
// surfaces this as "pending_already_resolved" to the operator.
var ErrAlreadyResolved = fmt.Errorf("pending_already_resolved")

func (s *EscalateStore) ResolveApprove(id, by string, grantSeconds int) error {
	return s.resolve(id, "approved", by, "", &grantSeconds)
}

func (s *EscalateStore) ResolveDeny(id, by, reason string) error {
	return s.resolve(id, "denied", by, reason, nil)
}

func (s *EscalateStore) ResolveTimeout(id string) error {
	return s.resolve(id, "timeout", "timeout-watcher", "", nil)
}

func (s *EscalateStore) resolve(id, resolution, by, reason string, grantSeconds *int) error {
	now := nowUnix()
	res, err := s.db.Exec(`
		UPDATE pending_approvals
		SET resolved_ts = ?, resolution = ?, resolution_by = ?,
		    resolution_reason = ?, remember_grant_seconds = ?
		WHERE id = ? AND resolved_ts IS NULL
	`, now, resolution, by, reason, nullableInt(grantSeconds), id)
	if err != nil {
		return err
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		return ErrAlreadyResolved
	}
	return nil
}

// nowUnix is a hook for tests to override time.Now().Unix().
var nowUnix = func() int64 { return timeNow().Unix() }

// timeNow is a hook for tests to override time.Now().
var timeNow = time.Now

func nullableInt(p *int) any {
	if p == nil {
		return nil
	}
	return *p
}
```

Add `import "time"` if it's not already in escalate.go.

- [ ] **Step 4: Run to verify pass**

Run: `cd go/execution-kernel && go test ./internal/gov/ -run TestPendingApprovals_Resolve -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add go/execution-kernel/internal/gov/escalate.go go/execution-kernel/internal/gov/escalate_test.go
git -c user.name=red -c user.email=jpleva91@gmail.com commit -m "feat(gov): Resolve operations (Approve, Deny, Timeout) + re-resolve refusal"
```

---

### Task 6: ListUnresolved + ListUnresolvedPastDeadline (sweeper helper)

**Files:**
- Modify: `internal/gov/escalate.go`
- Modify: `internal/gov/escalate_test.go`

- [ ] **Step 1: Failing test for List operations**

Append to `escalate_test.go`:

```go
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
```

- [ ] **Step 2: Run to verify failure**

Run: `cd go/execution-kernel && go test ./internal/gov/ -run TestPendingApprovals_ListUnresolved -v`
Expected: FAIL — undefined ListUnresolved

- [ ] **Step 3: Implement List methods**

Append to `escalate.go`:

```go
// ListUnresolved returns all pending_approvals rows where resolved_ts
// IS NULL, ordered by created_ts ASC. Used by the CLI's `pending list`.
func (s *EscalateStore) ListUnresolved() ([]PendingApproval, error) {
	rows, err := s.db.Query(`
		SELECT id FROM pending_approvals
		WHERE resolved_ts IS NULL
		ORDER BY created_ts ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PendingApproval
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		p, err := s.GetPending(id)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

// ListUnresolvedPastDeadline returns rows whose
// (created_ts + timeout_seconds) < nowSec. Used by the sweeper.
func (s *EscalateStore) ListUnresolvedPastDeadline(nowSec int64) ([]PendingApproval, error) {
	rows, err := s.db.Query(`
		SELECT id FROM pending_approvals
		WHERE resolved_ts IS NULL
		AND (created_ts + timeout_seconds) < ?
		ORDER BY created_ts ASC
	`, nowSec)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PendingApproval
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		p, err := s.GetPending(id)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}
```

- [ ] **Step 4: Run to verify pass**

Run: `cd go/execution-kernel && go test ./internal/gov/ -run TestPendingApprovals_ListUnresolved -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add go/execution-kernel/internal/gov/escalate.go go/execution-kernel/internal/gov/escalate_test.go
git -c user.name=red -c user.email=jpleva91@gmail.com commit -m "feat(gov): ListUnresolved + ListUnresolvedPastDeadline (sweeper helper)"
```

---

### Task 7: RememberGrants — HasUnexpired + Insert + Sweep

**Files:**
- Modify: `internal/gov/escalate.go`
- Modify: `internal/gov/escalate_test.go`

- [ ] **Step 1: Failing test**

Append to `escalate_test.go`:

```go
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
```

- [ ] **Step 2: Run to verify failure**

Run: `cd go/execution-kernel && go test ./internal/gov/ -run TestRememberGrants -v`
Expected: FAIL — undefined HasUnexpiredGrant

- [ ] **Step 3: Implement RememberGrants methods**

Append to `escalate.go`:

```go
// HasUnexpiredGrant returns true if there's a row in remember_grants
// for (rule_id, agent) whose expires_ts > now.
func (s *EscalateStore) HasUnexpiredGrant(ruleID, agent string) bool {
	now := nowUnix()
	var count int
	_ = s.db.QueryRow(`
		SELECT COUNT(*) FROM remember_grants
		WHERE rule_id = ? AND agent = ? AND expires_ts > ?
	`, ruleID, agent, now).Scan(&count)
	return count > 0
}

// InsertGrant writes a new grant row. ON CONFLICT replaces — so re-
// approving the same (rule, agent) extends the window from now,
// not from the original grant_ts.
func (s *EscalateStore) InsertGrant(ruleID, agent string, windowSeconds int) error {
	now := nowUnix()
	_, err := s.db.Exec(`
		INSERT INTO remember_grants (rule_id, agent, granted_ts, expires_ts)
		VALUES (?, ?, ?, ?)
		ON CONFLICT (rule_id, agent) DO UPDATE SET
			granted_ts = excluded.granted_ts,
			expires_ts = excluded.expires_ts
	`, ruleID, agent, now, now+int64(windowSeconds))
	return err
}

// SweepExpiredGrants deletes all rows whose expires_ts <= now.
// Returns the count removed.
func (s *EscalateStore) SweepExpiredGrants() (int, error) {
	now := nowUnix()
	res, err := s.db.Exec(`DELETE FROM remember_grants WHERE expires_ts <= ?`, now)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}
```

- [ ] **Step 4: Run to verify pass**

Run: `cd go/execution-kernel && go test ./internal/gov/ -run TestRememberGrants -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add go/execution-kernel/internal/gov/escalate.go go/execution-kernel/internal/gov/escalate_test.go
git -c user.name=red -c user.email=jpleva91@gmail.com commit -m "feat(gov): RememberGrants — HasUnexpired + Insert (replace) + Sweep"
```

---

## Phase 3 — Policy parser + validation

### Task 8: Parse `effect: escalate` + the four optional fields

**Files:**
- Modify: `internal/gov/policy.go`
- Create: `internal/gov/policy_escalate_test.go`

- [ ] **Step 1: Failing test for the parser**

Create `internal/gov/policy_escalate_test.go`:

```go
package gov

import (
	"strings"
	"testing"
)

func TestParseEscalateRule_AllFieldsExplicit(t *testing.T) {
	yaml := `
id: parse-test
mode: enforce
rules:
  - id: foo
    action: shell.exec
    effect: escalate
    channel: cli-only
    timeout_seconds: 1200
    remember_window_seconds: 0
    notify_template: "custom template body"
`
	p, err := parsePolicyYAML([]byte(yaml))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(p.Rules) != 1 {
		t.Fatalf("rules: got %d, want 1", len(p.Rules))
	}
	r := p.Rules[0]
	if r.Effect != EffectEscalate {
		t.Errorf("Effect: got %q, want %q", r.Effect, EffectEscalate)
	}
	if r.Escalation == nil {
		t.Fatal("Escalation: got nil, want struct")
	}
	if r.Escalation.Channel != "cli-only" {
		t.Errorf("Channel: %q", r.Escalation.Channel)
	}
	if r.Escalation.TimeoutSeconds != 1200 {
		t.Errorf("TimeoutSeconds: %d", r.Escalation.TimeoutSeconds)
	}
	if r.Escalation.RememberWindowSeconds != 0 {
		t.Errorf("RememberWindowSeconds: %d", r.Escalation.RememberWindowSeconds)
	}
	if r.Escalation.NotifyTemplate != "custom template body" {
		t.Errorf("NotifyTemplate: %q", r.Escalation.NotifyTemplate)
	}
}

func TestParseEscalateRule_DefaultsApplied(t *testing.T) {
	yaml := `
id: parse-test
mode: enforce
rules:
  - id: foo
    action: shell.exec
    effect: escalate
`
	p, err := parsePolicyYAML([]byte(yaml))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	r := p.Rules[0]
	if r.Escalation.Channel != "hermes" {
		t.Errorf("default Channel: %q want hermes", r.Escalation.Channel)
	}
	if r.Escalation.TimeoutSeconds != 600 {
		t.Errorf("default TimeoutSeconds: %d want 600", r.Escalation.TimeoutSeconds)
	}
	if r.Escalation.RememberWindowSeconds != 300 {
		t.Errorf("default RememberWindowSeconds: %d want 300", r.Escalation.RememberWindowSeconds)
	}
}

func TestParseEscalateRule_ValidationErrors(t *testing.T) {
	cases := []struct {
		name      string
		yaml      string
		wantSubst string
	}{
		{
			name: "timeout too short",
			yaml: `
mode: enforce
rules:
  - id: x
    action: shell.exec
    effect: escalate
    timeout_seconds: 5
`,
			wantSubst: "timeout_seconds",
		},
		{
			name: "timeout too long",
			yaml: `
mode: enforce
rules:
  - id: x
    action: shell.exec
    effect: escalate
    timeout_seconds: 100000
`,
			wantSubst: "timeout_seconds",
		},
		{
			name: "unknown channel",
			yaml: `
mode: enforce
rules:
  - id: x
    action: shell.exec
    effect: escalate
    channel: weird
`,
			wantSubst: "channel",
		},
		{
			name: "escalate on unknown action",
			yaml: `
mode: enforce
rules:
  - id: x
    action: unknown
    effect: escalate
`,
			wantSubst: "unknown",
		},
		{
			name: "negative remember_window",
			yaml: `
mode: enforce
rules:
  - id: x
    action: shell.exec
    effect: escalate
    remember_window_seconds: -1
`,
			wantSubst: "remember_window_seconds",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parsePolicyYAML([]byte(tc.yaml))
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tc.wantSubst) {
				t.Errorf("err = %v, want substring %q", err, tc.wantSubst)
			}
		})
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `cd go/execution-kernel && go test ./internal/gov/ -run TestParseEscalateRule -v`
Expected: FAIL — Rule struct has no Escalation field; parsePolicyYAML doesn't accept the new shape

- [ ] **Step 3: Inspect existing Rule struct + parser**

Run: `grep -n "type Rule\|^func parsePolicyYAML\|^func.*ParsePolicy\|effect:" go/execution-kernel/internal/gov/policy.go | head`

Note the existing Rule struct fields (likely `ID`, `Action`, `Effect`, `Reason`, etc.) and how the YAML parses (likely via `yaml.Unmarshal`).

- [ ] **Step 4: Add Escalation field to Rule + parse + validate**

In `policy.go`, modify the `Rule` struct to add:

```go
	// Escalation is non-nil only when Effect == EffectEscalate.
	// Built by the parser from the rule's optional escalate fields
	// (channel, timeout_seconds, remember_window_seconds, notify_template).
	// All defaults applied at parse time so consumers see a fully-
	// populated config.
	Escalation *EscalationConfig `yaml:"-" json:"-"`
```

Also add to the YAML-side intermediate struct (whichever one yaml.Unmarshal targets):

```go
	Channel               string `yaml:"channel,omitempty"`
	TimeoutSeconds        int    `yaml:"timeout_seconds,omitempty"`
	RememberWindowSeconds *int   `yaml:"remember_window_seconds,omitempty"`
	NotifyTemplate        string `yaml:"notify_template,omitempty"`
```

(`*int` for RememberWindowSeconds so we can distinguish "not set" from "explicit 0".)

In the parser's per-rule loop (after Effect is set), add:

```go
if r.Effect == EffectEscalate {
	cfg, err := buildEscalationConfig(yamlRule)
	if err != nil {
		return Policy{}, fmt.Errorf("rule %s: %w", r.ID, err)
	}
	r.Escalation = cfg
}
```

Add the validation function:

```go
func buildEscalationConfig(yr yamlRule) (*EscalationConfig, error) {
	if yr.Action == "unknown" {
		return nil, fmt.Errorf("effect: escalate not allowed on action: unknown — use deny instead")
	}
	channel := yr.Channel
	if channel == "" {
		channel = "hermes"
	}
	if channel != "hermes" && channel != "cli-only" {
		return nil, fmt.Errorf("channel: %q invalid (allowed: hermes, cli-only)", channel)
	}
	timeout := yr.TimeoutSeconds
	if timeout == 0 {
		timeout = 600
	}
	if timeout < 30 || timeout > 86400 {
		return nil, fmt.Errorf("timeout_seconds: %d out of range [30, 86400]", timeout)
	}
	window := 300
	if yr.RememberWindowSeconds != nil {
		window = *yr.RememberWindowSeconds
		if window < 0 {
			return nil, fmt.Errorf("remember_window_seconds: %d (must be >= 0)", window)
		}
	}
	return &EscalationConfig{
		Channel:               channel,
		TimeoutSeconds:        timeout,
		RememberWindowSeconds: window,
		NotifyTemplate:        yr.NotifyTemplate,
	}, nil
}
```

(Adapt struct names to match the actual yaml-unmarshal types in policy.go.)

- [ ] **Step 5: Run to verify pass**

Run: `cd go/execution-kernel && go test ./internal/gov/ -run TestParseEscalateRule -v`
Expected: PASS (all 7 subtests)

- [ ] **Step 6: Commit**

```bash
git add go/execution-kernel/internal/gov/policy.go go/execution-kernel/internal/gov/policy_escalate_test.go
git -c user.name=red -c user.email=jpleva91@gmail.com commit -m "feat(gov): parse + validate \"effect: escalate\" + four optional fields

Parser builds EscalationConfig with sensible defaults (channel=hermes,
timeout=600, remember_window=300). Validation rejects: timeout out of
[30,86400], unknown channel, escalate-on-unknown-action, negative
remember_window."
```

---

## Phase 4 — Gate integration

### Task 9: Wait — the polling helper (without notify; mocked-in tests only)

**Files:**
- Modify: `internal/gov/escalate.go`
- Modify: `internal/gov/escalate_test.go`

- [ ] **Step 1: Failing test for Wait — happy path (approve)**

Append to `escalate_test.go`:

```go
func TestWait_ApprovalUnblocks(t *testing.T) {
	store := mustOpenStore(t)
	defer store.Close()

	// Override the poll interval for fast tests.
	prev := WaitPollInterval
	WaitPollInterval = 50 * time.Millisecond
	defer func() { WaitPollInterval = prev }()

	cfg := EscalationConfig{
		Channel: "cli-only", TimeoutSeconds: 5, RememberWindowSeconds: 0,
	}
	a := Action{Type: ActShellExec, Target: "echo hi", Path: "/tmp"}

	// Spawn the Wait in a goroutine; resolve from the test thread.
	type result struct {
		res Resolution
		err error
	}
	resCh := make(chan result, 1)
	var insertedID string
	go func() {
		res, err := store.Wait(WaitArgs{
			RuleID: "test-rule", Agent: "agent-1", Action: a,
			Reason: "test reason", Config: cfg,
			NotifyFn: func(string, PendingApproval) error { return nil },
			OnInsert: func(id string) { insertedID = id },
		})
		resCh <- result{res, err}
	}()

	// Wait for the row to land, then approve.
	deadline := time.Now().Add(2 * time.Second)
	for insertedID == "" && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if insertedID == "" {
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

	cfg := EscalationConfig{Channel: "cli-only", TimeoutSeconds: 1, RememberWindowSeconds: 0}
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

	cfg := EscalationConfig{Channel: "cli-only", TimeoutSeconds: 5, RememberWindowSeconds: 300}
	a := Action{Type: ActShellExec, Target: "x", Path: "/tmp"}

	resCh := make(chan Resolution, 1)
	var insertedID string
	go func() {
		r, _ := store.Wait(WaitArgs{
			RuleID: "r", Agent: "a", Action: a, Reason: "x", Config: cfg,
			NotifyFn: func(string, PendingApproval) error { return nil },
			OnInsert: func(id string) { insertedID = id },
		})
		resCh <- r
	}()

	for insertedID == "" {
		time.Sleep(10 * time.Millisecond)
	}
	_ = store.ResolveApprove(insertedID, "operator-cli", 300)
	r := <-resCh

	if r.GrantedWindowSeconds != 300 {
		t.Errorf("GrantedWindowSeconds = %d, want 300", r.GrantedWindowSeconds)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `cd go/execution-kernel && go test ./internal/gov/ -run TestWait -v`
Expected: FAIL — undefined Wait, Resolution, WaitArgs, WaitPollInterval

- [ ] **Step 3: Implement Wait + Resolution**

Append to `escalate.go`:

```go
// WaitPollInterval is how often Wait checks the row for resolution.
// Exported as a var so tests can override (default: 2s).
var WaitPollInterval = 2 * time.Second

// Resolution is what Wait returns.
type Resolution struct {
	EscalationID         string
	Approved             bool
	OperatorReason       string
	GrantedWindowSeconds int
}

// OutcomeRuleID returns the chain rule_id that the gate should stamp
// on the resolved Decision.
func (r Resolution) OutcomeRuleID() string {
	if r.Approved {
		return "escalate-approved"
	}
	if r.OperatorReason == "" {
		return "escalate-timeout"
	}
	return "escalate-denied"
}

type WaitArgs struct {
	RuleID   string
	Agent    string
	Action   Action
	Reason   string
	Config   EscalationConfig
	NotifyFn func(id string, p PendingApproval) error // mockable; pass real notifyHermes from caller
	OnInsert func(id string)                          // optional test hook fired after row is inserted
}

func (s *EscalateStore) Wait(args WaitArgs) (Resolution, error) {
	id := newULID()
	now := nowUnix()

	paramsJSON := ""
	if args.Action.Params != nil {
		if b, err := json.Marshal(args.Action.Params); err == nil {
			paramsJSON = string(b)
		}
	}

	row := PendingApproval{
		ID: id, Agent: args.Agent, RuleID: args.RuleID,
		ActionType: string(args.Action.Type), ActionTarget: args.Action.Target,
		ActionParams: paramsJSON, Cwd: args.Action.Path, Reason: args.Reason,
		Channel: args.Config.Channel, TimeoutSeconds: args.Config.TimeoutSeconds,
		RememberWindowSeconds: args.Config.RememberWindowSeconds,
		CreatedTs: now,
	}
	if err := s.InsertPending(row); err != nil {
		return Resolution{EscalationID: id}, err
	}
	if args.OnInsert != nil {
		args.OnInsert(id)
	}

	// Fire notify in the background; failures get stamped on the row
	// but don't fail the Wait (CLI fallback still works).
	if args.Config.Channel == "hermes" && args.NotifyFn != nil {
		go func() { _ = args.NotifyFn(id, row) }()
	}

	deadline := time.Unix(now, 0).Add(time.Duration(args.Config.TimeoutSeconds) * time.Second)
	ticker := time.NewTicker(WaitPollInterval)
	defer ticker.Stop()
	for {
		<-ticker.C
		got, err := s.GetPending(id)
		if err != nil {
			return Resolution{EscalationID: id}, err
		}
		if got.ResolvedTs != nil {
			grant := 0
			if got.RememberGrantSeconds != nil {
				grant = *got.RememberGrantSeconds
			}
			return Resolution{
				EscalationID: id,
				Approved:     got.Resolution == "approved",
				OperatorReason: got.ResolutionReason,
				GrantedWindowSeconds: grant,
			}, nil
		}
		if timeNow().After(deadline) {
			_ = s.ResolveTimeout(id)
			return Resolution{EscalationID: id, Approved: false}, nil
		}
	}
}

// newULID is a hook so tests can produce predictable IDs. Default
// uses the existing chitin convention (see envelope ULID gen).
var newULID = func() string {
	// Use crypto/rand-backed ULID. Replace with the project's existing
	// ULID helper if one exists; this is a placeholder.
	return generateULID()
}
```

You'll also need `import "encoding/json"` and a `generateULID()` helper. Search for an existing ULID generator in the codebase first:

Run: `grep -rn "ulid\|ULID" go/execution-kernel/internal/ | head`

If one exists (e.g., in `internal/chain/` or `internal/envelope/`), reuse it. If not, add a tiny helper:

```go
// generateULID returns a 26-character Crockford-base32 ULID. If the
// project already has a shared helper, replace this with that.
func generateULID() string {
	var b [16]byte
	binary.BigEndian.PutUint64(b[:8], uint64(time.Now().UnixMilli()))
	_, _ = rand.Read(b[8:])
	return strings.ToUpper(hex.EncodeToString(b[:]))
}
```

(Use `crypto/rand`, `encoding/binary`, `encoding/hex`, `strings` — adjust imports.)

- [ ] **Step 4: Run to verify pass**

Run: `cd go/execution-kernel && go test ./internal/gov/ -run TestWait -v`
Expected: PASS (all three subtests)

- [ ] **Step 5: Commit**

```bash
git add go/execution-kernel/internal/gov/escalate.go go/execution-kernel/internal/gov/escalate_test.go
git -c user.name=red -c user.email=jpleva91@gmail.com commit -m "feat(gov): Wait — polling escalation resolver + Resolution type

Wait inserts the pending row, fires notify in the background, polls
every WaitPollInterval (default 2s) until resolved or timeout.
Returns Resolution with the rule_id the gate should stamp
(escalate-approved | escalate-denied | escalate-timeout)."
```

---

### Task 10: Wire the escalate branch into `gate.Evaluate`

**Files:**
- Modify: `internal/gov/gate.go`
- Modify: `internal/gov/gate_test.go`

- [ ] **Step 1: Failing test for the gate's escalate path**

Append to `gate_test.go` (or create a new gate_escalate_test.go):

```go
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
		ID: "test-escalate", Action: "shell.exec", Effect: EffectEscalate,
		Reason: "needs operator",
		Escalation: &EscalationConfig{
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

func TestGate_EscalateRememberGrantShortCircuits(t *testing.T) {
	g, dir := newTestGate(t)
	store, err := OpenEscalateStore(filepath.Join(dir, "p.sqlite"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	defer store.Close()
	g.EscalateStore = store

	g.Policy.Rules = []Rule{{
		ID: "test-escalate", Action: "shell.exec", Effect: EffectEscalate,
		Reason: "needs operator",
		Escalation: &EscalationConfig{
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
```

- [ ] **Step 2: Run to verify failure**

Run: `cd go/execution-kernel && go test ./internal/gov/ -run TestGate_Escalate -v`
Expected: FAIL — Gate has no EscalateStore field; Evaluate has no escalate branch

- [ ] **Step 3: Add EscalateStore field to Gate**

In `gate.go`'s `type Gate struct`, add (after the existing fields):

```go
	// EscalateStore is the sqlite-backed pending-approval store used
	// when a rule's effect is EffectEscalate. nil-safe: if unset, an
	// escalate-effect rule degrades to deny (with a warning logged).
	EscalateStore *EscalateStore

	// NotifyHermes is invoked by Wait when channel=hermes. nil-safe:
	// if unset, the escalation queues but no notification fires
	// (operator must use the CLI fallback).
	NotifyHermes func(id string, p PendingApproval) error
```

- [ ] **Step 4: Add the escalate branch in Evaluate**

Locate the existing `Evaluate` flow in `gate.go`. After step 4 (monitor-mode override) and before step 6 (counter recording), insert:

```go
	// 4.5: escalate-effect resolution.
	// If the matched rule's Effect is EffectEscalate and the policy
	// said deny, check remember_grants first (short-circuit to allow
	// if found), otherwise block in Wait until operator resolves.
	if d.Effect == EffectEscalate && !d.Allowed && g.EscalateStore != nil {
		if g.EscalateStore.HasUnexpiredGrant(d.RuleID, agent) {
			d.Allowed = true
			d.RuleID = "escalate-remember-grant"
		} else {
			ruleConfig := findRuleEscalation(g.Policy.Rules, d.RuleID)
			if ruleConfig != nil {
				resolution, _ := g.EscalateStore.Wait(WaitArgs{
					RuleID: d.RuleID, Agent: agent, Action: a,
					Reason: d.Reason, Config: *ruleConfig,
					NotifyFn: g.NotifyHermes,
				})
				d.Allowed = resolution.Approved
				d.RuleID = resolution.OutcomeRuleID()
				d.Reason = resolution.OperatorReason
				d.EscalationID = resolution.EscalationID
				if resolution.Approved && resolution.GrantedWindowSeconds > 0 {
					_ = g.EscalateStore.InsertGrant(
						resolution.OutcomeRuleID(), agent, resolution.GrantedWindowSeconds,
					)
					// NB: the grant is keyed on the resolved rule_id
					// ("escalate-approved"), not the original rule_id.
					// This means a repeat call hits the same rule, gets
					// EffectEscalate, looks up the grant under the
					// original rule_id — they MUST match. Use the
					// original RuleID for grant insert/lookup parity.
				}
			}
		}
	}
```

Wait — that grant-key bug is real. Fix to use the *original* rule_id from the matched rule, not the resolved one:

```go
				if resolution.Approved && resolution.GrantedWindowSeconds > 0 {
					_ = g.EscalateStore.InsertGrant(
						/* original */ findOriginalRuleID(g.Policy.Rules, d.EscalationID),
						agent, resolution.GrantedWindowSeconds,
					)
				}
```

Even cleaner: capture the original rule id BEFORE we overwrite d.RuleID:

```go
		} else {
			originalRuleID := d.RuleID
			ruleConfig := findRuleEscalation(g.Policy.Rules, originalRuleID)
			if ruleConfig != nil {
				resolution, _ := g.EscalateStore.Wait(WaitArgs{
					RuleID: originalRuleID, Agent: agent, Action: a,
					Reason: d.Reason, Config: *ruleConfig,
					NotifyFn: g.NotifyHermes,
				})
				d.Allowed = resolution.Approved
				d.RuleID = resolution.OutcomeRuleID()
				d.Reason = resolution.OperatorReason
				d.EscalationID = resolution.EscalationID
				if resolution.Approved && resolution.GrantedWindowSeconds > 0 {
					_ = g.EscalateStore.InsertGrant(originalRuleID, agent, resolution.GrantedWindowSeconds)
				}
			}
		}
```

Add the helper at the bottom of `gate.go`:

```go
// findRuleEscalation returns the EscalationConfig for the rule with
// the given id, or nil if no such rule (or the rule has no escalate
// config — shouldn't happen if parser invariants hold).
func findRuleEscalation(rules []Rule, ruleID string) *EscalationConfig {
	for _, r := range rules {
		if r.ID == ruleID {
			return r.Escalation
		}
	}
	return nil
}
```

- [ ] **Step 5: Make sure d.Effect is set during step 2 (policy evaluation)**

Find where the policy match sets the Decision. Add the line that copies the matched rule's Effect onto the Decision (likely a one-liner change in `Policy.Evaluate`). If Effect doesn't propagate, the new branch never fires.

- [ ] **Step 6: Run to verify pass**

Run: `cd go/execution-kernel && go test ./internal/gov/ -run TestGate_Escalate -v`
Expected: PASS (both subtests)

Then run the full gov suite to check for regressions:

Run: `cd go/execution-kernel && go test ./internal/gov/ -v 2>&1 | tail -10`
Expected: all existing tests still PASS

- [ ] **Step 7: Commit**

```bash
git add go/execution-kernel/internal/gov/gate.go go/execution-kernel/internal/gov/gate_test.go
git -c user.name=red -c user.email=jpleva91@gmail.com commit -m "feat(gov): wire escalate branch into Gate.Evaluate

EscalateStore + NotifyHermes hooks on Gate. New step 4.5 between policy
eval and counter recording: short-circuit on remember_grant hit,
otherwise Wait for operator resolution. Original rule_id used as the
grant key (not the resolved escalate-approved rule_id) so the next
call matches. EscalationID stamped on the Decision."
```

---

### Task 11: Sweeper — resolve orphaned rows on startup

**Files:**
- Modify: `internal/gov/escalate.go`
- Modify: `internal/gov/escalate_test.go`

- [ ] **Step 1: Failing test for the sweeper**

Append to `escalate_test.go`:

```go
func TestSweepStaleEscalations_ResolvesPastDeadline(t *testing.T) {
	store := mustOpenStore(t)
	defer store.Close()

	now := int64(1700000000)
	timeNow = func() time.Time { return time.Unix(now, 0) }
	defer func() { timeNow = time.Now }()

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
```

- [ ] **Step 2: Run to verify failure**

Run: `cd go/execution-kernel && go test ./internal/gov/ -run TestSweepStale -v`
Expected: FAIL — undefined SweepStale

- [ ] **Step 3: Implement SweepStale**

Append to `escalate.go`:

```go
// SweepStale resolves all unresolved rows whose deadline has passed.
// Called at gate startup (recovers orphaned rows from a crashed
// kernel) and optionally on a timer. Returns count resolved.
func (s *EscalateStore) SweepStale() (int, error) {
	now := nowUnix()
	stale, err := s.ListUnresolvedPastDeadline(now)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, p := range stale {
		if err := s.ResolveTimeout(p.ID); err == nil {
			count++
		}
	}
	return count, nil
}
```

- [ ] **Step 4: Run to verify pass**

Run: `cd go/execution-kernel && go test ./internal/gov/ -run TestSweepStale -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add go/execution-kernel/internal/gov/escalate.go go/execution-kernel/internal/gov/escalate_test.go
git -c user.name=red -c user.email=jpleva91@gmail.com commit -m "feat(gov): SweepStale — resolve orphaned escalations at startup"
```

---

## Phase 5 — CLI Handler A (`pending list/approve/deny`)

### Task 12: `pending list` CLI subcommand + JSON output

**Files:**
- Create: `cmd/chitin-kernel/pending.go`
- Create: `cmd/chitin-kernel/pending_test.go`

- [ ] **Step 1: Failing test for `pending list`**

Create `cmd/chitin-kernel/pending_test.go`:

```go
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
```

- [ ] **Step 2: Run to verify failure**

Run: `cd go/execution-kernel && go test ./cmd/chitin-kernel/ -run TestPendingList -v`
Expected: FAIL — undefined pendingList

- [ ] **Step 3: Implement pendingList**

Create `cmd/chitin-kernel/pending.go`:

```go
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"text/tabwriter"
	"time"

	"github.com/chitinhq/chitin/go/execution-kernel/internal/gov"
)

// pendingList writes the unresolved pending_approvals rows to out.
// If asJSON is true, emits a JSON array; else a tab-formatted table.
func pendingList(store *gov.EscalateStore, out io.Writer, asJSON bool) error {
	rows, err := store.ListUnresolved()
	if err != nil {
		return err
	}
	if asJSON {
		simple := make([]map[string]any, len(rows))
		for i, r := range rows {
			simple[i] = map[string]any{
				"id":              r.ID,
				"agent":           r.Agent,
				"rule_id":         r.RuleID,
				"action_type":     r.ActionType,
				"action_target":   r.ActionTarget,
				"reason":          r.Reason,
				"channel":         r.Channel,
				"created_ts":      r.CreatedTs,
				"timeout_seconds": r.TimeoutSeconds,
			}
		}
		b, err := json.Marshal(simple)
		if err != nil {
			return err
		}
		_, err = out.Write(b)
		return err
	}
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tAGE\tAGENT\tRULE\tTARGET\tEXPIRES_IN")
	now := time.Now().Unix()
	for _, r := range rows {
		age := now - r.CreatedTs
		expIn := r.CreatedTs + int64(r.TimeoutSeconds) - now
		target := r.ActionTarget
		if len(target) > 60 {
			target = target[:57] + "..."
		}
		fmt.Fprintf(tw, "%s\t%ds\t%s\t%s\t%s\t%ds\n",
			r.ID, age, r.Agent, r.RuleID, target, expIn)
	}
	return tw.Flush()
}
```

- [ ] **Step 4: Run to verify pass**

Run: `cd go/execution-kernel && go test ./cmd/chitin-kernel/ -run TestPendingList -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add go/execution-kernel/cmd/chitin-kernel/pending.go go/execution-kernel/cmd/chitin-kernel/pending_test.go
git -c user.name=red -c user.email=jpleva91@gmail.com commit -m "feat(cli): pending list — JSON + tab-formatted text output"
```

---

### Task 13: `pending approve` + `pending deny` CLI subcommands

**Files:**
- Modify: `cmd/chitin-kernel/pending.go`
- Modify: `cmd/chitin-kernel/pending_test.go`

- [ ] **Step 1: Failing test for approve + deny**

Append to `pending_test.go`:

```go
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
```

- [ ] **Step 2: Run to verify failure**

Run: `cd go/execution-kernel && go test ./cmd/chitin-kernel/ -run TestPendingApprove -v`
Expected: FAIL — undefined pendingApprove, pendingDeny

- [ ] **Step 3: Implement approve + deny**

Append to `pending.go`:

```go
func pendingApprove(store *gov.EscalateStore, id string, windowSeconds int) error {
	return store.ResolveApprove(id, "operator-cli", windowSeconds)
}

func pendingDeny(store *gov.EscalateStore, id string, reason string) error {
	return store.ResolveDeny(id, "operator-cli", reason)
}
```

- [ ] **Step 4: Run to verify pass**

Run: `cd go/execution-kernel && go test ./cmd/chitin-kernel/ -run TestPendingApprove -v ; go test ./cmd/chitin-kernel/ -run TestPendingDeny -v`
Expected: both PASS

- [ ] **Step 5: Commit**

```bash
git add go/execution-kernel/cmd/chitin-kernel/pending.go go/execution-kernel/cmd/chitin-kernel/pending_test.go
git -c user.name=red -c user.email=jpleva91@gmail.com commit -m "feat(cli): pending approve + deny + already-resolved refusal"
```

---

### Task 14: UID auth check

**Files:**
- Modify: `cmd/chitin-kernel/pending.go`
- Modify: `cmd/chitin-kernel/pending_test.go`

- [ ] **Step 1: Failing test for UID mismatch**

Append to `pending_test.go`:

```go
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
```

Add `import "strings"` to the test file.

- [ ] **Step 2: Run to verify failure**

Run: `cd go/execution-kernel && go test ./cmd/chitin-kernel/ -run TestPendingAuth -v`
Expected: FAIL — undefined authPendingFile

- [ ] **Step 3: Implement authPendingFile**

Append to `pending.go`:

```go
import (
	"os"
	"syscall"
)

// statOwnerUID + selfUID are mockable hooks. Production uses os.Stat
// and os.Geteuid; tests inject fakes.
var statOwnerUID = func(path string) (uint32, error) {
	st, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	sys, ok := st.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, fmt.Errorf("stat sys: not a Stat_t")
	}
	return sys.Uid, nil
}

var selfUID = func() uint32 { return uint32(os.Geteuid()) }

// authPendingFile returns nil if the current process's effective uid
// owns the pending_approvals.sqlite file. Otherwise returns
// "pending_unauthorized: ..." — caller should exit 2.
func authPendingFile(dbPath string) error {
	owner, err := statOwnerUID(dbPath)
	if err != nil {
		return fmt.Errorf("pending_unauthorized: stat %s: %w", dbPath, err)
	}
	self := selfUID()
	if owner != self {
		return fmt.Errorf("pending_unauthorized: file owned by uid %d, current uid %d", owner, self)
	}
	return nil
}
```

(Adjust imports — `os/exec` may already be imported; add `syscall`.)

- [ ] **Step 4: Run to verify pass**

Run: `cd go/execution-kernel && go test ./cmd/chitin-kernel/ -run TestPendingAuth -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add go/execution-kernel/cmd/chitin-kernel/pending.go go/execution-kernel/cmd/chitin-kernel/pending_test.go
git -c user.name=red -c user.email=jpleva91@gmail.com commit -m "feat(cli): authPendingFile — uid-owner check on pending_approvals.sqlite"
```

---

### Task 15: Wire `pending` subcommand dispatcher into main.go

**Files:**
- Modify: `cmd/chitin-kernel/main.go`
- Modify: `cmd/chitin-kernel/pending.go`

- [ ] **Step 1: Add cmdPending dispatcher to pending.go**

Append to `pending.go`:

```go
// cmdPending is the top-level dispatcher for `chitin-kernel pending <sub>`.
// Sub: list | approve | deny.
func cmdPending(args []string) {
	if len(args) < 1 {
		exitErr("pending_no_subcommand", "usage: chitin-kernel pending {list|approve|deny}")
	}
	sub, rest := args[0], args[1:]
	dbPath := filepath.Join(chitinDir(), "pending_approvals.sqlite")

	switch sub {
	case "list":
		asJSON := false
		for _, a := range rest {
			if a == "--json" {
				asJSON = true
			}
		}
		store, err := gov.OpenEscalateStore(dbPath)
		if err != nil {
			// File not existing yet is OK — list is just empty.
			if !os.IsNotExist(err) {
				exitErr("pending_open", err.Error())
			}
			if asJSON {
				fmt.Println("[]")
			}
			return
		}
		defer store.Close()
		if err := pendingList(store, os.Stdout, asJSON); err != nil {
			exitErr("pending_list", err.Error())
		}

	case "approve":
		if len(rest) < 1 {
			exitErr("pending_approve_missing_id", "usage: chitin-kernel pending approve <id> [--window <duration>]")
		}
		id := rest[0]
		windowSec := 0
		for i, a := range rest {
			if a == "--window" && i+1 < len(rest) {
				d, err := time.ParseDuration(rest[i+1])
				if err != nil {
					exitErr("pending_bad_window", err.Error())
				}
				windowSec = int(d.Seconds())
			}
		}
		if err := authPendingFile(dbPath); err != nil {
			fmt.Fprintln(os.Stderr, err.Error())
			os.Exit(2)
		}
		store, err := gov.OpenEscalateStore(dbPath)
		if err != nil {
			exitErr("pending_open", err.Error())
		}
		defer store.Close()
		if err := pendingApprove(store, id, windowSec); err != nil {
			exitErr("pending_approve", err.Error())
		}
		fmt.Printf(`{"ok":true,"action":"approve","id":%q,"window_seconds":%d}`+"\n", id, windowSec)

	case "deny":
		if len(rest) < 1 {
			exitErr("pending_deny_missing_id", "usage: chitin-kernel pending deny <id> [--reason <text>]")
		}
		id := rest[0]
		reason := ""
		for i, a := range rest {
			if a == "--reason" && i+1 < len(rest) {
				reason = rest[i+1]
			}
		}
		if err := authPendingFile(dbPath); err != nil {
			fmt.Fprintln(os.Stderr, err.Error())
			os.Exit(2)
		}
		store, err := gov.OpenEscalateStore(dbPath)
		if err != nil {
			exitErr("pending_open", err.Error())
		}
		defer store.Close()
		if err := pendingDeny(store, id, reason); err != nil {
			exitErr("pending_deny", err.Error())
		}
		fmt.Printf(`{"ok":true,"action":"deny","id":%q,"reason":%q}`+"\n", id, reason)

	default:
		exitErr("pending_unknown_subcommand", sub)
	}
}
```

(`exitErr` and `chitinDir` already exist in main.go; reuse.)

- [ ] **Step 2: Wire `cmdPending` into main.go's top-level switch**

Find the existing top-level subcommand switch in `main.go` (search for `case "gate":` or similar). Add:

```go
	case "pending":
		cmdPending(args)
```

- [ ] **Step 3: Build and smoke-test manually**

Run: `cd go/execution-kernel && go build -o /tmp/chitin-kernel-dev ./cmd/chitin-kernel && /tmp/chitin-kernel-dev pending list 2>&1 | head -5`
Expected: empty list (no rows yet) without error

- [ ] **Step 4: Commit**

```bash
git add go/execution-kernel/cmd/chitin-kernel/pending.go go/execution-kernel/cmd/chitin-kernel/main.go
git -c user.name=red -c user.email=jpleva91@gmail.com commit -m "feat(cli): wire \"chitin-kernel pending list/approve/deny\" subcommand"
```

---

## Phase 6 — Hermes investigation (open item from spec)

### Task 16: Confirm hermes message-send + reply-listing CLI surface

**Files:**
- Create: `docs/observations/2026-05-DD-hermes-cli-surface-for-pending-approvals.md` (DD = day of execution)

- [ ] **Step 1: Inspect hermes CLI capabilities for message send**

Run: `hermes message --help 2>&1 | head -30`
Run: `hermes message send --help 2>&1 | head -30`

Document the actual flag set: does it support a `--correlation-id` or equivalent? What does it return on success (JSON envelope with message_id?)? What does failure look like?

- [ ] **Step 2: Inspect hermes capabilities for inbound replies**

Run: `hermes message --help 2>&1`

Look for: `list`, `tail`, `subscribe`, `watch`, or similar. Document whether hermes can:
- List messages on a channel since a cursor (poll-friendly)
- Subscribe to new messages via long-running stream (better)
- Neither (we'd need to build a hermes plugin)

- [ ] **Step 3: Inspect hermes config to find the operator's channel name**

Run: `head -40 ~/.hermes/config.yaml 2>&1`

Document: what's the operator's primary channel ID (slack channel, whatsapp number, etc.)? Where is it configured?

- [ ] **Step 4: Write the observation note**

Create `docs/observations/<today>-hermes-cli-surface-for-pending-approvals.md` with:
- The exact `hermes message send` flag set (copy from --help)
- Whether `--correlation-id` (or equivalent) is supported
- Whether `hermes message list` (or equivalent) exists
- The operator-channel resolution path
- Any gaps that would force us to embed correlation IDs in the message body (Plan B from the spec)
- A recommendation for B1 + B2 implementation based on what the surface actually supports

- [ ] **Step 5: Commit the observation**

```bash
git add docs/observations/*hermes-cli-surface*.md
git -c user.name=red -c user.email=jpleva91@gmail.com commit -m "docs(observations): hermes CLI surface for pending-approval notify + reply"
```

If the hermes surface is missing what we need, **stop here and surface to the operator** — Tasks 17-19 may need redesign.

---

## Phase 7 — Hermes Handler B (depends on Task 16)

### Task 17: notifyHermes — outbound shell-out + template rendering

**Files:**
- Create: `cmd/chitin-kernel/notify_hermes.go`
- Create: `cmd/chitin-kernel/notify_hermes_test.go`

- [ ] **Step 1: Failing test — notifyHermes shells out with the right args**

Create `cmd/chitin-kernel/notify_hermes_test.go`:

```go
package main

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/chitinhq/chitin/go/execution-kernel/internal/gov"
)

func TestNotifyHermes_ShellsOutWithRenderedBody(t *testing.T) {
	var captured struct {
		bin  string
		args []string
	}
	prev := execHermes
	execHermes = func(bin string, args []string) ([]byte, error) {
		captured.bin = bin
		captured.args = args
		return []byte(`{"message_id":"msg-fake-1"}`), nil
	}
	defer func() { execHermes = prev }()

	row := gov.PendingApproval{
		ID: "01TEST", Agent: "claude-code", ActionType: "file.write",
		ActionTarget: "/etc/hostname", Reason: "system path write",
		Channel: "hermes", TimeoutSeconds: 600,
	}

	cfg := operatorConfig{Channel: "ops-approvals", HermesBin: "hermes"}

	msgID, err := notifyHermes("01TEST", row, cfg)
	if err != nil {
		t.Fatalf("notify: %v", err)
	}
	if msgID != "msg-fake-1" {
		t.Errorf("msgID = %q, want msg-fake-1", msgID)
	}
	if captured.bin != "hermes" {
		t.Errorf("bin = %q", captured.bin)
	}
	joined := strings.Join(captured.args, " ")
	if !strings.Contains(joined, "ops-approvals") {
		t.Errorf("missing channel in args: %q", joined)
	}
	if !strings.Contains(joined, "01TEST") {
		t.Errorf("missing escalation id in args (or body): %q", joined)
	}
}

func TestRenderNotifyTemplate_BuiltinDefault(t *testing.T) {
	row := gov.PendingApproval{
		ID: "01TEST", Agent: "claude-code", ActionType: "file.write",
		ActionTarget: "/etc/hostname", Reason: "system path write",
		TimeoutSeconds: 600,
	}
	var buf bytes.Buffer
	if err := renderNotifyTemplate(&buf, "" /* use default */, row); err != nil {
		t.Fatalf("render: %v", err)
	}
	body := buf.String()
	for _, want := range []string{"01TEST", "claude-code", "file.write", "/etc/hostname", "approve", "deny"} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q: %q", want, body)
		}
	}
}

func TestRenderNotifyTemplate_Override(t *testing.T) {
	row := gov.PendingApproval{ID: "01X", Agent: "a", ActionType: "shell.exec", ActionTarget: "ls"}
	tpl := "ID={{.ID}} TARGET={{.ActionTarget}}"
	var buf bytes.Buffer
	if err := renderNotifyTemplate(&buf, tpl, row); err != nil {
		t.Fatalf("render: %v", err)
	}
	want := "ID=01X TARGET=ls"
	if buf.String() != want {
		t.Errorf("got %q, want %q", buf.String(), want)
	}
}

// Sanity: %v in fmt to silence unused-import warnings if test
// shrinks during edits.
var _ = fmt.Sprintf
```

- [ ] **Step 2: Run to verify failure**

Run: `cd go/execution-kernel && go test ./cmd/chitin-kernel/ -run TestNotifyHermes -v ; go test ./cmd/chitin-kernel/ -run TestRenderNotifyTemplate -v`
Expected: FAIL — undefined symbols

- [ ] **Step 3: Implement notifyHermes + template rendering**

Create `cmd/chitin-kernel/notify_hermes.go`:

```go
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"text/template"

	"github.com/chitinhq/chitin/go/execution-kernel/internal/gov"
)

// execHermes is a mockable hook around the hermes CLI shell-out.
var execHermes = func(bin string, args []string) ([]byte, error) {
	return exec.Command(bin, args...).Output()
}

// operatorConfig is the resolved view of ~/.chitin/operator.yaml.
type operatorConfig struct {
	Channel   string
	HermesBin string
}

// builtinNotifyTemplate is the default body when the rule doesn't
// override notify_template. Operators see this in their hermes channel.
const builtinNotifyTemplate = `Operator approval needed
  agent:    {{.Agent}}
  action:   {{.ActionType}} on {{.ActionTarget}}
  reason:   {{.Reason}}
  timeout:  {{.TimeoutSeconds}}s

Reply on this thread:
  ` + "`approve`" + `              -> single call
  ` + "`approve 30m`" + `          -> approve + grant 30 min for this rule
  ` + "`deny`" + ` or ` + "`deny <reason>`" + ` -> deny

Or from a terminal: chitin-kernel approve {{.ID}} [--window 30m]`

// renderNotifyTemplate writes the body to w. If tpl is empty, uses
// builtinNotifyTemplate. Templates only see the PendingApproval
// fields — no shell exec, no arbitrary disk reads.
func renderNotifyTemplate(w io.Writer, tpl string, row gov.PendingApproval) error {
	if tpl == "" {
		tpl = builtinNotifyTemplate
	}
	t, err := template.New("notify").Parse(tpl)
	if err != nil {
		return err
	}
	return t.Execute(w, row)
}

// notifyHermes shells out to `hermes message send` with the rendered
// body. Returns the hermes message_id (used to correlate replies in
// Handler B2) or an error. The escalation id is included in the body
// AND, if hermes supports it, as a separate flag — Task 16's
// observation note records which.
func notifyHermes(id string, row gov.PendingApproval, cfg operatorConfig) (string, error) {
	var buf bytes.Buffer
	if err := renderNotifyTemplate(&buf, "" /* the rule's template comes through row.NotifyTemplate when threaded */, row); err != nil {
		return "", err
	}
	args := []string{
		"message", "send",
		"--channel", cfg.Channel,
		"--body", buf.String(),
	}
	// If hermes supports --correlation-id (per Task 16's observation),
	// append it. Else the id is in the body and the parser pulls it out.
	args = append(args, "--correlation-id", id)
	out, err := execHermes(cfg.HermesBin, args)
	if err != nil {
		return "", fmt.Errorf("hermes message send failed: %w", err)
	}
	var sent struct {
		MessageID string `json:"message_id"`
	}
	if err := json.Unmarshal(out, &sent); err != nil {
		// Hermes returned non-JSON output. Not fatal — we still queued
		// the row, operator can use CLI fallback. Return empty msg id.
		return "", nil
	}
	return sent.MessageID, nil
}
```

- [ ] **Step 4: Run to verify pass**

Run: `cd go/execution-kernel && go test ./cmd/chitin-kernel/ -run TestNotifyHermes -v ; go test ./cmd/chitin-kernel/ -run TestRenderNotifyTemplate -v`
Expected: both PASS

- [ ] **Step 5: Commit**

```bash
git add go/execution-kernel/cmd/chitin-kernel/notify_hermes.go go/execution-kernel/cmd/chitin-kernel/notify_hermes_test.go
git -c user.name=red -c user.email=jpleva91@gmail.com commit -m "feat(notify): notifyHermes + renderNotifyTemplate

Outbound notify shells out to \`hermes message send\` with the
rendered body. Built-in template covers the common case;
per-rule notify_template overrides it. execHermes is a
mockable hook for tests."
```

---

### Task 18: Inbound reply parser

**Files:**
- Modify: `cmd/chitin-kernel/notify_hermes.go`
- Modify: `cmd/chitin-kernel/notify_hermes_test.go`

- [ ] **Step 1: Failing test for the reply parser**

Append to `notify_hermes_test.go`:

```go
func TestParseHermesReply(t *testing.T) {
	cases := []struct {
		in            string
		wantApproved  bool
		wantWindowSec int
		wantDenied    bool
		wantReason    string
		wantUnparsed  bool
	}{
		{in: "approve", wantApproved: true},
		{in: "  Approve  ", wantApproved: true}, // case-insensitive, trim
		{in: "approve 30m", wantApproved: true, wantWindowSec: 1800},
		{in: "approve 1h", wantApproved: true, wantWindowSec: 3600},
		{in: "deny", wantDenied: true},
		{in: "deny no thank you", wantDenied: true, wantReason: "no thank you"},
		{in: "lol what", wantUnparsed: true},
		{in: "", wantUnparsed: true},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got, err := parseHermesReply(tc.in)
			if tc.wantUnparsed {
				if err == nil {
					t.Errorf("expected unparsed error, got %+v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("err: %v", err)
			}
			if got.Approved != tc.wantApproved {
				t.Errorf("Approved = %v, want %v", got.Approved, tc.wantApproved)
			}
			if got.Denied != tc.wantDenied {
				t.Errorf("Denied = %v, want %v", got.Denied, tc.wantDenied)
			}
			if got.WindowSeconds != tc.wantWindowSec {
				t.Errorf("WindowSeconds = %d, want %d", got.WindowSeconds, tc.wantWindowSec)
			}
			if got.Reason != tc.wantReason {
				t.Errorf("Reason = %q, want %q", got.Reason, tc.wantReason)
			}
		})
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `cd go/execution-kernel && go test ./cmd/chitin-kernel/ -run TestParseHermesReply -v`
Expected: FAIL — undefined parseHermesReply

- [ ] **Step 3: Implement parseHermesReply**

Append to `notify_hermes.go`:

```go
import (
	"strings"
	"time"
)

// HermesReplyParse is what parseHermesReply returns on a successful parse.
type HermesReplyParse struct {
	Approved      bool
	Denied        bool
	WindowSeconds int    // optional; 0 means "use rule default"
	Reason        string // optional; only set on deny
}

// parseHermesReply turns a chat reply body into a structured parse.
// Returns an error for unparseable input (caller ignores those —
// the operator may have replied with prose unrelated to approval).
func parseHermesReply(body string) (HermesReplyParse, error) {
	trimmed := strings.TrimSpace(body)
	if trimmed == "" {
		return HermesReplyParse{}, fmt.Errorf("empty reply")
	}
	lower := strings.ToLower(trimmed)
	switch {
	case lower == "approve":
		return HermesReplyParse{Approved: true}, nil
	case strings.HasPrefix(lower, "approve "):
		rest := strings.TrimSpace(trimmed[len("approve "):])
		dur, err := time.ParseDuration(rest)
		if err != nil {
			return HermesReplyParse{}, fmt.Errorf("approve <duration>: %w", err)
		}
		return HermesReplyParse{Approved: true, WindowSeconds: int(dur.Seconds())}, nil
	case lower == "deny":
		return HermesReplyParse{Denied: true}, nil
	case strings.HasPrefix(lower, "deny "):
		reason := strings.TrimSpace(trimmed[len("deny "):])
		return HermesReplyParse{Denied: true, Reason: reason}, nil
	}
	return HermesReplyParse{}, fmt.Errorf("unparsed reply: %q", trimmed)
}
```

- [ ] **Step 4: Run to verify pass**

Run: `cd go/execution-kernel && go test ./cmd/chitin-kernel/ -run TestParseHermesReply -v`
Expected: PASS (all 8 subtests)

- [ ] **Step 5: Commit**

```bash
git add go/execution-kernel/cmd/chitin-kernel/notify_hermes.go go/execution-kernel/cmd/chitin-kernel/notify_hermes_test.go
git -c user.name=red -c user.email=jpleva91@gmail.com commit -m "feat(notify): parseHermesReply — approve/deny + duration + reason"
```

---

### Task 19: `pending watch-hermes` subcommand + correlation strategies

**Files:**
- Modify: `cmd/chitin-kernel/notify_hermes.go`
- Modify: `cmd/chitin-kernel/pending.go`

- [ ] **Step 1: Implement watchHermes — single-tick poll**

The shape depends heavily on Task 16's findings. Sketch (assuming a `hermes message list --channel <X> --since <cursor>` exists):

Append to `notify_hermes.go`:

```go
// watchHermesOnce polls hermes for new messages on the operator
// channel since the cursor file's last_ts, parses each, and resolves
// matching pending_approvals rows. Returns the count resolved.
//
// Cursor: ~/.chitin/hermes-watch-cursor (just a unix timestamp).
// Resolution correlation: prefers the message's correlation_id if
// hermes returns one; falls back to body-embedded escalation id
// (`approve 01TEST...`); falls back to "single unresolved row" if
// only one is pending.
func watchHermesOnce(store *gov.EscalateStore, cfg operatorConfig, cursorPath string) (int, error) {
	cursor := readCursor(cursorPath) // 0 if missing
	args := []string{
		"message", "list",
		"--channel", cfg.Channel,
		"--since", fmt.Sprintf("%d", cursor),
		"--json",
	}
	out, err := execHermes(cfg.HermesBin, args)
	if err != nil {
		return 0, fmt.Errorf("hermes message list: %w", err)
	}
	var msgs []hermesMessage
	if err := json.Unmarshal(out, &msgs); err != nil {
		return 0, fmt.Errorf("parse hermes list: %w", err)
	}

	resolved := 0
	maxTs := cursor
	for _, m := range msgs {
		if m.Ts > maxTs {
			maxTs = m.Ts
		}
		parsed, perr := parseHermesReply(m.Body)
		if perr != nil {
			continue // not an approval reply
		}
		// Correlate to a pending_approvals row.
		id, cerr := correlateReply(store, m, parsed)
		if cerr != nil {
			continue
		}
		// Apply.
		if parsed.Approved {
			if err := store.ResolveApprove(id, "hermes-reply", parsed.WindowSeconds); err == nil {
				resolved++
			}
		} else if parsed.Denied {
			if err := store.ResolveDeny(id, "hermes-reply", parsed.Reason); err == nil {
				resolved++
			}
		}
	}
	writeCursor(cursorPath, maxTs)
	return resolved, nil
}

type hermesMessage struct {
	MessageID     string `json:"message_id"`
	Ts            int64  `json:"ts"`
	Body          string `json:"body"`
	CorrelationID string `json:"correlation_id"` // empty if hermes doesn't support
}

// correlateReply finds the pending_approvals row this reply applies to.
// Strategies in order:
//   1. m.CorrelationID matches a row.
//   2. The body contains an escalation id substring.
//   3. There is exactly one unresolved row (and the reply is unambiguous).
// Returns (id, nil) on success or ("", err) if ambiguous / no match.
func correlateReply(store *gov.EscalateStore, m hermesMessage, p HermesReplyParse) (string, error) {
	if m.CorrelationID != "" {
		if _, err := store.GetPending(m.CorrelationID); err == nil {
			return m.CorrelationID, nil
		}
	}
	// Body-embedded id: look for any 26-char Crockford-base32-ish token.
	rows, err := store.ListUnresolved()
	if err != nil {
		return "", err
	}
	for _, r := range rows {
		if strings.Contains(m.Body, r.ID) {
			return r.ID, nil
		}
	}
	if len(rows) == 1 {
		return rows[0].ID, nil
	}
	return "", fmt.Errorf("ambiguous reply: %d unresolved rows, no id in body", len(rows))
}

func readCursor(path string) int64 {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	var ts int64
	_, _ = fmt.Sscanf(string(b), "%d", &ts)
	return ts
}

func writeCursor(path string, ts int64) {
	_ = os.WriteFile(path, []byte(fmt.Sprintf("%d\n", ts)), 0o600)
}
```

- [ ] **Step 2: Wire `pending watch-hermes` subcommand**

In `pending.go`'s `cmdPending` switch, add:

```go
	case "watch-hermes":
		cfg, err := loadOperatorConfig()
		if err != nil {
			exitErr("operator_config_missing", err.Error())
		}
		store, err := gov.OpenEscalateStore(dbPath)
		if err != nil {
			exitErr("pending_open", err.Error())
		}
		defer store.Close()
		cursorPath := filepath.Join(chitinDir(), "hermes-watch-cursor")
		count, err := watchHermesOnce(store, cfg, cursorPath)
		if err != nil {
			exitErr("watch_hermes", err.Error())
		}
		fmt.Printf(`{"ok":true,"action":"watch-hermes","resolved":%d}`+"\n", count)
```

- [ ] **Step 3: Build to verify**

Run: `cd go/execution-kernel && go build ./cmd/chitin-kernel/...`
Expected: success

- [ ] **Step 4: Commit**

```bash
git add go/execution-kernel/cmd/chitin-kernel/notify_hermes.go go/execution-kernel/cmd/chitin-kernel/pending.go
git -c user.name=red -c user.email=jpleva91@gmail.com commit -m "feat(notify): watchHermesOnce + pending watch-hermes subcommand

Single-tick poll of hermes message list, parses replies, correlates
via (correlation_id | body-embedded id | sole unresolved), writes
resolution. Driven by systemd timer (Task 21) every 30s in v1."
```

---

### Task 20: Operator config loader (`~/.chitin/operator.yaml`)

**Files:**
- Modify: `cmd/chitin-kernel/notify_hermes.go`
- Modify: `cmd/chitin-kernel/notify_hermes_test.go`

- [ ] **Step 1: Failing test for config loader**

Append to `notify_hermes_test.go`:

```go
func TestLoadOperatorConfig_AppliesDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "operator.yaml")
	_ = os.WriteFile(path, []byte("channel: ops-approvals\n"), 0o600)

	cfg, err := loadOperatorConfigFrom(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Channel != "ops-approvals" {
		t.Errorf("Channel = %q", cfg.Channel)
	}
	if cfg.HermesBin != "hermes" {
		t.Errorf("HermesBin default = %q, want hermes", cfg.HermesBin)
	}
}

func TestLoadOperatorConfig_MissingFileError(t *testing.T) {
	_, err := loadOperatorConfigFrom("/nonexistent/path")
	if err == nil {
		t.Error("expected error, got nil")
	}
}
```

Add `import "path/filepath"` to the test file.

- [ ] **Step 2: Run to verify failure**

Run: `cd go/execution-kernel && go test ./cmd/chitin-kernel/ -run TestLoadOperatorConfig -v`
Expected: FAIL — undefined loadOperatorConfigFrom

- [ ] **Step 3: Implement the loader**

Append to `notify_hermes.go`:

```go
import "gopkg.in/yaml.v3"

func loadOperatorConfig() (operatorConfig, error) {
	return loadOperatorConfigFrom(filepath.Join(chitinDir(), "operator.yaml"))
}

func loadOperatorConfigFrom(path string) (operatorConfig, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return operatorConfig{}, fmt.Errorf("read %s: %w", path, err)
	}
	var raw struct {
		Channel   string `yaml:"channel"`
		HermesBin string `yaml:"hermes_bin"`
	}
	if err := yaml.Unmarshal(b, &raw); err != nil {
		return operatorConfig{}, fmt.Errorf("parse %s: %w", path, err)
	}
	if raw.Channel == "" {
		return operatorConfig{}, fmt.Errorf("operator.yaml missing required field: channel")
	}
	if raw.HermesBin == "" {
		raw.HermesBin = "hermes"
	}
	return operatorConfig{Channel: raw.Channel, HermesBin: raw.HermesBin}, nil
}
```

(yaml.v3 is likely already a dependency; if not, `go get gopkg.in/yaml.v3@latest`.)

- [ ] **Step 4: Run to verify pass**

Run: `cd go/execution-kernel && go test ./cmd/chitin-kernel/ -run TestLoadOperatorConfig -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add go/execution-kernel/cmd/chitin-kernel/notify_hermes.go go/execution-kernel/cmd/chitin-kernel/notify_hermes_test.go
git -c user.name=red -c user.email=jpleva91@gmail.com commit -m "feat(notify): operator.yaml loader (channel, hermes_bin)"
```

---

### Task 21: Systemd unit for `pending watch-hermes`

**Files:**
- Create: `infra/systemd/chitin-pending-watch.service`
- Create: `infra/systemd/chitin-pending-watch.timer`

- [ ] **Step 1: Write the service unit**

Create `infra/systemd/chitin-pending-watch.service`:

```ini
[Unit]
Description=Poll hermes for operator approval replies; resolve pending_approvals

[Service]
Type=oneshot
ExecStart=%h/.local/bin/chitin-kernel pending watch-hermes
StandardOutput=journal
StandardError=journal

# Cap — single hermes shell-out + sqlite writes; no build, no network
# beyond hermes-gateway.
MemoryMax=128M
CPUQuota=50%
TimeoutStartSec=30

# Don't auto-restart — failures want operator visibility via the journal.
Restart=no
```

- [ ] **Step 2: Write the timer**

Create `infra/systemd/chitin-pending-watch.timer`:

```ini
[Unit]
Description=Trigger chitin-pending-watch every 30s

[Timer]
OnBootSec=1min
OnUnitActiveSec=30s
Persistent=true

Unit=chitin-pending-watch.service

[Install]
WantedBy=timers.target
```

- [ ] **Step 3: Smoke install via the existing installer**

Run: `bash scripts/install-systemd-units.sh --dry-run 2>&1 | grep pending-watch`
Expected: `would symlink (new) ~/.config/systemd/user/chitin-pending-watch.{service,timer}`

- [ ] **Step 4: Commit**

```bash
git add infra/systemd/chitin-pending-watch.service infra/systemd/chitin-pending-watch.timer
git -c user.name=red -c user.email=jpleva91@gmail.com commit -m "feat(ops): chitin-pending-watch systemd unit + 30s timer"
```

---

## Phase 8 — Integration tests + sweeper-on-startup

### Task 22: Wire EscalateStore + NotifyHermes into the gate's hook path

**Files:**
- Modify: `cmd/chitin-kernel/gate_hook.go`

- [ ] **Step 1: Open EscalateStore in evalHookStdin and assign to gate**

Find the `gate := &gov.Gate{...}` struct literal in `evalHookStdin`. After setting up the existing fields, add:

```go
	// Operator-approval escalation. Open the pending_approvals store
	// + wire the hermes notify hook so rules with effect=escalate can
	// trigger real-time operator approval. Both nil-safe — if Open
	// fails or operator config is missing, escalate-effect rules
	// degrade to deny (with a stderr warning).
	if escalStore, eerr := gov.OpenEscalateStore(filepath.Join(cdir, "pending_approvals.sqlite")); eerr == nil {
		// One-shot sweep of orphaned rows from previous crashes.
		_, _ = escalStore.SweepStale()
		gate.EscalateStore = escalStore
		defer escalStore.Close()
		// Notify hook: only wire if operator.yaml loads cleanly.
		if cfg, cerr := loadOperatorConfig(); cerr == nil {
			gate.NotifyHermes = func(id string, p gov.PendingApproval) error {
				msgID, err := notifyHermes(id, p, cfg)
				if err != nil {
					writeJSONLine(errOut, map[string]string{
						"warning":      "notify_hermes_failed",
						"escalation_id": id,
						"error":        err.Error(),
					})
					return err
				}
				_ = msgID // already stamped on the row by Wait if we want to extend
				return nil
			}
		}
	}
```

- [ ] **Step 2: Build to verify no compile errors**

Run: `cd go/execution-kernel && go build ./...`
Expected: success

- [ ] **Step 3: Commit**

```bash
git add go/execution-kernel/cmd/chitin-kernel/gate_hook.go
git -c user.name=red -c user.email=jpleva91@gmail.com commit -m "feat(gov): wire EscalateStore + NotifyHermes into evalHookStdin

On every hook-stdin invocation: open pending_approvals.sqlite, run
SweepStale to recover orphaned rows from prior crashes, attach to
gate.EscalateStore. If operator.yaml loads, attach notifyHermes
hook; else escalate rules degrade to deny with a stderr warning."
```

---

### Task 23: End-to-end integration test

**Files:**
- Create: `cmd/chitin-kernel/escalate_e2e_test.go`

- [ ] **Step 1: Write the e2e test**

Create `cmd/chitin-kernel/escalate_e2e_test.go`:

```go
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
		code := evalHookStdin(bytes.NewReader(body), &out, &errOut, "claude-code", "", "", false, false)
		resCh <- result{out.String(), code}
	}()

	// Wait for a pending row to appear.
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

	// Override timeNow to simulate fast time passage.
	prevTimeNow := gov.TimeNowForTest()
	defer gov.RestoreTimeNow(prevTimeNow)

	var out, errOut bytes.Buffer
	// timeout in policy is 30s; we'll let the test poll loop tick a few times,
	// then advance simulated time past the deadline.
	go func() {
		time.Sleep(150 * time.Millisecond)
		gov.SetTimeNowForTest(time.Now().Add(60 * time.Second))
	}()
	code := evalHookStdin(bytes.NewReader(body), &out, &errOut, "claude-code", "", "", false, false)
	if code != 2 {
		t.Errorf("exit=%d want 2 (block on timeout); stdout=%q", code, out.String())
	}
}
```

(The test calls `gov.TimeNowForTest()` / `gov.SetTimeNowForTest()` / `gov.RestoreTimeNow()` — add tiny test-only exported helpers in escalate.go that wrap the unexported `timeNow` var.)

- [ ] **Step 2: Add the test-only time helpers in escalate.go**

Append to `escalate.go`:

```go
// TimeNowForTest / SetTimeNowForTest / RestoreTimeNow expose the
// internal timeNow hook for tests in other packages (cmd/chitin-kernel
// integration tests). Production callers use timeNow directly.
func TimeNowForTest() func() time.Time           { return timeNow }
func SetTimeNowForTest(at time.Time)             { timeNow = func() time.Time { return at } }
func RestoreTimeNow(prev func() time.Time)       { timeNow = prev }
```

- [ ] **Step 3: Run the e2e tests**

Run: `cd go/execution-kernel && go test ./cmd/chitin-kernel/ -run TestE2E_Escalate -v`
Expected: PASS (both subtests)

- [ ] **Step 4: Commit**

```bash
git add go/execution-kernel/cmd/chitin-kernel/escalate_e2e_test.go go/execution-kernel/internal/gov/escalate.go
git -c user.name=red -c user.email=jpleva91@gmail.com commit -m "test: end-to-end integration for escalate effect

evalHookStdin → policy match → escalate branch → Wait → CLI approve
→ resolved Decision → exit 0. Timeout path covered separately."
```

---

### Task 24: Run full suite + push branch

- [ ] **Step 1: Full test run**

Run: `cd go/execution-kernel && go test ./internal/gov/... ./cmd/chitin-kernel/... 2>&1 | tail -10`
Expected: all PASS

- [ ] **Step 2: Vet**

Run: `cd go/execution-kernel && go vet ./...`
Expected: clean

- [ ] **Step 3: Push branch**

Run: `git push -u origin docs/operator-approval-escalation-spec`

- [ ] **Step 4: Open PR**

Run:
```bash
gh pr create --base main --head docs/operator-approval-escalation-spec \
  --title "feat(gov): operator-approval escalation effect" \
  --body "$(cat <<'EOF'
## Summary

Adds \`effect: escalate\` to the chitin policy DSL — pauses the agent's
tool call and asks the operator to approve via hermes-gateway chat or
\`chitin-kernel pending approve\` CLI fallback. Approve grants a configurable
remember-window per (rule, agent).

Implements spec at \`docs/superpowers/specs/2026-05-07-operator-approval-escalation-design.md\`.

## What ships

- New action type / effect: \`EffectEscalate\`
- New SQLite tables: \`pending_approvals\`, \`remember_grants\` (in \`~/.chitin/pending_approvals.sqlite\`)
- New gate branch: between policy eval + counter recording, blocks on \`PendingApprovalStore.Wait\`
- New CLI: \`chitin-kernel pending list|approve|deny|watch-hermes\`
- New systemd unit: \`chitin-pending-watch.{service,timer}\` (30s tick)
- New file: \`~/.chitin/operator.yaml\` (operator-managed; channel + hermes_bin)

## Test plan

- [x] Unit tests: 24 new tests across \`escalate_test.go\`,
      \`policy_escalate_test.go\`, \`pending_test.go\`, \`notify_hermes_test.go\`
- [x] Integration tests: e2e approve + timeout in
      \`escalate_e2e_test.go\`
- [ ] Manual smoke: install systemd unit, configure operator.yaml,
      add an escalate rule to chitin.yaml, fire a test escalation,
      confirm whatsapp/slack notification arrives, reply, confirm
      resolution lands

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```

---

## Self-review

Spec coverage check:

| Spec section | Covered by |
|---|---|
| New EffectEscalate state | Task 1 |
| Decision shape changes | Task 2 |
| Pending_approvals table | Tasks 3-6 |
| Remember_grants table | Task 7 |
| Policy DSL extension | Task 8 |
| Wait() polling helper | Task 9 |
| Gate.Evaluate escalate branch | Task 10 |
| Sweeper for orphaned rows | Task 11 |
| Pending list CLI | Task 12 |
| Pending approve/deny CLI | Task 13 |
| UID auth | Task 14 |
| CLI dispatcher wiring | Task 15 |
| Hermes API investigation | Task 16 |
| notifyHermes outbound | Task 17 |
| parseHermesReply | Task 18 |
| watch-hermes subcommand | Task 19 |
| operator.yaml loader | Task 20 |
| Systemd timer | Task 21 |
| Wire into hook path | Task 22 |
| Integration tests | Task 23 |

All spec requirements covered.

Edge case mapping:

| Spec edge case | Covered by |
|---|---|
| E1 Hermes-gateway down at notify | Task 17 (notifyHermes returns err; row stamps notify_failed_reason) + Task 22 (gate degrades cleanly) |
| E2 Already-resolved | Task 13 (TestPendingApprove_RefusesAlreadyResolved) |
| E3 Kernel dies mid-Wait | Task 11 (SweepStale) + Task 22 (sweep on every hook startup) |
| E4 Concurrent same-(rule,agent) | Task 9 (creates two rows; Wait test covers concurrent insert) |
| E5 Window outlives session | by design — grant is time-keyed (Task 7) |
| E6 Two kernels racing | Task 5 (atomic UPDATE WHERE resolved_ts IS NULL) |
| E7 Escalate on action: unknown | Task 8 (validation rejects) |
| E8 Wrong uid CLI | Task 14 |
| E9 Notify template sandbox | Task 17 (text/template stdlib, fixed function set) |

All edge cases covered.

Type/method consistency: spot-checked. `Resolution.OutcomeRuleID()` referenced consistently across Tasks 9 + 10. `EscalateStore.Wait(WaitArgs)` signature consistent. `PendingApproval` field names consistent across Tasks 3-7 + 12-13.

No placeholders found.

---

## Plan complete and saved to `docs/superpowers/plans/2026-05-07-operator-approval-escalation.md`. Two execution options:

**1. Subagent-Driven (recommended)** — I dispatch a fresh subagent per task, review between tasks, fast iteration

**2. Inline Execution** — Execute tasks in this session using executing-plans, batch execution with checkpoints

Which approach?
