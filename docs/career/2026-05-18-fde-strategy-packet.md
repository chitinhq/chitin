# JP FDE / Applied AI Career Strategy — Locked Framework

> **Status:** OPERATIVE — locks the ChatGPT context packet (operator-supplied 2026-05-18) as the contract for all `career` kanban board tickets.
> **Owner:** red lane (c-001)
> **Companion ticket:** `t_493a9a49` on `career` board
> **Major finding from c-009:** [`santifer/career-ops`](https://github.com/santifer/career-ops) (45K stars, in operator's jpleva91 starred list) — operational job-search system built on Claude Code. We adopt this as the foundation instead of building from scratch. See §Tooling stack.

## TL;DR — the strategic question

> Which role archetype maximizes JP's compensation, leverage, technical credibility, and career optionality without mis-slotting him into low-leverage implementation, support, or sales-engineering work?

**Not** "can JP do FDE?" — working assumption is yes.

## Operator known context

| Field | Value |
|---|---|
| Handle | jpleva91 (red in the swarm) |
| Current role | Principal Software Engineer, Agero |
| Current scope | 3 squads · 12 engineers · 6 QA · 12M events/year · de facto Tech Lead Manager |
| Current cash baseline | $195K base + 15% target bonus ≈ low-to-mid $200K TC |
| Prior 7y | Sales → internet sales rep → IS manager → IS director · sales manager of 30 selling 300 cars/mo · variable ops with senior mgmt |
| Education | BS computer info systems · GA bootcamp · MS AI/ML |
| Trajectory | jr → principal during bootcamp + MS · awaiting EM promotion |
| Stated pain | "I hate management" · meetings all day · coordination + unreliable people |

## Positioning principle (LOCKED)

**Anti-narrative:**
> Former salesperson who became an engineer.

**Operative narrative:**
> Principal Software Engineer with unusually strong commercial/customer instincts from prior sales leadership experience.

Sales background is a **multiplier**, never a substitute for engineering depth.

## Target role archetypes — priority order

| Priority | Archetype | Why |
|---|---|---|
| 1 | Staff/Principal Forward Deployed Engineer | Strategic enterprise deployments, hands-on, roadmap feedback |
| 2 | AI Deployment Lead | Portfolio of enterprise AI deployments, repeatable model |
| 3 | Applied AI Architect | More architectural, must tie to production not just pre-sales |
| 4 | Field CTO-style | High leverage if senior enough; avoid sales theater |
| 5 | Strategic / Enterprise AI Architect | Only if comp + scope are strong |

**Lower priority / caution:**
- Generic Solutions Engineer (demo/RFP risk)
- Post-sales Implementation (custom integration grind)
- Demo/Prototype role (shallow unless elite comp)
- Customer Success Engineer (usually not senior enough)

## Comp thresholds — RE-CALIBRATED 2026-05-18 (operator pushback on $300K floor)

**Reality check:** Original $300K cash floor was aspirational, not a fair market read of operator's current band. Operator confirmed they would happily take $230-250K cash for the right role + trajectory premium. Re-calibrated to honest market math.

**Agero EM reference (if promotion lands):** ~$210K base + 15% bonus = **~$241K total comp**. This is the "stay-vs-leave" parity line — anything below this is a step backwards.

| TC band | Verdict |
|---|---|
| <$200K | Not worth it unless extraordinary non-financial upside (founding equity, frontier-lab brand) |
| **$230K–$280K cash + equity** | **Acceptable if role quality + trajectory premium is real** (AI-native co, meaningful equity) |
| **$260K–$340K base + equity** | **Target band — realistic FDE / Applied AI / Senior Solutions Engineer at AI-native cos** |
| **$320K–$410K base + equity** | **Stretch / Director-tier — wedge plays best here** |
| $400K+ TC | Frontier-lab ceiling — Anthropic / OpenAI / Cognition staff-level + equity |
| Quota/OTE-heavy | Evaluate separately for variance, territory, accelerators, loss of tech compounding |

**Floor rules:**
- **Hard cash floor:** $230K base
- **Stay-vs-leave parity:** $241K total (matches projected Agero EM)
- **Relocate gate (SF/NYC on-site):** $400K+ TC
- **No-relocate (remote):** $230K cash floor + meaningful equity

## Hard disqualifiers (LOCKED — updated 2026-05-18)

- Base below $230K cash AND no meaningful equity
- Role is mostly demos / RFPs / support / implementation without engineering or product influence
- No clear senior ladder
- Reporting buries role under low-leverage customer success
- Success metrics primarily sales support, not production adoption or business impact
- On-site without TC clearing $400K+ relocate gate
- JD-gated (law degree, active security clearance, etc.)
- Direct IP conflict with chitin/AgentGuard (agent-governance competitor)

## Strong-yes profile (LOCKED)

- $350K+ credible TC
- Staff/Principal-equivalent level
- Strategic AI deployment
- Hands-on enough to preserve technical credibility
- Direct product/engineering feedback loop
- Strong company brand or equity upside
- Clear ownership of production outcomes
- Sales background functions as multiplier, not substitute

## Scoring model (LOCKED, weighted 100)

| Criterion | Weight |
|---|---:|
| Compensation upside | 20 |
| Technical depth / credibility | 15 |
| Strategic customer exposure | 15 |
| Product/roadmap influence | 15 |
| Reusable leverage / platformization | 10 |
| Career-brand value | 10 |
| Reversibility | 5 |
| Lifestyle / travel fit | 5 |
| Equity upside | 5 |

## Tooling stack — `santifer/career-ops`

Operator starred this repo and it solves a huge chunk of our work natively:

| career-ops capability | Maps to our ticket | Status |
|---|---|---|
| **A-F scoring (10 weighted dims)** | c-003 role-quality scoring | Adopt directly (their dims overlap with packet) |
| **STAR+R interview story bank** | c-005 STAR stories | Adopt as story container |
| **ATS PDF CV generation** | c-004 resume positioning | Adopt — Space Grotesk + DM Sans, keyword-injected |
| **Portal scanner (45+ companies)** | c-002 market intel + c-010 target list | Adopt — pre-configured Anthropic, OpenAI, ElevenLabs, Retool, n8n + custom queries across Ashby/Greenhouse/Lever/Wellfound |
| **Negotiation scripts** | c-004 comp negotiation framing | Adopt directly |
| **Batch processing (claude -p workers)** | c-002 parallelism | Adopt — matches our owner-routing model |
| **Dashboard TUI** | (none — bonus visibility) | Adopt — visible pipeline |
| **Pipeline integrity (dedup, status normalize)** | (none — bonus) | Adopt |

**Important quote from career-ops README:**
> "The first evaluations won't be great. The system doesn't know you yet. Feed it context — your CV, career story, proof points, preferences, what you're good at, what you want to avoid. The more you nurture it, the better it gets."

This matches the c-001 / c-007 / c-008 onboarding sequence exactly.

**Author proof point:** 740 offers evaluated → 100 tailored CVs → landed Head of Applied AI. Target archetype overlap with JP's strong-yes profile.

## How our 3-agent swarm composes with career-ops

career-ops is the **operational** layer (scanning, scoring, PDF gen). Our swarm provides the **strategic** layer that career-ops can't:

| Layer | Owner | Tool |
|---|---|---|
| **Strategy framework** (this doc) | red | hand-authored from ChatGPT packet |
| **Operational pipeline** | career-ops (Claude Code native) | Playwright + Claude + Go dashboard |
| **Narrative polish** (frontier prose) | Clawta (GPT-5.5) | career-ops CV → Clawta refines |
| **Interview prep depth** (multi-day rehearsal) | Ares (Ollama Cloud) | career-ops story bank → Ares rehearses |
| **Cross-source synthesis** (market intel + role scoring + alt-path analysis) | red (1M ctx) | career-ops outputs → red aggregates |

We don't rebuild what career-ops already does. We layer 3-agent strengths on top.

## Decision flow per discovered role

```
career-ops scrape role
  ↓
career-ops A-F score (10 dims)
  ↓
Apply our hard disqualifiers (LOCKED above)
  ↓
If A-F >= 4.0 AND no disqualifier triggered:
  → Clawta: polish narrative for this specific role
  → red: cross-check comp against thresholds
  → Ares: pull relevant STAR stories from bank
  → Operator: final go/no-go on application
Else:
  → log + skip (career-ops "strongly recommends against <4.0")
```

## Initial strategy — 10 steps (LOCKED from packet)

1. Build target list of 20–30 companies (c-010 + career-ops portals.yml)
2. Filter for Staff/Principal-equivalent scope (c-003)
3. Exclude implementation-heavy + sales-support-heavy (c-003 hard flags)
4. Prepare resume variant for customer-facing AI deployment (c-004 variant A)
5. Prepare resume variant for Staff/Principal SWE (c-004 variant B)
6. Run both searches in parallel to compare market response
7. Use recruiter screens to test leveling + comp before full loops
8. Treat <$300K TC as likely no unless exceptional
9. Treat $350K–$500K as main target zone
10. Keep Agero/EM path as baseline unless external materially improves

## Standard recruiter-screen questions (LOCKED)

Ask early:
1. What org does this role sit in: Engineering, Product, Sales, Solutions, CS, or GTM?
2. What level is this role equivalent to internally?
3. Expected base / TC / equity range?
4. % production engineering vs demos/prototypes?
5. How are FDEs measured?
6. How do FDE learnings influence the product roadmap?
7. Accounts per FDE concurrently?
8. % travel expected?
9. Are FDEs expected to write production code?
10. Staff/Principal FDE ladder exist?
11. What distinguishes top performers?
12. Common failure modes in this role?
13. **NEW (2026-05-18 ratification, ReadyBench-weighted):** Is fractional / advisor work on a non-competing AI startup permitted with disclosure? If no, role is downgraded. This protects the ReadyBench 2% RSA vest + chitin OSS development.

**Interpretation:**
- ✅ Strong answers: production adoption, technical architecture, reusable assets, product feedback, measurable customer impact
- ❌ Weak answers: demos, sales support, customer happiness, implementation tickets, "wearing many hats" without authority

## Open questions to resolve with operator

1. Exact 2026 TC target (low-to-mid $200K baseline, $300K minimum, $350-500K target)
2. Travel tolerance %
3. Relocation OK or remote/hybrid only?
4. People mgmt / IC leadership / player-coach scope preference?
5. Startup/equity risk tolerance?
6. Production LLM/AI experience to claim today?
7. Strongest domain: AI infra, enterprise automation, automotive/insurance, dev tools, data platforms, security, workflow automation?
8. Cash vs equity vs optionality vs founder-prep optimization target?

## Primary strategic filter (LOCKED, final test for every role)

> Does this role compound both JP's engineering credibility AND commercial/customer leverage, or merely monetize his willingness to deal with customers?

**Choose only the former.**
