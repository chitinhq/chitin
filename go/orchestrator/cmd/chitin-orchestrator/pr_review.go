// pr_review.go — `chitin-orchestrator pr-review <PR#>` subcommand
// (spec 094 dialectic gate; this PR closes the CLI-entrypoint gap named
// in observation 7132 / PR #955's body).
//
// Flow:
//
//  1. Parse argv via Go's flag package, scoped to this subcommand.
//  2. Resolve --repo + --author (flag → gh auto-detection from PR#).
//  3. Validate policy class + arbiter type against the spec 094 enums.
//  4. Dial Temporal (client.go).
//  5. ExecuteWorkflow(PRReviewWorkflow, PRReviewInput{...}) with a
//     fresh UUID as the Temporal WorkflowID.
//  6. Print success line to stdout (WorkflowID + dispatch details);
//     exit 0.

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"go.temporal.io/sdk/client"

	"github.com/chitinhq/chitin/go/orchestrator/activities/review"
	"github.com/chitinhq/chitin/go/orchestrator/workflows"
)

// validPolicyClasses is the closed set of class values spec 093 defines.
// The CLI rejects unknown values before dispatch so a typo doesn't end
// up in the workflow's PolicyClass field and skew telemetry attribution
// downstream.
var validPolicyClasses = map[string]bool{
	"governance":    true,
	"spec-only":     true,
	"impl":          true,
	"live-fix":      true,
	"bookkeeping":   true,
	"research-docs": true,
}

// ghResolver abstracts the gh CLI for repo/author auto-detection so
// tests inject a fake without spawning the real CLI.
type ghResolver interface {
	resolveRepoAndAuthor(ctx context.Context, prNumber int) (repo, author string, err error)
}

type defaultGhResolver struct{}

// resolveRepoAndAuthor runs `gh pr view <PR> --json url,author` and
// parses the response. Errors surface to the caller as user-facing
// "could not look up PR" messages.
func (defaultGhResolver) resolveRepoAndAuthor(ctx context.Context, prNumber int) (string, string, error) {
	cmd := exec.CommandContext(ctx, "gh", "pr", "view", strconv.Itoa(prNumber), "--json", "url,author")
	out, err := cmd.Output()
	if err != nil {
		var stderr string
		if ee, ok := err.(*exec.ExitError); ok {
			stderr = strings.TrimSpace(string(ee.Stderr))
		}
		if stderr != "" {
			return "", "", fmt.Errorf("gh pr view %d: %w; stderr=%s", prNumber, err, stderr)
		}
		return "", "", fmt.Errorf("gh pr view %d: %w", prNumber, err)
	}
	var resp struct {
		URL    string `json:"url"`
		Author struct {
			Login string `json:"login"`
		} `json:"author"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		return "", "", fmt.Errorf("gh pr view %d: parse JSON: %w", prNumber, err)
	}
	repo := repoFromURL(resp.URL)
	if repo == "" {
		return "", "", fmt.Errorf("gh pr view %d: could not extract owner/repo from URL %q", prNumber, resp.URL)
	}
	return repo, resp.Author.Login, nil
}

// repoFromURL extracts the owner/repo slug from a GitHub PR URL.
// Example: https://github.com/chitinhq/chitin/pull/953 → chitinhq/chitin.
// Empty string on unparseable input.
func repoFromURL(url string) string {
	const prefix = "https://github.com/"
	if !strings.HasPrefix(url, prefix) {
		return ""
	}
	rest := strings.TrimPrefix(url, prefix)
	// rest = "<owner>/<repo>/pull/<n>" — first two segments are the slug.
	parts := strings.SplitN(rest, "/", 3)
	if len(parts) < 2 {
		return ""
	}
	return parts[0] + "/" + parts[1]
}

// cmdPRReview is the entrypoint dispatched from runMain.
func cmdPRReview(args []string) int {
	return runPRReview(context.Background(), args, os.Stdout, os.Stderr, defaultGhResolver{})
}

// runPRReview is the testable form. Returns the exit code.
func runPRReview(ctx context.Context, args []string, stdout, stderr io.Writer, gh ghResolver) int {
	fs := flag.NewFlagSet("pr-review", flag.ContinueOnError)
	fs.SetOutput(stderr)
	repo := fs.String("repo", "", "GitHub repo slug owner/name (default: auto-detect via `gh pr view`)")
	author := fs.String("author", "", "GitHub author login for FR-005 exclusion (default: auto-detect via `gh pr view`)")
	policyClass := fs.String("policy-class", "impl", "spec 093 policy class: governance|spec-only|impl|live-fix|bookkeeping|research-docs")
	arbiter := fs.String("arbiter", "machine", "arbiter type for disagreement: machine|operator")
	temporalHost := fs.String("temporal-host", "", "Temporal frontend host:port (default: $TEMPORAL_HOSTPORT or 127.0.0.1:7233)")

	fs.Usage = func() {
		fmt.Fprintln(stderr, "usage: chitin-orchestrator pr-review <PR#> [--repo owner/name] [--author login] [--policy-class CLASS] [--arbiter machine|operator] [--temporal-host host:port]")
	}

	if err := fs.Parse(args); err != nil {
		return exitUserError
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "error: exactly one positional argument required: <PR#>")
		fs.Usage()
		return exitUserError
	}
	prNumber, err := strconv.Atoi(fs.Arg(0))
	if err != nil || prNumber <= 0 {
		fmt.Fprintf(stderr, "error: PR# must be a positive integer (got %q)\n", fs.Arg(0))
		return exitUserError
	}

	if !validPolicyClasses[*policyClass] {
		fmt.Fprintf(stderr, "error: --policy-class %q not in {governance, spec-only, impl, live-fix, bookkeeping, research-docs}\n", *policyClass)
		return exitUserError
	}
	arb := review.ArbiterType(*arbiter)
	if !arb.Valid() {
		fmt.Fprintf(stderr, "error: --arbiter %q must be one of {machine, operator}\n", *arbiter)
		return exitUserError
	}
	// Guard against an operator-UX trap: the workflow's arbiter dispatch
	// for ArbiterOperator is stubbed at workflows/pr_review.go:267-271
	// with a Phase 4 TODO — it halts with "operator arbiter dispatch
	// not wired in Phase 2 foundational (US2 follow-up)". The CLI used
	// to accept --arbiter operator and dispatch anyway, leaving the
	// operator with a halted workflow and a confusing reason. Reject
	// here until the R-OPSURF GitHub-PR-comment surface ships.
	if arb == review.ArbiterOperator {
		fmt.Fprintln(stderr, "error: --arbiter operator is not yet wired (R-OPSURF Phase 4 follow-up).")
		fmt.Fprintln(stderr, "       Use --arbiter machine until the operator-arbiter PR-comment surface ships.")
		return exitUserError
	}

	// Auto-detect repo + author from gh when not provided. The CLI's
	// primary UX is `pr-review <PR#>` with no other flags; auto-detection
	// keeps that lean while letting CI surfaces override either field
	// when they know it ahead of time.
	resolvedRepo := *repo
	resolvedAuthor := *author
	if resolvedRepo == "" || resolvedAuthor == "" {
		ghRepo, ghAuthor, err := gh.resolveRepoAndAuthor(ctx, prNumber)
		if err != nil {
			fmt.Fprintf(stderr, "error: could not look up PR #%d via gh: %v\n", prNumber, err)
			return exitRuntimeError
		}
		if resolvedRepo == "" {
			resolvedRepo = ghRepo
		}
		if resolvedAuthor == "" {
			resolvedAuthor = ghAuthor
		}
	}
	if resolvedRepo == "" {
		fmt.Fprintln(stderr, "error: --repo could not be auto-detected; pass it explicitly")
		return exitUserError
	}

	c, host, err := dialTemporal(ctx, *temporalHost)
	if err != nil {
		fmt.Fprintf(stderr, "error: Temporal unreachable at %s — is the temporal-dev service running?\n", host)
		return exitRuntimeError
	}
	defer c.Close()

	workflowID := uuid.NewString()
	in := review.PRReviewInput{
		Repo:        resolvedRepo,
		PRNumber:    prNumber,
		PRAuthor:    resolvedAuthor,
		PolicyClass: *policyClass,
		ArbiterType: arb,
	}
	startOpts := client.StartWorkflowOptions{
		ID:        workflowID,
		TaskQueue: TaskQueue,
	}
	if _, err := c.ExecuteWorkflow(ctx, startOpts, workflows.PRReviewWorkflow, in); err != nil {
		fmt.Fprintf(stderr, "error: ExecuteWorkflow PRReviewWorkflow failed: %v\n", err)
		return exitRuntimeError
	}

	fmt.Fprintf(stdout, "dispatched PR review for %s#%d (author=%s, class=%s, arbiter=%s); workflow_id=%s\n",
		resolvedRepo, prNumber, resolvedAuthor, *policyClass, arb, workflowID)
	return exitSuccess
}
