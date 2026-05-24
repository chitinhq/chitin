# Retired Contract: chitin-console-api kanban routes

**Surface**: HTTP routes in `apps/chitin-console-api/src/server.mjs`
**Status**: RETIRED by spec 087
**FR**: FR-004

## What this contract was

A handful of HTTP routes the console UI called to render kanban-board state. The exact
route list is in `server.mjs` and is small enough that the implementer enumerates and
strips them in one partial-edit commit. Categories (reconstructed from `api.service.ts`
on the UI side):

| Likely route | UI consumer | Purpose |
|---|---|---|
| `GET /api/kanban/boards` | `api.service.ts::getKanbanBoards()` | list available boards |
| `GET /api/kanban/<board>/tickets` | `tickets.page.ts` | full ticket list for a board |
| `GET /api/kanban/<board>/queue` | `queue.page.ts` | ready-lane snapshot |
| `GET /api/kanban/<board>/tickets/<id>` | drill-in | one ticket detail |
| `GET /api/kanban/<board>/reports` | `reports.page.ts` | aggregate reports |

(Approximate — implementer reads the file and strips the actual handlers; this contract
documents the API shape that disappears, not a guaranteed list.)

## What replaces them

The console-API retains its non-kanban routes:

- argus telemetry routes
- ELO rating routes
- gov-decisions query routes
- sessions / orchestrator-diagram routes (the post-kanban visibility surfaces)
- sentinel routes

These are unaffected. The console-API process keeps running with a smaller route table.

## What UI consumers see post-retirement

The UI's kanban pages are deleted in the same PR partition that strips the routes, so the
caller side disappears alongside the callee. No transient state where the UI calls a
404'd route. Per data-model.md "Recommended partition order": 2f (UI pages) before 2e
(API routes), or both in the same commit.

## What external callers see after the retirement

- Anything outside chitin that polled `GET /api/kanban/...` gets a 404. External console
  consumers are not in scope per spec Assumptions; the operator updates them on their own
  schedule.

## Verification at merge

```
grep -nE '/api/kanban|kanban[a-zA-Z]*Routes|app\.(get|post|put|delete).*kanban' \
  apps/chitin-console-api/src/server.mjs | wc -l   # expect 0
grep -rln 'kanban' apps/chitin-console-api/src/ | wc -l   # expect 0
# UI side should be clean too — verified separately in retired-console-ui-pages.md
```
