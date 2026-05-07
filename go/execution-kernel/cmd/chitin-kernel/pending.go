package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
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
