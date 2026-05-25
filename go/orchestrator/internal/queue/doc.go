// Package queue computes and renders the operator escalation queue
// (spec 114) — the subset of open PRs in the chitin factory that need
// the operator's judgement, hiding PRs the factory is handling cleanly.
//
// Present in this package today:
//
//   - types.go        — shared Entry row shape rendered by every formatter
//   - format_table.go — text/tabwriter renderer (FR-005, T005)
//
// Planned for follow-up tasks (not yet implemented):
//
//   - scan.go         — chain-event reader over $CHITIN_DIR/events-*.jsonl (T002)
//   - live.go         — gh pr list adapter for open-PR snapshot (T003)
//   - filter.go       — composes events + live PRs into the "needs operator"
//     set per FR-003, producing []Entry (T004)
//   - format_md.go    — markdown table renderer (FR-006, T006)
//   - format_json.go  — JSON renderer (FR-007, T007)
//
// The shared Entry type carries the PR identity plus the canonical
// reason kind from the FR-008 closed taxonomy.
package queue
