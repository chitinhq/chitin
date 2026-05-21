package activities

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

// openProjectorDB opens a fresh SQLiteBoardProjector against a temp DB and
// returns it alongside a read-only handle for assertions. Both are closed via
// t.Cleanup.
func openProjectorDB(t *testing.T) (*SQLiteBoardProjector, *sql.DB) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "kanban.db")
	p, err := NewSQLiteBoardProjector(path)
	if err != nil {
		t.Fatalf("NewSQLiteBoardProjector: %v", err)
	}
	t.Cleanup(func() { _ = p.Close() })

	read, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open read handle: %v", err)
	}
	t.Cleanup(func() { _ = read.Close() })
	return p, read
}

// TestSQLiteBoardProjector_ProjectsTaskRow proves Project upserts a tasks row
// per node carrying the new status, and that the row is shaped for the Chitin
// Board read-model (spec 076 FR-014).
func TestSQLiteBoardProjector_ProjectsTaskRow(t *testing.T) {
	p, read := openProjectorDB(t)

	in := BoardProjectionInput{
		SchedulerRunID: "run-1",
		Transitions: []NodeTransition{
			{NodeID: "n-a", SpecRef: "076", TaskRef: "T009", FromStatus: "",
				ToStatus: "running", Capability: "code.implement", TargetRepo: "chitinhq/chitin"},
		},
	}
	if err := p.Project(context.Background(), in); err != nil {
		t.Fatalf("Project: %v", err)
	}

	var (
		title, status, createdBy, skills, tenant string
		completedAt                              sql.NullInt64
	)
	row := read.QueryRow(
		`SELECT title, status, created_by, skills, tenant, completed_at FROM tasks WHERE id = ?`, "n-a")
	if err := row.Scan(&title, &status, &createdBy, &skills, &tenant, &completedAt); err != nil {
		t.Fatalf("scan tasks row: %v", err)
	}
	if status != "running" {
		t.Errorf("status = %q, want running", status)
	}
	if createdBy != boardCreatedBy {
		t.Errorf("created_by = %q, want %q", createdBy, boardCreatedBy)
	}
	if skills != "code.implement" {
		t.Errorf("skills = %q, want code.implement", skills)
	}
	if tenant != "chitinhq/chitin" {
		t.Errorf("tenant = %q, want chitinhq/chitin", tenant)
	}
	if title != "spec 076 T009" {
		t.Errorf("title = %q, want %q", title, "spec 076 T009")
	}
	if completedAt.Valid {
		t.Errorf("completed_at set on a non-done node: %d", completedAt.Int64)
	}
}

// TestSQLiteBoardProjector_AppendsTaskEvent proves each transition appends a
// task_events row recording the from/to status change.
func TestSQLiteBoardProjector_AppendsTaskEvent(t *testing.T) {
	p, read := openProjectorDB(t)

	in := BoardProjectionInput{
		SchedulerRunID: "run-2",
		Transitions: []NodeTransition{
			{NodeID: "n-b", ToStatus: "running", FromStatus: "pending"},
		},
	}
	if err := p.Project(context.Background(), in); err != nil {
		t.Fatalf("Project: %v", err)
	}

	var kind, payload string
	row := read.QueryRow(
		`SELECT kind, payload FROM task_events WHERE task_id = ?`, "n-b")
	if err := row.Scan(&kind, &payload); err != nil {
		t.Fatalf("scan task_events row: %v", err)
	}
	if kind != "scheduler_node_transition" {
		t.Errorf("event kind = %q, want scheduler_node_transition", kind)
	}
	// Payload is hand-built JSON; assert the from/to status round-trips.
	for _, want := range []string{`"from_status":"pending"`, `"to_status":"running"`, `"scheduler_run_id":"run-2"`} {
		if !strings.Contains(payload, want) {
			t.Errorf("event payload %q missing %q", payload, want)
		}
	}
}

// TestSQLiteBoardProjector_UpsertUpdatesStatus proves a second transition for
// the same node updates the existing tasks row rather than inserting a
// duplicate — the board row is a live read-model of node state.
func TestSQLiteBoardProjector_UpsertUpdatesStatus(t *testing.T) {
	p, read := openProjectorDB(t)
	ctx := context.Background()

	if err := p.Project(ctx, BoardProjectionInput{
		SchedulerRunID: "run-3",
		Transitions:    []NodeTransition{{NodeID: "n-c", ToStatus: "running"}},
	}); err != nil {
		t.Fatalf("first Project: %v", err)
	}
	if err := p.Project(ctx, BoardProjectionInput{
		SchedulerRunID: "run-3",
		Transitions:    []NodeTransition{{NodeID: "n-c", FromStatus: "running", ToStatus: "done"}},
	}); err != nil {
		t.Fatalf("second Project: %v", err)
	}

	var count int
	if err := read.QueryRow(`SELECT COUNT(*) FROM tasks WHERE id = ?`, "n-c").Scan(&count); err != nil {
		t.Fatalf("count tasks: %v", err)
	}
	if count != 1 {
		t.Fatalf("tasks rows for n-c = %d, want 1 (upsert, not insert)", count)
	}

	var status string
	var completedAt sql.NullInt64
	if err := read.QueryRow(
		`SELECT status, completed_at FROM tasks WHERE id = ?`, "n-c").Scan(&status, &completedAt); err != nil {
		t.Fatalf("scan updated row: %v", err)
	}
	if status != "done" {
		t.Errorf("status after upsert = %q, want done", status)
	}
	if !completedAt.Valid {
		t.Error("completed_at not stamped when node reached done")
	}

	// Two transitions → two events appended (the event log is append-only).
	var events int
	if err := read.QueryRow(`SELECT COUNT(*) FROM task_events WHERE task_id = ?`, "n-c").Scan(&events); err != nil {
		t.Fatalf("count events: %v", err)
	}
	if events != 2 {
		t.Errorf("task_events for n-c = %d, want 2", events)
	}
}

// TestSQLiteBoardProjector_BatchIsAtomic proves a multi-transition batch lands
// every row — projection of one tick is a single transaction.
func TestSQLiteBoardProjector_BatchIsAtomic(t *testing.T) {
	p, read := openProjectorDB(t)

	in := BoardProjectionInput{
		SchedulerRunID: "run-4",
		Transitions: []NodeTransition{
			{NodeID: "n-1", ToStatus: "running"},
			{NodeID: "n-2", ToStatus: "running"},
			{NodeID: "n-3", ToStatus: "done"},
		},
	}
	if err := p.Project(context.Background(), in); err != nil {
		t.Fatalf("Project: %v", err)
	}

	var tasks, events int
	if err := read.QueryRow(`SELECT COUNT(*) FROM tasks`).Scan(&tasks); err != nil {
		t.Fatalf("count tasks: %v", err)
	}
	if err := read.QueryRow(`SELECT COUNT(*) FROM task_events`).Scan(&events); err != nil {
		t.Fatalf("count events: %v", err)
	}
	if tasks != 3 || events != 3 {
		t.Errorf("after 3-transition batch: tasks=%d events=%d, want 3 and 3", tasks, events)
	}
}

// TestSQLiteBoardProjector_EmptyBatchIsNoOp proves an empty batch writes
// nothing and does not fault.
func TestSQLiteBoardProjector_EmptyBatchIsNoOp(t *testing.T) {
	p, read := openProjectorDB(t)
	if err := p.Project(context.Background(), BoardProjectionInput{SchedulerRunID: "run-5"}); err != nil {
		t.Fatalf("Project empty batch: %v", err)
	}
	var tasks int
	if err := read.QueryRow(`SELECT COUNT(*) FROM tasks`).Scan(&tasks); err != nil {
		t.Fatalf("count tasks: %v", err)
	}
	if tasks != 0 {
		t.Errorf("empty batch wrote %d tasks rows, want 0", tasks)
	}
}

// TestSQLiteBoardProjector_SchemaIsReapplyable proves opening an existing DB a
// second time is idempotent — CREATE TABLE IF NOT EXISTS does not fault on a
// board the kernel (or a prior run) already created.
func TestSQLiteBoardProjector_SchemaIsReapplyable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "kanban.db")
	p1, err := NewSQLiteBoardProjector(path)
	if err != nil {
		t.Fatalf("first open: %v", err)
	}
	if err := p1.Project(context.Background(), BoardProjectionInput{
		SchedulerRunID: "run", Transitions: []NodeTransition{{NodeID: "n", ToStatus: "running"}},
	}); err != nil {
		t.Fatalf("project on first handle: %v", err)
	}
	_ = p1.Close()

	p2, err := NewSQLiteBoardProjector(path)
	if err != nil {
		t.Fatalf("reopen existing DB: %v", err)
	}
	defer func() { _ = p2.Close() }()
	// The earlier row must still be present — reopen does not wipe.
	if err := p2.Project(context.Background(), BoardProjectionInput{
		SchedulerRunID: "run", Transitions: []NodeTransition{{NodeID: "n", FromStatus: "running", ToStatus: "done"}},
	}); err != nil {
		t.Fatalf("project on reopened handle: %v", err)
	}
}

// TestBoardDBPath_PrefersEnv proves CHITIN_BOARD_DB overrides the canonical
// default path.
func TestBoardDBPath_PrefersEnv(t *testing.T) {
	t.Setenv("CHITIN_BOARD_DB", "/tmp/custom/board.db")
	got, err := BoardDBPath()
	if err != nil {
		t.Fatalf("BoardDBPath: %v", err)
	}
	if got != "/tmp/custom/board.db" {
		t.Errorf("BoardDBPath = %q, want the CHITIN_BOARD_DB value", got)
	}
}

// TestBoardDBPath_DefaultsToCanonical proves that with no env override the
// path falls back to the kernel's canonical ~/.chitin/kanban/chitin location.
func TestBoardDBPath_DefaultsToCanonical(t *testing.T) {
	t.Setenv("CHITIN_BOARD_DB", "")
	os.Unsetenv("CHITIN_BOARD_DB")
	got, err := BoardDBPath()
	if err != nil {
		t.Fatalf("BoardDBPath: %v", err)
	}
	home, _ := os.UserHomeDir()
	want := filepath.Join(home, ".chitin", "kanban", "chitin", "kanban.db")
	if got != want {
		t.Errorf("BoardDBPath = %q, want %q", got, want)
	}
}

// TestSQLiteBoardProjector_NilDBSurfacesError proves Project on a projector
// with no open DB returns an error rather than panicking.
func TestSQLiteBoardProjector_NilDBSurfacesError(t *testing.T) {
	var p *SQLiteBoardProjector
	if err := p.Project(context.Background(), BoardProjectionInput{
		SchedulerRunID: "run", Transitions: []NodeTransition{{NodeID: "n", ToStatus: "done"}},
	}); err == nil {
		t.Fatal("Project on a nil projector must return an error")
	}
	// Close on nil must be safe.
	if err := p.Close(); err != nil {
		t.Errorf("Close on nil projector: %v", err)
	}
}
