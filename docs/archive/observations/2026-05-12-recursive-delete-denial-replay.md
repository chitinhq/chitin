# Recursive Delete Denial Replay

Ticket: `t_af9d0df1`

Source: local replay over `~/.chitin/gov-decisions-2026-05-10.jsonl` through
`~/.chitin/gov-decisions-2026-05-13.jsonl`.

## Summary

Recent recursive-delete-shaped denials split into 11 rows:

- 5 rows normalized to `file.recursive_delete` under `no-rm-recursive`.
- 6 rows stayed `shell.exec` under the legacy substring fallback
  `no-destructive-rm`.
- True positives were actual recursive cleanup or clone-reset commands under
  `/tmp`.
- False positives were commands whose arguments mentioned destructive cleanup
  text or wrapped GitHub CLI PR commands with `--delete-branch`.

## Replay Classification

| Sample | Previous classification | Replay result after fix | Notes |
| --- | --- | --- | --- |
| `rtk gh pr close 532 ... --delete-branch` | `file.recursive_delete` deny | `github.pr.close` | False positive. `--delete-branch` is GitHub branch cleanup, not filesystem recursion. |
| `rg -n "...rm -rf..." ...` | `shell.exec` deny | `file.read` | False positive. Searching for a literal destructive command is read-only. |
| `tmp=$(mktemp -d); ...; rm -rf "$tmp"` | `file.recursive_delete` deny | `file.recursive_delete` deny | True positive. Variable-backed recursive cleanup remains blocked. |
| `cd /tmp && rm -rf <dir> && git clone ...` | `file.recursive_delete` deny | `file.recursive_delete` deny | True positive. Clone-reset convenience cleanup is still recursive deletion. |

## Follow-Up Guidance

Worker temp cleanup remains intentionally blocked. Use
`docs/runbooks/safe-temp-work.md` for the safe alternatives: framework-owned
temp directories, exact-file deletion followed by `rmdir`, or leaving unique
`mktemp -d` directories for host temp cleanup.
