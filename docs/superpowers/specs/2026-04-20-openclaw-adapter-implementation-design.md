# openclaw Adapter Implementation — Design Addendum

**Date:** 2026-04-20
**Supplements:** `docs/superpowers/specs/2026-04-19-dogfood-debt-ledger-design.md` (Phase F).
**Upstream observation:** `libs/adapters/openclaw/README.md` (Phase F Tasks F1+F2).
**Status:** Ready for a follow-up implementation plan. Socrates gate
(Phase F Task F4) evaluation below.

## One-sentence invariant (Knuth gate)

Every `chitin run openclaw <args>` invocation that the wrapper
successfully spawns emits exactly one `session_start` event before the
child's stdio is connected, and exactly one `session_end` event after
the child has been reaped (normal exit, non-zero exit, or killed by
signal), both events sharing the same `chain_id`; the only failure
mode under which `session_end` is not emitted is forcible death of the
wrapper itself (SIGKILL, OOM, or host power loss) — those cases
surface on the next chitin run as an orphaned-session finding, not as
a silent event loss.

The invariant is structured so boundary cases are named, not implied:

- Child never spawns (argv invalid, binary missing) → no `session_start`
  emitted; chitin surfaces the spawn failure through its normal error
  path, not through a half-chain.
- Child exits 0 → one `session_start`, one `session_end` with
  `exit_code: 0`.
- Child exits non-zero → one `session_start`, one `session_end` with
  `exit_code: N`.
- Child killed by signal → one `session_start`, one `session_end` with
  `exit_code: null`, `signal: SIGTERM` (etc.).
- Wrapper itself killed abruptly → one `session_start` with no
  matching `session_end`. Correctness is preserved because the
  unmatched event is detectable on the next chitin run.

## Adapter strategy

**v1: process-level wrap.** `chitin run openclaw [args...]` spawns
openclaw as a child process, wires stdio through, and emits
`session_start` / `session_end` bracketing the child's lifetime.

This strategy was chosen on the basis of three F2 findings:

1. openclaw's hook event vocabulary is command-, bootstrap-, cron-,
   and provider-runtime-scoped; it has no session-lifecycle hook
   events. An openclaw plugin is therefore the *only* path to
   high-fidelity session events, and plugins run in-process with the
   gateway — high coupling and a security posture inconsistent with
   chitin's minimal-trust capture.
2. openclaw's only structured, documented event surface is the
   gateway file log (JSONL, tailable via `openclaw logs` RPC). Log
   tail is a viable v1.5 upgrade but is coupled to an undocumented
   log-field schema and requires the gateway to be running — neither
   fits v1's "ship something that works on a bare install" target.
3. Process-wrap matches the bar set by the parent spec and the
   pre-existing SPIKE (Phase 1.5 minimum) and costs the least to
   ship.

The known v1 coverage gap is explicit and inherited from Q3 in the
README: `session_start` / `session_end` in v1 capture the wrapper's
child-process lifetime, not openclaw's own persistent-session
lifecycle. A user who drives openclaw through the systemd/launchd
daemon (post `openclaw onboard --install-daemon`) or through an
inbound chat channel (WhatsApp, Telegram, Signal, Matrix, …) will not
produce chitin events via process-wrap. Closing that gap is a v1.5 or
v2 upgrade (below); it is not a v1 requirement.

### Future upgrade paths (not in v1 scope)

- **v1.5 — gateway-log tail.** Subscribe to `openclaw logs --follow`
  as a child process, parse JSONL against the actual gateway log
  schema once empirically characterised, emit chitin events on
  session-id appearance/closure. Captures daemon- and channel-backed
  sessions. Depends on `logging.level ≥ info` and on openclaw's log
  schema staying stable across calendar versions.
- **v2 — in-process openclaw plugin.** Ship a chitin-emit plugin
  package, register through openclaw's plugin system, subscribe to
  whichever provider-runtime hooks (`wrapStreamFn`, `beforeAgentStart`,
  etc.) most cleanly bracket a session. Highest fidelity, tightest
  coupling; requires a security review because plugins run in-process
  with the gateway.

## Events emitted (v1 of capture)

- **`session_start`**
  - Emitted: synchronously before `exec()` of the child, after
    argv validation.
  - Payload: `surface: "openclaw"`, `chain_id`, `run_id`, `ts`, and
    `context.argv` (the openclaw subcommand + args, verbatim), plus
    whichever chitin envelope fields are required by the event schema
    at implementation time (`schema_version`, etc.).
- **`session_end`**
  - Emitted: synchronously after `wait()` on the child returns.
  - Payload: same `chain_id` / `run_id` as the preceding
    `session_start`; `exit_code` (integer or null), `signal` (string
    or null), `duration_ms`.
- **No inner events in v1.** `user_prompt`, `assistant_turn`,
  `tool_call`, `tool_result`, etc. are deferred until v1.5 or v2
  provide a viable inner-event source.

## Cost estimate (Socrates gate)

Elapsed-effort estimate: **3 days ± 1 day** (uncertainty range: 2 to 4
elapsed days). Breakdown, in the chitin monorepo's existing cadence:

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

**Gate verdict:** 3 days ± 1 is well under the 5-day threshold
defined in Task F4. The gate passes; a Phase F5 implementation plan
is warranted within this parent plan rather than spun out.

## Out of scope for v1

- Tool-call capture parity with Claude Code. (Phase 2 per the parent
  spec; requires a hook point or log fidelity not available in
  openclaw's v2026.4.15 surface.)
- Daemon- or channel-backed openclaw sessions (require v1.5
  gateway-log tail).
- Correlation between chitin's `chain_id` and openclaw's own session
  key (requires gateway RPC or plugin).
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
