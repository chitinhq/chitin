# Souls consolidation — scoping plan

**Status:** scoping (pre-implementation). Closes the planning portion of #25; the implementation phase opens its own PR(s) once an architect picks up.

**Goal:** answer the three open questions #25 lists ("which research is load-bearing? which matrix? which consolidation mode?") so that a follow-up implementation PR can start from a concrete spec instead of a re-litigation.

**Active soul for this scoping work:** da Vinci (per the issue's "this is architecture, not implementation" framing and the 2026-04-19 quorum's Phase D/E/F architecture assignment). Knuth and Curie own implementation phases that come AFTER this plan lands.

---

## What's in-repo today

The 2026-04-19 research wave produced:

- **Quantitative:** `docs/observations/research/2026-04-19-{trait,stage}-matrix.csv` + `-phrase` variants. `2026-04-19-soul-similarity.csv` (cosine over trait vectors) and `-jaccard.csv` (set-overlap on phrase membership).
- **Qualitative:** single-author archetype surveys for `davinci`, `lovelace`, `sun-tzu`. Synthesis paper from `socrates`.
- **Methodological:** Shannon's trait-factor analysis (`2026-04-19-trait-factor-analysis-shannon.md`).
- **Empirical:** in-flight A/B baseline test (`2026-04-19-soul-baseline-ab-test.md`).
- **Procedural:** the 2026-04-19 requorum vote (`docs/observations/quorums/2026-04-19-soul-archetype-requorum.md`).

The cosine similarity matrix's signal at a glance:

| Pair | Similarity | Note |
|---|---:|---|
| knuth ↔ turing | **0.218** | strongest cross-soul signal |
| jared_pleva ↔ shannon | 0.150 | personal archetype overlapping a canonical |
| jared_pleva ↔ turing | 0.147 | same |
| jared_pleva ↔ lovelace | 0.144 | same |
| knuth ↔ socrates | 0.141 | declarative/inquiry overlap |
| shannon ↔ turing | 0.140 | channel + computation |

Curie, hamilton, jobs, sun-tzu are isolates (≈0.0 across the row). Either they're genuinely orthogonal or the trait vocabulary doesn't capture them — Shannon's factor analysis is the right place to disambiguate.

---

## Q1 — Which research is load-bearing?

Three reasonable answers, in increasing strictness:

- **Empirical-first** (Curie's lens): the A/B baseline test (`2026-04-19-soul-baseline-ab-test.md`) is the only one with a measurable effect on agent behavior. Consolidation grounded here means "consolidate pairs the A/B test can't distinguish."
- **Theoretical-first** (Shannon's lens): the trait-factor analysis is the dimensionality-reduction view. Consolidation grounded here means "two souls in the same factor neighborhood are redundant."
- **Synthesis** (Socrates' lens): the archetype-survey synthesis is the qualitative read. Consolidation grounded here means "two souls whose synthesis paragraphs argue similar moves can fuse."

**Proposed answer:** prefer empirical-first as the load-bearing axis, because the cost of a wrong consolidation is paid by future agents, and the agent-behavior signal is what the soul system exists to influence. Use Shannon's factor analysis as a *secondary filter* (don't fuse two souls the A/B test can't distinguish UNLESS they also occupy the same factor neighborhood). Treat Socrates' synthesis as a *tie-breaker* and as commentary on the resulting shape — it's the most subjective view.

**What this rules out:** consolidating purely on similarity-matrix scores (the easy default) without grounding in either A/B behavior or factor structure. The matrix is a starting point, not a verdict.

**Open work before implementation:** the A/B baseline test is in-flight. Block consolidation on its completion — without n≥3 trials per pair, "the test can't distinguish them" is uncalibrated.

---

## Q2 — Which matrix?

The candidates from the issue:

- **Shannon factor-reduced trait space** — a small number of latent factors (the analysis suggests ~4) onto which traits project. Most principled.
- **Stage × trait cross-tabulation** — preserves the "which souls fit which lifecycle stages" view. Useful for routing decisions but not for consolidation.
- **Earlier 4-axis sentinel/evolve scoring** — couldn't locate an authoritative copy in-tree as of this plan; either it's in a private archive or it was never committed. Treat as missing.
- **Something newer** — not yet checked in; out of scope.

**Proposed answer:** use Shannon's factor-reduced space as the consolidation matrix. It IS the 4-axis matrix the issue references (Shannon's analysis converged to ~4 factors per the research note). Stage × trait stays as a secondary view for routing — that's a different decision (which soul to ACTIVATE for which work) than consolidation (which souls to KEEP in the canonical set).

**Concrete next step for whoever picks this up:** load `2026-04-19-trait-factor-analysis-shannon.md` and either (a) confirm the 4-factor extraction is the matrix, or (b) re-run with current souls to refresh. Don't take the cosine similarity CSV as authoritative without re-deriving from the factor space.

---

## Q3 — Consolidation mode

The three modes from the issue:

- **Fuse** — merge two souls' heuristics into one. **Irreversible**, hash changes, breaks any prior session's `soul_hash` reference.
- **Deprecate** — move under-used souls from canonical → experimental, or experimental → archived. **Reversible**.
- **Tier more aggressively** — restrict canonical to 4-5 cleanly-covering-the-matrix souls; move the rest to experimental. **Reversible**, preserves the library.

**Proposed answer:** start with **Tier more aggressively** as the v1 move. Reasons:

1. **Reversibility is cheap insurance.** v2 can fuse later if the tightened canonical set proves stable; fuse-now closes that door.
2. **Preserves the archive.** Single-author surveys for da Vinci / Lovelace / Sun-Tzu would be wasted if those souls were fused away. Tiering keeps them addressable.
3. **Matches Anthropic-research framing.** The persona/steering-vector literature treats personas as a library to address, not a small enum. A larger experimental tier with a smaller canonical hot-path mirrors that.
4. **Gives the A/B baseline test more to work with.** Fusing pre-empts the empirical signal; tiering preserves the cohort.

**Tiering criteria (proposed; open to refinement):**

- Canonical (target: 4–6): each canonical soul must occupy a distinct Shannon-factor neighborhood AND have at least one A/B trial showing measurable behavior delta vs. baseline.
- Experimental: everything else. Includes single-author archetypes and provisional / personal souls.
- Archived: souls neither used in A/B nor cited in canonical's factor coverage. Move out of `experimental/` to keep the live tier short.

**Hard constraints:**

- The **default sticky soul** stays in canonical (currently da Vinci per `memory/user_default_soul.md`).
- Souls with active scope notes (someone is using them right now) cannot be retiered until the scope note closes — automatic via `/soulswap`'s scope-note convention.
- ELO state preserves across tier moves (no reset).

---

## What this plan deliberately does NOT decide

- **Which specific souls move.** That's the implementation PR's job once the matrix is locked. This plan only fixes the framework.
- **Whether to add new souls.** Out of scope; addition follows the existing `souls/README.md` promotion ritual.
- **Reweighting the trait vocabulary.** The trait vector is the input to the matrix; changing it would invalidate the existing CSVs and force a re-run.

## Acceptance criteria for the implementation PR

When someone picks this up:

- [ ] Re-run (or confirm) Shannon's factor analysis on the current `canonical/` + `experimental/` set; commit the refreshed factor matrix.
- [ ] Run the A/B baseline test to ≥3 trials per soul; tabulate which pairs the test can't distinguish.
- [ ] Identify the 4–6 canonical souls per the tiering criteria above.
- [ ] Move souls between tiers via filesystem moves (preserve scope notes, preserve frontmatter `promoted_at` history).
- [ ] Update `souls/README.md`'s "Current library" section.
- [ ] No fusion, no archive deletion in v1.

## Memory + reference

- Active-soul handoff is now automated (`/soulswap` skill, ships in this PR's sibling commit).
- The "keep practices, drop ceremony" feedback (`memory/feedback_soul_system_keep_practices_drop_ceremony.md`) means tiering should NOT introduce a new ritual; it's a one-time reorganization.
- Quorum context: `docs/observations/quorums/2026-04-19-soul-archetype-requorum.md` — the requorum that established the current canonical set.
