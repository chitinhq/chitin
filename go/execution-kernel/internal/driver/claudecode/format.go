package claudecode

import (
	"encoding/json"
	"io"
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
//
// Lockdown is special: a regular deny invites the model to try a
// variant (the chain shows this as the loop driving total denials
// toward the lockdown threshold). Once locked, every subsequent tool
// call also returns RuleID="lockdown", and without a session-stop
// signal the model keeps retrying until the operator intervenes —
// observed as 26 lockdown fires from a single envelope over 7+ hours.
// Setting `continue: false` is Claude Code's documented affordance for
// the hook to stop the agent loop after honoring the block; we pair it
// with a `stopReason` carrying the operator-recovery command so the
// surfaced message matches the copilot driver's LockdownError.Error().
func Format(d gov.Decision) ([]byte, int) {
	if d.Allowed {
		return nil, ExitAllow
	}

	out := map[string]any{
		"decision": "block",
		"reason":   formatReason(d),
	}
	if d.RuleID != "" {
		out["rule_id"] = d.RuleID
	}
	if d.RuleID == "lockdown" {
		out["continue"] = false
		out["stopReason"] = formatLockdownStopReason(d)
	}
	body, err := json.Marshal(out)
	if err != nil {
		// json.Marshal of the simple map above can't realistically fail,
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
	reason := d.Reason
	if d.RuleID != "" && !strings.Contains(reason, d.RuleID) {
		if reason == "" {
			reason = d.RuleID
		} else {
			reason = d.RuleID + ": " + reason
		}
	}
	parts := []string{"chitin: " + reason}
	if d.Suggestion != "" {
		parts = append(parts, "suggest: "+d.Suggestion)
	}
	if d.CorrectedCommand != "" {
		parts = append(parts, "try: "+d.CorrectedCommand)
	}
	return strings.Join(parts, " | ")
}

// formatLockdownStopReason returns the operator-facing message Claude
// Code surfaces when it stops the agent loop. Mirrors the copilot
// driver's LockdownError.Error() so audit/operator tooling sees one
// shape across drivers. Agent name is included verbatim in the reset
// command — when empty (defensive: gate.go always stamps it on
// lockdown), the recovery command degrades to a usage hint.
func formatLockdownStopReason(d gov.Decision) string {
	agent := d.Agent
	if agent == "" {
		agent = "<agent>"
	}
	return "chitin: agent in lockdown — session terminated. " +
		"Reset with: chitin-kernel gate reset --agent=" + agent
}

// FormatWriter is the io.Writer-shaped variant used by the driver
// dispatcher (internal/driver/formatselect.go). It calls Format and
// writes the body to stdout (matching the historical hook ABI: JSON
// on stdout, exit 2). stderr is unused for claude-code.
func FormatWriter(d gov.Decision, stdout io.Writer, _ io.Writer) int {
	body, code := Format(d)
	if len(body) > 0 {
		_, _ = stdout.Write(body)
		_, _ = stdout.Write([]byte{'\n'})
	}
	return code
}
