---
status: superseded
owner: claude-code
kanban: null
implementation_pr: null
superseded_by: docs/decisions/2026-05-06-chitin-scope-narrow-to-kernel.md
effective_from: '2026-05-02'
effective_to: 2026-05-08
reason: 'Scheduling and dispatch live outside chitin per the kernel-narrow

  decision (2026-05-06). The libs/scheduler library, apps/cli scheduler

  subcommands, and the swarm-tunable rank/ingest boundary all moved out

  of chitin''s scope to hermes + the future orchestrator. Chitin owns

  kernel + plugins + data; nothing else.'
---

> **SUPERSEDED 2026-05-08.** Per
> `docs/decisions/2026-05-06-chitin-scope-narrow-to-kernel.md`,
> chitin no longer owns scheduling, dispatch, or work-tracking. The
> `libs/scheduler` library, its CLI subcommands, and the
> Angular dashboard described below are not being built in chitin.
> Retained as historical context for the design moves; do not implement.

# Scheduler — design plan (Nx-shaped, library + Angular dashboard)

**Status:** scoping (pre-implementation). Pins the library API surface, the Angular dashboard, the swarm-tunable boundary, and the Nx project layout BEFORE any code lands so downstream PRs are mechanical.

**Active soul for this design:** da Vinci (architecture / cross-domain; same lens that handled the souls-consolidation plan and Phase D/E/F architecture per quorum 2026-04-19).

## Why this exists

Two compatible goals, both served by the same artifact:

1. **Personal scheduler.** Operator's daily task list + calendar. Voice/chat ingest, time-of-day-aware slotting, Slack/ntfy notifications, local Angular dashboard.
2. **Swarm coordination substrate.** The same ranking primitive that decides "what's the operator's next task" also decides "what's the swarm's next backlog entry to dispatch." Today's `apps/runner/src/dispatcher.ts:pickEntryToDispatch` and a personal scheduler's `next()` are isomorphic — both rank a queue against context to pick a slot.

**Note:** The scheduler is also slated to subsume the swarm backlog. Backlog entries in `docs/swarm-backlog.md` will become scheduler items, retaining fields like `status`, `tier`, `role`, and `blocks`, and gaining scheduling metadata (e.g., deadlines, preferred windows). The dispatcher will eventually read from the scheduler API instead of markdown. The flat-file backlog is interim until this migration completes.

The hard rule: swarm may tune the heuristic, never the kernel. Architecture must enforce that, not by convention.

## Package layout (Nx)

```
libs/scheduler/                          (NEW pure library)
  package.json                           name: @chitin/scheduler
  project.json                           tags: ["layer:scheduler", "scope:lib"]
  src/
    index.ts                             public API barrel
    schema.ts                            Item tagged variant + zod schemas
    rank.ts                              the heuristic — swarm-tunable
    ingest.ts                            text → Item[] via Opus — swarm-tunable
    notify.ts                            ntfy/slack adapter dispatch
    store/
      sqlite.ts                          items.sqlite reads/writes
  tests/

apps/scheduler-dashboard/                (NEW Angular app — local web UI)
  package.json                           name: @chitin/scheduler-dashboard
  project.json                           tags: ["layer:scheduler", "scope:app"]
  angular.json
  src/
    main.ts
    app/
      app.config.ts                      standalone components, RxJS state
      today/                             timeline view
      inbox/                             paste/dictate ingest
      edit/                              item detail + reorder
      shared/services/                   wraps @chitin/scheduler library calls
  e2e/

apps/cli/src/commands/scheduler.ts       (UPDATE — gains scheduler subcommands)
apps/runner/src/dispatcher.ts   (v2 — refactor to consume @chitin/scheduler)
```

Nx project shapes mirror existing chitin libs: `name: @chitin/<package>` in `package.json`, `"main": "./src/index.ts"`, `"type": "module"`, `customConditions: ["chitin"]` resolves source TS in-tree (no build step for in-repo consumers).

## Public API of `@chitin/scheduler`

```ts
// libs/scheduler/src/index.ts
export type { Item, ItemType, WindowPref, Telemetry } from './schema';
export { ItemSchema, ingest } from './ingest';
export { rank, type RankContext, type RankResult } from './rank';
export { notify, type Notifier } from './notify';
export { openStore, type ItemStore } from './store/sqlite';
```

Five functions. Three (`rank`, `ingest`, `notify`) are pure-ish — testable without a database. Two (`openStore`, `ItemStore`) are the persistence boundary.

### Item — tagged variant

```ts
type ItemType = 'task' | 'event' | 'backlog';
type WindowPref = 'morning' | 'deep' | 'shallow' | 'evening' | 'any';

interface ItemBase {
  id: string;
  title: string;
  status: 'open' | 'scheduled' | 'in_progress' | 'completed' | 'cancelled';
  created_at: string;     // RFC3339
  source_url?: string;
  tags?: string[];
}

interface TaskItem extends ItemBase {
  item_type: 'task';
  est_min?: number;
  deadline?: string;
  window_pref?: WindowPref;
  priority?: 1 | 2 | 3 | 4 | 5;
  scheduled_start?: string;  // populated by rank.ts
}

interface EventItem extends ItemBase {
  item_type: 'event';
  start: string;
  duration_min?: number;
  source_calendar?: 'personal' | 'readybench' | 'manual';
}

interface BacklogItem extends ItemBase {
  item_type: 'backlog';
  tier: 'T0' | 'T1' | 'T2' | 'T3' | 'T4' | 'T5';
  blocks?: string[];        // ids of items that must complete first
  file_scope?: string[];    // glob patterns the entry's work touches
  estimated_loc?: number;
}

type Item = TaskItem | EventItem | BacklogItem;
```

Tagged variants with discriminator `item_type`. Zod handles the runtime parse via `z.discriminatedUnion`.

### `rank.next(ctx) → RankResult`

```ts
interface RankContext {
  now: string;                 // RFC3339
  open_items: Item[];          // candidates the ranker considers
  scheduled_events: EventItem[]; // already-blocked windows
  working_hours?: { start_hhmm: string; end_hhmm: string }[];
  window_clock_map: Record<WindowPref, { start_hhmm: string; end_hhmm: string }[]>;
  consumer: 'personal' | 'swarm';  // used for telemetry shaping
}

interface RankResult {
  ordered: Item[];             // ranked best-first
  slots?: Array<{ item_id: string; start: string; end: string; rationale: string }>;
  telemetry: Telemetry;        // emitted to the chain as item_decision events
}

function rank(ctx: RankContext): RankResult;
```

The function is pure — same input, same output. The swarm tunes it by editing `rank.ts` (gov-rule-restricted), and the bench harness can A/B-test rank versions against captured telemetry.

### `ingest.parse(text) → Item[]`

```ts
function ingest(text: string, opts?: { now?: string; preferred_model?: string }): Promise<Item[]>;
```

Uses Opus (or whatever `opts.preferred_model` configures) with a structured-output prompt. Returns parsed items. Failure modes: returns `[]` and logs a parse-failure telemetry event rather than throwing.

### `notify` and `openStore`

Adapter / persistence boundaries. Notifier registry (ntfy, slack, desktop) and SQLite reads/writes. Schema uniform across consumers.

## Storage

`<chitinDir>/scheduler/items.sqlite` — same parent dir as `gov.db` and `chain_index.sqlite`. Unified state. Schema:

```sql
CREATE TABLE items (
  id TEXT PRIMARY KEY,
  item_type TEXT NOT NULL,        -- 'task' | 'event' | 'backlog'
  title TEXT NOT NULL,
  status TEXT NOT NULL,
  created_at TEXT NOT NULL,
  source_url TEXT,
  tags TEXT,                      -- JSON array
  -- task fields
  est_min INTEGER,
  deadline TEXT,
  window_pref TEXT,
  priority INTEGER,
  scheduled_start TEXT,
  -- event fields
  event_start TEXT,
  duration_min INTEGER,
  source_calendar TEXT,
  -- backlog fields
  tier TEXT,
  blocks TEXT,                    -- JSON array
  file_scope TEXT,                -- JSON array
  estimated_loc INTEGER
);
CREATE INDEX idx_items_status ON items(status);
CREATE INDEX idx_items_type ON items(item_type);
CREATE INDEX idx_items_deadline ON items(deadline);
```

WAL mode (per the SQLite-WAL fix from #179).

## Angular dashboard wiring

`apps/scheduler-dashboard` is a standalone-component Angular app served on `localhost:3737` in dev. Production build outputs static files to `dist/apps/scheduler-dashboard/` served by a tiny Express process (so the API + static origin match).

Three views:
- **Today** — vertical timeline; events are blocked, tasks are slotted, drag to reorder.
- **Inbox** — large textarea for paste-and-parse; mic button (browser MediaRecorder → backend whisper).
- **Edit** — item detail with field editors and "reschedule" + "complete" actions.

Services in `app/shared/services/` are thin wrappers over `@chitin/scheduler` library calls. The dashboard imports the library directly via the in-tree TS resolution (`customConditions: ["chitin"]`).

API shape between dashboard and backend (a tiny Express in `apps/scheduler-dashboard/server.ts`):

```
POST /api/items/ingest        body: { text }     → Item[]
GET  /api/items?status=open                       → Item[]
PUT  /api/items/:id                               → Item
POST /api/items/:id/complete                      → { ok }
GET  /api/today                                   → { events, ranked_tasks, slots }
POST /api/voice/transcribe    multipart audio     → { text }
```

Localhost-only, no auth (single-user, bound to 127.0.0.1).

## Notifications

ntfy.sh self-hosted on the same box (lean choice from the prior conversation). Slack incoming webhook as alternative behind a flag. WhatsApp deferred.

Dispatch loop:
- `systemd --user` timer fires every 5 min
- Calls `chitin scheduler tick`
- For each item with `scheduled_start` in next 5–15 min: POST to ntfy or Slack
- Idempotent via marker file (one notification per item per slot)

## Multi-consumer story (v2)

Once the personal-scheduler half ships and runs for ~2 weeks, the temporal-worker dispatcher refactors:

```ts
// apps/runner/src/dispatcher.ts (v2)
import { rank } from '@chitin/scheduler';
import { loadBacklogAsItems } from './backlog-source';

const ctx: RankContext = {
  now: new Date().toISOString(),
  open_items: await loadBacklogAsItems(),
  consumer: 'swarm',
  // ... working_hours from operator config, etc.
};
const { ordered } = rank(ctx);
const next = ordered.find(canDispatch);
```

Backlog entries become `Item`s with `item_type: 'backlog'`. The dashboard's Today view gains a "Swarm" tab that shows what the swarm is working on. Same telemetry pipeline, same ranker, same UI.

## Gov rule (the hard rule, enforced architecturally)

```yaml
rules:
  - id: scheduler-heuristic-only
    action: file.write
    effect: deny
    target_regex: '^libs/scheduler/(?!src/(rank|ingest)\.ts$)'
    agent_match: ['claude-code-headless', 'local-*']
    reason: "Swarm may tune rank.ts and ingest.ts only. Schema, store, notify, and the dashboard are operator-authored."
```

Plus the existing `no-governance-self-modification` rule (which already covers `chitin.yaml` itself — closed in #173).

Plus the Nx tag rule:

```jsonc
// .eslintrc.json
{
  "@nx/enforce-module-boundaries": [
    "error",
    {
      "depConstraints": [
        { "sourceTag": "scope:app",   "onlyDependOnLibsWithTags": ["scope:lib"] },
        { "sourceTag": "layer:scheduler", "onlyDependOnLibsWithTags": ["layer:scheduler", "layer:contracts"] }
      ]
    }
  ]
}
```

Two enforcement layers: gov (agent-action) + nx (import graph). They complement each other.

## PR sequence

```
PR-A — design (this doc)                                    THIS PR
       just the plan; no code

PR-B — Nx scaffold + library foundation                     ~400 LOC
       nx generate @nx/js:library scheduler --directory=libs/scheduler
       Item schema + zod + sqlite store + tests
       BLOCKER: also installs @nx/angular as a workspace dep
                (needed for PR-D's Angular scaffold)

PR-C — rank + ingest + notify (the swarm-tunable surface)   ~400 LOC
       rank.ts heuristic v1 (greedy slot-picker)
       ingest.ts (Opus structured-output prompt)
       notify.ts (ntfy + slack adapters)
       item_decision telemetry events to chain
       CLI: chitin scheduler ingest/today/complete

PR-D — Angular dashboard scaffold + Today view              ~600 LOC
       nx generate @nx/angular:application scheduler-dashboard
                   --directory=apps/scheduler-dashboard
       Today, Inbox, Edit views (Inbox + Edit minimal in v1)
       tiny Express API in server.ts
       browser MediaRecorder + backend whisper transcription
       systemd-timer notification dispatch

PR-E — gov rule + Nx tag rule                               ~30 LOC
       chitin.yaml gains the scheduler-heuristic-only rule
       .eslintrc gains the @nx/enforce-module-boundaries entry

PR-F — (v2, deferred) swarm dispatcher consumes scheduler   ~250 LOC
       refactor temporal-worker/dispatcher.ts to call rank.next()
       backlog entries become item_type='backlog'
       dashboard's Swarm tab lights up
```

Total to dogfoodable: PR-B → PR-E (4 PRs of ~400 LOC each, ~10 days solo). PR-F is the unification slice and lands when the personal half has soaked enough that the heuristic is calibrated.

## Risks (flagged, not solved)

1. **@nx/angular installs ~50MB of tooling.** Acceptable, but lock the version (`@nx/angular@^22.0.0` matching existing Nx 22.6.5).
2. **The dashboard's Express server overlaps with kernel emit paths.** A naive `localhost:3737` API write that triggers a chitin event needs to NOT itself be a chitin-governed call (otherwise we recurse). Solution: dashboard writes go directly to sqlite + emit events; they don't go through the `chitin-kernel emit` binary.
3. **Voice transcription latency (~1–2s on 3090) is noticeable.** Acceptable for v1; could swap to a smaller whisper model if needed.
4. **The heuristic IS the hard problem.** v1 is greedy slot-picker — the swarm tunes from there. Don't over-engineer rank.ts in v1; let telemetry drive complexity.
5. **Operator's existing pre-staged WIP across worktrees.** This PR (design only) is safe; PR-B onwards needs the working tree clean before commit.

## Acceptance for this PR (design only)

- [x] Public API of `@chitin/scheduler` pinned (5 functions, Item tagged variant)
- [x] Storage location decided (`<chitinDir>/scheduler/items.sqlite`)
- [x] Dashboard wiring shape pinned (Angular standalone components, localhost Express, three views)
- [x] Multi-consumer story articulated (personal first, swarm v2)
- [x] Gov rule + Nx tag rule pinned
- [x] PR sequence with LOC estimates

## Memory + reference

- The scheduler is downstream of chitin's gov + emit primitives, NOT inside them. The hard rule that swarm doesn't touch the kernel stays enforceable at the package boundary (gov + Nx tag).
- This plan parallels `2026-05-02-souls-consolidation-plan.md` in shape: scoping doc first, implementation PRs after the framework is locked.
- Active soul: da Vinci (per the issue's "this is architecture" framing, same as #25).
