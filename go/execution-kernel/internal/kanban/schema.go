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
