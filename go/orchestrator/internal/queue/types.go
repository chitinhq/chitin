package queue

import (
	"encoding/json"
	"time"
)

// Entry is one row in the operator escalation queue — a single PR that
// requires the operator's attention along with the canonical reason it
// surfaced. T004 (filter.go) produces []Entry from the union of live PR
// state and chain events; T005-T007 (format_table/md/json) render it.
//
// Field set matches FR-005 (table columns) plus the timestamps the
// renderer needs to compute the two "age" columns at render time. The
// JSON renderer (T007) additionally surfaces the raw triggering chain
// event via TriggeringEvent so downstream tooling (FR-007) can inspect
// the source payload without rescanning the chain.
type Entry struct {
	// PRNumber is the GitHub PR number.
	PRNumber int `json:"pr_number"`
	// Title is the PR title as reported by `gh pr list`. Not truncated
	// here — the table renderer truncates to 60 runes per FR-005.
	Title string `json:"title"`
	// URL is the PR's html_url. Empty when the entry was constructed
	// without a live PR snapshot (chain-only fallback path).
	URL string `json:"url,omitempty"`
	// Reason is one of the closed FR-008 reason kinds (e.g.
	// "iteration_cap_hit", "sibling_rebase_failed"). The kind is
	// identical to the rule name from FR-003 and the chain event
	// payload's reason string from spec 113 FR-011.
	Reason string `json:"reason"`
	// SpecRef is the spec id parsed from the PR's "sched/run/<id>" or
	// "spec-<NNN>" label, when present. Empty when the PR carries no
	// spec-ref label (e.g. operator-authored or pre-spec-id work).
	SpecRef string `json:"spec_ref,omitempty"`
	// UpdatedAt is the PR's last update timestamp from GitHub. The
	// table renderer turns now-UpdatedAt into the "age" column.
	UpdatedAt time.Time `json:"updated_at"`
	// LastAutoActionAt is the timestamp of the most-recent
	// chitin-orchestrator-authored commit on the PR head, if any.
	// Zero value renders as "-" in the table.
	LastAutoActionAt time.Time `json:"last_auto_action_at,omitempty"`
	// TriggeringEvent is the raw chain event that surfaced this PR —
	// the pr_iteration_escalated or sibling_rebase_failed row from
	// $CHITIN_DIR/events-*.jsonl whose reason matched Reason above.
	// Nil for entries surfaced by a live-state rule (e.g.
	// `stale_no_automation`, `conflicting_persistent`) that has no
	// chain event behind it. Carried through the JSON renderer
	// (FR-007) and omitted from the table/markdown formats.
	TriggeringEvent *EscalationEvent `json:"triggering_event,omitempty"`
}

// EscalationEvent is one chain row that signals a PR may need operator
// attention — mirrors the shape produced by the scanner (T002,
// internal/queue/scan.go) so the two files share a single in-memory
// type without an import cycle through a third package.
//
// Reason carries the canonical spec 114 FR-008 reason kind:
//   - for event_type "pr_iteration_escalated" it is copied from payload.reason
//   - for event_type "sibling_rebase_failed" it is the literal string
//     "sibling_rebase_failed" (the event type IS the reason — spec 112 US2)
//
// Payload is the raw `payload` object from the JSONL row, preserved
// verbatim so downstream tools (FR-007 --format json) can surface the
// full event without a second scan or schema-coupling to the writer.
type EscalationEvent struct {
	EventType string          `json:"event_type"`
	Reason    string          `json:"reason"`
	PRNumber  int             `json:"pr_number"`
	Ts        time.Time       `json:"ts"`
	RunID     string          `json:"run_id"`
	Payload   json.RawMessage `json:"payload,omitempty"`
}
