# OpenClaw Adapter

**Status:** Investigation phase. Install path verified 2026-04-20; the four
SPIKE questions that drive adapter strategy are open until Phase F Task F2.

Tracked under:

- Plan — `docs/superpowers/plans/2026-04-19-dogfood-debt-ledger.md`, Phase F
- Parent spec — `docs/superpowers/specs/2026-04-19-dogfood-debt-ledger-design.md`, §"openclaw workstream"

The remaining sections are written in the order F1 → F2 → F3 asks them to
be filled in. Sections still reading "TBD (Task F2)" are not yet answered
by observation; do not read them as final.

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

## Adapter strategy

TBD (Task F2) — pending the answer to "Question 1: plugin/hook API vs
process-level wrap" below. The presence of `openclaw hooks {list, enable,
disable, info}` and `openclaw plugins install` is a strong prior for a
hook-based strategy, but this is not confirmed until F2 observation.

## Observable streams

TBD (Task F2) — pending the answer to "Question 2: streams produced during
a session". Candidate streams to test for: CLI stdout, daemon log files
under `~/.openclaw/`, WebSocket gateway frames, any structured event feed
exposed via `openclaw logs` or `openclaw system events`.

## Session semantics

TBD (Task F2) — pending the answer to "Question 3: session boundaries".
The existence of `openclaw sessions` ("List stored conversation sessions")
implies a persistent, named session concept; F2 determines whether
sessions are per-invocation, per-daemon, or per-user, and whether a session
can span multiple CLI commands.

## Tool-call surface

TBD (Task F2) — pending the answer to "Question 4: tool-call boundaries".
The top-level `agent` command ("Run one agent turn via the Gateway")
suggests a turn-based model. F2 determines whether tool calls inside a
turn are externally observable (hook events, log lines, gateway frames).

## Open SPIKE questions (answered in Task F2)

1. Does OpenClaw expose a hook/plugin API, or do we wrap its CLI at the
   process level?
2. What logs/streams does it produce during a session (stdout, structured
   log file, API events)?
3. How does it identify a "session" and its boundaries?
4. Does it support tool calls, and if so, where is the decision/execution
   boundary observable?

## Phase 1.5 minimum (target)

`chitin run openclaw [args]` emits a `session_start` on launch and a
`session_end` on exit. No inner events. This proves the envelope carries
surface-neutrally for a third surface even if the content is sparse. The
implementation plan for this minimum is a Phase F5 deliverable and is
conditional on the Task F4 cost gate passing (≤5 elapsed days of
estimated work); if it does not pass, Phase F is split into a follow-up
plan and the minimum is deferred.
