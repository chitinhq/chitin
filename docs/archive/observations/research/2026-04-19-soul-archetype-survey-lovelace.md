---
date: 2026-04-19
soul: lovelace
status: research-draft
related:
  - souls/canonical/
  - souls/experimental/
  - souls/elo.md
  - docs/observations/research/2026-04-19-soul-archetype-survey-davinci.md
  - docs/observations/research/2026-04-19-soul-archetype-survey-suntzu.md
---

# Are 8 named souls the right shape — or is the shape "what's the generator"?

## 1. Hypothesis (written before the research)

Prior, written cold before reading any external taxonomy:

**P(a clean generator with ≤4 axes covers the canonical 8) ≈ 0.4.**
**P(a generator covers all 15 with ≤2 holes and ≤2 collisions) ≈ 0.25.**
**P(the right answer is a hybrid: generator-as-source, list-as-cache) ≈ 0.55.**

Reasoning for the prior: every soul file in `souls/canonical/` follows a
fixed schema (`traits` 5–6 keywords, `best_stages` 5–6 task tags, an
`inspired_by` namesake, a status). That schema is itself a half-built
generator — author was already parameterizing, just not closing the loop.
That nudges the prior up. The pressure down is that **named human
archetypes carry connotative cargo** (Curie ≠ "experimental rigor on a
stick" — she carries grind, isolation, partner-loss persistence) that a
parameterized cell in a grid would lose. So I expect a generator to
**cover the cognitive axes** but **fail to capture what makes each soul
sticky in practice.** That's why my prior leans toward hybrid, not
replacement.

I don't expect a clean fail. I don't expect a clean fit. I expect to find
~3 axes that explain ~6 of the 8, with 2 leftovers that resist
parameterization — and the resistance itself will be the finding.

## 2. Method

Surveyed (in order):

1. **All 15 soul files** under `souls/canonical/` (8) and
   `souls/experimental/` (7), reading YAML frontmatter as the load-bearing
   evidence of unconscious parameterization.
2. **`souls/elo.md`** for measured signal.
3. **Quorum / trip-wire spec** at
   `docs/superpowers/specs/2026-04-19-dogfood-debt-ledger-design.md`
   lines 260–296 for promotion/demotion mechanics in current play.
4. **External taxonomies via WebSearch + WebFetch:**
   - Big Five / FFM (lexical hypothesis origin, dimensionality argument).
   - HEXACO (six-factor extension; cross-cultural replicability debate).
   - MBTI vs FFM (categorical-vs-dimensional critique; the 75%
     re-test inconsistency finding).
   - Ekman 6 vs Plutchik wheel vs Russell circumplex (basic-emotions vs
     dimensional valence-arousal — the "how many slots" debate).
   - Anthropic's persona-vector paper (Chen et al., July 2025; arXiv
     2507.21509) — *the* directly-relevant prior art for "souls as
     directions in latent space."
   - CrewAI (hand-defined per-agent roles) vs AutoGen (emergent
     conversational agents) — current production agent frameworks.
   - Belbin 9 team roles (3-category meta-grouping: Action / People /
     Cerebral).

Skipped: IPIP-NEO item-bank specifics (would deepen #1 without
changing the structural finding), Big Five language-corpus replications
(volume is the point but adds no axis), DnD/Jungian/MBTI 16 types as
generators (categorical, recovered as a critique target).

## 3. Generator-fit attempt — the centerpiece

### 3.1 Axes that actually appear in the existing files

Reading the YAML across all 15 souls, four axes recur in the `traits`
and `best_stages` fields. I name them by what the *files themselves* are
parameterizing over:

| Axis | Values (empirically observed) |
|---|---|
| **A. Time-relation to artifact** | `pre-build` (spec/design) / `build` (implement) / `post-build` (review/cleanup/incident) |
| **B. Cognitive mode** | `divergent` (generate options) / `convergent` (prune to one) / `measure` (evidence-gather) |
| **C. Object scope** | `local` (a function, a PR) / `system` (a service, a pipeline) / `meta` (orchestration, the swarm itself) |
| **D. Failure stance** | `prevent` (ban illegal states) / `survive` (assume failure happens) / `learn` (extract signal from failure) |

These are not invented — they fall out of grouping the 15 sets of
`best_stages` tags. `algorithm_design`, `correctness_review`, and
`refactor_to_invariant` (Turing) all collapse to (B=convergent,
C=local/system, D=prevent). `incident_response`, `postmortem_authorship`,
`rollback_decisions` (Hamilton) all collapse to (A=post-build, D=survive).
The axes are descriptive, not prescriptive.

### 3.2 The grid — placing all 15 souls

| Soul | A. Time | B. Mode | C. Scope | D. Failure |
|---|---|---|---|---|
| **da Vinci** | pre-build | divergent | system | prevent |
| **Lovelace** | pre-build | divergent | meta | prevent |
| **Curie** | build | measure | system | learn |
| **Knuth** | build | convergent | local | prevent |
| **Turing** | pre-build | convergent | local/system | prevent |
| **Shannon** | build | measure | system | learn |
| **Sun Tzu** | pre-build | convergent | meta | prevent |
| **Socrates** | post-build | convergent | system | prevent |
| Dijkstra | pre-build | convergent | local/system | prevent |
| Feynman | build | convergent | local | prevent |
| Hamilton | post-build | convergent | system | survive |
| Hopper | post-build | convergent | system | prevent |
| Jared Pleva | build | convergent | system | prevent |
| Jobs | pre-build | convergent | system | prevent |
| Jokić | build | divergent | meta | prevent |

### 3.3 What the grid actually shows

**Genuine coverage wins (the axes earn their keep):**

- **Curie vs Shannon** are tightly adjacent on (build / measure / system /
  learn). They differ only in *what* they measure — Curie measures
  experiments-vs-controls, Shannon measures channel SNR. The axes
  predict this adjacency, and the human eye agrees: these two souls
  are most likely to be confused in routing decisions.
- **da Vinci vs Lovelace** are differentiated only by axis C (system vs
  meta). That matches lived experience: da Vinci sketches the pipe;
  Lovelace asks what alphabet the pipe is written in. The axes capture
  the difference cleanly.
- **Hamilton is the only canonical-or-experimental soul on D=survive.**
  Every other soul assumes the system can be made correct. Hamilton
  alone says "no, the system will fail; design for that." The axis
  surfaces this as a singleton — which is itself a finding (see §5).
- **Knuth and Turing collapse onto the same cell.** (build/convergent/
  local-system/prevent) for both. Re-reading both files: Knuth is
  *implementation* discipline (boundaries, naming, sort tie-breakers);
  Turing is *specification* discipline (invariants, reductions,
  state-machine framing). The axes can't tell them apart — but the
  files can. **This is a generator failure, and a real one.**

**Genuine coverage failures (where the axes break):**

- **The Knuth/Turing collision** above: real difference (impl vs spec
  rigor), invisible in the 4 axes. Either the generator needs a fifth
  axis ("rigor target": code / spec / both), or these two are evidence
  that named-archetype distinctions carry information the parameter
  space doesn't.
- **Jobs and Hopper** both land at (post-build/convergent/system/prevent)
  if you squint, even though Jobs is "taste pruning of options" and
  Hopper is "deletion of dead code." Both *delete*; the axes can't see
  what they're deleting *for* (taste vs hygiene).
- **Socrates** is hard to place on axis A. The file says "preflight, code
  review, design review" — that's pre-build, build-time, *and*
  post-build. Socrates is genuinely time-orthogonal: he runs on whatever
  artifact exists. The axes treat time as a primary discriminator;
  Socrates is evidence that one soul can be *spread across* it. That's a
  schema-mismatch, not a coverage failure — but it's worth naming.
- **Jokić on (build/divergent/meta/prevent)** is the only soul I had to
  argue with myself about. Jokić is patient orchestration; "divergent"
  is the wrong word but "convergent" is also wrong. He *defers
  convergence*. The two-value axis B is too coarse — there's a third
  value, "hold open," that Jokić uniquely occupies.

### 3.4 Holes (cells with zero souls)

Of 3×3×3×3 = 81 nominal cells, the 15 souls populate ~13 distinct cells
(Knuth=Turing, Curie=Shannon, Jobs≈Hopper as collisions). Many "holes"
are nonsensical (e.g., post-build / divergent / local / survive — what
work is that?), so a raw 81-minus-13 count overstates the empty space.

But three holes are **genuinely interesting**:

| A | B | C | D | What this would be |
|---|---|---|---|---|
| post-build | divergent | meta | learn | "Mining policies from past incidents to *generate* new dispatch rules" — a Sentinel-shaped soul. Currently nobody owns this. |
| pre-build | measure | system | prevent | "Empirical risk assessment before commit — Fermi-estimate the blast radius before allowing the change." Closest is Socrates+Curie, but neither is shaped for it. |
| build | convergent | meta | survive | "Defensive orchestration — subagent dispatch that assumes any subagent may silently fail." Hamilton's failure-stance + Jokić's orchestration scope. Currently no canonical or experimental soul. |

These three holes are **predictions the generator makes that the
existing canonical+experimental set doesn't fill**. That's the Note G
test passing — the abstraction is producing reach beyond the input.

## 4. Comparison to other generative taxonomies

### 4.1 Big Five / HEXACO

The lexical-hypothesis story is directly analogous. Allport-Odbert
extracted 4504 trait adjectives from Webster's dictionary (1936); Cattell
factor-analyzed them down to ~16 clusters; later work compressed to 5.
HEXACO argued for 6 from larger cross-language adjective sets.

**Borrowable:** the *method* — start from the pile of trait words
already in use (the YAML `traits` field across 15 souls is exactly this
adjective list, in miniature), factor-analyze, see what falls out.
Chitin has the empirical material to do this; nobody has run the factor
analysis.

**Explicitly reject:** the **single global axis-set assumption.** The
Big Five / HEXACO argue for one universal personality space because
their target is *describing humans across cultures.* Chitin's target is
*routing cognitive work to the right lens.* Those are different jobs.
The "right" axes for Chitin are work-shaped (time-to-artifact, failure
stance, scope), not human-shaped (extraversion, agreeableness). Trying
to import OCEAN dimensions wholesale would be a category error.

### 4.2 MBTI vs FFM — the categorical critique

MBTI's killer flaw, from the literature: *up to 75% of test-takers get a
different 4-letter type on retest*, because the categories force a binary
split through the middle of a continuous distribution. Someone at the
51st percentile on T-vs-F gets the same letter as someone at the 95th,
and the next-week 49th-percentile result flips the label.

**Direct analogue for chitin:** if the canonical set is
"named-archetype-only," then assigning a soul to a piece of work is
exactly an MBTI-style discrete assignment. The recent quorum vote on
Knuth-vs-Turing for Phase B is evidence of this — the two souls landed
in the same cell of my generator grid, and the quorum had to do
qualitative tie-breaking. That's the discrete-categorical tax MBTI pays.

**Borrowable:** dimensional intensities. A soul *vector* (mode=0.7
convergent + 0.3 measure, scope=0.8 system + 0.2 meta) survives the
51st-percentile problem the way MBTI doesn't.

**Explicitly reject:** abandoning names. The MBTI critique is about
*forcing categorical answers onto continuous data*, not about *whether
named archetypes are useful as cached high-density compressions.*
Names are still doing real load-bearing work (see §6).

### 4.3 Ekman 6 vs Plutchik wheel vs Russell circumplex

The "how many basic emotions" debate is the closest structural analogue
to "how many basic souls." Three positions:

- **Ekman:** 6 named basics (happiness, sadness, anger, disgust,
  surprise, fear). Discrete, universal facial-expression-grounded.
- **Plutchik:** 8 primaries arranged in a wheel with intensity gradients
  and blend-products (joy + trust = love). **A generator that produces
  named instances.**
- **Russell:** 2-axis circumplex (valence × arousal). Pure dimensional;
  no named primaries at all.

Recent empirical work (Cowen-Keltner; Nature Reports 2023) found
Ekman's 6 inadequate for real-life emotion annotation, with low
within-subject agreement. The dimensional approaches scale better but
lose the connotative cargo of named emotions.

**This trichotomy maps directly onto chitin's choice:**

- Ekman shape = current "canonical 8 named souls" approach.
- Plutchik shape = **the hybrid this document is pointing toward** —
  named primaries (souls), arranged on axes, with composition rules
  ("Curie + Shannon = empirical observability"; "Knuth + Turing =
  formal-then-implementation pairing").
- Russell shape = pure persona-vector approach (§4.4) — no names, just
  coordinates in latent space.

Plutchik is the closest fit. He has 8, named, and *explicit
generative axes*. Worth reading the Plutchik wheel directly when the
re-quorum convenes.

### 4.4 Persona vectors (Anthropic, July 2025)

This is the prior art the soul system has to reckon with. Findings from
Chen et al. (arXiv 2507.21509) and the Anthropic blog post:

- Personality traits in LLMs are encoded as **linear directions in
  activation space** ("persona vectors").
- Extraction is **automated from a natural-language description** —
  given the word "evil" plus contrasting prompts, the vector falls out
  of activation differencing.
- Vectors can be **injected at varying intensities** during inference
  (continuous, not discrete).
- The Linear Representation Hypothesis (cited in subsequent work)
  suggests the model's "personality" is geometrically distinct from its
  "reasoning" — they live in orthogonal subspaces.

**What this means for chitin:**

- A "soul" in chitin is currently a *prompt prefix* (the `## Active
  Soul: X` block). A soul-as-persona-vector would be an *activation
  steering vector*, applied at inference time, parameterized by axis.
- **The persona-vector method is a working generator.** Given the
  4-axis schema in §3.1 and a contrasting-prompt setup, you could
  extract a vector for each cell, including the empty ones.
- This is the technically strongest case for the "generator beats
  list" position — it's been demonstrated to work, in production-grade
  research, on the same model family chitin runs on.

**Borrowable:** the contrastive-extraction method. Even without
modifying activations, a Plutchik-shaped soul library could use this to
*generate* the prompt prefix for a cell that currently has no named
soul.

**Explicitly reject (or at least defer):** activation-level steering as
a near-term move. Chitin's runtime is the Claude API; activation
steering isn't an API surface available to us. The vector idea is real;
the implementation is several layers of abstraction below where chitin
sits today.

### 4.5 CrewAI (hand-listed) vs AutoGen (emergent)

Production agent frameworks split exactly on this question:

- **CrewAI:** roles are pre-defined ("Researcher", "Writer",
  "Validator"), each agent gets a hand-crafted prompt template. **This
  is the canonical-list approach.**
- **AutoGen:** roles emerge from conversation; agents decide who speaks
  when. **This is closer to "the generator is the conversation, no
  fixed list at all."**

Both ship. Both have users. Neither is winning by attrition. That's
informative: the field hasn't converged on parameterized-vs-listed
because **they solve different problems.** CrewAI's value is
predictability and audit (roles are inspectable); AutoGen's value is
flexibility (novel role compositions emerge).

**Chitin's current shape is CrewAI-like.** The 8 canonical souls are
hand-defined templates. The question this doc is asking is whether
chitin should drift toward AutoGen-like generation.

### 4.6 Belbin 9 team roles

Belbin's 9 are clustered into 3 meta-groups (Action / People /
Cerebral), 3 roles each. **That's a 3×3 generator hiding inside a
hand-list of 9.** The grouping wasn't original-author intent; it was
discovered post-hoc through factor analysis of his observational data.

**Direct lesson for chitin:** the canonical 8 might *also* have a
hidden grouping that nobody has named yet. The 4-axis grid in §3.2 is
one attempt; there may be a simpler 2-axis grouping (perhaps Time × Mode,
collapsing C and D as secondary discriminators) that does most of the
work. Worth running the factor analysis.

## 5. Ripple analysis

What does the proposed 4-axis generator predict that the existing 15
souls don't fill?

**Predicted holes (from §3.4):**

1. **(post-build / divergent / meta / learn)** — A "policy miner from
   past incidents." Not Sentinel itself (which is the substrate); the
   *cognitive lens* that says "look at the ledger, generate three new
   invariants we should be checking." No current soul owns this.
2. **(pre-build / measure / system / prevent)** — "Pre-flight empirical
   risk estimation." Curie does measure-after; Socrates does
   challenge-before; nobody does *measure-before-commit-to-model the
   blast radius.* Closest external analog: Fermi estimation in physics.
3. **(build / convergent / meta / survive)** — "Defensive subagent
   dispatch." Hamilton's stance + Jokić's scope. Currently the swarm
   assumes subagents complete; the "what if 1-of-N silently drops" lens
   has no owner.

**Predicted collisions (cells with multiple souls in them):**

1. **Knuth ≡ Turing** at (build/convergent/local-system/prevent). Real
   difference (impl rigor vs spec rigor) is invisible to the axes. Two
   options: add a fifth axis ("rigor target": code / spec / both), or
   accept that the axes are 90% but not 100% — names carry the
   remainder.
2. **Curie ≈ Shannon** at (build/measure/system/learn). Real difference
   (experimental loop vs channel SNR). Same options.
3. **Jobs ≈ Hopper** at (post-build/convergent/system/prevent). Real
   difference (taste vs hygiene). Same options.

**The pattern in the collisions is real and informative:** every
collision pair is doing the same *operation* on a different *target*.
Knuth/Turing both do convergent rigor — on code vs on spec. Curie/Shannon
both measure — experiments vs channels. Jobs/Hopper both prune — for
taste vs for cruft. So the missing axis is **target object**, with
values (code, spec, experiment, channel, options, dead-state). Adding it
resolves all three collisions but blows the grid up to 3×3×3×3×6 = 486
cells, of which maybe 20 are populated. That's a generator with too
much air in it.

**Better diagnosis:** axis B ("Cognitive mode") is under-resolved.
"Convergent" hides at least three sub-modes (rigor, taste, prune) and
"divergent" hides at least two (analogize, generate). Refining B from
3 values to ~6 values would dissolve the collisions without exploding
the other axes. Net cell count goes from 81 to ~162; populated cells
~15 stays the same; coverage ratio drops from 19% to 9%, which is
about right for a generator that's predictive rather than redundant.

## 6. Open questions for the re-quorum

The decision rule from the brief:

> Is "canonical 8" the right shape of question, or is the right
> question "what's the generator and how many slots does it produce?"

**My answer (one paragraph, as the rule asks):** Neither alone. The
4-axis generator covers ~80% of the canonical 8 cleanly, predicts 3
real holes (no Sentinel-shaped lens, no pre-flight Fermi lens, no
defensive-orchestration lens), and surfaces 3 collisions
(Knuth=Turing, Curie≈Shannon, Jobs≈Hopper) that point to a missing
sub-axis on cognitive-mode. *But* the named archetypes carry
connotative cargo (Curie's grind, Jobs's taste, Hamilton's 1202 calm)
that the parameterized cells lose, and the persona-vector / Plutchik
literature both support a hybrid: **generator-as-source-of-truth for
the axes; named souls as a curated cache of high-traffic cells, with
permission to add more cells when the generator predicts a useful one
that nobody has named yet.** The right shape of the next move is (c)
hybrid, not (a) refine or (b) replace.

**Specific questions to put to the re-quorum:**

1. **Run the factor analysis.** The YAML `traits` and `best_stages`
   fields across 15 souls are 75–90 trait adjectives + 75–90 task tags.
   That's enough material for a small-N factor analysis. Question: does
   the same 4 axes (or the refined 5) fall out, or does the data
   surface a different basis?
2. **Decide whether the empty cells are real predictions or
   nonsense.** Three candidate holes named in §5. Question: does the
   quorum recognize any of them as "yes, that's a real lens we've been
   missing"? If yes for ≥1, the generator is paying rent.
3. **Resolve the Knuth/Turing collision.** Either add the missing
   sub-axis (rigor-target), or formally agree that they're a *pair*
   (always co-active for boundary work) and merge the cells. The
   2026-04-19 quorum already routed Phase B implementation to Knuth
   *because* Turing was upstream — that handoff pattern is the pair
   relationship, undocumented.
4. **Decide on persona-vectors as a future direction.** The Anthropic
   work is real and the math runs. Question: is chitin's roadmap
   ambitious enough to want activation-level steering eventually, or
   is the soul-as-prompt-prefix abstraction sufficient indefinitely?
5. **Plutchik or Russell.** If the answer is hybrid, which hybrid?
   Plutchik (named primaries on a wheel + composition rules) is
   the closer fit to the existing schema; Russell (pure dimensional)
   is closer to persona-vectors. The choice constrains tooling.
6. **Sticky default question for the hybrid.** The current sticky
   default is da Vinci. In a parameterized world, the default is a
   *cell* (pre-build / divergent / system / prevent), and "da Vinci" is
   the cache name for that cell. Does that make da Vinci-as-default
   stronger (because the cell is justified by axes, not by vibe) or
   weaker (because the cell could be filled by any soul that lands in
   it)?

---

**Lovelace closing note (against my own grain):** the canonical 8 may
yet be the right shape of question — a generator without a use case is
just a wider list. If the re-quorum looks at the three predicted holes
in §5 and says "we've never wanted any of those," then the named-
archetype list is doing the work and the axis exercise was decoration.
The Note G test is the empirical one: do the predicted holes feel like
*missing* lenses, or like *fictional* lenses? I don't get to answer
that — the quorum does.

## Sources

- Big Five / lexical hypothesis: [Wikipedia: Big Five personality traits](https://en.wikipedia.org/wiki/Big_Five_personality_traits); [Goldberg 1990](https://projects.ori.org/lrg/pdfs_papers/goldberg.big-five-factorsstructure.jpsp.1990.pdf); [Five Factor Model update (PMC)](https://pmc.ncbi.nlm.nih.gov/articles/PMC6732674/).
- HEXACO vs Big Five: [Thielmann et al. 2022](https://journals.sagepub.com/doi/10.1177/08902070211026793); [Wikipedia: HEXACO](https://en.wikipedia.org/wiki/HEXACO_model_of_personality_structure).
- MBTI categorical critique: [Big Five vs MBTI: No Contest (PCI)](https://pciassess.com/big-five-better-than-mbti/); [Dimensional models of personality (PMC)](https://pmc.ncbi.nlm.nih.gov/articles/PMC3811085/); [Myers-Briggs Type Indicator: Pseudoscience? (HumanPerformance)](https://humanperformance.ie/myers-briggs-type-indicator-pseudoscience/).
- Ekman / Plutchik / Russell: [Wikipedia: Emotion classification](https://en.wikipedia.org/wiki/Emotion_classification); [Experiments on real-life emotions challenge Ekman's model (Nature Sci Reports 2023)](https://www.nature.com/articles/s41598-023-36201-5).
- Persona vectors: [Anthropic research blog](https://www.anthropic.com/research/persona-vectors); [Chen et al. arXiv 2507.21509](https://arxiv.org/abs/2507.21509); [The Geometry of Persona arXiv 2512.07092](https://arxiv.org/html/2512.07092).
- CrewAI vs AutoGen: [CrewAI docs (Agents)](https://docs.crewai.com/en/concepts/agents); [CrewAI vs AutoGen (ZenML)](https://www.zenml.io/blog/crewai-vs-autogen); [Mastering Agents (Galileo)](https://galileo.ai/blog/mastering-agents-langgraph-vs-autogen-vs-crew).
- Belbin 9 roles: [Belbin Team Roles (official)](https://www.belbin.com/about/belbin-team-roles); [Aritzeta et al. 2007 (Wiley)](https://onlinelibrary.wiley.com/doi/abs/10.1111/j.1467-6486.2007.00666.x).
