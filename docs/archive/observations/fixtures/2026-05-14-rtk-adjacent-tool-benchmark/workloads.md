# Benchmark Workloads

## Shell and read comparison

1. `cat README.md`
2. `cat swarm/workflows/ticket_metadata.py`
3. `grep -R -n 'sentinel' swarm python/analysis`
4. `ls -R swarm/roles`
5. `pnpm exec vitest run apps/cli/tests/health.test.ts`
6. `gh pr view 112 --json number,title,files,reviews`

## Tool-native analogues used

- RTK:
  - `rtk read README.md`
  - `rtk read swarm/workflows/ticket_metadata.py`
  - `rtk grep sentinel swarm python/analysis`
  - `rtk ls swarm/roles`
  - `rtk pnpm exec vitest run apps/cli/tests/health.test.ts`
  - `rtk gh pr view 112 --json number,title,files,reviews`
- Lean-ctx:
  - `lean-ctx read README.md`
  - `lean-ctx read -m map swarm/workflows/ticket_metadata.py`
  - `lean-ctx grep sentinel swarm python/analysis`
  - `lean-ctx find swarm/roles`
  - `lean-ctx -c 'pnpm exec vitest run apps/cli/tests/health.test.ts'`
  - `lean-ctx -c 'gh pr view 112 --json number,title,files,reviews'`

## Code-review-graph trial

1. Base commit: `ab48bfc`
2. Review target: `80208b0` (`feat(swarm): add sentinel invariant role (#640)`)
3. Raw review baseline:
   - Read each changed file directly from the repo and tokenize the combined
     payload.
4. Graph review path:
   - `code-review-graph detect-changes --base ab48bfc`
