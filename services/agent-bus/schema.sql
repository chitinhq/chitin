-- agent-bus schema. Threaded message store for agent-to-agent + human-in-loop
-- comms across the chitin swarm. Reddit-style threads + replies.
--
-- Storage: ~/.chitin/agent-bus/bus.db (sqlite, WAL).
-- Consumers: MCP stdio server (services/agent-bus/server.py),
-- chitin-console-api (followup), Discord ingester (followup).
--
-- Schema is intentionally simple and stable — it's the contract for every
-- consumer. New columns are additive only; never repurpose a column.

PRAGMA journal_mode = WAL;
PRAGMA foreign_keys = ON;

-- Threads. Top-level conversations. Reddit "post" equivalent.
-- A thread can be board-scoped (e.g. chitin or readybench) or global.
-- Optional task_id links a thread to a kanban ticket without coupling
-- to the kanban schema (we just store the string id).
CREATE TABLE IF NOT EXISTS threads (
  id                 INTEGER PRIMARY KEY AUTOINCREMENT,
  board              TEXT,                                       -- chitin | readybench | NULL=global
  task_id            TEXT,                                       -- optional kanban ticket reference (e.g. t_abc123)
  title              TEXT NOT NULL,
  author             TEXT NOT NULL,                              -- agent id (e.g. red, hermes, clawta, claude-code)
  audience           TEXT,                                       -- comma-separated agent ids; NULL=public/all
  status             TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open','resolved','archived')),
  discord_thread_id  TEXT,                                       -- optional Discord channel/thread mirror
  created_at         INTEGER NOT NULL,
  updated_at         INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_threads_board    ON threads(board);
CREATE INDEX IF NOT EXISTS idx_threads_task     ON threads(task_id);
CREATE INDEX IF NOT EXISTS idx_threads_status   ON threads(status);
CREATE INDEX IF NOT EXISTS idx_threads_updated  ON threads(updated_at);

-- Messages. Replies live here. parent_id NULL = first message in thread.
-- audience NULL on a message means "inherit from thread."
-- ack_required=1 marks a message that wants explicit acknowledgement
-- (used for "directives" — the inbox shows these as unread until acked).
CREATE TABLE IF NOT EXISTS messages (
  id                  INTEGER PRIMARY KEY AUTOINCREMENT,
  thread_id           INTEGER NOT NULL REFERENCES threads(id),
  parent_id           INTEGER REFERENCES messages(id),
  author              TEXT NOT NULL,
  audience            TEXT,
  body                TEXT NOT NULL,
  kind                TEXT NOT NULL DEFAULT 'message' CHECK (kind IN ('message','directive','ack','system')),
  discord_message_id  TEXT,
  ack_required        INTEGER NOT NULL DEFAULT 0,
  created_at          INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_messages_thread     ON messages(thread_id);
CREATE INDEX IF NOT EXISTS idx_messages_author     ON messages(author);
CREATE INDEX IF NOT EXISTS idx_messages_created    ON messages(created_at);
CREATE INDEX IF NOT EXISTS idx_messages_ack_open   ON messages(ack_required, created_at) WHERE ack_required = 1;

-- Reads / acks. (message_id, agent_id) is the read receipt for an agent.
-- inbox(agent_id) = messages where audience targets agent AND no read row exists.
CREATE TABLE IF NOT EXISTS reads (
  message_id  INTEGER NOT NULL REFERENCES messages(id),
  agent_id    TEXT NOT NULL,
  read_at     INTEGER NOT NULL,
  PRIMARY KEY (message_id, agent_id)
);
CREATE INDEX IF NOT EXISTS idx_reads_agent ON reads(agent_id);

-- Attachments. Typed links a thread carries (Jira-style):
--   spec  -> path under .specify/specs/...
--   pr    -> github PR number (optionally repo-qualified: 'owner/repo#123')
--   task  -> kanban ticket id (t_xxxxxxxx)
--   discord -> discord message/thread id
--   url   -> arbitrary web URL
--   file  -> repo-relative file path
-- Console-side renderers turn these into clickable badges; MCP exposes them
-- in bus_read_thread().
CREATE TABLE IF NOT EXISTS attachments (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  thread_id   INTEGER NOT NULL REFERENCES threads(id),
  kind        TEXT NOT NULL CHECK (kind IN ('spec','pr','task','discord','url','file')),
  ref         TEXT NOT NULL,
  display     TEXT,
  created_at  INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_attachments_thread ON attachments(thread_id);
CREATE INDEX IF NOT EXISTS idx_attachments_kind   ON attachments(kind, ref);

-- Agents. Identity + last-seen for inbox display + presence.
-- Self-registered on first call; explicit registration is optional.
CREATE TABLE IF NOT EXISTS agents (
  id            TEXT PRIMARY KEY,
  display_name  TEXT,
  last_seen_at  INTEGER
);

-- Schema-version sentinel. Bump on every additive migration.
CREATE TABLE IF NOT EXISTS schema_version (
  version INTEGER PRIMARY KEY,
  applied_at INTEGER NOT NULL
);
INSERT OR IGNORE INTO schema_version(version, applied_at) VALUES (1, strftime('%s','now'));
