// session.go — `chitin-kernel session <sub> ...` subcommand router
// (spec 096; FRs 001-003).
//
// Three sub-subcommands:
//
//   session unlock -agent <id> [-reason <text>]
//   session lock   -agent <id> [-reason <text>]
//   session status [-agent <id>] [--text]
//
// Handlers live in session_unlock.go, session_lock.go, session_status.go.
// Chain emission for the state-mutating subcommands flows through the
// same canonical emit.Emitter the kernel's `emit` subcommand uses, via
// the shared emitSessionEvent helper in session_emit.go.

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/chitinhq/chitin/go/execution-kernel/internal/chain"
	"github.com/chitinhq/chitin/go/execution-kernel/internal/emit"
	"github.com/chitinhq/chitin/go/execution-kernel/internal/event"
	"github.com/chitinhq/chitin/go/execution-kernel/internal/kstate"
)

func cmdSession(args []string) {
	if len(args) == 0 {
		exitErr("missing_subcommand", "usage: chitin-kernel session <unlock|lock|status> [flags]")
	}
	sub := args[0]
	rest := args[1:]
	switch sub {
	case "unlock":
		cmdSessionUnlock(rest)
	case "lock":
		cmdSessionLock(rest)
	case "status":
		cmdSessionStatus(rest)
	default:
		exitErr("unknown_subcommand", fmt.Sprintf("session: unknown subcommand %q; expected unlock|lock|status", sub))
	}
}

// defaultGovDB returns the canonical gov.db path under ~/.chitin/, used
// as the default for --db-path. Mirrors the convention the rest of
// chitin-kernel uses (the `--dir` flag defaults to ".chitin" but gov
// state has its own file alongside; the cleanest default is the
// operator's $HOME/.chitin/gov.db).
func defaultGovDB() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".chitin/gov.db"
	}
	return filepath.Join(home, ".chitin", "gov.db")
}

// defaultChitinDir returns ~/.chitin (the chain state dir). Used by
// emitSessionEvent so the chain write lands alongside the operator's
// canonical state.
func defaultChitinDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".chitin"
	}
	return filepath.Join(home, ".chitin")
}

// emitSessionEvent writes one v2 Event to the chain via the kernel's own
// emit.Emitter (same code path `chitin-kernel emit` uses). Fail-soft
// per spec 096 D9 — any error is logged to stderr but does not propagate
// to the caller, because the gov.db state mutation already succeeded
// and the lock/unlock IS the load-bearing operation.
//
// Returns nil unconditionally; the caller treats this as best-effort.
func emitSessionEvent(chitinDir string, ev *event.Event) {
	// Lazy initialization: kstate.Init + chain index open. These are
	// the same setup steps cmdEmit performs.
	absDir, err := filepath.Abs(chitinDir)
	if err != nil {
		warnSession("chain emit failed: abs path: %v — %s succeeded; the audit chain lost this entry", err, ev.EventType)
		return
	}
	if err := kstate.Init(absDir, false); err != nil {
		warnSession("chain emit failed: kstate init: %v — %s succeeded; the audit chain lost this entry", err, ev.EventType)
		return
	}
	idx, err := chain.OpenIndex(filepath.Join(absDir, "chain_index.sqlite"))
	if err != nil {
		warnSession("chain emit failed: open chain index: %v — %s succeeded; the audit chain lost this entry", err, ev.EventType)
		return
	}
	defer idx.Close()
	if err := idx.RebuildFromJSONL(absDir); err != nil {
		warnSession("chain emit failed: rebuild index: %v — %s succeeded; the audit chain lost this entry", err, ev.EventType)
		return
	}
	em := emit.Emitter{
		LogPath: filepath.Join(absDir, fmt.Sprintf("events-%s.jsonl", ev.RunID)),
		Index:   idx,
	}
	em.EnableOTELFromEnv()
	if err := em.Emit(ev); err != nil {
		warnSession("chain emit failed: emit: %v — %s succeeded; the audit chain lost this entry", err, ev.EventType)
		return
	}
}

func warnSession(format string, args ...any) {
	fmt.Fprintln(os.Stderr, "warning: "+fmt.Sprintf(format, args...))
}

// validAgentName matches a strict allowlist for agent identifiers:
// alphanumeric, dash, underscore, dot. This is enforced by the CLI
// subcommands before any agent name is used as a filename component
// (sessionRunID flows into events-session-<agent>.jsonl). Without this
// guard, an operator passing `-agent "../../tmp/oops"` would create
// chain event files in unexpected subdirectories of ~/.chitin/ — not
// an external-attack vector but a sharp edge worth filing down.
var validAgentName = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// validateAgentName returns nil if agent is a well-formed identifier,
// or an error suitable for exitErr with kind="invalid_agent".
func validateAgentName(agent string) error {
	if agent == "" {
		return fmt.Errorf("agent name is required")
	}
	if !validAgentName.MatchString(agent) {
		return fmt.Errorf("agent name %q contains characters outside the allowlist [A-Za-z0-9._-]", agent)
	}
	if len(agent) > 128 {
		return fmt.Errorf("agent name longer than 128 chars")
	}
	return nil
}

// payloadJSON marshals an arbitrary payload to JSON for embedding in a
// v2 Event's Payload field. On marshal failure (essentially impossible
// for the simple shapes session events use) returns the empty JSON
// object so the chain entry is still well-formed.
func payloadJSON(payload any) json.RawMessage {
	b, err := json.Marshal(payload)
	if err != nil {
		return json.RawMessage("{}")
	}
	return b
}
