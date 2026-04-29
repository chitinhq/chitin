package claudecode

import (
	"encoding/json"
	"strings"

	"github.com/chitinhq/chitin/go/execution-kernel/internal/gov"
)

// Hook response exit codes per Claude Code's documented protocol.
//
//	0 — allow (empty stdout)
//	2 — block (JSON {"decision":"block","reason":"..."} on stdout;
//	    Claude Code feeds the reason back to the model as a tool error)
//	1 — non-blocking error (Claude Code surfaces to user, does not
//	    feed back to model). Used for chitin-internal failures like
//	    malformed input, not for governance denials.
const (
	ExitAllow         = 0
	ExitNonBlockError = 1
	ExitBlock         = 2
)

// Format turns a gov.Decision into the hook response: stdout JSON (or
// empty) + exit code. The Reason carries the model-visible block
// message and includes Suggestion + CorrectedCommand if present so the
// model can self-correct without operator intervention.
func Format(d gov.Decision) ([]byte, int) {
	if d.Allowed {
		return nil, ExitAllow
	}

	out := map[string]string{
		"decision": "block",
		"reason":   formatReason(d),
	}
	body, err := json.Marshal(out)
	if err != nil {
		// json.Marshal of map[string]string can't realistically fail,
		// but if it ever does, fall back to a fixed string so the model
		// still sees a denial it can react to.
		return []byte(`{"decision":"block","reason":"chitin: governance denied (marshal error)"}`), ExitBlock
	}
	return body, ExitBlock
}

// formatReason composes the model-visible message:
//
//	"chitin: <Reason> [| suggest: <Suggestion>] [| try: <CorrectedCommand>]"
//
// Empty fields are omitted. The "chitin:" prefix is always present so
// the model can recognize chitin-origin denials in transcript review.
//
// Identical shape to the v1 SDK driver's formatGuideError so audit-log
// analytics across both drivers can pattern-match the same way.
func formatReason(d gov.Decision) string {
	parts := []string{"chitin: " + d.Reason}
	if d.Suggestion != "" {
		parts = append(parts, "suggest: "+d.Suggestion)
	}
	if d.CorrectedCommand != "" {
		parts = append(parts, "try: "+d.CorrectedCommand)
	}
	return strings.Join(parts, " | ")
}
