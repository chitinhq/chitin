# Chitin Outcome Model + Roadmap

Date: 2026-05-14
Companion to: [`chitin-moat-audit.md`](./chitin-moat-audit.md)
Lens: principal engineer + product strategist.

This document models best / base / downside outcomes, then sequences three horizons (0-30, 31-90, 91-180 days), then identifies critical path, opportunity cost, reversibility, and the next-30 punchlist. Evidence labels and confidences match the audit doc.

---

## 1. Outcome scenarios

### 1.1 Best plausible outcome (12-18 months out)

**Narrative.** Chitin becomes the de-facto **open standard for cross-driver execution governance** of AI coding agents. The "Chitin Protocol" (chain envelope v2 + canonical action enum + gate decision shape) is referenced in OpenTelemetry's `gen_ai.*` SIG or in an AGENTS.md-adjacent governance spec. An EU AI Act conformance pack lands by Q3 2026 and is cited by a notable open-source agent (Aider, Continue, OpenDevin, or Hermes itself) as "use chitin for Article 12 logging." Two or three reference operators run chitin against 4+ drivers in earnest; one of them is a name-recognizable engineering org. Optional commercial vehicle: a hosted opt-in "policy intelligence" aggregator with anonymized counts ships behind a feature flag; founder retains optionality on open-core vs services.

**Required assumptions.**
- Anthropic / OpenAI / Google do NOT ship native deterministic cross-vendor gates in the same window (each ships their own vendor-locked gate; chitin remains the only cross-vendor option). `[assumption M]`
- EU AI Act enforcement causes at least one publicly visible procurement-style ask for "tamper-evident agent logs." `[web H]`
- Microsoft Agent Governance Toolkit ships but stays Microsoft-centric (Azure-pull-through), leaving non-Azure shops looking for an OSS alternative. `[assumption M]`
- A second maintainer is onboarded by month 4. `[assumption M]`

**Leading indicators.** External GitHub stars trajectory > 200 by D90; first non-author PR merged by D60; one inbound integration request by D45; one EU-AI-Act-mapping blog citation by D90.

**Failure triggers.** Anthropic + Microsoft ship overlapping native gates and explicit cross-vendor pivots; bus factor never increases above 1; an audit finds a chain-integrity bug that makes Article 12 claim false.

**Timeline.** Compliance pack + spec at D60. First external pilot at D90. Reference customer at D150. Cross-vendor spec proposal at D180.

**Technical milestones.** Gate-latency benchmark < 1 ms p50, < 5 ms p99; 1M-event chain replay in < 60 s; chain integrity continuous-CI fuzz with 0 false-allows; signed release binaries (linux+macos); homebrew tap.

**Product milestones.** v0.1 signed release; chitin-dashboard MVP shipped and the default surface; policy-pack registry + `install-pack` command; Article-12 conformance bundle (chain + decisions + retention attestation).

**Distribution milestones.** Landing page; one technical blog post per month; one cross-vendor case study; speak at one infra conference; AGENTS.md governance section authored.

**Capital implications.** Stays OSS-funded by author until D120; optional $0.5-1.5M seed or angel round in Q3 to support second maintainer + cloud aggregator beta. The cloud aggregator is the wedge for any commercial vehicle and should NOT be conflated with chitin-the-kernel.

**Reversible vs irreversible bets.** Spec publication = mostly reversible (can be rev'd). Trademark / brand = harder to undo. Hosted aggregator = a one-way door for the privacy promise — design opt-in + per-event consent before turning on.

**Confidence: M.** The technical platform supports the narrative; positioning + execution gates the outcome.

### 1.2 Base case (12-18 months out)

**Narrative.** Chitin ships v0.1 signed release, gets a working dashboard, an EU AI Act mapping doc, and 10-30 individual operators try it. The "cross-driver canonical chain" remains the differentiator but does not capture an enterprise category position. Hermes and OpenClaw remain the primary substrate consumers. Founder remains principal contributor; no second maintainer materializes. Velocity continues, but the gap between technical capability and external recognition does not close. A commercial vehicle is not pursued; chitin remains a strong personal-infrastructure project with a small community.

**Required assumptions.**
- Single-maintainer velocity holds. `[repo H]`
- No native vendor gate ships in window. `[web M]`
- Compliance positioning gets published but only attracts hobbyist attention. `[assumption M]`

**Leading indicators.** Stars 20-100 by D180; 0-2 non-author PRs; 1-3 inbound feature asks; no inbound integration partnerships.

**Failure triggers.** None — this is the steady-state.

**Timeline.** Signed release D45. Dashboard polish D90. Article-12 mapping D60. Optional benchmarks D120.

**Technical milestones.** Same as best case except commercial aggregator is not built; spec stays internal.

**Product milestones.** v0.1 release. CLI-only install. Policy-pack examples (no registry).

**Distribution milestones.** Landing page. 2-4 blog posts over 6 months.

**Capital implications.** Zero. Project funds itself via author's primary income.

**Reversible vs irreversible bets.** Almost everything reversible — the base case explicitly avoids one-way doors.

**Confidence: H.** This is the path of least resistance from today's state.

### 1.3 Downside case (12-18 months out)

**Narrative.** Anthropic ships a native deterministic gate (per RFC #45427) in Claude Code. Microsoft Agent Governance Toolkit becomes the OSS default for Microsoft and OpenAI shops. Endor Labs AURI captures the "AI security buyer" persona. Chitin remains correct, internal, and largely unknown. The substrate composition (hermes + openclaw) breaks at some point because both upstream projects are smaller and less stable than chitin. The author burns out on solo maintenance or de-prioritizes the project; the chain stays clean but the moat erodes from feature-creep elsewhere.

**Required assumptions.**
- Anthropic ships per RFC #45427 within 9 months. `[web M]`
- Two or more credible competitors win the "agent runtime security" narrative. `[web M]`
- No second maintainer joins. `[assumption M]`

**Leading indicators.** RFC #45427 closes "shipped" within 6 months; Endor Labs Security League becomes the cited benchmark; Microsoft Toolkit GitHub stars > 5k while chitin remains < 100.

**Failure triggers.** A chain-corruption issue in production; a high-profile blog ridicules "yet another local-only governance kernel"; substrate breakage from hermes or openclaw causes the swarm to stop working for a week.

**Timeline.** RFC #45427 ships D120. Project decline visible by D150. Maintainer pause by D180.

**Technical milestones.** No new milestones; chitin remains stable.

**Reversible vs irreversible bets.** All reversible from a code perspective. The lost reversible bet is *attention* — once a competing standard captures the narrative, retracing is expensive.

**Confidence: M.** This requires Anthropic to act, which is plausible but not certain.

---

## 2. Highest-leverage bets and avoidable failures

- **Single highest-leverage bet:** **Publish an EU AI Act Article 12 conformance pack within 30 days** — a doc + a verifying CLI subcommand (`chitin compliance article-12 verify`) that emits a signed attestation over a chain window. This is the one move that converts the technical chain into a regulatory + buyer claim with the lowest engineering cost and the highest external legibility. Evidence: chitin's chain already satisfies the technical requirements `[repo H]`; the regulatory deadline is fixed `[web H]`; no competitor has published this mapping yet `[web H]`.
- **Biggest avoidable failure mode:** **Building the cloud aggregator before the spec is published.** A SaaS shim before the open-source spec turns chitin into "yet another startup," not "the open standard." Specs must precede services for the open-core path.
- **Highest-opportunity-cost distraction:** **More driver normalizers without published normalizer-author SDK + first third-party plugin.** Each new driver costs days but does not increase external pull. Better: codify the normalizer interface so the next driver costs hours and the *interface itself* attracts contributors.
- **Shortest path to a visibly stronger moat:** Conformance pack + signed v0.1 release + named dashboard demo with one external operator wired across 4 drivers, all within 60 days. Three artifacts; one is regulatory, one is distributional, one is social proof.
- **Shortest path to invalidating the strategy:** Anthropic ships native deterministic in-dispatch gate AND chitin has no published "we gate 6 drivers, not 1" comparison. Mitigation: lock the cross-vendor messaging into the README + blog within 30 days, before the RFC ships.

---

## 3. Roadmap (3 horizons)

Items are tagged with: Objective / Why now / Dependency / Impact / Effort / Risk / Reversibility / Metric / Moat category / Evidence basis. Effort is S/M/L/XL (S ≤ 1 day, M ≤ 1 week, L ≤ 2 weeks, XL > 2 weeks of single-author time).

### 3.1 Horizon 1 — 0-30 days: raise confidence, remove ambiguity, close obvious gaps

H1 goal: close the **proof-and-positioning** gap. Engineering quality is high; external claims must catch up.

| # | Item | Objective | Why now | Dependency | Impact | Effort | Risk | Reversibility | Metric | Moat | Evidence |
|---|---|---|---|---|---|---|---|---|---|---|---|
| H1.1 | **EU AI Act Article 12 conformance pack** | Map each Article 12 obligation to a chitin control; ship a `chitin compliance article-12 verify` subcommand emitting a signed attestation over a chain window | Regulatory deadline 2026-08-02; no competitor has done this | None | Unlocks first compliance-buyer conversation | M | M (regulatory text interpretation risk) | Reversible | First conformance bundle generates without error; one external review by a privacy-engineering peer | Compliance, Brand, Switching costs | EU AI Act Article 12 `[web H]`; chain already hash-linked `[repo H]` |
| H1.2 | **Gate-latency + chain-throughput benchmark** | Publish p50/p99 gate latency, chain emit throughput, 1M-event replay time, compared to Microsoft Toolkit's "sub-ms" claim | Microsoft Toolkit's launch made performance a category claim | None | Defends the "deterministic" word | S | L (low risk; only risk is the numbers are bad) | Reversible | Benchmark merged into `docs/`; reproducible via `bench/perf/run.sh` | Technical, Brand | Microsoft Toolkit claim `[web]`; pure-Go kernel `[repo H]` |
| H1.3 | **Chitin Protocol Spec v0.1 (chain envelope + action enum + gate decision)** | Lift `docs/event-model.md` into a versioned spec doc with stability promise, change policy, and conformance-test definition | The spec is the standard play; without it, the architecture is implementation-defined | None | Anchors all integrations + spec-citation downstream | M | M (locking shape too early) | M (versioned spec) | Spec published; semver promise; conformance test suite shipped | Technical, Ecosystem | Existing event-model doc is 80% spec already `[repo H]` |
| H1.4 | **Signed v0.1.0 release (linux-x86_64 + linux-arm64 + darwin)** | Cut the first signed release. Source + binary + checksums + signature. Tag in repo | Currently `go build` from source; no install path | None | Distribution floor changes from 1→4 | M | L | Reversible (tag mutable until promoted) | `gh release view v0.1.0` returns artifacts; SHA-256 + minisign signature attached | Distribution, Brand | No releases today `[repo H]` |
| H1.5 | **Landing page + thesis blog post** | One-page positioning site + a single 1500-word blog post titled "Chitin: cross-driver execution governance for AI coding agents" | The "governance toolkit" + "EU AI Act" narratives are crowding the channel right now | H1.1 (for the compliance line) | Establishes name; first SEO surface | M | L | Reversible | Page live at chitinhq.io or GH Pages; blog post links from `README.md` | Brand, Distribution | Currently nothing externally facing `[repo H]` |
| H1.6 | **OWASP Agentic-AI Top 10 mapping** | Map chitin controls to each of the 10 risks; publish in `docs/compliance/owasp-agentic-top10.md` | Microsoft Toolkit's anchor narrative is this exact mapping | None | Buyer-legible | S | L | Reversible | Doc merged; cross-linked from README | Compliance, Brand | Microsoft Toolkit claim `[web H]` |
| H1.7 | **Cross-driver coverage README correction + comparison table** | Add a "Chitin vs native vendor gates" table to README showing chitin gates 6, native gates 1 | RFC #45427 may close any week | H1.4 | Locks the cross-vendor framing | S | L | Reversible | Table merged | Brand, Positioning | RFC #45427 `[web H]`; 6-driver coverage `[repo H]` |
| H1.8 | **Single-author bus-factor mitigation doc** | Publish "Chitin maintainer playbook" + tag 3-5 architectural ADRs as load-bearing + open one "wanted: co-maintainer" issue | A real risk on every dependency; visible to buyers/reviewers | None | Reduces the bus-factor finding from H confidence to M | S | L | Reversible | Doc merged; issue opened | Execution velocity, Brand | Single author per `[repo H]` |
| H1.9 | **Adversarial gate-bypass test suite v0** | Add 20 known shell/normalizer bypass attempts (UTF-8 weirdness, $IFS tricks, env-var rewrites, here-docs, subshell-chains) as failing-on-allow CI tests | The product's strongest claim is "deterministic gate" — no public negative-test suite is the evidentiary gap | None | Hardens the kernel's strongest claim | M | M (bug discovery probable, must fix as discovered) | Reversible | 20 cases merged; all pass; CI fails on regression | Technical, Defensibility | `gov.Gate` `[repo H]`; existing rules e.g. `no-destructive-rm-via-execute-code` `[repo H]` |
| H1.10 | **Telemetry-uplink design doc (opt-in, anonymized)** | Design (do NOT ship) the opt-in policy-intelligence uplink: schema, consent flow, privacy contract, scrubbing | The data moat is gated on this; designing early lets every later decision respect the boundary | None | Unblocks 90-day data plan | M | L (design doc only) | Reversible | Design doc + RFC merged | Data network effects, Brand | `[inference H]`; thesis "local-only" boundary `[repo H]` |

H1 total: ~3.5 weeks of focused single-author time. Most items are S/M.

### 3.2 Horizon 2 — 31-90 days: strengthen the moat and ship compounding primitives

H2 goal: turn the published spec + compliance pack into pull. Build the primitives that compound.

| # | Item | Objective | Why now | Dependency | Impact | Effort | Risk | Reversibility | Metric | Moat | Evidence |
|---|---|---|---|---|---|---|---|---|---|---|---|
| H2.1 | **Policy-pack registry + `chitin-kernel install-pack <name>`** | Ship a typed bundle format (`pack.yaml` + signature) and a registry lookup over `https://chitinhq.github.io/policy-packs/`; convert `examples/policy-packs/` to packs | The compounding primitive: every operator's policy investment becomes shareable | H1.3 (spec stability) | Multiplies operator value of every published pack | L | M (registry semantics — versioning, signature checks, deprecation) | Mostly reversible (registry contents); irreversible after first pack adoption | 3 packs ship; one third-party PR-contributed pack | Switching costs, Ecosystem, Brand | `examples/policy-packs/` exists `[repo H]` |
| H2.2 | **Chitin Dashboard v1 (primary surface, not MVP)** | Promote `apps/chitin-dashboard/` to the canonical view: per-driver decisions, severity-counter heatmap, chain inspector, OTEL projection check, signed-attestation viewer | A kernel without a console is invisible to non-engineer buyers | H1.4 | Converts kernel into a product non-CLI users can demo | L | M (scope creep risk) | Reversible | Dashboard renders 6 drivers, 4 panels, all real data | Product completeness, Brand | `apps/chitin-dashboard/` exists `[repo M]` |
| H2.3 | **First external pilot operator** | Recruit one credibly-named external operator + ship them through install, policy authoring, first incident, first replay, first compliance bundle | The "external use" gap is the single biggest score-mover | H1.4, H1.5, H1.6 | First reference customer; ground-truth feedback | XL (coordination cost) | M (pilot may abandon) | Reversible | Pilot completes 14 days of continuous gating with at least 2 drivers | Switching costs, Brand, Distribution | None today `[repo H]` |
| H2.4 | **Normalizer-author SDK + first third-party driver normalizer** | Publish a typed Go interface + harness for writing a `normalize.go`; partner with a community member or fork-author to ship one new driver normalizer | The category is fragmenting; chitin should be a substrate for new-driver work, not the only author | H1.3 | Scales driver coverage horizontally | M | M (interface stability risk) | M (versioned interface) | Interface published; one external normalizer PR merged | Ecosystem, Distribution | 30+ CLI tools in the awesome list `[web H]` |
| H2.5 | **OpenTelemetry SIG submission: `gen_ai.gate.*` semconv** | Submit chitin's gate decision attributes to the OTel `gen_ai` SIG | Anchors chitin's chain shape in an industry standard | H1.3 | One-way door to multi-vendor adoption | M | M (slow process; partial adoption likely) | Hard to reverse once accepted | Issue/PR opened in OTel SIG; first review comment received | Ecosystem, Brand, Compliance | `internal/emit/otel.go` `[repo H]`; `gen_ai.*` semconv planned `[repo M]` |
| H2.6 | **Chain integrity continuous fuzz in CI** | Add a property-based fuzz that generates events, tampers, replays, and asserts chain rejection | The "tamper-evident" claim is foundational; needs continuous evidence | None | Permanently hardens the strongest claim | M | L | Reversible | Fuzz runs nightly; 100K cases / run; zero false-accepts in 30 days | Technical, Defensibility, Compliance | `canon` package + `prev_hash`/`this_hash` `[repo H]` |
| H2.7 | **OWASP-Top-10 mapping → policy-pack** | Convert H1.6 doc into an installable `owasp-agentic-top10.pack.yaml` that enforces concrete rules per row | Makes the OWASP claim *executable*, not paper | H2.1 | Buyer-legible + technically demonstrable | M | L | Reversible | Pack ships; install + dry-run on a fixture chain produces expected decisions | Compliance, Switching costs | H1.6, `chitin.yaml` rule shape `[repo H]` |
| H2.8 | **Chitin Protocol conformance suite** | Spec + test-vector pack a 3rd-party can run to claim "chitin-protocol compatible" | Required to make spec real; consumed by H2.4 | H1.3 | Standardization play | M | L | Reversible | Suite published; chitin itself passes 100%; one outside repo passes too | Ecosystem, Technical, Distribution | None yet `[repo H]` |
| H2.9 | **Chitin protocol vs native vendor gate matrix** (kept evergreen) | Living doc + page comparing chitin coverage vs Anthropic / OpenAI / Google / Microsoft native gates as they ship | RFC #45427 is the visible one; others will follow | H1.7 | Defensible positioning under shifting native landscape | S | L | Reversible | Doc published + monthly update commitment | Brand, Positioning | `[web M-H]` |
| H2.10 | **Second-maintainer onboarding (mentor a contributor through 3 merged PRs)** | Bring one external contributor through 3 substantive PRs (driver normalizer, policy-pack, dashboard panel); offer merge bit if criteria met | Bus factor of 1 is the load-bearing risk | H2.4 (normalizer SDK) | Reduces bus factor; sustains velocity | XL (coordination cost) | M (no one shows up) | Reversible | Three PRs from one external contributor merged | Execution velocity, Brand | Single author `[repo H]` |

### 3.3 Horizon 3 — 91-180 days: scale the wedge, deepen lock-in, create visible differentiation

H3 goal: convert spec + pack + pilot into a defensible position with at least 3 reference operators and one inbound integration.

| # | Item | Objective | Why now | Dependency | Impact | Effort | Risk | Reversibility | Metric | Moat | Evidence |
|---|---|---|---|---|---|---|---|---|---|---|---|
| H3.1 | **3-5 reference operators across 4+ drivers each** | Recruit 2-4 more external operators, deliver a polished install path, capture testimonial / case study | Switching costs only materialize after sustained external use | H2.3 | Converts technical moat into commercial-relevant evidence | XL | M | Reversible per op | At least 3 sustained users with chain > 100K events; at least one quotable testimonial | Switching costs, Brand, Network | H2.3 `[repo H]` |
| H3.2 | **Opt-in "policy intelligence" aggregator (beta)** | Ship the H1.10 design behind an explicit opt-in: anonymized policy-decision counts + denied-action histograms uploaded to a single endpoint | Without cross-operator data, policy-pack quality plateaus | H1.10, H3.1 | Begins the data moat compounding | XL | H (privacy review must be airtight) | Hard (one-way trust shift) | 3 operators opt-in; aggregator dashboard renders cross-operator counters | Data, Brand | H1.10 design `[repo M]` |
| H3.3 | **Audit + threat model published** | Commission or self-publish a threat model + invite an external security review of the kernel | The "deterministic gate" claim needs external attestation | H1.9, H2.6 | Brand-positive; raises floor on Defensibility durability | L | M (findings will arrive) | Hard | Threat model + audit doc merged; findings tracked to remediation | Technical, Defensibility, Brand | No audit today `[repo H]` |
| H3.4 | **Policy-pack marketplace UX + 10 community packs** | Take H2.1's registry into a polished UX: list, install, fork, contribute, signed pack badges; recruit packs from community | Compounding economics: each pack multiplies operator value | H2.1, H3.1 | Network/community moat starts forming | L | M (community-supply risk) | Reversible | Registry hosts ≥ 10 packs from ≥ 5 contributors | Network, Ecosystem | None today `[repo H]` |
| H3.5 | **Production-scale benchmark (10M events, multi-driver concurrent)** | Move benchmark from synthetic to realistic; publish numbers; document scaling envelope | The single-box dogfood masks scale issues | H1.2 | Forecloses "doesn't scale" objections | L | M | Reversible | 10M-event run published; SQLite tuning notes; replay < 10 min | Scalability, Technical | Single-box today `[repo M]` |
| H3.6 | **Cross-vendor case study with named operator + named vendor partner** | One published case study where chitin gates Claude Code + Codex + Gemini in the same workflow, ideally with a vendor blog cross-post | The most repeatable distribution beat; converts existing technical evidence to brand | H3.1 | Brand catalyst; SEO + recruiting tailwind | XL (relationship-coordination) | H (vendor approval may stall) | Reversible | Case study published; cross-posted on a vendor blog | Brand, Distribution | None today `[repo H]` |
| H3.7 | **MCP-native gateway alignment** | Add MCP-first observation/policy paths so chitin can sit in front of MCP gateways too (not only behind parent drivers) | MCP is the consolidating contract surface; failing to align cedes that channel | H1.3, H2.5 | Future-proofs the gate against MCP becoming the dominant contract | L | M (MCP spec churn) | Reversible | MCP gateway driver normalizer shipped; one MCP server demoed under chitin | Workflow, Ecosystem, Technical | MCP via parent driver today `[repo H]` |
| H3.8 | **First commercial option finalized** | Decide and publish: open-core (chitin OSS + chitin-cloud aggregator) vs support contract vs policy-pack marketplace. One-way doors should be made deliberately | Business model can no longer stay undeclared; affects every downstream choice | H3.1, H3.2 | Capital optionality + recruiting | M | H (founder-strategy commitment) | Hard | Public commercial option page + first paying or design-partner customer | Business model | Undeclared today `[repo H]` |

---

## 4. Assumption table

| ID | Assumption | If wrong, what changes | Confidence in assumption |
|---|---|---|---|
| A1 | Anthropic does NOT ship native deterministic gate (per RFC #45427) within 6 months | Chitin's enforcement value for Claude-Code-only buyers degrades; cross-vendor pitch becomes the only pitch | M |
| A2 | EU AI Act causes at least one publicly visible procurement ask for tamper-evident agent logs in our market | Compliance pack remains a positioning lever but pull is slower; H1.1 still worth doing | H |
| A3 | Single-author velocity holds at current pace (15+ PRs / week) | H1.* delivery slips by 30-50% | M |
| A4 | At least one external operator is willing to be a named pilot | H2.3 + H3.1 timelines slip materially | M |
| A5 | Hermes + OpenClaw substrates remain stable enough that the four-hop swarm keeps demoing chitin | Demo story switches to a simpler single-driver dogfood; H2/H3 swarm-dependent items rework | M |
| A6 | OpenTelemetry SIG accepts (some of) `gen_ai.gate.*` proposal within 6 months | H2.5 stalls but doesn't invalidate any other item | L (SIG processes are slow) |
| A7 | Microsoft Agent Governance Toolkit does NOT add a cross-vendor gate competing with chitin's exact wedge | Cross-vendor framing collapses; chitin needs a deeper compliance / spec moat to survive | M |
| A8 | Bus factor of 1 holds long enough to get to H2.10 | H1.8 doc + H2.10 search become urgent earlier | M |

---

## 5. Leading indicators

Pick 5 and watch them weekly:

1. **GitHub stars on chitinhq/chitin.** Floor: 100 by D90. Ceiling: 1000 by D180. (Brand + Distribution proxy.)
2. **First non-author PR merged.** Target: D60. (Bus factor + ecosystem proxy.)
3. **Inbound integration ask count.** Target: 1 by D45, 3 by D120. (Distribution + ecosystem proxy.)
4. **Driver-gate latency p99 vs Microsoft Toolkit claim.** Target: published numbers. (Technical proxy.)
5. **Cited compliance citation (any blog / case study / regulator).** Target: 1 by D90. (Compliance + brand proxy.)

If any indicator misses by 30+ days, escalate to a refactor of the corresponding roadmap item.

---

## 6. Critical path

The minimum sequence to get to the **best-plausible** trajectory is:

```
H1.3 (Spec v0.1) ─► H1.1 (Article 12 pack)
   │                   │
   │                   ▼
   ├─► H1.6 (OWASP map) ─► H2.7 (OWASP pack)
   │                                │
   ▼                                ▼
H1.4 (v0.1 release) ─► H1.5 (landing page) ─► H2.3 (first pilot)
   │                                              │
   ▼                                              ▼
H1.2 (bench) ─► H2.6 (chain fuzz) ─► H3.3 (audit)  H3.1 (3-5 ops)
   │                                              │
   ▼                                              ▼
H2.1 (pack registry) ─► H3.4 (marketplace)     H3.8 (commercial)
   │
   ▼
H2.4 (normalizer SDK) ─► H2.10 (co-maintainer) ─► H3.7 (MCP align)
```

Single critical chain: **H1.3 → H1.1 → H1.4 → H1.5 → H2.3 → H3.1 → H3.8.** Everything else parallelizes.

---

## 7. Opportunity-cost analysis

| Item the team might consider but should NOT do now | Cost | Better use of that time |
|---|---|---|
| Adding more drivers beyond 6 | 1-2 weeks each | H2.4 (SDK so others ship drivers) |
| Building a "chitin cloud" SaaS before H3.2 design | XL | H1.10 design + privacy review |
| Self-hosted dashboard with auth + multi-user before H2.2 single-user view | L+ | H2.2 single-user dashboard polished to "show a buyer" quality |
| Reinventing OTel exporters | L | H2.5 OpenTelemetry SIG submission instead |
| Owning kanban data, workflow engine, or LLM provider abstraction | XL | Continue composing hermes + openclaw (already decided 2026-05-13) |
| Souls-library promotion as a primary product surface | M | Demote in operating-model.md; keep as analytics |
| New router heuristics without precision evidence | M | H1.9 adversarial suite first |
| TypeScript rewrite of any gov component | XL | Spec doc (`H1.3`) instead — codifies the same intent without rewrite |

---

## 8. Reversibility analysis

| Decision | Reversibility | Treatment |
|---|---|---|
| Publish Chitin Protocol spec | Versioned, mostly reversible (semver) | Move fast; v0.1 + change policy |
| Cut v0.1 signed release | Reversible per tag | Move fast; promote when stable |
| Publish EU AI Act conformance pack | Reversible (revise doc) | Move fast |
| Add policy-pack registry signature scheme | M (keys are sticky) | Design 2 weeks; signed after |
| Opt-in telemetry uplink | Hard to reverse trust | Design 30 days, ship behind flag, audit before default-on |
| Commit to commercial option | Hard | Wait until H3.8 |
| Trademark "Chitin Protocol" | Hard | Pursue after H1.3 only if v0.1 release lands cleanly |
| OpenTelemetry SIG semconv submission | Hard once accepted | Get spec stable first |
| Onboard co-maintainer with merge bit | Reversible but socially costly to revoke | Through H2.10 graduated trust |
| Cull a driver / feature | Reversible code-wise; signal cost | Use the 2026-05-08 audit-driven pattern |

---

## 9. Recommended next 30 days (the actual punch list)

This is what to actually do tomorrow morning, ordered by sequence:

1. **Day 1-2:** Write `docs/compliance/eu-ai-act-article-12.md` mapping every Article 12 obligation to chitin controls + a single CLI subcommand spec.
2. **Day 3-5:** Implement `chitin-kernel compliance article-12 verify --window 30d` emitting a signed attestation JSON (uses existing canonical-JSON + chain hash). [H1.1]
3. **Day 6-7:** Bench: `bench/perf/gate-latency.go` + run on the existing dogfood box; publish `bench/perf/RESULTS.md`. [H1.2]
4. **Day 8-10:** Lift `docs/event-model.md` into `docs/spec/chitin-protocol-v0.1.md` with semver, change policy, conformance vectors. [H1.3]
5. **Day 11-13:** Build linux + darwin signed binaries; cut tag `v0.1.0`; publish on GH Releases with checksums + minisign signature. [H1.4]
6. **Day 14-15:** Static landing page (GH Pages) + 1500-word "what chitin is" blog post. [H1.5]
7. **Day 16-18:** `docs/compliance/owasp-agentic-top10.md`. [H1.6]
8. **Day 19:** Add cross-driver coverage comparison table to README. [H1.7]
9. **Day 20-21:** Publish "maintainer playbook" + open co-maintainer issue. [H1.8]
10. **Day 22-27:** Adversarial gate-bypass test suite — 20 cases minimum. [H1.9]
11. **Day 28-30:** Telemetry-uplink design doc + privacy contract draft (no implementation). [H1.10]

Buffer day 31 is for slippage; if any single item slips > 3 days, drop H1.10 (it has the latest dependency).

---

## 10. Critical unknowns to resolve now

These are the questions whose answers materially change the roadmap. Resolve them inside H1 if possible.

1. **Will Anthropic ship native deterministic gate (RFC #45427)?** Determines weight of cross-vendor framing. Watch the issue. `[web M]`
2. **Is the operator willing to recruit a co-maintainer?** Determines H1.8 and H2.10 actionability. `[founder decision]`
3. **What is the commercial preference?** Open-core / support / marketplace / none. Determines H3.8 shape. `[founder decision]`
4. **What "operator" persona is targeted first?** Solo developer / engineering-org platform team / regulated-industry compliance team. Determines pilot recruiting (H2.3). `[founder decision]`
5. **Is "Chitin Protocol" the right brand for the spec, or is a vendor-neutral name (e.g., "AGI" — Agent Governance Interface) better?** Determines H1.3 + trademark. `[positioning call]`
6. **Will Hermes and OpenClaw remain stable substrates for the next 12 months?** Determines swarm-demo viability. Currently both are smaller upstream projects than chitin. `[upstream stability question]`
7. **Is the Linux-first posture a constraint or a deliberate choice?** Determines macOS / Windows roadmap inclusion. `[founder decision]`

---

## 11. End-state read

If H1.1, H1.3, H1.4, H1.5 ship in 30 days, **the moat score moves from 39 → ~47** purely from positioning. If H2.3, H2.5, H2.6 ship in 90 days, **score moves to ~54**. If H3.1, H3.3, H3.8 ship in 180 days, **score is ~64**, anchored by external use and one commercial commitment.

The single most valuable thing in this roadmap is **EU AI Act Article 12 conformance pack + signed release + landing page within 30 days.** Everything else is downstream of those three artifacts converting one external operator into a public reference.
