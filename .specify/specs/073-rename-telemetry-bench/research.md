# 073 — Phase 0 Research

A rename refactor has few open decisions. The two that matter:

## D1 — Kanban board rename: recreate-and-migrate, not in-place

- **Decision**: rename the `icarus` board to `chitin-bench` by creating the
  new board and migrating its tickets, rather than renaming the board
  directory in place.
- **Rationale**: a kanban board is a SQLite DB under
  `~/.hermes/kanban/boards/<name>/`. The board name is embedded in ticket
  references, cron `--board` flags, and agent config. A recreate-and-migrate
  pass (copy the DB, register the new board, repoint references, retire the
  old) is verifiable ticket-by-ticket (SC-004); an in-place `mv` risks
  dangling references with no checkpoint.
- **Alternatives**: in-place directory rename — rejected (no per-ticket
  verification, references break silently).

## D2 — Service cutover: beside-then-stop

- **Decision**: install `chitin-bench.service`, start it, confirm it is
  driving bench runs, then stop + disable `icarus-bench.service`.
- **Rationale**: FR-004 — no big-bang. The `flock` single-flight in the
  bench runner means the two services cannot stack a double run during the
  brief overlap; an in-flight run lost at cutover is re-picked by the LRU
  task selector — cheap.
- **Alternatives**: stop-old-then-start-new — rejected (a gap with no bench
  running, however short, is avoidable).

## D3 — Telemetry collapse: one package, history kept

- **Decision**: `python/argus` + the Sentinel detection/analysis from
  `python/analysis` move into a new `python/chitin_telemetry/` package via
  `git mv` (history preserved). The `/sentinel` skill becomes `/telemetry`.
- **Rationale**: FR-001 — one subsystem, one skill. `git mv` keeps `git
  blame`/history intact across the collapse.

## D4 — Historical spec files are not renamed

- **Decision**: superseded/historical specs that mention `icarus`/`argus`/
  `sentinel` keep their text; only *active* surfaces are renamed. The grep
  gate (FR-007) excludes `.specify/specs/`.
- **Rationale**: history is a record; rewriting it adds churn and risk for
  no operational gain.

## Phase 2 module map

Observed and applied in this worktree:

- **Argus package → `python/chitin_telemetry/`**:
  `beliefs.py`, `cli.py`, `config.py`, `detectors.py`, `findings_cli.py`,
  `findings_store.py`, `gpu.py`, `indexer.py`, `judge.py`, `kernel.py`,
  `llm.py`, `logs.py`, `migrations.py`, `prompts.py`, `reporter.py`,
  `session_indexer.py`, `sources.py`, package metadata, systemd units, and
  the full Argus test suite.
- **Sentinel / policy-analysis modules moved out of `python/analysis/` into
  `python/chitin_telemetry/`**:
  `decisions.py`, `detect.py`, `draft.py`, `llm_draft.py`, `loaders.py`,
  `models.py`, `telemetry.py` (renamed from `sentinel.py`), `writers.py`,
  `templates/`, the policy-analysis spec, and the matching tests.
- **`python/analysis/` left in place**:
  `analyzer.py`, `codex_mine.py`, `debt.py`, `floundering_calibration.py`,
  `predict.py`, `skill_mine.py`, `souls.py`, `speckit_adapter.py`,
  `superpowers_adapter.py`, `unified_spec.py`, `proposals/`, and their
  remaining tests.
- **External importer rewrites**:
  bench tasks now invoke `python -m chitin_telemetry.telemetry`; the swarm
  canary consumer and related tests import `chitin_telemetry.*`; the swarm
  skill surface is `swarm/roles/telemetry/SKILL.md`.
