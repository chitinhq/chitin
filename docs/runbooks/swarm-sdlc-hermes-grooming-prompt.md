# Patching Hermes prompt for grooming-only operator mode

This runbook explains how to mirror the canonical guidance in
`swarm/prompts/hermes-grooming-guidance.md` into the live Hermes
agent's prompt builder.

## Why

Hermes used to be told (via `DISPATCH_AND_TICKETING_GUIDANCE` in
`hermes-agent/agent/prompt_builder.py`) that its job included
dispatching to clawta. That guidance led to a hallucinated handoff
on 2026-05-11 (the "I'm taking over" Discord message) when no
mechanism actually picked tickets up. After Slice C ships the
autonomous poller, Hermes no longer needs (or should have) any
dispatch behavior — it grooms and reports.

## Where the change happens

The prompt content lives in
`hermes-agent/agent/prompt_builder.py`:

- `DISPATCH_AND_TICKETING_GUIDANCE` — replace whole-block with the
  content of `swarm/prompts/hermes-grooming-guidance.md`.
- `KANBAN_GUIDANCE` — keep, but remove the Discord-broadcast clause
  that narrates a dispatch ("🦞 ${HERMES_KANBAN_TASK}: done. PR:
  <url>") — that's now done by the lobster `finalize_dispatch` step.

Hermes-agent is in a different GitHub org (`NousResearch/hermes-agent`)
than chitin. The patch flow is:

1. Open a branch in `~/.hermes/hermes-agent` that inlines the new
   content.
2. Open the PR upstream in NousResearch/hermes-agent.
3. Restart the local hermes-gateway service to pick up the change.

## The patch (one-block replacement)

In `agent/prompt_builder.py`, replace:

```python
DISPATCH_AND_TICKETING_GUIDANCE = (
    "# Dispatch and ticketing protocol (operator mode)\n"
    ...
)
```

with:

```python
DISPATCH_AND_TICKETING_GUIDANCE = (
    "# Hermes operator protocol — grooming, NOT dispatch\n"
    "\n"
    "You are the swarm's groomer + operator voice. You read the board. "
    "You answer questions about it. You move tickets between triage and "
    "ready. You DO NOT dispatch — clawta-poller does that autonomously "
    "every 2 minutes.\n"
    "\n"
    "If you find yourself narrating 'I'll dispatch this' or 'I'm taking "
    "over', stop. That hallucinated a handoff that wasn't wired. The new "
    "path: mark the ticket ready (`kanban-flow ready t_XXXXXX`), then "
    "clawta-poller's next tick picks it up.\n"
    "\n"
    "## Rule 1 — answer 'what's next?' from the board\n"
    "Call `hermes kanban --board chitin list`. Summarize: triage items "
    "you can groom now, in_progress count + code_review PRs, blocked "
    "items needing operator decision.\n"
    "\n"
    "## Rule 2 — file tickets on hermes kanban, never GitHub Issues\n"
    "    hermes kanban --board chitin create \"<title>\" --body \"<body>\" "
    "--priority <int> --assignee <profile> --triage\n"
    "Then if ready: `kanban-flow ready t_XXXXXX`. The poller dispatches "
    "within 2 minutes.\n"
    "\n"
    "## Rule 3 — NEVER shell out to clawta\n"
    "The old guidance told you to run `clawta \"dispatch t_XXX\"`. That "
    "bypasses the SDLC. Don't. Clawta-poller is the ONLY dispatcher. If "
    "the operator wants force-dispatch right now, they can run "
    "`clawta-poller --once` themselves.\n"
    "\n"
    "## Rule 4 — comment the lifecycle, not the narrative\n"
    "When asked about a ticket: read the board (`hermes kanban show t_XXX`) "
    "and quote it. If your knowledge differs from the board, the board "
    "is right.\n"
    "\n"
    "Full canonical text: see swarm/prompts/hermes-grooming-guidance.md "
    "in chitin."
)
```

## Deploy

```bash
cd ~/.hermes/hermes-agent
git checkout -b clawta/hermes-grooming-only-prompt
$EDITOR agent/prompt_builder.py   # apply the replacement above
git add agent/prompt_builder.py
git commit -m "feat(prompt): Hermes is groomer-only; clawta-poller dispatches"
git push -u origin clawta/hermes-grooming-only-prompt
gh pr create --repo NousResearch/hermes-agent --title "feat(prompt): Hermes grooming-only" --body "Mirrors chitin swarm/prompts/hermes-grooming-guidance.md"
systemctl --user restart hermes-gateway
```

## Verification

After restart, ask Hermes "what's next?" in Discord. Expected reply
shape:

> Board state: <triage count> in triage, <ready count> in ready,
> <in_progress count> in_progress (PRs in code_review: <list>), 
> <blocked count> blocked. Want me to groom any of the triage items?

Anti-test: ask Hermes "dispatch t_e1d0e815 now". Expected reply:

> Clawta-poller fires every 2 minutes and will pick up t_e1d0e815 on
> its next tick. It's currently in `ready` — already in the queue.
> Want me to bump its priority?

If Hermes still says "I'll dispatch it via clawta", the prompt update
didn't take. Re-verify the prompt_builder.py change is in place and
the gateway was actually restarted.

## Rollback

If the new prompt breaks something, revert with:

```bash
cd ~/.hermes/hermes-agent
git revert HEAD
systemctl --user restart hermes-gateway
```
