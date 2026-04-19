# Toolchain

## TypeScript — Vite+

Unified toolchain via the `vp` CLI (alpha, MIT). Pinned version: **v0.1.18**.
System install: `curl -fsSL https://vite.plus | bash`.

| Role | Tool | CLI |
|------|------|-----|
| Dev server | Vite 8 | `vp dev` |
| Test runner | Vitest 4.1 | `vp test` (or `pnpm exec vitest`) |
| Linter | Oxlint 1.60 | `pnpm exec oxlint .` |
| Formatter | Oxfmt | `vp check --fix` |
| Bundler | Rolldown | `vp build` (or via Vite) |
| Type checker | @typescript/native-preview (tsgo) | `pnpm exec tsgo` |

## Nx

Top-level orchestrator. Owns project graph, affected computation, and module-boundary enforcement (via `@nx/enforce-module-boundaries` ESLint rule). Each TS Nx target shells to the appropriate Vite+ subcommand.

## Module boundary enforcement

Two-pass lint:
- **Oxlint** (fast, primary) — style, correctness, suspicious patterns.
- **ESLint** (narrow, secondary) — `@nx/enforce-module-boundaries` only, gated by layer tags.

## Go

Real Nx project with `project.json` and named targets driven by `nx:run-commands`. Go 1.22+; no custom Nx executors in Phase 1.

## Python

Not used in Phase 1. Reserved as `python/analysis` via `nx:run-commands` for later.

## Fallback plan

If Vite+ alpha regresses, the bundled components are all MIT upstream and can be used independently: Vitest, ESLint + Oxlint, Prettier, Vite 7. Swap config, no architectural change.
