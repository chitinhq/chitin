# Spec 051: Mini goal_id derived from spec references

**Status**: DRAFT 2026-05-19 — awaiting red sign-off (constitution §1
pair-write rule). Slot 051 free (050 was the last numbered spec).

**Author lens (Knuth)**: name the identifier. A goal_id is the stable
key for a session across state dir, worktree, branch, kitty user_var,
and the event log. An identifier minted from a sentence is a smell —
it tells you the author had not decided what the thing *is*.

## Summary

When Mini is dispatched via spec references (spec 050), the session's
`goal_id` is minted from the **composed goal text** by `mint_goal_id`.
The composed goal starts with the sentence "Implement the following
ratified specs in one shot, in order:" — so every spec-dispatched
session is named `implement-the-following-ratified-<hash>`. Observed
live: `implement-the-following-ratified-10d2884f`,
`implement-the-following-ratified-470a7d17`, etc.

This is useless as an identifier — every session looks identical, the
spec being worked is invisible in the id, and operators cannot tell two
sessions apart without opening each one. The `goal_id` should be
derived from the **spec references**, not the prose.

## Motivation

`goal_id` is load-bearing in five places: the state dir name
(`~/.swarm/octi/<goal_id>/`), the worktree
(`~/workspace/chitin-octi-<goal_id>/`), the git branch
(`octi/<goal_id>`), the kitty window `user_var:mini_goal`, and every
Discord event-log line (spec 050 R5). When the id is
`implement-the-following-ratified-10d2884f`:

1. **Operator can't scan.** `mini_list` and the #mini feed show a wall
   of `implement-the-following-ratified-*` rows. Which one is spec 039?
   Unknowable without drilling in.
2. **Worktree/branch names are noise.** `octi/implement-the-following-
   ratified-10d2884f` tells a reviewer nothing about what the branch
   contains.
3. **The hash is the only signal.** An 8-hex-char suffix is the entire
   discriminator. That is an accident, not a design.

## Non-goals

- No change to `mint_goal_id` for the free-form CLI path
  (`mini open --goal "..."` break-glass). That path has only prose to
  work with; deriving from prose is acceptable there.
- No retro-rename of existing sessions.

## Requirements

### R1 — spec-dispatched sessions get a spec-derived goal_id

When a session is opened against spec reference(s), the `goal_id` MUST
be derived from the resolved spec numbers, not the goal text. Shape:

```
spec-<NNN>[-<NNN>...]-<hash>
```

- Single spec: `spec-039-3a1f`
- Multiple / range: `spec-037-038-039-3a1f` OR, when the resolved set
  is a contiguous ascending run, the compact form `spec-037..039-3a1f`.
- `<hash>` is a short (4–8 hex) uniqueness suffix so two dispatches of
  the same spec set don't collide on the state dir.

The hash keeps the AC2 collision guarantee from spec 038/050; the
prefix makes the id legible.

### R2 — the MCP layer owns the derivation

`mini_open` (spec 050, `services/mini-mcp/server.py`) already resolves
the spec references before spawning. It is the natural owner of the
id. Two viable mechanisms — design review picks one:

- **(a)** `mini open` grows an optional `--goal-id <id>` flag; the MCP
  layer computes the spec-derived id and passes it.
- **(b)** `mini open` grows an optional `--spec <ref> ...` set; the CLI
  itself derives the id and composes the goal.

Proposed: (a) — keeps spec-resolution logic in one place (the MCP
server, spec 050), and `--goal-id` is a generally useful CLI affordance.

### R3 — collision behavior unchanged

If the derived `goal_id` (including hash) collides with an existing
state dir, the existing `GoalIdCollisionError` path fires unchanged.
The hash makes this near-impossible; the guarantee is not weakened.

## Boundary cases

1. **Many specs** (e.g. a 6-spec range) → the compact contiguous form
   `spec-040..045-<hash>` keeps the id bounded. A non-contiguous set
   that would produce an over-long id is truncated to
   `spec-<first>-<last>-multi-<hash>` past a length cap (cap TBD in
   design review).
2. **Branch/dir length limits** → the id must stay within filesystem
   and git ref length limits. The length cap in (1) enforces this.
3. **Re-dispatch of the same spec** → different hash → different id →
   no collision. Intended.

## Open questions

- **Q1 — length cap.** What is the max `goal_id` length before the
  truncation rule in boundary case 1 kicks in? Proposed: 64 chars
  (well under git ref / path limits).
- **Q2 — compact range form.** Use `037..039` (compact) or
  `037-038-039` (explicit)? Compact is shorter; explicit is greppable
  per spec. Proposed: compact for contiguous runs of 3+, explicit for
  2.

## Acceptance criteria

- **AC1** — `mini_open(specs=["039"])` produces a `goal_id` matching
  `^spec-039-[0-9a-f]{4,8}$`.
- **AC2** — `mini_open(specs=["037-039"])` produces a `goal_id` whose
  prefix names all three specs (compact or explicit per Q2).
- **AC3** — the state dir, worktree, branch, and kitty `user_var` all
  carry the spec-derived id consistently.
- **AC4** — two dispatches of the same spec set produce two distinct
  `goal_id`s (hash differs); neither raises a spurious collision.
- **AC5** — the free-form `mini open --goal "..."` path is unchanged —
  still uses `mint_goal_id` on the prose.

## Slice plan

Single slice — R1, R2, R3. Small, self-contained.
