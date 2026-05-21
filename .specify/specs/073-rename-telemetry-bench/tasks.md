# Tasks: Collapse to Chitin Telemetry + Chitin Bench

**Spec**: [spec.md](./spec.md) | **Plan**: [plan.md](./plan.md) | **Branch**: `073-rename-telemetry-bench`

**Input**: Rename + collapse refactor — `git mv` plus reference rewrites,
executed in the safe order set by `plan.md`: repo renames → telemetry
collapse → operational cutover → reference sweep.

## Conventions

- `[P]` = parallelizable (touches a disjoint file set from its siblings).
- Every task ends CI-green: `pytest` for touched Python, `go build`/`go vet`
  where Go references a renamed path. No task lands red.
- No history rewrite — historical/superseded spec files keep old names.
- Agent identities **Ares** and **Clawta** are NOT renamed (FR-006).

---

## Phase 1 — Repo renames (CI-gated) ✅ DONE

Shipped in PR #841 (`spec 073 Phase 1`, merged 2026-05-20): `swarm/icarus_harness/`
→ `swarm/chitin_bench/`, the `icarus-bench-*` / `icarus-watcher` scripts and
installers renamed per the Rename Map, `AGENT_IMPORT_PATH` updated to
`swarm.chitin_bench.agent:BenchAgent`, Go/test references rewritten.

---

## Phase 2 — Telemetry collapse (US1, P1)

Goal: one `python/chitin_telemetry` package and one `/telemetry` skill,
collapsed from `python/argus` + the Sentinel detection/analysis halves of
`python/analysis`. Satisfies FR-001, SC-001.

- **T001** — Inventory the observability surface: enumerate every module in
  `python/argus/` and every Sentinel detection/analysis module in
  `python/analysis/` (detection passes, analysis, the digest path). Record
  the old→new module map in `research.md` so the collapse is auditable.
- **T002** — Create `python/chitin_telemetry/` package skeleton: `__init__.py`,
  `pyproject.toml`/package metadata mirroring `python/argus`'s, and a test
  dir `python/chitin_telemetry/tests/`.
- **T003** — `git mv` `python/argus/*` into `python/chitin_telemetry/`;
  rewrite intra-package imports (`from argus...` → `from chitin_telemetry...`).
  `pytest python/chitin_telemetry` green.
- **T004** — `git mv` the Sentinel detection/analysis modules out of
  `python/analysis/` into `python/chitin_telemetry/`; leave non-Sentinel
  `python/analysis` code (e.g. `proposals/`) in place. Rewrite imports both
  directions. `pytest` green for both packages.
- **T005** [P] — Rewrite every external importer of `argus` / the moved
  `analysis` modules (Go adapters, `swarm/` scripts, other Python). `go build
  ./...` and `pytest` green.
- **T006** [P] — Rename the `/sentinel` skill to `/telemetry`:
  `claude/skills/sentinel.md` → `claude/skills/telemetry.md`, update the skill
  body, and rewire `.claude/commands/` via `scripts/sync-skills.sh`. Same
  capability, descriptive name (FR-001 AC2).
- **T007** — Verification: `pytest` green; a grep for `import argus` /
  `from argus` / Sentinel module paths over active code returns nothing.

---

## Phase 3 — Operational cutover (US2, P2 — last, careful)

Goal: the renamed bench service and board take over with zero lost state.
Satisfies FR-003, FR-004, SC-003, SC-004. Run-beside-then-cut — no big bang.

- **T008** — Install `chitin-bench.service` (from `swarm/systemd/chitin-bench.service`,
  renamed in Phase 1) **beside** the running `icarus-bench.service`; start it
  and confirm a bench tick runs under the new unit.
- **T009** — Stop + disable `icarus-bench.service` only after T008 proves the
  new unit healthy. An in-flight run under the old unit is cheap — the LRU
  picker re-runs it (FR-004, edge case). Confirm `chitin-bench.service` is the
  sole active bench unit.
- **T010** — Migrate the `icarus` kanban board → `chitin-bench` board,
  preserving every ticket, comment, and status. The `chitin-bench` board
  already exists (`list_boards` confirms) and was pre-migrated — reconcile
  any tickets still only on `icarus`, then retire the `icarus` board entry.
  Verify 100% ticket parity (SC-004).
- **T011** [P] — Repoint crons/watchers: any `hermes cron` job or board
  watcher referencing `icarus` → `chitin-bench`. `jobs/icarus/` output dir →
  `jobs/chitin-bench/`.
- **T012** — Verification: `chitin-bench.service` active; `systemctl` shows no
  `icarus-*` unit; the `chitin-bench` board holds every former `icarus`
  ticket; `/evolve` skill still resolves the bench (SC-005).

---

## Phase 4 — Reference sweep (US3, P3 — cleanup tail)

Goal: no abstract code name remains on an active surface. Satisfies FR-005,
FR-007, SC-002.

- **T013** [P] — Update `.specify/specs/INDEX.md`: the Telemetry/Bench rows
  reference Chitin Telemetry and Chitin Bench; mark the retired-name specs
  superseded by 073.
- **T014** [P] — Update the `/evolve` skill and any remaining docs
  (`docs/`, `README`, `CLAUDE.md` skill list) to the new names.
- **T015** — **Grep gate** (final): a repo-wide search for `icarus`, `argus`,
  `sentinel` as subsystem names returns only historical/superseded spec files
  (FR-007, SC-001, SC-002). Wire this grep as a CI check so a regression is
  caught. Spec 073 is not done until this gate is green.

---

## Dependencies

- Phase 1 (done) precedes all.
- Phase 2: T001 → T002 → T003 → T004 → {T005, T006} → T007.
- Phase 3 strictly after Phase 2 (the collapse must land first). T008 → T009;
  T010, T011 may run after T008; T012 closes the phase.
- Phase 4 after Phase 3. T013, T014 are `[P]`; T015 is the final gate.

## Parallel execution notes

- Within Phase 2, T005 (external importers) and T006 (skill rename) touch
  disjoint files and may run together once T004 lands.
- Phase 3 is operational (service + board) and MUST NOT be parallelized with
  Phase 2 code changes — state safety depends on a stable package.
