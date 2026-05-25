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
// Spec 094 review-mode work units take a separate codepath: argv is built
// via reviewArgvFor (which adds --skip-git-repo-check; FR-001) and the
// stdout is post-processed into a validated StructuredVerdict
// (FR-003/FR-004/FR-005). Non-review-mode invocations are unchanged
// (FR-002) — the trusted-directory check stays in force for local-driver
// implementation work where worktree trust matters.
func (d *Driver) Invoke(ctx context.Context, wu driver.WorkUnit) (driver.Result, error) {
	ctx, cancel := invocationContext(ctx, wu.Deadline)
	defer cancel()

	reviewMode := isReviewModeWorkUnit(wu)
	var cmd *exec.Cmd
	if reviewMode {
		argv := reviewArgvFor(wu, d.model)
		cmd = exec.CommandContext(ctx, d.command, argv...)
	} else {
		prompt := promptFor(wu)
		cmd = exec.CommandContext(ctx, d.command, "exec", "--model", d.model, prompt)
	}
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
// (SpecID="094", TaskID="review") and what claudecode's driver uses to
// route review-mode invocations — keeping the dispatch-side contract
// uniform across reviewer drivers.
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
// malformed_verdict explanation (spec 110 FR-005). 1 KiB is enough to
// fingerprint what the model emitted without flooding workflow history.
const reviewExplanationRawLimit = 1024

// reviewResultFromCommand turns the codex CLI's (stdout, stderr, err) triple
// from a review-mode invocation into a typed Result per spec 110
// FR-004/FR-005/FR-006 (parity with spec 109). On failure paths it returns
// StatusFailed with an explanation of the form
// "malformed_verdict: <detail>; raw: <first 1KiB of stdout>" so the dispatch
// activity can post-mortem without re-fetching the model's output. On
// success it canonically re-serializes the validated StructuredVerdict into
// Explanation, which is the field spec 094's DispatchMachineReviewer parses
// via verdict.ParseStructured.
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
		res.Explanation = "malformed_verdict: empty output from codex review-mode invocation; raw: "
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
	// Normalize nil list fields to empty slices so the canonical JSON has
	// stable `[]` for omitted fields instead of `null`. Same fix landed in
	// claudecode/review_mode.go (PR #1041): the model often omits empty
	// fields (e.g. Approve with no concerns), and a bare `"concerns":null`
	// breaks downstream consumers that expect arrays per the
	// StructuredVerdict schema.
	if v.Concerns == nil {
		v.Concerns = []string{}
	}
	if v.Recommendations == nil {
		v.Recommendations = []string{}
	}
	if v.Blockers == nil {
		v.Blockers = []string{}
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
