// session_status.go — `chitin-kernel session status` handler
// (spec 096 US2 + US3; FRs 001, 002, 006, 007, 009).
//
// Two modes:
//   - Inspect (`-agent <id>`): JSON object for one agent.
//   - List (no -agent): JSON array sorted by agent ASCII.
//
// `--text` switches both modes to a fixed-column table. status is
// strictly read-only — no chain event is emitted, no state mutated.

package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/chitinhq/chitin/go/execution-kernel/internal/gov"
)

// statusRow is the JSON shape per data-model.md Entity 3.
type statusRow struct {
	Agent     string `json:"agent"`
	Locked    bool   `json:"locked"`
	LockedTs  string `json:"locked_ts,omitempty"`
	UnlockTs  string `json:"unlock_ts,omitempty"`
	LockEpoch int    `json:"lock_epoch"`
	Total     int    `json:"total"`
	Level     string `json:"level"`
}

func toRow(s *gov.AgentStatus) statusRow {
	return statusRow{
		Agent:     s.Agent,
		Locked:    s.Locked,
		LockedTs:  s.LockedTs,
		UnlockTs:  s.UnlockTs,
		LockEpoch: s.LockEpoch,
		Total:     s.Total,
		Level:     s.Level,
	}
}

func cmdSessionStatus(args []string) {
	fs := flag.NewFlagSet("session status", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	agent := fs.String("agent", "", "inspect a single agent (omit for list mode)")
	textMode := fs.Bool("text", false, "render as a fixed-column table instead of JSON")
	dbPath := fs.String("db-path", defaultGovDB(), "path to gov.db")

	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: chitin-kernel session status [-agent <id>] [--text] [--db-path <path>]")
	}
	if err := fs.Parse(args); err != nil {
		os.Exit(1)
	}

	// agent is optional for status (list mode uses empty); when present
	// validate it against the same allowlist as unlock/lock so a path
	// traversal attempt is rejected even though status doesn't write
	// chain events (defense in depth — keeps the input surface uniform).
	if *agent != "" {
		if err := validateAgentName(*agent); err != nil {
			fs.Usage()
			exitErr("invalid_agent", err.Error())
		}
	}

	c, err := gov.OpenCounter(*dbPath)
	if err != nil {
		exitErr("open_govdb", fmt.Sprintf("cannot open gov.db at %s: %v", *dbPath, err))
	}
	defer c.Close()

	if *agent != "" {
		runStatusInspect(c, *agent, *textMode)
		return
	}
	runStatusList(c, *textMode)
}

func runStatusInspect(c *gov.Counter, agent string, textMode bool) {
	s, err := c.Status(agent)
	if err != nil {
		if errors.Is(err, gov.ErrNoAgent) {
			exitErr("no_agent", fmt.Sprintf("no agent_state row for %q", agent))
		}
		exitErr("status_failed", err.Error())
	}
	row := toRow(s)
	if textMode {
		printStatusTableHeader()
		printStatusTableRow(row)
		return
	}
	b, err := json.MarshalIndent(row, "", "  ")
	if err != nil {
		exitErr("json_encode", err.Error())
	}
	fmt.Println(string(b))
}

func runStatusList(c *gov.Counter, textMode bool) {
	all, err := c.StatusAll()
	if err != nil {
		exitErr("status_failed", err.Error())
	}
	if textMode {
		printStatusTableHeader()
		for _, s := range all {
			printStatusTableRow(toRow(&s))
		}
		return
	}
	rows := make([]statusRow, 0, len(all))
	for _, s := range all {
		rows = append(rows, toRow(&s))
	}
	b, err := json.MarshalIndent(rows, "", "  ")
	if err != nil {
		exitErr("json_encode", err.Error())
	}
	fmt.Println(string(b))
}

func printStatusTableHeader() {
	fmt.Printf("%-14s  %-6s  %-10s  %5s  %5s  %-22s  %-22s\n",
		"AGENT", "LOCKED", "LEVEL", "TOTAL", "EPOCH", "LOCKED_TS", "UNLOCK_TS")
}

func printStatusTableRow(r statusRow) {
	dash := "-"
	lockedTs := r.LockedTs
	if lockedTs == "" {
		lockedTs = dash
	}
	unlockTs := r.UnlockTs
	if unlockTs == "" {
		unlockTs = dash
	}
	fmt.Printf("%-14s  %-6v  %-10s  %5d  %5d  %-22s  %-22s\n",
		truncate(r.Agent, 14), r.Locked, r.Level, r.Total, r.LockEpoch, lockedTs, unlockTs)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n <= 1 {
		return "…"
	}
	return s[:n-1] + "…"
}
