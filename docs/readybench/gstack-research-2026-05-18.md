# gstack for ReadyBench — Research Brief

> **Author:** JP (Fractional CTO)
> **Date:** 2026-05-18
> **For:** Tarek (founder), William Murphy (CEO)
> **Trigger:** Tarek's Slack note 2026-05-18 — *"I think it's the tool we will need to integrate with for gathering and documenting the end client needs."*
> **Time-box for first conversation:** ~30 min on agreed direction, ~60 min on integration shape.

## TL;DR (read this if nothing else)

**Recommendation: integrate gstack — but as the discovery-and-spec layer ON TOP of the ResourceRequest model, not as a replacement.**

gstack is Garry Tan's open-source "Claude Code persona factory" (MIT, 23 specialist roles + 8 power tools). Its `/office-hours` skill encodes the YC "six forcing questions" framework used to expose demand reality, status quo, narrowest wedge, and future-fit. Mapped onto ReadyBench's `ResourceRequest` flow, this turns the customer's vague "I need a senior backend engineer" into a structured, validated, specific spec the matching engine can actually rank against.

**3 concrete integration paths, ordered by build cost vs. value:**
1. **Lightweight (1-2 wk):** Embed gstack's office-hours skill as a guided wizard during ResourceRequest creation. Customer answers the 6 forcing questions; output gets attached to the ResourceRequest as a "discovery doc." No infra changes.
2. **Medium (3-4 wk):** Add a "Discovery Mode" toggle on ResourceRequest. Behind the scenes runs `/office-hours` + `/plan-ceo-review` + `/document-generate` (Diataxis) to produce a structured spec document. Spec becomes a first-class entity that drives matching weights.
3. **Heavy (8-12 wk):** Rebuild the ResourceRequest creation flow around gstack as the spec orchestrator. Discovery output drives everything downstream — matching, candidate intro framing, post-engagement check-ins.

**My recommendation: start with Path 1, prove value with 5-10 real customers, then graduate to Path 2 if conversion lifts.**

---

## What gstack actually is

**Built by:** Garry Tan, President & CEO of Y Combinator. Previously: early Palantir engineer, Posterous founder (sold to Twitter), built Bookface (YC's internal social network).

**What it ships:**
- **23 Claude Code persona skills** — CEO, eng manager, designer, reviewer, QA lead, CSO (security), release engineer, retro lead, etc. Each is a Markdown SKILL.md file invoked as a slash command (`/review`, `/cso`, `/qa`, etc.).
- **8 power tools** — `/browse` (headless QA browser), `/autoplan` (full review pipeline), `/ship` (release automation), `/canary`, `/benchmark`, `/setup-deploy`, `/setup-gbrain`, `/setup-browser-cookies`.
- **Idempotent installer** — 30-second paste into Claude Code, optional team mode that commits gstack into a repo so collaborators auto-install.
- **MIT license.** No paid tier.

**Why Tarek's instinct is right:** gstack's `/office-hours` skill is the YC Office Hours discovery format codified as a structured prompt. It's the closest open-source analog to what a Series-A founder would pay an external consultant $5-15K to facilitate. Putting that in front of every ReadyBench customer at the request-creation moment is a real differentiator.

**Garry's own claim (verifiable):** he shipped ~810× more logical code in 2026 vs 2013 using this stack solo + part-time. He runs YC full-time and ships side products at team scale. Not vapor — the GitHub contribution charts are public.

---

## ReadyBench's current flow vs. where gstack fits

### Current ResourceRequest creation (from `backend/prisma/schema.prisma`)

```prisma
model ResourceRequest {
  description               String
  skillRequirements         RequestSkillRequirement[]
  budgetMin, budgetMax
  projectStart, timezone
  // ... plus filters on the Browse view
}
```

Customer fills out a form. They write a description, pick skill requirements, set budget + timezone + start date. Submit.

**The structural problem:** the description field is unstructured prose. The skill requirements are ATS-style filters. Neither captures:
- **Demand reality** (is this need urgent enough to pay for it?)
- **Status quo** (what are they doing today without ReadyBench?)
- **Desperate specificity** (what does "senior backend" actually mean in their stack + culture?)
- **Narrowest wedge** (what's the smallest piece of work that proves fit?)
- **Future-fit** (does this engagement set up the next 3 engagements or end at this one?)

Those five dimensions are the difference between a 5-week engagement that converts to a 12-month retainer and a 4-week engagement that ghosts after invoice. ReadyBench's matching engine can rank candidates against skills — but it can't rank candidates against unmeasured customer intent.

### Where gstack fits

**`/office-hours`** (gstack/office-hours/SKILL.md) runs exactly these 6 questions in two modes:
- **Startup mode:** demand reality, status quo, desperate specificity, narrowest wedge, observation, future-fit.
- **Builder mode:** design-thinking brainstorm for greenfield ideas.

Output: a saved design doc with the customer's answers structured into sections.

That's the spec ReadyBench's matching engine actually needs.

---

## Three integration paths

### Path 1 — Lightweight: discovery wizard, output as attachment (1-2 wk)

**What:** Add a "Run discovery (recommended)" button on the ResourceRequest creation form. Clicking it opens a chat-style wizard that walks the customer through gstack's 6 forcing questions. Output gets attached to the ResourceRequest as a discovery doc (markdown). Matching engine still uses the original description + skills fields.

**What changes for the customer:** 10-15 min added to request creation. Optional, skippable. We A/B-test the conversion impact.

**What changes for ReadyBench:**
- New `Discovery` attachment type on `ResourceRequest` (1 schema migration)
- New `/api/requests/:id/discovery` endpoint that orchestrates gstack `/office-hours` server-side
- Frontend wizard component (5-7 days)
- Internal dashboard view shows the discovery doc alongside the request

**Engineering cost:** 1-2 weeks for one engineer. ~$8-12K loaded cost.

**Risk:** customers skip the wizard → no signal collected. Mitigation: make it default-on with a "skip" button (loss-aversion framing).

**Why this is the right Path 1:** lowest infra change, highest learning velocity. We get real customer-facing data on whether forcing questions improve match quality within 30 days.

### Path 2 — Medium: structured spec as first-class entity (3-4 wk)

**What:** Discovery output becomes a structured `RequestSpec` entity (not just an attachment). Matching engine reads `RequestSpec` first, falls back to `description` for legacy requests.

Pipeline: `/office-hours` → `/plan-ceo-review` (CEO persona challenges the spec for soft/hard constraints) → `/document-generate` (Diataxis-framework spec doc). Output drives matching weights.

**What changes for the customer:** same wizard as Path 1, plus a "review your spec" gate where they see the structured doc + can edit before publishing.

**What changes for ReadyBench:**
- New `RequestSpec` schema (forcing-questions output + structured fields)
- Matching engine refactor to consume `RequestSpec` first
- "View spec" page for both customer + assigned candidate
- Spec versioning (so changes mid-engagement are tracked)

**Engineering cost:** 3-4 weeks for one engineer + 1-2 days designer. ~$25-35K loaded cost.

**Why this is Path 2:** matching gets materially smarter. Candidate views the spec, not the prose description. Both sides are looking at the same artifact. Reduces "mismatched expectations" cancellations — historically the #1 churn cause in talent marketplaces.

**Risk:** matching engine churn during the refactor → existing customers see worse matches mid-cutover. Mitigation: dual-write spec + description for 30 days, dual-read for matching.

### Path 3 — Heavy: gstack as the spec orchestrator end-to-end (8-12 wk)

**What:** Every customer interaction routes through a gstack persona — discovery (office-hours), spec review (plan-ceo-review), candidate intro framing (designer/codex), post-engagement check-in (retro), security review on the engagement boundary (cso). ReadyBench becomes a Claude Code-native platform; gstack is the persona library.

**What changes for the customer:** every touchpoint feels like talking to a senior consulting partner. Highest perceived value-per-touch.

**What changes for ReadyBench:**
- Full persona-driven UX across 4+ flows
- gstack integration is core infrastructure, not a feature
- Internal team needs Claude Code fluency (training cost real)
- Cost-per-request goes up (multiple LLM calls per touch) — needs careful unit-economics modeling

**Engineering cost:** 8-12 weeks for 2 engineers + 1 designer. ~$80-120K loaded cost.

**Why this is Path 3 not Path 1:** it's the right end-state IF Paths 1+2 prove value. Doing it first risks 8 weeks of engineering before we know if customers want any of this.

**Risk:** scope creep + Claude Code-shaped platform decisions become hard to reverse. The choice of "gstack everywhere" is a vendor bet — if Anthropic / Garry change direction, ReadyBench inherits the churn.

---

## My recommendation: Path 1 first, gated graduation to Path 2

**Sequence:**

1. **Week 1-2:** Ship Path 1. Discovery wizard, optional + default-on, output attached to ResourceRequest. Instrument: % customers who run discovery, % whose discovery doc gets read by candidates, time-to-first-match, eventual conversion to paid engagement.
2. **Week 3-6:** Measure. If discovery doc lifts conversion ≥15% vs control OR reduces "mismatched expectations" cancellations by ≥25%, graduate.
3. **Week 7-10:** Ship Path 2. Restructure spec as first-class, dual-write/dual-read transition.
4. **Week 11+:** Re-evaluate Path 3 based on Path 2 data.

**Why this sequence:**
- Fastest customer-facing learning (2 weeks to data, not 8-12)
- Lowest reversal cost if customers don't engage with discovery
- Gates engineering investment behind measured signal
- Aligned with YC's own "talk to users first, build later" advice (irony intentional given the tool's origin)

**What I'd commit to as Fractional CTO:**
- Path 1 implementation (or oversight if William wants me hands-on)
- The measurement framework
- Customer-side discovery copy (the wizard UX needs founder-quality language; happy to draft)
- The graduation decision with you both at week 6

**What you'd need from gstack itself:**
- Just install it locally for the ReadyBench dev team. MIT license, no contract required.
- A small wrapper service so we can invoke `/office-hours` headless from the ReadyBench backend instead of requiring customers to install Claude Code (this is the only real engineering primitive needed for Path 1).

---

## Risks + tradeoffs to discuss

1. **Vendor concentration on Anthropic.** gstack is Claude Code-native. ReadyBench would be committing to Claude as the LLM backbone for discovery. Tradeoffs vs. building model-agnostic: Claude Code is strongest at this kind of structured-conversation work today, but Anthropic pricing changes propagate.
2. **Customer-side friction.** 10-15 min of forcing-questions added to request creation. Some customers will skip; some will bounce. A/B test will tell us.
3. **Bench-side display.** Candidates will need to see + understand the structured spec. UX work has to happen on both sides of the marketplace, not just customer.
4. **gstack still maturing.** Repo is months old. Garry is the maintainer + heavy user, but the community is small. We'd be early adopters. Mitigation: fork what we depend on (we're MIT-licensed downstream).
5. **No vendor lock-in on the discovery framework itself.** Even if we drop gstack later, the discovery doc is plain Markdown. The data we capture is portable. The risk is purely operational, not data-strategic.

---

## What I'd want from you before committing engineering hours

(For the live conversation, not a homework ask.)

1. **Conversion target.** What's the current ResourceRequest → paid engagement rate? What lift would make Path 1 worth shipping?
2. **Churn signal.** Top 3 reasons engagements end early — is "mismatched expectations" actually #1 or am I guessing?
3. **Engineering bandwidth.** Who's building this — me + 1 backend engineer, or do you want to handle internally with my advisory?
4. **Tarek's product instinct.** What did he see in gstack that I'm not capturing? His Slack note is short; the meeting is the real input.
5. **Founder appetite for Claude Code platform commitment.** Path 3 is a bet on Anthropic. Path 1 is a low-stakes experiment. Where on that spectrum are you both comfortable?

---

## Appendix A — gstack skill inventory

23 persona skills + 8 power tools, all slash commands, all auto-loaded into Claude Code after install:

**Strategic / planning:** `/office-hours` `/plan-ceo-review` `/plan-eng-review` `/plan-design-review` `/plan-devex-review` `/autoplan` `/learn`
**Design:** `/design-consultation` `/design-shotgun` `/design-html` `/design-review` `/devex-review`
**Build / Ship:** `/codex` `/review` `/ship` `/land-and-deploy` `/canary` `/benchmark`
**QA + Browser:** `/browse` `/qa` `/qa-only` `/connect-chrome` `/setup-browser-cookies`
**Security:** `/cso` (OWASP + STRIDE audit)
**Ops:** `/freeze` `/guard` `/unfreeze` `/careful` `/setup-deploy` `/setup-gbrain` `/gstack-upgrade`
**Docs / Retro:** `/document-generate` `/document-release` `/retro` `/investigate`

For ReadyBench Path 1, only `/office-hours` is on the critical path. Path 2 adds `/plan-ceo-review` + `/document-generate`. Path 3 starts touching the design + QA + retro skills.

---

## Appendix B — comparison with chitin (my open-source agent governance project)

For context if Tarek asks how my own work fits: **gstack and chitin are complementary, not competitors.**

| Layer | gstack | chitin |
|---|---|---|
| Scope | Single Claude Code session × 23 personas | 3+ independent agents (Claude Code + Hermes + Clawta) coordinating via shared substrate |
| Persona pattern | Slash command invokes skill in current session | Each agent runs in own process/ecosystem; coordinates via kanban + agent-bus + GitHub |
| Best for | Solo founder shipping at team velocity | Multi-agent shipping with explicit review-author separation |
| ReadyBench use | Yes (this brief) | Indirect — chitin is my multi-agent infrastructure; not a customer-facing dependency |

I'd recommend ReadyBench adopt gstack directly. chitin stays my open-source side project.

---

## Next step

Set up the 30-min sync this week. I bring a UX sketch of the Path 1 wizard + the measurement framework. You bring the conversion/churn baseline numbers. We decide go/no-go on Path 1 in that meeting.

If go: I write the spec + start Path 1 the following week. Target ship date for the wizard: ~2 weeks from go.
