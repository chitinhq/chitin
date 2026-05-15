package kanban

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

// seedHermesChitinShape creates a DB matching the chitin hermes board
// schema (the wider schema with block_reason etc.) and inserts a few
// representative rows across the major tables.
func seedHermesChitinShape(t *testing.T, dbPath string) {
	t.Helper()
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

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

	var blockReason sql.NullString
	if err := db.QueryRow("SELECT block_reason FROM tasks WHERE id='t_bbb'").Scan(&blockReason); err != nil {
		t.Fatalf("select block_reason: %v", err)
	}
	if !blockReason.Valid || blockReason.String != "no_pr" {
		t.Errorf("block_reason: want 'no_pr', got %v", blockReason)
	}
}

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

	var blockReason sql.NullString
	if err := db.QueryRow("SELECT block_reason FROM tasks WHERE id='t_rb1'").Scan(&blockReason); err != nil {
		t.Fatalf("block_reason: %v", err)
	}
	if blockReason.Valid {
		t.Errorf("block_reason: want NULL, got %q", blockReason.String)
	}
}

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
