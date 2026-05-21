# Chitin-Owned Kanban DB — Foundation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Stand up `~/.chitin/kanban/<board>/kanban.db` as a chitin-owned mirror of the hermes kanban DBs, with a one-shot migration CLI (`chitin-kernel kanban migrate <board>`) and a board-config field exposing the new path. Hermes' DB remains source of truth — this plan only adds the chitin-owned copy + the tool to refresh it.

**Architecture:** A new `internal/kanban` package in `chitin-kernel` owns the canonical schema (union of the chitin + readybench hermes schemas) and the `Migrate(srcPath, destPath)` function. The migration drops + recreates the destination, applies the canonical schema, then `INSERT … SELECT`s every row from the source, mapping columns that exist and defaulting columns that don't. The CLI subcommand wires it to a board name via `boardconfig`. `boardconfig` learns one new field — `chitin_db_path` — that defaults to `~/.chitin/kanban/<board>/kanban.db`.

**Tech Stack:**
- Go 1.22+ (chitin-kernel)
- `modernc.org/sqlite` (already in go.mod)
- Standard `database/sql`
- Go test for TDD

**Scope (explicitly NOT in this plan):**
- Console-api flip to read the new DB — follow-up plan
- Dual-write or write surface in chitin — follow-up plan
- Retiring hermes' DB as source of truth — follow-up plan

---

## File Structure

**Create:**
- `go/execution-kernel/internal/kanban/schema.go` — canonical DDL (union schema)
- `go/execution-kernel/internal/kanban/schema_test.go` — DDL applies cleanly to empty DB
- `go/execution-kernel/internal/kanban/migrate.go` — `Migrate(srcPath, destPath)` + helpers
- `go/execution-kernel/internal/kanban/migrate_test.go` — TDD for migration
- `go/execution-kernel/cmd/chitin-kernel/kanban_cmd.go` — `kanban migrate` subcommand
- `go/execution-kernel/cmd/chitin-kernel/kanban_cmd_test.go` — CLI integration test

**Modify:**
- `go/execution-kernel/cmd/chitin-kernel/main.go` — register `case "kanban":` dispatch
- `go/execution-kernel/internal/boardconfig/boardconfig.go` — add `chitin_db_path` field with default
- `go/execution-kernel/internal/boardconfig/boardconfig_test.go` — test new field + default

---

## Task 1: Canonical schema DDL

**Files:**
- Create: `go/execution-kernel/internal/kanban/schema.go`
- Test: `go/execution-kernel/internal/kanban/schema_test.go`

The canonical schema is the **union** of the chitin and readybench hermes schemas — same tables, but every column from either board's `tasks` row included so a single schema can hold rows from both. The chitin board has 4 extra `tasks` columns (`block_reason`, `consecutive_failures`, `last_failure_error`, `max_retries`); readybench rows will get NULL/default for those.

Tables covered: `tasks`, `task_links`, `task_comments`, `task_events`, `task_runs`, `kanban_notify_subs`. (We drop `sqlite_sequence` — SQLite manages that itself when AUTOINCREMENT is present.)

- [ ] **Step 1: Write the failing schema test**

```go
// go/execution-kernel/internal/kanban/schema_test.go
package kanban

import (
	"database/sql"
	"path/filepath"
	"sort"
	"testing"

	_ "modernc.org/sqlite"
)

func TestApplySchemaCreatesAllCanonicalTables(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	if err := ApplySchema(db); err != nil {
		t.Fatalf("ApplySchema: %v", err)
	}

	rows, err := db.Query("SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' ORDER BY name")
	if err != nil {
		t.Fatalf("query tables: %v", err)
	}
	defer rows.Close()

	var got []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatalf("scan: %v", err)
		}
		got = append(got, n)
	}

	want := []string{
		"kanban_notify_subs",
		"task_comments",
		"task_events",
		"task_links",
		"task_runs",
		"tasks",
	}
	sort.Strings(got)
	if len(got) != len(want) {
		t.Fatalf("tables: want %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("tables[%d]: want %q, got %q", i, want[i], got[i])
		}
	}
}

func TestApplySchemaIncludesUnionColumnsOnTasks(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	if err := ApplySchema(db); err != nil {
		t.Fatalf("ApplySchema: %v", err)
	}

	rows, err := db.Query("SELECT name FROM pragma_table_info('tasks')")
	if err != nil {
		t.Fatalf("query pragma: %v", err)
	}
	defer rows.Close()

	cols := map[string]bool{}
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatalf("scan: %v", err)
		}
		cols[n] = true
	}

	// Union columns — must include the chitin-board-only ones.
	required := []string{
		"id", "title", "body", "assignee", "status",
		"block_reason", "consecutive_failures", "last_failure_error", "max_retries",
		"skills", "current_run_id", "workflow_template_id", "current_step_key",
	}
	for _, c := range required {
		if !cols[c] {
			t.Errorf("tasks missing union column %q", c)
		}
	}
}

func TestApplySchemaIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	if err := ApplySchema(db); err != nil {
		t.Fatalf("first apply: %v", err)
	}
	if err := ApplySchema(db); err != nil {
		t.Fatalf("second apply: %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd ~/workspace/chitin/go/execution-kernel && go test ./internal/kanban/ -run TestApplySchema -v`
Expected: FAIL — "no Go files in internal/kanban" or "undefined: ApplySchema"

- [ ] **Step 3: Write the schema implementation**

```go
// go/execution-kernel/internal/kanban/schema.go
package kanban

import "database/sql"

// CanonicalSchema is the union of the chitin and readybench hermes
// kanban schemas. Applied to a chitin-owned DB it must accept rows
// migrated from either board verbatim. Indexes match hermes so query
// patterns carry over unchanged.
const CanonicalSchema = `
CREATE TABLE IF NOT EXISTS tasks (
    id                   TEXT PRIMARY KEY,
    title                TEXT NOT NULL,
    body                 TEXT,
    assignee             TEXT,
    status               TEXT NOT NULL,
    priority             INTEGER DEFAULT 0,
    created_by           TEXT,
    created_at           INTEGER NOT NULL,
    started_at           INTEGER,
    completed_at         INTEGER,
    workspace_kind       TEXT NOT NULL DEFAULT 'scratch',
    workspace_path       TEXT,
    claim_lock           TEXT,
    claim_expires        INTEGER,
    tenant               TEXT,
    result               TEXT,
    idempotency_key      TEXT,
    spawn_failures       INTEGER NOT NULL DEFAULT 0,
    worker_pid           INTEGER,
    last_spawn_error     TEXT,
    max_runtime_seconds  INTEGER,
    last_heartbeat_at    INTEGER,
    current_run_id       INTEGER,
    workflow_template_id TEXT,
    current_step_key     TEXT,
    skills               TEXT,
    consecutive_failures INTEGER NOT NULL DEFAULT 0,
    last_failure_error   TEXT,
    max_retries          INTEGER,
    block_reason         TEXT DEFAULT NULL
);

CREATE TABLE IF NOT EXISTS task_links (
    parent_id  TEXT NOT NULL,
    child_id   TEXT NOT NULL,
    PRIMARY KEY (parent_id, child_id)
);

CREATE TABLE IF NOT EXISTS task_comments (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    task_id    TEXT NOT NULL,
    author     TEXT NOT NULL,
    body       TEXT NOT NULL,
    created_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS task_events (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    task_id    TEXT NOT NULL,
    run_id     INTEGER,
    kind       TEXT NOT NULL,
    payload    TEXT,
    created_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS task_runs (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    task_id             TEXT NOT NULL,
    profile             TEXT,
    step_key            TEXT,
    status              TEXT NOT NULL,
    claim_lock          TEXT,
    claim_expires       INTEGER,
    worker_pid          INTEGER,
    max_runtime_seconds INTEGER,
    last_heartbeat_at   INTEGER,
    started_at          INTEGER NOT NULL,
    ended_at            INTEGER,
    outcome             TEXT,
    summary             TEXT,
    metadata            TEXT,
    error               TEXT,
    driver_id           TEXT,
    repo_sha            TEXT,
    lease_id            TEXT,
    event_chain_hash    TEXT,
    idempotency_key     TEXT,
    model               TEXT NOT NULL DEFAULT '',
    soul_id             TEXT NOT NULL DEFAULT '',
    soul_hash           TEXT NOT NULL DEFAULT '',
    agent_fingerprint   TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS kanban_notify_subs (
    task_id          TEXT NOT NULL,
    platform         TEXT NOT NULL,
    chat_id          TEXT NOT NULL,
    thread_id        TEXT NOT NULL DEFAULT '',
    user_id          TEXT,
    notifier_profile TEXT,
    created_at       INTEGER NOT NULL,
    last_event_id    INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (task_id, platform, chat_id, thread_id)
);

CREATE INDEX IF NOT EXISTS idx_tasks_assignee_status ON tasks(assignee, status);
CREATE INDEX IF NOT EXISTS idx_tasks_status          ON tasks(status);
CREATE INDEX IF NOT EXISTS idx_tasks_tenant          ON tasks(tenant);
CREATE INDEX IF NOT EXISTS idx_tasks_idempotency     ON tasks(idempotency_key);
CREATE INDEX IF NOT EXISTS idx_links_child           ON task_links(child_id);
CREATE INDEX IF NOT EXISTS idx_links_parent          ON task_links(parent_id);
CREATE INDEX IF NOT EXISTS idx_comments_task         ON task_comments(task_id, created_at);
CREATE INDEX IF NOT EXISTS idx_events_task           ON task_events(task_id, created_at);
CREATE INDEX IF NOT EXISTS idx_events_run            ON task_events(run_id, id);
CREATE INDEX IF NOT EXISTS idx_runs_task             ON task_runs(task_id, started_at);
CREATE INDEX IF NOT EXISTS idx_runs_status           ON task_runs(status);
CREATE INDEX IF NOT EXISTS idx_notify_task           ON kanban_notify_subs(task_id);
`

// ApplySchema creates the canonical kanban tables and indexes if they
// don't already exist. Idempotent.
func ApplySchema(db *sql.DB) error {
	_, err := db.Exec(CanonicalSchema)
	return err
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd ~/workspace/chitin/go/execution-kernel && go test ./internal/kanban/ -run TestApplySchema -v`
Expected: PASS on all three (`TestApplySchemaCreatesAllCanonicalTables`, `TestApplySchemaIncludesUnionColumnsOnTasks`, `TestApplySchemaIsIdempotent`)

- [ ] **Step 5: Commit**

```bash
cd ~/workspace/chitin && rtk git add go/execution-kernel/internal/kanban/schema.go go/execution-kernel/internal/kanban/schema_test.go && rtk git commit -m "feat(kanban): add canonical chitin-owned kanban schema"
```

---

## Task 2: Migration function — happy path

**Files:**
- Create: `go/execution-kernel/internal/kanban/migrate.go`
- Test: `go/execution-kernel/internal/kanban/migrate_test.go`

The migration function takes `srcPath` (hermes DB) and `destPath` (chitin DB), creates the dest with `ApplySchema`, then `INSERT … SELECT`s every row from each table. To handle the column-shape difference between chitin and readybench source schemas, it introspects `pragma_table_info(<table>)` on the source and copies only those columns that the canonical schema also has — the rest get their default value.

- [ ] **Step 1: Write the failing happy-path test**

```go
// go/execution-kernel/internal/kanban/migrate_test.go
package kanban

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

// seedHermesChitinShape creates a DB matching the chitin hermes board
// schema (the "wider" schema with block_reason etc.) and inserts a few
// representative rows across the major tables.
func seedHermesChitinShape(t *testing.T, dbPath string) {
	t.Helper()
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	// We can reuse the canonical DDL — it's a superset of the chitin
	// hermes schema, so any chitin-board row inserts cleanly here.
	if _, err := db.Exec(CanonicalSchema); err != nil {
		t.Fatalf("seed schema: %v", err)
	}
	_, err = db.Exec(`
		INSERT INTO tasks (id, title, status, created_at, block_reason)
		VALUES
			('t_aaa', 'first', 'ready', 1000, NULL),
			('t_bbb', 'second', 'blocked', 1001, 'no_pr');
		INSERT INTO task_events (task_id, kind, created_at)
		VALUES ('t_aaa', 'created', 1000), ('t_aaa', 'promoted', 1010);
		INSERT INTO task_comments (task_id, author, body, created_at)
		VALUES ('t_aaa', 'red', 'a comment', 1005);
		INSERT INTO task_links (parent_id, child_id)
		VALUES ('t_aaa', 't_bbb');
		INSERT INTO task_runs (task_id, status, started_at)
		VALUES ('t_aaa', 'running', 1020);
	`)
	if err != nil {
		t.Fatalf("seed rows: %v", err)
	}
}

func TestMigrateCopiesAllRowsFromChitinShape(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.db")
	dest := filepath.Join(dir, "dest.db")
	seedHermesChitinShape(t, src)

	if err := Migrate(src, dest); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	db, err := sql.Open("sqlite", dest)
	if err != nil {
		t.Fatalf("open dest: %v", err)
	}
	defer db.Close()

	for _, tc := range []struct {
		table string
		want  int
	}{
		{"tasks", 2},
		{"task_events", 2},
		{"task_comments", 1},
		{"task_links", 1},
		{"task_runs", 1},
	} {
		var got int
		if err := db.QueryRow("SELECT count(*) FROM " + tc.table).Scan(&got); err != nil {
			t.Fatalf("count %s: %v", tc.table, err)
		}
		if got != tc.want {
			t.Errorf("%s rows: want %d, got %d", tc.table, tc.want, got)
		}
	}

	// Spot-check a value made the trip.
	var blockReason sql.NullString
	if err := db.QueryRow("SELECT block_reason FROM tasks WHERE id='t_bbb'").Scan(&blockReason); err != nil {
		t.Fatalf("select block_reason: %v", err)
	}
	if !blockReason.Valid || blockReason.String != "no_pr" {
		t.Errorf("block_reason: want 'no_pr', got %v", blockReason)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd ~/workspace/chitin/go/execution-kernel && go test ./internal/kanban/ -run TestMigrateCopiesAllRows -v`
Expected: FAIL — "undefined: Migrate"

- [ ] **Step 3: Write the migration implementation**

```go
// go/execution-kernel/internal/kanban/migrate.go
package kanban

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

// migrationTables is the ordered list of tables we copy. Order is
// preserved so foreign-ish references (task_events.task_id ->
// tasks.id) write the parent first, though SQLite doesn't enforce FK
// constraints unless explicitly enabled.
var migrationTables = []string{
	"tasks",
	"task_links",
	"task_comments",
	"task_events",
	"task_runs",
	"kanban_notify_subs",
}

// Migrate creates destPath (overwriting any existing DB at that path),
// applies the canonical schema, then copies every row from srcPath
// table-by-table. Only columns that exist in both source and
// canonical schemas are copied; canonical-only columns get their
// default value on the destination.
//
// Idempotent in the sense that calling it twice with the same source
// produces a destination DB that matches the source — but it does
// drop and recreate destPath each call, so it's a full refresh, not
// an incremental sync.
func Migrate(srcPath, destPath string) error {
	src, err := sql.Open("sqlite", "file:"+srcPath+"?mode=ro")
	if err != nil {
		return fmt.Errorf("open src: %w", err)
	}
	defer src.Close()

	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return fmt.Errorf("mkdir dest: %w", err)
	}
	// Full refresh — remove any existing dest so the schema below is
	// the authoritative shape.
	if err := os.Remove(destPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove dest: %w", err)
	}

	dest, err := sql.Open("sqlite", destPath)
	if err != nil {
		return fmt.Errorf("open dest: %w", err)
	}
	defer dest.Close()

	if err := ApplySchema(dest); err != nil {
		return fmt.Errorf("apply schema: %w", err)
	}

	for _, table := range migrationTables {
		if err := copyTable(src, dest, table); err != nil {
			return fmt.Errorf("copy %s: %w", table, err)
		}
	}
	return nil
}

// copyTable reads every row from src.<table> and inserts it into
// dest.<table>, mapping by column name. Columns present in source but
// not in dest are skipped; columns in dest but not in source get
// their schema default.
func copyTable(src, dest *sql.DB, table string) error {
	srcCols, err := tableColumns(src, table)
	if err != nil {
		return fmt.Errorf("src columns: %w", err)
	}
	if len(srcCols) == 0 {
		// Source doesn't have this table — nothing to copy.
		return nil
	}
	destCols, err := tableColumns(dest, table)
	if err != nil {
		return fmt.Errorf("dest columns: %w", err)
	}
	destSet := map[string]bool{}
	for _, c := range destCols {
		destSet[c] = true
	}

	var cols []string
	for _, c := range srcCols {
		if destSet[c] {
			cols = append(cols, c)
		}
	}
	if len(cols) == 0 {
		return nil
	}

	colList := strings.Join(cols, ", ")
	rows, err := src.Query("SELECT " + colList + " FROM " + table)
	if err != nil {
		return fmt.Errorf("select: %w", err)
	}
	defer rows.Close()

	placeholders := strings.TrimSuffix(strings.Repeat("?, ", len(cols)), ", ")
	insertSQL := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)", table, colList, placeholders)

	tx, err := dest.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	stmt, err := tx.Prepare(insertSQL)
	if err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("prepare: %w", err)
	}
	defer stmt.Close()

	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("scan: %w", err)
		}
		if _, err := stmt.Exec(vals...); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("insert: %w", err)
		}
	}
	if err := rows.Err(); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("rows: %w", err)
	}
	return tx.Commit()
}

// tableColumns returns the ordered column names of a table, or an
// empty slice if the table doesn't exist.
func tableColumns(db *sql.DB, table string) ([]string, error) {
	rows, err := db.Query("SELECT name FROM pragma_table_info(?)", table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd ~/workspace/chitin/go/execution-kernel && go test ./internal/kanban/ -run TestMigrateCopiesAllRowsFromChitinShape -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
cd ~/workspace/chitin && rtk git add go/execution-kernel/internal/kanban/migrate.go go/execution-kernel/internal/kanban/migrate_test.go && rtk git commit -m "feat(kanban): add Migrate function (one-shot DB copy)"
```

---

## Task 3: Migration handles the readybench (narrower) source schema

**Files:**
- Modify: `go/execution-kernel/internal/kanban/migrate_test.go` (append test)

The readybench hermes board has a narrower `tasks` schema — it's missing `block_reason`, `consecutive_failures`, `last_failure_error`, `max_retries`. The migration must accept those rows and leave the canonical-only columns as their default (NULL or `0`).

- [ ] **Step 1: Write the failing test**

Append to `go/execution-kernel/internal/kanban/migrate_test.go`:

```go
// seedHermesReadybenchShape creates a DB matching the readybench
// hermes board schema (narrower — no block_reason etc.) and inserts
// representative rows.
func seedHermesReadybenchShape(t *testing.T, dbPath string) {
	t.Helper()
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	_, err = db.Exec(`
		CREATE TABLE tasks (
			id                   TEXT PRIMARY KEY,
			title                TEXT NOT NULL,
			body                 TEXT,
			assignee             TEXT,
			status               TEXT NOT NULL,
			priority             INTEGER DEFAULT 0,
			created_by           TEXT,
			created_at           INTEGER NOT NULL,
			started_at           INTEGER,
			completed_at         INTEGER,
			workspace_kind       TEXT NOT NULL DEFAULT 'scratch',
			workspace_path       TEXT,
			claim_lock           TEXT,
			claim_expires        INTEGER,
			tenant               TEXT,
			result               TEXT,
			idempotency_key      TEXT,
			spawn_failures       INTEGER NOT NULL DEFAULT 0,
			worker_pid           INTEGER,
			last_spawn_error     TEXT,
			max_runtime_seconds  INTEGER,
			last_heartbeat_at    INTEGER,
			current_run_id       INTEGER,
			workflow_template_id TEXT,
			current_step_key     TEXT,
			skills               TEXT,
			consecutive_failures INTEGER NOT NULL DEFAULT 0,
			last_failure_error   TEXT,
			max_retries          INTEGER
		);
		INSERT INTO tasks (id, title, status, created_at)
		VALUES ('t_rb1', 'rb only', 'ready', 2000);
	`)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
}

func TestMigrateAcceptsReadybenchShapeWithoutBlockReason(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "rb.db")
	dest := filepath.Join(dir, "rb-chitin.db")
	seedHermesReadybenchShape(t, src)

	if err := Migrate(src, dest); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	db, err := sql.Open("sqlite", dest)
	if err != nil {
		t.Fatalf("open dest: %v", err)
	}
	defer db.Close()

	var count int
	if err := db.QueryRow("SELECT count(*) FROM tasks").Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Errorf("tasks: want 1, got %d", count)
	}

	// Canonical-only column should be NULL after migrating a row that
	// didn't have it in source.
	var blockReason sql.NullString
	if err := db.QueryRow("SELECT block_reason FROM tasks WHERE id='t_rb1'").Scan(&blockReason); err != nil {
		t.Fatalf("block_reason: %v", err)
	}
	if blockReason.Valid {
		t.Errorf("block_reason: want NULL, got %q", blockReason.String)
	}
}
```

- [ ] **Step 2: Run test to verify behavior**

Run: `cd ~/workspace/chitin/go/execution-kernel && go test ./internal/kanban/ -run TestMigrateAcceptsReadybenchShape -v`
Expected: PASS (the Task 2 implementation already handles this — column intersection is exactly what makes the narrower source work)

If FAIL, the column-intersection logic in `copyTable` is buggy — fix before continuing.

- [ ] **Step 3: Commit**

```bash
cd ~/workspace/chitin && rtk git add go/execution-kernel/internal/kanban/migrate_test.go && rtk git commit -m "test(kanban): cover readybench (narrower) source schema"
```

---

## Task 4: Row-count verification helper

**Files:**
- Modify: `go/execution-kernel/internal/kanban/migrate.go` (append `RowCounts` + `VerifyCounts`)
- Modify: `go/execution-kernel/internal/kanban/migrate_test.go` (append verification test)

The CLI surface (Task 6) needs to report "migrated N tasks, M events, …" and exit non-zero if counts disagree. A helper that returns a `map[string]int` of row counts per table is the right primitive.

- [ ] **Step 1: Write the failing test**

Append to `migrate_test.go`:

```go
func TestRowCountsReturnsPerTableCounts(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.db")
	dest := filepath.Join(dir, "dest.db")
	seedHermesChitinShape(t, src)
	if err := Migrate(src, dest); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	srcDB, _ := sql.Open("sqlite", src)
	defer srcDB.Close()
	destDB, _ := sql.Open("sqlite", dest)
	defer destDB.Close()

	srcCounts, err := RowCounts(srcDB)
	if err != nil {
		t.Fatalf("src counts: %v", err)
	}
	destCounts, err := RowCounts(destDB)
	if err != nil {
		t.Fatalf("dest counts: %v", err)
	}

	for _, table := range migrationTables {
		if srcCounts[table] != destCounts[table] {
			t.Errorf("%s: src=%d dest=%d", table, srcCounts[table], destCounts[table])
		}
	}
}

func TestVerifyCountsReturnsMismatchAsError(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.db")
	b := filepath.Join(dir, "b.db")
	seedHermesChitinShape(t, a)
	// Re-seed b with one fewer row in tasks.
	seedHermesChitinShape(t, b)
	bDB, _ := sql.Open("sqlite", b)
	if _, err := bDB.Exec("DELETE FROM tasks WHERE id='t_bbb'"); err != nil {
		t.Fatalf("tamper: %v", err)
	}
	bDB.Close()

	aDB, _ := sql.Open("sqlite", a)
	defer aDB.Close()
	bDB2, _ := sql.Open("sqlite", b)
	defer bDB2.Close()

	if err := VerifyCounts(aDB, bDB2); err == nil {
		t.Fatal("VerifyCounts: want mismatch error, got nil")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd ~/workspace/chitin/go/execution-kernel && go test ./internal/kanban/ -run "TestRowCounts|TestVerifyCounts" -v`
Expected: FAIL — "undefined: RowCounts" / "undefined: VerifyCounts"

- [ ] **Step 3: Add `RowCounts` and `VerifyCounts` to migrate.go**

Append to `go/execution-kernel/internal/kanban/migrate.go`:

```go
// RowCounts returns a map of table -> row count for every canonical
// table. Missing tables map to 0.
func RowCounts(db *sql.DB) (map[string]int, error) {
	out := map[string]int{}
	for _, table := range migrationTables {
		// Skip tables that don't exist on this side.
		cols, err := tableColumns(db, table)
		if err != nil {
			return nil, err
		}
		if len(cols) == 0 {
			out[table] = 0
			continue
		}
		var n int
		if err := db.QueryRow("SELECT count(*) FROM " + table).Scan(&n); err != nil {
			return nil, fmt.Errorf("count %s: %w", table, err)
		}
		out[table] = n
	}
	return out, nil
}

// VerifyCounts compares row counts table-by-table between two DBs and
// returns an error listing every mismatch. nil means the two DBs
// agree on every canonical table.
func VerifyCounts(src, dest *sql.DB) error {
	srcCounts, err := RowCounts(src)
	if err != nil {
		return fmt.Errorf("src: %w", err)
	}
	destCounts, err := RowCounts(dest)
	if err != nil {
		return fmt.Errorf("dest: %w", err)
	}
	var mismatches []string
	for _, table := range migrationTables {
		if srcCounts[table] != destCounts[table] {
			mismatches = append(mismatches,
				fmt.Sprintf("%s: src=%d dest=%d", table, srcCounts[table], destCounts[table]))
		}
	}
	if len(mismatches) > 0 {
		return fmt.Errorf("row-count mismatch: %s", strings.Join(mismatches, "; "))
	}
	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd ~/workspace/chitin/go/execution-kernel && go test ./internal/kanban/ -v`
Expected: PASS on every test in the package

- [ ] **Step 5: Commit**

```bash
cd ~/workspace/chitin && rtk git add go/execution-kernel/internal/kanban/migrate.go go/execution-kernel/internal/kanban/migrate_test.go && rtk git commit -m "feat(kanban): add RowCounts + VerifyCounts helpers"
```

---

## Task 5: `chitin_db_path` field in boardconfig

**Files:**
- Modify: `go/execution-kernel/internal/boardconfig/boardconfig.go`
- Modify: `go/execution-kernel/internal/boardconfig/boardconfig_test.go`

Expose the new path through the existing `chitin-kernel board-config <board> <field>` interface so callers (CLI, console-api, agents) can ask "where's the chitin-owned DB for this board?" without hardcoding `~/.chitin/kanban/<board>/kanban.db`.

The field defaults to `~/.chitin/kanban/<board>/kanban.db` (resolved at read time so it follows `$HOME`).

- [ ] **Step 1: Write the failing test**

Append to `go/execution-kernel/internal/boardconfig/boardconfig_test.go`:

```go
func TestChitinDBPathDefaultsToCanonicalLocation(t *testing.T) {
	home := setupHomeWithBoard(t, "chitin", map[string]string{
		"repo":           "chitinhq/chitin",
		"default_branch": "main",
		"workspace_root": "/tmp/chitin",
		"kernel_bin":     "chitin-kernel",
	})
	t.Setenv("HOME", home)

	got, err := Read("chitin", "chitin_db_path")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	want := filepath.Join(home, ".chitin", "kanban", "chitin", "kanban.db")
	if got != want {
		t.Errorf("chitin_db_path: want %q, got %q", want, got)
	}
}

func TestChitinDBPathExplicitOverride(t *testing.T) {
	home := setupHomeWithBoard(t, "chitin", map[string]string{
		"repo":             "chitinhq/chitin",
		"default_branch":   "main",
		"workspace_root":   "/tmp/chitin",
		"kernel_bin":       "chitin-kernel",
		"chitin_db_path":   "/var/run/chitin/kanban-chitin.db",
	})
	t.Setenv("HOME", home)

	got, err := Read("chitin", "chitin_db_path")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got != "/var/run/chitin/kanban-chitin.db" {
		t.Errorf("override: got %q", got)
	}
}
```

If `setupHomeWithBoard` doesn't already exist, check the existing test file for the helper that writes a `config.json` under `$HOME/.hermes/kanban/boards/<board>/` and adapt the test to use whatever shape it already has. The point is: write the field, default to the canonical path, allow override via the `config.json` value.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd ~/workspace/chitin/go/execution-kernel && go test ./internal/boardconfig/ -run TestChitinDBPath -v`
Expected: FAIL — "unknown field: chitin_db_path" from `UnknownFieldError`

- [ ] **Step 3: Add the field to `fieldSpecs` and wire dynamic default**

In `go/execution-kernel/internal/boardconfig/boardconfig.go`, add to the `fieldSpecs` map (alongside `chitin_yaml`):

```go
"chitin_db_path": {
    EnvVar: "KANBAN_BOARD_CHITIN_DB_PATH",
    // Default computed at read time — depends on $HOME and board slug.
    DefaultValue: "",
},
```

Then in the `Read` function (or wherever defaults are resolved), add a fall-back specifically for `chitin_db_path` when the resolved value is empty:

```go
if field == "chitin_db_path" && value == "" {
    home, err := os.UserHomeDir()
    if err != nil {
        return "", fmt.Errorf("resolve home: %w", err)
    }
    value = filepath.Join(home, ".chitin", "kanban", slug, "kanban.db")
}
```

(Patch lines pending the actual `Read` shape — match the existing pattern for `chitin_yaml`'s default.)

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd ~/workspace/chitin/go/execution-kernel && go test ./internal/boardconfig/ -v`
Expected: PASS on every test

- [ ] **Step 5: Commit**

```bash
cd ~/workspace/chitin && rtk git add go/execution-kernel/internal/boardconfig/boardconfig.go go/execution-kernel/internal/boardconfig/boardconfig_test.go && rtk git commit -m "feat(boardconfig): expose chitin_db_path field with canonical default"
```

---

## Task 6: `chitin-kernel kanban migrate <board>` CLI

**Files:**
- Create: `go/execution-kernel/cmd/chitin-kernel/kanban_cmd.go`
- Create: `go/execution-kernel/cmd/chitin-kernel/kanban_cmd_test.go`
- Modify: `go/execution-kernel/cmd/chitin-kernel/main.go` — register `case "kanban":`

The CLI shape:

```
chitin-kernel kanban migrate <board>
```

Source path: hermes DB at `$HOME/.hermes/kanban/boards/<board>/kanban.db`
(hardcoded location — that's where hermes always writes; not a board-config field today)

Dest path: `boardconfig.Read(<board>, "chitin_db_path")` (Task 5's new field)

On success: prints one line per migrated table (`tasks: 372`, `task_events: 9275`, ...) plus a verification line. Exit 0.
On row-count mismatch: prints the mismatch summary, exit 1.
On unknown board: exit 3 (matches `board-config` precedent).
On missing source DB: exit 2 with a clear error.

- [ ] **Step 1: Write the failing CLI integration test**

```go
// go/execution-kernel/cmd/chitin-kernel/kanban_cmd_test.go
package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestCLI_KanbanMigrateCopiesRowsToChitinDB(t *testing.T) {
	home := t.TempDir()
	// Seed a board config so board-config resolves the board.
	writeBoardConfig(t, home, "chitin", map[string]string{
		"repo":           "chitinhq/chitin",
		"default_branch": "main",
		"workspace_root": "/tmp/chitin",
		"kernel_bin":     "chitin-kernel",
	})
	// Seed a hermes-side source DB at the canonical hermes location.
	srcPath := filepath.Join(home, ".hermes", "kanban", "boards", "chitin", "kanban.db")
	seedSourceDBAt(t, srcPath)

	stdout, stderr, code := runCLIWithEnv(t, t.TempDir(), []string{"HOME=" + home},
		"kanban", "migrate", "chitin")
	if code != 0 {
		t.Fatalf("exit %d, stderr=%s", code, stderr)
	}
	if !strings.Contains(stdout, "tasks:") {
		t.Errorf("stdout missing per-table counts: %s", stdout)
	}

	destPath := filepath.Join(home, ".chitin", "kanban", "chitin", "kanban.db")
	if !fileExists(destPath) {
		t.Errorf("dest DB not created: %s", destPath)
	}
}

func TestCLI_KanbanMigrateUnknownBoardExit3(t *testing.T) {
	home := t.TempDir()
	_, stderr, code := runCLIWithEnv(t, t.TempDir(), []string{"HOME=" + home},
		"kanban", "migrate", "nope")
	if code != 3 {
		t.Fatalf("want exit 3, got %d, stderr=%s", code, stderr)
	}
}

func TestCLI_KanbanMigrateMissingSourceDBExit2(t *testing.T) {
	home := t.TempDir()
	writeBoardConfig(t, home, "chitin", map[string]string{
		"repo":           "chitinhq/chitin",
		"default_branch": "main",
		"workspace_root": "/tmp/chitin",
		"kernel_bin":     "chitin-kernel",
	})
	// No source DB written.
	_, stderr, code := runCLIWithEnv(t, t.TempDir(), []string{"HOME=" + home},
		"kanban", "migrate", "chitin")
	if code != 2 {
		t.Fatalf("want exit 2, got %d, stderr=%s", code, stderr)
	}
	if !strings.Contains(stderr, "source") && !strings.Contains(stderr, "kanban.db") {
		t.Errorf("stderr missing source-db reference: %s", stderr)
	}
}
```

`writeBoardConfig`, `seedSourceDBAt`, and `fileExists` are small test helpers. Add them in `kanban_cmd_test.go`:

```go
import (
	"database/sql"
	"encoding/json"
	"os"

	_ "modernc.org/sqlite"
)

func writeBoardConfig(t *testing.T, home, board string, fields map[string]string) {
	t.Helper()
	dir := filepath.Join(home, ".hermes", "kanban", "boards", board)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	buf, err := json.Marshal(fields)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), buf, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func seedSourceDBAt(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	// Use the canonical schema as the source shape — the migration
	// handles narrower sources via column intersection, but for a
	// CLI smoke we just want some rows to copy.
	if _, err := db.Exec(`
		CREATE TABLE tasks (id TEXT PRIMARY KEY, title TEXT NOT NULL, status TEXT NOT NULL, created_at INTEGER NOT NULL);
		INSERT INTO tasks (id, title, status, created_at) VALUES ('t_x', 'x', 'ready', 100);
	`); err != nil {
		t.Fatalf("seed: %v", err)
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
```

If `runCLIWithEnv` doesn't already exist in this package, check the other `*_test.go` files in `cmd/chitin-kernel/` — it should be there (used by `board_config_test.go`). Reuse it directly.

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd ~/workspace/chitin/go/execution-kernel && go test ./cmd/chitin-kernel/ -run TestCLI_KanbanMigrate -v`
Expected: FAIL on all three — `kanban` subcommand doesn't exist yet

- [ ] **Step 3: Write the subcommand implementation**

```go
// go/execution-kernel/cmd/chitin-kernel/kanban_cmd.go
package main

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	_ "modernc.org/sqlite"

	"github.com/chitinhq/chitin/go/execution-kernel/internal/boardconfig"
	"github.com/chitinhq/chitin/go/execution-kernel/internal/kanban"
)

// runKanbanCmd handles `chitin-kernel kanban <subcommand>`.
func runKanbanCmd(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: chitin-kernel kanban <migrate> <board>")
		return 2
	}
	switch args[0] {
	case "migrate":
		return runKanbanMigrate(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown kanban subcommand: %s\n", args[0])
		return 2
	}
}

func runKanbanMigrate(args []string) int {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "usage: chitin-kernel kanban migrate <board>")
		return 2
	}
	board := args[0]

	// Resolve dest path through boardconfig — exit 3 on unknown
	// board to match the board-config precedent.
	destPath, err := boardconfig.Read(board, "chitin_db_path")
	if err != nil {
		var unk boardconfig.UnknownBoardError
		if errorsAs(err, &unk) {
			fmt.Fprintln(os.Stderr, err.Error())
			return 3
		}
		fmt.Fprintln(os.Stderr, err.Error())
		return 2
	}

	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "resolve home: %v\n", err)
		return 2
	}
	srcPath := filepath.Join(home, ".hermes", "kanban", "boards", board, "kanban.db")
	if _, err := os.Stat(srcPath); err != nil {
		fmt.Fprintf(os.Stderr, "source kanban.db missing: %s\n", srcPath)
		return 2
	}

	if err := kanban.Migrate(srcPath, destPath); err != nil {
		fmt.Fprintf(os.Stderr, "migrate: %v\n", err)
		return 1
	}

	// Verify and print per-table counts.
	src, err := sql.Open("sqlite", "file:"+srcPath+"?mode=ro")
	if err != nil {
		fmt.Fprintf(os.Stderr, "open src for verify: %v\n", err)
		return 1
	}
	defer src.Close()
	dest, err := sql.Open("sqlite", destPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "open dest for verify: %v\n", err)
		return 1
	}
	defer dest.Close()

	if err := kanban.VerifyCounts(src, dest); err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		return 1
	}

	counts, _ := kanban.RowCounts(dest)
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Printf("%s: %d\n", k, counts[k])
	}
	fmt.Printf("ok: %s\n", destPath)
	return 0
}

// errorsAs is a small wrapper so we can keep stdlib errors.As inline
// without importing it at the top of the file twice across the cmd
// package. Inlined here to avoid touching unrelated imports.
func errorsAs(err error, target any) bool {
	return errors.As(err, target)
}
```

Note: `errors.As` needs an import. Replace the bottom helper with a direct import in the file header, dropping the `errorsAs` wrapper:

```go
import "errors"
// ...
if errors.As(err, &unk) {
```

- [ ] **Step 4: Register the subcommand in main.go**

Find the existing `switch` over subcommands (around line 31-79 per recon) and add:

```go
case "kanban":
    os.Exit(runKanbanCmd(args))
```

Match the existing pattern (`os.Exit(...)` vs return code) — read the surrounding cases to copy the style verbatim.

- [ ] **Step 5: Run tests to verify they pass**

Run: `cd ~/workspace/chitin/go/execution-kernel && go test ./cmd/chitin-kernel/ -run TestCLI_KanbanMigrate -v`
Expected: PASS on all three subtests

If FAIL on unknown board: the boardconfig error type may not be `UnknownBoardError` exactly — adjust the `errors.As` target to match the actual type.

- [ ] **Step 6: Run the full kernel test suite to catch regressions**

Run: `cd ~/workspace/chitin/go/execution-kernel && go test ./...`
Expected: PASS — no regressions in unrelated packages

- [ ] **Step 7: Commit**

```bash
cd ~/workspace/chitin && rtk git add go/execution-kernel/cmd/chitin-kernel/kanban_cmd.go go/execution-kernel/cmd/chitin-kernel/kanban_cmd_test.go go/execution-kernel/cmd/chitin-kernel/main.go && rtk git commit -m "feat(kernel): add chitin-kernel kanban migrate <board> CLI"
```

---

## Task 7: Live-DB smoke verification

**Files:** None — this is a hand-run verification step using the binary built from Tasks 1-6.

The unit tests prove the migration works on synthetic data. The smoke proves it works on the live hermes DBs (chitin: 372 tasks / 9275 events; readybench: 8 tasks / 232 events as of this plan's authoring).

- [ ] **Step 1: Build the kernel**

```bash
cd ~/workspace/chitin/go/execution-kernel && go build -o /tmp/chitin-kernel-new ./cmd/chitin-kernel
```

Expected: build succeeds, no diagnostics.

- [ ] **Step 2: Snapshot live hermes row counts**

```bash
DB=~/.hermes/kanban/boards/chitin/kanban.db
echo "=== chitin source ===" && for t in tasks task_events task_comments task_links task_runs kanban_notify_subs; do echo "$t: $(sqlite3 "$DB" "SELECT count(*) FROM $t" 2>/dev/null)"; done
DB=~/.hermes/kanban/boards/readybench/kanban.db
echo "=== readybench source ===" && for t in tasks task_events task_comments task_links task_runs kanban_notify_subs; do echo "$t: $(sqlite3 "$DB" "SELECT count(*) FROM $t" 2>/dev/null)"; done
```

Expected: per-table counts. Record them.

- [ ] **Step 3: Run the migration for both boards**

```bash
/tmp/chitin-kernel-new kanban migrate chitin
/tmp/chitin-kernel-new kanban migrate readybench
```

Expected output for each: per-table counts followed by `ok: /home/red/.chitin/kanban/<board>/kanban.db`. Exit 0.

- [ ] **Step 4: Cross-check counts**

```bash
DEST=~/.chitin/kanban/chitin/kanban.db
echo "=== chitin dest ===" && for t in tasks task_events task_comments task_links task_runs kanban_notify_subs; do echo "$t: $(sqlite3 "$DEST" "SELECT count(*) FROM $t")"; done
DEST=~/.chitin/kanban/readybench/kanban.db
echo "=== readybench dest ===" && for t in tasks task_events task_comments task_links task_runs kanban_notify_subs; do echo "$t: $(sqlite3 "$DEST" "SELECT count(*) FROM $t")"; done
```

Expected: every count matches the Step 2 snapshot. If anything differs, stop and diagnose before continuing.

- [ ] **Step 5: Spot-check a known row makes the trip intact**

```bash
DEST=~/.chitin/kanban/chitin/kanban.db
sqlite3 "$DEST" "SELECT id, status, assignee, block_reason FROM tasks WHERE id='t_8f4d2ee5'"
```

Expected: `t_8f4d2ee5|blocked|red|` (the tracking epic mentioned in this session) — exact value verbatim from the source.

- [ ] **Step 6: Console-api env-override smoke (read-only)**

The chitin-console-api already supports `HERMES_KANBAN_ROOT` env override (see `apps/chitin-console/README.md` data sources table). Verify the new DB is shape-compatible by pointing the API at the chitin-owned root:

```bash
HERMES_KANBAN_ROOT=$HOME/.chitin/kanban node apps/chitin-console-api/src/server.mjs &
API_PID=$!
sleep 1
curl -s http://127.0.0.1:7878/api/tickets?board=chitin | head -c 400
kill $API_PID
```

Wait — the existing console-api expects the hermes layout `$HERMES_KANBAN_ROOT/boards/<board>/kanban.db`, but the chitin-owned layout is `$CHITIN_ROOT/<board>/kanban.db` (no `boards/` segment). The smoke as written will fail to find the DB. That's expected and proves we need Plan 2 (console-api cutover) — record the failure as confirming the shape gap, don't try to "fix" it in this plan.

Expected outcome of Step 6: the API fails to find the DB (because of the `boards/` path mismatch) OR succeeds if you symlink `~/.chitin/kanban/boards/<board>` → `~/.chitin/kanban/<board>` for the smoke. Either way, **do not commit a symlink hack** — Plan 2 makes the console-api speak the chitin layout natively.

- [ ] **Step 7: Commit the verification artifact**

If the snapshot revealed any drift, append a short note to the plan file (this file) describing what was actually observed; if everything matched, no commit needed for this task.

---

## Self-Review

### Spec coverage check

Goal claims:
- ✓ Stand up `~/.chitin/kanban/<board>/kanban.db` — Task 6 CLI writes there
- ✓ Mirror of hermes DBs — Task 2 implements `Migrate`
- ✓ One-shot migration CLI — Task 6 `kanban migrate <board>`
- ✓ Board-config field exposing new path — Task 5 `chitin_db_path`
- ✓ Hermes remains source of truth — confirmed by the "explicitly NOT" section; no hermes writes touched

### Placeholder scan

- No "TBD" or "implement later" tokens. ✓
- No "handle edge cases" hand-waves. ✓
- Every step includes the code or the exact command. ✓
- One mild deferred-detail: Task 5 says "match the existing pattern for `chitin_yaml`'s default" rather than transcribing the `Read` function verbatim because the implementor needs to see the actual current shape — that's a "read and follow" instruction, not a placeholder.

### Type / name consistency

- `Migrate(src, dest string) error` defined Task 2, used unchanged in Task 6 CLI.
- `RowCounts(db *sql.DB) (map[string]int, error)` defined Task 4, used in Task 6 CLI.
- `VerifyCounts(src, dest *sql.DB) error` defined Task 4, used in Task 6 CLI.
- `ApplySchema(db *sql.DB) error` defined Task 1, used by `Migrate` in Task 2.
- `migrationTables` package-level var defined Task 2, used by `RowCounts` in Task 4. Consistent.
- `chitin_db_path` field name used identically across Tasks 5 and 6. ✓

### Scope

- Stays inside `chitin-kernel`. No hermes-side or chitin-console-side code modified — that's Plan 2+. ✓

---

## Execution Handoff

Plan complete and saved to `docs/superpowers/plans/2026-05-15-chitin-kanban-db-foundation.md`. Two execution options:

1. **Subagent-Driven (recommended)** — Dispatch a fresh subagent per task, review between tasks, fast iteration.
2. **Inline Execution** — Execute tasks in this session using executing-plans, batch with checkpoints.

Follow-up plans (separate documents, not included here):
- **Plan 2:** Chitin-console-api cutover — point reads at `~/.chitin/kanban/<board>/kanban.db` (new shape, no `boards/` segment), drop the `HERMES_KANBAN_ROOT` dependency.
- **Plan 3:** Chitin-side write surface — kanban CRUD endpoints in console-api so the UI can mutate tickets without going through hermes.
- **Plan 4:** Retire hermes' DB as source of truth — turn hermes' kanban interactions into a thin shim over chitin's DB, cron-safe rollout, validation window.
