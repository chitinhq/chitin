---
date: 2026-04-19
type: quorum-vote
scope: Phase B finish (dogfood-debt-ledger plan)
result: Knuth (unanimous, 8/8 canonical souls)
related:
  - docs/superpowers/plans/2026-04-19-dogfood-debt-ledger.md
  - docs/observations/2026-04-19-hook-payload-capture.md
  - souls/strikes/davinci.md
  - souls/elo.md
---

# Quorum vote — Phase B finish

## Question put to the quorum

Who implements the remaining Phase B tasks of the dogfood-debt-ledger
plan: global install/uninstall in
`go/execution-kernel/internal/hookinstall/` with `~/.claude/settings.json`
merge semantics, plus the Task B1 `SubscribedHooks` export and the test
suite specified in plan §428–610.

## Context

- **PR #19 closed without merge** under da Vinci. Strike record:
  `souls/strikes/davinci.md`. Failure mode: implemented against an
  *assumed* Claude Code hook schema; adversarial review caught the
  divergence post-PR.
- **Curie ran the Phase B restart investigation** under a scoped handoff
  (2026-04-19). Output: lab note at
  `docs/observations/2026-04-19-hook-payload-capture.md` confirming
  SessionStart, SubagentStop, and PreCompact payload shapes via the
  empirical loop (incl. forced-trial follow-through on PreCompact).
- The empirical wire is now ground truth. The remaining work is
  boundary-correctness implementation — the exact failure surface that
  broke PR #19.

## Vote

| Soul | Vote | Reasoning (one line) |
|---|---|---|
| Knuth | Knuth | Merge semantics on `settings.json` is a boundary problem (empty / missing / malformed / existing-hook / partial-write); his "name the invariant first" is the literal antidote. |
| da Vinci | Knuth | Architecture is decided; what remains is execution. |
| Socrates | Knuth (impl), Socrates (review) | Confirms the existing per-PR review trip-wire; impl ≠ review. |
| Shannon | Knuth | Channel is now empirically confirmed; what's left is mechanical correctness, not channel design. |
| Lovelace | Knuth | Concurs *provided* B1 keeps `SubscribedHooks` reusable across surfaces, not claude-code-specific. |
| Sun Tzu | Knuth | "Attack the plan, not the agents" — re-route to the right lens, no thrash. |
| Turing | Knuth | Emphasizes the symmetric-idempotency invariant `uninstall(install(s)) == s` must be stated before code. |
| Curie | Knuth | Per scope note, Curie hands off after Phase B restart investigation completes; empirical wire goes to the implementer. |

**Result: 8/8 → Knuth.**

## Decision

- **Implementer:** Knuth (scope-bound to Phase B finish only).
- **Reviewer:** Socrates (per existing trip-wire from
  `2026-04-19-dogfood-debt-ledger-design.md` §273).
- **Default unchanged:** da Vinci remains sticky default per quorum
  2026-04-13. Returns for Phases D/E/F (cross-surface architecture)
  after Phase B ships.

## Constraints carried by this handoff

1. **State the invariant first** (Knuth #1, Turing #1):
   `uninstall(install(s)) == s` for all `s ∈ {empty, missing, malformed,
   existing-hooks, chitin-already-installed}`. Write it down before code.
2. **Wire is observed, not assumed** — read
   `docs/observations/2026-04-19-hook-payload-capture.md` before any
   schema choice.
3. **Generalize per Lovelace's dissent** — `SubscribedHooks` export must
   be reusable across surfaces, not hard-coded to claude-code's payload
   shape.
4. **Test boundaries before mainline** (Knuth #4): empty file, missing
   file, malformed JSON, hooks-key-null, hooks-key-empty, existing
   non-chitin entries, double install (idempotent), uninstall after
   manual edit.
5. **Socrates reviews on PR open**, not at merge time — adversarial
   review is preflight, not postflight.

## Mechanism

- Active soul context for Knuth lives at `souls/canonical/knuth.md`
  (scope note added 2026-04-19).
- Curie scope note marked complete at `souls/canonical/curie.md`.
- Global active-soul block at `~/.claude/CLAUDE.md` is **not yet
  updated** — that swap is a separate user decision (changes the lens
  for the next session).

## Closing condition

Phase B PR merges → Knuth scope expires → da Vinci resumes default.
This record graduates to a retrospective once the PR ships, per spec
§337 ("every major graduation → 1-page note at
`docs/observations/retrospectives/`").
