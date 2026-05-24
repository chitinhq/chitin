package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/chitinhq/chitin/go/orchestrator/adapter"
	"github.com/chitinhq/chitin/go/orchestrator/adapter/speckit"
	"github.com/chitinhq/chitin/go/orchestrator/dag"
)

type taskLintRow struct {
	TaskID             string  `json:"task_id"`
	Capability         *string `json:"capability"`
	DescriptionExcerpt string  `json:"description_excerpt"`
	Classified         bool    `json:"classified"`
}

func cmdTasksLint(args []string) int {
	return runTasksLint(context.Background(), args, os.Stdout, os.Stderr)
}

func runTasksLint(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	_ = ctx

	fs := flag.NewFlagSet("tasks-lint", flag.ContinueOnError)
	fs.SetOutput(stderr)
	jsonOut := fs.Bool("json", false, "emit JSON array instead of a table")
	repoRoot := fs.String("repo-root", "", "Chitin repo root (default: $CHITIN_REPO_ROOT or `git rev-parse --show-toplevel`)")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "usage: chitin-orchestrator tasks-lint <spec-ref> [--json] [--repo-root path]")
	}

	if err := fs.Parse(args); err != nil {
		return exitUserError
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "error: exactly one positional argument required: <spec-ref>")
		fs.Usage()
		return exitUserError
	}

	repo, err := resolveRepoRoot(*repoRoot)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return exitRuntimeError
	}

	resolution, err := resolveSpecRef(repo, fs.Arg(0))
	if err != nil {
		var sre *SpecRefError
		if errors.As(err, &sre) {
			renderSpecRefError(stderr, sre)
			return exitUserError
		}
		fmt.Fprintf(stderr, "error: %v\n", err)
		return exitRuntimeError
	}

	cs, err := speckit.New().CompileSpec(repo, resolution.SpecRef)
	if err != nil {
		fmt.Fprintf(stderr, "error: spec %s compile failed: %v\n", resolution.SpecRef, err)
		return exitUserError
	}
	if cs == nil || cs.DAG == nil {
		fmt.Fprintf(stderr, "error: spec %s compiled to nil DAG\n", resolution.SpecRef)
		return exitUserError
	}

	rows := taskLintRows(cs.DAG)
	if *jsonOut {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(rows); err != nil {
			fmt.Fprintf(stderr, "error: cannot encode tasks-lint JSON: %v\n", err)
			return exitRuntimeError
		}
	} else {
		fmt.Fprintln(stdout, "task_id | capability | description_excerpt")
		for _, row := range rows {
			capability := "unclassified"
			if row.Capability != nil {
				capability = *row.Capability
			}
			fmt.Fprintf(stdout, "%s | %s | %s\n", row.TaskID, capability, row.DescriptionExcerpt)
		}
	}

	var unclassified []string
	for _, row := range rows {
		if !row.Classified {
			unclassified = append(unclassified, row.TaskID)
		}
	}
	if len(unclassified) > 0 {
		fmt.Fprintf(stderr, "error: unclassified task(s): %s\n", strings.Join(unclassified, ", "))
		return exitUserError
	}
	return exitSuccess
}

func taskLintRows(d *dag.DAG) []taskLintRow {
	var rows []taskLintRow
	if d == nil {
		return rows
	}
	for _, n := range d.Nodes() {
		if n.Kind == dag.NodeKindDeterministic {
			continue
		}
		taskID := n.TaskRef
		if taskID == "" {
			taskID = n.ID
		}
		row := taskLintRow{
			TaskID:             taskID,
			DescriptionExcerpt: excerpt(n.Description, 60),
		}
		if n.Capability != "" && n.Capability != adapter.NeedsClarification {
			capability := string(n.Capability)
			row.Capability = &capability
			row.Classified = true
		}
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool {
		return rows[i].TaskID < rows[j].TaskID
	})
	return rows
}

func excerpt(s string, limit int) string {
	s = strings.TrimSpace(s)
	if limit <= 0 || len(s) <= limit {
		return s
	}
	return s[:limit]
}
