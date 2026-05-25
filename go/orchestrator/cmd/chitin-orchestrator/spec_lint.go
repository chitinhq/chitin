// spec_lint.go — spec 115 `spec-lint` operator subcommand (FR-003).
//
// Reads spec.md + tasks.md from <spec-dir>, runs the deterministic L01..L07
// rules registered with the internal/speclint package, and emits a JSON
// array of {rule, file, line, severity, message} on stdout. The subcommand
// has named exit codes:
//
//	0 — clean (no violations)
//	2 — warnings only (no errors)
//	3 — errors present
//
// Bad invocation (missing arg, non-existent dir) returns 1 (exitUserError)
// — the standard user-error code shared with the rest of the orchestrator
// CLI. Marshal failures return exitRuntimeError (also 2 in the rest of the
// CLI; deliberately distinct from "warnings" here — marshal failure prints
// to stderr, while "warnings" prints a JSON array to stdout, so the two
// cases are unambiguous despite the shared numeric value).
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

// spec-lint-specific exit codes. Per spec 115 T002 task spec:
// 0 = clean, 2 = warnings, 3 = errors. exitUserError (1) is shared with the
// rest of the CLI for bad invocation.
const (
	specLintExitClean    = 0
	specLintExitWarnings = 2
	specLintExitErrors   = 3
)

func cmdSpecLint(args []string) int {
	return runSpecLint(context.Background(), args, os.Stdout, os.Stderr)
}

func runSpecLint(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	_ = ctx
	fs := flag.NewFlagSet("spec-lint", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprintln(stderr, "usage: chitin-orchestrator spec-lint <spec-dir>")
	}
	if err := fs.Parse(args); err != nil {
		return exitUserError
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "error: exactly one positional argument required: <spec-dir>")
		fs.Usage()
		return exitUserError
	}

	s, err := speclint.Load(fs.Arg(0))
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return exitUserError
	}

	violations := speclint.Run(s)
	// Always emit a JSON array — initialize so a clean run prints "[]"
	// rather than encoding/json's nil-slice "null" rendering. Downstream
	// parsers (PostLintViolations T010) can read stdout uniformly.
	if violations == nil {
		violations = []speclint.Violation{}
	}
	body, err := json.MarshalIndent(violations, "", "  ")
	if err != nil {
		fmt.Fprintf(stderr, "error: marshaling violations: %v\n", err)
		return exitRuntimeError
	}
	fmt.Fprintln(stdout, string(body))

	return specLintExitCode(violations)
}

// specLintExitCode maps a violation set to its named exit code per FR-003.
// Clean (0) when empty, errors (3) when any error severity is present, else
// warnings (2). Extracted so the mapping is unit-testable without depending
// on the global rule registry.
func specLintExitCode(violations []speclint.Violation) int {
	if len(violations) == 0 {
		return specLintExitClean
	}
	for _, v := range violations {
		if v.Severity == speclint.SeverityError {
			return specLintExitErrors
		}
	}
	return specLintExitWarnings
}
