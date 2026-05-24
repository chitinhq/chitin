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
//
// When the work unit is a review-mode dispatch (spec 094, TaskID matches
// the canonical "review" tool name), the invocation runs the review-mode
// path: it builds a StructuredVerdict-shaped prompt via reviewPromptFor,
// captures stdout, runs it through extractVerdictJSON + verdict.Validate,
// and returns a typed Result that the DispatchMachineReviewer activity can
// parse without further interpretation. All other work units take the
// pre-existing implementation path.
func (d *Driver) Invoke(ctx context.Context, wu driver.WorkUnit) (driver.Result, error) {
	ctx, cancel := invocationContext(ctx, wu.Deadline)
	defer cancel()

	if isReviewMode(wu) {
		return d.invokeReview(ctx, wu), nil
	}

	prompt := promptFor(wu)
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
	return resultFromCommand(ctx, wu, d.ID(), strings.TrimSpace(stdout.String()), strings.TrimSpace(stderr.String()), err), nil
}

// invokeReview is the review-mode dispatch (spec 109 FR-003..FR-005). It
// runs the claudecode CLI with the review-mode prompt, then post-processes
// stdout via extractVerdictJSON + json.Unmarshal + verdict.Validate. On a
// happy path it returns Result{Status: StatusSucceeded, Explanation: <the
// canonicalized StructuredVerdict JSON>} so the DispatchMachineReviewer
// activity (spec 094) can ParseStructured the explanation directly. On any
// parse / validate failure it returns Result{Status: StatusFailed,
// Explanation: "malformed_verdict: <reason>; raw: <first 1KiB>"} so the
// activity classifies the outcome as FailureMalformedShape from the driver
// side rather than after the fact.
func (d *Driver) invokeReview(ctx context.Context, wu driver.WorkUnit) driver.Result {
	prompt := reviewPromptFor(wu)
	cmd := exec.CommandContext(ctx, d.command, "--dangerously-skip-permissions", "-p", prompt)
	cmd.Dir = wu.WorktreePath

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()

	rawOut := strings.TrimSpace(stdout.String())
	rawErr := strings.TrimSpace(stderr.String())

	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return driver.Result{
			WorkUnitID:  wu.ID,
			DriverID:    d.ID(),
			Status:      driver.StatusTimeout,
			Explanation: composeTimeoutExplanation(d.ID(), wu.ID, rawErr),
		}
	}
	if runErr != nil {
		return driver.Result{
			WorkUnitID:  wu.ID,
			DriverID:    d.ID(),
			Status:      driver.StatusFailed,
			Explanation: composeRunErrExplanation(d.ID(), wu.ID, runErr, rawErr),
		}
	}
	if rawOut == "" {
		return driver.Result{
			WorkUnitID:  wu.ID,
			DriverID:    d.ID(),
			Status:      driver.StatusFailed,
			Explanation: "malformed_verdict: empty output from claudecode review-mode invocation",
		}
	}

	candidate, extractErr := extractVerdictJSON(rawOut)
	if extractErr != nil {
		return driver.Result{
			WorkUnitID:  wu.ID,
			DriverID:    d.ID(),
			Status:      driver.StatusFailed,
			Explanation: malformedVerdictExplanation(extractErr.Error(), rawOut),
		}
	}

	var sv verdict.StructuredVerdict
	if err := json.Unmarshal([]byte(candidate), &sv); err != nil {
		return driver.Result{
			WorkUnitID:  wu.ID,
			DriverID:    d.ID(),
			Status:      driver.StatusFailed,
			Explanation: malformedVerdictExplanation(fmt.Sprintf("json.Unmarshal: %v", err), rawOut),
		}
	}
	if err := verdict.Validate(sv); err != nil {
		return driver.Result{
			WorkUnitID:  wu.ID,
			DriverID:    d.ID(),
			Status:      driver.StatusFailed,
			Explanation: malformedVerdictExplanation(err.Error(), rawOut),
		}
	}

	// Re-serialize canonically (FR-005) so the activity reads back a
	// normalized representation regardless of incidental whitespace or
	// key ordering the model produced.
	canon, err := json.Marshal(sv)
	if err != nil {
		return driver.Result{
			WorkUnitID:  wu.ID,
			DriverID:    d.ID(),
			Status:      driver.StatusFailed,
			Explanation: malformedVerdictExplanation(fmt.Sprintf("re-serialize: %v", err), rawOut),
		}
	}
	return driver.Result{
		WorkUnitID:  wu.ID,
		DriverID:    d.ID(),
		Status:      driver.StatusSucceeded,
		Explanation: string(canon),
	}
}

// rawOutputCap bounds the raw-output snippet embedded in a malformed_verdict
// explanation per spec 109 FR-004 (cap at 1 KiB).
const rawOutputCap = 1024

// malformedVerdictExplanation composes the failure explanation in the
// "malformed_verdict: <reason>; raw: <first 1KiB>" shape spec 109 FR-004
// prescribes.
func malformedVerdictExplanation(reason, raw string) string {
	snippet := raw
	if len(snippet) > rawOutputCap {
		snippet = snippet[:rawOutputCap]
	}
	return fmt.Sprintf("malformed_verdict: %s; raw: %s", reason, snippet)
}

// composeTimeoutExplanation renders the timeout reason in the conventional
// shape the non-review path uses, so the activity sees a uniform format.
func composeTimeoutExplanation(driverID, wuID, stderr string) string {
	msg := fmt.Sprintf("driver %q timed out running work unit %q", driverID, wuID)
	if stderr != "" {
		msg += ": " + stderr
	}
	return msg
}

// composeRunErrExplanation renders the run-error reason in the conventional
// shape the non-review path uses.
func composeRunErrExplanation(driverID, wuID string, runErr error, stderr string) string {
	msg := fmt.Sprintf("driver %q failed running work unit %q: %v", driverID, wuID, runErr)
	if stderr != "" {
		msg += ": " + stderr
	}
	return msg
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

var _ driver.AgentDriver = (*Driver)(nil)
