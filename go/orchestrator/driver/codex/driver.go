package codex

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
	id      = "codex"
	version = "0.1.0"
)

// Driver wraps the Codex CLI behind the spec-075 AgentDriver contract.
type Driver struct {
	command string
	model   string
}

// Option configures a Driver.
type Option func(*Driver)

// WithCommand overrides the executable used by Ready and Invoke.
func WithCommand(command string) Option {
	return func(d *Driver) {
		if command != "" {
			d.command = command
		}
	}
}

// WithModel overrides the Codex model name used for invocation and the card.
func WithModel(model string) Option {
	return func(d *Driver) {
		if model != "" {
			d.model = model
		}
	}
}

// New returns a Codex AgentDriver.
func New(opts ...Option) *Driver {
	d := &Driver{command: "codex", model: "gpt-5.x-codex"}
	for _, opt := range opts {
		opt(d)
	}
	return d
}

// ID returns the stable driver identifier.
func (d *Driver) ID() string { return id }

// Card returns the Codex capability card.
func (d *Driver) Card() driver.CapabilityCard {
	return driver.CapabilityCard{
		DriverID:     id,
		Version:      version,
		AgentRuntime: "codex-cli",
		Model:        d.model,
		Capabilities: []driver.Capability{
			driver.CapCodeImplement,
			driver.CapCodeReview,
			driver.CapCodeRefactor,
			driver.CapTestAuthor,
		},
		Tier:      driver.TierFrontier,
		CostClass: driver.CostHigh,
		Constraints: driver.Constraints{
			QuotaBounded:     true,
			NetworkRequired:  true,
			MaxContextTokens: 400000,
			WorktreeRequired: true,
		},
	}
}

// Ready reports whether the Codex CLI binary is available.
func (d *Driver) Ready(ctx context.Context) (bool, string) {
	if err := ctx.Err(); err != nil {
		return false, err.Error()
	}
	if _, err := exec.LookPath(d.command); err != nil {
		return false, fmt.Sprintf("Codex runtime %q not found: %v", d.command, err)
	}
	return true, ""
}

// Invoke shells out to Codex in the work unit's dedicated worktree.
//
// When the work unit is a review-mode dispatch (spec 094, TaskID matches the
// canonical "review" tool name), the invocation runs the review-mode path:
// it builds argv via reviewArgvFor (which adds --skip-git-repo-check per
// spec 110 FR-001), captures stdout, runs it through extractVerdictJSON +
// verdict.Validate, and returns a typed Result the DispatchMachineReviewer
// activity can parse without further interpretation. All other work units
// take the pre-existing implementation path (FR-002).
func (d *Driver) Invoke(ctx context.Context, wu driver.WorkUnit) (driver.Result, error) {
	ctx, cancel := invocationContext(ctx, wu.Deadline)
	defer cancel()

	if isReviewMode(wu) {
		return d.invokeReview(ctx, wu), nil
	}

	prompt := promptFor(wu)
	cmd := exec.CommandContext(ctx, d.command, "exec", "--model", d.model, prompt)
	cmd.Dir = wu.WorktreePath

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return resultFromCommand(ctx, wu, d.ID(), strings.TrimSpace(stdout.String()), strings.TrimSpace(stderr.String()), err), nil
}

// invokeReview is the review-mode dispatch (spec 110 FR-003..FR-006). It
// runs the codex CLI with the review-mode argv + prompt, then post-processes
// stdout via extractVerdictJSON + json.Unmarshal + verdict.Validate. On a
// happy path it returns Result{Status: StatusSucceeded, Explanation: <the
// canonicalized StructuredVerdict JSON>} so the DispatchMachineReviewer
// activity (spec 094) can ParseStructured the explanation directly. On any
// parse / validate failure it returns Result{Status: StatusFailed,
// Explanation: "malformed_verdict: <reason>; raw: <first 1KiB>"} so the
// activity classifies the outcome as FailureMalformedShape from the driver
// side rather than after the fact.
func (d *Driver) invokeReview(ctx context.Context, wu driver.WorkUnit) driver.Result {
	argv := reviewArgvFor(wu, d.model)
	cmd := exec.CommandContext(ctx, d.command, argv...)
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
			Explanation: "malformed_verdict: empty output from codex review-mode invocation",
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

	// Re-serialize canonically (FR-006) so the activity reads back a
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
// explanation per spec 110 FR-005 (cap at 1 KiB).
const rawOutputCap = 1024

// malformedVerdictExplanation composes the failure explanation in the
// "malformed_verdict: <reason>; raw: <first 1KiB>" shape spec 110 FR-005
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
