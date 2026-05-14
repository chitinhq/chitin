# Cull `libs/governance/` — TS classify+decide substrate

**Date:** 2026-05-08
**Status:** Accepted
**Audit:** Sun Tzu lens, 2026-05-08 internal-duplication audit (Tier 7b)

## Context

`libs/governance/` was introduced as **Slice 1** of the
predictive-execution policy design
(`docs/superpowers/specs/2026-05-03-predictive-execution-policy-design.md`):
a TypeScript SDK exporting `classify()` + `decide()` plus four Zod
schemas (`ToolCallRequest`, `SemanticEnvelope`, `BlastVector`,
`Decision`). The package's own README states:

> **Status:** Slice 1 — substrate only. No ingress wiring yet (no Claude
> Code hook, no openclaw plugin extension). Those land in Slice 1.5.

Slice 1.5 never landed. The substrate has been a parallel-dimension
adjudicator since 2026-05-03: ~1284 LOC (8 source files, 3 test files,
README, package.json, two tsconfigs) sitting next to a fully shipped Go
adjudicator that is the actual ingress for every gated tool call.

## The duplication

The Go kernel `go/execution-kernel/internal/gov/{action,policy,gate,bounds,escalation}.go`
is the canonical adjudicator. It is fired from:

- Claude Code `PreToolUse` hook (`scripts/install-claude-code-hook.sh` →
  `bin/chitin-router-hook` → `cmd/chitin-kernel/gate_hook.go`)
- Codex CLI hook (`scripts/install-codex-hook.sh`)
- Gemini CLI hook (`scripts/install-gemini-hook.sh`)
- Hermes plugin (`docs/governance-setup-extras/`)
- openclaw plugin (`apps/openclaw-plugin-governance/`)

Every real ingress path calls the Go adjudicator. The TS `decide()`
function had **zero callers** outside its own test files —
`grep -rn '@chitin/governance' apps/ libs/ tools/` returned only the
package's own `index.ts`, `package.json`, and README.

The 2026-05-03 MCP-gate-coverage audit
(`docs/archive/observations/2026-05-03-mcp-gate-coverage-audit.md` §7)
already noted this:

> 7. **TS governance libs** (`libs/governance/src/classifier.ts`)
>    — In-process classifier; lines 36-49 cover [...]
>    — This is the advisory path — not security-critical because the
>    kernel gate is the authoritative line

Even on its own framing, the TS substrate was the *advisory* path; the
Go gate was *authoritative*. Slice 1.5 (the wiring that would have
made the TS path load-bearing) was never written.

## Audit verdict (Sun Tzu, 2026-05-08, Tier 7b)

The Sun Tzu lens flagged this as **internal duplication** with the
following framing:

> Two ingress paths sharing one adjudicator was the load-bearing claim
> of Slice 1. That claim never landed. Today the kernel runs a
> mature, hook-fired Go adjudicator with bounds, escalation, severity
> ladder, and chain audit. The TS `decide()` is none of those — it is
> a 132-line synchronous switch with no state, no audit, and no caller.
> Cull or absorb. Either delete (Go kernel is the truth) or commit to
> TS-as-canonical and delete the Go duplication — but operating both
> is the worst of both worlds.

Operator decision: **cull**. The Go kernel is the truth.

## What's gone

- `libs/governance/` entire directory (~1284 LOC)
  - 7 `src/*.ts` files: `classifier.ts`, `decide.ts`, `index.ts`, and
    four Zod schemas (`tool-call-request`, `semantic-envelope`,
    `blast-vector`, `decision`)
  - 3 `tests/*.test.ts` files (`classifier`, `decide`, `integration`)
  - `package.json`, `README.md`, `tsconfig.json`, `tsconfig.lib.json`
- Root `tsconfig.json` project reference to `./libs/governance`
- `eslint.config.mjs` `layer:governance` `depConstraints` entry (Nx
  enforce-module-boundaries rule for a tag no package uses anymore)
- `pnpm-lock.yaml` `libs/governance` workspace entry (settled by
  `pnpm install`)

`pnpm-workspace.yaml` did not need a change — it globs `libs/*` and
the directory simply no longer matches.

## Consumer touch points

Zero external consumers. The grep
`@chitin/governance|libs/governance` over all `*.ts`, `*.tsx`,
`*.json`, `*.yaml`, `*.yml` returned only:

- The package's own `src/index.ts`, `package.json`, `README.md`
- Internal cross-references in `tests/` (deleted with the package)
- The root `tsconfig.json` project ref (removed)
- `pnpm-lock.yaml` workspace entry (regenerated)
- Two historical doc references (left in place, factual): the
  2026-05-03 MCP-coverage audit and the predictive-execution spec —
  both correctly describe the substrate as it was when they were
  written

## Invariant restored

> One canonical tool-call adjudicator: the Go kernel
> (`go/execution-kernel/internal/gov/*`), fired from every ingress
> hook (Claude Code, Codex, Gemini, Hermes, openclaw).

There is now one decision substrate, one audit chain, one severity
ladder, one bounds enforcer. No parallel TS `decide()` to drift away
from it.

## Why pure subtraction (no replacement scaffold)

The Go kernel already has everything `libs/governance` was supposed
to grow into:

| What `libs/governance` shipped | Where the Go kernel does it |
|---|---|
| `ToolCallRequest` schema | `internal/gov/action.go` (`Action`, `Spec`) |
| `SemanticEnvelope` | `internal/normalize/normalize.go` |
| `BlastVector` (4-axis) | `internal/router/blast_radius.go` |
| `Decision` (7-decision space) | `internal/gov/policy.go` `Effect` enum |
| `classify()` | `internal/canon/detectors.go` + driver `normalize.go` |
| `decide()` | `internal/gov/gate.go` `Gate.Evaluate` |

There is nothing to port. The Go path has been the authoritative
implementation since the substrate was added; this PR simply removes
the parallel that was never wired.

## Verification

- `pnpm install` settles cleanly, lockfile drops the
  `libs/governance` workspace entry
- `pnpm exec tsc -b` passes (no broken project refs)
- `pnpm exec vitest run` — 302 pass / 31 files (two preexisting EPIPE
  warnings in `libs/router-plugin-api/typescript/index.test.ts`,
  unchanged from `origin/main`, unrelated to this cull)

## Followups

- The 2026-05-03 predictive-execution spec is now historical — its
  Slice 1 is reverted, Slices 1.5–8 will not happen as written. A
  future spec, if any, should start from the Go kernel as the
  substrate, not propose a parallel TS one.
- The 2026-05-03 MCP-coverage audit's §7 ("TS governance libs") and
  Gap B ("TS classifier MCP ingress rule") are obsoleted by this
  deletion; can be dropped from the backlog.

## Counterfactual

If Slice 1.5 had landed inside a week of Slice 1, the substrate would
have had a real consumer and the cull would have been an absorb
(Go ingress hooks calling into a TS-evaluated policy via subprocess,
or the inverse). Five days passed; the Go kernel kept growing as the
authoritative path; the TS substrate stayed at zero callers. By the
time this audit ran, "cull" was strictly cheaper than "absorb" — the
Go kernel had already shipped the features the TS substrate was
sketching.

Lesson: a "Slice 1 substrate without Slice 1.5 ingress" has a
two-week half-life. After that, either ingress lands or substrate
gets deleted. Operating both indefinitely is the worst of both
worlds, exactly as the audit named it.
