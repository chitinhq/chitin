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
	"github.com/chitinhq/chitin/go/orchestrator/internal/blob"
)

const (
	id      = "claudecode-glm"
	version = "0.1.0"

	modelEnv     = "CHITIN_CLAUDECODE_GLM_MODEL"
	contextEnv   = "CHITIN_CLAUDECODE_GLM_CONTEXT"
	ollamaEnv    = "CHITIN_OLLAMA_BIN"
	defaultModel = "glm-5.1"
	defaultURL   = "http://localhost:11434"
)

// Driver wraps Claude Code launched through Ollama's local gateway.
type Driver struct {
	ollamaCommand string
	claudeCommand string
	model         string
	baseURL       string
	maxContext    int
	client        *http.Client
	blobs         blob.Store
}

// Option configures a Driver. Tests use these hooks to keep Ready and Invoke
// hermetic while production follows environment defaults.
type Option func(*Driver)

func WithOllamaCommand(command string) Option {
	return func(d *Driver) {
		if command != "" {
			d.ollamaCommand = command
		}
	}
}

func WithClaudeCommand(command string) Option {
	return func(d *Driver) {
		if command != "" {
			d.claudeCommand = command
		}
	}
}

func WithModel(model string) Option {
	return func(d *Driver) {
		if model != "" {
			d.model = model
		}
	}
}

func WithBaseURL(baseURL string) Option {
	return func(d *Driver) {
		if baseURL != "" {
			d.baseURL = strings.TrimRight(baseURL, "/")
		}
	}
}

func WithHTTPClient(c *http.Client) Option {
	return func(d *Driver) {
		if c != nil {
			d.client = c
		}
	}
}

func WithBlobStore(store blob.Store) Option {
	return func(d *Driver) {
		d.blobs = store
	}
}

// New returns the claudecode-glm AgentDriver.
func New(opts ...Option) *Driver {
	model := strings.TrimSpace(os.Getenv(modelEnv))
	if model == "" {
		model = defaultModel
	}
	d := &Driver{
		ollamaCommand: strings.TrimSpace(os.Getenv(ollamaEnv)),
		claudeCommand: "claude",
		model:         model,
		baseURL:       defaultURL,
		maxContext:    intFromEnv(contextEnv, 32768),
		client:        &http.Client{Timeout: 2 * time.Second},
	}
	if d.ollamaCommand == "" {
		d.ollamaCommand = "ollama"
	}
	for _, opt := range opts {
		opt(d)
	}
	return d
}

func (d *Driver) ID() string { return id }

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
			MaxContextTokens: d.maxContext,
			WorktreeRequired: true,
		},
	}
}

func (d *Driver) Ready(ctx context.Context) (bool, string) {
	if err := ctx.Err(); err != nil {
		return false, err.Error()
	}
	if _, err := exec.LookPath(d.ollamaCommand); err != nil {
		return false, fmt.Sprintf("ollama binary %q not found: %v", d.ollamaCommand, err)
	}
	probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	tagsURL := strings.TrimRight(d.baseURL, "/") + "/api/tags"
	req, err := http.NewRequestWithContext(probeCtx, http.MethodGet, tagsURL, nil)
	if err != nil {
		return false, fmt.Sprintf("ollama daemon URL %q is invalid: %v", tagsURL, err)
	}
	resp, err := d.client.Do(req)
	if err != nil {
		return false, fmt.Sprintf("ollama daemon not reachable at %s", strings.TrimRight(d.baseURL, "/"))
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return false, fmt.Sprintf("ollama daemon unhealthy at %s: HTTP %d", strings.TrimRight(d.baseURL, "/"), resp.StatusCode)
	}
	var tags ollamaTags
	if err := json.NewDecoder(resp.Body).Decode(&tags); err != nil {
		return false, fmt.Sprintf("ollama tags response unreadable: %v", err)
	}
	if !tags.HasModel(d.model) {
		return false, fmt.Sprintf("model %s not present in ollama (try: ollama pull %s)", d.model, d.model)
	}
	if _, err := exec.LookPath(d.claudeCommand); err != nil {
		return false, fmt.Sprintf("claude CLI binary %q not found: %v", d.claudeCommand, err)
	}
	return true, ""
}

func (d *Driver) Invoke(ctx context.Context, wu driver.WorkUnit) (driver.Result, error) {
	ctx, cancel := claudecodeshared.InvocationContext(ctx, wu.Deadline)
	defer cancel()

	prompt := claudecodeshared.PromptFor(wu)
	args := []string{"launch", "claude", "--model", d.model, "--"}
	args = append(args, claudecodeshared.PrintArgs(prompt)...)
	cmd := exec.CommandContext(ctx, d.ollamaCommand, args...)
	cmd.Dir = wu.WorktreePath

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	out := strings.TrimSpace(stdout.String())
	errOut := strings.TrimSpace(stderr.String())
	res, resErr := claudecodeshared.ResultFromCommand(ctx, d.blobs, wu, d.ID(), out, errOut, err)
	if resErr != nil {
		return driver.Result{}, resErr
	}
	if res.Status == driver.StatusFailed && strings.Contains(errOut, "launch") && strings.Contains(errOut, "unknown") {
		res.Explanation += "; ollama v0.21+ required for launch subcommand"
	}
	return res, nil
}

type ollamaTags struct {
	Models []ollamaModel `json:"models"`
}

type ollamaModel struct {
	Name        string `json:"name"`
	Model       string `json:"model"`
	RemoteModel string `json:"remote_model"`
}

func (t ollamaTags) HasModel(want string) bool {
	for _, m := range t.Models {
		for _, have := range []string{m.Name, m.Model, m.RemoteModel} {
			if have == want || strings.HasPrefix(have, want+":") {
				return true
			}
		}
	}
	return false
}

func intFromEnv(name string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return fallback
	}
	return n
}

var _ driver.AgentDriver = (*Driver)(nil)
