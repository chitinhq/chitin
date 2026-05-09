---
date: 2026-04-20
type: implementation-notes
scope: Phase A restart on main (dogfood-debt-ledger plan)
active_soul: Knuth
related:
  - docs/superpowers/plans/2026-04-19-dogfood-debt-ledger.md
  - docs/observations/quorums/2026-04-19-phase-b-finish.md
  - docs/observations/2026-04-19-hook-payload-capture.md
  - souls/strikes/davinci.md
---

# Phase A restart — validate-and-improve notes

## Why we're re-implementing instead of cherry-picking

The `dogfood-debt-ledger` branch contains a working Phase A (commits
`685a7aa`, `284af1b`, `0d2c01c`, `8ba6715`, `3061af8`). Main does not.

That branch was also the origin of PR #19, which was closed without merge
after the schema-assumption strike on da Vinci
(`souls/strikes/davinci.md`). B1–B3 were re-implemented on main under
Knuth with the empirically-grounded wire and measurable improvements
(atomic writes, `_tag`-based identity, symmetric-idempotency invariant,
11 tests). Phase A is simpler than Phase B and was not implicated in the
strike — but choosing re-implementation over cherry-pick gives us the
same validation pass we gave B1–B3, and keeps main free of any
branch-era assumptions we might have missed.

## Branch refinements we will incorporate

Two post-plan refinements on the branch are validated improvements over
the plan text. Both will be carried forward in the re-implementation:

1. **Test colocation in `libs/contracts/tests/`** (branch `3061af8`).
   The plan writes `libs/contracts/src/chitindir-resolve.test.ts`.
   The package's existing test layout is `tests/` (verified: 4 other
   contracts tests live there). The plan's path is wrong for this
   package. Test goes in `libs/contracts/tests/chitindir-resolve.test.ts`.

2. **`/tmp/.chitin` sandbox hardening** (branch `8ba6715` TS, `ae51333`
   Go). The plan's orphan-fallback tests set `HOME` to a fake tmp dir
   but leave cwd at an inherited path that may itself have a `.chitin/`
   ancestor on the running machine (this box literally has `/tmp/` as a
   walk-up target). The walk-up would find that instead of falling
   back to the fake home. Fix: set cwd to a known-clean `t.TempDir()`
   subtree *and* ensure no `.chitin/` exists between cwd and filesystem
   root. Knuth #4: "the boundary is where the bugs live" — this is a
   test boundary that was under-specified in the plan.

## Branch refinements we will NOT carry forward without re-deriving

- **TS resolver details** (branch `0d2c01c`). The branch's resolver
  shape may or may not match the index-export convention on current
  main. Verify `libs/contracts/src/index.ts` uses extensionless imports
  (confirmed today: no `.js` suffix) and match that style rather than
  copying the branch's export statement blindly.

## Scope hand-off note

- **Active soul for Phase A + B4/B5/B6:** Knuth (per quorum
  `docs/observations/quorums/2026-04-19-phase-b-finish.md`, 8/8).
  Quorum explicitly scoped Knuth to "plan §428–610" (B1 + B2). B3 was
  already implemented by Knuth with a documented plan deviation; A1–A3
  and B4–B6 are carried under the same lens without a new quorum, per
  the "keep practices, drop ceremony" feedback.
- **Curie's Phase B restart investigation is complete**
  (`souls/canonical/curie.md` scope note updated).
- **Default soul unchanged:** da Vinci remains sticky default for
  Phases D/E/F (cross-surface architecture) once Phase B PR ships.

## Invariants to state before each Phase A task

Knuth discipline — write down before code:

- **A1 (symlink script):** `install-kernel` produces an executable
  symlink at `$BIN_DST` iff `$BIN_SRC` exists and is executable; never
  overwrites a regular file at `$BIN_DST`.
- **A2 (Go resolver):** for all `(cwd, boundary, filesystem)` with no
  `.chitin/` between cwd and boundary-or-root, `Resolve` returns
  `$HOME/.chitin` and that directory exists on return. For `(cwd,
  boundary)` with an existing `.chitin/` on the walk-up path,
  `Resolve` returns the closest such directory and does not create the
  orphan.
- **A3 (TS resolver):** same invariant as A2; outputs must match the Go
  resolver byte-for-byte on the same filesystem state (parity is the
  point of mirroring).

## Exit condition

Phase A ships when `pnpm install-kernel` works end-to-end on this box,
both resolvers pass their tests, and a smoke run of B4 (after it
lands) successfully emits an event via `resolveChitinDir` +
`runHook`.
