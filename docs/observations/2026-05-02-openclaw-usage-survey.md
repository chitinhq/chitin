---
date: 2026-05-02
status: observation
purpose: Survey how OpenClaw is being used in the wild — feeds chitin's positioning and roadmap.
---

# OpenClaw usage survey (2026-05-02)

## Why this matters

Chitin's slice-1e bet is **chitin-as-openclaw-plugin**. That commits us to a
specific layer in someone else's stack. Before pouring more code into that
layer, we need to know: what is everyone *else* doing with OpenClaw, and
where is chitin's wedge real vs. imagined?

## What I found

### OpenClaw is consolidating into the local-agent OS slot

OpenClaw crossed 100k+ GitHub stars in early 2026 and now ships as the
default local-agent runtime in two production stacks:

- **NVIDIA NemoClaw** — Nemotron 3 Super models orchestrated by OpenClaw +
  OpenShell for sandboxed on-prem deployment.
- **ggondim's deterministic dev pipeline** — multi-agent code/review/test
  loop using OpenClaw's `sessions_send` + Lobster YAML workflows.
- **abhi1693/openclaw-mission-control** — agent fleet dashboard via the
  Gateway protocol.
- **mergisi/awesome-openclaw-agents** — 162 production-ready agent
  templates (`SOUL.md`-based) across 19 categories.

Reading these together: OpenClaw is winning the **runtime-and-protocol**
layer. It is not winning the **durable-orchestration** or **policy-gate**
layers.

### Temporal integration was REJECTED upstream

GitHub issue [openclaw#10164] proposed native Temporal integration for
durable workflows, scheduling, and webhook routing. It cited four concrete
pain points:

1. Gateway crashes lose in-progress work.
2. No unified view of running/pending work.
3. Multi-step workflows with approval gates are manual.
4. Webhooks spawn fresh agents instead of coordinating existing ones.

**Status: Closed as not planned.** Zero discussion, zero reactions, no
maintainer pushback published — just a quiet "no."

This is a strategic gift to chitin. Our three-plane architecture
(Temporal control / OpenClaw execution / Chitin enforcement, locked
2026-04-30) is filling a gap the OpenClaw maintainers have explicitly
declined to fill themselves. We are not redundant with their roadmap —
we are *complementary* to a bounded one.

### The orchestration-pattern community is "don't orchestrate with LLMs"

ggondim's dev.to post is the clearest articulation:

> YAML pipeline config handles plumbing — routing, counting, retrying.
> LLM agents handle creative work — writing code, analyzing reviews.
> This inverts typical orchestration: don't orchestrate with LLMs.

Chitin already does this (Temporal does plumbing, agents do creative).
Worth keeping that as an explicit design principle in chitin's docs —
multiple ecosystems are converging on it.

The reusable pattern from the Lobster work:

- Session keys encoded as `pipeline:<project>:<role>` for deterministic
  agent addressing.
- Loop semantics in YAML (`loop.maxIterations`, `loop.condition`)
  instead of LLM-driven loops.
- Agents in **isolated workspaces with role-specific tools and models**
  — same shape as our worktree-per-task pattern.

### What people are NOT building on top of OpenClaw

- A policy/governance plane with declarative rules. The OpenClaw runtime
  has hooks (`before_tool_call`, etc.) but no general policy system —
  every agent template re-implements its own guardrails ad hoc.
- A swarm dispatcher with backlog drain semantics. Mission-control is a
  dashboard, not an autonomous worker.
- A chain-of-events log with hash linkage. OTEL spans are the closest
  thing — and ggondim's Lobster engine logs are flat JSON.

These are all chitin's lanes. The 162-template registry confirms the
demand surface: every template owner is solving auth/policy/audit *in
prompts* because there is no shared substrate. Chitin can be that
substrate.

## Implications for chitin's roadmap

1. **Position chitin as the policy + audit plane for the OpenClaw
   ecosystem, not as a competing runtime.** Slice-1e's
   chitin-as-openclaw-plugin shape is right; the framing should
   emphasize "we fill what OpenClaw's maintainers said no to."

2. **Extract the Temporal-as-control-plane piece as a reusable module
   for OpenClaw users who hit issue #10164's pain.** This is a public
   demo opportunity for the 2026-05-07 talk: "OpenClaw said no to
   Temporal; here is a community plugin that gives you durable
   workflows + governance in one."

3. **Adopt the `pipeline:<project>:<role>` session-key convention** for
   chitin's own worker addressing where it touches OpenClaw. Free
   interop with the rest of the ecosystem.

4. **Borrow the SOUL.md template format** for the soul system rather
   than inventing our own. Chitin already has soul artifacts at
   `souls/canonical/<name>.md` — aligning the schema with the
   awesome-openclaw-agents registry would let chitin contribute back.

5. **Skip building a registry / template gallery for chitin.** That
   layer is owned by the awesome-openclaw-agents repo. We integrate,
   we don't compete.

## What we should NOT do

- Don't build a new runtime. OpenClaw owns the runtime slot.
- Don't build a workflow YAML engine. Lobster + Temporal between them
  cover this; chitin sits above as policy.
- Don't try to compete with mission-control on dashboards. Our
  observability surface should emit OTEL spans (already locked, see
  `project_otel_emit_direction.md`) and let mission-control + Grafana
  consume them.

## Followups (added to swarm backlog)

- `openclaw-issue-10164-public-comment` — open a PR to the OpenClaw
  repo pointing at chitin as a community-built answer to the Temporal
  request. Soft outreach, links not commitments.
- `chitin-as-policy-plane-positioning-doc` — rewrite the README's
  one-liner to lead with "policy and audit plane for AI coding agents,
  built to compose with OpenClaw + Temporal."
- `soul-md-schema-alignment` — diff our `souls/canonical/*.md`
  frontmatter against the SOUL.md format awesome-openclaw-agents uses;
  publish migration notes if drift is small.

[openclaw#10164]: https://github.com/openclaw/openclaw/issues/10164

## Sources

- [OpenClaw — main repo](https://github.com/openclaw/openclaw)
- [OpenClaw Temporal integration — issue #10164 (closed/not planned)](https://github.com/openclaw/openclaw/issues/10164)
- [How I Built a Deterministic Multi-Agent Dev Pipeline Inside OpenClaw — ggondim, dev.to](https://dev.to/ggondim/how-i-built-a-deterministic-multi-agent-dev-pipeline-inside-openclaw-and-contributed-a-missing-4ool)
- [NVIDIA NemoClaw + OpenClaw — secure local AI agent](https://developer.nvidia.com/blog/build-a-secure-always-on-local-ai-agent-with-nvidia-nemoclaw-and-openclaw/)
- [openclaw-mission-control — abhi1693](https://github.com/abhi1693/openclaw-mission-control)
- [awesome-openclaw-agents — 162 templates registry](https://github.com/mergisi/awesome-openclaw-agents)
- [OpenClaw Design Patterns — Orchestration (Ken Huang)](https://kenhuangus.substack.com/p/openclaw-design-patterns-part-3-of)
- [OpenClaw multi-agent orchestration guide — Zen van Riel](https://zenvanriel.com/ai-engineer-blog/openclaw-multi-agent-orchestration-guide/)
