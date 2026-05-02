---
date: 2026-05-02
status: observation
purpose: Survey 2026 state-of-the-art for self-improving coding-agent swarms — informs what chitin's autonomous swarm should aim at.
---

# Self-improving swarm SoTA (2026-05-02)

## The question

Slice 7 shipped chitin's autonomous swarm. It can drain a backlog and
open PRs. The user asked: *what should it actually be doing?* This doc
surveys what 2026 self-improving systems are actually built around, so
chitin's roadmap targets real capabilities rather than vibes.

## The four dominant patterns in 2026

### 1. Live tool synthesis (Live-SWE-agent)

[Live-SWE-agent] (NeurIPS-track 2026) holds 77.4% on SWE-bench Verified
without test-time scaling — current best for live agents. The mechanism
is small:

- Scaffold and agentic loop are **fixed** (~100 lines).
- The agent **synthesizes new tools at runtime** — bash scripts written
  to its environment.
- A single reflection prompt after each tool result asks "would creating
  a new tool accelerate progress?"

Critically: **no offline training, no scaffold rewrite.** The agent's
action space grows; the harness doesn't.

**Chitin parallel:** our backlog entries already encode "add a tool" as
a discrete unit. The Live-SWE pattern says we're on the right track —
expanding what the swarm can DO is safer and higher-yield than letting
it rewrite its own gate logic.

### 2. Iterative scaffold-tuning (MiniMax M2.7)

[MiniMax M2.7] ran 100+ rounds of:

```
analyze failure trajectories
  → plan changes to scaffold code
  → modify scaffold
  → run evals
  → compare results
  → keep or revert
```

Result: 30% performance gain, 56.22% on SWE-Pro. The model itself
participated in choosing what to change about its own scaffold.

**Chitin parallel:** PR #105 tonight — the swarm fixed its own
prompt-builder path-handling. That is exactly the M2.7 loop, except
chitin's "compare results" step is a human merge button, which is
correct for our risk profile. Don't automate the merge.

### 3. Parallel-agent RL coordination (Kimi K2.5)

[Kimi K2.5] uses Parallel-Agent RL to self-direct **swarms of up to 100
sub-agents over 1500-step workflows**. The headline isn't sub-agent
count — it's that one trained model can DIRECT a swarm without an
external orchestrator.

**Chitin doesn't compete with this.** Kimi is in the model-training
business; we're in the policy-and-audit business. But the implication
matters: in 12 months, single-model swarms will be commodity, and the
governance layer will be the thing customers pay for.

### 4. Multi-agent role pipelines (deterministic substrate)

[Lobster + OpenClaw multi-agent dev] showed the production pattern:
programmer / reviewer / tester agents in isolated workspaces, with YAML
pipeline plumbing and LLM creative work. Roles are fixed, prompts
evolve, the substrate is deterministic.

**Chitin parallel:** worktree-per-entry already gives isolation. We
don't have role-typed workers yet — every entry runs through one
generic dispatcher. Adding role typing (research / fix / test / doc) is
one of the highest-leverage next moves.

## What the field has settled on

Across all four patterns, three things show up everywhere:

1. **Verifiable outcomes.** Self-improvement converges where success
   is mechanically checkable — code compiles, tests pass, latency
   drops. Chitin's PR-with-CI already provides this. Don't extend
   autonomous loops to domains without a checker (UX, taste calls,
   product strategy).

2. **Bounded blast radius.** Live-SWE writes tools, not scaffold.
   M2.7 reverts on regression. Lobster keeps LLMs out of plumbing.
   Chitin's worktree + gate-policy + human-merge boundary is in line
   with what wins.

3. **Long-term memory + global observability** ([LangChain agentic
   engineering] post). "Leader agents coordinate and govern across
   agent groups, providing long-term memory for continuous improvement
   and global observability." Chitin's chain-of-events + decision
   ledger is the right shape for this. Surfacing it as a queryable
   layer (slice 8 candidate) is the leverage point.

## Where chitin is ahead of the field

- **Policy as code, not as prompt.** Most templates in
  awesome-openclaw-agents put guardrails in the system prompt.
  Chitin's `chitin.yaml` rules are declarative + auditable. The 2026
  industry-analyst literature ([CIO — agentic AI workflows 2026],
  [CNCF — autonomous enterprise platform-control forecast]) is
  converging on what we already ship.
- **Hash-linked decision chain.** Most projects log JSON; we log a
  chain. When auditors arrive in 2027, this matters.
- **Three-plane separation.** Temporal + OpenClaw + Chitin each own
  one job. The literature describes this as the "governable agent
  systems" pattern — most public stacks merge two of the three.

## Where chitin is behind the field

- **No autonomous evaluation harness.** We have CI as a binary signal.
  Live-SWE has a benchmark loop; M2.7 has eval-suite delta. Chitin
  needs a way to measure "did my change improve dispatcher reliability"
  beyond "the next PR opened cleanly." The workspace-level `/evolve`
  skill (defined in the parent workspace's `CLAUDE.md`, not in this
  repo) is the right shape; needs to be wired into a chitin-internal
  bench loop and exercised regularly.
- **No role-typed workers.** Every entry routes through one prompt
  template. Adding research / fix / refactor / test types lets us tune
  prompts per role and route to per-role models.
- **Single-step entries only.** No multi-step workflows where one
  worker hands off to another (programmer → reviewer → fixer). This
  is the Lobster/Live-SWE delta — sub-agent composition.
- **No swarm-level memory beyond the backlog file.** Workers don't
  share lessons. Live-SWE's tool registry is the analogue we lack —
  every dispatch starts fresh.

## Concrete next moves (in priority order)

These are landing as `in_design` entries in `docs/swarm-backlog.md` via
follow-up PR #110 (a separate, docs-only PR). They are not yet on `main`
when this observation merges — promote what looks right after #110
lands:

1. **Role-typed backlog entries** (T2 spec, T1 impl). Add a `role:`
   field to entries. Define `research`, `fix`, `test`, `doc`, `gov`
   to start. Dispatcher picks prompt template per role.

2. **Eval harness wiring** (T2). Periodic chitin-internal eval — pick a
   reference set of past PRs, replay them, score diff vs. merged
   version. Detects swarm regressions before they ship.

3. **Lessons-learned sidecar** (T1). After each merged swarm PR,
   distill one sentence ("when X file pattern, prefer Y") and append
   to `docs/swarm-lessons.md`. Dispatcher prepends recent lessons to
   the prompt.

4. **Multi-step flows** (T3 spec). One entry can spawn N sub-tasks via
   the same dispatcher. Programmer-then-reviewer is the simplest case.
   Lobster's `loop.maxIterations` is the prior art.

5. **OpenClaw mission-control hookup** (T2). Emit OTEL spans in the
   format mission-control expects, get a free fleet view.

6. **Live-tool synthesis (deferred)**. The Live-SWE pattern is hot but
   risky for chitin — letting workers write executables the gate
   doesn't know about widens the blast radius. Defer until role-typing
   and lessons-learned are in.

## What to NOT do, even though it sounds cool

- **Don't auto-merge.** Every public 2026 self-improvement system
  keeps a human (or a higher-trust agent) on the merge button. The
  CIO 2026 piece and the Anthropic trends report both flag this as
  the line that doesn't get crossed.
- **Don't let workers edit their own gate.** Already enforced by
  `no-governance-self-modification`. Resist the temptation to relax
  it for "obvious" cases — every public failure of a self-improving
  system in the past year touched this boundary.
- **Don't build a benchmark suite from scratch.** SWE-bench Verified
  and SWE-EVO are the standards. Wire chitin's eval harness to feed
  them, don't invent.

[Live-SWE-agent]: https://arxiv.org/html/2511.13646v3
[MiniMax M2.7]: https://www.marktechpost.com/2026/04/12/minimax-just-open-sourced-minimax-m2-7-a-self-evolving-agent-model-that-scores-56-22-on-swe-pro-and-57-0-on-terminal-bench-2/
[Kimi K2.5]: https://www.kimi.com/blog/kimi-k2-5
[Lobster + OpenClaw multi-agent dev]: https://dev.to/ggondim/how-i-built-a-deterministic-multi-agent-dev-pipeline-inside-openclaw-and-contributed-a-missing-4ool
[LangChain agentic engineering]: https://www.langchain.com/blog/agentic-engineering-redefining-software-engineering
[CIO — agentic AI workflows 2026]: https://www.cio.com/article/4134741/how-agentic-ai-will-reshape-engineering-workflows-in-2026.html
[CNCF — autonomous enterprise platform-control forecast]: https://www.cncf.io/blog/2026/01/23/the-autonomous-enterprise-and-the-four-pillars-of-platform-control-2026-forecast/

## Sources

- [Live-SWE-agent — arxiv 2511.13646v3](https://arxiv.org/html/2511.13646v3)
- [MiniMax M2.7 self-evolving agent](https://www.marktechpost.com/2026/04/12/minimax-just-open-sourced-minimax-m2-7-a-self-evolving-agent-model-that-scores-56-22-on-swe-pro-and-57-0-on-terminal-bench-2/)
- [Kimi K2.5 — visual agentic intelligence + 100-agent swarms](https://www.kimi.com/blog/kimi-k2-5)
- [SWE-EVO benchmark for long-horizon software evolution](https://arxiv.org/html/2512.18470v1)
- [Self-Improving AI Agents — 2026 Guide (o-mega)](https://o-mega.ai/articles/self-improving-ai-agents-the-2026-guide)
- [Anthropic 2026 Agentic Coding Trends Report (PDF)](https://resources.anthropic.com/hubfs/2026%20Agentic%20Coding%20Trends%20Report.pdf)
- [LangChain — Agentic Engineering: Swarms](https://www.langchain.com/blog/agentic-engineering-redefining-software-engineering)
- [Live-SWE-agent — code](https://github.com/OpenAutoCoder/live-swe-agent)
