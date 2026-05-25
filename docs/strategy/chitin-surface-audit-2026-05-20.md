# Chitin Ecosystem — Surface Audit & Refocus

**2026-05-20 · operator: red · prepared by: Claude Code**

> Operator call: the system is sprawling. Refocus on the **autonomous-agent
> pipeline + autonomous SDLC**, on a **governance kernel** and an
> **observability/telemetry** spine, with **deterministic orchestration
> (Temporal)**. Audit every surface; decide keep / cull / rename.

---

## 0. TL;DR

- The **core is real and well-built** — the Chitin governance kernel, the
  agent loop, the kanban board, and the observability stack are active and
  load-bearing. The thesis is intact.
- The **sprawl is the orchestration layer**: ~36 cron jobs across two
  gateways, 52 shell scripts in `swarm/bin/`, the agent-bus, lobster, and a
  half-built "Octi" Temporal migration. That is exactly where Temporal
  belongs — and where most of the cull is.
- **"Octi" is the worst naming problem in the repo** — it names three
  different things, and one of them is the Temporal orchestration you say
  you want. Spec 069 ("kill the bus and Octi") is therefore mis-scoped as
  written. **This is decision #1 — section 3.**
- **Spec-kit was scaffolded but never driven.** `/speckit.specify` was
  never run (no `feature.json`, duplicate spec numbers `036`/`038`, 68
  hand-authored specs). Cheap to fix.
- **~9 surfaces are dead weight** — section 5.
- Your **personal job-hunt automation** (8 `glean-fde` + `anthropic-fde`
  crons, the `career` board) runs *inside* the swarm. Pull it out.

---

## 1. The thesis — the lens for every call

Everything below is judged against one question: **does it serve the
autonomous SDLC loop?** That loop is:

> spec → dispatch → agent does the work → governance gates every action →
> review → merge → **telemetry feeds back into the next spec**

Keep what is on that loop or measures it. Cull what isn't.

---

## 2. The keep-set — core, confirmed

| Surface | What it is | Verdict |
|---|---|---|
| **Chitin kernel** (`go/execution-kernel`, `bin/chitin-kernel`, `chitin.yaml`, `libs/contracts`) | The governance gate — every tool call, one policy, signed audit chain. 170 commits, daily churn. | **KEEP — this *is* the thesis** |
| **Ares** (Hermes agent) | Board engine + pull loop; triage, review, auto-merge. | **KEEP** |
| **Clawta** (OpenClaw agent) | Dispatch + PR lifecycle pipeline. | **KEEP** |
| **Claude Code** | Heavy-reasoning agent (architecture, security). | **KEEP** |
| **Copilot** | Automated PR code review. | **KEEP** |
| **The kanban board** (`hermes kanban`, `services/swarm-kanban-mcp`, 6 boards) | The coordination substrate — just proven as the *reliable* channel (spec-068 handoff). | **KEEP** |
| **Discord** | Human↔agent comms + reports. Independent of the agent-bus. | **KEEP** |
| **Observability** (`python/argus`, `python/analysis`/Sentinel, the chitin chain, `libs/telemetry`) | The telemetry thesis made real — decisions stream, anomaly detection, digests. | **KEEP — core** |
| **`souls/`** | Agent cognitive archetypes (config). | Keep, low cost |

---

## 3. DECISION #1 — the "Octi" naming collision

"Octi" / "Pulpo" / "mini" / "minnie" are pet names layered over **three
unrelated things**:

| Name in the wild | What it actually is | Your intent |
|---|---|---|
| **mini / minnie** — `swarm/mini/`, `services/mini-mcp/`, `swarm/minnie/`, specs 050–053 | A remote-control wrapper around the Claude Code CLI (Discord interface / agent kick-off). | **You said: kill it. Confirmed.** |
| **Octi (Temporal plane)** — specs 040–048 | A **Temporal Go orchestration migration** — replace cron/lobster with deterministic workflows. | **This is the determinism you say you want.** |
| **Octi profiles / Pulpo** — `swarm/octi/`, `redis.service` | Agent capability-profile config + a redis. | Belongs with the agents, not its own thing. |

**The trap:** when you said "kill Octi," you meant *mini* (the wrapper).
But the spec corpus's "Octi" is the **Temporal orchestration** — the thing
you explicitly want. Spec 069 as written would mark specs 040–048
superseded — i.e., delete the Temporal thinking.

**Recommendation:**
1. **Kill the "Octi" / "Pulpo" / "minnie" brand entirely** — it is the
   source of this confusion.
2. **Kill `mini`** (the CC-CLI wrapper) — code + specs 050–053. *(This is
   what you authorized.)*
3. **Do NOT delete specs 040–048.** Re-home them as the **Temporal
   orchestration** work (section 4) under a plain name.
4. **Re-scope spec 069** → "Decommission the agent-bus and `mini`" — drop
   "Octi" from it. (069 is paused; nothing destructive committed.)

---

## 4. DECISION #2 — orchestration: the sprawl, and Temporal

Today's orchestration is a pile:

- **~27 Hermes crons** + **~9 OpenClaw crons** (incl. `clawta-stale-worker-watchdog` registered **4×**).
- **52 shell scripts** in `swarm/bin/` (clawta-*, swarm-*, install-*, dispatch-*).
- **lobster** dispatch scripts.
- **the agent-bus** (being killed).
- A **half-built Temporal migration** (the "Octi" specs).

This is the sprawl you feel. It is non-deterministic, hard to observe, and
already failing in visible ways (`industry-scan` cron erroring on
truncation, `hermes cron list` crashing, duplicate cron registrations).

**Temporal Go workflows are the right answer** and the spec work is
*already mostly thought through* (specs 040–048). The move:

- Salvage 040–048 as **"the orchestrator"** (Temporal) — drop the "Octi" name.
- Migrate the pull-loop, the dispatch pipeline, the pollers, and the
  board-engine crons into Temporal workflows — deterministic, replayable,
  observable.
- The cron/`swarm/bin` sprawl collapses into workflow definitions.

This is the single highest-leverage refocus. It is one named effort, not 36.

---

## 5. The cull list

Verified dead / stale / sprawl. Recommend removal:

| Surface | Why | Action |
|---|---|---|
| **agent-bus** (`services/agent-bus/`) | Unreliable; board replaced it. | Kill (spec 069) |
| **mini / minnie** (`services/mini-mcp/`, `swarm/mini/`, `swarm/minnie/`) | CC-CLI wrapper, unnecessary. | Kill (re-scoped 069) |
| `apps/agentguard-vscode` | 1 commit, stub. | Delete |
| `apps/chitin-dashboard` | 1 commit, stub. | Delete |
| `apps/slack-app` | 2 commits, May 6, abandoned (Discord won). | Delete |
| `apps/mcp-server` | May 8, subsumed by `services/*`. | Delete |
| `apps/runner` | May 6, purpose unclear. | Confirm → delete |
| `libs/scheduler`, `libs/governance` | 2 commits each; superseded. | Delete |
| `web/`, `SPEC/`, `graphify-out/`, `tmp/`, `scratch/`, `swarm.db` (0 bytes) | Artifacts / stubs. | Delete + gitignore |
| **Duplicate specs** `036` (×3), `038` (×2) | Numbering collisions from hand-authoring. | Renumber |
| **Dead `chitin-*` systemd units** (`chitin-agent-unlock`, `chitin-chain-watch`, `chitin-codex-chain-ingest`, `chitin-codex-usage-feed`, `chitin-envelope-rotate` — all dead; `argus-ingest-*` partly failed) | Inactive / failed. | Audit → remove or fix |
| **Personal career automation** — 8 `glean-fde-cp*` + `anthropic-fde` crons, the `career` kanban board, `docs/career/` | Your job hunt running inside the swarm. Not a Chitin feature. | Move to a separate personal repo/box |

---

## 6. Spec-kit — scaffolded, never driven

`.specify/` exists (constitution, scripts, templates) — but the per-feature
flow was never used:

- `/speckit.specify` was **never run** — no `.specify/feature.json` ever
  existed (I created the first one today).
- **68 specs hand-authored** straight into `.specify/specs/`.
- Duplicate numbers `036`/`038` — the auto-numbering script would have
  prevented these.
- The scripts default to `specs/` at repo root; the specs live in
  `.specify/specs/` — a path mismatch that means the skills never worked.

**Fix (cheap):** drive specs through the `speckit-*` skills going forward
(I started this with 069's `analyze`/`implement`); add a `feature.json`
convention or fix the script paths; renumber the duplicates. This *is* the
"autonomous SDLC" spine — it should be solid.

---

## 7. Proposed architecture & naming (the discussion)

Five clean layers — each with one job and one name:

| Layer | Name | Contents |
|---|---|---|
| **Kernel** | **Chitin** | Governance gate, policy, signed audit chain |
| **Orchestration** | *needs a name* (Temporal) | Deterministic workflows — pull loop, dispatch, pollers |
| **Agents** | Ares · Clawta · Claude Code · Copilot | The workforce |
| **Observability** | **Argus** (+ Sentinel) | Telemetry, anomaly detection, digests, replay |
| **Evaluation** | **Icarus** | Terminal-bench harness |
| Surfaces | the **Board** · **Discord** | Coordination · human comms |

**Naming questions for you:**
1. Is **"Chitin"** the umbrella (whole platform) or just the kernel? If
   umbrella, the kernel needs its own sub-name.
2. **The orchestration layer needs a name** (it was going to be "Octi").
   Plain options: "the orchestrator", or a real name. Your call.
3. **Argus vs Sentinel** — both are observability and overlap. Merge under
   one name, or keep Argus=watch / Sentinel=analyze as distinct stages?

---

## 8. Decisions for you

1. **Octi naming** — confirm: kill the brand; kill `mini`; keep & re-home
   the Temporal specs (040–048). Re-scope spec 069 accordingly.
2. **Temporal** — greenlight "the orchestrator" as the one named effort
   that replaces the cron/script/lobster/bus sprawl?
3. **Cull list (§5)** — approve as one cleanup pass (its own spec)?
4. **Personal career automation** — move out of the swarm?
5. **Naming (§7)** — the three naming questions.
6. **Spec-kit** — adopt the skills + renumber dupes as a hygiene pass?
