# Graph Report - ./docs  (2026-04-29)

## Corpus Check
- 67 files · ~203,627 words
- Verdict: corpus is large enough that graph structure adds value.

## Summary
- 270 nodes · 327 edges · 19 communities detected
- Extraction: 85% EXTRACTED · 15% INFERRED · 0% AMBIGUOUS · INFERRED: 48 edges (avg confidence: 0.77)
- Token cost: 0 input · 0 output

## Community Hubs (Navigation)
- [[_COMMUNITY_Soul Archetype Research|Soul Archetype Research]]
- [[_COMMUNITY_Kernel Architecture & Layout|Kernel Architecture & Layout]]
- [[_COMMUNITY_ACP Refusal & Demo Surface|ACP Refusal & Demo Surface]]
- [[_COMMUNITY_Implementation Plans|Implementation Plans]]
- [[_COMMUNITY_V1 Archive & Canonicalization|V1 Archive & Canonicalization]]
- [[_COMMUNITY_Foundational Design Specs|Foundational Design Specs]]
- [[_COMMUNITY_OTELGenAI Translation|OTEL/GenAI Translation]]
- [[_COMMUNITY_Governance v1 & Autonomy Post-Mortem|Governance v1 & Autonomy Post-Mortem]]
- [[_COMMUNITY_Hook Performance & Cost Governance|Hook Performance & Cost Governance]]
- [[_COMMUNITY_Soul Library Operations|Soul Library Operations]]
- [[_COMMUNITY_OTEL Receiver Code|OTEL Receiver Code]]
- [[_COMMUNITY_Toolchain|Toolchain]]
- [[_COMMUNITY_Phase 1.5 Plan-Spec Pairs|Phase 1.5 Plan-Spec Pairs]]
- [[_COMMUNITY_Strategic Arc & Audience|Strategic Arc & Audience]]
- [[_COMMUNITY_Local-First Posture|Local-First Posture]]
- [[_COMMUNITY_chitin.yaml Config|chitin.yaml Config]]
- [[_COMMUNITY_Surface Neutrality|Surface Neutrality]]
- [[_COMMUNITY_Cost-Gov v3 Slice|Cost-Gov v3 Slice]]
- [[_COMMUNITY_Python (deferred)|Python (deferred)]]

## God Nodes (most connected - your core abstractions)
1. `gov.Gate.Evaluate API` - 11 edges
2. `Soul archetype survey (da Vinci cross-domain)` - 11 edges
3. `Adversarial synthesis (Socrates)` - 11 edges
4. `SP-0 openclaw OTEL capture & schema inventory` - 10 edges
5. `Soul archetype survey (Lovelace generator-fit)` - 10 edges
6. `Hook payload capture observation` - 9 edges
7. `Soul archetype survey (Sun Tzu agent-framework terrain)` - 9 edges
8. `chitin health Command` - 8 edges
9. `Copilot CLI Without Fear Talk Runbook` - 8 edges
10. `Empirical trait/stage factor analysis (Shannon)` - 8 edges

## Surprising Connections (you probably didn't know these)
- `Tiny OTLP/HTTP Receiver` --semantically_similar_to--> `OTEL Projection`  [INFERRED] [semantically similar]
  docs/observations/fixtures/2026-04-20-openclaw-otel-capture/receiver.py → docs/thesis.md
- `Tiny OTLP/HTTP Receiver` --semantically_similar_to--> `OTEL Projection (one-way bridge)`  [INFERRED] [semantically similar]
  docs/observations/fixtures/2026-04-20-openclaw-otel-capture/receiver.py → docs/event-model.md
- `Plan: Hermes Dialect Adapter v1` --references--> `Code: openclaw.go translator (SP-1, PR #35)`  [EXTRACTED]
  docs/superpowers/plans/2026-04-21-hermes-dialect-adapter-v1.md → go/execution-kernel/internal/ingest/openclaw.go
- `Plan: SP-2 Complete openclaw Translator` --references--> `Code: openclaw.go translator (SP-1, PR #35)`  [EXTRACTED]
  docs/superpowers/plans/2026-04-21-sp2-complete-openclaw-translator.md → go/execution-kernel/internal/ingest/openclaw.go
- `gov.Gate.Evaluate API` --semantically_similar_to--> `gov.Gate`  [INFERRED] [semantically similar]
  docs/governance-setup.md → docs/thesis.md

## Hyperedges (group relationships)
- **Three Drivers Around gov.Gate** — thesis_govgate, thesis_claudecode, thesis_copilotcli, thesis_openclaw [EXTRACTED 1.00]
- **Layer Contracts v1 Four Invariants** — layercontracts_kernelauthority, layercontracts_driverconstraint, layercontracts_routingscope, layercontracts_aggregationrole [EXTRACTED 1.00]
- **Three Analysis Output Streams from Event Chain** — operatingmodel_threeoutputstreams, governancedebtledger_index, architecture_eventchainondisk [EXTRACTED 1.00]
- **Five-pass soul archetype audit corpus** — soul_archetype_survey_davinci_doc, soul_archetype_survey_lovelace_doc, soul_archetype_survey_suntzu_doc, soul_archetype_synthesis_socrates_doc, trait_factor_analysis_shannon_doc [EXTRACTED 1.00]
- **Translator design framings (A/B/C tradeoff)** — openclaw_otel_capture_framing_a, openclaw_otel_capture_framing_b, openclaw_otel_capture_framing_c [EXTRACTED 1.00]
- **Tool-boundary governance (closure of three root causes)** — autonomy_v1_post_mortem_root_causes, autonomy_v1_post_mortem_extract_shell_intent, autonomy_v1_post_mortem_governance_v1_pr_45 [EXTRACTED 1.00]
- **All hermes-related plans (probe, dialect adapter, staged tick)** —  [INFERRED 0.85]
- **openclaw OTEL translator workstream (SP-0 evidence + SP-1 + SP-2)** —  [EXTRACTED 1.00]
- **Governance lineage (v1 -> copilot CLI -> cost-governance superseded -> v3)** —  [INFERRED 0.90]
- **Cost-Governance Kernel evolution chain** —  [EXTRACTED 1.00]
- **Hermes spec evolution chain (probe -> adapter -> staged tick)** —  [EXTRACTED 1.00]
- **Copilot CLI integration evolution chain** —  [EXTRACTED 1.00]

## Communities

### Community 0 - "Soul Archetype Research"
Cohesion: 0.07
Nodes (44): OpenClaw SOUL.md primary-source verification, Filename-only convergence (not system convergence), OpenClaw has no taxonomy/canonical set/lifecycle, Retraction of earlier survey claim, OpenClaw SOUL.md (per-workspace persona file), souls/canonical/ (8 canonical souls), souls/elo.md (opinion-weighted scoreboard), Belbin team roles (9) (+36 more)

### Community 1 - "Kernel Architecture & Layout"
Cohesion: 0.06
Nodes (38): apps/cli, Event Chain On Disk (.chitin/events-<run_id>.jsonl), Go Kernel, gov.db Envelope Counter, Hard Rule: Go-only Side Effects, chitin-kernel Subcommands, Layer Map, libs/adapters/<surface>/ (+30 more)

### Community 2 - "ACP Refusal & Demo Surface"
Cohesion: 0.07
Nodes (34): ACP Wire Protocol (Zed Industries), Copilot SDK On-Refusal Behavior (rung 3/4), Deferred Synthetic-Tool-Response Paths (A/B/C), Milestone B Standard Refusal Decision, ACP Refusal-Frame Visibility Spike, ToolCallStatus Closed Enum, Contingency Paths, Demo 1 — Force-Push Warmup (+26 more)

### Community 3 - "Implementation Plans"
Cohesion: 0.11
Nodes (25): Plan: Hermes Probe, Plan: SP-1 openclaw-dialect OTEL Translator, Plan: Hermes Dialect Adapter v1, Plan: SP-2 Complete openclaw Translator, Plan: Chitin Governance v1, Plan: Hermes Staged Tick v1, Plan: Copilot CLI Governance v1, Plan: Copilot SDK Feasibility Spike (+17 more)

### Community 4 - "V1 Archive & Canonicalization"
Cohesion: 0.09
Nodes (23): .chitin/ Resolution Order, chitinhq/chitin-archive (v1 frozen), Extracted to v2 Reference Reimpls, Left Behind (swarm/orchestration), Other Archived Repos (clawta, octi, sentinel, ...), Parked for Phase 2 Governance, action_type Closed Enum (read|write|exec|git|net|dangerous), canon Package (+15 more)

### Community 5 - "Foundational Design Specs"
Cohesion: 0.17
Nodes (20): Dogfood-Driven Governance-Debt Ledger Design Spec, Phase 1.5 Observability Chain Contract Design, Hermes Probe Design, OpenClaw Adapter Implementation Design Addendum, OTEL-Transport Ingest Workstream Meta-Spec, SP-1 openclaw-dialect OTEL Translator Design, Hermes Dialect Adapter v1 Design, SP-2 Complete openclaw Translator (Spans-Only v1) Design (+12 more)

### Community 6 - "OTEL/GenAI Translation"
Cohesion: 0.14
Nodes (19): api_call_count is per-turn not per-session, chitin-sink hermes plugin, hermes-dialect-adapter-v1 plan, Hermes post_api_request capture, Token-key deviation (input_tokens/output_tokens), @openclaw/diagnostics-otel plugin, SP-0 openclaw OTEL capture & schema inventory, Framing A: pivot to gen_ai.*-compliant first consumer (+11 more)

### Community 7 - "Governance v1 & Autonomy Post-Mortem"
Cohesion: 0.15
Nodes (19): Canary PR #43: delete go/ directory, Hermes autonomy v1 post-mortem, extractShellIntent canonical action vocab, chitin governance v1 PR #45, Three root causes (etiquette, execute_code, gate-ignore), openclaw acpx ACP driver, @github/copilot-sdk MIT public preview, Copilot CLI openclaw research spike (+11 more)

### Community 8 - "Hook Performance & Cost Governance"
Cohesion: 0.14
Nodes (17): cost-governance-kernel-design spec, Daemon-mode escape hatch (deferred), Claude Code hook cold-start latency verdict, p95 = 3ms cold-start measurement, capture.sh tee script, Observability chain-contract spec, Dispatch audit findings, hook-dispatch.ts (claude-code adapter) (+9 more)

### Community 9 - "Soul Library Operations"
Cohesion: 0.22
Nodes (9): souls/canonical/ + souls/experimental/, Curie Hook-Payload Capture, Invariant uninstall(install(s)) == s, PR #19 Closed Without Merge (da Vinci strike), Phase B Finish Quorum (8/8 Knuth), Soul-Archetype Research Corpus (5 surveys + synthesis), souls/elo.md Trainer's-Note Scoreboard, Freeze Period 2026-04-19 -> 2026-06-18 (+1 more)

### Community 10 - "OTEL Receiver Code"
Cohesion: 0.33
Nodes (2): Handler, BaseHTTPRequestHandler

### Community 11 - "Toolchain"
Cohesion: 0.5
Nodes (4): Go 1.22+ Project, Two-Pass Module Boundary Lint (Oxlint+ESLint), Nx Top-Level Orchestrator, Vite+ Toolchain (vp CLI)

### Community 12 - "Phase 1.5 Plan-Spec Pairs"
Cohesion: 0.5
Nodes (4): Plan: Dogfood-Driven Governance-Debt Ledger, Plan: Phase 1.5 Observability Chain Contract, Spec: Dogfood Debt Ledger Design (2026-04-19), Spec: Observability Chain Contract Design (2026-04-19)

### Community 13 - "Strategic Arc & Audience"
Cohesion: 1.0
Nodes (2): Audience Sequencing A1->A2->A4, Strategic Arc

### Community 14 - "Local-First Posture"
Cohesion: 1.0
Nodes (2): Local-Only by Default (Phase 1), Local-First

### Community 15 - "chitin.yaml Config"
Cohesion: 1.0
Nodes (1): chitin.yaml

### Community 16 - "Surface Neutrality"
Cohesion: 1.0
Nodes (1): Surface Neutrality

### Community 17 - "Cost-Gov v3 Slice"
Cohesion: 1.0
Nodes (1): Cost-gov v3 Slice

### Community 18 - "Python (deferred)"
Cohesion: 1.0
Nodes (1): Python (deferred to python/analysis)

## Knowledge Gaps
- **107 isolated node(s):** `Claude Code Driver`, `Strategic Arc`, `Audience Sequencing A1->A2->A4`, `Principle: Real Execution Before Policy`, `chitin.yaml` (+102 more)
  These have ≤1 connection - possible missing edges or undocumented components.
- **Thin community `OTEL Receiver Code`** (6 nodes): `Handler`, `.do_POST()`, `.log_message()`, `main()`, `BaseHTTPRequestHandler`, `receiver.py`
  Too small to be a meaningful cluster - may be noise or needs more connections extracted.
- **Thin community `Strategic Arc & Audience`** (2 nodes): `Audience Sequencing A1->A2->A4`, `Strategic Arc`
  Too small to be a meaningful cluster - may be noise or needs more connections extracted.
- **Thin community `Local-First Posture`** (2 nodes): `Local-Only by Default (Phase 1)`, `Local-First`
  Too small to be a meaningful cluster - may be noise or needs more connections extracted.
- **Thin community `chitin.yaml Config`** (1 nodes): `chitin.yaml`
  Too small to be a meaningful cluster - may be noise or needs more connections extracted.
- **Thin community `Surface Neutrality`** (1 nodes): `Surface Neutrality`
  Too small to be a meaningful cluster - may be noise or needs more connections extracted.
- **Thin community `Cost-Gov v3 Slice`** (1 nodes): `Cost-gov v3 Slice`
  Too small to be a meaningful cluster - may be noise or needs more connections extracted.
- **Thin community `Python (deferred)`** (1 nodes): `Python (deferred to python/analysis)`
  Too small to be a meaningful cluster - may be noise or needs more connections extracted.

## Suggested Questions
_Questions this graph is uniquely positioned to answer:_

- **Why does `gov.Gate.Evaluate API` connect `ACP Refusal & Demo Surface` to `Kernel Architecture & Layout`, `V1 Archive & Canonicalization`?**
  _High betweenness centrality (0.075) - this node is a cross-community bridge._
- **Why does `gov.Gate` connect `Kernel Architecture & Layout` to `ACP Refusal & Demo Surface`?**
  _High betweenness centrality (0.063) - this node is a cross-community bridge._
- **Are the 2 inferred relationships involving `gov.Gate.Evaluate API` (e.g. with `gov.Gate` and `Parked for Phase 2 Governance`) actually correct?**
  _`gov.Gate.Evaluate API` has 2 INFERRED edges - model-reasoned connections that need verification._
- **What connects `Claude Code Driver`, `Strategic Arc`, `Audience Sequencing A1->A2->A4` to the rest of the system?**
  _107 weakly-connected nodes found - possible documentation gaps or missing edges._
- **Should `Soul Archetype Research` be split into smaller, more focused modules?**
  _Cohesion score 0.07 - nodes in this community are weakly interconnected._
- **Should `Kernel Architecture & Layout` be split into smaller, more focused modules?**
  _Cohesion score 0.06 - nodes in this community are weakly interconnected._
- **Should `ACP Refusal & Demo Surface` be split into smaller, more focused modules?**
  _Cohesion score 0.07 - nodes in this community are weakly interconnected._