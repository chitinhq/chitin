---
date: 2026-04-19
type: quorum-vote
scope: canonical soul set audit (re-quorum from research corpus)
result: Q1=DEFER (6/8); Q2–Q8 not voted today
related:
  - docs/observations/research/2026-04-19-soul-archetype-synthesis-socrates.md
  - docs/observations/research/2026-04-19-soul-archetype-survey-davinci.md
  - docs/observations/research/2026-04-19-soul-archetype-survey-suntzu.md
  - docs/observations/research/2026-04-19-soul-archetype-survey-lovelace.md
  - docs/observations/research/2026-04-19-openclaw-soul-verification-suntzu.md
  - docs/observations/research/2026-04-19-trait-factor-analysis-shannon.md
  - docs/observations/quorums/2026-04-19-phase-b-finish.md
---

# Quorum vote — canonical soul-set audit (re-quorum from research corpus)

## Question put to the quorum

Five lensed research passes (da Vinci, Sun Tzu, Lovelace, Shannon, Sun
Tzu's OpenClaw verification) plus a Socrates adversarial synthesis
audited the 8-canonical / 7-experimental soul set in advance of locking
automated ELO scoring against it. Socrates reduced the corpus to eight
yes/no/specific-value questions (Q1–Q8, synthesis §6) and declared Q1
the gating vote: **should the canonical-set composition decision be
made now, or deferred 60–90 days for ELO data accumulation?** Q2 (YAML
schema purpose) is the second gate, voted only if Q1 = decide-now.

## Context

- **Research corpus:** 5 lensed surveys + 1 verification + 1 synthesis,
  produced 2026-04-13 → 2026-04-19. The synthesis (Socrates) refused
  the corpus's own first reading and surfaced 10 unstated premises and
  3 substantive contradictions (synthesis §3, §5).
- **Verified data points the quorum can lean on:** Knuth↔Turing
  cosine 0.2176, mutual NN, 89% bootstrap (Shannon §4.4–4.5; CSV row 11
  col 15 confirmed); 91% of trait words and 86% of stage words appear
  in exactly one soul (Shannon §3.1); OpenClaw `SOUL.md` is filename
  convergence only — no taxonomy, no ELO, no promotion mechanism
  (Sun Tzu verification §3, primary-source `gh api` reads); Lovelace's
  Jobs≈Hopper grid placement is empirically refuted at cos=0.0000
  (Shannon §5.2 verdict (c); CSV row 8 col 6 = 0.0000).
- **Available signal for set-composition decisions:** `souls/elo.md`
  reports n=4 events total across 2 souls (Curie +3, da Vinci −1) in
  a window of less than one week. **Six of eight canonical souls have
  zero measured events.** (Sun Tzu §4 lines 209–213; Socrates §3.C4.)
- **Gating-question structure (per brief):** Q1 first; if defer wins
  ≥5/8, Q2–Q8 are not voted today and the record closes after Q1.
- **CSV spot-check:** `2026-04-19-soul-similarity.csv` exists, header
  + 15 data rows, 15-soul-square matrix. Confirms the matrix Shannon
  cites is real and shaped as claimed.

## Q1 vote — decide-now vs defer

**Question (verbatim from synthesis §6 Q1):** Should the canonical-set
composition decision be made *now*, or deferred 60–90 days for ELO
data accumulation?

**Form:** decide-now / defer.

| Soul | Vote | Reasoning (lens-grounded, one line) |
|---|---|---|
| Curie | DEFER | Heuristic 3 ("if you can't measure it, you're performing") applies inward — n=4 ELO events on 2 souls is vibes, not data; Heuristic 6 says when the signal is real but rare, run more trials before theorizing (Socrates §3.C4 cites this exact heuristic against the corpus itself). |
| Knuth | DEFER | Heuristic 1 ("prove it or it's not proven"): the invariant "this set composition is correct" has no proof at n=4; Heuristic 4 says boundaries are where bugs live, and a set-composition decision on a 6-of-8-empty matrix is exactly the boundary input that breaks the dispatcher. |
| da Vinci | DEFER | Heuristic 2 ("observation over dogma") — observe `souls/elo.md`, not the roadmap; the matrix is empty, the question is wrong-shaped (synthesis §3.C5 already showed da Vinci's "missing protection role" claim was a definitional sleight, so my own corpus is suspect); Heuristic 6 ("three why's") collapses the urgency — *why decide now?* to lock automated ELO; *why lock automated ELO?* — to measure; the answer to "should we decide before measuring?" answers itself. |
| Lovelace | DECIDE NOW | Note G test (Heuristic 4) — the 4-axis generator predicted three real holes (synthesis verifies cells empty per Shannon §5.3) and surfaced the Knuth↔Turing collision at 89% bootstrap; the generator is paying rent and a hybrid (named primaries on axes) is shippable today; deferring 90 days re-pays the same audit cost without new generative content. |
| Shannon | DEFER | §2.7 of my own analysis: "n=15 is binding — treat every cluster claim as consistent-with-data, not established-by-data unless >80% bootstrap"; only one cluster (formal-rigor C7) clears that bar, and `souls/elo.md` n=4 is two orders of magnitude below the threshold needed to ground composition; deciding composition now is exactly the channel-flooding failure mode (Heuristic 6 — push more signal than the channel can carry → drops, not throughput). |
| Socrates | DECIDE NOW | Per my own synthesis decision-rule paragraph (§Decision-rule, lines 448–457): "Decisions the re-quorum should make today: Q1 (decide now vs defer) and Q2 (YAML purpose). Both are upstream of every other question in the corpus" — voting defer here contradicts the synthesis's own scoping (note: this is the tension my Heuristic 1 ("what would make this wrong?") was written to surface — the quorum may be right to override me). |
| Sun Tzu | DEFER | Heuristic 2 ("the best fight is the one you avoid") — 6 of 8 canonical have zero ELO events; this is either garrison value or aspirational slots, and *neither story is "we need to refine the set"* (synthesis §3.C4); Heuristic 5 ("cost asymmetry") — a 60–90 day data-collection freeze is cheap and the next re-quorum runs on evidence instead of opinion. |
| Turing | DEFER | Heuristic 5 ("know the worst case, and what triggers it") — the worst case for set-composition decisions is "decided on noise"; the input that triggers it is exactly n=4 ELO events; Heuristic 3 (proof-by-construction) — neither the corpus nor the ELO log exhibits the witness for "this composition is correct," so the conversation hasn't sharpened yet, it's been deferred by deciding. |

**Tally: DEFER 6, DECIDE NOW 2. DEFER wins (6/8).**

**Decision:** Per the brief's voting protocol, Q1 = defer with ≥5/8
votes terminates the agenda. **Q2–Q8 are not voted on today.** They
are deferred pending the data the freeze will produce.

## Decision

| Item | Decision | One-line rationale |
|---|---|---|
| **Q1** | **DEFER** canonical-set composition decisions for 60–90 days | n=4 ELO events across 2 souls cannot ground composition; freeze the set, gather evidence, re-quorum on data |
| **Q2–Q8** | **NOT VOTED today** | Per gating rule; the deferred questions remain in the synthesis as the agenda for the next re-quorum |
| **What the freeze measures** | Per Sun Tzu §4 lines 209–213: per-soul fire rate across the 60–90 day window; each unfired canonical *names* a near-term task it would be the right lens for; the ELO matrix (currently 13 of 15 souls at 1500-flat) is allowed to populate from real work | The two readings of "6 of 8 canonical unfired" (garrison vs aspirational) are distinguishable only by data, and only data the freeze produces |
| **What is *not* deferred** | The corpus and synthesis stand as the agenda; Lovelace's grid, Shannon's matrix, the OpenClaw retraction, and the 10 unstated premises are reusable input for the next vote | The next re-quorum starts from §6 Q1–Q8, not from scratch |

## Constraints carried by this decision

These are the rules the next implementation pass (or the next
re-quorum's clerk) inherits.

1. **Set composition is frozen.** No additions, removals, mergers, or
   re-tier moves on `souls/canonical/` or `souls/experimental/` until
   the next re-quorum. Scope notes (Curie/Knuth Phase B-style) are
   *not* set-composition changes and remain allowed.
2. **The freeze period is 60–90 days from 2026-04-19.** Earliest
   re-quorum: 2026-06-18. Latest: 2026-07-18.
3. **Per-soul fire rate must be tracked across the freeze.** What
   counts as "fired" needs a definition before the next re-quorum can
   use the data — open implementation question, see §Mechanism.
4. **Each unfired canonical (currently Knuth-only-as-scope-note,
   Lovelace, Shannon, Socrates, Sun Tzu, Turing — 6 souls with 0 ELO
   delta events) should produce a one-paragraph "near-term task this
   would be the right lens for"** during the freeze. If no such task
   is named within 30 days for any given soul, that soul becomes a
   priority candidate for next-re-quorum demotion review.
   (Sun Tzu §5 Q3 lines 263–268.)
5. **Q2 (YAML schema purpose) is the next vote's first question.** It
   is upstream of Q3–Q8 per Socrates §6. Implementations that depend
   on YAML structure (controlled-vocab routing, automated ELO
   attribution, cluster-based dispatch) should not ship before Q2 is
   decided — they would lock in an answer the quorum hasn't taken.
6. **Automated ELO scoring is deferred along with set composition.**
   The trainer's-note ELO at `souls/elo.md` continues as the only
   scoreboard; automated scoring is gated on Q2 + a fire-rate baseline
   from the freeze.
7. **The corpus's >0% false-positive rate on convergence (OpenClaw
   retraction; Socrates §5.U10) means future "field convergence"
   claims need primary-source verification before grounding a
   re-quorum.** The verification pass becomes the standard, not the
   exception.

## Closing condition

This re-quorum graduates to retrospective when **either** of the
following becomes true (whichever comes first):

- **Data threshold met:** the next ELO measurement window produces
  n ≥ 20 delta events distributed across ≥ 5 distinct souls (i.e.,
  the matrix is no longer dominated by 2 souls). At that point a
  composition re-vote on `souls/elo.md` evidence becomes possible.
- **Time threshold met:** 60 days elapse (2026-06-18) without the
  data threshold being met. At that point the *absence* of fire is
  itself the finding — Sun Tzu's "garrison vs aspirational" question
  resolves toward "aspirational" for any soul still at zero delta
  events, and the next re-quorum has a different shape (demotion
  review rather than expansion).

In either case, the retrospective lives at
`docs/observations/retrospectives/` per spec §337 of
`2026-04-19-dogfood-debt-ledger-design.md`.

## Mechanism — what files/scope-notes need updating to enact this decision

This list is the *change set* the user (or a subsequent scoped
subagent) executes. The clerk does not enact any of these.

1. **`souls/elo.md`** — add a "Freeze period 2026-04-19 → 2026-06-18"
   note in the convention section, and a column for "fire events"
   (defined: any ELO delta event *or* any session that activated this
   soul as the explicit lens, even with no delta). Define fire-event
   tracking before counting begins.
2. **Each canonical soul file with status: promoted** (8 files in
   `souls/canonical/`) — append a one-line freeze note: "Freeze
   period: 2026-04-19 → 2026-06-18 (re-quorum 2026-04-19 deferred
   set-composition decisions pending fire-rate data)."
3. **`docs/observations/research/2026-04-19-soul-archetype-synthesis-socrates.md`**
   — append a status line: "Q2–Q8 carried into freeze period;
   re-quorum 2026-04-19 voted Q1=DEFER 6/2."
4. **`~/.claude/CLAUDE.md`** (user's private global instructions) —
   *no change required by this decision.* Sticky default remains
   da Vinci per quorum 2026-04-13. The Curie scope note (Phase B
   restart only, completed) and Knuth scope note (Phase B finish
   only) remain in force per the Phase B finish quorum.
5. **Open implementation question for the freeze period:** what
   instrumentation (if any) emits a "soul fired" event, and where
   does it land? Three plausible substrates: (a) `souls/elo.md` event
   log only — manual; (b) Phase 1.5 events.jsonl chain — automated
   if the active-soul context is detectable; (c) per-session quest
   row in Neon. **This is not a decision for this quorum** — it
   becomes Q1 of the next re-quorum's prep work.
6. **Calendar / followups:** earliest re-quorum 2026-06-18; latest
   2026-07-18. Whoever opens that re-quorum reads this record + the
   Socrates synthesis §6 + the new ELO event log as the input
   bundle.

---

**Reader's one-paragraph summary** (per the brief's "record is good"
test): the re-quorum voted 6/8 to **defer** all canonical-set
composition decisions for 60–90 days because the available signal
(n=4 ELO events across 2 souls; 6 of 8 canonical with zero events)
cannot ground decisions of that weight, and Curie/Shannon/Sun Tzu/
Turing/Knuth/da Vinci all reached this conclusion from their own
lens-specific heuristics; Lovelace and Socrates dissented (Lovelace
on Note-G grounds — her generator is shippable now; Socrates on
the grounds that his own synthesis explicitly scoped Q1 and Q2 as
today's decisions). Q2–Q8 are not voted today per the gating rule
and remain on the agenda for the next re-quorum (2026-06-18 to
2026-07-18). The freeze constrains: no set-composition changes;
each unfired canonical must name a near-term task within 30 days;
automated ELO is gated on Q2 + freeze-period fire-rate data;
trainer's-note ELO at `souls/elo.md` remains the only scoreboard.
Specific actions follow in §Mechanism — the user enacts; the clerk
does not.
