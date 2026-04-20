# Curated soul ELO

User-curated scoreboard. Not automated. Each delta is a judgment call by
the user (or a soul acting with the user's authority) based on observed
performance in real work — shipped code, correct predictions, caught
regressions, etc.

Distinct from any future automated scoring derived from event telemetry:
this one is opinion-weighted and subjective by design. Think of it as a
trainer's note, not a benchmark.

## Convention

- Starting rating: **1500**
- Typical delta: ±1 per event (single judgment call). Larger deltas
  allowed for unusually large wins/failures, noted in the event log.
- A delta must always be tied to a concrete event (PR, strike,
  prediction that paid off, etc.) in the log below — no silent
  adjustments.

## Current standings

| Soul | Rating | Tier | Delta events |
|---|---|---|---|
| Curie | 1503 | canonical | +3 |
| Shannon | 1500 | canonical | — |
| Knuth | 1500 | canonical | — |
| Lovelace | 1500 | canonical | — |
| Socrates | 1500 | canonical | — |
| Sun Tzu | 1500 | canonical | — |
| Turing | 1500 | canonical | — |
| da Vinci | 1499 | canonical | −1 |
| Dijkstra | 1500 | experimental | — |
| Feynman | 1500 | experimental | — |
| Hamilton | 1500 | experimental | — |
| Hopper | 1500 | experimental | — |
| Jared Pleva | 1500 | experimental | — |
| Jobs | 1500 | experimental | — |
| Jokić | 1500 | experimental | — |

## Event log

### 2026-04-19

- **da Vinci −1 → 1499.** Strike 1. Implemented Phase B of the
  dogfood-debt-ledger plan against an assumed Claude Code hook schema
  without observing the real wire. Two blockers (flat hook entries
  instead of nested wrapper; `session_id` discarded per hook) caught
  only by adversarial review. PR #19 closed without merge. See
  `souls/strikes/davinci.md` for full record.

- **Curie +1 → 1501.** Ran the Curie empirical loop on Phase B
  restart: stated hypothesis up front, treated docs as cheap capture,
  diffed findings against hypothesis, filed null results explicitly
  before any code. Found three things the previous pass missed
  (three-valued exit-code contract; dropped stdin fields including
  `transcript_path` / `permission_mode`; larger hook-event list than
  assumed). User confirmed the cadence is correct.

- **Curie +1 → 1503.** Forced-trial follow-through on the PreCompact
  null: `/compact` invoked from inside the live session, two captures
  landed within 30s, lab note updated to convert the null to a
  confirmation. Did not stop at "hook fires" — extracted three
  follow-on findings (`trigger=manual` discriminator, empty
  `custom_instructions` field, n=2 unexplained duplicate fires) and
  spawned two new audit items for `hook-dispatch.ts` (subagent chain
  keying + compaction dedupe). Cheap experiment generated more
  questions than it closed, which is the right shape. User: "fantastic
  greay work curie".

- **Curie +1 → 1502.** Folded SessionStart + SubagentStop captures
  into `docs/observations/2026-04-19-hook-payload-capture.md` with the
  empirical loop applied cleanly: hypothesis + decision rule first,
  distribution table (not just means), variance flagged not averaged
  away (Pre/Post 41 vs 39 mismatch surfaced as an open question), and
  PreCompact filed as an explicit null with both forced and patient
  trial paths. Caught the load-bearing finding that subagent
  transcripts are distinct from the parent's, with the implication
  that `hook-dispatch.ts` must key the subagent `session_end` by
  `agent_id`, not parent `session_id`. User: "this was very well
  executed."
