# Chitin Console

Angular operations console for the chitin/hermes execution kernel. Read-only,
localhost-bound. Aggregates the hermes kanban, the chain ledger (gov-decisions),
the openclaw ELO + clawta dispatch tables, the argus observatory index, and the
local policy file into a single dashboard.

## Surfaces

| Route | Purpose |
| --- | --- |
| `/overview` | KPI strip (ready, in-flight, triage, done 7d, p50 age, success 7d) + live runs · 24h + recent sessions + cost histogram + swarm ELO top 8 |
| `/sessions` | Chain sessions list aggregated from `~/.chitin/gov-decisions-*.jsonl` (driver, events, allow/deny, cost, ts range) |
| `/sessions/:chainId` | Chrome-devtools-style timeline of one chain, stacked by action_type, with color-coded allow/deny/heuristic cells and click-to-inspect drawer; top tools + top rules; cost rollup |
| `/tickets` | Dense filterable ticket table (status, assignee, full-text); click a row to open the detail drawer with body, runs, clawta dispatch decisions, and raw kanban events |
| `/elo` | Sortable swarm ELO leaderboard from `~/.openclaw/data/clawta.db:swarm_elo`, filterable by class / driver, with elo distribution bar |
| `/argus` | Status of `~/.argus/index.db` (table/row counts) and latest findings (gracefully degrades when the findings table is absent) |
| `/policy` | Read-only line-numbered view of `chitin.yaml` with simple syntax highlighting. Edit + adopt flow ships with slice 6 of the dashboard epic |
| `/suggestions` | Placeholder for the analyzer cron (slice 5). Surfaces the planned rubric so the page is informative pre-ship |

## Coverage against the dashboard epic (`t_8f4d2ee5`)

- ✅ Slice 3 (Dashboard MVP): session list + timeline view + ELO board + policy read view shipped here in Angular/Nx instead of the spec's Vite/React.
- ✅ Slice 4 (cost vis): per-session cost rollup KPI + 24h cost histogram on overview. Stacked-area is a follow-up.
- ⏳ Slice 1 (capture extension): the prompt/thinking/I/O sidecar is a kernel-side feature; the console renders any extra fields that show up on chain events but doesn't capture them.
- ⏳ Slice 2 (replay API in Go): the JS API server here does the same join (ledger ⨝ blobs), but written in Node so the dashboard can stand alone. Trivial to swap for the eventual Go `chitin chain replay` JSON output.
- ⏳ Slice 5 (analyzer cron): `/suggestions` is wired to read from a future `analyzer_suggestions` table; rubric is surfaced today.
- ⏳ Slice 6 (policy composer): policy view is read-only; the editable form + auto-PR adoption flow is not yet built.

## Layout

```
apps/
  chitin-console/        # Angular standalone app (Nx)
    src/app/
      api.service.ts     # typed HttpClient wrapper over /api
      api.types.ts       # response shapes
      app.{ts,html,css}  # shell: topbar + nav + router-outlet
      app.routes.ts      # lazy routes, one chunk per page
      pages/             # one component per route (standalone)
      ui/                # status pill, kpi card, sparkbar, loader, empty state
      utils.ts           # age/usd/pct/ts/ulid helpers
    proxy.conf.json      # /api → http://127.0.0.1:7878
  chitin-console-api/    # node http server (no deps beyond better-sqlite3)
    src/server.mjs       # opens kanban.db, clawta.db, argus index, ledger files
```

## Data sources

| Source | Path | Used by |
| --- | --- | --- |
| Hermes kanban | `~/.hermes/kanban/boards/<board>/kanban.db` | tickets, runs, events, KPIs |
| Chain ledger | `~/.chitin/gov-decisions-YYYY-MM-DD.jsonl` | sessions, session detail, cost histogram |
| Swarm ELO | `~/.openclaw/data/clawta.db` (`swarm_elo`) | ELO leaderboard, overview top-8 |
| Clawta dispatch | `~/.openclaw/data/clawta_decisions.db` (`clawta_decisions`) | per-ticket "why this driver" panel |
| Argus index | `~/.argus/index.db` | argus status, findings |
| Policy | `<repo>/chitin.yaml` (or `$CHITIN_POLICY_FILE`) | policy view |

The chain ledger key is auto-detected: `chain_id` → `session_id` → `envelope_id`.
Current chitin emits `envelope_id`, so sessions are grouped by envelope.

## Run locally

```bash
# 1. start the API (read-only, localhost-only)
node apps/chitin-console-api/src/server.mjs
# → http://127.0.0.1:7878

# 2. in another shell, start the Angular dev server
NX_IGNORE_UNSUPPORTED_TS_SETUP=true pnpm nx serve chitin-console
# → http://127.0.0.1:4200  (proxies /api → 7878)
```

Optional env:

| var | default |
| --- | --- |
| `CHITIN_CONSOLE_PORT` | 7878 |
| `CHITIN_CONSOLE_HOST` | 127.0.0.1 |
| `HERMES_KANBAN_ROOT` | `~/.hermes/kanban` |
| `ARGUS_INDEX_DB` | `~/.argus/index.db` |
| `CLAWTA_DB` | `~/.openclaw/data/clawta.db` |
| `CLAWTA_DECISIONS_DB` | `~/.openclaw/data/clawta_decisions.db` |
| `CHITIN_POLICY_FILE` | `<repo>/chitin.yaml` |
| `CHITIN_REPO_ROOT` | inferred from server.mjs path |

## Build a production bundle

```bash
NX_IGNORE_UNSUPPORTED_TS_SETUP=true pnpm nx build chitin-console
# → dist/apps/chitin-console/browser
```

## Theme

The palette + typography mirror `chitin/web/index.html`:

- bg `#0A0E15` · panel `#11161F` · plate `#161D29`
- accent chitin-amber `#D4A574` · glow `#F5C088`
- run-green `#22C55E` · bone `#F5F5F0` · muted `#94A3B8`
- mono Space Mono · display Space Grotesk
- "exoskeleton segments": every panel is a `.plate` with a bevelled
  amber-line top-right corner accent and a subtle inner highlight.

Animations cap at 200–300 ms and respect `prefers-reduced-motion`. Focus
rings are amber and visible. Z-index uses a fixed scale
(`--z-sticky:10`, `--z-overlay:30`, `--z-drawer:40`, `--z-modal:50`)
so drawers/modals don't collide with the sticky topbar.

## What's intentionally not done yet

- No write-back. Every endpoint opens DBs read-only and the policy view is
  display-only. The chain-replay preview, adoption flow, and analyzer-side
  table writes are out of scope until kernel slices 1, 5, 6 land.
- No event capture for tool I/O / prompts / thinking. The console renders
  whatever fields the ledger has today; once slice 1 ships the sidecar, the
  drawer can render prompt + thinking + tool I/O by event id.
- No live streaming. The overview polls health every 15s; tables are refresh-
  on-navigation. WebSocket streaming is a slice 6+ follow-up.
