---
date: 2026-04-19
status: in-progress
type: ab-test
related:
  - docs/observations/research/2026-04-19-soul-archetype-synthesis-socrates.md
  - docs/observations/quorums/2026-04-19-soul-archetype-requorum.md
---

# Soul vs single-prompt-baseline A/B test

## Why this exists

Socrates's adversarial synthesis surfaced the foundational hole: zero
data exists on whether named cognitive lenses produce measurable
behavioral differences vs a single generic prompt. Every "soul X works
for task Y" claim in the entire research corpus rests on assumed-but-
unmeasured efficacy. This test produces the missing data.

## Design

- **Sample:** the next 5 real user-bringing tasks. Mix of types (bug
  fix, design call, research pass, implementation, review) — same mix
  the soul system claims to handle.
- **Treatment (A):** chosen soul lens, full activation as currently
  practiced (frontmatter + heuristics + scope note).
- **Control (B):** single generic prompt — *"Be rigorous and empirical.
  Name the invariant before coding. State the hypothesis before the
  experiment. Attack the strongest form of any claim before accepting
  it."*
- **Blinding:** Claude runs both A and B per task. User judges blind
  before being told which was which.
- **Rubric (3 metrics, no others):**
  1. **Quality** (1–5, user judgment) — how well the output solved the
     task
  2. **Tokens spent** — total input + output across the run
  3. **Decision velocity** — wall-clock to a usable result
- **Decision rule:** soul wins iff
  - quality(A) > quality(B) by ≥1 point on ≥3 of 5 tasks, AND
  - tokens(A) ≤ 1.2 × tokens(B) on average
- **Failure cases:**
  - tokens(A) > 1.5 × tokens(B) for equal quality → souls are pure
    overhead, recommend deprecation regardless of other findings
  - quality(A) < quality(B) on ≥3 tasks → souls actively hurt; same
    recommendation
- **Verdict trigger:** after task 5, OR after task 3 if the signal is
  unambiguous (≥3 in one direction with no contrary points).

## Per-task ledger

(Append one row per task as it arrives. Don't backfill from memory —
each row is a real run, not a recollection.)

| # | Date | Task type | Soul chosen | Quality(A) | Quality(B) | Tokens(A) | Tokens(B) | Velocity(A) | Velocity(B) | Notes |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | — | — | — | — | — | — | — | — | — | pending |
| 2 | — | — | — | — | — | — | — | — | — | pending |
| 3 | — | — | — | — | — | — | — | — | — | pending |
| 4 | — | — | — | — | — | — | — | — | — | pending |
| 5 | — | — | — | — | — | — | — | — | — | pending |

## Verdict

(Filled in after task 5 or unambiguous earlier signal.)

## Constraints carried during the test

- No new soul ceremony for routine work while the test runs (per
  feedback memory `feedback_soul_system_keep_practices_drop_ceremony.md`).
- Scope notes and the empirical-loop discipline remain in use — they're
  *practices*, not under test here.
- The just-completed re-quorum's "set frozen 60–90 days" decision still
  stands; this test produces the data the next quorum acts on.
