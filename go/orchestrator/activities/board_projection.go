package activities

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// NodeTransition is one node-state change the scheduler projects to the
// Chitin Board read-model (spec 076 FR-014). It is a write-only record: the
// board reflects scheduler state, it never drives it (spec 070 FR-016).
type NodeTransition struct {
	// NodeID is the DAG node whose status changed.
	NodeID string `json:"node_id"`
	// SpecRef is the source spec the node derives from — e.g. "076".
	SpecRef string `json:"spec_ref"`
	// TaskRef is the task within the spec — e.g. "T009"; may be empty.
	TaskRef string `json:"task_ref"`
	// FromStatus is the node's previous status, as its spec wire name —
	// empty when the node had no prior projected status.
	FromStatus string `json:"from_status"`
	// ToStatus is the node's new status, as its spec wire name.
	ToStatus string `json:"to_status"`
	// Capability is the node's required capability tag — carried for board
	// display.
	Capability string `json:"capability"`
	// TargetRepo is the repository the node's work unit operates on.
	TargetRepo string `json:"target_repo"`
}

// BoardProjectionInput is the typed input to the ProjectToBoard activity — a
// batch of node-state transitions from one scheduler tick.
type BoardProjectionInput struct {
	// SchedulerRunID identifies the scheduler run the transitions belong to,
	// so the board can group a run's nodes.
	SchedulerRunID string `json:"scheduler_run_id"`
	// Transitions is the batch of node-state changes observed on this tick.
	// It is ordered by the scheduler (node id ascending) so projection is
	// deterministic.
	Transitions []NodeTransition `json:"transitions"`
}

// BoardProjector is the write-only sink for node-state transitions — the
// seam between the scheduler and the Chitin Board read-model (spec 076
// FR-014). It is an INTERFACE rather than a concrete board client because
// the kernel's board lives in a different Go module (internal/kanban) that
// the orchestrator module cannot import directly today; defining the seam
// here lets the scheduler ship without blocking on that cross-module wiring.
//
// An implementation MUST be write-only: Project records the transitions and
// returns. It MUST NOT be consulted to decide what the scheduler runs next —
// the runnable frontier is computed purely from the DAG and node states
// (spec 070 FR-016).
type BoardProjector interface {
	// Project records a batch of node-state transitions to the board
	// read-model. It returns an error only on a genuine write fault; a
	// projection fault must never stall the scheduler — the caller logs and
	// continues.
	Project(ctx context.Context, in BoardProjectionInput) error
}

// logBoardProjector is the fallback BoardProjector: it logs each transition
// rather than writing the real board. The concrete projector is
// SQLiteBoardProjector below; logBoardProjector remains as the safe default
// when no board DB is configured or reachable — projection is a read-model
// side effect, never on the scheduling critical path, so a missing board must
// degrade to logging rather than fault the scheduler.
type logBoardProjector struct{}

// Project logs each node-state transition. It never returns an error — a
// logging sink cannot fault — so the scheduler never stalls on projection.
func (logBoardProjector) Project(_ context.Context, in BoardProjectionInput) error {
	for _, t := range in.Transitions {
		from := t.FromStatus
		if from == "" {
			from = "(none)"
		}
		log.Printf(
			"board-projection: run=%s node=%s spec=%s task=%s %s -> %s cap=%s repo=%s",
			in.SchedulerRunID, t.NodeID, t.SpecRef, t.TaskRef, from, t.ToStatus, t.Capability, t.TargetRepo,
		)
	}
	return nil
}

// NewLogBoardProjector returns the fallback logging BoardProjector — the
// stand-in used when no board DB is configured (see SQLiteBoardProjector).
func NewLogBoardProjector() BoardProjector { return logBoardProjector{} }

// boardSchema is the minimal subset of the Chitin Board's canonical kanban
// schema (kernel internal/kanban.CanonicalSchema) that the projector writes:
// the `tasks` read-model row per DAG node and a `task_events` append per
// transition. It is embedded verbatim — NOT imported — because the kernel's
// kanban package lives in a separate, internal Go module the orchestrator
// cannot import. Kept a strict subset of the canonical DDL so a projector
// write against a board the kernel also owns is schema-compatible: every
// column here exists, with the same type, in the kernel's full schema, and
// CREATE TABLE IF NOT EXISTS is a no-op when the kernel already created them.
//
// The orchestrator only ever writes these two tables; it never reads the
// board to make a scheduling decision (spec 070 FR-016).
const boardSchema = `
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

CREATE TABLE IF NOT EXISTS task_events (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    task_id    TEXT NOT NULL,
    run_id     INTEGER,
    kind       TEXT NOT NULL,
    payload    TEXT,
    created_at INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_tasks_status ON tasks(status);
CREATE INDEX IF NOT EXISTS idx_events_task  ON task_events(task_id, created_at);
`

// boardCreatedBy is the `tasks.created_by` value stamped on every row the
// orchestrator projects, so a human reading the board can tell scheduler-
// projected rows apart from rows written by other producers.
const boardCreatedBy = "chitin-orchestrator"

// SQLiteBoardProjector is the concrete, write-only BoardProjector (spec 076
// FR-014). It writes orchestrator node-state directly into the Chitin Board
// SQLite database: an upserted `tasks` row per DAG node carrying the node's
// current status, and an appended `task_events` row per transition recording
// the from/to status change.
//
// It is WRITE-ONLY by construction — the board is a read-projection of
// scheduler state (spec 070 FR-016). SQLiteBoardProjector exposes no read
// path; the scheduler computes its runnable frontier purely from the DAG.
type SQLiteBoardProjector struct {
	// db is the open handle to the board SQLite database. modernc.org/sqlite
	// is pure Go (no cgo). A *sql.DB is a connection pool safe for concurrent
	// use, so one projector instance serves every projection activity.
	db *sql.DB
	// path is the resolved DB path, retained for error messages.
	path string
}

// BoardDBPath resolves the Chitin Board SQLite DB path: the CHITIN_BOARD_DB
// env var when set, else the canonical ~/.chitin/kanban/chitin/kanban.db
// location the kernel uses (kernel internal/boardconfig). It returns an error
// only when neither the env var nor the home directory can be resolved.
func BoardDBPath() (string, error) {
	if p := os.Getenv("CHITIN_BOARD_DB"); p != "" {
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("activities: resolving board DB path: %w", err)
	}
	return filepath.Join(home, ".chitin", "kanban", "chitin", "kanban.db"), nil
}

// NewSQLiteBoardProjector opens (creating its parent directory and applying
// the minimal schema if needed) the board SQLite DB at path and returns a
// projector bound to it. A path of "" resolves via BoardDBPath. The caller
// owns the returned projector's lifecycle and SHOULD Close it at shutdown.
//
// Opening the board is best-effort wiring done once at startup: callers that
// want the scheduler to keep running when the board is unavailable should
// fall back to NewLogBoardProjector on error rather than aborting.
func NewSQLiteBoardProjector(path string) (*SQLiteBoardProjector, error) {
	if path == "" {
		resolved, err := BoardDBPath()
		if err != nil {
			return nil, err
		}
		path = resolved
	}
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("activities: creating board DB dir %s: %w", dir, err)
		}
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("activities: opening board DB %s: %w", path, err)
	}
	// One writer at a time keeps SQLite happy under a connection pool.
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(boardSchema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("activities: applying board schema to %s: %w", path, err)
	}
	return &SQLiteBoardProjector{db: db, path: path}, nil
}

// Close releases the board DB handle. Safe to call on a nil receiver.
func (p *SQLiteBoardProjector) Close() error {
	if p == nil || p.db == nil {
		return nil
	}
	return p.db.Close()
}

// Project records a batch of node-state transitions to the board read-model.
// All transitions in the batch are written in a single SQLite transaction so
// a tick's projection is atomic — the board never reflects a half-applied
// tick. For each transition it upserts the node's `tasks` row to the new
// status and appends a `task_events` row describing the change.
//
// The board is a read-projection (spec 070 FR-016): Project only ever writes.
func (p *SQLiteBoardProjector) Project(ctx context.Context, in BoardProjectionInput) error {
	if p == nil || p.db == nil {
		return fmt.Errorf("activities: SQLiteBoardProjector has no open DB")
	}
	if len(in.Transitions) == 0 {
		return nil
	}

	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("activities: board projection begin tx: %w", err)
	}
	// Roll back unless an explicit Commit clears the deferred call. Rollback
	// on an already-committed tx returns sql.ErrTxDone, which is harmless.
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	now := time.Now().UnixMilli()
	for _, t := range in.Transitions {
		title := boardTaskTitle(t)
		// Upsert the node's read-model row. ON CONFLICT updates the mutable
		// projected fields; created_at is preserved by leaving it out of the
		// SET list. completed_at is stamped when the node reaches "done",
		// cleared otherwise so a re-opened node reads correctly.
		var completedAt any
		if t.ToStatus == "done" {
			completedAt = now
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO tasks (id, title, body, status, created_by, created_at, completed_at, tenant, skills)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
    title        = excluded.title,
    body         = excluded.body,
    status       = excluded.status,
    completed_at = excluded.completed_at,
    tenant       = excluded.tenant,
    skills       = excluded.skills`,
			t.NodeID, title, boardTaskBody(t), t.ToStatus, boardCreatedBy, now, completedAt,
			t.TargetRepo, t.Capability,
		); err != nil {
			return fmt.Errorf("activities: board projection upsert task %s: %w", t.NodeID, err)
		}

		// Append the transition as a task_events row. The payload is a small
		// JSON object so a board reader can reconstruct the change.
		if _, err := tx.ExecContext(ctx, `
INSERT INTO task_events (task_id, kind, payload, created_at)
VALUES (?, ?, ?, ?)`,
			t.NodeID, "scheduler_node_transition", boardEventPayload(in.SchedulerRunID, t), now,
		); err != nil {
			return fmt.Errorf("activities: board projection append event for %s: %w", t.NodeID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("activities: board projection commit: %w", err)
	}
	committed = true
	return nil
}

// boardTaskTitle builds the human-facing title for a node's board row from its
// spec and task refs, falling back to the node id when neither is set.
func boardTaskTitle(t NodeTransition) string {
	switch {
	case t.SpecRef != "" && t.TaskRef != "":
		return fmt.Sprintf("spec %s %s", t.SpecRef, t.TaskRef)
	case t.SpecRef != "":
		return fmt.Sprintf("spec %s — %s", t.SpecRef, t.NodeID)
	default:
		return t.NodeID
	}
}

// boardTaskBody builds the body text for a node's board row — a one-line
// summary of the node's routing metadata for a human reading the board.
func boardTaskBody(t NodeTransition) string {
	return fmt.Sprintf("node %s · capability %s · repo %s", t.NodeID, t.Capability, t.TargetRepo)
}

// boardEventPayload builds the JSON payload stored on the task_events row for
// one transition. It is hand-built (not encoding/json) so the field order is
// fixed and the projection is byte-deterministic for a given input.
func boardEventPayload(runID string, t NodeTransition) string {
	from := t.FromStatus
	if from == "" {
		from = "(none)"
	}
	return fmt.Sprintf(
		`{"scheduler_run_id":%q,"node_id":%q,"spec_ref":%q,"task_ref":%q,"from_status":%q,"to_status":%q,"capability":%q,"target_repo":%q}`,
		runID, t.NodeID, t.SpecRef, t.TaskRef, from, t.ToStatus, t.Capability, t.TargetRepo,
	)
}

// BoardProjection is the ProjectToBoard activity (spec 076 FR-014).
// Projecting node-state transitions to the board read-model is a SIDE EFFECT
// — a write to an external store — so it MUST run in an activity, never in
// workflow code. The activity is bound to a BoardProjector at worker-host
// startup.
type BoardProjection struct {
	// projector is the write-only board sink. It is never read by the
	// scheduler — the board reflects scheduler state, never drives it.
	projector BoardProjector
}

// NewBoardProjection returns a ProjectToBoard activity bound to projector.
// A nil projector falls back to the logging projector so the activity is
// always usable.
func NewBoardProjection(projector BoardProjector) *BoardProjection {
	if projector == nil {
		projector = NewLogBoardProjector()
	}
	return &BoardProjection{projector: projector}
}

// ActivityName is the stable Temporal activity name ProjectToBoard registers
// under and the scheduler workflow dispatches to.
func (a *BoardProjection) ActivityName() string { return "ProjectToBoard" }

// Execute projects one tick's batch of node-state transitions to the board
// read-model. It is the activity function registered with the Temporal
// worker. Projection is write-only (spec 070 FR-016); the result is never
// fed back into scheduling.
func (a *BoardProjection) Execute(ctx context.Context, in BoardProjectionInput) error {
	if a.projector == nil {
		return fmt.Errorf("activities: ProjectToBoard has no BoardProjector bound")
	}
	if len(in.Transitions) == 0 {
		return nil
	}
	if err := a.projector.Project(ctx, in); err != nil {
		return fmt.Errorf("activities: ProjectToBoard for run %s: %w", in.SchedulerRunID, err)
	}
	return nil
}
