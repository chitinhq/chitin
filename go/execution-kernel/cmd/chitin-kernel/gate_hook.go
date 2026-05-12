package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/chitinhq/chitin/go/execution-kernel/internal/canon"
	"github.com/chitinhq/chitin/go/execution-kernel/internal/cost"
	"github.com/chitinhq/chitin/go/execution-kernel/internal/driver/claudecode"
	"github.com/chitinhq/chitin/go/execution-kernel/internal/driver/codex"
	"github.com/chitinhq/chitin/go/execution-kernel/internal/driver/gemini"
	"github.com/chitinhq/chitin/go/execution-kernel/internal/driver/hermes"
	"github.com/chitinhq/chitin/go/execution-kernel/internal/gov"
	"github.com/chitinhq/chitin/go/execution-kernel/internal/tier"
)

// reChitinAdminCmd matches a shell command whose first executable token
// is the bare `chitin-kernel` binary, optionally preceded by `env VAR=val`
// or inline `VAR=val` env prefixes. Path-prefixed forms
// (`/usr/local/bin/chitin-kernel`) intentionally don't match — install
// puts chitin-kernel on PATH, and operators using literal paths have
// explicit invocation control. See isChitinAdminCommand for rationale.
var reChitinAdminCmd = regexp.MustCompile(
	`^(?:\s*(?:env\s+|[A-Za-z_][A-Za-z0-9_]*=\S*\s+))*chitin-kernel(?:\s|$)`,
)

// isChitinAdminCommand returns true when action's leading shell segment
// invokes chitin-kernel directly. Such commands bypass envelope spend
// (so an exhausted/closed envelope can be recovered from inside the
// gated session via `envelope grant`) but are still subject to policy
// evaluation — an operator who wants to deny chitin-kernel invocations
// from the agent can add a shell.exec policy rule.
//
// Accepts any shell-derived action type (ActShellExec OR a re-tag like
// ActFileRecursiveDelete that fired on a chained-segment match). Without
// this breadth, `chitin-kernel envelope list && rm -rf /` would fail the
// admin matcher because the rm-rf re-tag changes the action type away
// from ActShellExec — the admin matcher needs to recognize that the
// leading segment is still chitin-kernel.
func isChitinAdminCommand(a gov.Action) bool {
	switch a.Type {
	case gov.ActShellExec, gov.ActFileRecursiveDelete:
		return reChitinAdminCmd.MatchString(strings.TrimSpace(a.Target))
	}
	return false
}

type chitinAdminClass string

const (
	chitinAdminNone     chitinAdminClass = ""
	chitinAdminRead     chitinAdminClass = "read"
	chitinAdminMutation chitinAdminClass = "mutation"
)

func classifyChitinAdminCommand(a gov.Action) chitinAdminClass {
	switch a.Type {
	case gov.ActShellExec, gov.ActFileRecursiveDelete:
	default:
		return chitinAdminNone
	}

	// Scan every segment: mutation in any chitin-kernel segment wins;
	// a piped read (`decisions recent | head`) stays read because the
	// non-chitin segments are skipped and the pipe-to-bash attack vector
	// is caught by shell.exec + file.write rules.
	pipeline := canon.ParseAST(strings.TrimSpace(a.Target))
	result := chitinAdminNone
	for i := range pipeline.Segments {
		cmd := pipeline.Segments[i].Command
		if !isChitinKernelCommand(cmd.Raw) {
			continue
		}
		cls := classifyChitinKernelSegment(cmd)
		if cls == chitinAdminMutation {
			return chitinAdminMutation
		}
		if cls != chitinAdminNone {
			result = cls
		}
	}
	return result
}

func classifyChitinKernelSegment(cmd canon.Command) chitinAdminClass {
	fields := chitinKernelFields(cmd.Raw)
	if len(fields) < 2 {
		return chitinAdminMutation
	}
	subcommandIndex := chitinKernelSubcommandIndex(fields)
	if subcommandIndex >= len(fields) {
		return chitinAdminRead
	}
	switch fields[subcommandIndex] {
	case "gate":
		if len(fields) > subcommandIndex+1 && fields[subcommandIndex+1] == "status" {
			return chitinAdminRead
		}
		return chitinAdminMutation
	case "envelope":
		if len(fields) > subcommandIndex+1 {
			switch fields[subcommandIndex+1] {
			case "inspect", "list", "tail":
				return chitinAdminRead
			}
		}
		return chitinAdminMutation
	case "decisions", "chain", "health", "chain-info", "chain-verify", "router", "simulate":
		return chitinAdminRead
	default:
		return chitinAdminMutation
	}
}

func chitinKernelSubcommandIndex(fields []string) int {
	i := 1
	for i < len(fields) {
		field := fields[i]
		name := field
		if before, _, ok := strings.Cut(field, "="); ok {
			name = before
		}
		if chitinKernelGlobalFlagTakesValue(name) {
			i++
			if field == name && i < len(fields) {
				i++
			}
			continue
		}
		if chitinKernelGlobalBoolFlag(name) {
			i++
			continue
		}
		return i
	}
	return i
}

func chitinKernelGlobalBoolFlag(flag string) bool {
	switch flag {
	case "--help", "--version", "--verbose", "-h", "-V", "-v":
		return true
	default:
		return false
	}
}

func chitinKernelGlobalFlagTakesValue(flag string) bool {
	switch flag {
	case "--config":
		return true
	default:
		return false
	}
}

func isChitinKernelCommand(raw string) bool {
	return len(chitinKernelFields(raw)) > 0
}

func chitinKernelFields(raw string) []string {
	fields := strings.Fields(strings.TrimSpace(raw))
	for len(fields) > 0 {
		if fields[0] == "env" || strings.Contains(fields[0], "=") {
			fields = fields[1:]
			continue
		}
		break
	}
	if len(fields) > 0 && fields[0] == "command" {
		fields = fields[1:]
	}
	if len(fields) == 0 || filepath.Base(fields[0]) != "chitin-kernel" {
		return nil
	}
	fields[0] = "chitin-kernel"
	return fields
}

func authorityCanMutateGovernance(authority string) bool {
	switch authority {
	case "supervisor", "operator", "system":
		return true
	default:
		return false
	}
}

// runHookStdin is the production entry point for the Claude Code
// PreToolUse hook. It wires real stdin/stdout/os.Exit around the pure
// evalHookStdin core. The split keeps evalHookStdin testable in-process
// while production gets the full os-level behavior.
func runHookStdin(agent, envelopeFlag, policyFile string, requirePolicy, noRecord bool) {
	code := evalHookStdin(os.Stdin, os.Stdout, os.Stderr, agent, envelopeFlag, policyFile, requirePolicy, noRecord)
	os.Exit(code)
}

// evalHookStdin is the pure hook-driver core. Reads one PreToolUse
// payload from r, runs governance + envelope spend, writes the hook
// response to out (or warning JSON to errOut), and returns the exit code:
//
//	0 — allow (out empty)
//	2 — block (out is `{"decision":"block","reason":"..."}`)
//	1 — non-blocking error (out empty; errOut has the diagnostic)
//
// requirePolicy controls the no-chitin.yaml-in-cwd behavior:
//   - false (default): fail open with a stderr warning. Convenient but
//     means an operator running `claude` in any directory without a
//     policy gets ungoverned tool calls.
//   - true: fail closed (exit 2 block) so chitin governance is never
//     silently absent. Operators who want the strict guarantee
//     install with --require-policy.
//
// Latency-sensitive: every Claude Code tool call cold-starts this code.
// The acceptance gate is p95 ≤ 100ms cold-start; if not met, daemon
// mode (gate.sock) is the next step.
//
// PERF NOTE: this opens two sqlite handles against ~/.chitin/gov.db
// (Counter + BudgetStore). Sharing one *sql.DB would halve cold-start
// open cost. Deferred until Milestone D's 8-shim stress test surfaces
// real contention numbers — at 3ms p95 today the headroom is ample.
func evalHookStdin(r io.Reader, out, errOut io.Writer, agent, envelopeFlag, policyFile string, requirePolicy, noRecord bool) int {
	if agent == "" {
		agent = "claude-code"
	}

	in, err := io.ReadAll(r)
	if err != nil {
		writeJSONLine(errOut, map[string]string{"error": "hook_read_stdin", "message": err.Error()})
		return claudecode.ExitNonBlockError
	}
	var payload claudecode.HookInput
	if err := json.Unmarshal(in, &payload); err != nil {
		writeJSONLine(errOut, map[string]string{"error": "hook_parse_stdin", "message": err.Error()})
		return claudecode.ExitNonBlockError
	}

	cwd := payload.Cwd
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	absCwd, _ := filepath.Abs(cwd)

	// Route normalize warnings (wrong-type fields, etc.) to the same
	// stderr stream so operators see malformed payloads explicitly
	// rather than via a silent default-deny.
	claudecode.SetWarnSink(errOut)
	codex.SetWarnSink(errOut)
	gemini.SetWarnSink(errOut)
	hermes.SetWarnSink(errOut)
	defer claudecode.SetWarnSink(nil)
	defer codex.SetWarnSink(nil)
	defer gemini.SetWarnSink(nil)
	defer hermes.SetWarnSink(nil)

	// Dispatch by agent: codex + gemini use different tool names
	// than Claude Code (apply_patch vs Edit/Write, run_shell_command
	// vs Bash, etc.). Wire shape on stdin is byte-identical across
	// the three; we copy the relevant fields into the vendor-specific
	// HookInput when --agent matches.
	var action gov.Action
	switch agent {
	case "codex":
		cPayload := codex.HookInput{
			SessionID:      payload.SessionID,
			TranscriptPath: payload.TranscriptPath,
			Cwd:            payload.Cwd,
			HookEventName:  payload.HookEventName,
			ToolName:       payload.ToolName,
			ToolInput:      payload.ToolInput,
		}
		action, err = codex.Normalize(cPayload)
	case "gemini":
		gPayload := gemini.HookInput{
			SessionID:      payload.SessionID,
			TranscriptPath: payload.TranscriptPath,
			Cwd:            payload.Cwd,
			HookEventName:  payload.HookEventName,
			ToolName:       payload.ToolName,
			ToolInput:      payload.ToolInput,
		}
		action, err = gemini.Normalize(gPayload)
	case "hermes":
		hPayload := hermes.HookInput{
			SessionID:      payload.SessionID,
			TranscriptPath: payload.TranscriptPath,
			Cwd:            payload.Cwd,
			HookEventName:  payload.HookEventName,
			ToolName:       payload.ToolName,
			ToolInput:      payload.ToolInput,
		}
		action, err = hermes.Normalize(hPayload)
	default:
		action, err = claudecode.Normalize(payload)
	}
	if err != nil {
		writeJSONLine(errOut, map[string]string{"error": "hook_normalize", "message": err.Error()})
		return claudecode.ExitNonBlockError
	}

	// --policy-file (or $CHITIN_POLICY_FILE, both passed as policyFile by
	// main.go cmdGateEvaluate) bypasses the cwd-walk inheritance lookup
	// and loads an explicit policy. Mirrors the non-hook gate-evaluate
	// path's behavior (main.go:843-847). Without this thread-through, an
	// operator running `gate evaluate --hook-stdin --policy-file X` got
	// X silently ignored — this caused 2026-05-06's hook-capture replay
	// to apply per-cwd inherited policy instead of chitin's policy,
	// muddying the counterfactual analysis. Found while replaying the
	// 17-day Curie capture dataset.
	var policy gov.Policy
	if policyFile != "" {
		policy, err = gov.LoadPolicyFile(policyFile)
	} else {
		policy, _, err = gov.LoadWithInheritance(absCwd)
	}
	if err != nil {
		errMsg := err.Error()
		if !strings.HasPrefix(errMsg, "no_policy_found") {
			writeBlockReason(out, "policy_invalid: "+errMsg)
			return claudecode.ExitBlock
		}
		// No policy in cwd. Default behavior is fail-open with a stderr
		// warning so operators running `claude` in arbitrary dirs aren't
		// blocked on every tool. With --require-policy, fail closed —
		// the operator chose strict-mode and must scaffold a chitin.yaml
		// (or run from a policy-covered cwd) before claude works.
		if requirePolicy {
			writeBlockReason(out, "chitin: no chitin.yaml found from cwd up; --require-policy refuses to allow ungoverned tool calls")
			return claudecode.ExitBlock
		}
		writeJSONLine(errOut, map[string]string{
			"warning": "no_policy_found",
			"note":    "chitin governance hook fired in cwd without chitin.yaml; allowing (install with --require-policy to fail closed)",
		})
		return claudecode.ExitAllow
	}

	cdir := chitinDir()
	_ = os.MkdirAll(cdir, 0o755)
	dbPath := filepath.Join(cdir, "gov.db")

	counter, err := gov.OpenCounter(dbPath)
	if err != nil {
		writeJSONLine(errOut, map[string]string{"error": "hook_counter_open", "message": err.Error()})
		return claudecode.ExitNonBlockError
	}
	defer counter.Close()

	envelope, store, err := resolveEnvelope(cdir, dbPath, envelopeFlag)
	if err != nil {
		writeBlockReason(out, "chitin: "+err.Error())
		return claudecode.ExitBlock
	}
	if store != nil {
		defer store.Close()
	}

	rates := loadRates(absCwd, errOut)
	identity := gov.FingerprintContextFromEnv()

	gate := &gov.Gate{
		Policy: policy, Counter: counter,
		LogDir: cdir, Cwd: absCwd,
		ClassifyTier: tier.Route,
		EstimateCost: func(a gov.Action, _ string) gov.CostDelta {
			return cost.Estimate(a, agent, rates)
		},
		// P2 routing-as-learning-system: stamp fingerprint dims onto every
		// Decision the gate writes when the dispatching agent supplies
		// them via env. FingerprintContextFromEnv centralizes the env
		// read so all Gate constructors stay in sync.
		Fingerprint: identity,
		// noRecord=true suppresses Counter.RecordDenial + WriteLog inside
		// gov.Gate. We additionally skip OnDecision wiring + envelope
		// spend below so a smoke evaluation has zero persistent side
		// effects regardless of which path it traverses.
		NoRecord: noRecord,
	}
	if classifyChitinAdminCommand(action) == chitinAdminMutation {
		effectiveAuthority := gov.ResolveTrustedAuthority(identity, policy.Authority)
		if !authorityCanMutateGovernance(effectiveAuthority) {
			policy.Rules = append(policy.Rules, gov.Rule{
				ID:         "governance-mutation-authority-required",
				Action:     gov.ActionMatcher{string(gov.ActShellExec), string(gov.ActFileRecursiveDelete)},
				Effect:     "deny",
				Target:     action.Target,
				Reason:     "governance mutation requires trusted supervisor/operator/system authority; self-reset is not permitted",
				Suggestion: "Ask the operator or a trusted supervisor to perform the governance mutation.",
			})
			if policy.InvariantModes == nil {
				policy.InvariantModes = map[string]string{}
			}
			policy.InvariantModes["governance-mutation-authority-required"] = "enforce"
			gate.Policy = policy
		}
	}
	// Operator-approval escalation wiring removed in cull Phase 3
	// (2026-05-08). Hermes' tools/approval.py provides operator-prompt
	// + reply-parse + persistent allowlist natively; chitin no longer
	// maintains its own pending_approvals + bridge POST + watcher.
	// F4 addendum: wire OnDecision to emit a `decision` chain event via
	// the canonical path. chain_id = HookInput.SessionID when available
	// (Claude Code provides one); otherwise a fresh UUID. F4 OTEL
	// projection picks up the event automatically when configured.
	// Skip wiring if chain_id can't be resolved (rand failure) — agent name
	// is preserved separately as AgentInstanceID; surface is the driver
	// origin ("claude-code"), not the agent identifier.
	//
	// noRecord=true also skips this wiring: OnDecision writes a v2 chain
	// event via the decision emitter, which is a second persistence
	// path that NoRecord must mute. Mirrors cmdGateEvaluate's behavior.
	if !noRecord {
		hookChainID := payload.SessionID
		if hookChainID == "" {
			hookChainID = newChainID()
		}
		if hookChainID != "" {
			// Surface label tracks the dispatched driver so chain
			// telemetry doesn't mislabel codex/gemini events as
			// claude-code. The agent flag drives this — claude-code
			// is the default when the flag is empty.
			surface := agent
			if surface == "" {
				surface = "claude-code"
			}
			de, deClose, deErr := newDecisionEmitter(cdir, hookChainID, surface, func() string { return hookChainID })
			if deErr == nil {
				defer deClose()
				gate.OnDecision = de.emitDecision
			}
		}
	}
	// Operator-recovery commands (chitin-kernel envelope grant, use, etc.)
	// pass nil envelope so policy still evaluates but no spend is debited.
	// Without this, an exhausted/closed envelope deadlocks the operator's
	// own recovery surface — the agent's gate hook denies the very
	// `envelope grant` call that would reopen the envelope. Decision is
	// logged without envelope stamping (the call doesn't belong to any
	// envelope's spend); a structured info line goes to errOut so
	// operators auditing the hook see when an exemption fired.
	//
	// noRecord also forces nil envelope so smoke probes never consume
	// budget — without this an operator validating policy via
	// `--no-record --hook-stdin` would still debit the active envelope
	// per probe, surprising the next legitimate caller with reduced
	// remaining calls.
	spendEnvelope := envelope
	if noRecord || isChitinAdminCommand(action) {
		spendEnvelope = nil
		if envelope != nil && !noRecord {
			writeJSONLine(errOut, map[string]string{
				"info":    "chitin_admin_exempt",
				"command": action.Target,
				"note":    "envelope spend skipped; policy still evaluated",
			})
		}
	}
	d := gate.Evaluate(action, agent, spendEnvelope)

	body, code := claudecode.Format(d)
	if len(body) > 0 {
		_, _ = out.Write(body)
		_, _ = out.Write([]byte{'\n'})
	}
	return code
}

// chitinDir returns the chitin state dir. The CHITIN_HOME env var
// override exists primarily for tests to redirect ~/.chitin to a temp
// dir without root-touching real state.
func chitinDir() string {
	if v := os.Getenv("CHITIN_HOME"); v != "" {
		return v
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".chitin")
}

// resolveEnvelope walks the precedence chain:
//  1. envelopeFlag (--envelope=<id>)
//  2. CHITIN_BUDGET_ENVELOPE env var
//  3. <chitinDir>/current-envelope file
//  4. None — returns (nil, nil, nil): gate + audit only, no spend.
func resolveEnvelope(chitinDir, dbPath, envelopeFlag string) (*gov.BudgetEnvelope, *gov.BudgetStore, error) {
	id := envelopeFlag
	if id == "" {
		id = os.Getenv("CHITIN_BUDGET_ENVELOPE")
	}
	if id == "" {
		path := filepath.Join(chitinDir, "current-envelope")
		if b, err := os.ReadFile(path); err == nil {
			id = strings.TrimSpace(string(b))
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, nil, fmt.Errorf("read current-envelope: %w", err)
		}
	}
	if id == "" {
		return nil, nil, nil
	}
	store, err := gov.OpenBudgetStore(dbPath)
	if err != nil {
		return nil, nil, fmt.Errorf("open budget store: %w", err)
	}
	env, err := store.Load(id)
	if err != nil {
		store.Close()
		return nil, nil, fmt.Errorf("load envelope %s: %w", id, err)
	}
	return env, store, nil
}

// loadRates loads cost.RateTable from the cwd's chitin.yaml if present,
// falling back to defaults. Errors are non-fatal: log and use defaults.
// Hook latency is more important than perfectly-current rates.
func loadRates(cwd string, errOut io.Writer) cost.RateTable {
	path := filepath.Join(cwd, "chitin.yaml")
	r, err := cost.LoadRates(path)
	if err != nil {
		writeJSONLine(errOut, map[string]string{"warning": "rates_load", "note": err.Error()})
		return cost.DefaultRates()
	}
	return r
}

func writeBlockReason(out io.Writer, reason string) {
	body, _ := json.Marshal(map[string]string{"decision": "block", "reason": reason})
	_, _ = out.Write(body)
	_, _ = out.Write([]byte{'\n'})
}

func writeJSONLine(out io.Writer, v map[string]string) {
	b, _ := json.Marshal(v)
	_, _ = out.Write(b)
	_, _ = out.Write([]byte{'\n'})
}
