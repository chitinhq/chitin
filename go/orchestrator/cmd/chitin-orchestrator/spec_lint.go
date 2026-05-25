// spec_lint.go — spec 115 FR-003 `spec-lint` operator subcommand (T002).
//
// Reads spec.md + tasks.md from <spec-dir>, runs every registered rule in
// the speclint package (L01-L07 land via T003-T009 sibling tasks), and
// emits the merged violation list as JSON on stdout. Exit codes per
// FR-003:
//
//	0 — clean (no violations)
//	1 — user error (bad args, spec dir missing, spec.md or tasks.md absent)
//	2 — warnings only (no error-severity violations)
//	3 — at least one error-severity violation
//
// Exit code 1 reuses the standard subcommand "user error" semantics from
// main.go (spec 097); 2 and 3 are spec-115-specific severity codes. The
// subcommand is pure: no Temporal dial, no kernel emit, no network.
//
// The JSON output is always emitted (even on exit 0 with `[]`) so the
// PostLintViolations activity (T010) and humans can both consume it from
// stdout uniformly.

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/chitinhq/chitin/go/orchestrator/internal/speclint"
)

// Exit codes specific to the spec-lint subcommand (FR-003). These do not
// shadow the package-level exitUserError (1) — that's still returned for
// argv / file-missing errors. 2 and 3 only fire when the lint completed
// and at least one rule produced output.
const (
	specLintExitClean    = 0
	specLintExitWarnings = 2
	specLintExitErrors   = 3
)

// cmdSpecLint is the entrypoint dispatched from runMain.
func cmdSpecLint(args []string) int {
	return runSpecLint(context.Background(), args, os.Stdout, os.Stderr)
}

func runSpecLint(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	_ = ctx

	fs := flag.NewFlagSet("spec-lint", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprintln(stderr, "usage: chitin-orchestrator spec-lint <spec-dir>")
		fmt.Fprintln(stderr, "  <spec-dir> is the directory of one spec, e.g. .specify/specs/115-spec-review-gate")
		fmt.Fprintln(stderr, "  emits JSON [{rule,file,line,severity,message}] on stdout")
		fmt.Fprintln(stderr, "  exit: 0=clean, 1=usage/IO error, 2=warnings only, 3=errors")
	}

	if err := fs.Parse(args); err != nil {
		return exitUserError
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "error: exactly one positional argument required: <spec-dir>")
		fs.Usage()
		return exitUserError
	}
	specDirArg := fs.Arg(0)

	paths, err := speclint.ResolveSpecPaths(specDirArg)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return exitUserError
	}

	violations, err := speclint.Run(paths)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return exitRuntimeError
	}

	// Always emit the JSON array, even when empty — downstream consumers
	// (PostLintViolations activity, operator tooling) can then parse stdout
	// uniformly without branching on exit code.
	if err := writeSpecLintJSON(stdout, violations); err != nil {
		fmt.Fprintf(stderr, "error: writing output: %v\n", err)
		return exitRuntimeError
	}

	return specLintExitCode(violations)
}

// writeSpecLintJSON emits violations as a JSON array with stable key
// ordering. We marshal a non-nil slice so an empty result is `[]`, not
// `null` — that distinction matters to JSON consumers that range over
// the array without nil-checking first.
func writeSpecLintJSON(w io.Writer, violations []speclint.Violation) error {
	if violations == nil {
		violations = []speclint.Violation{}
	}
	body, err := json.MarshalIndent(violations, "", "  ")
	if err != nil {
		return err
	}
	if _, err := w.Write(body); err != nil {
		return err
	}
	_, err = fmt.Fprintln(w)
	return err
}

// specLintExitCode maps the violation set to the FR-003 exit code.
func specLintExitCode(violations []speclint.Violation) int {
	switch speclint.Worst(violations) {
	case speclint.SeverityError:
		return specLintExitErrors
	case speclint.SeverityWarning, speclint.SeverityInfo:
		return specLintExitWarnings
	default:
		return specLintExitClean
	}
}
