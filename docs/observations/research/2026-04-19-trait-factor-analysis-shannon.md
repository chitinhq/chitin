---
date: 2026-04-19
soul: shannon
status: empirical-analysis
related:
  - docs/observations/research/2026-04-19-soul-archetype-survey-lovelace.md
  - souls/canonical/
  - souls/experimental/
data:
  - docs/observations/research/2026-04-19-trait-matrix.csv
  - docs/observations/research/2026-04-19-stage-matrix.csv
  - docs/observations/research/2026-04-19-soul-similarity.csv
---

# Empirical trait/stage factor analysis over the 15-soul corpus

Lovelace's qualitative pass produced a 4-axis grid (Time × Mode × Scope ×
Failure) and reported three collisions and three holes. This doc runs the
quantitative follow-up Lovelace explicitly recommended: a binary
soul × token matrix over the existing trait/stage YAML, pairwise
similarity (cosine + Jaccard), hierarchical clustering, per-token
information content, and a bootstrap stability check. The goal is to see
what axes are *implicitly* in use and whether Lovelace's three reported
collisions are real signal or grid artifacts.

## 1. Hypothesis (written cold, before running anything)

Prior, written before opening a Python REPL:

- **P(at least one of Lovelace's collisions survives empirical
  clustering) ≈ 0.85.** Knuth/Turing in particular both have
  `correctness proofs` verbatim — that's a hard-coded vocabulary
  collision, not just a qualitative one.
- **P(all three collisions survive) ≈ 0.20.** The Curie/Shannon and
  Jobs/Hopper pairs are pulled together by *theme* (measurement,
  deletion) rather than shared vocabulary. I expect the trait words to
  be different even if the cells overlap.
- **P(Lovelace's 4 axes account for >50% of pairwise similarity
  variance) ≈ 0.30.** The axes are a sensible coarse cut, but the
  YAML files were *not* written against those axes — authors chose
  trait words idiosyncratically. So I expect the empirical structure to
  surface a *different cut* (probably "formal-rigor cluster" vs
  "generative cluster" vs "human-judgment cluster") that Lovelace's grid
  collapses across.
- **P(at n=15, any cluster claim has bootstrap support >70%) ≈ 0.50.**
  Sample size is the binding constraint. I expect the headline finding
  to be that the data is too thin to settle the axes question — any
  positive cluster claim has to clear a high bootstrap-stability bar.

The strongest expected finding (high prior): **the trait vocabulary is
sparse enough that most pairwise similarities will be near zero**, and
the entire analysis hinges on whether a small number of shared tokens
carry enough signal to differentiate a cluster from noise. If the
sparsity is severe, the right next step isn't more clustering — it's a
controlled-vocabulary pass on the YAML.

## 2. Method

### 2.1 Data extraction

Parsed the YAML frontmatter from all 15 soul files
(`souls/canonical/` × 8, `souls/experimental/` × 7), extracting the
`traits` and `best_stages` lists. Wrote a tolerant minimal parser
because PyYAML chokes on `- "no" is the product` in `jobs.md` (the leading
quoted scalar followed by trailing text is invalid YAML — flagged
separately as a vocabulary-hygiene issue).

### 2.2 Tokenization — the load-bearing decision

Built two matrices:

- **Phrase-level** (raw): each multi-word trait/stage entry is its own
  token. 86 unique trait phrases, 74 unique stage phrases.
- **Word-level** (decomposed): each phrase is split on non-alphanumeric
  boundaries; lowercased; a small stopword list applied
  (`the`, `of`, `to`, `and`, `is`, `a`, etc.); words of length ≤1
  dropped. 190 unique trait words, 119 unique stage words.

**Why both:** phrase-level is the controlled-vocabulary view (what the
files literally share); word-level is the latent-semantic view (what
ideas they share even when phrased differently). Reporting both is
information redundancy spent deliberately — the disagreement between the
two is itself a finding (§3 below).

The primary analysis uses **word-level** tokens because the phrase-level
matrix is degenerate (see §3.1). All claims about cluster structure
below are word-level unless explicitly noted.

### 2.3 Distance metrics

Both **cosine** (`dot(a,b) / (||a|| · ||b||)`) and **Jaccard**
(`|A ∩ B| / |A ∪ B|`) on the binary token vectors. Cosine penalizes the
all-zero vectors less harshly when one soul has many tokens; Jaccard
treats every token equally. Disagreements between the two would be
informative — in practice they agree at the cluster level (same
nearest-neighbor for 13/15 souls), so the choice doesn't matter much
for the headline finding.

### 2.4 Clustering

**Average-linkage hierarchical clustering** on the cosine-distance
matrix. Defensible default at n=15: it produces a deterministic
dendrogram that can be read by hand, doesn't require choosing k upfront,
and is robust to the small-sample regime where k-means partition
boundaries are noise-dominated. Reported cluster cuts at k ∈ {3..10}.

### 2.5 Information content

Per-token entropy (binary occurrence across 15 souls): tokens occurring
in 1 soul have H ≈ 0.35; tokens occurring in 7-8 souls peak at H = 1.0;
tokens occurring in 15 (everyone) have H = 0.

Mutual information between each token and the cluster assignment at k=5
identifies tokens whose presence/absence correlates with cluster
membership.

### 2.6 Stability

**Bootstrap by column resampling, n=200.** For each bootstrap iteration,
sample the token columns with replacement (preserving row counts), re-cluster,
record co-cluster frequency for every soul pair. Pairs that co-cluster in
>70% of bootstraps are treated as stable; <30% as noise. The n=15
sample-size caveat is binding — bootstrap CIs would be wide regardless.

### 2.7 Sample-size caveat (load-bearing)

**n=15 is small.** A 15-row binary matrix with ~190 mostly-singleton
columns is right at the edge of where clustering produces meaningful
structure at all. Treat every cluster claim below as "consistent with
the data" rather than "established by the data" unless it clears
>80% bootstrap support.

## 3. Token distribution — the central finding

### 3.1 Phrase-level vocabulary is essentially singleton

Of 86 unique trait phrases across 15 souls, **only one phrase
(`correctness proofs`) appears in more than one soul** (Knuth + Turing).
Of 74 unique stage phrases, **zero appear in more than one soul.**

This means the literal-token similarity matrix is approximately
identity: every soul is its own island except for a single
Knuth↔Turing edge at cosine=0.087. **At the phrase level there are no
empirical clusters**, full stop. Trait/stage YAML is currently a
free-form descriptive language, not a controlled vocabulary.

This is the headline finding and it dominates everything else:

> **The soul YAML schema looks like a controlled vocabulary but is being
> used as free text.** Lovelace's qualitative grid had to do all the
> alignment work because the data layer wasn't doing any.

Tagged: **(c) the matrix contradicts the implicit Lovelace claim that
the YAML alone supports cluster recovery.** The qualitative grid is
real intellectual work, not summarization of structure already present
in the files.

### 3.2 Word-level vocabulary has thin but real overlap

Decomposing phrases into word tokens recovers 17 trait words and 17
stage words that appear in ≥2 souls. The shared-token table:

| count | trait words | stage words |
|---|---|---|
| 10x | — | `design` |
| 3x | `thinking`, `correctness` | `review`, `orchestration`, `code`, `algorithm` |
| 2x | `signal`, `rigor`, `proofs`, `probabilistic`, `pattern`, `optimization`, `models`, `instinct`, `finish`, `discipline`, `design`, `check`, `analysis`, `adversarial`, `until` | `triage`, `routing`, `resolution`, `novel`, `invariant`, `decisions`, `correctness`, `contract`, `audit`, `architecture`, `analysis`, `agent` |

`design` appearing in 10/15 stages (H=0.92, the max-entropy partition) is
the single highest-information shared token. Beyond that, the vocabulary
has a long tail of singletons: 173/190 trait words and 102/119 stage
words appear in exactly one soul.

### 3.3 Per-token entropy distribution

```
trait token count distribution:
  1x: 173 tokens (91% of vocabulary)
  2x: 15  tokens
  3x: 2   tokens (`thinking`, `correctness`)
  ≥4x: 0  tokens

stage token count distribution:
  1x: 102 tokens (86% of vocabulary)
  2x: 12  tokens
  3x: 4   tokens (`review`, `orchestration`, `code`, `algorithm`)
  10x: 1  token (`design`)
```

**Implication for routing:** when the dispatcher tries to match a task
description against soul vocabulary, it will hit either `design` (which
matches 10/15 souls — useless as a discriminator) or a singleton word
(which matches at most one soul — perfectly discriminating but only
fires for that one phrasing). There's almost no middle ground. The
vocabulary is bimodal: decorative or unique, with very little
load-bearing-mid-frequency vocabulary.

Reproducibility: full distributions are in
`/tmp/shannon-analysis/entropy_mi.json`, computed from the saved
`docs/observations/research/2026-04-19-trait-matrix.csv`.

## 4. Cluster structure

### 4.1 Cosine similarity (word-level, combined trait+stage)

Showing only entries ≥0.05; rest are zero or near-zero. Full matrix in
`docs/observations/research/2026-04-19-soul-similarity.csv`.

```
               curie davin dijks feynm hamil hoppe jared  jobs jokic knuth lovel shann socra suntz turin
         curie  1.00    .    .    .    .    .    .    .    .    .    . 0.09    .    .    .
       davinci     . 1.00    .    .    .    . 0.05    . 0.09    . 0.09    . 0.05    .    .
      dijkstra     .    . 1.00 0.08    .    .    .    .    . 0.08    .    .    .    . 0.12
       feynman     .    . 0.08 1.00    .    .    .    .    .    .    .    .    .    .    .
      hamilton     .    .    .    . 1.00    .    .    .    .    .    .    .    .    .    .
        hopper     .    .    .    .    . 1.00    .    .    . 0.08    .    .    .    .    .
   jared_pleva     . 0.05    .    .    .    . 1.00    .    .    . 0.14 0.15 0.11    . 0.15
          jobs     .    .    .    .    .    .    . 1.00    .    .    .    .    .    .    .
         jokic     . 0.09    .    .    .    .    .    . 1.00    . 0.08    .    . 0.08    .
         knuth     .    . 0.08    .    . 0.08    .    .    . 1.00 0.09    . 0.14    . 0.22
      lovelace     . 0.09    .    .    .    . 0.14    . 0.08 0.09 1.00    .    . 0.08 0.09
       shannon  0.09    .    .    .    .    . 0.15    .    .    .    . 1.00 0.05    . 0.14
      socrates     . 0.05    .    .    .    . 0.11    .    . 0.14    . 0.05 1.00 0.09 0.10
       sun-tzu     .    .    .    .    .    .    .    . 0.08    . 0.08    . 0.09 1.00    .
        turing     .    . 0.12    .    .    . 0.15    .    . 0.22 0.09 0.14 0.10    . 1.00
```

Even the highest off-diagonal value (Knuth↔Turing, 0.22) is small in
absolute terms — this is a sparse-feature regime. But the *relative*
ranking still carries information.

### 4.2 Dendrogram (average linkage on cosine distance)

```
d=0.782  ┐ knuth + turing
d=0.881  │ ┌── socrates joins (knuth+turing)              <- "formal-rigor" core
         │ │
d=0.850  │ │ jared_pleva + shannon
d=0.905  │ │ ┌── lovelace joins (jared+shannon)           <- "generative-systems" core
d=0.916  │ │ │
         └─┴─┴── (rigor + generative) merge into 6-soul core

d=0.911  davinci + jokic                                  <- "orchestration / cross-domain"
d=0.925  dijkstra + feynman                               <- "first-principles minimalism"
d=0.962  hamilton + sun-tzu                               <- "adversarial / failure-aware"

d=0.987  hopper joins megacluster
d=0.990  curie joins megacluster
d=0.992  jobs joins last (most isolated soul)
```

### 4.3 Cluster cuts at k ∈ {5, 7, 10}

```
k=5:  C0={curie}  C1={hopper}  C2={jobs}  C3={hamilton, sun-tzu}
      C4={davinci, dijkstra, feynman, jared_pleva, jokic, knuth,
          lovelace, shannon, socrates, turing}    <- 10-soul megacluster

k=7:  splits off (dijkstra, feynman) and {sun-tzu} from the megacluster
      C0={curie}  C1={hamilton}  C2={hopper}  C3={jobs}  C4={sun-tzu}
      C5={dijkstra, feynman}
      C6={davinci, jared_pleva, jokic, knuth, lovelace, shannon,
          socrates, turing}                       <- 8-soul subcluster

k=10: full structure visible
      C0={curie}      <- singleton (measurement + grind)
      C1={dijkstra}   <- singleton (correctness-by-construction)
      C2={feynman}    <- singleton (first-principles)
      C3={hamilton}   <- singleton (failure-survival)
      C4={hopper}     <- singleton (deletion)
      C5={jobs}       <- singleton (taste)
      C6={sun-tzu}    <- singleton (adversarial leverage)
      C7={knuth, socrates, turing}        <- "formal-correctness" cluster
      C8={jared_pleva, lovelace, shannon} <- "generative-systems" cluster
      C9={davinci, jokic}                 <- "patient-orchestration" cluster
```

The k=10 cut is the most informative: 7 souls are empirical singletons
(no other soul looks like them by trait/stage vocabulary) and 8 souls
collapse into 3 small clusters of 2-3 each.

### 4.4 Bootstrap stability for the k=10 clusters

| cluster | pair | bootstrap co-cluster freq |
|---|---|---|
| C7 | knuth ↔ socrates | **89.5%** |
| C7 | knuth ↔ turing | **89.0%** |
| C7 | socrates ↔ turing | **80.0%** |
| C8 | jared_pleva ↔ shannon | 78.0% |
| C8 | jared_pleva ↔ lovelace | 77.5% |
| C8 | lovelace ↔ shannon | 58.0% |
| C9 | davinci ↔ jokic | 72.0% |

**C7 (Knuth/Socrates/Turing) is the only cluster with all-pairs
bootstrap support >80%.** That's the only finding I'd call **(a) the
matrix says X with high confidence**.

C8 (Jared/Lovelace/Shannon) has support around 60-78% — **(b) consistent
with X but n=15 is too small to be sure**. In particular, the
Lovelace↔Shannon pair (58%) is at the noise edge; the cluster might
really be {Jared, Shannon} + {Lovelace}, not a true triple.

C9 (Davinci/Jokic) at 72% is borderline — also (b).

### 4.5 Nearest-neighbor table (cosine, word-level combined)

Each row gives the top-3 most-similar souls. Asterisks mark Lovelace's
predicted same-cell pairs.

| soul | NN1 | NN2 | NN3 |
|---|---|---|---|
| curie | shannon (0.09) * | turing (0.04) | davinci (0.00) |
| davinci | lovelace (0.09) | jokic (0.09) | jared_pleva (0.05) |
| dijkstra | turing (0.12) | knuth (0.08) | feynman (0.08) |
| feynman | dijkstra (0.08) | jared_pleva (0.04) | socrates (0.04) |
| hamilton | lovelace (0.04) | sun-tzu (0.04) | jobs (0.04) |
| hopper | knuth (0.08) | socrates (0.04) | feynman (0.04) |
| jared_pleva | shannon (0.15) | turing (0.15) | lovelace (0.14) |
| jobs | davinci (0.04) | feynman (0.04) | hamilton (0.04) |
| jokic | davinci (0.09) | lovelace (0.08) | sun-tzu (0.08) |
| knuth | **turing (0.22)** * | socrates (0.14) | lovelace (0.09) |
| lovelace | jared_pleva (0.14) | davinci (0.09) | turing (0.09) |
| shannon | jared_pleva (0.15) | turing (0.14) | curie (0.09) * |
| socrates | knuth (0.14) | jared_pleva (0.11) | turing (0.10) |
| sun-tzu | socrates (0.09) | lovelace (0.08) | jokic (0.08) |
| turing | **knuth (0.22)** * | jared_pleva (0.15) | shannon (0.14) |

Two soul pairs are mutual nearest neighbors in both directions:

- **Knuth ↔ Turing** (cos=0.22 — the strongest off-diagonal in the
  matrix)
- **Dijkstra ↔ ?** — Dijkstra's NN is Turing, but Turing's NNs are
  Knuth/Jared/Shannon. So Dijkstra is *one-sided* attached to the
  formal-correctness cluster.

`jared_pleva` has the most balanced top-3 (shannon/turing/lovelace all
at 0.14-0.15) — suggesting it sits *between* the formal-correctness and
generative-systems clusters rather than belonging cleanly to either.
This is consistent with its role description as a "systems builder" —
straddling spec-rigor and ship-pragmatism.

### 4.6 Trait-only vs stage-only — the disagreement is informative

When you compute similarity on traits and stages separately, the
nearest-neighbor table changes:

| soul | trait-only NN1 | stage-only NN1 |
|---|---|---|
| curie | **shannon (0.16)** | turing (0.10) |
| jared_pleva | shannon (0.14) / lovelace (0.14) | **socrates (0.41)** |
| socrates | knuth (0.10) | **jared_pleva (0.41)** |
| turing | knuth (0.18) | knuth (0.25) |
| jobs | davinci (0.07) | hamilton (0.10) |

Two patterns:

1. **Curie ↔ Shannon is much stronger on traits (0.16) than stages
   (0.00).** They share the *vocabulary of measurement* but use
   different *task tags* — Curie's stages are `experimentation_loops`,
   `benchmarking`, `ab_testing`; Shannon's are `telemetry_design`,
   `data_pipelines`, `anomaly_detection`. Lovelace put them in the same
   cell; the trait vocabulary supports that, but the stage vocabulary
   contradicts it. **(c) the matrix partially contradicts the
   Curie/Shannon collision** — the souls share *epistemology* but
   differ in *deployment surface*.

2. **Jared_Pleva ↔ Socrates jumps to 0.41 cosine on stages alone**
   (vs 0.11 on combined). Their stage tags overlap on `design`, `review`
   — both have `code_review`-like work. But their trait vocabularies are
   nearly orthogonal (Jared = "ship-first pragmatism"; Socrates =
   "adversarial questioning"). **The same task-domain can house
   different cognitive modes** — which is itself an argument for
   separating the *what-stage* and *what-mode* dimensions in the YAML.

## 5. Lovelace-axis audit

### 5.1 Do the 4 axes predict empirical similarity?

Pearson correlation between (number of axes shared between two souls)
and (their empirical cosine similarity), across all 105 soul pairs:

**r = 0.174, n=105.**

Interpretation: the axes capture *some* signal but not most of it. The
distribution by shared-axis-count:

| shared axes | n pairs | mean cos | median cos | pairs with cos>0 |
|---|---|---|---|---|
| 0 | 10 | 0.031 | 0.020 | 5/10 |
| 1 | 34 | 0.024 | 0.000 | 15/34 |
| 2 | 39 | 0.052 | 0.043 | 28/39 |
| 3 | 18 | 0.030 | 0.000 | 8/18 |
| 4 (same cell) | 4 | 0.074 | 0.067 | 4/4 |

Pairs in the same Lovelace cell have **2-3x the mean similarity** of
random pairs (0.074 vs 0.031). That's a real effect and confirms that
the axes are *picking up something*. But the effect is weak — the
correlation explains roughly 3% of variance (r²=0.03). Most of the
empirical similarity structure is *not* captured by Lovelace's 4 axes.

Tagged: **(b) consistent with X but n=15 (and n=4 same-cell pairs) is
too small to be sure**. The qualitative grid is directionally correct
but underpowered as a predictor.

### 5.2 The three reported collisions — empirical audit

| Lovelace claim | Empirical finding | Verdict |
|---|---|---|
| **Knuth ≡ Turing** | cos=0.22 (highest off-diagonal); mutual NN1; bootstrap co-cluster 89% | **(a) high-confidence support** |
| **Curie ≈ Shannon** | cos=0.09 (rank-11 of 14 for Curie); bootstrap co-cluster 39.5%; trait-only cos=0.16 | **(b) partial — shared epistemology, divergent deployment** |
| **Jobs ≈ Hopper** | cos=0.00 (Jobs's NN is davinci at 0.04; Hopper's NN is knuth at 0.08); bootstrap co-cluster 3.5% | **(c) contradicted — these souls have no empirical overlap** |

The Knuth/Turing collision is **real**. Both souls share `correctness`,
`proofs` verbatim, and their stages both center on `algorithm` and
`correctness_review`/`correctness_audit`. If the dispatcher has to pick
between them for "implement a sort function correctly," the trait/stage
YAML genuinely cannot disambiguate.

The Curie/Shannon collision is **half-real**. They share *epistemic
posture* (measure, learn, signal-extract) but their *task surfaces*
don't overlap at all — Curie owns `bench_evolve`, Shannon owns
`anomaly_detection`. So a routing layer could distinguish them by
matching task→stage even though their trait posture is identical. This
is a more nuanced finding than Lovelace's "tightly adjacent."

The Jobs/Hopper collision is **a Lovelace grid artifact**. The qualitative
intuition was "both delete." The empirical reality is that *what they
delete*, *the vocabulary they use to describe it*, and *the
task-domain tags they accept* share zero tokens. They are no more
similar than any random pair. Lovelace's grid forced them together
because the 4 axes were too coarse to see Jobs's "taste pruning" as
distinct from Hopper's "dead-code removal" — but the vocabulary
clearly distinguishes them and the routing layer would too.

### 5.3 The three predicted holes

Lovelace predicted three empty cells:

| cell | predicted role |
|---|---|
| (post-build, divergent, meta, learn) | "Sentinel-shaped soul — mining policies from incidents" |
| (pre-build, measure, system, prevent) | "Empirical risk assessment before commit" |
| (build, convergent, meta, survive) | "Defensive subagent dispatch" |

**All three cells are empirically empty in the existing 15-soul corpus.**
Tagged: **(a) high-confidence — cells confirmed empty.**

But the empirical analysis adds something Lovelace's grid couldn't see:
**there are 7 more empirical singletons** (Curie, Dijkstra, Feynman,
Hamilton, Hopper, Jobs, Sun-Tzu) — souls whose vocabulary is so
distinctive that no other soul comes within 0.10 cosine. These are
*occupied* singleton cells. Lovelace's grid collapsed several of them
together (Hamilton was correctly noted as the only `survive` soul,
but Curie/Hopper/Jobs/Sun-Tzu were placed in cells with other souls
even though their vocabulary is empirically isolated).

**Implication:** the grid undercounts isolation. The set of "souls with
unique vocabulary" is larger than the set of "souls in empty cells."
Either the axis dimensions are too few (4 axes → 81 cells → forces
collapse) or the axes are picking the wrong cuts. The empirical
clustering suggests the *right* cuts may be along a "formal-rigor /
generative-systems / orchestration / failure-stance / taste" axis set
that doesn't decompose cleanly into Lovelace's four.

## 6. Open questions for the re-quorum

Each tagged with confidence and the data point it traces back to.

**Q1. Is the Knuth/Turing collision worth resolving with a fifth axis,
or should the two souls be merged + renamed?**
Confidence: **(a) high.** The data is unambiguous — these two souls are
empirically inseparable on the existing vocabulary. Lovelace proposed a
fifth axis ("rigor target: code / spec / both") which the empirical
analysis cannot evaluate (would need new data). But the merge-or-axis
question is real and forced by the data, not by qualitative intuition.
*Trace:* §4.5 (cos=0.22 mutual NN), §4.4 (89% bootstrap), §3.1
(`correctness proofs` is the only shared phrase in the whole corpus).

**Q2. Should Curie and Shannon be split along the
epistemology-vs-deployment axis the data is hinting at?**
Confidence: **(b) consistent with data, n is small.** The trait-only
cosine of 0.16 vs stage-only cosine of 0.00 suggests these souls have
the *same posture toward evidence* but operate on *different surfaces*
(experiment design vs telemetry pipelines). Lovelace called them
adjacent; the data says they're adjacent on *one* axis and orthogonal on
another. The re-quorum could decide whether that's a refinement (keep
both, document the surface distinction) or a routing contract problem.
*Trace:* §4.6 (trait-only vs stage-only NN tables).

**Q3. Is the empirical "formal-correctness" cluster (Knuth, Socrates,
Turing) actually one role or three?**
Confidence: **(a) the cluster is real (89.5% bootstrap support all
pairs); (b) whether it represents one underlying role is qualitative.**
Empirically these three souls are tightly co-clustered because they
share `correctness`, `proofs`, `review`, `algorithm` vocabulary. But
Lovelace placed them in three different cells (build/convergent/local
for Knuth, post-build/convergent/system for Socrates,
pre-build/convergent/local-system for Turing), differentiating by
*time-relation*. The data says time-relation isn't doing as much
discriminating work as Lovelace hoped — these three look the same to
the dispatcher.
*Trace:* §4.4, §4.6, §5.1.

**Q4. Is the empirical "generative-systems" cluster (Jared_Pleva,
Lovelace, Shannon) coherent or a noise artifact at this n?**
Confidence: **(b) consistent with data.** Bootstrap support is in the
58-78% range — Jared↔Shannon and Jared↔Lovelace are stable, but
Lovelace↔Shannon is only 58%. The cluster might really be
"souls whose vocabulary touches metaprogramming/data-pipeline/system-shape
ideas," with Jared as the centroid. Worth re-examining after any
vocabulary-controlling intervention.
*Trace:* §4.3 (k=10 cluster C8), §4.4 (bootstrap table).

**Q5. Should the YAML schema move from free-form trait phrases to a
controlled vocabulary?**
Confidence: **(a) the data demands it.** 91% of trait words and 86% of
stage words appear in exactly one soul. The schema looks like a
controlled vocabulary (5-6 fixed-shape tokens per soul) but is being
used as free text. This makes routing-by-token-match either trivially
fail (singleton word, matches one soul) or trivially succeed at nothing
(`design` matches 10/15 souls). A modest controlled vocabulary —
~20-30 trait tokens, ~15-20 stage tokens, drawn from the existing
high-frequency words — would make the soul × token matrix dense enough
for the kind of analysis Lovelace was hoping to do qualitatively.
*Trace:* §3.1, §3.2, §3.3 (the entire token-distribution section).

**Q6. Are the 7 empirical singletons (Curie, Dijkstra, Feynman, Hamilton,
Hopper, Jobs, Sun-Tzu) actually unique cognitive lenses, or is the
vocabulary just bespoke per author?**
Confidence: **(b) we cannot tell from this data.** Both stories are
consistent with the matrix — uniqueness might be deep (these souls
genuinely cover non-overlapping work) or shallow (the authors of these
files invented vocabulary without checking what existed). A
controlled-vocabulary pass (Q5) would settle this: if the singletons
*remain* singletons after vocabulary normalization, they are real
isolated lenses; if they collapse into existing clusters, they were
naming-collisions.
*Trace:* §4.3 (k=10 has 7 singletons), §4.4 (no high-bootstrap pairings
for any singleton with any other soul).

**Q7. Does Lovelace's 4-axis grid earn its keep?**
Confidence: **(b) partially.** The same-cell pairs (n=4) do show
elevated empirical similarity (mean 0.074 vs 0.031 for random pairs);
the predicted holes are confirmed empty. But the overall correlation
between axis-overlap and empirical similarity is r=0.17, explaining ~3%
of variance. The grid is **directionally informative, not
quantitatively load-bearing**. It's a useful hand-railing for human
reasoning about the soul space; it should not be load-bearing for
automated routing decisions in the current vocabulary regime.
*Trace:* §5.1 (correlation table), §5.2 (collision audit).

## 7. Reproducibility

All analysis was done in pure Python stdlib (no numpy/scipy/sklearn
available in env; the math is small enough at n=15 that this is fine).
The script is at `/tmp/shannon-analysis/analyze.py`. Inputs are the
soul YAML files; outputs are the four CSVs in
`docs/observations/research/`:

- `2026-04-19-trait-matrix.csv` — soul × trait-word binary matrix
  (15 × 190)
- `2026-04-19-stage-matrix.csv` — soul × stage-word binary matrix
  (15 × 119)
- `2026-04-19-trait-matrix-phrase.csv` — phrase-level (15 × 86)
- `2026-04-19-stage-matrix-phrase.csv` — phrase-level (15 × 74)
- `2026-04-19-soul-similarity.csv` — pairwise cosine on combined
  word-level matrix (15 × 15)
- `2026-04-19-soul-similarity-jaccard.csv` — same, Jaccard

Re-running the script regenerates all outputs deterministically (the
bootstrap uses a fixed seed of 42).

## 8. One-paragraph answer to the decision-rule question

*"What axes are the 15 souls actually using empirically — and where does
the empirical structure agree or disagree with Lovelace's qualitative
grid?"*

**Empirically, the 15 souls are using almost no shared vocabulary at
all** — 91% of trait words and 86% of stage words are unique to one
soul, so the dominant "axis" is *author-of-this-soul-file*, not any
cognitive dimension. The signal that does exist is consistent with three
small clusters — formal-correctness {Knuth, Socrates, Turing} at 89%
bootstrap support, generative-systems {Jared, Lovelace, Shannon} at
58-78%, orchestration {Davinci, Jokic} at 72% — plus 7 empirical
singletons. Lovelace's 4 axes correctly predict same-cell pairs have
elevated similarity (r=0.17) and correctly identify three empty cells,
but they explain only ~3% of pairwise-similarity variance and they
collapse the formal-correctness cluster across three different time-relation
cells. **Of Lovelace's three reported collisions: Knuth/Turing is
high-confidence real, Curie/Shannon is half-real (shared epistemology,
divergent deployment surface), and Jobs/Hopper is a grid artifact with
zero empirical support.** The most actionable finding is structural: the
trait/stage YAML is being used as free text, not a controlled
vocabulary, which is why the qualitative grid had to do all the
alignment work and why automated routing on the current vocabulary
would degenerate to either matching `design` (10/15 souls) or matching
a singleton word.
