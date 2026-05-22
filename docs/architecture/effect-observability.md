# Effect Observability — the precondition behind Chitin's enforcement claim

Status: active architecture reference. Companion to
[`layer-contracts.md`](./layer-contracts.md) §1. Created 2026-05-22.

Chitin's product claim is *complete mediation*: the [`README.md`](../../README.md)
and [`architecture.md`](../architecture.md) say "every tool call ...
passes through one declarative policy," and `layer-contracts.md` §1
says "the kernel is the only enforcement point." Those statements are
true **for effects the kernel can observe**. This doc states that
precondition explicitly, per surface, so neither Chitin's own docs nor
downstream consumers overclaim what the chain proves.

The precondition has a name in the governance literature — the
*Effect Observability Assumption*: a runtime can only enforce, and can
only audit, the effects it mediates. This doc is the repo-grounded
version of that assumption. Every fact here is sourced from
[`governance-setup.md`](../governance-setup.md) ("Install paths by
driver"), [`driver-conformance.md`](../driver-conformance.md), and
`layer-contracts.md`; the research framing is background only.

## The invariant, and its precondition

`layer-contracts.md` §1 is **normative**: no driver, orchestrator, or
adapter is *permitted* to route around `gov.Gate.Evaluate`.

This doc is **descriptive**: it enumerates where an effect *can*
travel un-mediated anyway — by misconfiguration, by an ungoverned
surface, or by the structural limit of invocation-level gating — and
what Chitin records when that happens.

The honest one-sentence claim:

> Chitin gates and records every tool call that a wired, installed,
> verdict-honoring driver surface delivers to it. Effects outside that
> set are neither enforced nor recorded.

The overclaim to avoid:

> Chitin records everything the agent did.

It does not. It records everything the agent did **through a mediated
surface**.

## Two different coverage questions

These are routinely conflated. They are orthogonal.

| Question | Doc | Failure mode |
|---|---|---|
| **Action-vocabulary coverage** — a tool call reached the gate; does it normalize to the right canonical action? | `driver-conformance.md` | Falls to `ActUnknown` → **fail-closed deny** |
| **Effect-mediation coverage** — can a side effect reach the OS *without producing a gated tool call at all*? | this doc | **Silent** — no decision, no chain row |

A driver can have 100% action-vocabulary coverage and still have a
wide-open bypass surface. The fail-closed posture on unknown actions
(`governance-setup.md`, closed-enum section) protects the *first*
question. It does nothing for the *second*: an effect that never
becomes a tool call is never normalized, so it never gets the chance
to be `unknown`.

## Per-surface mediation map

Mediation mechanisms are from `governance-setup.md` §"Install paths by
driver." Behavior **on bypass** is identical for every hook surface:
**silent** — no `gov.Decision`, no row in `gov-decisions-*.jsonl`, no
chain event, no OTEL span. Chitin does not know the effect happened.

| Surface | Mediation mechanism | What is mediated | Structural bypass surface |
|---|---|---|---|
| Claude Code | `PreToolUse` hook (`~/.claude/settings.json`) | Each tool call Claude Code emits as a hooked tool | Hook not installed; tool not matched by the hook matcher; **effects transitive to a gated `Bash` call** |
| Codex CLI | `PreToolUse` hook (`~/.codex/config.toml`, codex ≥ 0.128.0) | `Bash`, `apply_patch`, `read_file`, MCP tool calls | Codex < 0.128.0 (no hook support); `codex_hooks` disabled; transitive `Bash` effects |
| Gemini CLI | `BeforeTool` hook (`~/.gemini/settings.json`) | shell, read/list/search, edit/replace/write, web tool calls | Hook not installed; transitive shell effects |
| Hermes | `pre_tool_call` shell hook (`~/.hermes/config.yaml`) | terminal/file/patch/web/delegation/kanban tool calls | Hook not installed; Hermes-internal runtime plumbing that does not surface as a `pre_tool_call` |
| Copilot CLI | In-kernel SDK driver — **only via `chitin-kernel drive copilot`** | SDK permission kinds (shell, write, read, MCP, URL, memory, custom tool, hook) | Running `copilot` directly, not through `drive`; VS Code Copilot agent-mode execution |
| OpenClaw | `before_tool_call` plugin (`apps/openclaw-plugin-governance`) | Tool calls dispatched by OpenClaw's pi-runtime | Plugin absent from the openclaw allowlist; standalone Claude/Codex/Gemini/Copilot processes (govern those through their own driver integration) |
| VS Code Copilot | **None.** Repo instructions only | Nothing — `AGENTS.md` / `.github/instructions/*` steer behavior, not enforce it | The entire surface. This is **not a security boundary** (`driver-conformance.md`). Route terminal-side execution through a chitin-aware CLI where enforcement is required |

The most important row is not a misconfiguration. **Transitive `Bash`
effects** are a *structural* property of invocation-level gating: the
gate evaluates the command string presented in the tool call; whatever
that command then does — fork a daemon, write files, open a socket —
is not itself a separate gated tool call. This is inherent to the
PreToolUse pattern, not a defect. It bounds what "mediated" means, and
must be stated rather than papered over.

## Enforcement vs. observation: the `mode` axis

Even on a fully mediated surface, *enforcement* and *observability*
are distinct (`governance-setup.md` §"The three modes"):

- **`monitor`** — the decision is recorded; the effect is **allowed**.
  Observed, not enforced.
- **`enforce` / `guide`** — the effect is blocked. Enforcement still
  depends on the driver honoring gate exit code `1` (`deny`). A driver
  that ignores a non-zero hook exit is a mediated-but-not-enforced
  surface.

So a chain row proves *observation*. It proves *enforcement* only when
the surface was in `enforce`/`guide` **and** the driver honored the
verdict.

## What this means for the chain and for consumers

The chain is tamper-evident and replayable — but completeness is
scoped to mediated effects. Therefore:

- **Absence is not proof.** A consumer (analytics lib, audit reader,
  policy backtester) must not read "no chain row" as "no effect." It
  means "no *mediated* effect."
- **Post-incident reconstruction of a bypassed effect** depends on
  OS-level evidence (filesystem timestamps, shell history), not the
  chain. The chain reconstructs mediated decisions only.
- **OTEL spans inherit the same scope.** OTEL is a one-way projection
  of the chain (`layer-contracts.md` §4); it cannot show what the
  chain never recorded.

## Rules for contributors

1. **State the observability assumption.** Any new governance feature's
   docs must say what effects it mediates, what it cannot see, and what
   happens on bypass. Adopt this as a repo convention, not as silent
   practice.
2. **Scope enforcement claims.** Do not write "all" / "every" tool
   calls without the surface qualifier. The "every tool call" phrasing
   in `README.md` and `architecture.md` is shorthand for "every tool
   call on a wired, installed, verdict-honoring surface." Read it that
   way; do not strengthen it.
3. **A new driver documents two things, not one.** Its normalizer
   (→ `driver-conformance.md`) *and* its mediation mechanism + bypass
   surface (→ the table above). A driver with a normalizer but an
   undocumented bypass surface is not done.
4. **Do not "fix" a bypass surface by widening Chitin's scope.**
   Transitive `Bash` effects are not fixed by Chitin spawning a
   sandbox, an in-gate LLM monitor, or a process supervisor — those
   are substrate/OS concerns and out of bucket
   ([`AGENTS.md`](../../AGENTS.md)). The in-scope response is to
   *document* the surface honestly and let the operator choose
   containment (worktrees, OS sandboxing) at the substrate layer.

## Open questions (carried, not committed)

Candidates only — not accepted work without a spec/ticket.

- Should the kernel expose a hook-health check ("is my surface
  actually installed and wired?") so an operator can detect an
  un-mediated driver before it matters? This stays *observation of
  Chitin's own install state* — in-bucket if pursued — but is not
  scheduled here.
- Should a structured "mediation scope" field travel on the chain or
  in schemas, so consumers get the observability scope as data rather
  than as prose? Open; needs a `libs/contracts` schema decision.
- Should replay/trace tests become a required acceptance criterion for
  normalizer and policy changes? Raised by current agent-governance
  research; defer to a decision doc.
