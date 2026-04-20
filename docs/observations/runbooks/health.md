# `chitin health` runbook

What each output row means and what to do when it's not green.

`chitin health` exits **0** iff every row is `[PASS]` or `[WARN]`. Exits **1**
if any row is `[FAIL]` or the `.chitin` dir is missing. CI should gate on the
exit code; humans should read the table.

## Output rows

### `chitin dir MISSING` — [FAIL]

The `.chitin/` state dir at the resolved path does not exist.

**Do:**

1. If on a fresh box: run `chitin install --surface claude-code --global`. This creates `$HOME/.chitin/` and wires the Claude Code hook.
2. If on an existing box: the resolver may have walked up from cwd and fallen back to `$HOME/.chitin/` which you haven't created. Check `pwd` — if you expected to report on a repo-local `.chitin/`, it isn't there. `chitin-kernel init --dir .chitin` creates one in the current dir.
3. If passing `--chitin-dir` directly: verify the path. A typo here currently reads as "cleanly installed but no events," which this row is designed to catch.

### `events total 0` — [WARN]

No events in the window. Either nothing has run through the hook in the last 24h, or the hook isn't wired.

**Do:**

1. Run anything through Claude Code in a dir with `.chitin/`. Re-run `chitin health`.
2. If still zero: check `cat ~/.claude/settings.json` for a `PreToolUse` hook entry pointing at chitin. `chitin install --surface claude-code --global` will repair it.
3. If events arrive but the window is empty, widen: `chitin health --window-hours 168`.

### `hook failures > 0` — [FAIL]

`kernel-errors.log` has non-empty lines — the hook crashed N times.

**Do (shed order):**

1. **Stop before autopsy.** Check `tail -n 20 .chitin/kernel-errors.log` to see the last errors. If they are `parse_event` on malformed JSON, the hook is wedged and every Claude Code invocation is failing silently — priority one.
2. Identify the failure class:
   - `parse_event` → adapter is sending malformed envelopes; check adapter version.
   - `sqlite_busy` → concurrent emits; rare but possible on heavy parallel agents.
   - `chitindir_resolve` → resolver can't find a writable `.chitin/`; check `$HOME` perms.
3. After fixing, truncate the log: `: > .chitin/kernel-errors.log` — then re-run `chitin health` to confirm.

### `schema drift > 0` — [FAIL]

Events that violate the v2 contract (parse fail, missing `schema_version`, `schema_version != "2"`, empty `surface`, unparseable `ts`).

The kernel emits events to `.chitin/events-<run_id>.jsonl` (one file per run). `chitin health` scans every `*.jsonl` under the dir, so a drift count reflects events across all run files.

**Do:**

1. Most common cause: legacy events from a pre-v2 kernel. Find candidates with `ls -1 .chitin/events-*.jsonl` and check one: `head -1 .chitin/events-<run_id>.jsonl | jq .`. If `schema_version` is null or `"1"`, archive that run file: `mv .chitin/events-<run_id>.jsonl .chitin/events-<run_id>.jsonl.legacy-$(date +%s)` and let newer runs accumulate cleanly.
2. Less common: a corrupted line (truncated mid-write, partial UTF-8). Walk the files: `for f in .chitin/events-*.jsonl; do jq -c . "$f" > /dev/null || echo "bad: $f"; done`.
3. If the drift persists after archiving legacy data, the adapter is emitting malformed envelopes — file a ledger entry.

### `orphaned chains > 0` — [WARN]

Reserved. Not yet computed; always 0. Will be wired in Phase D via an `events.db` scan.

### `clock skew suspected` — [WARN]

An event has a `ts` > 1h in the future.

**Do:**

1. `timedatectl status` (linux) / `sntp -sS time.apple.com` (mac) to resync.
2. If containerized: check the container's epoch. A container booted from a bad clock will stamp events in the future; fix at the container runtime.
3. After resync, re-run `chitin health`. The warning will clear on the next event write; existing misstamped events will age out of the window.

## Known limitations (tracked for Phase D ledger)

- **Large JSONL scans are O(n) with no timeout.** A 1GB total across `events-*.jsonl` files can take minutes; no progress indicator. Future: cap scan duration, return partial with `truncated: true`.
- **Silent `$HOME/.chitin` fallback.** If cwd has no `.chitin/`, the resolver quietly uses `$HOME/.chitin/`. The output header labels the resolved dir, but operators skim. Future: render a `[WARN]` when resolved dir ≠ cwd-local `.chitin/`.
