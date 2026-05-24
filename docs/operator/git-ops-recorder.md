# Git ops recorder

Operator-side diagnostic plumbing that records every git ref mutation
in this checkout to a JSONL log with full process-tree attribution.
Written to answer the recurring "who reverted this file?" question
that the reflog can't.

## What it captures

Two hooks installed under `.git/hooks/`:

| Hook | Trigger | Captures |
|---|---|---|
| `reference-transaction` | every ref mutation: commit, checkout, reset, push, pull, fetch FETCH_HEAD updates, branch -D, tag, etc. | three phases (prepared / committed / aborted), old/new oids, ref names |
| `post-checkout` | every `git checkout` (both branch switches AND `git checkout -- some/file`) | previous HEAD, new HEAD, kind (branch vs file), branch name after checkout |

Both hooks walk the calling process tree up to 6 levels via `/proc/PID/cmdline`
and `/proc/PID/stat`, capturing each frame's PID and full argv. **The process
tree is the load-bearing forensic signal** — it shows whether the git op came
from this Claude session, the openclaw gateway, a hermes worker, a cron job,
or an operator typing in a terminal.

Records are appended to `~/.chitin/git-ops.jsonl` (overridable via
`$CHITIN_GIT_OPS_LOG_DIR`). One JSON line per event. Best-effort —
the hooks NEVER block git ops; log write failures are swallowed silently.

## Install

From this repo's root:

```bash
./swarm/bin/install-git-ops-recorder.sh           # install symlinks
./swarm/bin/install-git-ops-recorder.sh --verify  # confirm installed state
./swarm/bin/install-git-ops-recorder.sh --remove  # tear down (log preserved)
```

The installer is symlink-based: hook source lives at
`swarm/hooks/git-ops-recorder/` and the replay tool at `swarm/bin/git-ops-replay`.
Future updates ship via `git pull` — no re-install needed.

For worktrees: the installer auto-detects the active gitdir via
`git rev-parse --absolute-git-dir`. Run it from within the worktree.
For an unusual setup, use `--git-dir PATH` to point at a specific
gitdir explicitly.

If a pre-existing `reference-transaction` or `post-checkout` hook is
present, the installer **refuses to overwrite** and prints a chaining
instruction. Don't silently clobber existing hooks.

## Query / replay

The `git-ops-replay` tool is symlinked to `~/.chitin/bin/git-ops-replay`
by the installer. Add `~/.chitin/bin` to your `$PATH` for bare
`git-ops-replay` invocation; or call it by absolute path.

```bash
# Everything in the log:
git-ops-replay

# Recent events:
git-ops-replay --since 1h
git-ops-replay --since 30m
git-ops-replay --since 2026-05-23T15:00

# Filter by what happened:
git-ops-replay --event post-checkout         # only checkouts
git-ops-replay --event reference-transaction # only ref mutations
git-ops-replay --kind file                   # only file-level checkouts (the usual revert suspect)
git-ops-replay --kind branch                 # only branch switches
git-ops-replay --phase committed             # only successful ref mutations

# Filter by what was touched:
git-ops-replay --ref refs/heads/main         # only events touching main
git-ops-replay --ref refs/heads/spec/096     # only events touching spec/096-* branches

# Filter by who did it (process-tree attribution — the forensic query):
git-ops-replay --pid-cmd-contains openclaw   # came from openclaw
git-ops-replay --pid-cmd-contains hermes     # came from hermes
git-ops-replay --pid-cmd-contains claude     # came from a Claude session
git-ops-replay --pid-cmd-contains cron       # came from a cron job

# Limit and serialize:
git-ops-replay --tail 10
git-ops-replay --tail 10 --json | jq
```

## Output format

Default human-readable rendering shows: timestamp, event type, key
fields (phase / kind / ref / oid prefixes), then the top 5 process-tree
frames as `pid=N  <argv>`.

JSON mode (`--json`) emits the raw record per line, suitable for `jq`.

Record schema for `reference-transaction`:

```json
{
  "ts": "2026-05-23T18:42:51.551569Z",
  "event": "reference-transaction",
  "phase": "committed",
  "repo": "/home/red/workspace/chitin",
  "cwd": "/home/red/workspace/chitin",
  "updates": [
    {"old": "abc...", "new": "def...", "ref": "refs/heads/main"}
  ],
  "process_tree": [
    {"pid": 3181913, "cmd": "bash .../hooks/reference-transaction committed"},
    {"pid": 3181688, "cmd": "git tag -d test-recorder-tag"},
    {"pid": 3181573, "cmd": "/usr/bin/zsh -c ..."},
    {"pid": 1000065, "cmd": "claude --dangerously-skip-permissions"},
    {"pid": 999285, "cmd": "/usr/bin/zsh"},
    {"pid": 5713, "cmd": "/usr/bin/kitty"}
  ]
}
```

Record schema for `post-checkout`:

```json
{
  "ts": "2026-05-23T18:44:32.791394Z",
  "event": "post-checkout",
  "kind": "branch",
  "prev_head": "8bcc905e37...",
  "new_head": "c0aaeb1018...",
  "branch_after": "main",
  "repo": "/home/red/workspace/chitin",
  "cwd": "/home/red/workspace/chitin",
  "process_tree": [...]
}
```

## What it DOESN'T see

- **Read-only operations**: `git status`, `git log`, `git show`. No
  ref mutation = no log entry. By design.
- **Operations that happened before the hook was installed.** No
  retroactive capture.
- **Operations on different clones** of this repo. Each clone needs
  its own hook install. The exception is `git worktree`-created
  worktrees, which share the parent repo's `hooks` dir and so DO
  fire the recorder.
- **Remote-side operations.** Only local-side mutations are captured.

## Log hygiene

The log appends forever. Rotate manually if it grows too large:

```bash
# Archive and start fresh:
mv ~/.chitin/git-ops.jsonl ~/.chitin/git-ops-$(date +%Y%m%d).jsonl

# Or trim to recent only:
tail -10000 ~/.chitin/git-ops.jsonl > /tmp/recent && mv /tmp/recent ~/.chitin/git-ops.jsonl
```

A future improvement could be daily rollover (`git-ops-2026-05-23.jsonl`);
the current implementation keeps it simple by using one file.

## Failure modes

- **Disk full / log unwritable**: hook still exits 0; ref op succeeds; record lost.
- **`/proc` unavailable** (non-Linux): process tree walk returns the bash hook PID
  only; no parent attribution. Hook still records the event.
- **`date` missing `%6N`** (older BusyBox): timestamp resolution drops to whole seconds.
  Records still land.
- **Existing hooks at install target**: installer refuses to overwrite; operator must chain manually.
