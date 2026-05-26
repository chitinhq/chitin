package claudecodeglm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/chitinhq/chitin/go/orchestrator/driver"
	"github.com/chitinhq/chitin/go/orchestrator/driver/claudecodeshared"
)

const (
	id      = "claudecode-glm"
	version = "0.1.0"

	defaultModel         = "glm-5.1"
	defaultContextTokens = 32768
	ollamaBinEnv         = "CHITIN_OLLAMA_BIN"
	modelEnv             = "CHITIN_CLAUDECODE_GLM_MODEL"
	contextEnv           = "CHITIN_CLAUDECODE_GLM_CONTEXT"
	tagsURL              = "http://localhost:11434/api/tags"
)

// Driver wraps Claude Code through Ollama's local Claude integration.
type Driver struct {
	ollamaBin string
	claudeBin string
	model     string
	context   int
	client    *http.Client
}

// Option configures a Driver.
type Option func(*Driver)

// WithOllamaBin overrides the ollama executable path.
func WithOllamaBin(path string) Option {
	return func(d *Driver) {
		if path != "" {
			d.ollamaBin = path
		}
	}
}

// WithClaudeBin overrides the Claude Code executable path checked by Ready.
func WithClaudeBin(path string) Option {
	return func(d *Driver) {
		if path != "" {
			d.claudeBin = path
		}
	}
}

// WithModel overrides the local Ollama model.
func WithModel(model string) Option {
	return func(d *Driver) {
		if model != "" {
			d.model = model
		}
	}
}

// WithHTTPClient overrides the Ready probe client.
func WithHTTPClient(c *http.Client) Option {
	return func(d *Driver) {
		if c != nil {
			d.client = c
		}
	}
}

// New returns the claudecode-glm AgentDriver.
func New(opts ...Option) *Driver {
	ollamaBin := strings.TrimSpace(os.Getenv(ollamaBinEnv))
	if ollamaBin == "" {
		ollamaBin = "ollama"
	}
	model := strings.TrimSpace(os.Getenv(modelEnv))
	if model == "" {
		model = defaultModel
	}
	contextTokens := defaultContextTokens
	if raw := strings.TrimSpace(os.Getenv(contextEnv)); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			contextTokens = n
		}
	}
	d := &Driver{
		ollamaBin: ollamaBin,
		claudeBin: "claude",
		model:     model,
		context:   contextTokens,
		client:    &http.Client{Timeout: 2 * time.Second},
	}
	for _, opt := range opts {
		opt(d)
	}
	return d
}

// ID returns the stable driver identifier.
func (d *Driver) ID() string { return id }

// Card returns the claudecode-glm capability card.
func (d *Driver) Card() driver.CapabilityCard {
	return driver.CapabilityCard{
		DriverID:     id,
		Version:      version,
		AgentRuntime: "claude-code",
		Model:        d.model,
		Capabilities: []driver.Capability{
			driver.CapCodeImplement,
			driver.CapSpecImplement,
		},
		Tier:      driver.TierLocal,
		CostClass: driver.CostZero,
		Constraints: driver.Constraints{
			QuotaBounded:     false,
			NetworkRequired:  false,
			MaxContextTokens: d.context,
			WorktreeRequired: true,
		},
	}
}

// Ready verifies that ollama, its daemon, the configured model, and the
// Claude Code child CLI are available.
func (d *Driver) Ready(ctx context.Context) (bool, string) {
	if err := ctx.Err(); err != nil {
		return false, err.Error()
	}
	if _, err := exec.LookPath(d.ollamaBin); err != nil {
		return false, fmt.Sprintf("ollama binary %q not found: %v", d.ollamaBin, err)
	}
	if _, err := exec.LookPath(d.claudeBin); err != nil {
		return false, fmt.Sprintf("Claude Code runtime %q not found: %v", d.claudeBin, err)
	}

	probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(probeCtx, http.MethodGet, tagsURL, nil)
	if err != nil {
		return false, fmt.Sprintf("ollama daemon probe URL invalid: %v", err)
	}
	resp, err := d.client.Do(req)
	if err != nil {
		return false, "ollama daemon not reachable at http://localhost:11434"
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return false, fmt.Sprintf("ollama daemon unhealthy at http://localhost:11434: HTTP %d", resp.StatusCode)
	}

	var tags struct {
		Models []struct {
			Name  string `json:"name"`
			Model string `json:"model"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tags); err != nil {
		return false, fmt.Sprintf("ollama tags response malformed: %v", err)
	}
	for _, m := range tags.Models {
		if m.Name == d.model || m.Model == d.model {
			return true, ""
		}
	}
	return false, fmt.Sprintf("model %s not present in ollama (try: ollama pull %s)", d.model, d.model)
}

// Invoke shells out through `ollama launch claude` in the worktree.
func (d *Driver) Invoke(ctx context.Context, wu driver.WorkUnit) (driver.Result, error) {
	ctx, cancel := claudecodeshared.InvocationContext(ctx, wu.Deadline)
	defer cancel()

	args := append([]string{"launch", "claude", "--model", d.Card().Model, "--"}, claudecodeshared.PrintArgsFor(wu)...)
	cmd := exec.CommandContext(ctx, d.ollamaBin, args...)
	cmd.Dir = wu.WorktreePath

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	res := claudecodeshared.ResultFromCommand(ctx, wu, d.ID(), stdout.String(), stderr.String(), err)
	if err != nil && strings.Contains(stderr.String(), "unknown command") && strings.Contains(stderr.String(), "launch") {
		res.Explanation = "ollama v0.21+ required for launch subcommand: " + res.Explanation
	}
	return res, nil
}

var _ driver.AgentDriver = (*Driver)(nil)
