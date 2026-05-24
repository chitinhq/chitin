// tasks_lint.go — spec 107 `tasks-lint` operator subcommand.
//
// It compiles a spec-kit spec through the same spec-077 adapter used by
// `schedule`, then re-runs the shared closed-taxonomy keyword mapper over
// every agent node's task description. No dispatch, registry, Temporal, or
// chain side effects happen here: this is a local authoring lint.

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
	"unicode/utf8"

	"github.com/chitinhq/chitin/go/orchestrator/adapter"
	"github.com/chitinhq/chitin/go/orchestrator/adapter/speckit"
	"github.com/chitinhq/chitin/go/orchestrator/dag"
)

// TasksLintRow is the per-task classification record emitted by tasks-lint.
type TasksLintRow struct {
	TaskID             string  `json:"task_id"`
	Capability         *string `json:"capability"`
	DescriptionExcerpt string  `json:"description_excerpt"`
	Classified         bool    `json:"classified"`
}

// cmdTasksLint is the entrypoint dispatched from runMain.
func cmdTasksLint(args []string) int {
	return runTasksLint(context.Background(), args, os.Stdout, os.Stderr)
}

func runTasksLint(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	_ = ctx

	fs := flag.NewFlagSet("tasks-lint", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "emit JSON instead of a human-readable table")
	repoRoot := fs.String("repo-root", "", "Chitin repo root (default: $CHITIN_REPO_ROOT or `git rev-parse --show-toplevel`)")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "usage: chitin-orchestrator tasks-lint <spec-ref> [--json] [--repo-root path]")
	}

	if err := fs.Parse(normalizeTasksLintArgs(args)); err != nil {
		return exitUserError
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "error: exactly one positional argument required: <spec-ref>")
		fs.Usage()
		return exitUserError
	}
	specRefArg := fs.Arg(0)

	repo, err := resolveRepoRoot(*repoRoot)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return exitRuntimeError
	}

	resolution, err := resolveSpecRef(repo, specRefArg)
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

	rows := tasksLintRows(cs.DAG)
	unclassified := unclassifiedTaskIDs(rows)

	if *asJSON {
		body, _ := json.MarshalIndent(rows, "", "  ")
		fmt.Fprintln(stdout, string(body))
	} else {
		renderTasksLintTable(stdout, rows)
	}

	if len(unclassified) > 0 {
		fmt.Fprintf(stderr, "error: unclassified task(s): %s\n", strings.Join(unclassified, ", "))
		return exitUserError
	}
	return exitSuccess
}

// normalizeTasksLintArgs lets operators use the documented shape
// `tasks-lint <spec-ref> --json --repo-root path` while still delegating
// validation and help text to flag.FlagSet.
func normalizeTasksLintArgs(args []string) []string {
	flags := make([]string, 0, len(args))
	positionals := make([]string, 0, 1)
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--json":
			flags = append(flags, arg)
		case arg == "--repo-root":
			flags = append(flags, arg)
			if i+1 < len(args) {
				i++
				flags = append(flags, args[i])
			}
		case strings.HasPrefix(arg, "--repo-root="):
			flags = append(flags, arg)
		case strings.HasPrefix(arg, "-"):
			flags = append(flags, arg)
		default:
			positionals = append(positionals, arg)
		}
	}
	return append(flags, positionals...)
}

func tasksLintRows(d *dag.DAG) []TasksLintRow {
	if d == nil {
		return nil
	}
	var rows []TasksLintRow
	for _, n := range d.Nodes() {
		if n.Kind == dag.NodeKindDeterministic {
			continue
		}
		capability, ok := adapter.MapCapability(n.Description)
		row := TasksLintRow{
			TaskID:             n.TaskRef,
			DescriptionExcerpt: excerpt(n.Description, 60),
			Classified:         ok,
		}
		if ok {
			cap := string(capability)
			row.Capability = &cap
		}
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].TaskID != rows[j].TaskID {
			return rows[i].TaskID < rows[j].TaskID
		}
		return rows[i].DescriptionExcerpt < rows[j].DescriptionExcerpt
	})
	return rows
}

func unclassifiedTaskIDs(rows []TasksLintRow) []string {
	var ids []string
	for _, row := range rows {
		if !row.Classified {
			ids = append(ids, row.TaskID)
		}
	}
	return ids
}

func renderTasksLintTable(w io.Writer, rows []TasksLintRow) {
	taskWidth := len("task_id")
	capWidth := len("capability")
	for _, r := range rows {
		if len(r.TaskID) > taskWidth {
			taskWidth = len(r.TaskID)
		}
		capability := tasksLintCapabilityText(r)
		if len(capability) > capWidth {
			capWidth = len(capability)
		}
	}

	fmt.Fprintf(w, "%-*s  %-*s  description_excerpt\n", taskWidth, "task_id", capWidth, "capability")
	fmt.Fprintf(w, "%s\n", repeatRune('-', taskWidth+2+capWidth+2+len("description_excerpt")))
	for _, r := range rows {
		fmt.Fprintf(w, "%-*s  %-*s  %s\n", taskWidth, r.TaskID, capWidth, tasksLintCapabilityText(r), r.DescriptionExcerpt)
	}
}

func tasksLintCapabilityText(row TasksLintRow) string {
	if row.Capability == nil {
		return "unclassified"
	}
	return *row.Capability
}

func excerpt(s string, maxRunes int) string {
	if maxRunes <= 0 || s == "" {
		return ""
	}
	if utf8.RuneCountInString(s) <= maxRunes {
		return s
	}
	var b strings.Builder
	count := 0
	for _, r := range s {
		if count >= maxRunes {
			break
		}
		b.WriteRune(r)
		count++
	}
	return b.String()
}
