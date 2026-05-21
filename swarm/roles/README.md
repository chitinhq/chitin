# Swarm worker roles

Each `<name>/SKILL.md` defines a worker role: what tickets it claims,
what tools it uses, what success looks like, and the SDLC lifecycle it
walks.

Roles compose with **drivers** and **models** at dispatch time:

```
<driver>/<model>/<role>   e.g. codex/gpt-5.5/programmer
                                claude-code/sonnet-4-6/researcher
                                copilot/gpt-4.1/reviewer
```

Clawta's pick_driver step decides the triple (or accepts an
operator override). The role's SKILL.md is inlined into the worker's
system prompt at dispatch time.

ELO leaderboard (see `swarm-elo leaderboard`) buckets by the full
`(driver, model, role, task_class)` tuple so we can tell whether
`codex/gpt-5.5/programmer` is better at refactors than at bugfixes,
or whether `claude-code/sonnet-4-6/researcher` outperforms
`gemini/gemini-2.5-pro/researcher`.

## Adding a role

1. `swarm/roles/<name>/SKILL.md` with frontmatter:
   `name`, `description`, `allowed_tools`, `success_criteria`.
2. Body sections: When to apply, Lifecycle, The recipe, Anti-patterns,
   Output template.
3. Update `kanban-dispatch.lobster` to recognize the new role (it
   reads `ROLE` env var; default is `programmer`).
4. Optionally extend `swarm-elo` task-class mapping if the new role
   implies a new class.

## Current roles

| Role         | When                                | Output            | Lifecycle                  |
|--------------|-------------------------------------|-------------------|----------------------------|
| `programmer` | Code change tickets (feat/fix/refactor/test) | PR | ready → in_progress (through PR open + merge) → done |
| `researcher` | Investigation tickets               | Findings comment  | ready → in_progress → done |
| `reviewer`   | PR review tickets                   | PR review comment | ready → in_progress → done (the PR's own ticket stays in_progress regardless of verdict) |
| `telemetry`  | Chain-mined invariant authoring tickets | PR | ready → in_progress (through PR open + merge) → done |

## Deployment

Roles are git-tracked in this directory. To make them available to the
running swarm:

```bash
# Symlink from ~/.openclaw/roles → swarm/roles in this repo
ln -sf "$PWD/swarm/roles" ~/.openclaw/roles
```

The kanban-dispatch.lobster reads from `~/.openclaw/roles/<role>/SKILL.md`
so the symlink keeps the deployed roles in sync with main.
