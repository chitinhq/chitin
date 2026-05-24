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
// Review-mode invocations (wu.Tool == reviewToolName, spec 094 FR-002 /
// spec 109) route through the StructuredVerdict path: the driver post-
// processes the model's stdout into JSON, validates against the closed
// schema, and returns a typed Result so the dispatch activity never sees
// malformed prose escaping the driver boundary.
func (d *Driver) Invoke(ctx context.Context, wu driver.WorkUnit) (driver.Result, error) {
	ctx, cancel := invocationContext(ctx, wu.Deadline)
	defer cancel()

	if wu.Tool == reviewToolName {
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

// invokeReview runs the review-mode dispatch path. It builds the
// StructuredVerdict-shaped prompt, executes claude, then post-processes
// stdout into a validated StructuredVerdict. On any extract / parse /
// validate failure it returns StatusFailed with a "malformed_verdict:"
// explanation so the dispatch activity can classify the outcome as
// FailureMalformedShape without re-interpreting the wrapper error
// (spec 094 FR-014; spec 109 FR-004, FR-005). The raw model output is
// captured in OutputRef in every case so operators can post-mortem
// without a separate audit fetch.
func (d *Driver) invokeReview(ctx context.Context, wu driver.WorkUnit) driver.Result {
	prompt := reviewPromptFor(wu)
	cmd := exec.CommandContext(ctx, d.command, "--dangerously-skip-permissions", "-p", prompt)
	if wu.WorktreePath != "" {
		cmd.Dir = wu.WorktreePath
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()

	raw := strings.TrimSpace(stdout.String())
	stderrStr := strings.TrimSpace(stderr.String())
	res := driver.Result{WorkUnitID: wu.ID, DriverID: d.ID(), OutputRef: raw}

	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		res.Status = driver.StatusTimeout
		res.Explanation = fmt.Sprintf("driver %q review-mode timed out on work unit %q", d.ID(), wu.ID)
		if stderrStr != "" {
			res.Explanation += ": " + stderrStr
		}
		return res
	}
	if runErr != nil {
		res.Status = driver.StatusFailed
		res.Explanation = fmt.Sprintf("driver %q review-mode failed on work unit %q: %v", d.ID(), wu.ID, runErr)
		if stderrStr != "" {
			res.Explanation += ": " + stderrStr
		}
		return res
	}

	extracted, err := extractVerdictJSON(raw)
	if err != nil {
		res.Status = driver.StatusFailed
		res.Explanation = fmt.Sprintf("malformed_verdict: %v; raw: %s", err, truncateRaw(raw))
		return res
	}

	var v verdict.StructuredVerdict
	if err := json.Unmarshal([]byte(extracted), &v); err != nil {
		res.Status = driver.StatusFailed
		res.Explanation = fmt.Sprintf("malformed_verdict: %v; raw: %s", err, truncateRaw(raw))
		return res
	}
	if err := verdict.Validate(v); err != nil {
		res.Status = driver.StatusFailed
		res.Explanation = fmt.Sprintf("malformed_verdict: %v; raw: %s", err, truncateRaw(raw))
		return res
	}

	// Canonical re-serialization (FR-005): emit the verdict the activity
	// will parse, not the model's free-form whitespace.
	canonical, err := json.Marshal(v)
	if err != nil {
		// Marshal of a Validate-passed value can't realistically fail, but
		// guard so the contract holds even under unusual encoder errors.
		res.Status = driver.StatusFailed
		res.Explanation = fmt.Sprintf("malformed_verdict: %v; raw: %s", err, truncateRaw(raw))
		return res
	}
	res.Status = driver.StatusSucceeded
	res.Explanation = string(canonical)
	return res
}

// truncateRaw caps the raw model output snippet at 1 KiB (spec 109 US2 /
// FR-004), so a runaway model can't blow up the activity result record.
func truncateRaw(s string) string {
	const limit = 1024
	if len(s) <= limit {
		return s
	}
	return s[:limit]
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
