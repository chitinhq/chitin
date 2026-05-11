// Package gemini formats hook responses for gemini CLI's BeforeTool ABI.
//
// Gemini's hook ABI is byte-identical to Claude Code's (per gemini
// CLI's `hooks migrate` command and the install-gemini-hook.sh
// comment). Empirically it accepts stdout JSON OR stderr text; we
// emit both for symmetry with codex so the hook payload shape is
// uniform across CLIs, and so any future gemini-version change to
// the ABI doesn't surface as a leak.
package gemini

import (
	"io"

	"github.com/chitinhq/chitin/go/execution-kernel/internal/driver/codex"
	"github.com/chitinhq/chitin/go/execution-kernel/internal/gov"
)

// Format defers to the codex formatter since their wire requirements
// are a superset of claude-code's (stdout JSON + stderr text). Kept as
// a thin alias so if gemini's ABI diverges from codex's in the future,
// only this file changes.
func Format(d gov.Decision, stdout io.Writer, stderr io.Writer) int {
	return codex.Format(d, stdout, stderr)
}
