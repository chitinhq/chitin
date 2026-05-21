# Constitution amendment draft (2026-05-17)
# Not yet proposed — pending red's morning review per §10.4

## §6 additions (tracked source)

### §6.3 Runtime deploy contract

Every script that runs on the operator's box must have either:
1. A symlink from the install path to the repo source (preferred), OR
2. An idempotent installer script at `swarm/bin/install-<name>.sh` with `--verify` mode

If a script has both a symlink and an installer, the installer's `--verify` mode should confirm the symlink resolves correctly. The installer must be tracked in git and reviewed like any other code change (§10.1).

Deploy drift is tech debt. The runtime drift audit protocol:
- Run `./install-*.sh --verify` after every merge that touches `swarm/bin/` or `swarm/prompts/`
- Any drift found → open a ticket, fix immediately (§6 violation)

### §6.4 Cron prompt durability

Cron job prompts (e.g., board-watchdog) that affect ticket state must:
1. Have their authoritative text tracked in `swarm/prompts/<name>.md`
2. Have an installer that copies the tracked text to `~/.hermes/cron/jobs.json`
3. Be verified after every change (run `./install-board-watchdog-prompt.sh --verify`)

Rationale: During the 2026-05-16 validation run, the board-watchdog prompt was modified in `jobs.json` but not tracked in git. This meant the fix could silently revert or drift. Per §6 (tracked source over local patches), prompts that control enforcement must be version-controlled.

## §10 additions (governance lessons from validation run)

### §10.7 Verify-before-draft

Before drafting a spec or PR, check `git log` and `gh pr list` to verify the work hasn't already been done. The spec-kit batch 002-004 drafts were wasted because those specs had already been merged in a prior session. Time cost: ~20 minutes of parallel work.

### §10.8 Editing primary checkout

The primary checkout at `~/workspace/chitin` is READ-ONLY (per §2). All branch work happens in sibling worktrees (`~/workspace/chitin-*`). This rule was violated 3 times in one session before being corrected. Each violation required backing out changes and re-doing them in a worktree.

### §10.9 Cron prompts are untracked code

Cron job prompts in `jobs.json` are the most consequential untracked code in the system. A prompt change (like the watchdog spec-awareness fix) that isn't tracked in git can silently revert between job edits. Always pair prompt changes with a tracked `swarm/prompts/<name>.md` file and an installer script (see §6.4).