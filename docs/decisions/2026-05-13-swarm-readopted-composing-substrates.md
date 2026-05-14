# Swarm re-adopted: chitin composes hermes + openclaw substrates

Status: durable boundary. Supersedes
[`2026-05-06-chitin-scope-narrow-to-kernel.md`](./2026-05-06-chitin-scope-narrow-to-kernel.md)
on the "what lives in the chitin repo" question. Keeps everything that
decision said about the kernel being the single side-effect authority.

Date: 2026-05-13

## The change in one sentence

Chitin owns a swarm — but it builds that swarm by composing two
substrates it does not own: **hermes** (kanban as source of truth) and
**openclaw** (agent runtime + Lobster workflow + agent cards). Chitin's
contribution at every hop is the chain event, the `CHITIN_DRIVER`
identity stamp, and the `gov.Gate` enforcement.

## What 2026-05-06 got right (kept)

- The kernel is the only side-effect authority. `gov.Gate.Evaluate` is
  the only enforcement point. `~/.chitin/*` has one writer.
- `apps/runner/`, `infra/temporal/`, the markdown work-tracking file,
  the chitin-side dispatcher and PR mirror are gone and stay gone.
  Reintroducing any of them would re-create the duplicate-orchestration
  problem the 05-06 cull resolved.
- Layer Contracts v1 (kernel authority, driver constraint, routing
  scope, aggregation role) still hold.

## What changed (and why)

The 05-06 decision said "whatever orchestrator the operator runs is
agnostic — chitin doesn't know or care." That was the right move at
the time because chitin's in-tree orchestration code was duplicating
hermes' kanban without leveraging it. Once that duplication was
deleted, two things became visible:

1. **Hermes kanban is a real substrate, not just another consumer.**
   It exposes a SQLite-backed task graph, status transitions,
   comments, and `task_events`. The chitin-owned audit invariant
   ("every status change has a matching comment + event") rides ON
   TOP of that substrate.

2. **OpenClaw + Lobster + agent cards is a real agent-runtime
   substrate.** Lobster workflows handle multi-step dispatch with
   approval gates; agent cards parameterize `(driver, model, role)`
   triples; the openclaw plugin runtime gates `before_tool_call`
   through `chitin-kernel`. Building a parallel runtime in chitin
   would duplicate that.

The 2026-05-11 spec
([`docs/superpowers/specs/2026-05-11-hermes-clawta-lobster-finish-design.md`](../superpowers/specs/2026-05-11-hermes-clawta-lobster-finish-design.md))
crystallized the four-hop pipeline that emerges when those two
substrates are composed:

```
hermes (kanban substrate)
  → clawta (chitin-owned tick + dispatch wrapper)
    → openclaw / kanban-dispatch.lobster (runtime substrate)
      → frontier-coder CLI (leaf execution under gov.Gate)
```

Every hop emits a chain event keyed by `CHITIN_DRIVER`. Every leaf
tool call is gated by `gov.Gate.Evaluate`. The chain is the unifying
contract.

## The boundary, restated

Chitin owns:

1. **Kernel** — `go/execution-kernel/`. Gate, escalation, lockdown,
   envelope, ingest, OTEL projection. Single side-effect authority.
   Unchanged from 05-06.
2. **Driver integrations** — `internal/driver/{claudecode,codex,
   gemini,hermes,copilot}` (hook normalizers + decision formatters)
   and `apps/openclaw-plugin-governance/` (openclaw plugin shape;
   different by design — plugin runtime, not hook runtime). Plus
   `bin/chitin-router-hook` (the compiled Go shim every PreToolUse-
   class hook points at).
3. **The chain + analysis surface** — `~/.chitin/*`, `python/analysis/*`,
   `internal/ingest/*`. Unchanged from 05-06.
4. **The swarm (new since 05-06)** — `swarm/`:
   - `swarm/bin/` — clawta tick scripts: `clawta-poller` (ready→
     dispatch), `clawta-pr-lifecycle` (PR→kanban reflection),
     `clawta-worker-pool-guard`, `clawta-blocked-escalator`, audit +
     report scripts.
   - `swarm/workflows/` — `kanban-dispatch.lobster` (the four-hop
     pipeline expressed as a Lobster workflow), `_pick_driver.py`
     (operator-side driver picker; see open question below),
     `spawn_worker_subprocess.py`, judge + ELO helpers.
   - `swarm/data/agent-cards/` — git-tracked agent-card source;
     deployed via symlink to `~/.openclaw/data/agent-cards/`.
   - `swarm/roles/` — git-tracked SKILL.md per worker role; deployed
     via symlink to `~/.openclaw/roles/`.
   - `swarm/systemd/` — `clawta-poller.timer`, `swarm-audit.timer`,
     `architecture-audit.timer`. These are operationally chitin-owned
     because they drive chitin-owned scripts; not the "11 timers"
     deleted on 05-06.
   - `swarm/prompts/` — operator-tunable prompts for hermes grooming
     and clawta classification.
5. **Operational scaffolding** — `infra/systemd/` (kernel redeploy,
   agent-unlock, chain-watch, envelope-rotate). Unchanged from 05-06.

Chitin does NOT own:

- The kanban data itself. The SQLite DB lives in hermes; chitin reads
  and writes via `kanban-flow` (which goes through the hermes CLI).
- The openclaw agent runtime, the Lobster workflow engine, or the
  acpx subprocess gateway. These are upstream substrates chitin
  composes.
- Anthropic / OpenAI / Google API auth. Each frontier-coder CLI
  speaks to its own backend under its own auth.
- The chitin-aware bits of hermes itself (e.g.,
  `tools/approval.py`). Those live in hermes.

## What this supersedes from 2026-05-06

The 05-06 decision listed under "what chitin does NOT own":

| 05-06 said | 05-13 reality |
|---|---|
| Work tracking, kanban, board state | Hermes substrate, but chitin's clawta-poller reads kanban + dispatches |
| Dispatch (picking what to run next, spawning runners) | `swarm/workflows/kanban-dispatch.lobster` + `clawta-poller` do this; they compose openclaw, they don't reimplement it |
| Scheduling (when to run, how often, in what order) | `swarm/systemd/clawta-poller.timer` fires the tick |
| Workflow definitions | `kanban-dispatch.lobster` IS a workflow definition, expressed in Lobster (substrate-native), not chitin TS |
| PR-merge → status-flip pipelines | `clawta-pr-lifecycle` does this — reading PR state and writing kanban via `kanban-flow` |
| Mirroring between work-tracking surfaces | N/A — single substrate (hermes kanban), no mirror |
| "Don't add hermes-aware features to chitin" | Reversed: chitin IS hermes-aware by design now |

Everything else from 05-06 stands.

## Why the swarm lives in this repo (not hermes / not openclaw)

The chitin contribution at each hop is what unifies the pipeline:

- The chain event schema (`bounded_context_v1` + `chitin.driver`
  identity stamping) is chitin's contract. The swarm scripts that
  emit those events need to live alongside the schema.
- `gov.Gate` policy authoring (`chitin.yaml` driver-keyed rules) is
  chitin's contract. Rules that gate the swarm's hops live in the
  same repo as the rules engine.
- The `chitin-router-hook` Go binary is shared across all four
  frontier CLIs. The swarm dispatches to those CLIs and expects
  inner-hop events with `driver=<cli>`. The driver identity
  guarantee comes from a binary chitin compiles.
- Audit invariants ("every status change has a matching
  `task_events` row + comment") are enforced by `kanban-flow`, which
  is a chitin chokepoint over a hermes-owned database.

Putting the swarm in hermes would require shipping chitin's chain +
policy + router-hook contract surface there. Putting it in openclaw
would require shipping hermes' kanban contract there. The composition
point is the natural home.

## Things NOT to do (durable warnings, updated)

- **Don't move kanban data into chitin.** The kanban DB stays in
  hermes. Chitin reads/writes via `kanban-flow` only.
- **Don't add a second source-of-truth for ticket state.** The
  hermes kanban + `task_events` table is canonical; chitin's chain
  is canonical for execution events. Don't try to denormalize one
  into the other.
- **Don't reintroduce `apps/runner/`** or any chitin-side Temporal
  worker. The Lobster workflow + clawta-poller subprocess pair
  replaces all of it.
- **Don't add a TypeScript "kanban adapter" library.** `kanban-flow`
  (bash) is the boundary; the substrate is the SQLite DB underneath.
  Wrapping it in TS adds a layer without value.
- **Don't fork driver-picking into chitin.** `_pick_driver.py` lives
  operator-side (under `swarm/workflows/`) because the picking heuristic
  is fast-changing operational logic. See open question below.

## Open questions surfaced by this change

1. **`AllowedDrivers` as active kernel primitive.** Layer Contract v1
   #2 says the kernel exposes `AllowedDrivers(req)` and the
   orchestrator must consume it. Today: passive (a schema field on
   `ExecutionRequest`); the Lobster `pick_driver` step ranks by cost
   from agent cards. The 04-30 addendum deferred the active primitive
   ("DEFERRED — not in slice 1/2 ... chitin-kernel task validate").
   With the swarm now in-tree, the orchestrator (`_pick_driver.py`)
   and the kernel live one step apart — close enough to wire the
   active primitive cleanly. Either ship it or update the contract.

2. **Conformance test coverage for the four-hop chain.** The 05-11
   spec calls for per-CLI tests that assert `(chain shows
   allowed:false) AND (leaf CLI did not execute the tool)`. Until
   those land, the enforcement invariant is asserted but not proven.

3. **`swarm/data/agent-cards/` → `~/.openclaw/data/agent-cards/` sync.**
   Today: operator-side symlink. Make this an idempotent install
   step in a `scripts/install-swarm.sh` so a fresh box reaches the
   same state.

## References

- Predecessor: [`2026-05-06-chitin-scope-narrow-to-kernel.md`](./2026-05-06-chitin-scope-narrow-to-kernel.md)
  — established kernel single-writer + deleted `apps/runner/`. Still
  load-bearing for everything except the "no orchestration in chitin"
  exclusion.
- Architectural shape: [`docs/superpowers/specs/2026-05-11-hermes-clawta-lobster-finish-design.md`](../superpowers/specs/2026-05-11-hermes-clawta-lobster-finish-design.md)
  — the four-hop pipeline.
- Observability shape: [`docs/superpowers/specs/2026-05-12-swarm-observability-via-chitin-cli.md`](../superpowers/specs/2026-05-12-swarm-observability-via-chitin-cli.md)
  — chitin-kernel CLI as the canonical observability plane.
- State machine: [`docs/runbooks/swarm-sdlc-status-machine.md`](../runbooks/swarm-sdlc-status-machine.md)
  — the kanban states + transitions the swarm walks.
- Layer Contracts: [`docs/architecture/layer-contracts.md`](../architecture/layer-contracts.md)
  — unchanged by this decision; the four invariants still hold.
