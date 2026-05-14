# RTK Adjacent Tool Benchmark Fixtures

Date: 2026-05-14
Ticket: t_351fde8b

This fixture bundle captures the exact benchmark workloads and the kanban
comment body used by `docs/archive/observations/2026-05-14-rtk-adjacent-tool-benchmark.md`.

## Files

- `workloads.md` - the shell and review workflows benchmarked
- `kanban-comment.md` - the exact verdict comment posted to the Hermes board

## Notes

- Token counts in the observation were normalized with `cl100k_base` to keep
  the three tool outputs comparable.
- The shell rows intentionally mix raw commands with tool-native read modes.
  That reflects the operator decision point: "which tool would I actually reach
  for on this workload?"
