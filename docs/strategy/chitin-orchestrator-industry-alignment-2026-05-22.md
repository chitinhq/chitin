# Chitin Orchestrator — Industry Alignment (2026-05-22)

**Author**: Claude Code (claudecode driver) via background research dispatch
**Date**: 2026-05-22
**Status**: Reference. Cited by constitution §7 (spec 092) but not in its normative path.
**Companion**: [spec 092 — codify-swarm-orchestrator](../../specs/092-codify-swarm-orchestrator/spec.md)

This is the research report that grounds chitin's orchestrator-as-swarm architecture in industry consensus. Lands as a separate PR from the §7 amendment (per Ares: "constitutional amendments need crisp diffs"). The constitution body deliberately omits vendor names; citations live here.

## 1. Executive summary

Five findings that matter for Chitin:

1. **The "single vs multi-agent" debate is over; orchestrator + isolated workers won.** Anthropic (multi-agent research system, 90% gain), Cognition (Devin manages Devins, March 2026), Google (A2A + ADK), and GitHub (Copilot coding agent, isolated branches) have all converged on a coordinator that scopes work and dispatches to subagents in isolated execution environments. Chitin's "orchestrator + worktree-per-work-unit" sits squarely in this consensus.
2. **Heterogeneous-model swarms are empirically supported.** Anthropic explicitly uses Opus as lead, Sonnet as workers. "Heterogeneous Swarms" (arXiv 2502.04510) shows 18.5% gains over role/weight baselines. Chitin's model-conditioned drivers (Ares/Clawta/Opus/Copilot) are aligned with research consensus, not a quirky bet.
3. **Anthropic's four-layer safety model (Model / Harness / Tools / Environment) maps almost exactly to Chitin's stack** — and identifies the harness layer (instructions + guardrails) as the right place to enforce policy. Chitin Kernel is the canonical "tools layer gate"; the orchestrator is the harness; the worktree is the environment. The model layer is the only one Chitin doesn't own. This is the correct partition.
4. **The harness `continue:false` bug Chitin is hitting is a known, named anti-pattern.** Claude Code issue #55754 documents exactly this: hooks that block without honoring `stop_hook_active` cause runaway loops. Anthropic's published guidance: the harness must (a) auto-set `stop_hook_active` after the first forced continuation, and (b) hard-cap iterations. This is a fixable harness bug, not an open research problem.
5. **MCP 2026-07-28 RC + 2026 roadmap pushes the protocol toward statelessness and Tasks-as-extension.** This validates Chitin's architecture (kernel as gate, drivers as MCP-natives, no MCP server hosting) but flags an upcoming opportunity: Tasks (with retry/expiry semantics, landing as an extension) is the natural protocol surface for orchestrator-dispatched work-units. Chitin should track this — it's where the industry's "single substrate" question is being resolved.

## 2. Anthropic's stated guidance

### 2.1 Orchestrator-worker is one of FIVE patterns, not the default

Anthropic's [Multi-agent coordination patterns](https://claude.com/blog/multi-agent-coordination-patterns) explicitly enumerates: **Generator-Verifier, Orchestrator-Subagent, Agent Teams, Message Bus, Shared State.** For orchestrator-subagent specifically:

> "Use this pattern when task decomposition is clear and subtasks have minimal interdependence. ... [Avoid] when subagents need to share intermediate findings or when sequential execution becomes a bottleneck."

Failure mode they call out: "The orchestrator becomes an information bottleneck."

### 2.2 Orchestrator-worker for research, not necessarily for write-heavy work

The [Building a Multi-Agent Research System](https://www.anthropic.com/engineering/built-multi-agent-research-system) post is explicit:

> "A multi-agent system with Claude Opus 4 as the lead agent and Claude Sonnet 4 subagents outperformed single-agent Claude Opus 4 by 90.2%."

But the same post warns about write-heavy work:

> "Subagents call tools to store their work in external systems, then pass lightweight references back."

And notes the synchronous-execution bottleneck:

> "Lead agents execute subagents synchronously, waiting for each set of subagents to complete before proceeding."

### 2.3 The "Building Effective Agents" decision tree

From [Building Effective Agents](https://www.anthropic.com/research/building-effective-agents):

> "Workflows offer predictability and consistency for well-defined tasks, whereas agents are the better option when flexibility and model-driven decision-making are needed at scale."

> "Developers should consider adding complexity 'only when it demonstrably improves outcomes.'"

Anti-pattern called out by name: framework over-reliance — "extra layers of abstraction that can obscure the underlying prompts and responses, making them harder to debug."

### 2.4 Anthropic's four-layer safety model

[Trustworthy agents in practice](https://www.anthropic.com/research/trustworthy-agents):

> "[Behavior] depends on all four layers working together. A well-trained model can still be exploited through a poorly configured **harness**, an overly permissive **tool**, or an exposed **environment**."

The Model layer is Anthropic's; the other three are the integrator's responsibility. Tool gating belongs at the **tools** layer with MCP-style "allow / needs approval / block" per action.

### 2.5 HITL vs HOTL distinction, and the production failure mode

Anthropic distinguishes HITL (block until approval) from HOTL (monitor + intervene). Critically, their own data shows HITL is failing at production scale:

> "93% of permission prompts approved without reading, clarification rate of just 16.4% on complex tasks."

Implication: per-action approvals don't scale; **plan-level review** does:

> "Rather than asking for approval for each action one-by-one, Claude shows the user its intended plan of action up-front."

### 2.6 Subagents in the Claude Agent SDK

[Building agents with the Claude Agent SDK](https://claude.com/blog/building-agents-with-the-claude-agent-sdk):

> "Subagents use their own isolated context windows, and only send relevant information back to the orchestrator, rather than their full context."

Loop pattern: **"gather context → take action → verify work → repeat."** Verification is explicitly named as a phase — Chitin's chain + Argus/Sentinel is the verification surface.

## 3. Community / research consensus where Anthropic is silent

### 3.1 Cognition's counter-position is more nuanced than the headline

[Don't Build Multi-Agents](https://cognition.ai/blog/dont-build-multi-agents) — two principles:

> "Share context, and share full agent traces, not just individual messages."
> "Actions carry implicit decisions, and conflicting decisions carry bad results."

But by March 2026, Cognition shipped [Devin can now Manage Devins](https://cognition.ai/blog/devin-can-now-manage-devins):

> "Each managed Devin is a full Devin, running in its own isolated virtual machine with its own terminal, browser, and development environment. ... The main Devin session acts as a coordinator: it scopes the work, assigns each piece to a managed Devin, monitors progress, resolves any conflicts, and compiles the results."

**Reconciliation:** Cognition's original objection was to subagents that share *partial* context and produce *conflicting writes*. Their later architecture (full VM isolation + coordinator + read-only trajectory inspection) is precisely the orchestrator-worker pattern done with proper isolation. The convergence on isolated-execution + coordinator is industry-wide.

### 3.2 Heterogeneous swarms beat homogeneous ones

[Heterogeneous Swarms paper](https://arxiv.org/pdf/2502.04510) — 18.5% over 15 role/weight baselines across 12 tasks. [TagRouter](https://arxiv.org/pdf/2506.12473) and [InferenceDynamics](https://arxiv.org/pdf/2505.16303) both show capability-tag routing beats uniform pools on cost/accuracy frontier.

### 3.3 Worktree-per-task is the de facto isolation primitive

[Multi-Agent AI Coding Workflow: Git Worktrees That Scale](https://blog.appxlab.io/2026/03/31/multi-agent-ai-coding-workflow-git-worktrees/) and [Augment Code's guide](https://www.augmentcode.com/guides/git-worktrees-parallel-ai-agent-execution) both prescribe Chitin's exact pattern: branch per agent, separate working directory, shared `.git`. Known limitation: filesystem only; ports, DBs, env state still collide.

### 3.4 Single point of failure on the orchestrator is real

[Beam.ai's six patterns](https://beam.ai/agentic-insights/multi-agent-orchestration-patterns-production):

> "A single-process orchestrator coordinating dozens of parallel agents is the easiest scaling cliff to fall off — you should move to durable execution, partition workflows by tenant, and treat orchestrator capacity as a first-class metric."

This is exactly why OpenAI runs Codex on Temporal in production. Chitin's choice of Temporal as the orchestrator host puts it in the same architectural class as the most heavily-stressed AI orchestrator in industry today.

### 3.5 A2A is becoming the agent-to-agent protocol layer; MCP stays tool-layer

[Zylos research on protocol convergence](https://zylos.ai/research/2026-03-26-agent-interoperability-protocols-mcp-a2a-acp-convergence): MCP is "vertical" (agent → tools); A2A is "horizontal" (agent ↔ agent). By April 2026, A2A has 150+ organizations and is a Linux Foundation project. Anthropic has not endorsed A2A; the standard story is "MCP + A2A together."

### 3.6 Cancellation propagation: push vs poll, both valid

[Kenhuang on cancellation](https://kenhuangus.substack.com/p/chapter-2-cancellation-and-abort) — Claude Code uses hierarchical AbortController (push); Hermes uses thread-scoped `_interrupt_requested` flag (poll). Both converge on the invariant: "one public `interrupt()` method at the harness root that propagates the cancellation everywhere automatically."

## 4. Chitin alignment check

Six places where Chitin's architecture matches best practice:

1. **Orchestrator-worker with isolated worktrees** matches Anthropic's pattern #2 and Cognition's Devin-manages-Devins. Citation: [Multi-agent coordination patterns](https://claude.com/blog/multi-agent-coordination-patterns) + [Devin can now Manage Devins](https://cognition.ai/blog/devin-can-now-manage-devins).
2. **Model-conditioned driver specialization** (Ares/Clawta/Opus/Copilot) matches Anthropic's Opus-lead + Sonnet-worker recipe and the heterogeneous-swarms research. Capability-tag routing is empirically validated.
3. **Spec → plan → tasks → DAG** is the orchestrator-subagent pattern done right per Anthropic's "scaling rules" guidance: detailed task descriptions with objectives, output formats, tool guidance, task boundaries. This is precisely what Anthropic says vague task descriptions get wrong ("research the semiconductor shortage" → duplicated work).
4. **Chitin Kernel as the tools-layer gate** sits at exactly the layer Anthropic's four-layer model identifies as a "shared responsibility" choke point. Policy enforcement at the orchestrator/tools boundary, not the model, matches [Sondera's attack analysis](https://blog.sondera.ai/p/anthropic-attack-agent-security-blueprint): "The malicious intent lived in the orchestration layer, not in any single, isolated request."
5. **Hash-linked audit chain** matches the "Governance-Aware Agent Telemetry" reference architecture and the broader "delegation logs must be immutable, time-stamped, and cryptographically verifiable" consensus.
6. **Temporal as the orchestrator host** matches OpenAI Codex's production architecture and the Temporal-for-AI guidance: deterministic workflow + non-deterministic activities, automatic checkpoint/replay on crash. Avoids the single-process-orchestrator scaling cliff.

## 5. Chitin divergence flags

Six places where Chitin diverges from best practice:

1. **Harness ignores `continue:false` from agents — agents loop.** *(b) accidental divergence to fix.* This is the canonical Claude Code stop-hook bug (issue #55754). The harness must auto-set `stop_hook_active` after the first forced continuation and cap iterations. Anthropic published the answer; Chitin's harness just isn't honoring it. **Spec 091** addresses this directly.
2. **Drivers communicate through the orchestrator, not directly — but there's no formal A2A surface.** *(c) genuine open question.* If the swarm grows beyond ~5-7 driver types, A2A is the emerging standard for horizontal coordination. Today, all communication routes through the activity log; that's fine, but worth keeping the door open for A2A as the bus protocol if the swarm ever spans organizational boundaries.
3. **Top-of-funnel ad-hoc work and DAG-dispatched work share only the kernel gate — no unified work-unit handle.** *(c) genuine open question.* MCP Tasks (now an extension in the 2026-07-28 RC) provides exactly the abstraction needed: "a server can answer `tools/call` with a task handle, and the client drives it with `tasks/get`, `tasks/update`, and `tasks/cancel`." This is the protocol-level way to unify chat-driven work and structured work under one substrate.
4. **No plan-level review checkpoint by default.** *(b) accidental divergence to fix.* Anthropic's own data: per-action approvals are rubber-stamped 93% of the time; plan-level review is what works. Chitin has spec-kit (which IS a plan-level review), but it's spec-author-only; there's no formal "operator sees the orchestrator's intended DAG before dispatch" checkpoint. Cheap to add.
5. **Synchronous subagent execution in the DAG walker — no streaming of subagent state back to the operator surface.** *(c) genuine open question.* Anthropic flagged this as their own current limitation. Chitin inherits the same constraint: "waiting for each set of subagents to complete before proceeding." Fix is non-trivial (streaming partial results, operator can steer mid-flight). Worth tracking as research advances.
6. **No formal "verification" phase in the agent loop.** *(a) deliberate good-reason divergence with a caveat.* Anthropic's gather → act → **verify** loop names verification as a phase. Chitin's chain + Argus/Sentinel does this *after* the work unit completes, not within the agent's loop. This is a deliberate choice (verification is a separate driver responsibility) and aligns with Cognition's "Actions carry implicit decisions" principle (verification should be deterministic, not LLM-judged). Caveat: there is no documented place where a work unit's PR-level verifier produces a structured failure-routing finding back to the orchestrator. Sentinel touches this but isn't formally wired as a verifier-driver.

## 6. Specific recommendations

### Priority 1 — Fix the harness `continue:false` bug

**What:** Make the harness honor `stop_hook_active` and cap iterations. After the first forced continuation, subsequent block signals must not re-block. Hard cap of N iterations.
**Why:** Without this, every driver Chitin governs can be sent into a runaway loop by a misconfigured policy. Highest-leverage safety win on the table.
**Ship-test:** A test harness that submits a `continue:false` to every driver and confirms the loop terminates after N iterations with `failure-kind=stop_signal_ignored` stamped in the chain.
**Status in Chitin:** [Spec 091](../../specs/091-fix-clawta-lockdown-loop/spec.md) addresses this. ACs added per Ares's review.

### Priority 2 — Adopt MCP Tasks (extension) as the unified work-unit handle

**What:** Wrap orchestrator-dispatched work units as MCP Tasks. `tools/call` returns a task handle; ad-hoc and DAG-dispatched work both use the same `tasks/get`, `tasks/update`, `tasks/cancel` surface. The kernel gate stays where it is — Tasks just adds a uniform handle.
**Why:** This is the "single substrate" question. MCP 2026-07-28 RC + 2026 roadmap converge on Tasks as the abstraction. Adopting it now means Chitin's work units are introspectable through any MCP-aware client — including future operator surfaces.
**Ship-test:** An MCP client (e.g., Claude Desktop) can list, inspect, and cancel any orchestrator-running work unit by Task ID, without going through the activity log.
**Status in Chitin:** Future spec; gated behind spec 091 landing (Clawta's sequencing point — don't build a surface on top of a gate that can't reliably say stop).

### Priority 3 — Add a plan-level review checkpoint

**What:** Before the orchestrator walks a freshly-derived DAG, render the DAG + spec-kit chain to the operator surface and require an explicit "go" (configurable per spec). This is the spec-kit equivalent of Claude Code's "real-time to-do checklist."
**Why:** Anthropic's data shows per-tool approvals are rubber-stamped; plan-level review is the only HITL pattern that survives production. Chitin has all the inputs (spec, plan, tasks, DAG) — they just aren't surfaced as a reviewable artifact between derivation and dispatch.
**Ship-test:** An operator approves a DAG; a separate test simulates an operator rejecting it; the orchestrator records "plan_rejected" on the chain and never dispatches.

### Priority 4 — Make verification a first-class driver

**What:** Promote the existing PR-level checks (CI + Copilot review + Argus/Sentinel signals) into a formal "verifier" driver class that the orchestrator dispatches as the terminal node of every work unit. Failed verification routes to a remediation work unit, not a human-only stop.
**Why:** Anthropic's gather → act → **verify** loop, plus Anthropic's [generator-verifier coordination pattern](https://claude.com/blog/multi-agent-coordination-patterns) ("Use this pattern when output quality is critical and evaluation criteria can be made explicit"). Chitin's PRs already pass through verification; formalizing the driver class makes it observable and routable.
**Ship-test:** A failed verification produces a `verifier_finding` row on the chain with a routing decision; the orchestrator either dispatches remediation or escalates.

### Priority 5 — Define a "no-driver-bypass" invariant test

**What:** Write a property test: for every PR landed on `main`, there exists an orchestrator-dispatched work-unit ID, and that work-unit's chain trail shows kernel-gated tool calls only. Run nightly. The constitutional rule ("implementation MUST go through orchestrator + DAG") must be empirically auditable.
**Why:** The rule is stated (constitution §7); no automated check enforces it. This is a Knuthian invariant: state it in one sentence, then prove the code makes it hold. Sentinel is the natural home for the check.
**Ship-test:** Sentinel emits `invariant_violation:driver_bypass` if it finds a PR without an orchestrator work-unit ID, or a work-unit with un-gated tool calls.

### Priority 6 — Document the operator-surface routing matrix

**What:** Explicit doc: which operator surface (Discord, terminal, web console) connects to which class of work? Anthropic's HITL/HOTL distinction maps to this — terminal is HITL (operator paired with Claude Code), Discord is HOTL (operator monitors but does not block), web console reads-only. Today this is implicit.
**Why:** Without this, the question "should I do this in chat or as a DAG?" is answered ad hoc per session. Making it explicit reduces drift.
**Ship-test:** A new operator can answer "where do I file this work?" by reading one page.

### Priority 7 — Track A2A as a watch item, not an adoption item

**What:** Don't adopt A2A now. Do add a quarterly review: is the swarm spanning organizational boundaries? Are we federating with other agent systems? If yes, A2A is the protocol; if no, the activity log + Temporal is enough.
**Why:** A2A solves a problem Chitin doesn't have (cross-org agent federation). Adopting prematurely is the framework-over-reliance anti-pattern Anthropic explicitly names.

### Priority 8 — Cap orchestrator-spawned subagents per work unit

**What:** Enforce a hard cap (e.g., 5) on parallel subagents the orchestrator can spawn per work-unit; configurable per spec. Anthropic's published failure mode: "spawning 50+ subagents for simple queries."
**Why:** Multi-agent systems use 15× more tokens than single-agent. Without a cap, a single misclassified work unit can burn an order of magnitude more budget than intended.
**Ship-test:** Sentinel flags any work unit that exceeds the cap; chain records the cap value at dispatch time.

### Priority 9 — Make the chain queryable as the verifier's source of truth

**What:** Argus/Sentinel and the verifier driver should consume chain rows directly (not parallel logs). One source of truth, hash-linked, replay-able. (This may already be true; if so, document it.)
**Why:** Closed-loop principle: telemetry IS the enforcement signal.
**Ship-test:** Sentinel's input list is exactly `{chain rows}`; no other source.

### Priority 10 — Don't pursue full agent-to-agent direct messaging

**What:** Resist the temptation to add direct driver-to-driver messaging. Keep all communication mediated through the orchestrator / chain / activity log.
**Why:** Anthropic's research system warns about it: "Direct agent-to-agent communication: Subagent outputs filtered through lead agents caused information loss. Better pattern: Subagents call tools to store their work in external systems, then pass lightweight references back." Chitin's "chain + activity log as the shared substrate" IS the "external system" pattern.

## 7. MCP 2026-07-28 specifically — relevance for single-surface unification

The [MCP 2026-07-28 release candidate](https://blog.modelcontextprotocol.io/posts/2026-07-28-release-candidate/) has three changes that matter for Chitin's "single-substrate" question:

### 7.1 Stateless protocol

> "The `Mcp-Session-Id` header and the protocol-level session ... are also removed."
> "Any MCP request can land on any server instance ... a remote MCP server that previously needed sticky sessions, a shared session store, and deep packet inspection at the gateway can now run behind a plain round-robin load balancer."

**Implication for Chitin:** the kernel gate IS stateless — it consumes a tool call, evaluates against `chitin.yaml`, emits deny/allow, writes to chain. This is precisely the architecture the new MCP recommends. Chitin's "no MCP server hosting in the kernel" decision (2026-05-08) anticipated this.

### 7.2 Stateful applications, stateless protocol — the explicit-handle pattern

> "Agents 'mint an explicit handle ... from a tool and have the model pass it back as an ordinary argument on later calls.' This makes 'the state visible to the model rather than hidden away,' enabling agent composition."

**Implication for Chitin:** work-unit IDs should be MCP-visible handles. The orchestrator mints a work-unit ID; drivers pass it as an argument on every subsequent tool call; the kernel uses it as the chain-row correlator. This is what's needed to make chat-driven and DAG-driven work co-exist on one substrate.

### 7.3 Tasks graduates to an extension

> "A server can answer `tools/call` with a task handle, and the client drives it with `tasks/get`, `tasks/update`, and `tasks/cancel`."
> "Task creation is server-directed: the client advertises the extension and the server decides when a call should run as a task."

**Implication for Chitin:** this is exactly the orchestrator-dispatched work-unit lifecycle. The 2026 roadmap adds **retry semantics + expiry policies** as the next refinement. Chitin's orchestrator already handles retry + expiry through Temporal; aligning the user-facing API to the MCP Tasks shape means a single MCP client can drive both ad-hoc work (sync `tools/call`) and DAG work (`tools/call` → Task handle → `tasks/get`).

### 7.4 Concrete unification recipe

If the question is "how do chat-driven ad-hoc work and spec-driven structured work coexist under one orchestrator," the MCP-shaped answer is:

1. **Every work unit, ad-hoc or DAG, gets a Task handle.** Mint at intake.
2. **The kernel gates every `tools/call` regardless of origin** (already true).
3. **The activity log / chain indexes by task handle** (small change if not already).
4. **Operator surfaces (Discord, terminal, web) become MCP clients that drive task handles** via `tasks/get` / `tasks/update` / `tasks/cancel`.
5. **The orchestrator is just an MCP server that decides when a `tools/call` should return a task handle** vs run inline — exactly as the RC describes ("the server decides when a call should run as a task").

This is the single-surface unification, expressed in protocol terms the rest of the industry is converging on.

## Sources

- [Building Effective Agents](https://www.anthropic.com/research/building-effective-agents) — Anthropic
- [Building a Multi-Agent Research System](https://www.anthropic.com/engineering/built-multi-agent-research-system) — Anthropic
- [Multi-agent coordination patterns](https://claude.com/blog/multi-agent-coordination-patterns) — Anthropic
- [Trustworthy agents in practice](https://www.anthropic.com/research/trustworthy-agents) — Anthropic
- [Our framework for developing safe and trustworthy agents](https://www.anthropic.com/news/our-framework-for-developing-safe-and-trustworthy-agents) — Anthropic
- [Building agents with the Claude Agent SDK](https://claude.com/blog/building-agents-with-the-claude-agent-sdk) — Anthropic
- [Effective context engineering for AI agents](https://www.anthropic.com/engineering/effective-context-engineering-for-ai-agents) — Anthropic
- [Don't Build Multi-Agents](https://cognition.ai/blog/dont-build-multi-agents) — Cognition (Walden Yan)
- [Devin can now Manage Devins](https://cognition.ai/blog/devin-can-now-manage-devins) — Cognition
- [MCP 2026-07-28 release candidate](https://blog.modelcontextprotocol.io/posts/2026-07-28-release-candidate/) — MCP Blog
- [MCP 2026 Roadmap](https://blog.modelcontextprotocol.io/posts/2026-mcp-roadmap/) — MCP Blog
- [Claude Code issue #55754: Stop hook infinite loop](https://github.com/anthropics/claude-code/issues/55754) — GitHub
- [Cancellation & Abort Propagation: Claude Code vs Hermes](https://kenhuangus.substack.com/p/chapter-2-cancellation-and-abort) — Ken Huang
- [The Anthropic Attack: Architectural Blueprint](https://blog.sondera.ai/p/anthropic-attack-agent-security-blueprint) — Sondera
- [Heterogeneous Swarms](https://arxiv.org/pdf/2502.04510) — arXiv 2502.04510
- [TagRouter](https://arxiv.org/pdf/2506.12473) — arXiv 2506.12473
- [InferenceDynamics](https://arxiv.org/pdf/2505.16303) — arXiv 2505.16303
- [Multi-Agent AI Coding Workflow: Git Worktrees That Scale](https://blog.appxlab.io/2026/03/31/multi-agent-ai-coding-workflow-git-worktrees/)
- [Six Multi-Agent Orchestration Patterns for Production](https://beam.ai/agentic-insights/multi-agent-orchestration-patterns-production) — Beam.ai
- [Agent Interoperability Protocols 2026](https://zylos.ai/research/2026-03-26-agent-interoperability-protocols-mcp-a2a-acp-convergence) — Zylos
- [Anthropic's Shared Responsibility Security Model](https://www.backslash.security/blog/anthropics-shared-responsibility-security-model-for-ai-agents) — Backslash
- [Temporal for AI](https://temporal.io/solutions/ai) — Temporal
