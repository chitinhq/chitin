package claudecode

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/chitinhq/chitin/go/orchestrator/activities/review/verdict"
	"github.com/chitinhq/chitin/go/orchestrator/driver"
)

const (
	id      = "claudecode"
	version = "0.1.0"
)

// Driver wraps the Claude Code CLI behind the spec-075 AgentDriver contract.
type Driver struct {
	command string
}

// Option configures a Driver. It exists primarily so tests and operators can
// point the driver at a non-default binary path without changing core code.
type Option func(*Driver)

// WithCommand overrides the executable used by Ready and Invoke.
func WithCommand(command string) Option {
	return func(d *Driver) {
		if command != "" {
			d.command = command
		}
	}
}

// New returns a Claude Code AgentDriver.
func New(opts ...Option) *Driver {
	d := &Driver{command: "claude"}
	for _, opt := range opts {
		opt(d)
	}
	return d
}

// ID returns the stable driver identifier.
func (d *Driver) ID() string { return id }

// Card returns the Claude Code capability card.
func (d *Driver) Card() driver.CapabilityCard {
	return driver.CapabilityCard{
		DriverID:     id,
		Version:      version,
		AgentRuntime: "claude-code",
		Model:        "claude-code-cli",
		Capabilities: []driver.Capability{
			driver.CapCodeImplement,
			driver.CapCodeReview,
			driver.CapSpecAuthor,
			driver.CapDocsWrite,
			driver.CapTestAuthor,
		},
		Tier:      driver.TierFrontier,
		CostClass: driver.CostHigh,
		Constraints: driver.Constraints{
			QuotaBounded:     true,
			NetworkRequired:  true,
			MaxContextTokens: 200000,
			WorktreeRequired: true,
		},
	}
}

// Ready reports whether the Claude Code CLI binary is available.
func (d *Driver) Ready(ctx context.Context) (bool, string) {
	if err := ctx.Err(); err != nil {
		return false, err.Error()
	}
	if _, err := exec.LookPath(d.command); err != nil {
		return false, fmt.Sprintf("Claude Code runtime %q not found: %v", d.command, err)
	}
	return true, ""
}

// Invoke shells out to Claude Code in the work unit's dedicated worktree.
func (d *Driver) Invoke(ctx context.Context, wu driver.WorkUnit) (driver.Result, error) {
	ctx, cancel := invocationContext(ctx, wu.Deadline)
	defer cancel()

	reviewMode := isReviewModeWorkUnit(wu)
	var prompt string
	if reviewMode {
		prompt = reviewPromptFor(wu)
	} else {
		prompt = promptFor(wu)
	}
	// --dangerously-skip-permissions is mandatory for dispatch-mode
	// invocations: the chitin worker spawns claude headlessly inside a
	// fresh worktree, and without this flag claude's sandbox refuses
	// `Write` and `touch` against the worktree path (surfaced by the
	// 2026-05-24 dogfood; claudecode would author its proposed code in
	// the explanation but produce no commits → no PR). claude's own
	// help text recommends the flag "for sandboxes with no internet
	// access" — chitin's worker context matches that intent.
	cmd := exec.CommandContext(ctx, d.command, "--dangerously-skip-permissions", "-p", prompt)
	cmd.Dir = wu.WorktreePath

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	stdoutStr := strings.TrimSpace(stdout.String())
	stderrStr := strings.TrimSpace(stderr.String())
	if reviewMode {
		return reviewResultFromCommand(ctx, wu, d.ID(), stdoutStr, stderrStr, err), nil
	}
	return resultFromCommand(ctx, wu, d.ID(), stdoutStr, stderrStr, err), nil
}

// isReviewModeWorkUnit reports whether wu should be routed through the
// spec 094 review-mode codepath. The discriminator mirrors what the
// DispatchMachineReviewer activity already sets on the WorkUnit
// (SpecID="094", TaskID="review") — using the existing convention keeps
// the dispatch-side contract unchanged.
func isReviewModeWorkUnit(wu driver.WorkUnit) bool {
	return wu.SpecID == "094" && wu.TaskID == "review"
}

func invocationContext(parent context.Context, deadline time.Time) (context.Context, context.CancelFunc) {
	if deadline.IsZero() {
		return context.WithCancel(parent)
	}
	return context.WithDeadline(parent, deadline)
}

func promptFor(wu driver.WorkUnit) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Chitin work unit: %s\n", wu.ID)
	if wu.SpecID != "" {
		fmt.Fprintf(&b, "Spec: %s\n", wu.SpecID)
	}
	if wu.TaskID != "" {
		fmt.Fprintf(&b, "Task: %s\n", wu.TaskID)
	}
	if wu.WorktreePath != "" {
		fmt.Fprintf(&b, "Worktree: %s\n", wu.WorktreePath)
	}
	b.WriteString("\nInstructions:\n")
	b.WriteString(wu.Context)
	return b.String()
}

func resultFromCommand(ctx context.Context, wu driver.WorkUnit, driverID, stdout, stderr string, runErr error) driver.Result {
	res := driver.Result{WorkUnitID: wu.ID, DriverID: driverID, OutputRef: stdout}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		res.Status = driver.StatusTimeout
		res.Explanation = fmt.Sprintf("driver %q timed out running work unit %q", driverID, wu.ID)
		if stderr != "" {
			res.Explanation += ": " + stderr
		}
		return res
	}
	if runErr != nil {
		res.Status = driver.StatusFailed
		res.Explanation = fmt.Sprintf("driver %q failed running work unit %q: %v", driverID, wu.ID, runErr)
		if stderr != "" {
			res.Explanation += ": " + stderr
		}
		return res
	}
	res.Status = driver.StatusSucceeded
	res.Explanation = fmt.Sprintf("driver %q completed work unit %q", driverID, wu.ID)
	if stderr != "" {
		res.Explanation += "; stderr: " + stderr
	}
	return res
}

// reviewExplanationRawLimit caps the raw-output snippet embedded in a
// malformed_verdict explanation (spec 109 FR-004, US2 independent test).
// 1 KiB is enough to fingerprint what the model actually emitted without
// flooding the workflow history.
const reviewExplanationRawLimit = 1024

// reviewResultFromCommand turns the claude CLI's (stdout, stderr, err)
// triple from a review-mode invocation into a typed Result per
// spec 109 FR-003/FR-004/FR-005. Failure paths return
// StatusFailed with an explanation of the form
// "malformed_verdict: <detail>; raw: <first 1KiB of stdout>" so the
// dispatch activity can post-mortem without re-fetching the model's
// output. The success path canonically re-serializes the validated
// StructuredVerdict into the explanation field, which is what spec
// 094's DispatchMachineReviewer activity parses via
// verdict.ParseStructured.
func reviewResultFromCommand(ctx context.Context, wu driver.WorkUnit, driverID, stdout, stderr string, runErr error) driver.Result {
	res := driver.Result{WorkUnitID: wu.ID, DriverID: driverID, OutputRef: stdout}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		res.Status = driver.StatusTimeout
		res.Explanation = fmt.Sprintf("driver %q timed out running review-mode work unit %q", driverID, wu.ID)
		if stderr != "" {
			res.Explanation += ": " + stderr
		}
		return res
	}
	if runErr != nil {
		res.Status = driver.StatusFailed
		res.Explanation = fmt.Sprintf("driver %q failed running review-mode work unit %q: %v", driverID, wu.ID, runErr)
		if stderr != "" {
			res.Explanation += ": " + stderr
		}
		return res
	}
	if stdout == "" {
		res.Status = driver.StatusFailed
		res.Explanation = "malformed_verdict: empty output from claudecode review-mode invocation; raw: "
		return res
	}
	extracted, extractErr := extractVerdictJSON(stdout)
	if extractErr != nil {
		res.Status = driver.StatusFailed
		res.Explanation = fmt.Sprintf("malformed_verdict: %v; raw: %s", extractErr, truncateForExplanation(stdout))
		return res
	}
	var v verdict.StructuredVerdict
	if err := json.Unmarshal([]byte(extracted), &v); err != nil {
		res.Status = driver.StatusFailed
		res.Explanation = fmt.Sprintf("malformed_verdict: %v; raw: %s", err, truncateForExplanation(stdout))
		return res
	}
	if err := verdict.Validate(v); err != nil {
		res.Status = driver.StatusFailed
		res.Explanation = fmt.Sprintf("malformed_verdict: %v; raw: %s", err, truncateForExplanation(stdout))
		return res
	}
	canonical, err := json.Marshal(v)
	if err != nil {
		res.Status = driver.StatusFailed
		res.Explanation = fmt.Sprintf("malformed_verdict: canonical re-serialize: %v; raw: %s", err, truncateForExplanation(stdout))
		return res
	}
	res.Status = driver.StatusSucceeded
	res.Explanation = string(canonical)
	return res
}

func truncateForExplanation(s string) string {
	if len(s) <= reviewExplanationRawLimit {
		return s
	}
	return s[:reviewExplanationRawLimit]
}

var _ driver.AgentDriver = (*Driver)(nil)
