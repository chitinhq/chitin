# OpenClaw Adapter

**Status:** Investigation phase. Install path verified 2026-04-20 (Task
F1); the four SPIKE questions are answered below by observation (Task
F2). The adapter-implementation design addendum that consumes these
answers lives at
`docs/superpowers/specs/2026-04-20-openclaw-adapter-implementation-design.md`
(Task F3).

Tracked under:

- Plan — `docs/superpowers/plans/2026-04-19-dogfood-debt-ledger.md`, Phase F
- Parent spec — `docs/superpowers/specs/2026-04-19-dogfood-debt-ledger-design.md`, §"openclaw workstream"

## What OpenClaw is

OpenClaw is a local-first, daemon-backed AI gateway published to npm under
the `openclaw` package name. Upstream: <https://github.com/openclaw/openclaw>.

From `npm view openclaw`:

- Package: `openclaw@2026.4.15` (calendar versioning — YYYY.M.patch)
- Description: "Multi-channel AI gateway with extensible messaging integrations"
- Engines: `node >= 22.14.0`
- Single bin: `openclaw` → `openclaw.mjs`

The CLI surface (from `openclaw --help`) exposes daemon-style subcommands
including `gateway`, `daemon`, `hooks`, `plugins`, `sessions`, `agent`,
`system` (events, heartbeat, presence), and `mcp`. The presence of both
`hooks` and `plugins` subcommands is the first positive signal for a
plugin-style adapter strategy; this is confirmed or refuted in Task F2.

## Install path

Verified 2026-04-20 on Linux (this box — see the machine-topology memory;
Node 24.15.0 installed via the user's `vite-plus` Node runtime at
`/home/red/.vite-plus/js_runtime/node/24.15.0`, so no sudo required).

```bash
npm install -g openclaw@latest
```

- Adds ~794 transitive packages; install wall time ~2 minutes on this box.
- Binary is linked into the npm prefix `bin/` (here: `/home/red/.vite-plus/bin/openclaw`).
- No `preinstall`/`install`/`postinstall` hooks in the published package;
  the only lifecycle script is `prepare`, which gates itself on being
  inside a git work tree and so is a no-op for a tarball install.
- **The bare install does not start a daemon.** `openclaw onboard
  --install-daemon` is a separate, explicit step that registers a
  launchd/systemd unit. Phase F does not run that step; the investigation
  uses the CLI against a transient, ad-hoc gateway or not at all.

## Smoke verification

```text
$ which openclaw
/home/red/.vite-plus/bin/openclaw

$ openclaw --version
OpenClaw 2026.4.15 (041266a)
```

`openclaw --help` renders the full command tree listed in "What OpenClaw
is" above. First invocation also wrote a config file at
`~/.openclaw/openclaw.json` with an expected-but-unconfigured
`OLLAMA_CLOUD_API_KEY` slot — i.e. the tool is usable CLI-wise before
any credentials are supplied, which matters for the observability
adapter (we do not need to wire auth to capture events).

## Adapter strategy (Q1: hook-API vs process-level wrap)

**Finding — openclaw has a rich plugin/hook system, but none of its
hook events are session-lifecycle events.** The adapter strategy for
chitin's envelope (session_start/session_end) must therefore be
**process-level wrap for v1**, with **gateway-log tail as an optional
v1.5 upgrade** and **an in-process openclaw plugin as the v2 high-fidelity
option**. See the F3 addendum for the costed recommendation.

Evidence observed 2026-04-20:

- `openclaw hooks list` on a fresh install reports **5 of 5 hooks ready**:
  `boot-md` (event `gateway:startup`), `bootstrap-extra-files` (event
  `agent:bootstrap`), `command-logger` (event `command`), `session-memory`
  (events `command:new`, `command:reset`), and `memory-core-short-term-
  dreaming-cron` (cron). Every one of those event names is a
  command-, bootstrap-, or cron-scoped event, not a session-lifecycle
  event.
- `openclaw hooks info command-logger` shows `Events: command` —
  a single event type. This confirms the hook-event vocabulary is
  CLI-command-scoped, not turn- or session-scoped.
- `openclaw plugins list` reports **58 of 98 plugins loaded** on a bare
  install (stock plugins under
  `/home/red/.vite-plus/js_runtime/node/24.15.0/lib/node_modules/openclaw/dist/extensions`).
- The documented plugin architecture (docs.openclaw.ai/plugins/architecture)
  describes a **44-hook provider runtime**: `normalizeModelId`,
  `resolveDynamicModel`, `wrapStreamFn`, `capabilities`,
  `fetchUsageSnapshot`, etc. These are inference-pipeline hooks for
  model providers — useful for extending inference, not for
  session-lifecycle capture.
- The docs are explicit: "[the documentation] does not describe
  subscription mechanisms for session lifecycle events like
  `session_start`, `session_end`, `user_prompt`, `assistant_turn`,
  `tool_call`, or `tool_result`." Legacy hook names
  (`before_agent_start`, `before_model_resolve`, `before_prompt_build`)
  exist but are turn- or model-scoped, not session-scoped.
- Plugins run **in-process via jiti** with no sandboxing, registering
  capabilities into a central registry and optionally exposing HTTP
  routes via `api.registerHttpRoute(...)`. A chitin adapter written as
  an openclaw plugin would therefore share the gateway process — tight
  coupling, fragile against version bumps.

Strategy comparison (filled in in the F3 addendum; summary here):

| Strategy           | Fidelity | Coupling | v1 effort   | Covers non-CLI sessions? |
| ------------------ | -------- | -------- | ----------- | ------------------------ |
| Process-level wrap | Low      | None     | Trivial     | No (CLI-invoked only)    |
| Gateway-log tail   | Medium   | Log-schema | Moderate  | Yes                      |
| In-process plugin  | High     | Hard to openclaw internals | High | Yes                      |

## Observable streams (Q2: what is produced during a session)

**Finding — the structured, tailable surface is the gateway file log
(JSONL, one entry per line, rolling daily). No session-scoped event
bus exists outside that log.**

Evidence observed 2026-04-20:

- `openclaw logs` is documented as "Tail gateway file logs via RPC".
  The gateway log lives at `/tmp/openclaw/openclaw-YYYY-MM-DD.log` by
  default and rolls daily on the gateway host's timezone. Path and
  level are configurable via `logging.file` and `logging.level` in
  `openclaw.json`.
- The Control UI tails the same file via the gateway RPC method
  `logs.tail`. No separate event bus is documented.
- A second JSONL stream lives at `~/.openclaw/logs/config-audit.jsonl`
  (7 entries on this box, from prior installs). Its schema is rich —
  fields include `ts`, `source`, `event`, `pid`, `argv`, `cwd`,
  `previousHash`, `nextHash`, `gatewayModeBefore`, `gatewayModeAfter`,
  and so on — but its events are **config-write events**, not session
  events. Example event types: `config.write`. Not a session-lifecycle
  source, but confirms openclaw's logging idiom is structured JSONL
  with PID/argv, which is useful for v1.5 log-tail strategy if the
  gateway log follows the same shape.
- State directories under `~/.openclaw/`: `agents/`, `canvas/`,
  `credentials/`, `devices/`, `flows/`, `identity/`, `logs/`,
  `matrix/`, `memory/`, `qqbot/`, `tasks/`, `workspace/`. Session
  transcript files are documented at `~/.openclaw/sessions/` but that
  directory only materialises after the first session runs on this
  box.
- stdout of a CLI invocation is human-oriented TTY output (emoji,
  tables, ANSI), not a structured event stream. `--no-color` strips
  ANSI but does not switch to a structured format.

## Session semantics (Q3: boundaries)

**Finding — sessions are persistent, session-key-identified entities
that live beyond a single CLI invocation. A "session" is not bounded
by CLI-process lifetime; it is bounded by explicit
`/new` / `/reset` (or equivalent RPC), by session-pruning rules, or
by manual `openclaw sessions cleanup`.**

Evidence observed 2026-04-20:

- Documented at docs.openclaw.ai/concepts/session,
  /concepts/session-pruning, and
  /reference/session-management-compaction: sessions persist across
  CLI invocations and span multiple turns. Sessions store transcripts
  plus agent assignment and channel binding.
- `openclaw sessions` is documented as "List stored conversation
  sessions". Subcommand `sessions cleanup` removes expired or
  orphaned entries — session lifetime has an explicit
  pruning/expiration model, not tied to any CLI process.
- A session can be addressed from the CLI via `--session-id`, `--to`
  (session key + delivery), or implicitly routed through an agent.
- The bundled `session-memory` hook subscribes to `command:new` and
  `command:reset` — i.e. the CLI slash commands that *do* correspond
  to session-lifecycle events are modelled as **command events, not
  session-lifecycle events**. This is a near-miss from chitin's point
  of view: the signal exists, but it is addressed as "the user ran
  `/new`" rather than as "session X started". Translating requires
  correlating the command event against session state before vs after.

**Consequence for process-wrap v1:** `chitin run openclaw [args]`
captures the CLI-process lifecycle, which does NOT cleanly map to
session lifecycle. We therefore redefine v1 event semantics:
`session_start` = `cli_invocation_start`, `session_end` =
`cli_invocation_end`. This is explicit in the F3 addendum's
invariant statement.

## Tool-call surface (Q4: where is the decision/execution boundary observable)

**Finding — tool calls are coordinated by the gateway with an explicit
approval system (`exec-approvals.json`); the boundary is externally
observable via gateway log entries and via the
`openclaw exec-policy` / `openclaw approvals` surfaces, but not as a
hook event.**

Evidence observed 2026-04-20:

- `openclaw agent` is documented as "Run one agent turn via the
  Gateway" — tool calls happen inside a turn and are coordinated by
  the gateway.
- Sensitive ops (shell commands, etc.) flow through the approvals
  gate documented by the `openclaw approvals` and `openclaw
  exec-policy` commands. A file `~/.openclaw/exec-approvals.json`
  exists on this box (177 B, pre-populated by the onboarding flow
  that ran before today under the `/home/jared` user — see the
  config-audit.jsonl argv for historical context).
- Documented state directories `flows/` and `tasks/` suggest tool
  calls and durable background operations also leave on-disk records.
- No hook event corresponds to `tool_call_start` or
  `tool_call_end`. Tool-call observability therefore has to come from
  gateway log tail (if logged at that level) or from a plugin that
  wraps the gateway's tool-dispatch internals.

**v1 scope decision: tool-call capture is out of scope for Phase F.**
The Phase 1.5 minimum is `session_start`/`session_end` only, and even
at that we are capturing CLI-process start/end (see Q3). Tool-call
chain parity stays deferred to the Phase 2 work named in the parent
spec.

## Phase 1.5 minimum (target)

`chitin run openclaw [args]` emits a `session_start` on the wrapped
CLI process's spawn and a `session_end` on the wrapped CLI process's
exit (including error exit). No inner events. Per the Q3 finding above,
these events capture **CLI-invocation lifecycle**, not openclaw's
own persistent-session lifecycle — this is a deliberate v1 scope
reduction, not a semantic mismatch, and is recorded as the
one-sentence invariant in the F3 addendum.

The known coverage gap is explicit: sessions driven by another openclaw
CLI invocation, by the daemon after `onboard --install-daemon`, or by
an inbound chat channel (WhatsApp, Telegram, Signal, …) are invisible
to a process-wrap adapter. Closing that gap requires the v1.5
gateway-log tail or v2 in-process plugin strategies covered in the F3
addendum.

The implementation plan for this minimum is a Phase F5 deliverable and
is conditional on the Task F4 cost gate passing (≤5 elapsed days of
estimated work); if it does not pass, Phase F is split into a follow-up
plan and the minimum is deferred.
