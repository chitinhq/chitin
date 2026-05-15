package main

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

// seedKanbanSourceDB writes a minimal hermes-shape kanban.db at the
// given path. Uses the canonical schema since it's a superset of any
// hermes board.
func seedKanbanSourceDB(t *testing.T, path string, rowCount int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	_, err = db.Exec(`
		CREATE TABLE tasks (
			id TEXT PRIMARY KEY,
			title TEXT NOT NULL,
			status TEXT NOT NULL,
			created_at INTEGER NOT NULL,
			block_reason TEXT
		);
	`)
	if err != nil {
		t.Fatalf("create tasks: %v", err)
	}
	for i := 0; i < rowCount; i++ {
		if _, err := db.Exec(
			"INSERT INTO tasks (id, title, status, created_at) VALUES (?, ?, ?, ?)",
			"t_"+strings.Repeat("a", i+1), "title", "ready", 1000+i,
		); err != nil {
			t.Fatalf("insert: %v", err)
		}
	}
}

func TestCLI_KanbanMigrateCopiesRowsToChitinDB(t *testing.T) {
	home := t.TempDir()
	writeCLIConfig(t, home, "chitin", `{"repo":"chitinhq/chitin","default_branch":"main","workspace_root":"~/workspace/chitin","kernel_bin":"chitin-kernel"}`)
	srcPath := filepath.Join(home, ".hermes", "kanban", "boards", "chitin", "kanban.db")
	seedKanbanSourceDB(t, srcPath, 3)

	stdout, stderr, code := runCLIWithEnv(t, t.TempDir(), []string{"HOME=" + home},
		"kanban", "migrate", "chitin")
	if code != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "tasks: 3") {
		t.Errorf("stdout missing tasks count: %q", stdout)
	}
	if !strings.Contains(stdout, "ok:") {
		t.Errorf("stdout missing ok line: %q", stdout)
	}

	destPath := filepath.Join(home, ".chitin", "kanban", "chitin", "kanban.db")
	if _, err := os.Stat(destPath); err != nil {
		t.Errorf("dest DB not created at %s: %v", destPath, err)
	}

	db, err := sql.Open("sqlite", destPath)
	if err != nil {
		t.Fatalf("open dest: %v", err)
	}
	defer db.Close()
	var n int
	if err := db.QueryRow("SELECT count(*) FROM tasks").Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 3 {
		t.Errorf("dest tasks: want 3, got %d", n)
	}
}

func TestCLI_KanbanMigrateUnknownBoardExit3(t *testing.T) {
	home := t.TempDir()
	// Need at least one board so we don't get ErrNoBoardsInitialized.
	writeCLIConfig(t, home, "chitin", `{"repo":"chitinhq/chitin","default_branch":"main","workspace_root":"~/workspace/chitin","kernel_bin":"chitin-kernel"}`)

	_, stderr, code := runCLIWithEnv(t, t.TempDir(), []string{"HOME=" + home},
		"kanban", "migrate", "nope")
	if code != 3 {
		t.Fatalf("exit=%d stderr=%q", code, stderr)
	}
	if !strings.Contains(stderr, "unknown_board") && !strings.Contains(stderr, "unknown board") {
		t.Errorf("stderr missing unknown board signal: %q", stderr)
	}
}

func TestCLI_KanbanMigrateMissingSourceDBExit2(t *testing.T) {
	home := t.TempDir()
	writeCLIConfig(t, home, "chitin", `{"repo":"chitinhq/chitin","default_branch":"main","workspace_root":"~/workspace/chitin","kernel_bin":"chitin-kernel"}`)
	// No source DB written.

	_, stderr, code := runCLIWithEnv(t, t.TempDir(), []string{"HOME=" + home},
		"kanban", "migrate", "chitin")
	if code != 2 {
		t.Fatalf("exit=%d stderr=%q", code, stderr)
	}
	if !strings.Contains(stderr, "source_missing") && !strings.Contains(stderr, "kanban.db") {
		t.Errorf("stderr missing source signal: %q", stderr)
	}
}

func TestCLI_KanbanNoSubcommandExit2(t *testing.T) {
	home := t.TempDir()
	_, stderr, code := runCLIWithEnv(t, t.TempDir(), []string{"HOME=" + home}, "kanban")
	if code != 2 {
		t.Fatalf("exit=%d stderr=%q", code, stderr)
	}
	if !strings.Contains(stderr, "usage") {
		t.Errorf("stderr missing usage: %q", stderr)
	}
}
