package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/chitinhq/chitin/go/execution-kernel/internal/cost"
	"github.com/chitinhq/chitin/go/execution-kernel/internal/driver/claudecode"
	"github.com/chitinhq/chitin/go/execution-kernel/internal/gov"
	"github.com/chitinhq/chitin/go/execution-kernel/internal/tier"
)

// runHookStdin is the production entry point for the Claude Code
// PreToolUse hook. It wires real stdin/stdout/os.Exit around the pure
// evalHookStdin core. The split keeps evalHookStdin testable in-process
// while production gets the full os-level behavior.
func runHookStdin(agent, envelopeFlag string, requirePolicy bool) {
	code := evalHookStdin(os.Stdin, os.Stdout, os.Stderr, agent, envelopeFlag, requirePolicy)
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
func evalHookStdin(r io.Reader, out, errOut io.Writer, agent, envelopeFlag string, requirePolicy bool) int {
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
	defer claudecode.SetWarnSink(nil)

	action, err := claudecode.Normalize(payload)
	if err != nil {
		writeJSONLine(errOut, map[string]string{"error": "hook_normalize", "message": err.Error()})
		return claudecode.ExitNonBlockError
	}

	policy, _, err := gov.LoadWithInheritance(absCwd)
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

	gate := &gov.Gate{
		Policy: policy, Counter: counter,
		LogDir: cdir, Cwd: absCwd,
		ClassifyTier: tier.Route,
		EstimateCost: func(a gov.Action, _ string) gov.CostDelta {
			return cost.Estimate(a, agent, rates)
		},
	}
	d := gate.Evaluate(action, agent, envelope)

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
