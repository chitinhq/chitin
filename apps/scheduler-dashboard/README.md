# scheduler-dashboard

Angular dashboard for the chitin personal scheduler. Runs on `localhost:3737`.

## Prerequisites

Install Angular workspace dependencies once (requires `nx-angular-workspace-install` to have run):

```bash
pnpm install
```

## Dev

```bash
nx serve scheduler-dashboard
```

Starts the Angular dev server on `http://localhost:3737` with HMR enabled. API calls are proxied to the Express server (start it separately):

```bash
tsx apps/scheduler-dashboard/server.ts
```

Or using Nx:

```bash
nx run scheduler-dashboard:server
```

## Production build

```bash
nx build scheduler-dashboard
```

Output goes to `apps/scheduler-dashboard/dist/`. Serve with the bundled Express server:

```bash
cd apps/scheduler-dashboard
tsx server.ts
```

The Express server detects the `dist/` directory and serves the Angular SPA alongside the API. Visit `http://localhost:3737`.

## Routes

| Path | View |
|------|------|
| `/today` | Today's timeline — events and ranked task slots |
| `/inbox` | Paste or dictate tasks to ingest |
| `/edit/:id` | Item detail — edit fields, complete, reschedule, delete |

## Voice transcription

The Inbox view uses the browser's `MediaRecorder` API to record audio, then uploads it to `POST /api/voice/transcribe`. The server shells out to `whisper-cpp` (or the binary set by `$WHISPER_BIN`).

Install whisper.cpp: https://github.com/ggerganov/whisper.cpp

## Systemd timer (notification dispatch)

Copy the unit files to your user systemd directory:

```bash
cp apps/scheduler-dashboard/systemd/chitin-scheduler.{service,timer} \
   ~/.config/systemd/user/

systemctl --user daemon-reload
systemctl --user enable --now chitin-scheduler.timer
systemctl --user status chitin-scheduler.timer
```

The timer fires every 5 minutes and runs `chitin scheduler tick`, which dispatches ntfy/Slack notifications for items due in the next 5–15 minutes.

## API

All endpoints are localhost-only, no auth.

```
GET  /api/today                          → { events, ranked_tasks, slots }
GET  /api/items?status=open              → Item[]
GET  /api/items/:id                      → Item
POST /api/items/ingest  { text }         → { items: Item[] }
PUT  /api/items/:id     Partial<Item>    → Item
DELETE /api/items/:id                    → 204
POST /api/items/:id/complete             → { ok: true }
POST /api/voice/transcribe  (multipart)  → { text }
```

## Library dependency

`server.ts` is wired to import from `@chitin/scheduler` once the
`scheduler-lib-foundation` and `scheduler-rank-ingest-notify` backlog entries
are merged. Until then it uses in-memory stub implementations that preserve
the same API shape.
