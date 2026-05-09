# Hermes CLI surface for pending-approvals integration

> **Cull note (2026-05-08):** The operator-approval escalation feature this doc supports was built (PRs #380–#396) and then culled (PRs #397–#400). Operator approvals are now handled by hermes' `tools/approval.py`. The hermes CLI surface documented here remains factual; retained for reference.

Date: 2026-05-07
Purpose: confirm the hermes CLI surface required by Tasks 17-19 of the
operator-approval escalation plan
(`docs/superpowers/plans/2026-05-07-operator-approval-escalation.md`)
and the parent design
(`docs/superpowers/specs/2026-05-07-operator-approval-escalation-design.md`,
"Open items"). Investigated against the operator's hermes installation
at `/home/red/.local/bin/hermes` (symlink to
`/home/red/.hermes/hermes-agent/venv/bin/hermes`).

## Headline finding

**`hermes message` does not exist.** The planned CLI shape
(`hermes message send --channel <X> --body <Y> --reply-to-correlation-id <Z>`)
is fictional. Both the spec sketch and the plan code stubs were written
against an assumed surface that hermes never shipped. Tasks 17–19 need
to be redesigned around what hermes actually provides before B1/B2
implementation can start.

```text
$ hermes message --help
hermes: error: argument command: invalid choice: 'message' (choose from
'chat', 'model', 'fallback', 'gateway', 'setup', 'whatsapp', 'slack',
'login', 'logout', 'auth', 'status', 'cron', 'webhook', 'kanban',
'hooks', 'doctor', 'dump', 'debug', 'backup', 'import', 'config',
'pairing', 'skills', 'plugins', 'curator', 'memory', 'tools', 'mcp',
'sessions', 'insights', 'claw', 'version', 'update', 'uninstall',
'acp', 'profile', 'completion', 'dashboard', 'logs')
```

The full hermes top-level subcommand set (verbatim from `hermes --help`):
chat, model, fallback, gateway, setup, whatsapp, slack, login, logout,
auth, status, cron, webhook, kanban, hooks, doctor, dump, debug, backup,
import, config, pairing, skills, plugins, curator, memory, tools, mcp,
sessions, insights, claw, version, update, uninstall, acp, profile,
completion, dashboard, logs.

There is nothing here that says "send a one-shot message to the
operator's whatsapp / slack channel from the CLI." The closest
candidates and what they actually are:

- `hermes chat` — interactive REPL; not a one-shot.
- `hermes whatsapp` — only `Configure WhatsApp and pair via QR code`.
  No subcommand for sending.
- `hermes slack` — only `manifest` (Slack app manifest generator).
- `hermes gateway` — `run / start / stop / restart / status / install /
  uninstall / setup / migrate-legacy`. No send.
- `hermes webhook` — `subscribe / list / remove / test`. Inbound only
  (HTTP webhook → agent activation). Cannot push a one-shot message
  out to whatsapp.
- `hermes kanban` — task board. Has `comment`, `tail`, `watch`,
  `notify-subscribe`. **This is the closest thing hermes has to the
  primitive we want, but it's task-shaped, not channel-message-shaped.**

## `hermes message send` — outbound notify

**Does not exist.** No flag set to copy.

For comparison, the *only* outbound-shaped CLI surfaces in hermes:

### `hermes kanban create` (task creation)

```text
usage: hermes kanban create [-h] [--body BODY] [--assignee ASSIGNEE]
                            [--parent PARENT] [--workspace WORKSPACE]
                            [--tenant TENANT] [--priority PRIORITY] [--triage]
                            [--idempotency-key IDEMPOTENCY_KEY]
                            [--max-runtime MAX_RUNTIME]
                            [--created-by CREATED_BY] [--skill SKILLS]
                            [--json]
                            title
```

Notes:
- `--idempotency-key` is the closest thing to a correlation handle
  hermes exposes on this command. Reusing the same key dedups; we can
  pass the chitin escalation id directly and get idempotency for free.
- `--json` returns a JSON envelope (presumably with the task id) on
  stdout — usable for capturing a stable handle.
- Tasks are not channel messages. Surfacing one to the operator via
  whatsapp/slack requires a separate `hermes kanban notify-subscribe
  --platform whatsapp --chat-id <id> <task_id>` call (see below).

### `hermes kanban comment` (append to a task)

```text
usage: hermes kanban comment [-h] [--author AUTHOR] task_id text [text ...]
```

This is the natural fit for "the kernel's reply / clarification" style
of follow-up message after the initial escalation has been raised.

### `hermes kanban notify-subscribe` (route task events to a chat)

```text
usage: hermes kanban notify-subscribe [-h] --platform PLATFORM --chat-id CHAT_ID
                                      [--thread-id THREAD_ID]
                                      [--user-id USER_ID]
                                      task_id
```

This is what causes a kanban task's events (created, comment, completed,
blocked, etc.) to actually be pushed out to a messaging platform. It is
the bridge from hermes-internal task state to the operator's whatsapp
DM / slack channel. **Without this, `kanban create` is silent —
the operator never sees it on whatsapp.**

### Whatsapp bridge HTTP API (internal, do not use)

The python gateway runs a sidecar `whatsapp-bridge/bridge.js` Node
process bound to `127.0.0.1:3000` exposing `POST /send {chatId,
message, replyTo}` returning `{success, messageId}`, plus
`GET /messages` (destructive long-poll). This is an internal IPC
between python gateway and Baileys; **chitin must not call it** —
it will race the gateway's drain logic and corrupt inbound message
delivery (`/messages` is `splice`-based). Only mentioned here so we
don't accidentally rediscover and use it.

**Verdict:** no native `hermes message send`. To deliver a chitin
escalation to the operator's whatsapp DM, we have to model the
escalation as a **kanban task** and pair it with `notify-subscribe`.
Correlation is achieved via `--idempotency-key=<chitin-escalation-id>`
on create + `--json` to capture the resulting `task_id`.

## Inbound reply listing

Hermes does **not** expose a generic "list messages on a channel since
cursor X" CLI. Specifically:

- `hermes message list` — does not exist.
- `hermes whatsapp tail` — does not exist (whatsapp subcommand has no
  args at all beyond `--help`).
- `hermes gateway` — no message-listing subcommand.
- `hermes logs gateway` — exists, can `--follow` and `--since 1h`, but
  it is a *log* file (free-form text + structured records). Parsing
  inbound messages out of `gateway.log` is not a stable API; any log
  format change in a hermes update will break it.

What hermes **does** offer for poll/subscribe-shaped inbound:

### `hermes kanban tail <task_id>`

```text
usage: hermes kanban tail [-h] [--interval INTERVAL] task_id
```

Follows a single task's event stream (comments, status changes, etc.).
Polling-shaped (the `--interval` flag is the giveaway), but exits
naturally when the task is completed/archived. **This is our inbound
correlation primitive for v1.** When the operator replies to a kanban
task in whatsapp ("approve" / "deny <reason>"), the gateway routes
that reply back as a comment on the task; `tail` surfaces it.

### `hermes kanban watch`

```text
usage: hermes kanban watch [-h] [--assignee ASSIGNEE] [--tenant TENANT]
                           [--kinds KINDS] [--interval INTERVAL]
```

Live-stream of task events, filterable by tenant/assignee/kind.
Polling under the hood (`--interval` default 0.5s). Useful if we're
running a long-lived watcher for *all* outstanding chitin escalations
at once instead of one tail per task.

### `hermes kanban show <task_id> --json`

One-shot snapshot of task + comments + events. Suitable for the
"poll every 30s" cadence the spec assumed (B2 was sketched as a
30-second systemd timer). Cheaper than `tail` for that cadence.

**Verdict:** no native message-list on a channel. `kanban tail` /
`kanban watch` / `kanban show --json` are the available inbound
primitives, all task-scoped (not channel-scoped). This is fine —
*if* outbound is also kanban-shaped, the inbound side is naturally
correlated via `task_id`.

## Operator channel resolution

There is no `default_channel` config key, no `operator_channel`
field, and no CLI command that returns "the operator's primary chat
id." What exists:

### `~/.hermes/config.yaml`

The `whatsapp:`, `slack:`, `telegram:`, `discord:`, `mattermost:`
blocks have per-platform settings (reactions, channel_prompts) but
no concept of a "default channel" the gateway should treat as the
operator's. WhatsApp specifically is `whatsapp: {}` — empty.

### `~/.hermes/channel_directory.json`

Maintained by the gateway, currently:

```json
{
  "updated_at": "2026-05-07T10:02:19.155142",
  "platforms": {
    "whatsapp": [
      {
        "id": "139358870986999@lid",
        "name": "Jared Pleva",
        "type": "dm",
        "thread_id": null
      }
    ],
    "slack": [],
    "telegram": [],
    ...
  }
}
```

This is the **only** place on disk that names the operator's whatsapp
DM. The gateway populates it as channels become known (i.e., once
the operator has sent or received a message). It's a living file, not
stable config. There is no `hermes config get-default-channel` style
CLI — `hermes config show` does not surface a default chat id either.

### Implications

The plan's spec already anticipated this (item 7: operator-managed
config in `~/.chitin/operator.yaml`). Since hermes itself doesn't
expose a default channel, **chitin must own this config**. Concretely,
`~/.chitin/operator.yaml` should declare:

```yaml
hermes_bin: /home/red/.local/bin/hermes
operator_channel:
  platform: whatsapp           # whatsapp | slack | telegram
  chat_id: "139358870986999@lid"
  # thread_id: optional
```

Reading `~/.hermes/channel_directory.json` programmatically as a
*fallback* (if `~/.chitin/operator.yaml` is missing) is plausible but
adds coupling to a hermes implementation detail; recommend not doing
it in v1.

**Verdict:** operator-managed via `~/.chitin/operator.yaml` (per the
spec's component #7). Hermes contributes nothing to channel
resolution; chitin owns it.

## Recommendation for Tasks 17-19

The original plan assumed `hermes message send` and `hermes message
list`. Both are fictional. The recommended redesign maps the same
escalation lifecycle onto kanban tasks. **This is a non-trivial
redesign — flagging for operator confirmation before B1 starts.**

### Task 17 (notifyHermes outbound) — redesign

Replace the planned shell-out:

```go
// PLANNED (does not work — hermes message send doesn't exist):
exec.Command("hermes", "message", "send",
    "--channel", operatorChannelFromConfig(),
    "--body", msg,
    "--reply-to-correlation-id", id,
).Output()
```

With a two-call kanban dance:

```go
// 1. Create a task carrying the escalation, idempotent on chitin id.
out, err := exec.Command("hermes", "kanban", "create",
    "--body", msg,                          // rendered template body
    "--idempotency-key", escalationID,      // 01JCN6X7... — repeatable
    "--tenant", "chitin-pending-approvals", // namespace for kanban watch
    "--created-by", "chitin-kernel",
    "--max-runtime", strconv.Itoa(timeoutSec)+"s",
    "--json",
    fmt.Sprintf("Approval needed: %s", title),
).Output()
// out → {"task_id": "...", ...}

// 2. Subscribe the operator's chat to the task so they actually see it.
exec.Command("hermes", "kanban", "notify-subscribe",
    "--platform", op.Platform,    // whatsapp
    "--chat-id",  op.ChatID,      // from ~/.chitin/operator.yaml
    taskID,
).Run()
```

Persist `task_id` in the `pending_approvals` row (replaces the planned
`notify_msg_id` column — call it `hermes_task_id` instead). The
escalation id is the idempotency key, so re-running notify after a
crash is safe — it returns the existing task id.

Stamp `notify_failed_reason` if either call fails (gateway down,
operator chat unknown, etc.).

### Task 19 (watch-hermes inbound) — redesign

Replace the planned `hermes message list --channel <X> --since
<cursor>` polling. Two viable shapes; recommend **shape A** for v1:

**Shape A: per-tick `hermes kanban show --json` over open rows.**

`chitin-kernel pending watch-hermes` runs on a 30s systemd timer.
Each tick:

1. `SELECT id, hermes_task_id, last_event_seq FROM pending_approvals
   WHERE resolution IS NULL AND hermes_task_id IS NOT NULL`
2. For each row: `hermes kanban show --json <task_id>`, parse comments
   appended since `last_event_seq`.
3. For each new comment from the operator (filter by author /
   platform-source field in the JSON): match the body against the
   `approve` / `deny` grammar, write the resolution.
4. Update `last_event_seq` regardless (so we don't reparse).
5. If the task's status is `archived` / completed without a parsed
   resolution, treat as `resolution=timeout` to be safe.

Pro: stateless cursor (per-row `last_event_seq` lives in the same
sqlite as the row); naturally bounded work per tick; no long-running
process.
Con: O(open_rows) hermes calls per tick. Fine at single-operator scale
(< 10 open at a time).

**Shape B: long-running `hermes kanban watch --tenant
chitin-pending-approvals --kinds comment` subprocess.**

A single watcher daemon parses the streaming output. Lower latency,
single hermes process to babysit, but introduces a new long-running
chitin daemon shape we don't have elsewhere — heavier. Defer to v2 if
30s latency on shape A becomes an issue.

**Recommended: shape A.** Matches the spec's "30s systemd timer"
cadence and avoids a new daemon class.

### Correlation strategies

The spec listed three correlation strategies in priority order. With
the kanban shape:

1. ~~`--reply-to-correlation-id`~~ — does not exist on `kanban create`.
   Closest equivalent is `--idempotency-key` for *create* idempotency,
   not for reply correlation.
2. **`task_id` is the correlation handle.** Inbound replies are
   structurally tied to a task — every comment from the operator
   carries the parent `task_id` in the kanban event stream. **This
   is strictly better than the planned message-id-based correlation:
   we get correlation guaranteed-by-construction instead of by
   convention.**
3. Body-token fallback (`approve <id>`) is no longer needed for
   correlation but stays useful for the "operator opens a fresh
   whatsapp thread instead of replying to the kanban-routed one"
   edge case. Keep it as a parser fallback, not a primary path.

### Operator-channel resolution (Task 17 prerequisite)

Add a load step at notify time:

```go
type OperatorChannel struct {
    Platform string `yaml:"platform"`
    ChatID   string `yaml:"chat_id"`
    ThreadID string `yaml:"thread_id,omitempty"`
}
// Read ~/.chitin/operator.yaml; if missing, return a typed
// OperatorChannelMissing error and stamp notify_failed_reason
// = "operator_channel_unconfigured" so `chitin-kernel pending
// list` can surface the misconfig to the operator.
```

Document the file shape in the plan's Task 7 (operator-config
bootstrap) so it ships before B1 runs in production.

## Gaps that need redesign

1. **The notify shape is a kanban task, not a message.** Operator
   needs to be okay with their pending-approval prompts arriving as
   "a kanban task got assigned to you, here's its body" rather than
   a flat whatsapp DM. With `notify-subscribe`, the experience on
   whatsapp is reasonably close to a DM — the gateway formats task
   events as messages — but it is not identical. Operator
   confirmation needed before we lock this in.

2. **`pending_approvals` schema column rename.** `notify_msg_id` is
   no longer an accurate name — it's a `hermes_task_id`. Rename
   before plan Task 5 (schema) lands; cheaper than a migration after.

3. **`last_event_seq` cursor column is new.** Not in the spec's
   schema sketch. Add to Task 5.

4. **`hermes kanban tail` requires a known task_id.** Not a problem
   for shape A (we look up open rows from sqlite) but worth noting:
   if hermes-gateway is restarted and loses transient kanban state,
   we need to detect "task no longer exists" and stamp the row as
   `notify_failed`. Add to plan Task 19.

5. **Template rendering body length.** `kanban create` accepts a
   `--body` of arbitrary length, but whatsapp delivery via
   `notify-subscribe` will chunk long bodies. Keep notify templates
   short (under ~1000 chars) for legibility on phones. Add a soft
   limit + truncation to `renderTemplate` in Task 17.

6. **Operator hasn't configured `~/.chitin/operator.yaml` yet.** The
   plan's Task 7 (operator-config bootstrap) is now a strict
   prerequisite of Task 17, not just a parallel piece. Sequence
   accordingly.

7. **Idempotency-key is per-create only.** If we re-run notify with
   the same escalation id after the original task was archived (e.g.,
   a stuck row swept by the timeout sweeper), kanban will *return
   the archived task id*, not create a new one. The watch-hermes
   loop must treat an archived task as a terminal state and not try
   to re-poll it. Add to plan Task 19.

8. **No native channel-scoped message listing means the design
   cannot support "operator just types `approve` in whatsapp without
   replying to anything"** without significant extra work (parsing
   `gateway.log` or running a hermes-side webhook subscription).
   Constrain v1 to "operator must reply to the kanban-routed
   notification thread" and document it in the operator runbook.
   Surface to operator before B1 — this is a UX constraint they
   may want changed.
