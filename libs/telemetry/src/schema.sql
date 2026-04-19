CREATE TABLE IF NOT EXISTS events (
  run_id         TEXT NOT NULL,
  session_id     TEXT NOT NULL,
  ts             TEXT NOT NULL,
  surface        TEXT NOT NULL,
  driver         TEXT NOT NULL,
  agent_id       TEXT NOT NULL,
  tool_name      TEXT NOT NULL,
  action_type    TEXT NOT NULL,
  result         TEXT NOT NULL,
  duration_ms    INTEGER NOT NULL,
  error          TEXT,
  raw_input      TEXT NOT NULL,
  canonical_form TEXT NOT NULL,
  metadata       TEXT NOT NULL,
  PRIMARY KEY (run_id, session_id, ts, tool_name)
);

CREATE INDEX IF NOT EXISTS idx_events_surface ON events (surface);
CREATE INDEX IF NOT EXISTS idx_events_run     ON events (run_id);
CREATE INDEX IF NOT EXISTS idx_events_session ON events (session_id);
CREATE INDEX IF NOT EXISTS idx_events_ts      ON events (ts);
