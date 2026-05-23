// session_unlock.go — `chitin-kernel session unlock` handler
// (spec 096 US1; FRs 001, 004, 006, 007, 008, 009).
//
// Flow per contracts/unlock-subcommand.md:
//
//  1. Parse argv (flag.NewFlagSet scoped to this subcommand).
//  2. Open gov.db (run schema migration via OpenCounter — idempotent).
//  3. Counter.Unlock(agent) — returns UnlockResult.
//  4. Emit session_unlocked chain event (fail-soft).
//  5. Print {"ok":true,...} JSON and exit 0; or exitErr on failure.

package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/chitinhq/chitin/go/execution-kernel/internal/event"
	"github.com/chitinhq/chitin/go/execution-kernel/internal/gov"
)

func cmdSessionUnlock(args []string) {
	fs := flag.NewFlagSet("session unlock", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	agent := fs.String("agent", "", "agent name (must match an existing agent_state row)")
	reason := fs.String("reason", "", "operator-supplied reason; carried into the session_unlocked chain event")
	dbPath := fs.String("db-path", defaultGovDB(), "path to gov.db")
	dir := fs.String("dir", defaultChitinDir(), "path to .chitin state dir (for chain emit)")

	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: chitin-kernel session unlock -agent <id> [-reason <text>] [--db-path <path>]")
	}
	if err := fs.Parse(args); err != nil {
		os.Exit(1)
	}
	if *agent == "" {
		fs.Usage()
		exitErr("missing_agent", "-agent is required")
	}

	c, err := gov.OpenCounter(*dbPath)
	if err != nil {
		exitErr("open_govdb", fmt.Sprintf("cannot open gov.db at %s: %v", *dbPath, err))
	}
	defer c.Close()

	res, err := c.Unlock(*agent)
	if err != nil {
		if errors.Is(err, gov.ErrNoAgent) {
			exitErr("no_agent", fmt.Sprintf("no agent_state row for %q", *agent))
		}
		exitErr("unlock_failed", err.Error())
	}

	// Build the chain event payload per contracts/chain-events.md Event 2.
	payload := map[string]any{
		"agent":             *agent,
		"lock_epoch_after":  res.LockEpochAfter,
		"reason":            *reason,
		"locked_ts_before":  res.LockedTsBefore,
		"total_at_unlock":   res.TotalAtUnlock,
	}
	ev := &event.Event{
		SchemaVersion:   "2",
		RunID:           sessionRunID(*agent), // see helper at bottom
		SessionID:       sessionRunID(*agent),
		Surface:         "chitin-kernel-session",
		AgentInstanceID: fmt.Sprintf("chitin-kernel-cli-%d", os.Getpid()),
		EventType:       "session_unlocked",
		ChainID:         "", // emitter populates if applicable
		ChainType:       "operator-cli",
		Ts:              time.Now().UTC().Format(time.RFC3339Nano),
		Payload:         payloadJSON(payload),
	}
	emitSessionEvent(*dir, ev)

	// Success line. JSON with everything an operator (or chain consumer)
	// might want to grep for.
	out := map[string]any{
		"ok":               true,
		"agent":            *agent,
		"idempotent":       res.Idempotent,
		"lock_epoch_after": res.LockEpochAfter,
		"reason":           *reason,
	}
	b, _ := json.Marshal(out)
	fmt.Println(string(b))
}

// sessionRunID generates the chain RunID used for session_locked /
// session_unlocked events. Keyed by agent so all events for one agent
// land in the same ~/.chitin/events-session-<agent>.jsonl file —
// chronological per-agent audit becomes trivial.
func sessionRunID(agent string) string {
	return "session-" + agent
}
