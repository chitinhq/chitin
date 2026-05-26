package claudecode

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/chitinhq/chitin/go/orchestrator/driver"
	"github.com/chitinhq/chitin/go/orchestrator/driver/claudecodeshared"
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
			// NOTE (2026-05-25): claudecode does NOT declare
			// CapSpecImplement. The whole-spec dispatch payload is
			// large (full spec.md + tasks.md + plan.md per spec 119
			// FR-003) and claudecode = opus-4.7 per-token cost makes
			// each invocation prohibitively expensive at our scale.
			// codex (gpt-5.x-codex) is the sole CapSpecImplement
			// declarer until a local-model alternative (glm-5.1 via
			// ollama) ships its own driver. Re-adding this capability
			// to claudecode requires an explicit cost-policy change.
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
	ctx, cancel := claudecodeshared.InvocationContext(ctx, wu.Deadline)
	defer cancel()

	reviewMode := isReviewMode(wu)
	// --dangerously-skip-permissions is mandatory for dispatch-mode
	// invocations: the chitin worker spawns claude headlessly inside a
	// fresh worktree, and without this flag claude's sandbox refuses
	// `Write` and `touch` against the worktree path (surfaced by the
	// 2026-05-24 dogfood; claudecode would author its proposed code in
	// the explanation but produce no commits → no PR). claude's own
	// help text recommends the flag "for sandboxes with no internet
	// access" — chitin's worker context matches that intent.
	var argv []string
	if reviewMode {
		argv = []string{"--dangerously-skip-permissions", "-p", reviewPromptFor(wu)}
	} else {
		argv = claudecodeshared.PrintArgsFor(wu)
	}
	cmd := exec.CommandContext(ctx, d.command, argv...)
	cmd.Dir = wu.WorktreePath

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	out := strings.TrimSpace(stdout.String())
	errOut := strings.TrimSpace(stderr.String())
	if reviewMode {
		return reviewResult(ctx, wu, d.ID(), out, errOut, err), nil
	}
	return claudecodeshared.ResultFromCommand(ctx, wu, d.ID(), out, errOut, err), nil
}

var _ driver.AgentDriver = (*Driver)(nil)
