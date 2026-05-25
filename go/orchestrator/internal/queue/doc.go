// Package queue computes and renders the operator escalation queue
// (spec 114) — the subset of open PRs in the chitin factory that need
// the operator's judgement, hiding PRs the factory is handling cleanly.
//
// The package is composed of:
//
//   - scan.go         — chain-event reader over $CHITIN_DIR/events-*.jsonl (T002)
//   - live.go         — gh pr list adapter for open-PR snapshot (T003)
//   - filter.go       — composes events + live PRs into the "needs operator"
//     set per FR-003, producing []Entry (T004)
//   - format_table.go — text/tabwriter renderer (FR-005, T005)
//   - format_md.go    — markdown table renderer (FR-006, T006)
//   - format_json.go  — JSON renderer (FR-007, T007)
//
// The shared Entry type (types.go) is the single shape the three
// formatters render. It carries the PR identity plus the canonical
// reason kind from the FR-008 closed taxonomy, and (for the JSON
// renderer) the raw triggering chain event that surfaced the PR.
package queue
