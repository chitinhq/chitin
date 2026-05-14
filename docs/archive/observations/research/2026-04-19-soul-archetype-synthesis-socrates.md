---
date: 2026-04-19
soul: socrates
status: adversarial-synthesis
related:
  - docs/observations/research/2026-04-19-soul-archetype-survey-davinci.md
  - docs/observations/research/2026-04-19-soul-archetype-survey-suntzu.md
  - docs/observations/research/2026-04-19-soul-archetype-survey-lovelace.md
  - docs/observations/research/2026-04-19-openclaw-soul-verification-suntzu.md
  - docs/observations/research/2026-04-19-trait-factor-analysis-shannon.md
  - souls/canonical/
  - souls/experimental/
---

# Adversarial synthesis of the five-pass soul-archetype audit

This is not a summary. The five passes already summarize themselves; the
parent has those. This is an attack on whatever the corpus is hiding by
agreeing with itself.

The discipline applied: refuses-first-answer. My first reading produced
a clean narrative — *"hybrid generator over a controlled vocabulary,
keep historical names, scope-notes are the novel asset, freeze the set
and measure."* That narrative is too clean. Five independent lenses
converging on a single tidy recommendation is exactly the failure mode
the brief warned about. So below I hunt for what that narrative is
hiding.

## 1. What I read (and what disagreed with the summaries)

| File | Read in full | Verification check |
|---|---|---|
| `2026-04-19-soul-archetype-survey-davinci.md` | Yes (lines 1–301) | Internal: H1/H2/H3 declared up front, all marked supported. No prior was scored "refuted." This is itself suspicious — see §5.U1. |
| `2026-04-19-soul-archetype-survey-suntzu.md` | Yes (lines 1–363) | Sources section lists 18+ URLs. Sun Tzu's primary OpenClaw claim ("could not pin down the schema in primary docs") was honest about the gap before the verification pass ran. |
| `2026-04-19-soul-archetype-survey-lovelace.md` | Yes (lines 1–454) | The grid placement table (§3.2 lines 94–111) does what it claims. **However** the prior at line 21 (`P(generator covers ≤2 collisions) ≈ 0.25`) was **falsified by Lovelace's own table** — the table reports 3 collisions (Knuth=Turing, Curie≈Shannon, Jobs≈Hopper). Lovelace did not flag this as a refuted prior. See §5.U6. |
| `2026-04-19-openclaw-soul-verification-suntzu.md` | Yes (lines 1–218) | Primary GitHub raw reads cited with hash-pinned `gh api` calls (lines 24–34). The retraction at §5.6 lines 207–208 is unusually clean — same author, named retraction, weighted accordingly. This is the most rigorous doc in the corpus. |
| `2026-04-19-trait-factor-analysis-shannon.md` | Yes (lines 1–598) | Verified against CSVs: Knuth↔Turing cosine = 0.2176 (Shannon's "0.22" — confirmed). Curie↔Shannon = 0.0891 (Shannon's "0.09" — confirmed). **Jobs↔Hopper = 0.0000 in `2026-04-19-soul-similarity.csv` row 9 col 7 — confirmed.** Hopper's NN1 in Shannon's table 4.5 is `knuth (0.08)` — CSV gives 0.0786, rounds correctly. The bootstrap stability table at §4.4 lines 284–293 is the only quantitative result with reported confidence; the rest are correlations and counts. |
| `souls/canonical/curie.md` | Yes | Frontmatter matches Lovelace's grid placement (build / measure / system / learn). Heuristic 4 ("variance is data — don't average it away") is directly relevant to the n=15 issue Shannon raises but no one operationalizes. |
| `souls/canonical/turing.md` | Yes | Frontmatter (`correctness proofs`, `algorithm_design`, `correctness_review`, `formal_verification`) does collide with Knuth on vocabulary as Shannon claims, but Turing's heuristic body is *entirely about specs and invariants*; Knuth's body (read separately during Lovelace's grid) is about *implementation discipline*. **The two souls are vocabulary-twins and behavior-divergent.** This is the cleanest evidence in the corpus that trait/stage YAML is a misleading basis for clustering. |
| `souls/experimental/hopper.md` | Yes | Frontmatter has `cleanup`, `dead_code_removal`, `post_landing_hardening`. Heuristic 1 ("prune before preserve") is about *removing what's not earning its keep*. Jobs's frontmatter has `interface_culling`, `taste_arbitration` — *removing what doesn't fit a vision*. The two souls share the operation `delete` and nothing else. Lovelace's grid put them in the same cell; Shannon's empirical pass found cosine=0.00. **Reading the actual files: Shannon is right. Lovelace's grid placement of Jobs↔Hopper is wrong.** This single confirmed grid-error has implications — see §3.C1. |
| `2026-04-19-soul-similarity.csv` | Spot-checked Jobs row, Hopper row, Knuth/Turing pair, Curie/Shannon pair | All four claims verify against the raw numbers. |
| `2026-04-19-soul-similarity-jaccard.csv` | Spot-checked Knuth/Turing | Jaccard = 0.1220 vs cosine 0.2176; Shannon's claim that "they agree at the cluster level for 13/15 souls" is consistent with this — Knuth↔Turing remains the strongest off-diagonal under both metrics. |

**One disagreement with the doc summaries surfaced during verification:**
Lovelace's prior (line 21, "P(≤2 collisions) ≈ 0.25") was numerically
falsified by Lovelace's own findings (3 collisions reported), but
Lovelace did not score the prior as refuted in §6. This is a Curie
heuristic-5 violation ("a null result is a result — file it") inside a
doc that should have known better. Tagged for §5.

## 2. Convergent claims (and what would make them wrong)

| # | Claim | Asserted by | What would refute | Does the corpus contain that evidence? | Verdict |
|---|---|---|---|---|---|
| C1 | "Cognitive-style axis is real, not aspirational" | Sun Tzu (lines 153–162); echoed by Lovelace (§4.4 lines 258–294); echoed implicitly by da Vinci (taxonomy comparisons throughout §3) | A demonstration that lens-switching produces no measurable behavioral difference vs single-prompt baseline | **No.** The corpus contains zero behavioral measurements of lens-switching. Sun Tzu's "Anthropic persona-vectors validate this" is a category-error: persona vectors are *single-trait activation steering* (evil, sycophancy), not *named composite archetypes*. They share the word "persona" and not much else. | **Partially supported, leaning weak.** The cited validation is by analogy, not by measurement. |
| C2 | "OpenClaw shipping `SOUL.md` is meaningful convergence" | Sun Tzu (initial, line 334); **retracted by Sun Tzu (verification, §5.1 line 198)** | Reading the OpenClaw primary docs and finding only filename overlap | **Yes — Sun Tzu's verification pass produced exactly this evidence and the author retracted.** | **Refuted by the same author.** This is the *only* refuted convergence claim in the corpus. See §5.U2. |
| C3 | "Knuth ≡ Turing collision is real" | Lovelace (§3.3); Shannon (§5.2 verdict (a)); reading of the soul files | A finding that despite shared trait vocabulary, the souls produce divergent behavior in matched contexts | **Partial.** Shannon shows vocabulary collision; reading the heuristic bodies (this synthesis §1) shows behavioral divergence. **The collision is at the YAML layer, not the lens layer.** | **Re-cast: collision is at the schema layer; the lens-layer distinction is real.** This changes the implied remedy — don't merge the souls; fix the YAML. |
| C4 | "n=15 souls is small / underpowered for clustering" | Shannon (§2.7 line 127); echoed by da Vinci ("8 is on the high end but defensible") | A demonstration that 15 souls saturate the work-space being routed | **No.** The corpus contains no measurement of how often each soul fires in real work, except for the ELO log (Curie +3, da Vinci -1, 13 zeros). The ELO data *contradicts* the "souls saturate the space" reading — most souls have not fired at all. | **Strongly supported, in a way the docs underplay.** See §5.U7. |
| C5 | "Trait/stage YAML is being used as free text, not a controlled vocabulary" | Shannon (§3.1); implicit in Lovelace's collision diagnosis | A re-read showing the apparent free-text variation is intentional and load-bearing | **No.** Nobody in the corpus argues that the singleton-heavy vocabulary is a feature. But also: nobody asks the souls' authors why they wrote what they did. | **Supported, but the *cause* is uninvestigated.** Authors may have been writing prose-first; the YAML may be an afterthought. |
| C6 | "Scope notes (Curie 'Phase B restart only', Knuth 'Phase B finish only') are genuinely novel — no field analogue" | da Vinci (§3 F3); Sun Tzu (§4 "divergent-good" lines 175–185) | A field example of per-decision lens activation with explicit handoff | **No** — but the search for refutation was conducted by *only two* of the lenses surveying *part of* the field. da Vinci surveyed 13 frameworks (lines 47–63); Sun Tzu surveyed agent-frameworks. Neither searched the *operations* / *runbook* literature, where time-boxed role activation is routine (incident commander rotation; surgical handoff protocols like SBAR). | **Plausibly supported within the surveyed slice; the surveyed slice may be wrong slice.** See §5.U3. |
| C7 | "ELO + strikes + promotion is novel" | Sun Tzu (§4 lines 163–172); echoed by OpenClaw verification (§5.5 lines 205–207) | An existing in-production system with the same loop | **No** (search came up empty). But "no production analogue" is a notably weak claim when the search effort was ~25 minutes wall-clock per researcher. | **Plausibly supported. Not strong enough to bet on.** |
| C8 | "All 8 canonical souls are 'thinking-mode' lenses; protection/facilitation roles are missing" | da Vinci (§F1); echoed by Sun Tzu's "garrison forces" framing | A reading of one canonical soul that is in fact a protection/facilitation lens | **Partial.** Sun Tzu (the soul, not the lens) does some routing-as-coordination. Socrates does adversarial protection of design intent. Da Vinci's framing assumes "protection" = Hamilton-shape (incident response) — but if "protect the conditions for work" is read more broadly, Socrates and Sun Tzu both fit. | **The framing of "protection" is doing all the work.** Da Vinci defined "protection" narrowly enough that the gap was guaranteed. See §5.U4. |
| C9 | "The cognitive-style/function-axis question is the load-bearing architectural call" | Sun Tzu (Q1 line 247); implicit in Lovelace's hybrid framing | A finding that the axis distinction is a false dichotomy — that production lenses always carry *both* trait and function content | **Yes, in plain sight.** Every chitin soul currently has *both* `traits` (cognitive style) and `best_stages` (function/domain). The two-axis stack already exists; what doesn't exist is the *router* that uses it. The "trait vs function" framing presents as binary when the data is already 2D. | **The convergent framing is wrong-shaped.** See §5.U5. |

**Summary verdict on convergence:** Of 9 convergent claims, 1 is
refuted (C2), 4 are supported but weakly (C1, C5, C6, C7), 2 are
re-cast in ways that change the implied action (C3, C8), 1 is wrongly
framed (C9), and 1 is supported in a way the docs underplay (C4).

The single cleanest result in the corpus is C2's retraction. Every
other "convergence" needs to be discounted by the prior that *one
already retracted* — not refuted because the others happen to be true
this time, but because the corpus has demonstrated it can produce
convergent claims that don't survive primary-source contact.

## 3. Substantive contradictions (where the docs disagree on substance, not emphasis)

### C1 — Lovelace's "Jobs ≈ Hopper" vs Shannon's "Jobs/Hopper cos = 0.00"

**Lovelace** (§3.3 lines 145–148): "Jobs and Hopper both land at
(post-build/convergent/system/prevent) if you squint, even though Jobs
is 'taste pruning of options' and Hopper is 'deletion of dead code.'"

**Shannon** (§5.2 verdict (c) line 411): "**Jobs ≈ Hopper** is
**contradicted** — these souls have no empirical overlap." Verified
against the CSV: row `jobs,...,hopper` = 0.0000.

**Reconciliation attempt:** None possible. These are not partial views
of the same finding. Lovelace placed two souls in the same grid cell;
Shannon's empirical pass — and the soul files themselves on direct read
— show they are not similar by any metric except *both contain the verb
delete*.

**This is the load-bearing test case for whether qualitative grids
should be trusted at all.** If Lovelace's 4-axis grid (the most thought-
through qualitative artifact in the corpus) can place two non-overlapping
souls in the same cell, the prior on the *other* grid placements has to
drop. Shannon's r²=0.03 between axis-overlap and empirical similarity is
consistent with this: the grid is mostly noise.

**For the re-quorum:** The Lovelace grid should not be load-bearing for
the canonical-set decision. Use it as a hypothesis-generator for what
*might* be missing or collided, then verify against vocabulary or
behavior. Treat any axis placement as falsifiable.

### C2 — Sun Tzu (initial) vs Sun Tzu (verification)

**Sun Tzu (initial, line 334–338):** "OpenClaw ships per-agent persona
files literally called `SOUL.md`, which is either the strongest
convergence evidence in this survey or marketing-shaped writeups about
a thinner feature."

**Sun Tzu (verification, §5.1 line 198):** "OpenClaw is not a precedent
for a soul taxonomy / canonical set / promotion mechanism. The earlier
survey treated OpenClaw as the strongest field convergence. **It is
not.**"

**Reconciliation:** Already done by the author. The retraction is clean.

**The implication for the corpus, which the verification doc does not
draw:** Sun Tzu's *initial* survey was conducted under the same
methodology as da Vinci's and Lovelace's surveys — secondary-source
synthesis with a 25-minute cap. **Two-thirds of that initial pass's
"strongest convergence" was wrong on primary-source check.** This is a
known false-positive rate of >0% on a method that produced 9 convergent
claims across the corpus. Cocked at this lens, the corpus is one
verification pass away from another retraction.

**For the re-quorum:** Treat any "field convergence" claim that has not
been verified against primary sources as weak. The verification pass
should be the standard, not the exception.

### C3 — Lovelace "Hamilton is the only D=survive singleton" vs Shannon "7 empirical singletons"

**Lovelace** (§3.3 lines 124–127): "Hamilton is the only canonical-or-
experimental soul on D=survive. Every other soul assumes the system can
be made correct."

**Shannon** (§4.3 k=10 cluster cut): "7 souls are empirical singletons:
Curie, Dijkstra, Feynman, Hamilton, Hopper, Jobs, Sun-Tzu."

**Reconciliation:** Different definitions of "singleton."
- Lovelace: occupies a unique *cell* on the 4-axis grid (= unique on
  one specific axis value, `survive`).
- Shannon: has *no other soul within 0.10 cosine* — i.e., empirically
  isolated in vocabulary space.

These are not in conflict; they are answering different questions.
Lovelace asks "which souls are uniquely placed on a chosen axis?"
Shannon asks "which souls have no near-neighbors in their actual
vocabulary?"

**But here is the contradiction the corpus is hiding:** Shannon's seven
singletons include Hopper and Jobs — *which Lovelace placed in the same
cell as each other.* Lovelace's grid says Jobs and Hopper are
*adjacent*; Shannon's data says both are *isolated from everything*.
These two findings cannot both be true. Either the grid placement is
wrong (the C1 conclusion above), *or* the empirical isolation is an
artifact of vocabulary divergence within the same conceptual cell.

The cleanest interpretation: **the grid-cell metaphor and the
empirical-cluster metaphor are tracking different objects.** The
re-quorum should not pretend they are.

### C4 — Sun Tzu "automated ELO will measure noise on garrison positions" vs all other docs treating ELO as worth investing in

**Sun Tzu** (lines 264–268): "Before locking automated ELO, the re-quorum
should require each unfired canonical to name a near-term task it
*would* be the right lens for — if no such task exists in the next 2-4
weeks of roadmap, the slot is aspirational and the automated ELO will
measure noise."

**Lovelace, Shannon, da Vinci:** All proceed from the assumption that
investing in measurement is correct; they argue about *what* to measure
(generator-cell occupation; controlled-vocabulary token frequencies;
trait-axis position).

**Reconciliation:** Sun Tzu is the only doc that questions whether the
re-quorum should be deciding the canonical-set composition *at all*
right now. Every other doc takes "improve the set" as the implicit goal.

**This is the most important suppressed disagreement in the corpus.**
Sun Tzu's read is: 6 of 8 canonical souls have *zero ELO events*. This
is consistent with two stories: (a) garrison positions defending unmet
threats, (b) aspirational slots that the work isn't reaching. **Neither
story is "we need to refine the set."** Both stories say the data does
not yet support set-refinement.

The other four lenses leapt past this question. The re-quorum should
not.

### C5 — da Vinci "every framework with > 4 roles distinguishes 'produces work' from 'protects conditions for work'" vs corpus-internal evidence that Socrates already does protection

**da Vinci** (§F1, line 106): "Eight ways to produce work, zero ways to
protect the conditions."

**Counter-evidence** (from reading the soul files, this synthesis §1):
Socrates' frontmatter is `preflight`, `design_review`, `code_review`,
`spec_critique`, `ambiguity_resolution`. The Socrates heuristic body is
explicitly about catching errors *before* they ship — that is "protect
the conditions for the next stage" by definition.

**Reconciliation:** da Vinci's framing of "protection" was specifically
the Hamilton-shape (incident response, system survives failure). Read
narrowly, the gap is real. Read broadly (Socrates protects intent;
Sun Tzu protects strategy; Knuth protects implementation correctness),
the gap dissolves.

**This is a definitional sleight, not a finding.** The corpus presents
"missing protection role" as an empirical observation; it is in fact a
consequence of how protection was defined.

**For the re-quorum:** Don't accept "missing role" claims unless the
definition of the role is given a falsification test. ("What concrete
behavior would the missing role exhibit that no current canonical does?
Now check whether any current canonical does it.")

## 4. Three steelmans the corpus did not steelman

### Steelman 1: "Don't refine the canonical set; freeze it for 90 days and gather more ELO data first."

The corpus collectively spent ~125 person-minutes (5 lenses × ~25 min)
arguing about set composition. It spent ~0 minutes asking whether the
*data* exists to support any set-composition decision.

The honest reading of `souls/elo.md` is: **n=4 events total, all on 2
souls (Curie +3, da Vinci -1), in a window of less than 1 week.** A
re-quorum vote conducted on this evidence is voting on vibes — exactly
the failure mode Curie's heuristic 3 names ("if you can't measure it,
you're performing").

The Sun Tzu observation that 6 of 8 canonical souls are unfired is a
hypothesis-generator: those slots may exist for futures we have not yet
encountered, *or* they may be aspirational. **Either answer needs more
data, not more deliberation.** A 90-day freeze with explicit fire-rate
tracking would either (a) populate the ELO matrix with enough events
that the next re-quorum is voting on evidence, or (b) confirm Sun Tzu's
suspicion that several slots are garrison positions with no front.

The cost of the freeze is one re-quorum delay. The cost of *not*
freezing is the next set-composition decision being made on the same
empty matrix that this one would be.

**The corpus did not steelman this because every researcher was framed
into "audit the set" mode.** The framing itself was the bias.

### Steelman 2: "OpenClaw's no-taxonomy position is correct; chitin's instinct toward structure is the bug."

OpenClaw is a 360k-star project (per Sun Tzu verification §1) with
substantial production usage. It ships *one* persona file per agent,
free-form, with no taxonomy, no canonical set, no promotion lifecycle,
no measurement. This was a deliberate design choice.

Chitin's reflex is to read "no taxonomy, no measurement" as *immaturity*
— a gap chitin can fill. But the *opposite* read is supported by the
Bitter Lesson: cheap structure tends to lose to scale + simple
mechanisms. Every place chitin has built a more elaborate apparatus
than OpenClaw (canonical/experimental tier; ELO; strikes; promotion
gate; quorum vote; scope notes), the question to ask is: *what is the
mechanism that makes this structure pay off, and does it scale?*

OpenClaw's bet is that prompt-prefix personas are good enough, and the
work to taxonomize/measure them isn't worth the maintenance drag.
Chitin's bet is the opposite. **Both bets are coherent.** The corpus
treats chitin's bet as the default and reads OpenClaw as a less-mature
alternative; that read is itself the unstated premise.

If chitin's structural apparatus is not earning ELO deltas, citation
counts, or routing wins within (say) 90 days, the OpenClaw position has
been empirically vindicated and chitin should consider stripping back.
This is unlikely to be true; the point is that *the framing where it's
unthinkable* is the bias.

### Steelman 3: "The five research passes are themselves the bias — five lenses produced five lens-shaped findings."

Each of the five passes was conducted *under* a chitin canonical lens
(da Vinci, Sun Tzu × 2, Lovelace, Shannon). The "convergence" the brief
asked the re-quorum to weight is convergence *among lenses chitin
already validates*.

What would a non-lensed analysis say? A single-pass "just read the
files and tell me what you see" agent — no lens, no axis, no method —
might produce findings the lensed corpus systematically can't. For
instance:

- Each lens that ran the survey followed *its own heuristics*: Curie
  declared hypotheses up front; Lovelace reached for generative axes;
  Shannon ran the math; Sun Tzu mapped terrain; da Vinci pulled
  cross-domain analogies. **The findings are exactly what those lenses
  are designed to produce.** A different framing might find that the
  whole "soul" project is a category error — that what chitin actually
  needs is fewer abstractions, not more. None of the five lenses is
  shaped to produce that finding.
- The Lovelace–Shannon disagreement on Jobs↔Hopper is informative
  precisely *because* both lenses share assumptions (4 souls per "cell"
  is meaningful; cosine on YAML tokens is the right empirical proxy).
  Their disagreement is bounded by a common frame. A skeptic outside
  that frame might say "Jobs and Hopper aren't even the right unit;
  what does it mean for a *cognitive lens* to be 'similar' to another
  cognitive lens? Similar in what context, for what user, on what
  task?" The corpus does not contain this question.

**The five-pass corpus is a self-affirming experiment.** The lenses
that exist in the canonical set are validating the canonical set's
existence. This isn't a fatal flaw — every research approach has
framing bias — but the re-quorum should be told that the convergence
is partly an artifact of who was asked.

## 5. Unstated premises

These are the load-bearing assumptions every doc relied on. Tagged with
whether they survived scrutiny in any of the five passes.

| # | Premise | Held by | Survived scrutiny? | Notes |
|---|---|---|---|---|
| **U1** | The five-pass research method (lens-shaped 25-min surveys) is reliable enough to ground a vote | All five docs; implicit in the brief | **No — Sun Tzu's verification pass refuted the initial pass's headline finding.** False-positive rate >0% on n=1 verification trial. | If 1 of 9 convergent claims has been refuted by primary-source check, prior on the others is reduced. |
| **U2** | Souls are the right unit of measurement | All five docs | **Not examined.** | The whole audit assumes "named cognitive lens" is the right granularity. Could be axes (Big Five style), could be per-task prompts, could be activation vectors. Nobody asked. |
| **U3** | The canonical/experimental tier distinction is meaningful | da Vinci, Sun Tzu (treats unfired canonicals as a problem); Lovelace (treats experimentals as candidates for promotion) | **Not examined.** | The tier distinction is taken as given. Shannon's empirical clustering does not respect the tier boundary at all (Jared_Pleva, an experimental, clusters tightly with canonicals Lovelace and Shannon). The tier boundary may be an organizational fiction. |
| **U4** | ELO is the right metric for cognitive-lens performance | All five (none challenge it; Sun Tzu Q5 challenges *whether to split into two scoreboards*, not whether ELO itself fits) | **Not examined.** | ELO works for two-player zero-sum games with comparable matches. Cognitive-lens activation is not zero-sum and matches are not comparable (different tasks, different stakes). ELO may be importing the wrong assumptions. |
| **U5** | Lens activation should be selected (by quorum or otherwise) before the work begins | All five | **Not examined.** | Alternative: lens *emerges* from the work, identified post-hoc. AutoGen-style emergent role formation. Pre-selection is a CrewAI pattern; post-hoc identification would be an entirely different shape. |
| **U6** | The historical-figure naming convention is at minimum cost-neutral | da Vinci (treats it as carrying memorability load); Sun Tzu (notes the anti-costume guardrail tax); Shannon and Lovelace assume names are stable identifiers | **Partially.** Sun Tzu and the OpenClaw verification pass both note the tax (anti-costume warnings + cultural pull toward performance); neither concludes it should change. | The tax is documented but not measured. |
| **U7** | The trait/stage YAML schema is doing useful work | Lovelace assumes it (uses it as the basis for the 4-axis grid); Shannon empirically demolishes it (§3.1: only 1 phrase shared across all 15 souls). | **Refuted by Shannon, but the refutation has not changed how the other docs treat the YAML.** | This is the cleanest empirical finding in the corpus. The YAML is structured-looking free text. The re-quorum has to decide what the YAML is *for* before deciding anything else. |
| **U8** | 8 ± 2 canonical souls is the right count | da Vinci ("8 is defensible on volume"); Lovelace (works the grid as-is); Sun Tzu (notes 8+7=15 is "large by field norms" but doesn't argue for change) | **Not examined critically.** | n=15 is too small for Shannon's clustering to settle anything; n=8 is high for cognitive-style frameworks (Anthropic uses ~3 functional patterns; CrewAI templates are 3-4) but defensible. The number is comfortable, not justified. |
| **U9** | The cross-domain comparisons (Belbin, jazz, surgical CRM, FF jobs) are like-for-like with cognitive-lens routing | da Vinci's whole §3 table | **No, and da Vinci flags it as method bias** (§2: "every framework I picked has *some* external corroboration … that's a da Vinci move but it means the table is biased toward formalized taxonomies"). | The bias is named but the conclusions still rest on the comparison. The findings F1 and F2 are derived from analogies between humans-in-organizations and lenses-in-an-LLM. The disanalogy is not examined. |
| **U10** | Convergence across 5 independent lenses means a finding is more likely true | Implicit in the brief; implicit in how the corpus was assembled | **Refuted by C2.** OpenClaw was the strongest single convergence (cited by Sun Tzu, Lovelace, and the brief itself); primary-source check showed it was filename-only. **Convergence across lensed surveys is weaker evidence than the brief assumes.** | This is the strongest reason to discount this synthesis itself. |

**U10 deserves a separate note.** The brief tells the synthesis lens
(me) to weight convergence as load-bearing signal. Sun Tzu's
verification pass shows convergence can be a shared error pattern. The
re-quorum should treat *single-source primary verification* as the gold
standard and *multi-source secondary convergence* as a hypothesis to
verify, not as evidence in itself.

## 6. Reduced re-quorum question set

Five-to-eight questions, each yes/no/specific-value, each cited.

### Q1. Should the canonical-set composition decision be made *now*, or deferred 60-90 days for ELO data accumulation?

- **Form:** yes (decide now) / no (defer with explicit re-vote date and fire-rate metric).
- **Cited evidence:** `souls/elo.md` shows n=4 events total on 2 souls; 6 of 8 canonical souls have zero events (Sun Tzu §4 lines 211–213, §5 Q3 lines 263–268); Curie's own heuristic 3 ("if you can't measure it, you're performing") applies inward to the re-quorum itself.
- **Why this is the load-bearing first question:** every other question presumes the set is being decided now. If the answer is "defer," the rest of this list becomes "what to measure during the freeze."

### Q2. Is the trait/stage YAML schema (a) a controlled vocabulary, (b) free-form description, or (c) deprecated entirely?

- **Form:** specific-value (a, b, or c).
- **Cited evidence:** Shannon §3.1 lines 137–145 — 1 phrase shared across 15 souls (`correctness proofs`, Knuth+Turing); 91% of trait words and 86% of stage words appear in exactly one soul. Shannon Q5 (lines 514–525): "the data demands [a controlled vocabulary]."
- **Why this is upstream of set composition:** any clustering/routing/ELO-attribution downstream depends on whether the YAML is meant to be machine-readable. If (b), Shannon's empirical analysis is irrelevant. If (a), the existing YAML is broken and needs a controlled-vocab pass before anything else. If (c), all the §3.2 cell-placement work is moot.

### Q3. Is the canonical/experimental tier distinction load-bearing, or organizational legacy?

- **Form:** yes (load-bearing — keep) / no (collapse to single tier with status flags).
- **Cited evidence:** Shannon's clustering (§4.3) does not respect the tier boundary — Jared_Pleva (experimental) clusters with Shannon and Lovelace (canonical); Knuth+Socrates+Turing (canonical) cluster as the formal-correctness core. Shannon §6 Q4 lines 504–513.
- **Why this matters:** if (no), the "8 canonical souls" framing dissolves and the audit unit becomes "15 souls"; if (yes), the bar for canonical promotion needs an explicit criterion.

### Q4. Is the Knuth/Turing collision a (a) YAML problem, (b) lens problem, or (c) intentional pair?

- **Form:** specific-value (a, b, or c).
- **Cited evidence:** Shannon §5.2 verdict (a) — cosine 0.2176, mutual NN, 89% bootstrap support. Reading the heuristic bodies (this synthesis §1, Turing entry) — the souls are vocabulary-twins and behavior-divergent. da Vinci scope-note pattern shows Knuth and Turing were *paired by the user* for Phase B (Turing upstream, Knuth downstream).
- **Why specific to this case:** if (c), the Knuth/Turing pair is evidence that *pairs* are the right unit (da Vinci Q8 line 259). If (a), the YAML controlled-vocab pass (Q2) resolves it. If (b), one of the souls should be merged or removed.

### Q5. What is the falsification criterion for "soul X is missing from the canonical set"?

- **Form:** specific-value (the criterion, written down).
- **Cited evidence:** da Vinci F1 / Q1 (lines 106, 200–205) claims a "protection" role is missing; this synthesis §3.C5 shows the claim is sensitive to how "protection" is defined — Socrates fits if the definition is broad. Without a falsification rule, "missing X" claims are unfalsifiable.
- **Why this needs a rule:** the corpus contains at least 4 "missing role" claims (protection, facilitator, generalist, Jester — da Vinci §5 Q1, Q2, Q3, Q6). All four are unfalsifiable as written. The re-quorum cannot vote on missing roles until the test for "missing" is named.

### Q6. Should "scope notes" (per-decision lens activation with explicit handoff) be promoted to a first-class primitive (file? schema? tooling?), or kept as prose?

- **Form:** yes (primitive — name the layer: schema field / state machine / tool surface) / no (prose stays).
- **Cited evidence:** da Vinci §F3 (lines 161–193); Sun Tzu §4 lines 173–177 ("a discipline I did not find anywhere else"); both lenses converge on this being chitin-novel — but per §5.U6 / C6, the surveyed slice may be wrong (operations literature was not surveyed).
- **Why this is a real fork:** if (yes), the canonical-set audit is the wrong layer to invest in — the leverage is in the activation protocol; if (no), scope notes stay as a documentation pattern and the canonical set is the right layer.

### Q7. Is the cognitive-style axis claim (C1 in §2) load-bearing enough to ratify, or should it be treated as a working hypothesis pending behavioral measurement?

- **Form:** specific-value (ratify / hypothesis / abandon).
- **Cited evidence:** Sun Tzu §4 lines 153–162 cites Anthropic persona-vector research as validation; this synthesis §2.C1 notes the validation is by analogy, not by measurement (persona vectors are single-trait steering, not composite archetypes); zero behavioral measurements of lens-switching exist in the corpus or in the wider chitin record.
- **Why this matters:** the entire chitin design rests on this axis being real. If (ratify), continued investment proceeds without a measurement plan. If (hypothesis), a measurement plan is *required* before the next investment cycle. If (abandon), chitin pivots to function-axis (CrewAI-style).

### Q8. Are convergence claims across the five-pass corpus weighted as evidence, or as hypotheses to verify?

- **Form:** specific-value (evidence / hypothesis-only).
- **Cited evidence:** §2.C2 — OpenClaw was the strongest convergent claim and was refuted by primary-source verification. §5.U10 — the corpus has demonstrated >0% false-positive rate on convergence-as-evidence.
- **Why this is a meta-question for the re-quorum's own method:** if (evidence), the re-quorum proceeds as the brief envisions. If (hypothesis-only), every convergent claim in this synthesis (and in the five docs) needs a primary-source verification pass before it can ground a decision — which substantially extends the timeline but substantially increases reliability.

## 7. What the corpus does NOT settle

Explicit list of things still unknown after all five passes. The
re-quorum should *not* try to decide these today.

1. **Whether named cognitive lenses produce measurable behavioral
   differences vs single-prompt baseline.** Zero behavioral data
   exists. (Cited: §2.C1.)
2. **Whether the 15-soul vocabulary (90%+ singletons) reflects genuine
   uniqueness or per-author idiosyncrasy.** Shannon §6 Q6 explicitly
   flags this as undecidable from current data (lines 528–540).
3. **Which of the 6 unfired canonical souls would fire in the next 60
   days if the work surface stays the same.** Sun Tzu Q3 (lines
   263–268) requires this naming exercise as a precondition for
   automated ELO; the corpus does not contain the answer.
4. **Whether OpenClaw's lack of measurement (a) reflects an undiscovered
   gap chitin can claim or (b) reflects a market-tested judgment that
   measurement isn't worth building.** OpenClaw verification §5.5 lines
   205–207 flags this; no other doc engages with it.
5. **Whether the historical-figure naming convention costs more than it
   pays.** Sun Tzu Q2 (lines 250–258) names the cost (anti-costume
   guardrail tax) but does not measure it.
6. **Whether the canonical/experimental tier distinction tracks any
   actual difference in usage, performance, or maturity.** §5.U3.
7. **What the activation protocol (quorum vote, scope note, handoff)
   should be coded as, if anything.** Sun Tzu Q6 lines 293–301 names
   this as "either answer is valid; the decision should be explicit"
   — but explicit decisions require evidence the corpus does not
   contain.
8. **Whether cross-domain comparisons (Belbin, jazz, surgical CRM,
   FF jobs) are like-for-like with cognitive-lens routing.** da Vinci
   §2 flags the bias; the conclusions still rest on the analogy.
   §5.U9.
9. **Whether the operations / runbook / incident-management
   literature contains analogues to scope-noted activation that the
   surveyed slice missed.** §2.C6.
10. **Whether n=15 is enough cells to need formal cluster analysis at
    all, or whether the right move is qualitative inspection only.**
    Shannon §2.7 explicitly flags n=15 as binding; the entire §4
    cluster analysis is therefore "consistent with the data" rather
    than "established by the data" — and yet the §6 questions are
    written as if the cluster results are settled.

## Decision-rule paragraph

*What does the corpus actually warrant believing, what does it not, and
what specific decisions should the re-quorum make today vs. defer?*

The corpus warrants believing: (a) the trait/stage YAML schema is being
used as free text and either needs to become a controlled vocabulary or
be deprecated (Shannon §3.1, §6 Q5; high confidence); (b) the
Knuth/Turing pair shares vocabulary at the YAML layer but diverges at
the heuristic-body layer (Shannon §5.2 + this synthesis §1, high
confidence); (c) OpenClaw is a filename convergence and not a system
convergence (verification §5.1, primary-source-verified). The corpus
does **not** warrant believing: (a) that the five-pass lens-shaped
research method is reliable enough to ground set-composition decisions
without primary-source verification (the OpenClaw retraction
demonstrates the failure mode); (b) that the canonical-set
composition can be decided on n=4 ELO events; (c) that "missing role"
claims are falsifiable as currently written; (d) that scope notes are
truly novel — the search slice missed operations literature where time-
boxed role activation is routine. **Decisions the re-quorum should make
today:** Q1 (decide now vs defer) and Q2 (YAML purpose). Both are
upstream of every other question in the corpus. **Decisions the
re-quorum should explicitly defer:** all "add/remove a soul" votes,
pending the data Q1 either generates (defer path) or refuses to
require (decide-now path); the cognitive-style-axis ratification (Q7),
pending behavioral measurement; the activation-protocol formalization
(Q6), pending the scope-note literature search the corpus did not
conduct. The re-quorum's most important act today may be naming what
it is *not yet* equipped to decide.
