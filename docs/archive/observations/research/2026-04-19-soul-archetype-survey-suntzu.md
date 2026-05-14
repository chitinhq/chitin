---
date: 2026-04-19
soul: sun-tzu
status: research-draft
related:
  - souls/canonical/
  - souls/experimental/
  - souls/elo.md
  - docs/observations/research/2026-04-19-soul-archetype-survey-davinci.md
  - docs/observations/research/2026-04-19-soul-archetype-survey-lovelace.md
---

# Soul archetype survey — Sun Tzu cut: agent-framework terrain map

Research brief for the canonical-8 re-quorum. Cap: ~30 min wall.
Frame: terrain map of agent frameworks shipping in production today,
April 2026. Goal: locate chitin's canonical-8 set on that terrain
before we lock automated ELO scoring against it.

## 1. Hypothesis (written before researching)

Prior, in one sentence: **the field has converged on role-typed
agents, but on a *function/domain* axis (researcher, coder,
reviewer, web-surfer) — not the *cognitive-style* axis chitin uses,
and almost no framework has a promotion/demotion mechanism for its
roles.**

Sub-priors:
- CrewAI, AutoGen, LangGraph all expose roles as a first-class
  primitive but the role taxonomy is *task-shaped* not
  *thinking-shaped*.
- OpenAI Swarm / Agents SDK reduces "role" to "the agent you hand off
  to" — function-typed but not cognitively-typed.
- Anthropic's own published work on multi-agent systems uses
  function-shaped sub-agents (search/plan/synthesize), not named
  cognitive lenses.
- Chitin's "named cognitive archetype + per-task selection + ELO
  scoreboard + promotion/demotion" composite is divergent — possibly
  signal, possibly aspirational.
- Negative-evidence prediction: I will *not* find a production
  framework that has explicitly removed persona/role typing after
  having it. The trend is additive, not subtractive.

Decision rule for the writeup: at least 6 named frameworks with URLs;
if I can't find OpenClaw's role taxonomy in primary sources, I file a
null and note the secondary-source discrepancy explicitly.

## 2. Method

- Read all 8 canonical and 7 experimental `souls/*.md` files for
  shape baseline. Read `souls/elo.md` for the measured signal.
- WebSearch + WebFetch sweep on named frameworks. Where a framework
  had primary docs, I fetched them; where it had only secondary
  coverage, I noted that.
- Specifically went hunting for negative evidence (frameworks that
  *removed* role typing after shipping it).
- Cap was held: ~25 minutes of search + ~5 minutes of writeup.

**What I could not pin down:**

- **OpenClaw `SOUL.md` mechanism, in primary sources.** WebFetch on
  `https://docs.openclaw.ai/concepts/multi-agent` confirms OpenClaw
  has per-agent personas via `SOUL.md` and `AGENTS.md` workspace
  files, but the *primary docs* expose **no role taxonomy, no role
  count, no promotion/demotion mechanism, no role assignment
  algorithm**. Secondary sources (o-mega.ai, freecodecamp,
  shopclawmart blog) describe a richer SOUL.md schema (identity /
  voice / behavioral rules / persona-drift "enforced" mode) and
  multi-instance role-delegation patterns, but I could not find this
  in OpenClaw's own docs. I also could not find a `github.com/openclaw`
  org that holds the framework itself; `github.com/Gen-Verse/OpenClaw-RL`
  is a related but distinct RL training framework that *uses* OpenClaw,
  not the framework itself. **Filing this as: OpenClaw has the
  closest-shaped concept to chitin souls in the field, but the
  taxonomy details are documented in secondary sources only — call
  this an unverified-but-suggestive convergence.**
- I did not deeply read the OpenAI Agents SDK reference for
  inheritance/composition between agents — the handoff pattern is
  clear at the surface but its long-arc role-evolution story isn't.
- Anthropic's persona-vector research is well-documented but
  Anthropic explicitly does **not** prescribe production deployment
  — it's diagnostic/steering, not a named-persona system.

## 3. Framework matrix

Columns: framework / version / has explicit roles? / role count /
taxonomy axis / assignment mechanism / role-effectiveness
measurement / extensibility / promotion-demotion mechanism.

| Framework | Ver / date | Explicit roles? | # built-in | Taxonomy axis | Assignment | Effectiveness measurement | Extensible? | Promote/demote? |
|---|---|---|---|---|---|---|---|---|
| **CrewAI** | docs current 2026-04 | Yes — `Agent(role=, goal=, backstory=)` | 0 built-in (template patterns: Researcher, Writer, Customer Support, Manager) | **Function/domain** ("employee" model — role = job title) | **Manual** at crew-construction; manager agent can delegate within crew | None first-party; user instruments | Yes — arbitrary string roles | **No** |
| **Microsoft AutoGen / Magentic-One** | AutoGen 0.4 (Jan 2025) → folded into Microsoft Agent Framework GA Q1 2026 | Yes — concrete subclasses | **3 named in Magentic-One**: `MultimodalWebSurfer`, `FileSurfer`, `MagenticOneCoderAgent` + a manager | **Function/capability** (modality-typed) | **LLM-routed** by manager agent against a task ledger | Benchmark-reported (GAIA, etc.) | Yes — subclass `BaseAgent` | **No** |
| **LangGraph** | OSS, 2026-04 | "Roles" are nodes in a typed `State` graph — researcher/writer/fact-checker by convention | 0 built-in | **Function** by node responsibility; graph topology is the load-bearing primitive, not the role | **Manual** (graph edges) + LLM-conditional via routers | None first-party; checkpoint replay aids debugging | Yes — arbitrary nodes | **No** |
| **OpenAI Swarm** | Reference, 2024-10 | "Roles" implicit in agents-you-can-handoff-to | 0 built-in | **Function** (triage / billing / specialist) | **LLM-routed** via `transfer_to_X` tool | None | Yes | **No** |
| **OpenAI Agents SDK** | Production successor, March 2025+ | Same handoff model + guardrails/tracing/sessions | 0 built-in | **Function** | LLM-routed handoffs | Tracing UI; no role-effectiveness scorecard | Yes | **No** |
| **Anthropic multi-agent research system** | Published 2025-06 (Anthropic Engineering blog) | Yes — orchestrator + parallel sub-agents | Pattern, not a named set | **Function** (planner / parallel searcher / synthesizer) | **LLM-routed** (orchestrator dispatches sub-agents) | Internal; not publicly scored | Pattern is reusable | **No** |
| **Anthropic persona-vector research** | arXiv 2507.21509 (2025-09); AAAI 2026 follow-on | Yes — *traits as activation directions* (evil, sycophancy, hallucination-propensity, big-five-style) | N/A (continuous activation space) | **Trait/cognitive style** — closest axis match to chitin | Steering vector applied at inference; or trained-in | Diagnostic only; no production scorecard | Researcher-extracts new vectors per trait | **No** (not a named-persona system at all) |
| **Cognition Devin** | Annual review 2025; multi-Devin 2025-mid | Single specialized agent; 2025+ adds "managed Devins" delegated by a parent | 1 named role: software engineer (sub-Devins inherit) | **Domain** (software engineering only) | Parent Devin decomposes & delegates | Internal benchmark + customer task-success | No (single product) | **No** |
| **Microsoft Semantic Kernel / Microsoft Agent Framework** | v1.0, late 2025; GA Q1 2026 | "Agent Personas" — predefined behavioral patterns | Examples (customer service, financial analysis); no fixed enum | **Function/domain** | Manual instructions string per agent | None first-party | Yes | **No** |
| **MultiAgentBench / academic** | ACL 2025 (arXiv 2503.01935) | Yes — coding collaboration uses specialized code roles (debugger, test-writer, reviewer) | Per-experiment | **Function** | Per-protocol (star/chain/tree/graph) | Milestone-KPI scoring (this is the closest *measurement* analogue to chitin's ELO) | Yes | Not a deployed framework — it's a benchmark |
| **"Improving Role Consistency..." (arXiv 2604.02770, 2025)** | Paper | Role-clarity matrix as fine-tune regularizer | N/A | Function | N/A | Identifies *role-specification failure* as a major failure mode and proposes quantitative role-clarity score | N/A | N/A |
| **OpenClaw** (per primary docs) | docs.openclaw.ai 2026-04 | Per-agent persona via `SOUL.md` + `AGENTS.md` workspace files | Not specified in primary docs | Not specified | Per-channel/per-account *deterministic* binding (channel, account, peer, guild/team); the *what-persona-runs-here* binding is a routing decision, not an LLM call | None documented | Yes — file-driven | **Not documented in primary** |
| **OpenClaw** (per secondary blogs) | unverified | Detailed persona schema (identity / voice / behavioral rules / forbidden phrases / signature phrases) with "enforced" persona-drift monitoring; multi-instance role delegation (coder / writer / etc.) | Not specified | Mixed (function in delegation; trait/voice in single-agent) | Manual + workspace-file binding | Persona-drift monitor (closest to chitin's strike system) | Yes | **Not documented** |

**Key cross-row reads:**

- **Every production framework has explicit roles. None has
  promotion/demotion.** The ELO + strikes + promote/demote loop in
  chitin (`souls/elo.md`, `souls/strikes/`, `status: promoted`
  frontmatter) has no clear analogue in any shipped framework I
  found. Closest analogues are *evolutionary* (DEEVO, ART) which are
  research-stage tournament rankers for *agents themselves*, not for
  named cognitive personas.
- **The taxonomy axis is overwhelmingly function/domain.** Of 12+
  frameworks/systems mapped, only **Anthropic's persona vectors** sit
  on the cognitive-style/trait axis — and they're a research
  diagnostic, not a production system.
- **Effectiveness measurement is rare.** Most frameworks ship roles
  with zero first-party measurement. MultiAgentBench (academic) is
  the cleanest exception. Chitin's user-curated ELO (1500 ± single
  digits) is more measurement than most production frameworks
  expose.
- **Negative-evidence search returned nothing.** I could not find a
  framework that had personas/roles and explicitly removed them.
  The directional pressure is additive — frameworks add role/persona
  configurability over time, they don't strip it. (Caveat: I was
  searching for the negative; absence of evidence here is *weakly*
  evidence of absence given a 25-min cap.)

## 4. Terrain analysis

### Where chitin is converged-on with the field (no need to relitigate)

- **Having explicit roles at all.** The whole field has settled this.
  Monolithic "one giant prompt" agents are losing ground; orchestrated
  role-typed agents are winning. Chitin is on the right side of this.
- **Per-role files / per-role system prompts.** CrewAI, OpenClaw,
  Semantic Kernel, AutoGen all do some variant of "the role is a
  reusable file/object with identity + instructions". Chitin's
  `souls/canonical/*.md` is structurally similar.
- **Switching/handoff between roles within a session.** OpenAI Swarm
  and AutoGen have the strongest version of this; chitin's "swap
  back to da Vinci after Phase B" notes (e.g. Knuth scope-note in
  `souls/canonical/knuth.md`) are the same move expressed prose-first
  rather than tool-call-first.

### Where chitin is divergent-good (worth defending)

- **Cognitive-style taxonomy axis.** Almost no production framework
  uses cognitive-style roles. The one place we see this axis at all
  in the field is **Anthropic's own persona-vector research** — and
  *that's significant*: the actor closest to the model says the
  cognitive-trait axis is real and steerable. CrewAI's roles encode
  *what an agent does*; chitin's souls encode *how an agent thinks
  while doing it*. These are orthogonal axes and the field has only
  developed the function axis. Chitin is doing the trait axis with a
  prose mechanism while Anthropic is doing it with activation steering
  — both treat trait as a first-class object. **This convergence
  with Anthropic's research direction is the strongest single piece
  of evidence in this survey that the cognitive-style axis is real,
  not aspirational.**
- **Promotion/demotion + ELO + strikes.** Nothing in production has
  this. The closest is academic tournament-style ELO for *whole
  agents* (DEEVO/ART) and Cognition's internal Devin benchmarking.
  No framework has an ELO loop on *named cognitive lenses inside a
  single agent*. This is genuinely novel. The risk: if ELO is
  measuring what amounts to noise (n=4 events, ±1 per event), the
  novelty is theatrical. The current `souls/elo.md` is honest about
  being subjective; the question is whether automated scoring against
  it will reveal signal or expose that there's no there there.
- **Per-task lens-switching with explicit scope notes.** Knuth's and
  Curie's frontmatter scope notes ("active for Phase B only, swap back
  to da Vinci after") are a discipline I did not find anywhere else.
  Magentic-One has dynamic ledger-driven dispatch but not this kind
  of *human-readable, time-boxed* lens commitment with a prescribed
  return. This is good practice.
- **Quorum-vote selection of the active soul.** Per the Knuth
  scope-note: "Quorum vote 2026-04-19 unanimous (all 8 canonical souls
  converged on Knuth)". I found no shipping framework that uses an
  internal multi-persona vote to select the active persona. (Closest:
  research-stage debate/voting protocols in MultiAgentBench, but those
  vote on *answers*, not on *which persona should answer*.) This is
  unusual.

### Where chitin may be divergent-stale (worth re-examining)

- **All canonical archetypes are dead historical figures.** Eight
  named-after-historical-individuals souls (Curie, Knuth, da Vinci,
  Shannon, Lovelace, Socrates, Sun Tzu, Turing). Field convention is
  function-name (Researcher, Coder, Reviewer) or product-name (Devin)
  or capability-name (WebSurfer, FileSurfer). **The historical-figure
  convention may be carrying load that's not tracked.** Risk: the
  costume warning each canonical file opens with ("you are not
  imitating the voice/mannerisms") is doing real work — but it's
  doing real work *because* the naming pulls toward performance.
  CrewAI sidesteps this by naming roles after jobs. Worth asking the
  re-quorum: does the historical-figure naming earn its keep, or is
  it a cost we're paying to fight off?
- **8 canonical is on the high side for cognitive lenses.** Magentic-One
  ships with 3 named agents. Anthropic's multi-agent research system
  uses 3 functional patterns. CrewAI templates show 3-4 roles per
  crew. Devin is 1. Persona-vector research enumerates a handful of
  traits per study. **Chitin's 8 + 7 = 15 souls is large by field
  norms.** This may be correct for chitin's surface area (8 distinct
  reasoning needs across 6 execution surfaces) but the re-quorum
  should sanity-check whether all 8 canonical actually fire often
  enough to justify the slot. The ELO log shows only Curie (+3) and
  da Vinci (-1) with deltas. The other 6 canonical have **zero
  measured events.** That's a Sun Tzu read: 6 of 8 canonical lenses
  are occupied positions with no engagement on them yet — either they
  defend ground we don't realize is being threatened, or they're
  garrison forces with no front.
- **No assignment-mechanism formalism.** Field has converged on either
  (a) manual/static binding (CrewAI, OpenClaw per-channel) or (b)
  LLM-routed handoff (Swarm, AutoGen, Magentic-One ledger).
  Chitin's quorum vote is a third thing, but it's currently
  prose-driven, not coded. The re-quorum should decide whether to
  formalize the selection mechanism or accept that prose-vote is the
  load-bearing primitive (in which case it should be documented as
  such, not as an interim).
- **Effectiveness measurement is opinion-weighted by design** (per
  `souls/elo.md`). This was a *good* default during bootstrap. Now
  that automated ELO is on the table, the question is whether
  opinion-weighted ELO will sit alongside automated ELO or be
  superseded. Anthropic's persona-vector work suggests there *is* a
  measurable substrate (activation directions); MultiAgentBench
  suggests there *is* a measurable behavioral substrate (milestone
  KPIs). Chitin can borrow from both. The risk is collapsing
  subjective judgment into automated score and losing the trainer's
  note distinction the current ELO file makes explicit.

## 5. Open questions for the re-quorum

Specific, evidence-cited, named.

1. **Does the cognitive-style taxonomy axis earn its keep against the
   field's function axis?** Evidence for keep: Anthropic's persona-vector
   research (arXiv 2507.21509, AAAI 2026) treats character traits as
   first-class steerable objects, validating the axis. Evidence
   against keep: every shipped multi-agent framework I surveyed
   (CrewAI, AutoGen, LangGraph, OpenAI Agents SDK, Semantic Kernel)
   uses the function/domain axis. **Recommend the re-quorum decide
   whether chitin operates on the trait axis (current), the function
   axis (field convention), or both stacked (e.g., "Curie-as-Researcher"
   = trait × function matrix).** This is the load-bearing
   architectural call.

2. **Should the historical-figure naming convention persist?** Every
   canonical file opens with ~3 lines warning against costume
   ("you are not imitating Curie's voice / Sun Tzu's martial
   metaphors / Knuth's TeX nostalgia"). That guardrail exists *because
   the naming pulls toward performance*. CrewAI's `role="Researcher"`
   doesn't need the guardrail. Trade: historical-figure names are
   load-bearing for memorability and lens-distinctness; they cost
   ~30 lines of anti-costume scaffolding per soul. **Re-quorum:
   keep, rename to function-shaped (e.g. `correctness-rigorist`,
   `terrain-router`), or dual-name?**

3. **6 of 8 canonical souls have zero ELO deltas.** Curie +3, da Vinci
   -1, the other 6 unmoved. Two readings: (a) they defend positions
   that haven't been tested yet (Sun Tzu read — garrison value), or
   (b) they're aspirational slots that the work isn't actually
   reaching. **Before locking automated ELO, the re-quorum should
   require each unfired canonical to name a near-term task it
   *would* be the right lens for** — if no such task exists in the
   next 2-4 weeks of roadmap, the slot is aspirational and the
   automated ELO will measure noise.

4. **Is OpenClaw's `SOUL.md` actually doing the same thing chitin's
   `souls/canonical/*.md` does, or is the convergence cosmetic?**
   I could not verify the secondary-source description of OpenClaw's
   SOUL.md schema (identity/voice/rules/persona-drift "enforced") in
   primary docs. **If OpenClaw is doing the same thing chitin is,
   that's the strongest external validation of this whole design
   direction. If it's marketing-shaped writeups about a thinner
   feature, chitin is genuinely first-mover here.** Worth a 30-min
   primary-source verification before the re-quorum locks anything.

5. **Should the ELO loop be split into two scoreboards — opinion-weighted
   (current) and automated (planned)?** `souls/elo.md` explicitly
   distinguishes itself from "any future automated scoring derived
   from event telemetry: this one is opinion-weighted and subjective
   by design. Think of it as a trainer's note, not a benchmark."
   **The re-quorum should ratify this two-scoreboard design before
   the automated one ships, so they're not implicitly merged.**
   Anthropic's persona-vector work is an existence proof that
   activation-level scoring is feasible; MultiAgentBench is an
   existence proof that behavioral-KPI scoring is feasible. Chitin
   can build either or both as the *automated* scoreboard, distinct
   from the trainer's note.

6. **The quorum-vote selection mechanism is currently prose-driven
   and undocumented as a primitive.** No shipping framework uses
   internal multi-persona voting to select the active persona.
   This is either a genuine novel primitive worth formalizing, or
   it's an artifact of how chitin happens to be operated right now.
   **Re-quorum: is "quorum vote" a load-bearing mechanism worth
   coding, or is it a session-level prose ritual that should stay
   prose?** Either answer is valid; the decision should be
   explicit.

7. **Does the cognitive-style/function-axis question affect the
   experimental tier?** Several experimentals (Hopper = "prune /
   audit", Hamilton = "incident response", Dijkstra = "protocol
   design") read more like function-shaped roles than cognitive
   styles. If the re-quorum answers Q1 with "trait axis only", these
   may belong somewhere else. If "function axis", they may belong
   *promoted*. **The experimental tier composition is dependent on
   the Q1 answer; don't audit experimentals before resolving Q1.**

---

**One-paragraph summary** (per the brief's decision rule):

The agent-framework field in April 2026 has converged on **explicit,
function/domain-typed roles assigned manually or by LLM handoff, with
no first-party effectiveness measurement and no promotion/demotion
mechanism**. CrewAI, AutoGen/Magentic-One, LangGraph, OpenAI Swarm /
Agents SDK, Semantic Kernel, and Anthropic's own multi-agent research
system all sit on this axis. Chitin's canonical-8 set is **convergent
with the field on having explicit, file-defined, switchable roles** —
and **divergent on three dimensions**: (1) cognitive-style taxonomy
axis instead of function/domain (validated by Anthropic's persona-vector
research as the right substrate, but unique among shipping frameworks);
(2) ELO + strikes + promotion/demotion loop on named lenses (no
production analogue; closest is research-stage tournament ELO on whole
agents); (3) historical-figure naming with anti-costume guardrails (a
chitin-specific stylistic choice that pays a documentation tax).
**Six of eight canonical souls have zero measured ELO events**, which
is the most actionable finding before locking automated scoring —
either each unfired canonical names a near-term task it would be the
right lens for, or the automated ELO will measure noise on garrison
positions. The single most important external signal: **OpenClaw
ships per-agent persona files literally called `SOUL.md`**, which is
either the strongest convergence evidence in this survey or
marketing-shaped writeups about a thinner feature; primary-source
verification is recommended before the re-quorum locks anything.

## Sources

- OpenClaw multi-agent docs (primary): https://docs.openclaw.ai/concepts/multi-agent
- OpenClaw guide (secondary, o-mega.ai): https://o-mega.ai/articles/openclaw-creating-the-ai-agent-workforce-ultimate-guide-2026
- OpenClaw-RL (related, distinct): https://github.com/Gen-Verse/OpenClaw-RL
- CrewAI agents docs: https://docs.crewai.com/en/concepts/agents
- CrewAI repo: https://github.com/crewaiinc/crewai
- AutoGen / Magentic-One: https://microsoft.github.io/autogen/stable//user-guide/agentchat-user-guide/magentic-one.html
- AutoGen repo: https://github.com/microsoft/autogen
- Microsoft Agent Framework overview: https://learn.microsoft.com/en-us/agent-framework/overview/
- LangGraph repo: https://github.com/langchain-ai/langgraph
- LangGraph docs: https://docs.langchain.com/oss/python/langgraph/overview
- OpenAI Swarm: https://github.com/openai/swarm
- OpenAI Agents SDK: https://openai.github.io/openai-agents-python/
- OpenAI Cookbook — Orchestrating Agents: https://cookbook.openai.com/examples/orchestrating_agents
- Anthropic multi-agent research system: https://www.anthropic.com/engineering/multi-agent-research-system
- Anthropic persona vectors: https://www.anthropic.com/research/persona-vectors
- Persona Vectors paper (arXiv 2507.21509): https://arxiv.org/abs/2507.21509
- Steering LLM Interactions Using Persona Vectors (AAAI 2026): https://openreview.net/forum?id=HpUDi5Pe8S
- Cognition Devin 2025 Performance Review: https://cognition.ai/blog/devin-annual-performance-review-2025
- MultiAgentBench (ACL 2025, arXiv 2503.01935): https://arxiv.org/abs/2503.01935
- Improving Role Consistency in Multi-Agent Collaboration (arXiv 2604.02770): https://arxiv.org/html/2604.02770
- Galileo Agent Leaderboard: https://github.com/rungalileo/agent-leaderboard
