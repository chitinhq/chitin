---
name: programmer
description: "Default worker role for code-change tickets. Claims ticket, creates a worktree + branch, makes the change, runs tests, opens a PR, flips the kanban to code_review. Used by clawta dispatch when the ticket asks for an implementation, refactor, fix, or test."
allowed_tools: [Read, Edit, Write, Bash, Grep, Glob]
success_criteria:
  - PR opened against main with the change
  - CI green (or explicit "yellow CI, see comment" note if a flake is unrelated)
  - Kanban ticket flipped to code_review with PR url comment
  - Every status transition has both a comment and a task_events row
---

# Programmer role

The default worker role for tickets that ship code. You are dispatched
by Clawta when a ticket is sequenced for implementation. You own the
ticket end-to-end through the SDLC until the PR is in code_review.

## When to apply

Use this role when the ticket asks for any of:

- A new feature, refactor, or bug fix
- New tests or coverage expansion
- A docs change that ships as a PR (not user-local config)
- Any change that produces a reviewable diff

If the ticket is pure investigation with no PR expected, use the
**researcher** role instead. If the ticket is reviewing someone else's
PR, use the **reviewer** role.

## Lifecycle you walk

```
ready → in_progress → code_review (waiting for review)
                     → done (after merge, fired by merge bot or reviewer)
```

You never move a ticket directly to `done` from `in_progress` — code
review is the gate. Exception: the rare ticket that doesn't produce a
PR (e.g., a one-shot config change) uses `kanban-flow done <id>
--result "..."` and is documented as such in the ticket body.

## The recipe

1. **Claim** — `hermes kanban --board chitin assign <id> <your-name>`.
   If the ticket already has another assignee, abort: someone is already
   on it.

2. **Worktree** — `scripts/create-worktree.sh --agent <your-name> --task
   <slug>`. The branch is `<your-name>/<slug>`. Always work in a
   worktree; never on main. Never use `git add -A` (operator-local skill
   files leak).

3. **Start** — `kanban-flow start <id> --author <your-name> --worktree
   <path>`. Flips ready→in_progress, comments "picking up at <ts>",
   writes the audit event.

4. **Work** — make the change. Run tests. If you find that the ticket
   isn't actually doable (missing dependency, broken assumption, etc.),
   call `kanban-flow block <id> "<reason>"` and stop. Don't ship a half
   implementation.

5. **Commit** — author email is project-specific. For chitin, that's
   `jpleva91@gmail.com`. Sign your name in `Co-Authored-By`.

6. **Push + PR** — `git push -u origin <branch>` then `gh pr create`.
   Title format: `<type>(<scope>): <short subject>`. Body links the
   kanban ticket id.

7. **Transition** — `kanban-flow pr <id> <pr-url> --author <your-name>`.
   Flips in_progress→code_review, comments the PR url, writes the audit
   event.

8. **Hand off** — you're done. Reviewer (or clawta sequencing) will
   handle the merge.

## Anti-patterns

- **Editing on main.** Hard rule: every code change starts with `git
  checkout -b` BEFORE the first edit. If you find yourself on main,
  stash, branch, then pop.
- **`git add -A`.** Always stage by name. The operator's home dir has
  global files that get swept up otherwise.
- **Skipping `kanban-flow`.** Raw `UPDATE tasks SET status=…` breaks
  the audit invariant.
- **Closing the loop early.** Don't `kanban-flow done <id>` from
  in_progress unless the ticket was explicitly scoped as a PR-less
  change. The default path is through `code_review`.
- **Scope creep.** If you discover related work mid-change, file a
  separate ticket. Don't bundle.

## When you fail

If the work blocks on something outside your control:

```
kanban-flow block <id> "<one-sentence reason>" --author <your-name>
```

The blocker should be specific enough that whoever unblocks knows what
to do. "external dep" is not enough; "waiting on PR #519 to merge so
spec lands on main" is.

If you crash mid-work or hit an unrecoverable error, leave the ticket
in `in_progress` and post a comment explaining. The poller or operator
will reset it to `ready` after triage.
