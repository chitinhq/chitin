Benchmarked RTK against lean-ctx and code-review-graph on a real repo workload.

Shell/read table:
- `cat README.md`: raw 2548, RTK 2548, lean-ctx 154
- `cat swarm/workflows/ticket_metadata.py`: raw 520, RTK 520, lean-ctx 209
- `grep -R -n 'sentinel' swarm python/analysis`: raw 1130, RTK 1130, lean-ctx 622
- `ls -R swarm/roles`: raw 65, RTK 21, lean-ctx 55
- `pnpm exec vitest run apps/cli/tests/health.test.ts`: raw 5152, RTK 8, lean-ctx 5152
- `gh pr view 112 --json number,title,files,reviews`: raw 881, RTK 881, lean-ctx 881

Review trial:
- `code-review-graph detect-changes --base ab48bfc` vs raw changed-file reads on `80208b0`
- Raw review context: 25,884 tokens
- Graph review context: 5,557 tokens
- Result: 4.66x fewer tokens, 78.5% reduction

Verdict:
- Stick with RTK for daily shell-first work. It still wins on noisy test and directory output.
- Do not switch wholesale to lean-ctx. It helps on semantic file reads, but its direct shell path did not beat RTK on the hot-path commands that matter here.
- Run code-review-graph alongside RTK for review narrowing.

RTK gaps worth filing:
1. Add a compact `gh pr view --json ...` filter path.
2. Add semantic read modes like dependency/API maps for code reads.
3. Improve recursive grep compaction.
