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
