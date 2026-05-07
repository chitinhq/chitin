package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/chitinhq/chitin/go/execution-kernel/internal/gov"
)

// pendingList writes the unresolved pending_approvals rows to out.
// If asJSON is true, emits a JSON array; else a tab-formatted table.
func pendingList(store *gov.EscalateStore, out io.Writer, asJSON bool) error {
	rows, err := store.ListUnresolved()
	if err != nil {
		return err
	}
	if asJSON {
		simple := make([]map[string]any, len(rows))
		for i, r := range rows {
			simple[i] = map[string]any{
				"id":              r.ID,
				"agent":           r.Agent,
				"rule_id":         r.RuleID,
				"action_type":     r.ActionType,
				"action_target":   r.ActionTarget,
				"reason":          r.Reason,
				"channel":         r.Channel,
				"created_ts":      r.CreatedTs,
				"timeout_seconds": r.TimeoutSeconds,
			}
		}
		b, err := json.Marshal(simple)
		if err != nil {
			return err
		}
		_, err = out.Write(b)
		return err
	}
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tAGE\tAGENT\tRULE\tTARGET\tEXPIRES_IN")
	now := time.Now().Unix()
	for _, r := range rows {
		age := now - r.CreatedTs
		expIn := r.CreatedTs + int64(r.TimeoutSeconds) - now
		target := r.ActionTarget
		if len(target) > 60 {
			target = target[:57] + "..."
		}
		fmt.Fprintf(tw, "%s\t%ds\t%s\t%s\t%s\t%ds\n",
			r.ID, age, r.Agent, r.RuleID, target, expIn)
	}
	return tw.Flush()
}

func pendingApprove(store *gov.EscalateStore, id string, windowSeconds int) error {
	return store.ResolveApprove(id, "operator-cli", windowSeconds)
}

func pendingDeny(store *gov.EscalateStore, id string, reason string) error {
	return store.ResolveDeny(id, "operator-cli", reason)
}

// statOwnerUID + selfUID are mockable hooks. Production uses os.Stat
// and os.Geteuid; tests inject fakes.
var statOwnerUID = func(path string) (uint32, error) {
	st, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	sys, ok := st.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, fmt.Errorf("stat sys: not a Stat_t")
	}
	return sys.Uid, nil
}

var selfUID = func() uint32 { return uint32(os.Geteuid()) }

// authPendingFile returns nil if the current process's effective uid
// owns the pending_approvals.sqlite file. Otherwise returns
// "pending_unauthorized: ..." — caller should exit 2.
func authPendingFile(dbPath string) error {
	owner, err := statOwnerUID(dbPath)
	if err != nil {
		return fmt.Errorf("pending_unauthorized: stat %s: %w", dbPath, err)
	}
	self := selfUID()
	if owner != self {
		return fmt.Errorf("pending_unauthorized: file owned by uid %d, current uid %d", owner, self)
	}
	return nil
}

// cmdPending is the top-level dispatcher for `chitin-kernel pending <sub>`.
// Sub: list | approve | deny.
func cmdPending(args []string) {
	if len(args) < 1 {
		exitErr("pending_no_subcommand", "usage: chitin-kernel pending {list|approve|deny}")
	}
	sub, rest := args[0], args[1:]
	dbPath := filepath.Join(chitinDir(), "pending_approvals.sqlite")

	switch sub {
	case "list":
		asJSON := false
		for _, a := range rest {
			if a == "--json" {
				asJSON = true
			}
		}
		store, err := gov.OpenEscalateStore(dbPath)
		if err != nil {
			// File not existing yet is OK — list is just empty.
			if !os.IsNotExist(err) {
				exitErr("pending_open", err.Error())
			}
			if asJSON {
				fmt.Println("[]")
			}
			return
		}
		defer store.Close()
		if err := pendingList(store, os.Stdout, asJSON); err != nil {
			exitErr("pending_list", err.Error())
		}

	case "approve":
		if len(rest) < 1 {
			exitErr("pending_approve_missing_id", "usage: chitin-kernel pending approve <id> [--window <duration>]")
		}
		id := rest[0]
		windowSec := 0
		for i, a := range rest {
			if a == "--window" && i+1 < len(rest) {
				d, err := time.ParseDuration(rest[i+1])
				if err != nil {
					exitErr("pending_bad_window", err.Error())
				}
				windowSec = int(d.Seconds())
			}
		}
		if err := authPendingFile(dbPath); err != nil {
			fmt.Fprintln(os.Stderr, err.Error())
			os.Exit(2)
		}
		store, err := gov.OpenEscalateStore(dbPath)
		if err != nil {
			exitErr("pending_open", err.Error())
		}
		defer store.Close()
		if err := pendingApprove(store, id, windowSec); err != nil {
			exitErr("pending_approve", err.Error())
		}
		fmt.Printf(`{"ok":true,"action":"approve","id":%q,"window_seconds":%d}`+"\n", id, windowSec)

	case "deny":
		if len(rest) < 1 {
			exitErr("pending_deny_missing_id", "usage: chitin-kernel pending deny <id> [--reason <text>]")
		}
		id := rest[0]
		reason := ""
		for i, a := range rest {
			if a == "--reason" && i+1 < len(rest) {
				reason = rest[i+1]
			}
		}
		if err := authPendingFile(dbPath); err != nil {
			fmt.Fprintln(os.Stderr, err.Error())
			os.Exit(2)
		}
		store, err := gov.OpenEscalateStore(dbPath)
		if err != nil {
			exitErr("pending_open", err.Error())
		}
		defer store.Close()
		if err := pendingDeny(store, id, reason); err != nil {
			exitErr("pending_deny", err.Error())
		}
		fmt.Printf(`{"ok":true,"action":"deny","id":%q,"reason":%q}`+"\n", id, reason)

	case "watch-hermes":
		cfg, err := loadOperatorConfig()
		if err != nil {
			exitErr("operator_config_missing", err.Error())
		}
		store, err := gov.OpenEscalateStore(dbPath)
		if err != nil {
			exitErr("pending_open", err.Error())
		}
		defer store.Close()
		count, err := watchHermesOnce(store, cfg)
		if err != nil {
			exitErr("watch_hermes", err.Error())
		}
		fmt.Printf(`{"ok":true,"action":"watch-hermes","resolved":%d}`+"\n", count)

	default:
		exitErr("pending_unknown_subcommand", sub)
	}
}
