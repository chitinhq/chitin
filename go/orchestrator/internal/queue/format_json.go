package queue

import (
	"bytes"
	"encoding/json"
)

// FormatJSON renders entries as a machine-readable JSON array (FR-007).
// One object per PR, carrying the FR-005 column fields plus the raw
// triggering chain event payload when one is attached. Output is
// indented for diff-friendliness and ends in a trailing newline so
// shell redirects (`> queue.json`) produce a POSIX-compliant text file.
//
// The empty case renders as `[]\n` rather than `null\n` — downstream
// tools can iterate over the result unconditionally without nil-guards
// (spec 114 edge case: "no escalations at all" still produces a
// valid, parseable shape). The CLI is responsible for the friendlier
// "✅ no PRs need attention" message in the human-facing formats; the
// JSON renderer is deliberately literal.
//
// HTML escaping is disabled because the values are GitHub-sourced PR
// titles and event payloads that may legitimately contain `<`, `>`,
// `&`, or `/` — preserving them verbatim keeps the JSON round-trip
// byte-identical for tools that compare hashes.
//
// Marshal failure is essentially impossible (Entry's fields are simple
// types and TriggeringEvent.Payload is json.RawMessage, which is
// pass-through), but the error return is preserved so callers can
// distinguish a renderer fault from an empty queue.
func FormatJSON(entries []Entry) (string, error) {
	if entries == nil {
		entries = []Entry{}
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(entries); err != nil {
		return "", err
	}
	return buf.String(), nil
}
