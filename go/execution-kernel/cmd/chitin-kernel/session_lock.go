// session_lock.go — `chitin-kernel session lock` handler
// (spec 096 US1; FRs 001, 003, 005, 006, 007, 009).
//
// Operator kill-switch CLI. Wraps Counter.OperatorLock and emits a
// session_locked chain event with source="operator_cli" so the chain
// trail distinguishes operator-initiated locks from auto-escalation.

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/chitinhq/chitin/go/execution-kernel/internal/event"
	"github.com/chitinhq/chitin/go/execution-kernel/internal/gov"
)

func cmdSessionLock(args []string) {
	fs := flag.NewFlagSet("session lock", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	agent := fs.String("agent", "", "agent name (created if it does not yet exist)")
	reason := fs.String("reason", "", "operator-supplied reason; carried into the session_locked chain event")
	dbPath := fs.String("db-path", defaultGovDB(), "path to gov.db")
	dir := fs.String("dir", defaultChitinDir(), "path to .chitin state dir (for chain emit)")

	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: chitin-kernel session lock -agent <id> [-reason <text>] [--db-path <path>]")
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

	res, err := c.OperatorLock(*agent)
	if err != nil {
		exitErr("lock_failed", err.Error())
	}

	payload := map[string]any{
		"agent":            *agent,
		"lock_epoch_after": res.LockEpochAfter,
		"source":           "operator_cli",
		"reason":           *reason,
	}
	ev := &event.Event{
		SchemaVersion:   "2",
		RunID:           sessionRunID(*agent),
		SessionID:       sessionRunID(*agent),
		Surface:         "chitin-kernel-session",
		AgentInstanceID: fmt.Sprintf("chitin-kernel-cli-%d", os.Getpid()),
		EventType:       "session_locked",
		ChainID:         "",
		ChainType:       "operator-cli",
		Ts:              time.Now().UTC().Format(time.RFC3339Nano),
		Payload:         payloadJSON(payload),
	}
	emitSessionEvent(*dir, ev)

	out := map[string]any{
		"ok":               true,
		"agent":            *agent,
		"lock_epoch_after": res.LockEpochAfter,
		"reason":           *reason,
	}
	b, _ := json.Marshal(out)
	fmt.Println(string(b))
}
