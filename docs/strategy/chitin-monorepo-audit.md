# Chitin Monorepo Structure Audit

**Date**: 2026-05-20
**Author**: red (session audit)
**Companion spec**: `.specify/specs/074-polyglot-monorepo-layout/spec.md`
**Scope**: How the Chitin repo uses Nx, how Go and Python sit (or don't sit)
inside it, and what a correct polyglot layout looks like.

---

## TL;DR

- Chitin is an **Nx 22.6.5 monorepo**. `nx show projects` reports **18
  registered projects**.
- Nx is **language-agnostic** — it orchestrates *tasks*, not JavaScript. The
  Go `execution-kernel` and the Python `analysis` / `router-plugin-api-python`
  packages are *already* first-class Nx projects today, driven through
  `nx:run-commands`. So "Go isn't integrated" is **half true** at most.
- There are two real problems, and the folder location is the smaller one:
  1. **Registration gap** — a large amount of code is invisible to Nx:
     `go/run-sdk`, `python/argus`, all of `services/*`, most of `swarm/*`,
     and `bench/`. They have no `project.json`, so `nx affected`, the
     dependency graph, remote caching, and CI's affected-detection are all
     **blind** to them. This is the actual integration failure.
  2. **Inconsistency** — three different registration mechanisms, two
     contradictory polyglot layouts, and inconsistent tags/targets.
- The "Go isn't under `apps/` or `libs/`" feeling is a **navigation /
  consistency** problem, **not a functional one**. Since Nx 16.8, `apps/`
  and `libs/` are pure convention — a project works identically wherever it
  lives, as long as it is *registered*.
- **Recommendation**: a 3-phase change (spec 074). Phase 1 closes the
  registration gap (high value, low risk). Phase 2 standardizes registration,
  tags, and targets. Phase 3 converges the folder layout to a single
  type-first scheme (`apps/` + `libs/`, language as a tag).

---

## 1. Current state

### 1.1 What Nx sees today — 18 registered projects

| Project (Nx name)                    | Path                                  | Lang   | How registered            |
|--------------------------------------|---------------------------------------|--------|---------------------------|
| `chitin-console`                     | `apps/chitin-console`                 | TS/Ng  | `project.json`            |
| `chitin-console-api`                 | `apps/chitin-console-api`             | TS     | `project.json`            |
| `@chitin/cli`                        | `apps/cli`                            | TS     | `package.json` `nx` field |
| `chitin-agentguard`                  | `apps/agentguard-vscode`              | TS     | inferred (`package.json`) |
| `@chitin/dashboard`                  | `apps/chitin-dashboard`               | TS/Ng  | inferred (`package.json`) |
| `@chitinhq/openclaw-plugin-governance` | `apps/openclaw-plugin-governance`   | TS     | inferred (`package.json`) |
| `execution-kernel`                   | `go/execution-kernel`                 | **Go** | `project.json`            |
| `@chitin/contracts`                  | `libs/contracts`                      | TS     | inferred (`package.json`) |
| `@chitin/telemetry`                  | `libs/telemetry`                      | TS     | inferred (`package.json`) |
| `@chitin/run-sdk`                    | `libs/run-sdk`                        | TS     | inferred (`package.json`) |
| `@chitin/adapter-claude-code`        | `libs/adapters/claude-code`           | TS     | inferred (`package.json`) |
| `@chitin/adapter-ollama-local`       | `libs/adapters/ollama-local`          | TS     | inferred (`package.json`) |
| `@chitin/adapter-openclaw`           | `libs/adapters/openclaw`              | TS     | inferred (`package.json`) |
| `@chitin/router-plugin-api`          | `libs/router-plugin-api/typescript`   | TS     | `package.json` `nx` field |
| `chitin-router-plugin-api-python`    | `libs/router-plugin-api/python`       | **Py** | `project.json`            |
| `chitin-analysis`                    | `python/analysis`                     | **Py** | `project.json`            |
| `@chitin/generators`                 | `tools/generators`                    | TS     | inferred (`package.json`) |
| `@chitin/tooling-lint`               | `tools/lint`                          | TS     | inferred (`package.json`) |

Three of the 18 are Go/Python — Nx polyglot orchestration **already works
here**. The mechanism is uniform: a `project.json` with `nx:run-commands`
targets shelling out to `go build` / `pytest` / `compileall`.

### 1.2 What Nx does NOT see — the registration gap

| Directory                       | Lang        | Why it matters                                                |
|---------------------------------|-------------|---------------------------------------------------------------|
| `go/run-sdk`                    | Go          | A Go module (`cmd/sdk-fixture`); no `project.json`            |
| `python/argus`                  | Python      | A real package w/ CLI entry point + systemd units; depends on `chitin-analysis`; **not registered** |
| `services/agent-bus`            | Python      | Discord mirror service (~28 KB server) — but see §1.6         |
| `services/mini-mcp`             | Python      | MCP server                                                    |
| `services/swarm-kanban-mcp`     | Python      | Kanban MCP server                                             |
| `swarm/mini`, `swarm/workflows`, `swarm/bin`, `swarm/tests` | Python + shell + `.lobster` | Dispatch / judge / watchdog system |
| `bench/`                        | Python      | Governance benchmark harness (`run.py`)                       |
| `web/`                          | TS          | Static SPA + a vitest file; tests run via root `vite.config.ts` only |

Consequence: a change in any of these does **not** light up in `nx affected`,
is **not** cached, does **not** appear in `nx graph`, and CI must hand-roll
per-language test steps (it does — see §1.5) instead of letting Nx compute
the affected set.

### 1.3 Three ways a project gets registered today

1. **Explicit `project.json`** — `chitin-console`, `chitin-console-api`,
   `execution-kernel` (Go), `chitin-analysis` (Py),
   `chitin-router-plugin-api-python` (Py).
2. **`nx` field inside `package.json`** — `apps/cli`,
   `libs/router-plugin-api/typescript`.
3. **Pure inference** by the `@nx/js/typescript` plugin from a bare
   `package.json` + `tsconfig.json` — the adapters, contracts, telemetry,
   run-sdk, dashboard, agentguard, generators, lint.
4. **(Not registered at all)** — everything in §1.2.

Mechanisms 1–3 all work, but mixing them means there is no single place to
look to answer "is this code an Nx project, and what are its targets?"

### 1.4 Two contradictory polyglot layouts

The repo contains **both** patterns for placing non-TS code, and they
disagree:

- **Pattern A — domain co-located, language as subfolder.**
  `libs/router-plugin-api/` holds `python/` *and* `typescript/` side by
  side: one domain ("router plugin API"), two language implementations,
  each its own Nx project. This is the Nx-idiomatic polyglot pattern.
- **Pattern B — language-first top-level folders.**
  `go/` and `python/` are top-level siblings of `apps/` and `libs/`. Code
  is grouped by *language*, not by *type* or *domain*.

`router-plugin-api` is the proof that Pattern A works in this repo. The
existence of both is the inconsistency the goal describes as "weird."

### 1.5 How the build actually works today

- **Root `package.json`** — three scripts only (`install-kernel`,
  `worktree`, `console`). No unified `build` / `test` / `lint`.
- **Root `Makefile`** — one target (`drive-copilot-live`), a manual Go
  integration test.
- **Root `vite.config.ts`** — global Vitest config; globs
  `web/**`, `libs/**`, `apps/**` test files.
- **`.github/workflows/ci.yml`** — does the heavy lifting and reveals the
  gap: it runs **Nx affected** for TS (`typecheck`, `test`, `validate`),
  then **separately and unconditionally** runs `go test ./...`,
  `go vet ./...`, hand-picked Python `unittest` runs, a governance gate, and
  the kernel build. Go and Python are bolted on *beside* Nx, not *through*
  it. After Phase 1 these become `nx affected -t test` like everything else.
- **`pnpm-workspace.yaml`** lists `go/*` and `tools/*` as pnpm packages —
  but `go/run-sdk` has no `package.json`, so that glob is half-empty and
  misleading. `python/*` is not a pnpm member at all.

### 1.6 Coordination with in-flight specs

Two specs already in flight change what is worth migrating:

- **Spec 069 — decommission the agent-bus and Octi.** `services/agent-bus`
  and `swarm/octi` are being **removed**. Do not invest in registering or
  relocating them — Phase 1 should explicitly skip code marked for deletion.
- **Spec 070 — Chitin Orchestrator (Temporal).** Replaces ~36 cron jobs,
  the lobster dispatch, and much of `swarm/bin` + `swarm/workflows` with
  durable Temporal workflows (a new Go project). The surviving swarm code
  after 070 is the set worth registering under spec 074.

Spec 074 must run **after / alongside** 069 and 070's deletions, not ahead
of them.

---

## 2. What "integrate Go/Python properly" actually means

Nx does not "support languages" — it **orchestrates tasks**. A task is any
command: `go build`, `pytest`, `tsc -b`, `ng build`. A directory becomes a
full monorepo citizen when it has:

1. A **`project.json`** (or `package.json` `nx` field) that gives it a name,
   a `projectType`, `tags`, and `targets`.
2. **Standard target names** — `build`, `test`, `lint`, `validate` — so
   `nx run-many -t test` and `nx affected -t test` hit every language
   uniformly.
3. **Tags** — the repo already has a `lang:*` / `type:*` / `scope:*` /
   `layer:*` taxonomy (enforced by `tools/lint/layer-tag-coverage.ts` and
   `eslint.config.mjs` boundary rules). Every project, Go and Python
   included, should carry the full tag set.

None of this requires moving a single file. **Registration is independent
of folder location.** That is the key fact behind the recommendation.

---

## 3. Layout options considered

### Option A — Language-first, formally blessed
Keep `go/` and `python/` as top-level peers of `apps/` and `libs/`; just
register everything inside them.
- **Pro**: zero file moves; no Go module-path rewrites; no Python import
  churn; lowest risk.
- **Con**: `apps/` and `libs/` stop meaning "all apps" / "all libs" —
  half the apps (the Go binary) live elsewhere. Contradicts the
  `router-plugin-api` precedent. New contributors must learn two axes
  (type *and* language) to find code.

### Option B — Type-first, language as a tag *(recommended)*
Every project lives under `apps/` or `libs/` by *what it is*, regardless of
language. Language is expressed by the `lang:*` tag, and — only for genuine
multi-language domains — a `python/` / `typescript/` / `go/` **subfolder**,
exactly as `libs/router-plugin-api` already does.
- **Pro**: one mental model; `apps/` and `libs/` mean what they say; matches
  the Nx folder-structure guidance and the repo's own precedent; CI and
  tooling globs simplify.
- **Con**: requires moving `go/execution-kernel`, `go/run-sdk`, and
  `python/*`. Moving a Go module changes its module path, which forces a
  one-shot import rewrite (mechanical, gated by `go build && go test`).
  Moving a Python package updates one relative path dependency
  (`argus → analysis`).

### Option C — Domain-first `packages/`
Drop `apps/`/`libs/` entirely; group by domain (`packages/kernel/`,
`packages/telemetry/{go,ts}`, ...).
- **Pro**: maximum cohesion; co-locates everything about one domain.
- **Con**: largest churn of all three; discards the working `apps`/`libs`
  + `layer:*` boundary system; not worth the disruption for a repo this
  size.

### Decision

**Adopt Option B as the destination**, reached through a **staged
migration** so the high-value, low-risk work is not held hostage by the
mechanical file moves:

| Phase | What | Value | Risk |
|-------|------|-------|------|
| **1** | Register every surviving code directory as an Nx project (`project.json` + standard targets + full tags). No file moves. | **High** — `nx affected`, caching, graph, and CI become correct across all three languages. | **Low** |
| **2** | Standardize: one registration mechanism, consistent target names, full `lang:*`/`type:*`/`scope:*`/`layer:*` tags, extend `layer-tag-coverage` enforcement to Go/Python. | Medium — consistency, enforced boundaries. | Low |
| **3** | Relocate `go/*` and `python/*` into `apps/`/`libs/` by type; language becomes a tag/subfolder. One isolated, mechanical PR per project. | Medium — the cosmetic/navigation fix the goal asked for. | Medium (Go module path) |

Phase 1 is the real "integration." Phase 3 is the part the goal *describes*
("Go isn't under apps/libs") — worth doing, but cosmetic, and explicitly
staged last so it can never block Phase 1.

See `.specify/specs/074-polyglot-monorepo-layout/spec.md` for the full
specification, acceptance criteria, and invariants.
