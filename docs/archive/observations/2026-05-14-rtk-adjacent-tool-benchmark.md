# RTK Adjacent Tool Benchmark

Date: 2026-05-14
Ticket: t_351fde8b

This benchmark compares the operator's installed `rtk` against two adjacent
tools:

- `yvgude/lean-ctx` at HEAD `513ef79c57d2eade41d308eb71423c60414c00f5`
- `tirth8205/code-review-graph` at HEAD `52cf3bc63ee77c8b204fb809791a5f212e83a2de`

## Setup

- `rtk` was already installed at `/home/red/.local/bin/rtk`.
- `lean-ctx` was installed locally via `npm install --prefix /tmp/t351fde8b-tools lean-ctx-bin`.
- `code-review-graph` was installed locally via
  `uv tool install --python python3 /tmp/code-review-graph-t351fde8b`
  with `UV_TOOL_BIN_DIR=/tmp/t351fde8b-tools/bin`.

## Method

- Token counts were measured with `gpt-tokenizer`'s `cl100k_base`
  implementation for one consistent baseline across all tools.
- For shell-heavy workflows, the benchmark compared the raw command output
  against the nearest single-command analogue from each tool.
- For file reads, the benchmark used each tool's default or strongest
  read-oriented mode rather than forcing byte-identical output. That matters:
  `lean-ctx read` and `lean-ctx read -m map` deliberately return summaries,
  not raw file contents.
- `cargo` and `go` benchmark rows were not included because this workstation
  does not have `cargo` or `go` on `PATH`.

## Shell And Read Benchmark

| Workflow | Raw tokens | RTK tokens | Lean-ctx tokens | RTK delta | Lean-ctx delta | Notes |
| --- | ---: | ---: | ---: | ---: | ---: | --- |
| `cat README.md` | 2548 | 2548 | 154 | 0.0% | -94.0% | Lean-ctx summary stub was dramatically smaller; RTK `read` preserved the full file. |
| `cat swarm/workflows/ticket_metadata.py` | 520 | 520 | 209 | 0.0% | -59.8% | Lean-ctx `read -m map` surfaced API/deps better than a raw read. |
| `grep -R -n 'sentinel' swarm python/analysis` | 1130 | 1130 | 622 | 0.0% | -45.0% | Lean-ctx `grep` grouped matches; RTK `grep` did not materially reduce this recursive search. |
| `ls -R swarm/roles` | 65 | 21 | 55 | -67.7% | -15.4% | RTK won on compact directory summaries. |
| `pnpm exec vitest run apps/cli/tests/health.test.ts` | 5152 | 8 | 5152 | -99.8% | 0.0% | RTK collapsed a passing test run to `PASS/FAIL`; lean-ctx `-c` was pass-through here. |
| `gh pr view 112 --json number,title,files,reviews` | 881 | 881 | 881 | 0.0% | 0.0% | Neither tool improved this GitHub JSON view in the tested path. |

### Read On The Table

- RTK is strongest when the workload is noisy shell output from tests,
  build-ish tools, and directory scans.
- Lean-ctx is strongest when the workload is repo reading rather than shell
  noise, especially when its higher-level read modes are allowed to summarize
  instead of reproducing the full source.
- Lean-ctx's direct `-c` command path underperformed on this workstation for
  shell-heavy tasks. In the tested setup it did not reduce `vitest` or GitHub
  CLI output at all.
- RTK also left some workflows untouched in this sample, especially raw file
  reads and the tested `gh pr view --json` path.

## Filter Coverage

### RTK covered well in this trial

- Passing test runs (`vitest`) with extremely aggressive collapse.
- Compact directory listings (`ls`).
- Flag-preserving `find`/filesystem listing flows better than lean-ctx's
  higher-level `find` subcommand shape.

### Lean-ctx covered well in this trial

- File-read compression without needing a separate wrapper per file type.
- Structural read modes like `map`, which RTK does not currently expose.
- Grep-style search summarization better than RTK on this recursive match set.

### RTK-specific gaps worth filing upstream

1. Add a compact `gh pr view --json ...` filter path. Both tools emitted the
   full 881-token JSON payload in this benchmark.
2. Add semantic read modes beyond full-file passthrough, especially a
   dependency/API map mode for code reads.
3. Improve recursive grep compaction. In this sample RTK's grep wrapper did not
   reduce output where lean-ctx trimmed roughly 45%.

### Lean-ctx-specific gaps observed in this trial

1. The direct `lean-ctx -c` shell path did not compress `vitest` output on this
   workstation, despite README claims about test-runner patterns.
2. Its higher-level subcommands are not always a drop-in analogue for raw shell
   flags. `find` in particular is a different interface, which makes apples to
   apples shell benchmarking harder.

## Code-Review-Graph Trial

Trial target: the merged change at `80208b0` (`feat(swarm): add sentinel
invariant role (#640)`) against base `ab48bfc`.

Diff shape:

- 11 changed files
- 554 insertions, 4 deletions
- Key files in `python/analysis/`, `swarm/workflows/`, and tests

Measured review context:

| Review mode | Tokens |
| --- | ---: |
| Raw changed-file reads | 25,884 |
| `code-review-graph detect-changes --base ab48bfc` | 5,557 |

Result:

- 4.66x fewer tokens
- 78.5% reduction

Qualitative read:

- The graph output correctly concentrated review attention on
  `python/analysis/sentinel.py`, `python/analysis/writers.py`, and
  `swarm/workflows/ticket_metadata.py`.
- It also surfaced 14 test gaps and ranked the highest-risk changed functions,
  which is useful for review steering.
- The output is still verbose enough that it is best viewed as a review
  narrowing layer, not a full replacement for reading the changed code.

## Recommendation

Stick with RTK for daily shell-first coding work. It still has the strongest
payoff on the operator's actual hot path here: tests, command output, and
directory/file-system summarization.

Do not switch wholesale to lean-ctx right now. It has real strengths on
read-oriented workflows and semantic file-map modes, but its direct shell path
did not beat RTK on the shell-heavy commands that matter most in this repo.

Run `code-review-graph` alongside RTK, not instead of it. It is complementary:
RTK trims noisy command output, while code-review-graph reduces the amount of
repo context a reviewer needs to read in the first place.
