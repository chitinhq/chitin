// Package queue computes the spec 114 operator escalation surface.
//
// scan.go is the chain-event reader: it walks `$CHITIN_DIR/events-*.jsonl`
// (the canonical append-only JSONL store written by `chitin-kernel emit`)
// and indexes escalation events by PR number. The scanner is a pure
// READER — it does not introduce any new `chitin-kernel` subcommand,
// preserving the architectural rule that the kernel owns the WRITE
// path (constitution §1).
//
// The escalation taxonomy is spec 114 FR-008 (closed set, kept in sync
// with spec 113 FR-011's `reason` strings):
//
//   - iteration_cap_hit                    ← pr_iteration_escalated
//   - human_reviewer_present               ← pr_iteration_escalated
//   - lease_lost                           ← pr_iteration_escalated
//   - iteration_completed_with_skips       ← pr_iteration_escalated
//   - sibling_rebase_failed                ← sibling_rebase_failed (event_type)
//
// The remaining reason kinds in FR-008 (`dialectic_request_changes`,
// `stale_no_automation`, `conflicting_persistent`) are derived from
// live PR state, not the chain, and are computed by internal/queue/filter.go.
package queue

import (
	"bufio"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// EscalationEvent is one chain event observed during a Scan that signals
// a PR may need operator attention.
//
// Reason carries the canonical spec 114 FR-008 reason kind:
//   - for event_type "pr_iteration_escalated" it is copied from payload.reason
//   - for event_type "sibling_rebase_failed" it is the literal string
//     "sibling_rebase_failed" (the event type IS the reason)
//
// Payload is the raw `payload` object from the JSONL row, preserved so
// downstream tooling (FR-007 --format json) can surface the full event
// without a second scan.
type EscalationEvent struct {
	EventType string          `json:"event_type"`
	Reason    string          `json:"reason"`
	PRNumber  int             `json:"pr_number"`
	Ts        time.Time       `json:"ts"`
	RunID     string          `json:"run_id"`
	Payload   json.RawMessage `json:"payload"`
}

// escalationEventTypes is the closed set of chain event_types the scanner
// matches. Other event types are skipped without parsing payload.
var escalationEventTypes = map[string]struct{}{
	"pr_iteration_escalated": {},
	"sibling_rebase_failed":  {},
}

// piEscalatedReasons is the closed set of pr_iteration_escalated payload
// reasons recognised by spec 114 FR-008. An event with an unknown reason
// is skipped (logged elsewhere; the scanner is silent on this — see the
// "implementers MUST NOT invent additional event types" clause of spec
// 113 FR-010).
var piEscalatedReasons = map[string]struct{}{
	"iteration_cap_hit":              {},
	"human_reviewer_present":         {},
	"lease_lost":                     {},
	"iteration_completed_with_skips": {},
}

// Scan walks chainDir (or the resolved default when chainDir == "") for
// every `events-*.jsonl` file, filters rows whose event_type is in the
// escalation taxonomy, and returns an index keyed by PR number.
//
// When since is non-zero, events with ts strictly before since are
// dropped. When since is the zero value, no time filter is applied.
//
// Ordering invariant: within a PR's slice, events are appended in the
// order encountered during the scan (file order via filepath.Glob, then
// line order within each file). The kernel does not guarantee that file
// order matches global wall-clock order, so callers that need a
// time-ordered view MUST sort by EscalationEvent.Ts with a stable
// tie-breaker (e.g. PRNumber + RunID + ts).
//
// Boundary contracts:
//   - chainDir does not exist → returns an empty map, nil error
//   - chainDir is empty (no matching files) → empty map, nil error
//   - malformed JSON line → skipped (spec 114 edge case: "never crashes
//     on malformed events")
//   - payload.pr_number missing or ≤ 0 → skipped (orphan PR ref edge case)
//   - payload.ts unparseable → skipped (cannot apply since filter safely)
//   - per-file os.PathError (e.g. rotated mid-scan) → tolerated; the scan
//     continues with the remaining files and returns nil error
func Scan(chainDir string, since time.Time) (map[int][]EscalationEvent, error) {
	if chainDir == "" {
		chainDir = ResolveChainDir()
	}
	pattern := filepath.Join(chainDir, "events-*.jsonl")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, err
	}
	out := make(map[int][]EscalationEvent)
	if len(matches) == 0 {
		return out, nil
	}
	for _, path := range matches {
		if ferr := scanFile(path, since, out); ferr != nil {
			if errors.Is(ferr, fs.ErrNotExist) {
				continue
			}
			// Other IO errors are also tolerated — partial visibility is
			// strictly better than no visibility for a queue surface.
			// The caller can warn from a single logger if it cares.
			continue
		}
	}
	return out, nil
}

// ResolveChainDir mirrors the kernel's chain dir resolution: $CHITIN_DIR
// → $HOME/.chitin → ./.chitin as last resort. Exported so other queue
// helpers (filter, format) can share the lookup.
func ResolveChainDir() string {
	if d := os.Getenv("CHITIN_DIR"); d != "" {
		return d
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".chitin")
	}
	return ".chitin"
}

// scanFile streams one JSONL file. Tolerant: any single bad line is
// skipped, but a file-open error propagates so Scan can decide whether
// to ignore (fs.ErrNotExist) or short-circuit.
func scanFile(path string, since time.Time, out map[int][]EscalationEvent) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	// Kernel-side cap is ~1 MiB per event line; match it.
	sc.Buffer(make([]byte, 64*1024), 1<<20)

	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		// Cheap pre-filter avoids the JSON parse cost on the ~99% of
		// chain lines that are not escalation events. The substrings
		// are payload-distinct enough that false positives are rare;
		// the structural check below is authoritative.
		if !looksLikeEscalation(line) {
			continue
		}
		ev, ok := parseEscalation(line)
		if !ok {
			continue
		}
		if !since.IsZero() && ev.Ts.Before(since) {
			continue
		}
		out[ev.PRNumber] = append(out[ev.PRNumber], ev)
	}
	// Scanner.Err here would surface a torn line or oversize line; we
	// already tolerated everything per-line, so propagate nil — partial
	// visibility is preferred over a hard error from the queue.
	_ = sc.Err()
	return nil
}

// looksLikeEscalation is a byte-level pre-filter. It returns true if
// the line contains a substring matching one of the escalation
// event_types. False positives are filtered out by parseEscalation.
func looksLikeEscalation(line []byte) bool {
	s := string(line)
	if strings.Contains(s, "pr_iteration_escalated") {
		return true
	}
	if strings.Contains(s, "sibling_rebase_failed") {
		return true
	}
	return false
}

// parseEscalation parses one JSONL line into an EscalationEvent. Returns
// (zero, false) for any row that fails the structural contract (wrong
// event_type, missing pr_number, unknown reason, unparseable ts).
func parseEscalation(line []byte) (EscalationEvent, bool) {
	var row struct {
		EventType string          `json:"event_type"`
		Ts        string          `json:"ts"`
		RunID     string          `json:"run_id"`
		Payload   json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal(line, &row); err != nil {
		return EscalationEvent{}, false
	}
	if _, ok := escalationEventTypes[row.EventType]; !ok {
		return EscalationEvent{}, false
	}

	var p struct {
		PRNumber int    `json:"pr_number"`
		Reason   string `json:"reason"`
	}
	if err := json.Unmarshal(row.Payload, &p); err != nil {
		return EscalationEvent{}, false
	}
	if p.PRNumber <= 0 {
		return EscalationEvent{}, false
	}

	reason, ok := classifyReason(row.EventType, p.Reason)
	if !ok {
		return EscalationEvent{}, false
	}

	ts, err := parseTs(row.Ts)
	if err != nil {
		return EscalationEvent{}, false
	}

	return EscalationEvent{
		EventType: row.EventType,
		Reason:    reason,
		PRNumber:  p.PRNumber,
		Ts:        ts,
		RunID:     row.RunID,
		Payload:   row.Payload,
	}, true
}

// classifyReason maps (event_type, payload.reason) to a canonical
// spec 114 FR-008 reason kind. Returns ("", false) when the pair is
// not in the closed taxonomy.
func classifyReason(eventType, payloadReason string) (string, bool) {
	switch eventType {
	case "sibling_rebase_failed":
		// Event type IS the reason for this kind; payload.reason is
		// not part of spec 112 US2's contract.
		return "sibling_rebase_failed", true
	case "pr_iteration_escalated":
		if _, ok := piEscalatedReasons[payloadReason]; !ok {
			return "", false
		}
		return payloadReason, true
	}
	return "", false
}

// parseTs accepts RFC3339 and RFC3339Nano (the two timestamp formats
// the kernel emits — see emit.withinDedupWindow for the parallel
// handling in the writer).
func parseTs(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, errors.New("empty ts")
	}
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t, nil
	}
	return time.Parse(time.RFC3339, s)
}
