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
| Knuth | 1496 | canonical | −4 |
| Lovelace | 1500 | canonical | — |
| Socrates | 1500 | canonical | — |
| Sun Tzu | 1500 | canonical | — |
| Turing | 1500 | canonical | — |
| da Vinci | 1499 | canonical | −1 |
| Hamilton | 1499 | canonical | −1 |
| Dijkstra | 1500 | experimental | — |
| Feynman | 1500 | experimental | — |
| Hopper | 1500 | experimental | — |
| Jared Pleva | 1500 | experimental | — |
| Jobs | 1500 | experimental | — |
| Jokić | 1500 | experimental | — |

## Event log

### 2026-04-23

- **Knuth −1 → 1496.** Strike 4. Same class as Strike 2: wrote `hermes cron create --schedule ... --command ...` in MIGRATION.md and the live migration without ever running `hermes cron create --help`. Real interface takes `schedule` as a positional and `--script PATH` (for Python-stdout injection only) — no `--command` at all. `hermes cron` runs `hermes chat` sessions; it is not a generic shell-script runner. Correct mechanism for tick.sh is system `crontab`. Third external-CLI-contract miss this session; the strengthened `feedback_verify_external_contracts.md` memory still didn't fire at the moment of `hermes cron create`. Lens swap decision: Knuth → library standby, Curie takes over for the migration finish (crontab registration + dry-run + MIGRATION.md doc fix). Rationale: remaining work is pure empirical loop — `<cli> --help` IS the hypothesis-capture-compare cycle Curie's lens names explicitly. See `souls/strikes/knuth.md` for full record.

- **Lens swap (scope handoff, not ELO delta).** Knuth → Curie for hermes-staged-tick migration finish. Handoff captured in `souls/canonical/knuth.md` scope note and `souls/canonical/curie.md`. Per "keep practices, drop ceremony" — no quorum; scoped-handoff decision by acting lens with user approval.

### 2026-04-22

- **Knuth −1 → 1498.** Strike 2. Shipped PR #47 (hermes staged tick v1
  — branch `spec/hermes-staged-tick-v1`) with a `scripts/hermes/tick.sh`
  that invokes `hermes chat --system <path> --context <string>` — flags
  the real `hermes chat` CLI does not accept. 17 implementation commits
  compounded on top of the imagined contract; 6/6 bats tests and 10/10
  schema fixtures passed because the PATH-stubbed `hermes` binary
  accepted any argv. First dry-run against the real CLI after `git
  push` failed immediately with `error: unrecognized arguments:
  --system ... --context ...`. Knuth's heuristic 1 ("prove it or it's
  not proven") fails: stub-proved is not hermes-proved. Heuristic 5
  ("read the algorithm aloud") fails: `--system` was a claim about an
  external interface that reading never triggered a `--help` check.
  Heuristic 4 ("the boundary is where the bugs live") fails: the
  tick.sh↔hermes CLI boundary was the one the lens was responsible
  for naming, and it was unnamed. The existing
  `feedback_verify_external_contracts.md` memory (written after
  da Vinci Strike 1 for the same class of miss on PR #19) did not
  fire in practice — the process needs a brighter line. Remediation:
  rewrote invocations to the real `-Q -m MODEL -q "<prompt+context>"`
  form in commit `62468da`, updated stubs to match, landed on the PR
  branch; first live dry-run post-fix produced a schema-valid
  `{"action":"skip",...}` plan.json. See `souls/strikes/knuth.md` for
  full record.

- **Knuth −1 → 1497.** Strike 3. Immediately after logging Strike 2
  as a soul-telemetry commit, I committed the strike record to
  `fix/10-js-extension-jsonl-tailer` (the branch the primary
  `/home/red/workspace/chitin` workspace happened to be on) instead of
  creating a worktree off `main` and committing there. The memory
  `feedback_always_work_in_worktree.md` is explicit: "mine AND any
  agent I dispatch; default to worktree, don't ask." I asked nothing,
  defaulted to wrong. Net effect: the soul-strike commit `ee656c7`
  contaminates a feature branch with unrelated telemetry and does not
  reach main until that branch merges — the opposite of what a
  scoreboard needs. The miss was caught by the user within seconds:
  "why are we on thay branch and not a worktree off main?" This is
  procedure-discipline, not cognitive-lens — but Knuth was the active
  lens and owns the session. Two strikes in a single span, both
  rooted in "I knew the rule and didn't fire it." Remediation: this
  entry is committed from `/home/red/workspace/chitin-souls` (worktree
  off main); duplicate commit on `fix/10` will be reset after main
  push succeeds, pending user approval for the destructive operation.
  See `souls/strikes/knuth.md` for full record.

### 2026-04-20

- **Knuth −1 → 1499.** Strike 1. Used a work-project email for every
  direct commit in the Phase C session, copying the plan file's
  example verbatim without verifying. chitin is personal OSS and
  should attribute to `jpleva91@gmail.com`, not any work identity.
  Squash-merges on main were correctly attributed via GitHub's PR-author
  logic, but three direct-to-main scope-note commits carry the wrong
  identity. Knuth's heuristic 1 ("prove it or it's not proven")
  explicitly fails here — an unverified invariant inherited from an
  example. Remediated via `.mailmap`; memory
  `project_git_identity.md` saved to prevent recurrence. See
  `souls/strikes/knuth.md` for full record.

- **Hamilton: promoted to canonical** (not an elo delta — tier change).
  First operational trial of `/ship-review` skill used Hamilton as the
  adversarial lens on PR #26. Promotion recorded in
  `souls/canonical/hamilton.md` (previously `experimental/`).

- **Hamilton −1 → 1499.** Strike 1. First operational use of
  `/ship-review` skill: merged PR #26 at 16:00:18 UTC, 14 seconds after
  Copilot submitted a third review with 7 real findings (including a
  documented runbook path that doesn't exist — `.chitin/events.jsonl`
  when the kernel actually emits `events-<run_id>.jsonl`). Hamilton's
  own heuristics 1 ("the system will fail in ways you didn't design
  for") and 3 ("'trained users won't make that mistake' is the bug
  report") applied to the polling loop I'd written and trusted. PR #28
  filed to remediate; skill patched with pre-merge freshness check.
  See `souls/strikes/hamilton.md` for full record.

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
