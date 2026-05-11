// Package driver dispatches per-CLI hook response formatting.
//
// Claude Code's hook ABI: exit 2 + JSON {"decision":"block","reason":...}
// on STDOUT. The model reads stdout for the block signal.
//
// Codex's hook ABI (discovered 2026-05-11 session
// 019e1849-5b78-7e90-9181-691cccd314e6): exit 2 + human-readable reason
// on STDERR. When stderr is empty on an exit-2 hook, codex surfaces
// "PreToolUse hook exited with code 2 but did not write a blocking
// reason to stderr" and proceeds with the call — i.e., the deny is
// observed but not enforced.
//
// We emit BOTH stdout JSON and stderr text for codex/gemini so the
// chain ledger entry shape stays uniform regardless of which CLI
// fired, while each CLI's native hook ABI sees the signal it expects.
package driver

import (
	"io"

	"github.com/chitinhq/chitin/go/execution-kernel/internal/driver/claudecode"
	"github.com/chitinhq/chitin/go/execution-kernel/internal/driver/codex"
	"github.com/chitinhq/chitin/go/execution-kernel/internal/driver/gemini"
	"github.com/chitinhq/chitin/go/execution-kernel/internal/gov"
)

// Formatter writes the hook response for a single Decision. Returns
// the exit code the hook should terminate with. Writers correspond to
// the caller's os.Stdout / os.Stderr; both are non-nil.
type Formatter func(d gov.Decision, stdout io.Writer, stderr io.Writer) int

// FormatFor returns the Formatter for the named agent surface. Unknown
// agents fall back to claude-code's shape (stdout-only) — the
// historical default before per-driver dispatch existed.
func FormatFor(agent string) Formatter {
	switch agent {
	case "codex":
		return codex.Format
	case "gemini":
		return gemini.Format
	default:
		return claudecode.FormatWriter
	}
}
