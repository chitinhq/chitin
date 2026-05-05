# Tier × driver × model matrix

Status: draft, operator-marked-up. Replaces the implicit
`TIER_DRIVER_DEFAULTS` / `TIER_MODEL_DEFAULTS` in
`apps/runner/src/dispatcher.ts` (the always-T0 + escalation-loop
architecture made the old single-driver-per-tier mapping too rigid).

Date: 2026-05-05

## Why this exists

Three problems forced this doc:

1. The matrix kept getting re-derived from scratch every session,
   losing detail each time.
2. The previous mapping was one driver per tier — but most operators
   have multiple subscriptions (Anthropic Max + Copilot Pro + Codex +
   Google AI Pro + Ollama Cloud) and want the runner to pick across
   all of them.
3. Model capabilities don't cleanly map to one tier. Haiku-4-5 is a
   strong T1 *and* a serviceable T2; Sonnet-4-6 covers T2 *and* T3
   depending on task class. The matrix has overlap by design.

## Architectural anchors (locked)

- **Hermes is the entry point.** The kanban dispatcher spawns the
  `chitin-runner` profile per claimed card; the profile invokes
  `chitin-execute-request --from-kanban-card $HERMES_KANBAN_TASK`.
- **T0 is always Hermes + glm-4.7-flash on the local 3090.** Free.
  Every entry starts here regardless of the entry's groomed `tier:`
  hint.
- **Escalation is mid-flight, kernel-driven.** The router/advisor
  pipeline (heuristic flags → advisor LLM → continue+nudge or
  takeover+escalate) is the engagement mechanism, not the
  dispatcher's tier ladder.
- **Budget is the second axis.** "Pick lowest tier that can
  accomplish the work" — telemetry tells us what each tier actually
  succeeds at; routing learns from that over time.
- **Matrix is operator-owned config.** Chitin ships a sane default;
  operators with different subs override per-tier.

## Default matrix (operator can override)

Every (driver, model) is verified to be free under the standard
operator subs. Cost notes call out the per-call pricing relative to
each provider's "1.0×" baseline.

### T0 — local, free, fastest

| driver | model | provider | notes |
|---|---|---|---|
| **hermes** | glm-4.7-flash | ollama-launch | local 3090; ~30B; primary T0 |
| hermes | qwen3-coder:30b | ollama-launch | local 3090; coding-specialized; cover when glm whiffs |
| openclaw | glm-flash-agent | (n/a) | same model, openclaw runtime — preserves the openclaw surface |
| openclaw | deepseek-coder | (n/a) | local 3090; coding-specialized; cover |
| gemini | gemma-2 | google AI Pro | local-on-cloud-CLI; gemini CLI auto-routes |

T0 is the always-start tier. Failures here trigger advisor → either
nudge (continue same tier) or escalate (bump to T1).

### T1 — first real cloud bump (NEVER local)

The "T1 is not local" rule: if T0 (local) failed, another local model
is unlikely to succeed. T1 is the first meaningful capability bump
and lives entirely in cloud APIs.

| driver | model | provider | notes |
|---|---|---|---|
| **copilot** | gpt-4.1 | Copilot Pro | cheap+fast cloud; primary T1 |
| copilot | gpt-5.0-mini | Copilot Pro | similar tier |
| copilot | claude-haiku-4-5 | Copilot Pro | Anthropic via Copilot, 0.33× rate |
| codex | gpt-5.0-mini | OpenAI sub | cross-provider alt |
| gemini | gemini-2.0-flash | Google AI Pro | cross-provider alt |
| claude-code-headless | claude-haiku-4-5 | Anthropic Max | direct Anthropic alt |

### T2 — cloud, mid-tier reasoning

| driver | model | provider | notes |
|---|---|---|---|
| **copilot** | gpt-5.0 | Copilot Pro | mid-tier reasoning; primary T2 |
| copilot | claude-haiku-4-5 | Copilot Pro | overlap from T1 — haiku stretches into T2 |
| codex | gpt-5.0 | OpenAI sub | direct OpenAI alt |
| claude-code-headless | claude-haiku-4-5 | Anthropic Max | direct Anthropic alt |

### T3 — cloud, heavier reasoning

| driver | model | provider | notes |
|---|---|---|---|
| **copilot** | claude-sonnet-4-6 | Copilot Pro | judgment-heavy primary T3 |
| copilot | gpt-5.4 | Copilot Pro | OpenAI heavy; 1× rate on Copilot |
| codex | gpt-5.4 | OpenAI sub | direct |
| gemini | gemini-2.5-pro | Google AI Pro | google heavy |
| **hermes** | glm-5.1:cloud | ollama-cloud | "opus-light" via Ollama Cloud sub |
| openclaw | glm-cloud-agent | (n/a) | same model, openclaw runtime |
| claude-code-headless | claude-sonnet-4-6 | Anthropic Max | direct Anthropic |

### T4 — frontier, escalation-only

| driver | model | provider | notes |
|---|---|---|---|
| **claude-code-headless** | claude-opus-4-7 | Anthropic Max | frontier; primary T4 |
| codex | gpt-5.5 | OpenAI sub | per operator: "really good, tier 4"; expensive even at 1× |
| gemini | gemini-3 | Google AI Pro | when GA; heavy frontier |
| copilot | claude-sonnet-4-6 | Copilot Pro | capability fallback if Anthropic down |

### Advisor (the router/advisor LLM at the kernel layer)

The advisor is the LLM the kernel spawns when a heuristic fires
(floundering, blast-radius, etc.). It returns
`{nudge, verdict: continue|takeover, escalate: bool}`. Operator
picks the model based on cost vs diagnosis quality:

| profile | model | provider | rationale |
|---|---|---|---|
| **default** | claude-haiku-4-5 | Anthropic Max | cheap; many advisor calls per dispatch are fine |
| heavy | claude-sonnet-4-6 | Anthropic Max | better diagnosis when haiku's nudges aren't landing |
| budget-conscious | gpt-4.1 | Copilot Pro | $0 marginal cost on Copilot Pro |

## Schema (chitin.yaml addition)

```yaml
tiers:
  T0:
    description: "local 3090, free, fastest"
    primary: { driver: hermes, model: glm-4.7-flash, provider: ollama-launch }
    alternates:
      - { driver: hermes, model: qwen3-coder:30b, provider: ollama-launch }
      - { driver: openclaw, model: glm-flash-agent }
      - { driver: openclaw, model: deepseek-coder }
      - { driver: gemini, model: gemma-2 }
  T1:
    description: "first cloud bump (never local)"
    primary: { driver: copilot, model: gpt-4.1 }
    alternates:
      - { driver: copilot, model: gpt-5.0-mini }
      - { driver: copilot, model: claude-haiku-4-5 }
      - { driver: codex, model: gpt-5.0-mini }
      - { driver: gemini, model: gemini-2.0-flash }
      - { driver: claude-code-headless, model: claude-haiku-4-5 }
  T2:
    description: "cloud, mid-tier reasoning"
    primary: { driver: copilot, model: gpt-5.0 }
    alternates:
      - { driver: copilot, model: claude-haiku-4-5 }
      - { driver: codex, model: gpt-5.0 }
      - { driver: claude-code-headless, model: claude-haiku-4-5 }
  T3:
    description: "cloud, heavier reasoning"
    primary: { driver: copilot, model: claude-sonnet-4-6 }
    alternates:
      - { driver: copilot, model: gpt-5.4 }
      - { driver: codex, model: gpt-5.4 }
      - { driver: gemini, model: gemini-2.5-pro }
      - { driver: hermes, model: "glm-5.1:cloud", provider: ollama-cloud }
      - { driver: openclaw, model: glm-cloud-agent }
      - { driver: claude-code-headless, model: claude-sonnet-4-6 }
  T4:
    description: "frontier, escalation-only"
    primary: { driver: claude-code-headless, model: claude-opus-4-7 }
    alternates:
      - { driver: codex, model: gpt-5.5 }
      - { driver: gemini, model: gemini-3 }
      - { driver: copilot, model: claude-sonnet-4-6 }

advisor:
  driver: claude-code-headless
  model: claude-haiku-4-5
  # When the kernel's router/advisor pipeline fires, this is the
  # LLM that returns {nudge, verdict, escalate}. Cheap by default;
  # operator can flip to sonnet for higher-quality diagnosis.
```

## Routing strategy

The matrix above is the SUPERSET of what's possible. The effective
matrix for any given operator is:

```
operator_matrix = default_matrix
                  ∩ (CLIs the operator has installed + authed)
                  ∩ (models each CLI actually offers them today)
                  ∩ (cost ≤ operator's per-tier budget cap)
                  ⊕ operator_overrides_in_chitin.yaml
```

Two levels:

### Level 1 — operator-declared (chitin.yaml)

The schema above. Operator writes the matrix they want. Chitin
trusts it. Fast to set up; no auto-discovery.

### Level 2 — auto-detected (`chitin doctor matrix`)

A new command that PROBES each CLI on the operator's machine and
emits the operator's effective matrix:

```
chitin doctor matrix --emit chitin.yaml.suggested
```

Probes:

| driver | how to check available | how to check cost |
|---|---|---|
| copilot | `copilot model list` | static map: gpt-4.1 = 0.33×, gpt-5.4 = 1×, claude-* via 0.33× rate |
| codex | `codex model list` | static map per OpenAI sub plan |
| gemini | `gemini --list-models` | free under Google AI Pro (Gemma + 2.0-flash); 2.5-pro within sub limits |
| claude-code | `claude --models` | static: haiku/sonnet/opus rates published |
| hermes | `hermes model list` + `ollama list` | local models = free; ollama-cloud = sub |
| openclaw | `openclaw agent --list-agents` | local + cloud — same as hermes |

Output: a `chitin.yaml.suggested` the operator can diff against
their current and pick. Auto-detection is intentionally
SUGGESTION-ONLY — operators may want to force-disable a combo even
when it's available (e.g., gemini-3 might be available but the
operator doesn't trust it for prod yet).

### Cost-aware picking within a tier

When multiple primary candidates fit a tier (e.g., T1 has gpt-4.1,
gpt-5.0-mini, claude-haiku-4-5 all available), runner prefers the
cheapest available combo:

1. `cost_per_call = cost_per_token × estimated_tokens` (or a
   simpler "free under sub: 0; paid: $X" classifier)
2. Tie-break by recent success rate from
   `routing-elo` analysis when available

This implements "lowest tier that can accomplish the work, by
budget" — within a tier, also pick the cheapest available combo
that's been working.

### Three picking strategies for which alternate to use within a tier

1. **Operator-preferred** (now): use `primary`. Set
   `CHITIN_T<N>_DRIVER=copilot:gpt-5.0` env var to override.
2. **Cost-aware** (next): pick cheapest available combo per the
   detection above; primary becomes the highest-cost-acceptable
   ceiling.
3. **Best-historical** (long-term): consult `routing_elo.py`'s ELO
   table once ≥30 dispatches per (tier, combo, task_class) bucket
   exist. Then we get "lowest tier that can accomplish the work,
   based on observed capability + cost" — the user's stated goal.

Start at (1). Add (2) when `chitin doctor matrix` ships. Move to
(3) when ELO is statistically meaningful per bucket.

## OpenClaw — proposed role

Per the operator's call: openclaw is NOT in the kanban-dispatch
ladder (Hermes is the entry point). It stays in the codebase for one
of:

- **(A) Code-review specialist** — peer-reviewer / reviewer roles
  dispatch through openclaw, giving the review-graph a non-Hermes
  lens. Same shape as codex's existing role in `REVIEW_TIER_DRIVER`.
- **(B) Sandboxed-execution surface** — openclaw has first-class
  container sandboxing (verified via the agent research). Use when
  an entry calls for risky execution that should be isolated.
- **(C) Operator-CLI** — kept for manual operator-driven runs;
  not in the swarm dispatch path.

**Default lean: (A).** Fits the existing review-graph's "alternative
provider lens" pattern. Cleanest deprecation story for
`apps/openclaw-plugin-governance/` (still needed for the openclaw
runtime; the hermes plugin handles non-openclaw paths).

## TODO — benchmark cross-references

The capability claims above are operator-asserted; pin them to data:

- [ ] Run a representative chitin task across all T0 combos
      (glm-flash, qwen-coder, deepseek-coder, gemma-2) and record
      success rate / time-to-first-commit. Likely fits the existing
      `clawta/bench` harness.
- [ ] Same across T1 combos to confirm gpt-4.1 is the right primary
      (vs codex/gpt-5.0-mini or claude-haiku via copilot).
- [ ] T3 combos to validate sonnet-via-copilot vs glm-5.1:cloud vs
      gemini-2.5-pro on heavy-reasoning entries. This is where
      provider-redundancy matters most.
- [ ] Codex pricing: confirm `gpt-5.4 = 1× rate` and `gpt-5.5 = 5×+
      rate` (or whatever the actual ratio is) per the operator's
      "much higher expense" note. The matrix should call this out
      explicitly so the runner doesn't escalate to gpt-5.5 unless
      cost-cap allows.

## Open questions (operator decides)

1. **gemini-3 availability.** Is it on the operator's Google AI Pro
   today, or future GA? If future, drop from the matrix until it
   ships.
2. **Per-task-class routing.** Should `task_class: refactor` prefer
   coding-specialized models (qwen-coder, deepseek-coder) over
   general-purpose ones at T0/T1? Routing-as-learning would discover
   this; explicit per-task-class config is the simpler short-term.
3. **Advisor cost cap.** Each advisor call is one LLM round-trip. At
   floundering threshold = 90s, a stuck T0 dispatch could fire the
   advisor every 90s for the duration of the run. Need a per-run
   cap (e.g., max 3 advisor calls per attempt).
