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
