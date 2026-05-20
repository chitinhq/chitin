# Spec 052: Agent worktree-discipline + mention-ownership guardrails

**Status**: DRAFT 2026-05-19 — awaiting red sign-off (constitution §1
pair-write rule). Slot 052 free.

**Origin**: agent-bus thread #15 ("Worktree-discipline + @mini-shadowing
— Ares/Clawta alignment", 2026-05-19). Clawta acked both invariants
with a concrete fix plan; Ares did not respond. This spec promotes that
thread into a ratifiable artifact.

**Author lens (Sun Tzu)**: this is a positioning problem, not an
algorithm. Two agents stepped on ground that wasn't theirs — one wrote
into the operator's main checkout, both answered a mention addressed to
a third agent. The fix is to draw the boundaries so the wrong move is
structurally unavailable, not merely discouraged.

## Summary

Two governance failures observed live on 2026-05-19:

1. **Worktree-discipline breach.** Ares investigated Mini in response
   to an operator `@mini` Discord post and wrote five mid-edit files
   directly into `~/workspace/chitin` on `main` — including a
   `swarm/bin/mini` left with an `IndentationError` that broke the CLI.
   Constitution §2 already forbids this ("Do not edit the primary
   checkout directly — always use a worktree"), but nothing *enforces*
   it. The rule is honored only by agents that remember it.

2. **Mention-shadowing.** The operator typed `@mini` in Discord; Ares
   answered it. Later Clawta answered an `@mini` in another channel.
   `@mini` belongs to Mini's listener; `@clawta` to Clawta's;
   `@hermes` to Hermes'. An agent answering a mention addressed to a
   peer defeats the dedicated-listener routing and confuses the
   operator about who is responding.

This spec adds enforceable guardrails for both. Constitution §2 gets
teeth; mention ownership becomes a checked invariant.

## Motivation

- **The breach was silent and destructive.** Ares' edits were never
  committed, never branched — just left in the working tree. The
  operator discovered them only when the Mini CLI failed to run. WIP
  in the main checkout is unattributable, un-reviewable, and one
  `git checkout` away from being lost.
- **Shadowing erodes trust in routing.** Spec 039 built a dedicated
  `@mini` listener precisely so Mini owns its mentions. If any agent
  may answer any mention, the listener is decorative.
- **Both are recurring-class failures.** Without enforcement they will
  happen again on the next agent that wasn't briefed.

## Non-goals

- No change to how worktrees are *created* (that is constitution §2 +
  existing `git worktree` flows).
- No new Discord inbound path. Mention ownership is about which agent
  *responds*, not new routing.
- This spec does not re-litigate spec 039's listener design.

## Requirements

### R1 — pre-write guard against the primary checkout on main

A shared guard in the agent tool-policy layer MUST refuse a file-
mutation tool call when BOTH:

- the resolved target path is inside the primary checkout
  (`~/workspace/chitin`, i.e. the tracked repo, not a sibling
  `~/workspace/chitin-*` worktree or `~/.cache/chitin/swarm-worktrees/*`),
  AND
- that checkout's current branch is `main`.

The error MUST name the offending path and tell the agent to create or
use a worktree (`~/workspace/chitin-<slug>/` or
`~/.cache/chitin/swarm-worktrees/<agent>-<task_id>/`). Pseudocode:

```
def guard_write(path: Path) -> None:
    primary = Path.home() / "workspace" / "chitin"
    rp = path.resolve()
    if not rp.is_relative_to(primary):
        return                       # worktree / elsewhere — fine
    if _git_branch(primary) == "main":
        raise PermissionError(
            f"refuse to write {rp}: primary checkout is on main. "
            f"Create or cd to a worktree first."
        )
```

The guard runs **before** the mutation, on the tool-policy boundary —
not inside individual scripts.

### R2 — startup read-only flag for a mis-placed session

When an agent session starts with cwd inside the primary checkout on
`main`, the session MUST surface a warning before any planning that
implies edits, and SHOULD mark repo files read-only for that session.
This catches the failure before the first write attempt, not at it
(Clawta's slice-2 suggestion in thread #15).

### R3 — mention ownership

Each agent with a dedicated listener owns its `@handle`:

| Handle    | Owner / listener                          |
|-----------|-------------------------------------------|
| `@mini`   | `mini-mention-listener` (spec 039)        |
| `@clawta` | `clawta-mention-listener`                 |
| `@hermes` | Hermes gateway                            |

An agent's inbound/mention filter MUST NOT match a handle owned by a
peer **unless that agent's own handle is also addressed in the same
message**. Concretely: Ares' filter ignores a bare `@mini`; it engages
only if the message also says `@ares`. Clawta's filter applies the
same rule symmetrically.

The owned-handle set is a single shared list so a new agent with a
listener is added in one place.

### R4 — guardrails are tested, not just documented

R1 and R3 ship with tests (constitution — invariants are proven, not
asserted). R1: a write to `~/workspace/chitin/<file>` on main raises;
the same write in a worktree passes; a write on a non-main branch in
the primary checkout passes. R3: fixture messages `@mini ping`,
`@clawta you there`, `@ares and @mini both`, narrative "Mini is
installed" — each asserts the correct set of responders.

## Boundary cases

1. **Primary checkout transiently on a feature branch** → R1 allows
   the write (branch != main). Intended: the breach is specifically
   "main in the primary checkout".
2. **Symlink into the primary checkout** → `path.resolve()` must
   resolve symlinks before the containment check, so a symlinked path
   cannot smuggle a write past the guard.
3. **Message addresses two owned handles** (`@ares @mini`) → both
   owners may engage. Not a violation — the operator explicitly
   addressed both.
4. **An agent with no dedicated listener** (e.g. a one-off worker) →
   not in the owned-handle table; unaffected by R3.

## Open questions

- **Q1 — guard layer location.** Where exactly does R1's guard live —
  the openclaw tool-policy hook, a chitin-kernel gate, or a shared
  Python module imported by each agent's harness? Constitution §1 puts
  side-effect gating in `chitin-kernel`; a write-path guard may fit
  there. Resolve in design review.
- **Q2 — read-only enforcement (R2).** "Mark repo files read-only" —
  actual `chmod`, or a soft policy-layer block? `chmod` is heavy-
  handed and racy across sessions. Proposed: soft policy block + a
  loud startup warning; no chmod.
- **Q3 — owned-handle list source of truth.** New file, or extend the
  agent-bus `_CANONICAL_MENTIONS` map (which already enumerates
  `clawta/ares/hermes/mini/...`)? Proposed: reuse / extend that map —
  it is already the mention registry.
- **Q4 — Ares input.** Ares never responded in thread #15. Does this
  spec land on Clawta's plan alone, or does Ares get a review pass
  first? Operator call.

## Acceptance criteria

- **AC1** — a file-mutation attempt on a path inside
  `~/workspace/chitin` while that checkout is on `main` raises a
  `PermissionError` naming the path; the same write in a
  `~/workspace/chitin-*` worktree succeeds.
- **AC2** — a write to the primary checkout while it is on a non-`main`
  branch succeeds (R1 boundary case 1).
- **AC3** — a symlinked path that resolves into the primary checkout on
  main is also refused (boundary case 2).
- **AC4** — an agent session starting with cwd in the primary checkout
  on `main` emits the R2 warning before any edit-implying plan.
- **AC5** — given `@mini ping`, only Mini's listener engages; Ares and
  Clawta do not respond.
- **AC6** — given `@ares and @mini`, both Ares and Mini may engage.
- **AC7** — the owned-handle set lives in exactly one place; adding a
  new agent listener is a one-line change.

## Slice plan

- **Slice 1** — R3 mention ownership (R3 + R4 tests for it). Smallest,
  highest-frequency failure, no kernel changes.
- **Slice 2** — R1 + R2 worktree-discipline guard (R1/R2 + R4 tests).
  Depends on Q1 (guard layer) resolution.
