# chitin-runner — hermes profile

A hermes kanban worker profile that proxies backlog work into
`chitin-execute-request`. Replaces `chitin-dispatcher.service`: the
kanban dispatcher claims a ready card, spawns this profile, the
profile invokes the runner CLI, the runner runs the agent (with
kernel-driven tier escalation built in).

## Install

```bash
hermes profile create chitin-runner --no-alias
cp apps/runner/hermes-profile/SOUL.md ~/.hermes/profiles/chitin-runner/SOUL.md
```

## Prerequisites

`chitin-execute-request` must be on the profile worker's `$PATH`. The bin
lives at `apps/runner/bin/chitin-execute-request`. Easiest:

```bash
sudo ln -sf "$(pwd)/apps/runner/bin/chitin-execute-request" /usr/local/bin/chitin-execute-request
sudo ln -sf "$(pwd)/apps/runner/bin/chitin-agent-runner"    /usr/local/bin/chitin-agent-runner
```

(Or add `apps/runner/bin/` to `$PATH` in your shell profile if you'd
rather not symlink globally.)

## Wire-up to the kanban

Cards assigned to `chitin-runner` will be claimed by hermes's built-in
dispatcher (`hermes gateway start`) and spawn this profile. Update the
mirror to assign that:

```bash
# in apps/runner/src/kanban-mirror.ts, change the --assignee value
# from entry.role to 'chitin-runner' (or override per-role per taste)
```

## How a spawn looks

When a card is claimed, hermes runs:

```
hermes -p chitin-runner --skills kanban-worker chat -q "work kanban task t_<id>"
```

with `HERMES_KANBAN_TASK=t_<id>` set. The SOUL.md tells the LLM
exactly what to do: invoke `chitin-execute-request --from-kanban-card
"$HERMES_KANBAN_TASK"`, parse the JSON envelope, call `kanban_complete`.
