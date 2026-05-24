// validate_driver_coverage.go — spec 105 FR-004/005: the
// `chitin-orchestrator validate-driver-coverage` audit subcommand.
//
// Surfaces the registry-vs-taxonomy gap that the autonomous-loop
// dogfood on 2026-05-24 found: a capability constant existed in
// driver/taxonomy.go but no registered driver listed it, so every
// spec containing that capability was rejected at DAG validation.
//
// This subcommand walks every entry in driver.KnownCapabilities()
// and lists the registered drivers that declare it (via the
// taxonomic DriversDeclaring method introduced in spec 105 FR-003,
// NOT the operational DriversFor that filters on Ready). Exit code
// 0 iff every capability has ≥ 1 declarer; exit 1 on any gap.

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"

	"github.com/chitinhq/chitin/go/orchestrator/driver"
)

// CoverageRow is the per-capability audit record. Returned as a slice
// from coverageRows; rendered as a table by default, or JSON when
// --json is passed.
type CoverageRow struct {
	Capability   string   `json:"capability"`
	DeclaringIDs []string `json:"declaring_drivers"`
	ReadyIDs     []string `json:"ready_drivers"`
}

// cmdValidateDriverCoverage is the entrypoint dispatched from runMain.
func cmdValidateDriverCoverage(args []string) int {
	return runValidateDriverCoverage(context.Background(), args, os.Stdout, os.Stderr)
}

func runValidateDriverCoverage(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("validate-driver-coverage", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "emit JSON instead of a human-readable table")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "usage: chitin-orchestrator validate-driver-coverage [--json]")
	}
	if err := fs.Parse(args); err != nil {
		return exitUserError
	}

	registry, err := buildRegistry("impl")
	if err != nil {
		fmt.Fprintf(stderr, "error: cannot build driver registry: %v\n", err)
		return exitRuntimeError
	}

	rows := coverageRows(ctx, registry)
	missing := 0
	for _, r := range rows {
		if len(r.DeclaringIDs) == 0 {
			missing++
		}
	}

	if *asJSON {
		body, _ := json.MarshalIndent(rows, "", "  ")
		fmt.Fprintln(stdout, string(body))
	} else {
		renderCoverageTable(stdout, rows)
		fmt.Fprintf(stdout, "\n%d capabilit(y/ies) registered; %d unimplemented\n",
			len(rows)-missing, missing)
	}

	if missing > 0 {
		fmt.Fprintf(stderr, "\nerror: %d capabilit(y/ies) have zero declaring drivers — add CapXxx to a driver's Capabilities slice (driver/<id>/driver.go)\n", missing)
		return exitUserError
	}
	return exitSuccess
}

// coverageRows builds one CoverageRow per capability in
// driver.KnownCapabilities(). Pure function; testable.
func coverageRows(ctx context.Context, registry *driver.Registry) []CoverageRow {
	caps := driver.KnownCapabilities()
	out := make([]CoverageRow, 0, len(caps))
	for _, c := range caps {
		declaring := registry.DriversDeclaring(c)
		ready := registry.DriversFor(ctx, c)
		row := CoverageRow{
			Capability:   string(c),
			DeclaringIDs: idsOf(declaring),
			ReadyIDs:     idsOf(ready),
		}
		sort.Strings(row.DeclaringIDs)
		sort.Strings(row.ReadyIDs)
		out = append(out, row)
	}
	return out
}

func idsOf(drivers []driver.AgentDriver) []string {
	if len(drivers) == 0 {
		return []string{}
	}
	out := make([]string, 0, len(drivers))
	for _, d := range drivers {
		out = append(out, d.ID())
	}
	return out
}

// renderCoverageTable writes a human-readable table to w. Column widths
// are computed from the data so the table looks neat at any taxonomy
// size.
func renderCoverageTable(w io.Writer, rows []CoverageRow) {
	capWidth := len("capability")
	for _, r := range rows {
		if len(r.Capability) > capWidth {
			capWidth = len(r.Capability)
		}
	}
	fmt.Fprintf(w, "%-*s  declaring                                  ready\n", capWidth, "capability")
	fmt.Fprintf(w, "%s\n", repeatRune('-', capWidth+2+42+5))
	for _, r := range rows {
		declaring := joinIDs(r.DeclaringIDs)
		ready := joinIDs(r.ReadyIDs)
		flag := "  "
		if len(r.DeclaringIDs) == 0 {
			flag = " !"
		}
		fmt.Fprintf(w, "%-*s%s%-42s %s\n", capWidth, r.Capability, flag, declaring, ready)
	}
}

func joinIDs(ids []string) string {
	if len(ids) == 0 {
		return "(none)"
	}
	out := ""
	for i, id := range ids {
		if i > 0 {
			out += ", "
		}
		out += id
	}
	return out
}

func repeatRune(r rune, n int) string {
	out := make([]rune, n)
	for i := range out {
		out[i] = r
	}
	return string(out)
}
