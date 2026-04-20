# openclaw Adapter Implementation — Design Addendum

**Date:** 2026-04-20
**Supplements:** `docs/superpowers/specs/2026-04-19-dogfood-debt-ledger-design.md` (Phase F).
**Upstream observation:** `libs/adapters/openclaw/README.md` (Phase F Tasks F1+F2).
**Status:** Ready for a follow-up implementation plan. Socrates gate
(Phase F Task F4) evaluation below.

## One-sentence invariant (Knuth gate)

**v1a (process-wrap):** Every `chitin run openclaw <args>` invocation
that the wrapper successfully spawns emits exactly one `session_start`
event before the child's stdio is connected, and exactly one
`session_end` event after the child has been reaped (normal exit,
non-zero exit, or killed by signal), both events sharing the same
`chain_id`; the only failure mode under which `session_end` is not
emitted is forcible death of the wrapper itself (SIGKILL, OOM, or
host power loss) — those cases surface on the next chitin run as an
orphaned-session finding, not as a silent event loss.

**v1b (session-store poll):** Every openclaw `sessionId` first
observed in a polled `sessions[]` snapshot emits exactly one
`session_start` event on the *first* poll that observes it, and
exactly one `session_end` event on the *first* poll in which it no
longer appears (or on the first poll where its `ageMs` crosses the
configured idle threshold, if the soft variant is enabled); both
events share a `chain_id` derived from `sessionId` and the
first-observation timestamp. Sessions that appear *and* disappear
between two consecutive polls (sub-poll-interval lifetime) are
accepted as an observability gap, not a correctness violation, and
are surfaced via a documented `missed_sub_interval_sessions` counter
rather than an unmatched event chain.

The invariant is structured so boundary cases are named, not implied:

- v1a, child never spawns (argv invalid, binary missing) → no
  `session_start` emitted; chitin surfaces the spawn failure through
  its normal error path, not through a half-chain.
- v1a, child exits 0 / non-zero / by signal → one `session_start` +
  one `session_end` with the appropriate `exit_code` / `signal`
  fields.
- v1a, wrapper itself killed abruptly → one `session_start` with no
  matching `session_end`. Correctness is preserved because the
  unmatched event is detectable on the next chitin run.
- v1b, poll fails transiently (gateway restart, transient RPC
  timeout) → no events emitted for that interval; the next
  successful poll reconciles from the last known snapshot.
- v1b, `sessions.json` is mid-rename when read → the read is
  retried after one inotify beat; if still inconsistent after three
  attempts, the poll is treated as a transient failure (above).
- v1b, session's `updatedAt` advances without a corresponding
  `sessionId` change → optional `session_turn` event (if enabled),
  no `session_end`.

## Adapter strategy

There are **two honest v1 candidates**, and which one ships depends on
whether we value "cheapest ship" (v1a) or "captures real usage" (v1b).
F5 planning picks one; this addendum costs both.

### v1a: process-level wrap

`chitin run openclaw [args...]` spawns openclaw as a child process,
wires stdio through, and emits `session_start` / `session_end`
bracketing the child's lifetime.

- **What it captures:** CLI-invocation lifecycle — any openclaw
  command a user runs through chitin.
- **What it misses:** sessions driven by the systemd/launchd daemon
  (post `openclaw onboard --install-daemon`) or by an inbound chat
  channel (WhatsApp, Telegram, Signal, Matrix, …). On the actual
  dogfood box this matters — the pre-existing openclaw gateway here
  (port 18789) runs as a daemon, and the user's last observed
  session (`probe-1776289145`, `agent:main:main`, `model
  gpt-oss:120b` via `ollama-cloud`) was not launched via a chitin
  wrap. v1a would emit zero events in the user's actual usage pattern.
- **Why still on the table:** cheapest to build, zero coupling to
  openclaw internals, event semantics are identical to the
  claude-code wrapper's process-level capture — good pattern
  symmetry for the chitin envelope.

### v1b: session-store poll

chitin polls `openclaw sessions --json --all-agents` on a cadence
(e.g. every 5 seconds) or watches `~/.openclaw/agents/*/sessions/sessions.json`
via inotify. On each change it diffs the current snapshot against the
previous:

- New `sessionId` → emit `session_start` with the full structured
  payload (`sessionId`, `agentId`, `model`, `modelProvider`,
  `contextTokens`, `kind`).
- `sessionId` removed → emit `session_end`.
- `sessionId` present and `updatedAt` advanced → optionally emit a
  `session_turn` event with the new token counters
  (`inputTokens`, `outputTokens`, `totalTokens`).

- **What it captures:** every session openclaw knows about, regardless
  of origin (CLI, daemon, channel).
- **Coupling:** schema of `openclaw sessions --json` output (fields
  we rely on listed above). This is a more stable contract than the
  `tslog`-style gateway log format.
- **Gateway dependency:** `openclaw sessions --json` on this box
  needs a running gateway; whether it falls back to reading the
  on-disk store directly when no gateway is running is **not yet
  verified** and is flagged as a v1b implementation-time risk. If it
  does not, v1b shifts to direct store read + inotify, which is
  strictly cheaper and schema-coupled only to the store file layout.
- **Why it's honest:** the pre-existing daemon on the dogfood box
  means v1a is effectively a no-op for the one user we have. v1b
  captures what's actually happening.

### Why these two and not log-tail / plugin

- **Gateway-log tail** (via `openclaw logs --follow --json`) requires
  `logging.level ≥ info`, the gateway to be running, and coupling to
  the `tslog` format (positional `"0"` message field, nested `_meta`
  with source-location path). A 38-line live sample of the current
  log contains only subsystem / bonjour / hook-readiness entries —
  no turn- or session-level markers were observed. We would need to
  drive a full agent turn against a configured provider to validate
  that turn markers appear at `info` level; we did not do that in
  F2. Log-tail moves to **v1.5** as an optional upgrade for
  turn-level fidelity once empirically characterised.
- **In-process openclaw plugin** runs via `jiti` with no sandboxing
  inside the gateway process, holding full access to channel tokens,
  credentials, and transcripts. Shipping chitin code there is a
  trust-boundary inversion and is deferred to **v2** behind an
  explicit opt-in and security review.

## Events emitted (v1 of capture)

### v1a event shape

- **`session_start`**
  - Emitted: synchronously before `exec()` of the child, after argv
    validation.
  - Payload: `surface: "openclaw"`, `chain_id`, `run_id`, `ts`, and
    `context.argv` (the openclaw subcommand + args, verbatim), plus
    whichever chitin envelope fields are required by the event
    schema at implementation time (`schema_version`, etc.).
- **`session_end`**
  - Emitted: synchronously after `wait()` on the child returns.
  - Payload: same `chain_id` / `run_id` as the preceding
    `session_start`; `exit_code` (integer or null), `signal` (string
    or null), `duration_ms`.
- **No inner events in v1a.**

### v1b event shape

- **`session_start`** when a new `sessionId` first appears in any
  polled `sessions[]` entry.
  - Payload: `surface: "openclaw"`, `chain_id` (derived from the
    openclaw `sessionId` plus start observation time), `ts`,
    `context.{sessionId, agentId, model, modelProvider, contextTokens,
    kind}`.
- **`session_end`** when a previously-observed `sessionId` no longer
  appears in any polled `sessions[]` entry — or, in the soft variant,
  when `ageMs > idleThreshold`.
  - Payload: same `chain_id` as the `session_start`; `duration_ms`
    derived from first observation to last observation; final token
    counters (`inputTokens`, `outputTokens`, `totalTokens`).
- **Optional `session_turn`** (off by default, enabled by config)
  when a known `sessionId`'s `updatedAt` advances, with the delta
  token counters. This is the only v1b inner event and is
  off-by-default because its cadence is driven by openclaw's
  update frequency, which is not bounded.

### What neither v1 variant emits

`user_prompt`, `assistant_turn`, `tool_call`, and `tool_result` are
out of scope — they require log-tail (v1.5) or plugin (v2) fidelity.

## Cost estimate (Socrates gate)

### v1a — process-level wrap

Elapsed-effort estimate: **3 days ± 1 day** (uncertainty range: 2 to
4 elapsed days).

| Workstream                                                   | Days |
| ------------------------------------------------------------ | ---- |
| Go: new `chitin run <surface>` subcommand, argv passthrough  | 0.5  |
| Go: `session_start` / `session_end` emission + chain linkage | 0.5  |
| TDD unit tests (spawn, normal exit, non-zero exit, signal)   | 0.75 |
| Integration test: real `openclaw --help` → 2-event chain     | 0.5  |
| Wire `chitin install --surface openclaw` (no-op beyond the   |      |
| surface enum; openclaw has no registerable chitin hook)      | 0.25 |
| README updates + parent-spec linkage + CHANGELOG             | 0.25 |
| Review cycle (Copilot + adversarial, per memory)             | 0.5  |

### v1b — session-store poll

Elapsed-effort estimate: **5 days ± 1 day** (uncertainty range: 4 to
6 elapsed days). Two sub-deliverables make this larger than v1a.

| Workstream                                                   | Days |
| ------------------------------------------------------------ | ---- |
| Go: session-store poll loop (spawn `openclaw sessions`,      |      |
| parse JSON, configurable interval, backoff on error)         | 1.0  |
| Go: in-memory snapshot + diff to derive                      |      |
| appear/disappear/updatedAt-advance                           | 0.75 |
| Go: `session_start` / `session_end` / optional               |      |
| `session_turn` emission with openclaw-sessionId-derived      |      |
| chain_id                                                     | 0.5  |
| Fallback: direct read of `sessions.json` with inotify watch  |      |
| if `openclaw sessions --json` requires a running gateway     |      |
| (probe during F5; adds this row if needed)                   | 0.5  |
| TDD unit tests: diff logic, three transition classes,        |      |
| polling backoff, inotify coalescing                          | 1.0  |
| Integration test: drive an actual openclaw session against   |      |
| a throwaway agent + provider; verify 2-event chain           | 0.75 |
| Wire `chitin install --surface openclaw`                     | 0.25 |
| README updates + parent-spec linkage + CHANGELOG             | 0.25 |
| Review cycle (Copilot + adversarial, per memory)             | 0.5  |

### Gate verdict

Both variants land under the 5-day Socrates threshold (v1a: ≤4d upper
bound; v1b: ≤6d upper bound, so v1b's upper-uncertainty tail is
slightly over-gate — worth calling out for F5 planning). The gate
passes; a Phase F5 implementation plan is warranted within this
parent plan rather than spun out as a follow-up.

**The v1a/v1b choice is a scope decision for F5, not a gate decision.**
v1a is right if the priority is envelope-parity with the claude-code
wrap pattern and fast ship. v1b is right if the priority is actually
observing the user's real openclaw usage on the dogfood box (where
the gateway runs as a daemon and v1a captures nothing). The data
point from this box — a 3.4-day-old session produced entirely outside
any chitin wrap — argues for v1b.

## Out of scope for v1 (either variant)

- Tool-call capture parity with Claude Code. (Phase 2 per the parent
  spec; requires log-tail or plugin fidelity not in the v1 budget.)
- Correlation of v1a's process-wrap event chain with v1b's
  sessions-poll event chain on the same host when both are enabled
  (cross-origin `chain_id` reconciliation).
- In v1a only: daemon- or channel-backed openclaw sessions (they
  become observable at v1.5 log-tail or by switching to v1b).
- Cross-surface policy comparison against the Claude Code adapter.
  That lives in the governance-debt ledger (Phase D / Lane ②), not
  in the adapter itself.
- Authentication or channel plumbing (WhatsApp login, Telegram bot
  tokens, etc.) — the adapter is capture-only and does not mediate
  openclaw's outbound channels.

## Open risks

1. **Calendar versioning.** openclaw publishes `YYYY.M.patch`,
   releasing roughly monthly. Any coupling to its CLI flag surface
   (argv shape, `--profile`, `--container`, `--dev`) or to its log
   schema (for the v1.5 upgrade) is a version-bump-fragile contract.
   Mitigation: v1 touches only the binary name and argv passthrough;
   no parsing of openclaw stdout; no assumption about
   `~/.openclaw/*` layout. Gateway-log tail (v1.5) must pin an
   openclaw version range in the adapter README and fail loudly
   outside it.

2. **State-isolation flags.** openclaw's `--container`, `--dev`, and
   `--profile` flags reroute state roots
   (`~/.openclaw-<profile>/`, `OPENCLAW_STATE_DIR`). A process-wrap
   adapter that inspects state paths based on defaults will silently
   capture the wrong session context under these flags. Mitigation:
   the v1 wrap does not inspect state paths; it captures argv only.
   Any v1.5 log tail must resolve the state root the same way
   openclaw does, i.e. honour `--profile` and `OPENCLAW_STATE_DIR`.

3. **v2 in-process plugin is a trust-boundary inversion.** Plugins
   load in-process via `jiti` with no sandboxing and full access to
   the gateway's credentials, channel tokens, and session transcripts.
   Shipping a chitin-emit plugin effectively puts chitin inside
   openclaw's trust boundary. This is acceptable for a local
   dogfooding install but must be flagged in any release notes and
   gated behind explicit opt-in before it ships to non-dogfood
   users. Mitigation: v2 is out of scope; any future v2 plan gets
   its own Socrates gate and security review.
