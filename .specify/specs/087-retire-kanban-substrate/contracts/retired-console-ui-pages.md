# Retired Contract: chitin-console kanban pages

**Surface**: `apps/chitin-console/src/app/pages/{queue,tickets,reports}.page.{ts,html}`
**Status**: RETIRED by spec 087
**FR**: FR-005

## What this contract was

Operator-facing UI pages in the chitin-console app that displayed kanban-board state.
Three primary pages plus supporting shared assets.

| Page | Route | Renders |
|---|---|---|
| `queue.page.ts` | `/queue` (probable) | ready-lane snapshot — what an agent might pick up next |
| `tickets.page.ts` | `/tickets` (probable) | full ticket inventory across boards |
| `reports.page.ts` + `reports.page.html` | `/reports` (probable) | aggregate views over the kanban data |

Shared assets that also surfaced kanban state:

- `api.service.ts` — TypeScript API client; exposed `getKanban*` / `getTickets*` methods.
- `sdlc-diagram.page.ts` — the SDLC pipeline diagram; included a kanban node.
- `overview.page.html` — the dashboard; included a kanban-status section.
- `index.html` — root template; nav link to `/queue` / `/tickets`.
- `README.md` — feature-list mention.

## What replaces them

The chitin-console retains its non-kanban surfaces:

- **sessions** page — primary "what's running" view post-retirement
- **orchestrator-diagram** page — workflow / activity visualization
- argus / sentinel / ELO views — unaffected

These are the surfaces the operator's daily "view the platform" routine uses post-merge
(User Story 2). FR-010 / SC-004 — if any specific kanban view turns out irreplaceable in
daily use, that's a separate (non-blocking) UI gap, not a reason to keep kanban alive.

## What operators see post-retirement

- The kanban-page routes are gone from the app router. Navigating to `/queue` or
  `/tickets` shows the app's not-found page.
- The nav-bar's kanban links are removed.
- The dashboard overview no longer shows a "kanban status" tile.
- The SDLC diagram no longer shows a kanban node.

A new operator reading the operator runbook for "view current work" is pointed at the
sessions / orchestrator pages (FR-010 — active operator docs updated in partition 6).

## Verification at merge

```
# pages gone
test -e apps/chitin-console/src/app/pages/queue.page.ts && echo FAIL || echo PASS
test -e apps/chitin-console/src/app/pages/tickets.page.ts && echo FAIL || echo PASS

# shared assets cleaned
grep -nE 'kanban|board' apps/chitin-console/src/app/api.service.ts | wc -l   # expect 0
grep -nE 'kanban' apps/chitin-console/src/app/pages/sdlc-diagram.page.ts | wc -l   # expect 0
grep -nE 'kanban' apps/chitin-console/src/app/pages/overview.page.html | wc -l   # expect 0
grep -nE 'kanban' apps/chitin-console/src/index.html | wc -l   # expect 0

# app builds + runs
pnpm nx build chitin-console
pnpm nx test chitin-console   # if a test target exists
```
