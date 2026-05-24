// copilot_dispatch.go — `chitin-orchestrator schedule --driver copilot`
// dispatch path (spec 099 US1).
//
// Slice 2: shells out to `gh issue create` to dispatch the spec to
// Copilot via GitHub issue assignment, then emits the
// `copilot_dispatched` chain event per contracts/chain-events.md
// Event 1. Slice 3+ extends factory-listen for the consumer (PR
// detection) side.

package main

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"
)

// copilotDispatchInput is the closed input shape for the Copilot branch.
// Kept small on purpose — the function is invoked only from runSchedule
// after spec validation, so all repo-resolution work has already happened.
type copilotDispatchInput struct {
	SpecRef string // resolved spec ref, e.g. "099-github-native-dispatch"
	Repo    string // GitHub repo slug, e.g. "owner/name", from --repo flag
}

// runCopilotDispatch handles the --driver copilot branch of the schedule
// subcommand. Creates a GitHub issue assigned to @copilot via the `gh`
// CLI, then emits the copilot_dispatched chain event.
//
// Exit codes per contracts/cli-driver-flag.md:
//
//	exitSuccess (0)      — issue created, chain event emitted
//	exitUserError (1)    — gh not on PATH, gh non-zero (e.g. assignee
//	                       invalid, repo inaccessible), GitHub auth error
//	exitRuntimeError (2) — only on truly unexpected failures
func runCopilotDispatch(ctx context.Context, in copilotDispatchInput, stdout, stderr io.Writer) int {
	if _, err := exec.LookPath("gh"); err != nil {
		fmt.Fprintln(stderr, "error: gh CLI not found on PATH (install: https://cli.github.com)")
		return exitUserError
	}

	title := fmt.Sprintf("Run spec %s", in.SpecRef)
	body := fmt.Sprintf(
		"Dispatched by chitin-orchestrator at %s.\n\n"+
			"Spec ref: `%s`\n\n"+
			"Please draft a PR implementing the spec's tasks.md against the default branch. "+
			"The PR will be reviewed by chitin's dialectic review workflow (spec 094) once opened. "+
			"Apply the `chitin-dispatch` label on the PR and include `Closes #ISSUE` so the orchestrator can correlate.\n",
		time.Now().UTC().Format(time.RFC3339),
		in.SpecRef,
	)

	dispatchedAt := time.Now().UTC()
	cmd := exec.CommandContext(ctx, "gh", "issue", "create",
		"--repo", in.Repo,
		"--title", title,
		"--body", body,
		"--label", "chitin-dispatch",
		"--label", "driver:copilot",
		"--assignee", "copilot",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		// gh exits non-zero on Copilot-not-assignable, repo-not-found,
		// auth failure, etc. Surface stderr/stdout verbatim — the
		// operator needs the actual message to fix.
		fmt.Fprintf(stderr, "error: gh issue create failed: %v\n%s", err, string(out))
		return exitUserError
	}

	issueURL := extractIssueURL(string(out))
	if issueURL == "" {
		fmt.Fprintf(stderr, "error: gh issue create succeeded but no issue URL in output:\n%s", string(out))
		return exitUserError
	}
	issueNumber := extractIssueNumber(issueURL)
	if issueNumber == 0 {
		fmt.Fprintf(stderr, "error: could not parse issue number from URL %q\n", issueURL)
		return exitUserError
	}

	emitCopilotDispatched(ctx, CopilotDispatchedPayload{
		Repo:         in.Repo,
		SpecRef:      in.SpecRef,
		IssueURL:     issueURL,
		IssueNumber:  issueNumber,
		DispatchedAt: dispatchedAt.Format(time.RFC3339),
	}, stderr)

	fmt.Fprintf(stdout, "copilot dispatched: %s\n  spec_ref: %s\n  issue_number: %d\n",
		issueURL, in.SpecRef, issueNumber)
	return exitSuccess
}

// extractIssueURL pulls the first https://github.com/...issues/NNN URL
// out of gh's combined output. gh issue create by default prints the
// URL on its own line.
func extractIssueURL(out string) string {
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "https://github.com/") && strings.Contains(line, "/issues/") {
			return line
		}
	}
	return ""
}

// extractIssueNumber parses the trailing integer from a GitHub issue URL.
// Returns 0 if the URL doesn't have the expected /issues/<N> shape.
func extractIssueNumber(url string) int {
	idx := strings.LastIndex(url, "/")
	if idx < 0 || idx == len(url)-1 {
		return 0
	}
	tail := url[idx+1:]
	// Strip query/fragment if any.
	if i := strings.IndexAny(tail, "?#"); i >= 0 {
		tail = tail[:i]
	}
	var n int
	for _, c := range tail {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int(c-'0')
	}
	return n
}
