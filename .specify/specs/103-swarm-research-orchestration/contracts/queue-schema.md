# Contract — `~/.chitin/swarm-results.db` schema

SQLite via `modernc.org/sqlite`. Local-only; never tracked in git; FR-017 invariant.

## Schema v1

```sql
PRAGMA journal_mode = WAL;          -- single-writer + concurrent readers
PRAGMA foreign_keys = ON;
PRAGMA busy_timeout = 5000;

CREATE TABLE findings (
  queue_id                  TEXT PRIMARY KEY,
  ts                        TEXT NOT NULL,           -- RFC 3339
  source                    TEXT NOT NULL,
  agent_attribution         TEXT,
  tag                       TEXT,
  topic                     TEXT,
  file_path                 TEXT,
  frontmatter_json          TEXT,
  body_excerpt              TEXT,
  status                    TEXT NOT NULL CHECK (status IN
                              ('unprocessed','spec_drafted','discarded','deferred','source_deleted')),
  spec_drafted_ref          TEXT,
  triggered_by_chain_event  TEXT,
  confidence_signal         REAL,
  novelty_signal            TEXT,
  affects_core_infra        INTEGER NOT NULL DEFAULT 0,
  estimated_loc_range       TEXT,
  notes                     TEXT
);

CREATE INDEX findings_status_tag ON findings(status, tag);
CREATE INDEX findings_ts ON findings(ts);
CREATE UNIQUE INDEX findings_source_path ON findings(source, file_path)
  WHERE file_path IS NOT NULL;

CREATE TABLE _schema_version (
  version    INTEGER PRIMARY KEY,
  applied_at TEXT    NOT NULL
);
```

## Column semantics

| Column | Type | Notes |
|---|---|---|
| `queue_id` | TEXT | ULID. Lexicographically sortable, encodes timestamp + randomness. |
| `ts` | TEXT | When the finding was first ingested (NOT when the source file was authored). |
| `source` | TEXT | Matches `sources[].name` in `ingestion-sources.yml`. Required. |
| `agent_attribution` | TEXT | `ares` \| `clawta` \| NULL. Resolved from frontmatter or path heuristic. |
| `tag` | TEXT | Free-form. Inherited from schedule's `tag` if correlated; from `tag_default` of source if not; NULL otherwise. |
| `topic` | TEXT | Best-effort: from frontmatter.topic, OR from path (e.g., `Research/<TOPIC>/sources/`). |
| `file_path` | TEXT | Relative to source root. Used by `findings_source_path` unique index. NULL allowed for findings without a source file (rare). |
| `frontmatter_json` | TEXT | Full parsed frontmatter, JSON-serialized for query-ability. |
| `body_excerpt` | TEXT | First ~512 chars of the file body after frontmatter. For preview in `swarm-queue show`. |
| `status` | TEXT | State machine; see data-model.md. |
| `spec_drafted_ref` | TEXT | Populated when transitioning to `spec_drafted`. |
| `triggered_by_chain_event` | TEXT | Best-effort correlation (see research.md R8). |
| `confidence_signal` | REAL | 0.0–1.0 if from sentinel-mined frontmatter; NULL otherwise. |
| `novelty_signal` | TEXT | `novel` \| `partial-overlap` \| `covered`. NULL if not estimable. |
| `affects_core_infra` | INTEGER | 0 \| 1. Boolean signal future spec-authoring uses to prioritize. |
| `estimated_loc_range` | TEXT | `s` \| `m` \| `l` \| `xl`. Heuristic from agent. |
| `notes` | TEXT | Operator notes on triage. |

## Unique constraint rationale

`findings_source_path` (partial unique index over rows where `file_path IS NOT NULL`):
- Enforces FR-008 dedup ("file modified twice within 5s emits only the latest")
- Allows multiple findings with `file_path = NULL` (sentinel-mined findings without a file path, ad-hoc swarm-ask outputs)

`Repo.Upsert` uses `INSERT ... ON CONFLICT (source, file_path) DO UPDATE` — preserves `status`, `notes`, `spec_drafted_ref` (i.e., operator triage state) on re-ingestion; refreshes `ts`, `frontmatter_json`, `body_excerpt`, `confidence_signal`.

## Migration policy

`_schema_version` tracks applied migrations. v1 == initial creation. Future migrations:
- `v2`: any column rename, table rename, drop
- `v1 → v2 ...`: applied in order on open, recorded with `applied_at`

`Repo.Open(path)` runs migrations to current version automatically. Failure on open → fatal (operator must intervene).

## Concurrency

- WAL mode + busy_timeout=5000 handles writer + multiple readers
- Single writer process: `chitin-orchestrator` (CLI subcommands + worker activities all in-process)
- Multiple reader processes: console-api reads via `?mode=ro` for the `/swarm-queue` page

## Operator inspection

```bash
sqlite3 ~/.chitin/swarm-results.db
.schema findings
SELECT status, COUNT(*) FROM findings GROUP BY status;
SELECT * FROM findings WHERE tag = 'research-scan' ORDER BY ts DESC LIMIT 10;
```

## FR-017 privacy invariant

The DB is local-only:
- `~/.chitin/` is in `.gitignore` (already added in #976)
- The file is NEVER copied into the repo
- Console UI surfaces it through Tailscale-authed routes only (v1 accepts; future spec hardens)
- No webhook listener exposes the DB's contents

Any PR that surfaces this DB over an unauthenticated network endpoint violates the contract.
