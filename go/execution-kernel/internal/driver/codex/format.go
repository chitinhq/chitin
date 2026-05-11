// Package codex formats hook responses for codex CLI's PreToolUse ABI.
//
// Unlike claude-code (which reads stdout for the block JSON), codex
// requires the human-readable reason on STDERR when exiting with the
// block code. Without stderr text, codex emits "PreToolUse hook
// exited with code 2 but did not write a blocking reason to stderr"
// and PROCEEDS WITH THE CALL — defeating the deny.
//
// We emit BOTH:
//   - stdout JSON (same shape as claude-code) so chain telemetry
//     and any future codex-side JSON-aware parsing sees a uniform record
//   - stderr text so codex's current ABI hard-blocks
package codex

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/chitinhq/chitin/go/execution-kernel/internal/driver/claudecode"
	"github.com/chitinhq/chitin/go/execution-kernel/internal/gov"
)

// Format emits the codex-shaped hook response. Returns the exit code.
func Format(d gov.Decision, stdout io.Writer, stderr io.Writer) int {
	body, code := claudecode.Format(d)
	if len(body) > 0 {
		_, _ = stdout.Write(body)
		_, _ = stdout.Write([]byte{'\n'})
	}
	if code == claudecode.ExitAllow {
		// Allow path: claudecode.Format returns (nil, ExitAllow), so no
		// stdout was written above and no stderr should be either —
		// emitting noise on allow would create spurious chain events.
		return code
	}
	// Decode the body to extract the reason field for stderr emission.
	// We trust claudecode.Format produces well-formed JSON; on any decode
	// failure, fall back to a fixed message so codex still gets a
	// non-empty stderr (the critical invariant).
	var parsed struct {
		Reason string `json:"reason"`
		RuleID string `json:"rule_id"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		fmt.Fprintln(stderr, "chitin: governance denied (block reason unavailable)")
		return code
	}
	reason := strings.TrimSpace(parsed.Reason)
	if reason == "" {
		reason = "chitin: governance denied"
	}
	if parsed.RuleID != "" && !strings.Contains(reason, parsed.RuleID) {
		reason = parsed.RuleID + ": " + reason
	}
	fmt.Fprintln(stderr, reason)
	return code
}
