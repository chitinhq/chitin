package router

// spawnPeer — synchronously invoke a peer CLI (claude-code, copilot,
// codex, gemini) for a routed escalation. Fresh worktree, recursive-
// escalation guard, normalized ToolCallResult with full provenance.
//
// Per docs/design/2026-05-06-kernel-gate-escalation.md (step 3 of 6).
// Behind a flag — no gate path calls SpawnPeer yet (step 4 wires the
// in-gate path); SpawnPeer is reachable from tests + a future
// `chitin-kernel router test-spawn` debug subcommand.
//
// Invariants (the gate caller can rely on these):
//   - Peer always runs in a fresh, empty worktree (mkdtemp). Worker's
//     dirty tree is NEVER exposed.
//   - Spawn env always carries CHITIN_NO_ESCALATE=1. Peer can be
//     gated/denied/advised but cannot itself spawn another peer.
//   - Spawn enforces a wall-clock timeout. SIGKILL on overrun, no
//     orphaned processes.
//   - Worktree cleanup runs on success AND failure (defer-based).
//   - Returned ToolCallResult carries full Provenance: escalation_id,
//     worker workflow_id, route, candidate, trigger signal, severity,
//     spawn time + duration, peer exit code, raw peer stdout/stderr.
//
// The function is pure with respect to global state EXCEPT for the
// subprocess + filesystem side effects within the temp worktree.
// Specifically: no logging side effects, no chain writes — those are
// the gate caller's responsibility (so SpawnPeer is unit-testable).

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

var (
	ErrSpawnFailed       = errors.New("peer spawn failed")
	ErrSpawnTimeout      = errors.New("peer spawn timed out")
	ErrUnsupportedDriver = errors.New("driver has no spawn template")
	ErrWorktreeSetup     = errors.New("worktree setup failed")
)

// ToolCallResult is what SpawnPeer returns when the peer succeeds.
// The Content field is what the worker sees in place of its own tool
// result; Provenance is the full audit trail the chain layer records.
type ToolCallResult struct {
	// Content is the worker-visible output. For claude-code/codex/etc
	// this is typically the peer's stdout text body — what the peer
	// said in response to the routed prompt. Type is `any` so future
	// per-driver shapes (file diffs, JSON envelopes, etc.) can land
	// without breaking the contract.
	Content any

	// Provenance — required, never optional. Every escalated result
	// carries this so /mine + conformance extractors can join peer
	// outcomes back to the workflow that triggered them.
	Provenance Provenance

	// Raw stdout/stderr — kept in chain for replay/audit, never shown
	// to the worker.
	RawPeerStdout string
	RawPeerStderr string
}

// Provenance — full attribution for one peer-spawn event.
type Provenance struct {
	EscalationID     string
	WorkerWorkflowID string
	TriggerSignal    string
	Severity         string
	Route            string
	Candidate        Candidate
	SpawnedAt        time.Time
	DurationMs       int64
	PeerExitCode     int
	WorktreePath     string
}

// SpawnConfig is what SpawnPeer needs from the caller. Held separate
// from RouteRequest because the gate caller has additional context
// (workflow_id, route metadata from the matched rule) that RouteRequest
// alone doesn't carry.
type SpawnConfig struct {
	Decision RouteDecision
	Request  RouteRequest

	// SpawnTimeoutSeconds — passed in from RoutesPolicy. 0 = use default 60.
	SpawnTimeoutSeconds int

	// PromptText — what to ask the peer. Built by the gate caller from
	// the worker's tool call + advisor nudge + chain tail. SpawnPeer
	// just pipes it in; doesn't compose it.
	PromptText string

	// Spawner is dependency-injected so unit tests can stub out the
	// real subprocess execution. Production wires defaultSpawner.
	Spawner Spawner
}

// Spawner is the seam for unit testing — production uses execSpawner;
// tests use a stub that returns canned output without touching the
// filesystem or spawning real processes.
type Spawner interface {
	Run(ctx context.Context, name string, args []string, env []string, workDir string, stdin string) (stdout, stderr string, exitCode int, err error)
}

// execSpawner is the production implementation — uses os/exec.
type execSpawner struct{}

func (execSpawner) Run(ctx context.Context, name string, args []string, env []string, workDir string, stdin string) (string, string, int, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = workDir
	cmd.Env = env
	if stdin != "" {
		cmd.Stdin = newStringReader(stdin)
	}
	out, err := cmd.Output()
	stdout := string(out)
	stderr := ""
	exitCode := -1
	if cmd.ProcessState != nil {
		exitCode = cmd.ProcessState.ExitCode()
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		stderr = string(exitErr.Stderr)
	}
	return stdout, stderr, exitCode, err
}

// newStringReader returns a stdin-shaped Reader for the given string.
// Just delegates to strings.Reader — earlier versions used a hand-
// rolled implementation that returned a custom "EOF" error instead
// of io.EOF, which made subprocesses (notably bash scripts reading
// from stdin) see a non-standard error and bail with mangled exit
// codes ("exit=0, EOF" telemetry from the 2026-05-06 in-gate test).
func newStringReader(s string) io.Reader {
	return strings.NewReader(s)
}

// DefaultSpawner — real subprocess spawn. Use in production.
func DefaultSpawner() Spawner { return execSpawner{} }

// SpawnPeer is the step-3 entry point. Fresh worktree, recursive-
// escalation guard, per-driver template, timeout, normalized result.
func SpawnPeer(ctx context.Context, cfg SpawnConfig) (*ToolCallResult, error) {
	if cfg.Spawner == nil {
		cfg.Spawner = DefaultSpawner()
	}
	timeout := cfg.SpawnTimeoutSeconds
	if timeout <= 0 {
		timeout = 60
	}

	tmpl, ok := spawnTemplate(cfg.Decision.Candidate.Driver)
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedDriver, cfg.Decision.Candidate.Driver)
	}

	// Fresh worktree. Cleanup runs on success AND failure.
	worktree, err := os.MkdirTemp("", "chitin-peer-spawn-")
	if err != nil {
		return nil, fmt.Errorf("%w: mkdtemp: %v", ErrWorktreeSetup, err)
	}
	defer os.RemoveAll(worktree)

	// Recursive escalation guard. Even if the peer is itself a chitin-
	// gated CLI, it cannot spawn another peer — its gate sees this env
	// var and short-circuits the escalation logic.
	env := append(os.Environ(), "CHITIN_NO_ESCALATE=1")

	escalationID := newEscalationID()
	startedAt := time.Now()

	spawnCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	stdout, stderr, exitCode, runErr := cfg.Spawner.Run(
		spawnCtx,
		tmpl.Command,
		tmpl.ArgsFor(cfg.Decision.Candidate.Model),
		env,
		worktree,
		cfg.PromptText,
	)
	durationMs := time.Since(startedAt).Milliseconds()

	prov := Provenance{
		EscalationID:     escalationID,
		WorkerWorkflowID: cfg.Request.WorkerWorkflowID,
		TriggerSignal:    cfg.Request.Signal,
		Severity:         cfg.Request.Severity,
		Route:            cfg.Decision.Rule.Route,
		Candidate:        cfg.Decision.Candidate,
		SpawnedAt:        startedAt,
		DurationMs:       durationMs,
		PeerExitCode:     exitCode,
		WorktreePath:     worktree,
	}

	// Distinguish timeout from generic spawn failure — they have
	// different operational meanings (timeout = peer was alive but
	// slow; failure = peer didn't run or crashed).
	if errors.Is(spawnCtx.Err(), context.DeadlineExceeded) {
		return nil, fmt.Errorf("%w after %ds (escalation_id=%s)",
			ErrSpawnTimeout, timeout, escalationID)
	}
	if runErr != nil {
		return nil, fmt.Errorf("%w (escalation_id=%s, exit=%d): %v",
			ErrSpawnFailed, escalationID, exitCode, runErr)
	}

	return &ToolCallResult{
		Content:       stdout,
		Provenance:    prov,
		RawPeerStdout: stdout,
		RawPeerStderr: stderr,
	}, nil
}

// newEscalationID — short, URL-safe, sortable. 16 bytes hex = 32 chars.
// Not a UUID because we don't need cross-machine uniqueness; locally-
// unique per process is enough (escalations are recorded with the
// worker's workflow_id which adds external uniqueness).
func newEscalationID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// Fallback: timestamp-based. Not collision-safe under high
		// concurrency but the gate is single-tool-call-at-a-time per
		// worker so the worst case is a duplicate ID across runs.
		return fmt.Sprintf("esc-%d", time.Now().UnixNano())
	}
	return "esc-" + hex.EncodeToString(b)
}

// copilotChatHelperPath resolves to the operator's
// scripts/peer-copilot-chat.sh. Honors CHITIN_REPO env override (used
// by tests + operators with non-default install paths). Default is
// $HOME/workspace/chitin/scripts/peer-copilot-chat.sh.
//
// Why a string instead of always-look-up: spawnTemplates is a package
// var initialized at import time. Resolving the path then-once is
// fine — operators don't move the chitin repo mid-run. If they
// did, they'd restart the kernel anyway.
func copilotChatHelperPath() string {
	repo := os.Getenv("CHITIN_REPO")
	if repo == "" {
		home, _ := os.UserHomeDir()
		repo = home + "/workspace/chitin"
	}
	return repo + "/scripts/peer-copilot-chat.sh"
}

// ─── Per-driver spawn templates ────────────────────────────────────────────
//
// Each template is the SHAPE of how to invoke that CLI for an
// escalation. Templates are intentionally minimal — the prompt
// composition (which is the operator-tunable part) is the gate
// caller's job; templates just carry the raw command + arg shape.
//
// Add new drivers by:
//   1. Define a SpawnTemplate value
//   2. Register in spawnTemplates map
// Tests in spawn_peer_test.go cover each driver's args.

type SpawnTemplate struct {
	// Command is the binary name (PATH-resolved by exec).
	Command string

	// ArgsFor returns the argv tail for this template, with `model`
	// substituted. Returned slice is fresh per call (no shared state).
	ArgsFor func(model string) []string
}

// spawnTemplates registers the supported drivers. Drivers absent from
// this map return ErrUnsupportedDriver — explicit fail-closed.
var spawnTemplates = map[string]SpawnTemplate{
	"claude": {
		Command: "claude",
		ArgsFor: func(model string) []string {
			return []string{
				"-p",
				"--model", model,
				"--output-format", "text",
				"--dangerously-skip-permissions",
			}
		},
	},
	"copilot": {
		// scripts/peer-copilot-chat.sh — non-interactive Copilot Chat
		// API invocation. The previous template used `gh copilot
		// suggest -t shell` which requires a TTY (verified 2026-05-06
		// in-gate test: returned exit=1, fail-open kicked in
		// correctly). This helper hits api.githubcopilot.com/chat/
		// completions directly with `gh auth token` as the bearer —
		// same pattern as python/analysis/operator_matrix.py
		// probe_copilot, proven to work for Pro + Enterprise plans.
		//
		// The helper script is resolved via copilotChatHelperPath()
		// which honors CHITIN_REPO env (default $HOME/workspace/chitin)
		// so the script is found regardless of where the kernel binary
		// is installed.
		Command: copilotChatHelperPath(),
		ArgsFor: func(model string) []string {
			return []string{model}
		},
	},
	"codex": {
		Command: "codex",
		ArgsFor: func(model string) []string {
			return []string{"exec", "-m", model, "--skip-git-repo-check"}
		},
	},
	"gemini": {
		Command: "gemini",
		ArgsFor: func(model string) []string {
			return []string{"-p", "", "--model", model, "--yolo"}
		},
	},
}

func spawnTemplate(driver string) (SpawnTemplate, bool) {
	t, ok := spawnTemplates[driver]
	return t, ok
}
