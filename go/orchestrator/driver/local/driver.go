package local

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/chitinhq/chitin/go/orchestrator/driver"
)

const (
	id      = "local"
	version = "0.1.0"

	// baseURLEnv is the environment variable holding the base URL of the
	// operator-hosted, OpenAI-compatible endpoint the driver drives.
	baseURLEnv = "CHITIN_LOCAL_LLM_URL"
)

// Driver is the reference local-LLM AgentDriver (FR-014): a coding-agent
// loop against a self-hosted, OpenAI-compatible endpoint. Unlike the hosted
// CLI drivers it has no quota and no marginal cost — readiness is governed
// by the endpoint's configuration and reachability, not a binary on PATH.
type Driver struct {
	command string
	model   string
	baseURL string
	client  *http.Client
}

// Option configures a Driver.
type Option func(*Driver)

// WithCommand overrides the executable used by Invoke to run the
// coding-agent loop.
func WithCommand(command string) Option {
	return func(d *Driver) {
		if command != "" {
			d.command = command
		}
	}
}

// WithModel overrides the model name used for invocation and the card.
func WithModel(model string) Option {
	return func(d *Driver) {
		if model != "" {
			d.model = model
		}
	}
}

// WithBaseURL overrides the OpenAI-compatible endpoint base URL, taking
// precedence over the CHITIN_LOCAL_LLM_URL environment variable. It exists
// primarily so tests and operators can point the driver at a specific
// endpoint without changing core code.
func WithBaseURL(baseURL string) Option {
	return func(d *Driver) {
		if baseURL != "" {
			d.baseURL = baseURL
		}
	}
}

// WithHTTPClient overrides the HTTP client used by Ready to probe the
// endpoint's reachability.
func WithHTTPClient(c *http.Client) Option {
	return func(d *Driver) {
		if c != nil {
			d.client = c
		}
	}
}

// New returns the reference local-LLM AgentDriver. The endpoint base URL
// defaults to the CHITIN_LOCAL_LLM_URL environment variable; WithBaseURL
// overrides it.
func New(opts ...Option) *Driver {
	d := &Driver{
		command: "chitin-local-agent",
		model:   "local-default",
		baseURL: strings.TrimSpace(os.Getenv(baseURLEnv)),
		client:  &http.Client{Timeout: 5 * time.Second},
	}
	for _, opt := range opts {
		opt(d)
	}
	return d
}

// ID returns the stable driver identifier.
func (d *Driver) ID() string { return id }

// Card returns the local-LLM capability card. Tier is local and the cost
// class is free — a model on the operator's own GPU draws no quota and has
// no marginal cost. Capabilities are scoped coding work, not the full
// frontier set.
func (d *Driver) Card() driver.CapabilityCard {
	return driver.CapabilityCard{
		DriverID:     id,
		Version:      version,
		AgentRuntime: "local-llm",
		Model:        d.model,
		Capabilities: []driver.Capability{
			driver.CapCodeImplement,
			driver.CapCodeRefactor,
			driver.CapBulkCodegen,
		},
		Tier:      driver.TierLocal,
		CostClass: driver.CostFree,
		Constraints: driver.Constraints{
			QuotaBounded:     false,
			NetworkRequired:  false,
			MaxContextTokens: 32000,
			WorktreeRequired: true,
		},
	}
}

// Ready reports whether the self-hosted endpoint is configured and
// reachable. A driver whose endpoint is unset or unreachable reports
// not-ready so the scheduler routes elsewhere rather than failing the work
// unit by trying anyway (FR-008).
func (d *Driver) Ready(ctx context.Context) (bool, string) {
	if err := ctx.Err(); err != nil {
		return false, err.Error()
	}
	if d.baseURL == "" {
		return false, fmt.Sprintf("local-LLM endpoint not configured: set %s", baseURLEnv)
	}
	probeURL := strings.TrimRight(d.baseURL, "/") + "/v1/models"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, probeURL, nil)
	if err != nil {
		return false, fmt.Sprintf("local-LLM endpoint %q is not a valid URL: %v", d.baseURL, err)
	}
	resp, err := d.client.Do(req)
	if err != nil {
		return false, fmt.Sprintf("local-LLM endpoint %q not reachable: %v", d.baseURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= http.StatusInternalServerError {
		return false, fmt.Sprintf("local-LLM endpoint %q unhealthy: HTTP %d", d.baseURL, resp.StatusCode)
	}
	return true, ""
}

// Invoke runs the coding-agent loop against the self-hosted endpoint in the
// work unit's dedicated worktree. The endpoint base URL is passed to the
// loop via the environment so the same OpenAI-compatible client config is
// used by Ready and Invoke.
func (d *Driver) Invoke(ctx context.Context, wu driver.WorkUnit) (driver.Result, error) {
	ctx, cancel := invocationContext(ctx, wu.Deadline)
	defer cancel()

	prompt := promptFor(wu)
	cmd := exec.CommandContext(ctx, d.command, "--model", d.model, "-p", prompt)
	cmd.Dir = wu.WorktreePath
	cmd.Env = append(os.Environ(), baseURLEnv+"="+d.baseURL)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return resultFromCommand(ctx, wu, d.ID(), strings.TrimSpace(stdout.String()), strings.TrimSpace(stderr.String()), err), nil
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
