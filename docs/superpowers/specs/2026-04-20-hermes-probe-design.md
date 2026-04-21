# Hermes Probe — Design

**Date:** 2026-04-20
**Status:** Design. Ready for user review, then handoff to writing-plans for an execution plan.
**Parent context:** Strategic-roadmap north star — "fully autonomous swarm + product that builds itself" (memory: `project_strategic_roadmap.md`). Local stack (RTX 3090 + Ollama Cloud subscription) is currently underexercised. Hermes on Ollama (`ollama launch hermes`) is the current candidate swarm-node primitive.
**Related workstream (not coupled):** OTEL GenAI ingest (`docs/superpowers/specs/2026-04-20-otel-genai-ingest-workstream-design.md`). The probe may opportunistically bank a Hermes OTEL capture, but it does NOT build any ingest code — the probe and the ingest workstream are independent tracks.

## Preamble

Phase F of the dogfood-debt-ledger plan established OTEL GenAI as chitin's canonical ingest direction, with openclaw as the first consumer. A parallel strategic observation surfaced during the brainstorm recorded 2026-04-20: Jared's local stack (3090, Ollama Cloud, Copilot CLI, Claude Code CLI subscriptions) is paid for but underexercised. No autonomous agent is currently running on the 3090. Every swarm-strategy conversation is therefore proceeding on theory rather than lived ground truth — a measurement gap, not just idle capacity.

Hermes on Ollama is notable because Ollama productized it as a first-class launcher target (`ollama launch hermes`). That's infra-layer signal that the self-improving-agent-with-memory pattern is being treated as a primitive, and Hermes ships with 70+ skills, cross-session memory, and automatic skill creation — the features the swarm-node end-state requires.

This probe exists to replace theory with experience. It is scoped to answer whether Hermes is the swarm-node primitive for this stack, with an opportunistic side-bank of OTEL-capture data if available.

## One-sentence invariant

By end of the probe week (or at the review-gate decision point), the probe produces a committed observations doc at `docs/observations/2026-04-??-hermes-probe.md` that answers four questions with evidence: (1) did the async-teammate pattern earn a habit, (2) did the local model carry the same workload the cloud model did, (3) are Hermes's memory + skill-gain properties observable, (4) does Hermes emit OTEL telemetry chitin could ingest.

## Scope

### In scope

- Install Hermes via `ollama launch hermes` on the local Linux workstation.
- Configure one messaging gateway (Telegram by default) as the async channel.
- Run Hermes on `qwen3.5:cloud` for days 1–3, swap to local `qwen3.6` on the 3090 for days 4–7.
- Use Hermes as an async teammate for real work that comes up during the week — no synthetic tasks.
- Day-1 OTEL-surface investigation, ≤30 min hard cap. If Hermes has an export config, enable it and capture one payload.
- Daily probe-log entries in a scratch file during the week.
- Observations doc written at the review gate (day 7) with verdict, habit log, checklist evidence, and findings.

### Out of scope

- Any chitin ingest, adapter, governance, or policy code for Hermes. The probe is observer-only.
- Structured SP-0-style semconv characterization of Hermes's OTEL output. The ≤30 min day-1 investigation is opportunistic, not a workstream commitment.
- Multi-Hermes setups or actual swarm topology. Single-node only.
- Concurrent operation of Hermes + openclaw + Claude Code through chitin (Lane-② cross-surface scenario). Separate brainstorm if the probe verdict triggers it.
- Any Readybench / bench-devs content. Chitin is OSS; the content-boundary rule applies.
- Designing the follow-up cycle in advance. The verdict triggers a follow-up brainstorm; this spec does not commit to its shape.

## Architecture

```
[User] ──messaging──▶ [Hermes gateway]
                            │
                            ▼
                [Hermes agent runtime]
                (memory + skills, 70+ built-in)
                            │
                            ▼
                    [Ollama provider]
                            │
                    ┌───────┴────────┐
                    ▼                ▼
           days 1–3: cloud    days 4–7: local
           qwen3.5:cloud      qwen3.6 on 3090

            [optional, day-1 only]
            OTEL export → local file
                    │
                    ▼
            docs/observations/
            2026-04-??-hermes-otel-capture.md
            (if Hermes supports OTEL export)
```

### Components

1. **Hermes install.** Via `ollama launch hermes`. Ollama's wizard handles Hermes install script + provider config + gateway wiring + messaging in a single command path.

2. **Model — cloud phase.** `qwen3.5:cloud` for days 1–3. Chosen because its stated profile ("reasoning, coding, agentic tool use with vision") matches async-teammate work, and keeping the same model family across the mid-week swap isolates the local-vs-cloud variable.

3. **Model — local phase.** `qwen3.6` on the 3090 for days 4–7, ~24 GB — fits the 3090's memory exactly. If it OOMs or is unusably slow, fall back to `gemma4` (~16 GB) and record the fall-back as a finding rather than ending the probe.

4. **Messaging gateway.** Telegram (bot created via @BotFather — lowest-friction setup). If Signal / Discord / etc. is preferred, swap the platform — the specific channel is not the variable being tested. One platform, one identity, one conversation thread across the full week.

5. **Chitin's role: observer only.** Chitin is not integrated during the probe. The only touch-point is the day-1 OTEL investigation: if Hermes exposes an OTEL export config, capture one payload to `docs/observations/` in the same shape as the openclaw observation doc (`libs/adapters/openclaw/README.md` as the template for what "captured and characterized" looks like). No `chitin ingest`, no policy, no event chain.

6. **Probe log.** A single scratch file `docs/observations/2026-04-??-hermes-probe-log.md` — short daily entries during the week. Rolls up into the final observations doc at exit.

## Operational plan

### Day 1 — setup + OTEL scan

- Run `ollama launch hermes`. Configure Ollama provider at `http://127.0.0.1:11434/v1`, set `qwen3.5:cloud` as primary.
- Wire Telegram gateway through Hermes's setup wizard (`hermes gateway setup` if the Ollama launcher doesn't prompt for it).
- Verify round-trip: send a message from Telegram, confirm Hermes replies coherently.
- **OTEL investigation — ≤30 min hard cap.** Check Hermes's install for OTEL surface: scan `hermes --help` and `hermes setup`, grep the install directory for `otel` / `opentelemetry` / `gen_ai`, inspect the config file for telemetry/export sections. Two outcomes:
  - Surface exists: enable it, point export at a local file, let it capture during day-1 usage, commit a payload to `docs/observations/2026-04-??-hermes-otel-capture.md` with a short characterization (attribute keys, span-types, semconv compliance check).
  - No surface: one-sentence finding in the probe log, move on.
- Seed the probe log with initial entry: install version, model in use, gateway platform, OTEL finding.

### Days 2–3 — cloud phase

- Use Hermes as async teammate for real work that comes up. No synthetic tasks — if nothing real comes up, that is itself data (see kill criterion 3).
- Brief daily probe-log entry: what was offloaded, what came back, habit-signal Y/N (did you message unprompted today).
- Observe for checklist items: multi-turn memory holding across separate sessions, any auto-created skill persisting beyond a single conversation.

### Day 4 — model swap

- Switch Hermes's primary model from `qwen3.5:cloud` to local `qwen3.6` via Hermes provider config. Keep the gateway, Telegram identity, conversation thread, and memory store intact — the swap isolates the model, not the state.
- Smoke-test: ask Hermes something it should remember from days 1–3. Confirm memory survived the swap.
- If `qwen3.6` OOMs or is unusably slow: fall back to `gemma4`, log the fall-back, continue.

### Days 5–7 — local phase

- Same usage pattern as days 2–3.
- Daily probe-log entry now includes an explicit "did local feel materially worse than cloud" comparison.

### Day 7 — review gate

Write the final observations doc at `docs/observations/2026-04-??-hermes-probe.md`. Decide one of three outcomes:

- **Extend** — promising but undecided; name the extension duration (max 1 week), the delta question the extension answers, explicit re-review at end of extension.
- **Close, verdict yes** — promote Hermes to durable stack item; trigger the follow-up brainstorm for "minimum chitin-governs-Hermes setup."
- **Close, verdict no** — document the reason, archive in strategic-roadmap memory, re-open the local-stack question as a separate thread.

### Habit-verdict mechanics

Each day, the probe-log entry ends with `habit: Y` or `habit: N` — did you message Hermes unprompted for real work that day. End-of-week count is the habit verdict:

- ≥3 Y days → habit forming → evidence supports "yes."
- ≤1 Y day → no habit → evidence supports "no."
- 2 Y days → ambiguous; checklist evidence breaks the tie, and if still ambiguous, default to "extend."

## Kill criteria & failure modes

### Hard kill — close probe early, document why

1. **Messaging gateway broken >24h and unfixable.** Without the async channel, the probe isn't testing what it claims to test. Close, note the friction as a finding.
2. **Cloud model can't complete *any* offered task usefully by end of day 2.** Not "some were rough" — none landed. That's an agent-scaffold-is-broken signal, not a swarm-primitive signal, and cloud shouldn't be the bottleneck.
3. **No real work to offload by end of day 2.** If you haven't messaged Hermes unprompted at all, the probe is answering a different question than it thought (do I have async work?) rather than the intended one (is Hermes the swarm primitive?). Close with that finding.

### Soft kill — note and continue

4. **Local model OOMs or unusably slow.** Fall back from `qwen3.6` to `gemma4`. If `gemma4` also fails, record "local stack can't carry this workload today" as a finding and finish the probe on cloud. The local-stack verdict IS the finding — don't abandon the probe over it.
5. **No OTEL surface in Hermes.** Expected outcome for some fraction of agent platforms. Log as finding, continue the probe.
6. **Auto-skill-creation did not fire during the week.** Note in the evidence checklist as "property not observed." Does not fail the probe — the habit verdict remains the primary signal.

### Inconclusive close — the only true probe failure

If the probe ends without a decided verdict (no / yes / explicit-extend), that is the probe's only real failure mode: wasted week, no durable output. The review-gate mechanics (§ "Day 7") are designed to prevent this. If extension is chosen and the second review is still undecided, the default is "no" for decisiveness — ambiguous probes corrode the probe discipline itself.

## Exit deliverables

Committed to the chitin repository at probe close:

- `docs/observations/2026-04-??-hermes-probe.md` — the observations doc. Structure:
  - **Verdict** — yes / no / extend, with one paragraph of reasoning.
  - **Habit log** — the 7 (or more, if extended) daily Y/N entries.
  - **Checklist evidence** — one row per swarm-node property, with evidence or "not observed":
    - Multi-turn memory holding across sessions.
    - Auto-created skill persisting beyond one session.
    - Local model carrying async-teammate workload.
    - OTEL surface observed.
  - **Findings** — what surprised you (positive or negative), what the next probe would want to answer.
- `docs/observations/2026-04-??-hermes-otel-capture.md` — only if Hermes has an OTEL surface. Same shape as the openclaw observation artifact.
- Strategic-roadmap memory update (`memory/project_strategic_roadmap.md`) reflecting the verdict and, if applicable, the next durable stack addition.

### What "success" means for the probe itself

The probe succeeds by producing the verdict + evidence, regardless of whether the verdict is yes or no. A well-documented "no" is a success outcome. The only failure is an inconclusive close.

## Verdict-triggered follow-ups (anticipated, not committed)

### If verdict = yes

- Add Hermes to strategic-roadmap memory as durable stack component.
- Trigger follow-up brainstorm: "minimum chitin-governs-Hermes setup." The observations doc's OTEL finding decides the adapter shape — OTEL file-intake if Hermes emits gen_ai-compliant spans, a Hermes-SP-0-style spike if partial, a bespoke wrap if nothing usable.
- Multi-Hermes / swarm-topology follow-up is explicitly NOT triggered by this probe. Single-node governance story ships first.

### If verdict = no

- Archive Hermes as "probed 2026-04/05, not adopted" in strategic-roadmap memory with the reason.
- The local-stack question re-opens — the 3090 is still idle. Trigger a separate brainstorm for "what else exercises the local model stack" (local coding assistant, bespoke async worker, different agent platform). The capability gap does not close just because Hermes didn't fit.
- OTEL capture (if obtained) stays banked as one more datapoint for the OTEL GenAI adoption landscape, independent of whether chitin ingests Hermes.

### If verdict = extend

- Named extension duration (1 week max).
- Named delta question — what will the extension answer that week 1 didn't.
- Explicit re-review at end of extension, with "no" as the default if still undecided.

## Self-review

### Placeholder scan

- `2026-04-??` date placeholders in `docs/observations/` filenames are intentional. The exact dates are set when the probe runs and when each deliverable lands — the probe is not yet scheduled.
- No `TBD` / `TODO` literals. Every deliberate unknown (specific real-work tasks during the week, exact OTEL surface characterization if present) is explicitly deferred to probe execution, not hidden behind a placeholder.

### Internal consistency

- The three-outcome exit (extend / close-yes / close-no) in § "Day 7" is the same set referenced in § "Verdict-triggered follow-ups" — no divergence.
- Kill criteria in § "Kill criteria" do not overlap or contradict the exit deliverables in § "Exit deliverables" — hard kills produce a shortened observations doc with the kill reason in place of the normal verdict; soft kills produce the full doc with the soft-kill event as a finding.
- OTEL-capture handling is consistently "opportunistic, ≤30 min cap, commit-if-present, log-and-move-on if absent" across §§ Architecture, Operational plan, Exit deliverables, and Verdict-triggered follow-ups.

### Scope check

- The probe is a single brainstorm → execution cycle. It does not bundle "probe Hermes" with "integrate Hermes" — integration is explicitly in § "Verdict-triggered follow-ups" as a separate future cycle contingent on verdict.
- No hidden scope: the in-scope list matches what the operational plan does day-by-day; the out-of-scope list matches the phrases that could have crept in during brainstorming but were explicitly rejected (chitin integration, multi-Hermes, SP-0-style characterization, Readybench content).

### Ambiguity check

- "Real work" (days 2–3, 5–7): deliberately user-defined. The probe cannot pre-specify what async work will come up during the week; that would be the synthetic-task failure mode. The only constraint is "you'd actually care about the result."
- "Usably slow" / "usably useful" (kill criteria, soft kill #4; hard kill #2): qualitative, user judgment. Probe is short enough and user-driven enough that a codified threshold would be fake precision.
- "Material difference" in the local-vs-cloud daily comparison: also qualitative. The point is the probe log captures the user's lived experience, not a latency benchmark.

## Execution handoff

Next action: write an execution plan via the `superpowers:writing-plans` skill. The plan will break the operational plan into ordered executable tasks, check-pointed by day and by decision gate.
