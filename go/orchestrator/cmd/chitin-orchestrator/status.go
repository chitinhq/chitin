// status.go — `chitin-orchestrator status [-run-id <id>]` subcommand
// (spec 097 US2; FRs 001, 006, 007, 010, 011).
//
// Two modes:
//
//   - List mode (no -run-id flag): query Temporal for every running
//     SchedulerWorkflow execution, fetch each one's status via the
//     spec-076 "status" query handler, and emit a sorted JSON array.
//   - Inspect mode (-run-id <id>): query one execution's status and
//     emit the raw SchedulerStatus JSON.
//
// status is strictly read-only — no chain event is emitted, no Temporal
// state is mutated. See contracts/status-subcommand.md for the contract.

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"go.temporal.io/api/workflowservice/v1"
	"go.temporal.io/sdk/client"

	"github.com/chitinhq/chitin/go/orchestrator/workflows"
)

// statusListEntry is one row in the list-mode JSON output.
// Field order matches data-model.md Entity 5.
type statusListEntry struct {
	RunID        string `json:"run_id"`
	SpecRef      string `json:"spec_ref"`
	Tick         int    `json:"tick"`
	FrontierSize int    `json:"frontier_size"`
	StartedAt    string `json:"started_at"`
}

func cmdStatus(args []string) int {
	return runStatus(context.Background(), args, os.Stdout, os.Stderr)
}

func runStatus(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	fs.SetOutput(stderr)
	runID := fs.String("run-id", "", "inspect a single run by its Temporal WorkflowID")
	textMode := fs.Bool("text", false, "render as a fixed-column table instead of JSON")
	temporalHost := fs.String("temporal-host", "", "Temporal frontend host:port")

	fs.Usage = func() {
		fmt.Fprintln(stderr, "usage: chitin-orchestrator status [-run-id <id>] [--text] [--temporal-host host:port]")
	}

	if err := fs.Parse(args); err != nil {
		return exitUserError
	}
	if fs.NArg() > 0 {
		fmt.Fprintln(stderr, "error: status takes no positional arguments")
		fs.Usage()
		return exitUserError
	}

	c, host, err := dialTemporal(ctx, *temporalHost)
	if err != nil {
		fmt.Fprintf(stderr, "error: Temporal unreachable at %s — is the temporal-dev service running?\n", host)
		return exitRuntimeError
	}
	defer c.Close()

	if *runID == "" {
		return runStatusList(ctx, c, *textMode, stdout, stderr)
	}
	return runStatusInspect(ctx, c, *runID, *textMode, stdout, stderr)
}

func runStatusList(ctx context.Context, c client.Client, textMode bool, stdout, stderr io.Writer) int {
	// Filter on running SchedulerWorkflow executions only.
	const queryStr = `WorkflowType = "SchedulerWorkflow" AND ExecutionStatus = "Running"`
	req := &workflowservice.ListWorkflowExecutionsRequest{Query: queryStr}
	resp, err := c.ListWorkflow(ctx, req)
	if err != nil {
		fmt.Fprintf(stderr, "error: list workflows failed: %v\n", err)
		return exitRuntimeError
	}

	entries := make([]statusListEntry, 0, len(resp.Executions))
	for _, ex := range resp.Executions {
		wfID := ex.Execution.GetWorkflowId()
		entry := statusListEntry{
			RunID:     wfID,
			StartedAt: ex.StartTime.AsTime().UTC().Format("2006-01-02T15:04:05Z"),
		}
		// Fetch live SchedulerStatus via the spec-076 "status" query handler.
		// Failure here is tolerated — we still emit the row, just without
		// the per-tick details.
		val, qerr := c.QueryWorkflow(ctx, wfID, "", workflows.StatusQueryName)
		if qerr == nil && val != nil {
			var st workflows.SchedulerStatus
			if derr := val.Get(&st); derr == nil {
				entry.Tick = st.Tick
				entry.FrontierSize = len(st.Frontier)
			}
		}
		entries = append(entries, entry)
	}
	// Sort by StartedAt descending (most recent first), RunID as tie-breaker.
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].StartedAt != entries[j].StartedAt {
			return entries[i].StartedAt > entries[j].StartedAt
		}
		return entries[i].RunID < entries[j].RunID
	})

	if textMode {
		fmt.Fprintf(stdout, "%-40s  %-6s  %-8s  %s\n", "RUN_ID", "TICK", "FRONTIER", "STARTED_AT")
		for _, e := range entries {
			fmt.Fprintf(stdout, "%-40s  %-6d  %-8d  %s\n", trunc(e.RunID, 40), e.Tick, e.FrontierSize, e.StartedAt)
		}
		return exitSuccess
	}
	enc := json.NewEncoder(stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(entries); err != nil {
		fmt.Fprintf(stderr, "error: json encode: %v\n", err)
		return exitRuntimeError
	}
	return exitSuccess
}

func runStatusInspect(ctx context.Context, c client.Client, runID string, textMode bool, stdout, stderr io.Writer) int {
	val, err := c.QueryWorkflow(ctx, runID, "", workflows.StatusQueryName)
	if err != nil {
		// not-found is a user error; transport errors are runtime.
		msg := err.Error()
		if strings.Contains(msg, "not found") || strings.Contains(msg, "NotFound") {
			fmt.Fprintf(stderr, "error: no scheduler run with run_id %q\n", runID)
			return exitUserError
		}
		fmt.Fprintf(stderr, "error: scheduler query failed: %v\n", err)
		return exitRuntimeError
	}
	var st workflows.SchedulerStatus
	if err := val.Get(&st); err != nil {
		fmt.Fprintf(stderr, "error: decoding status payload: %v\n", err)
		return exitRuntimeError
	}

	if textMode {
		fmt.Fprintf(stdout, "RUN_ID=%s  TICK=%d  FRONTIER=%d  STALLED=%v  COMPLETE=%v\n",
			st.RunID, st.Tick, len(st.Frontier), st.Stalled, st.Complete)
		fmt.Fprintln(stdout, "NODE_STATUS:")
		ids := make([]string, 0, len(st.NodeStatus))
		for id := range st.NodeStatus {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			fmt.Fprintf(stdout, "  %-30s  %s\n", id, st.NodeStatus[id])
		}
		return exitSuccess
	}
	enc := json.NewEncoder(stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(st); err != nil {
		fmt.Fprintf(stderr, "error: json encode: %v\n", err)
		return exitRuntimeError
	}
	return exitSuccess
}

// trunc returns s truncated to n runes with a trailing ellipsis if it
// was too long. Used for fixed-column text rendering.
func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n <= 1 {
		return "…"
	}
	return s[:n-1] + "…"
}
