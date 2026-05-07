package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/chitinhq/chitin/go/execution-kernel/internal/driver/claudecode"
	"github.com/chitinhq/chitin/go/execution-kernel/internal/router"
)

// runRouterHookStdin is the production entry point for the
// router-augmented PreToolUse hook. Wires real stdin/stdout/os.Exit
// around evalRouterHookStdin (the pure core).
//
// The pipeline:
//   1. Capture the kernel verdict (calls evalHookStdin internally
//      with a buffered stdout so we can re-process the result)
//   2. Load the router policy from chitin.yaml
//   3. If policy disabled OR kernel denied + advisor not configured
//      for kernel_denied → emit kernel verdict directly (fast path)
//   4. Run heuristics (blast-radius + floundering) per policy
//   5. If any heuristic fired (or kernel denied + advisor wants it)
//      → call advisor via `claude -p`
//   6. Compose final response: kernel verdict + advisor nudge if any
//
// Cold-start target: ~10ms (heuristics are pure Go); only the
// advisor call (when fired) takes seconds (LLM latency).
func runRouterHookStdin(agent, envelopeFlag, policyFile string, requirePolicy, noRecord bool) {
	code := evalRouterHookStdin(os.Stdin, os.Stdout, os.Stderr, agent, envelopeFlag, policyFile, requirePolicy, noRecord)
	os.Exit(code)
}

func evalRouterHookStdin(r io.Reader, out, errOut io.Writer, agent, envelopeFlag, policyFile string, requirePolicy, noRecord bool) int {
	if agent == "" {
		agent = "claude-code"
	}

	// Read stdin once — we reuse for both the kernel evaluator and
	// the router pipeline.
	in, err := io.ReadAll(r)
	if err != nil {
		writeJSONLine(errOut, map[string]string{"error": "router_hook_read_stdin", "message": err.Error()})
		return claudecode.ExitNonBlockError
	}

	// Parse to peek at the cwd / session_id for policy + heuristics
	var payload claudecode.HookInput
	if err := json.Unmarshal(in, &payload); err != nil {
		writeJSONLine(errOut, map[string]string{"error": "router_hook_parse_stdin", "message": err.Error()})
		return claudecode.ExitNonBlockError
	}
	cwd := payload.Cwd
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	absCwd, _ := filepath.Abs(cwd)

	// Load router policy (separate from gov.Policy — this is the
	// router's own configuration surface)
	policy := router.LoadPolicy(absCwd)

	// Step 1: kernel verdict via evalHookStdin's pure core
	var kernelOut bytes.Buffer
	kernelCode := evalHookStdin(bytes.NewReader(in), &kernelOut, errOut, agent, envelopeFlag, policyFile, requirePolicy, noRecord)

	// Fast path: router policy disabled → emit kernel verdict directly
	if !policy.Enabled {
		if kernelOut.Len() > 0 {
			_, _ = out.Write(kernelOut.Bytes())
		}
		return kernelCode
	}

	// Step 2: heuristics
	hookInput := router.HookInput{
		HookEventName: payload.HookEventName,
		ToolName:      payload.ToolName,
		ToolInput:     payload.ToolInput,
		Cwd:           cwd,
		SessionID:     payload.SessionID,
	}
	outcome := router.HeuristicOutcome{}
	if cfg, ok := policy.Heuristics["blast_radius"]; ok && cfg.Enabled {
		score := router.ScoreBlastRadius(hookInput, cfg.Threshold)
		outcome.BlastRadius = &score
		if score.Fired {
			outcome.AnyFired = true
		}
	}
	if cfg, ok := policy.Heuristics["floundering"]; ok && cfg.Enabled {
		events := router.ReadChainEvents(payload.SessionID)
		score := router.DetectFloundering(events, router.FlounderingThresholds{
			MaxLoopCount:    cfg.MaxLoopCount,
			MaxStallSeconds: cfg.MaxStallSeconds,
		}, time.Now())
		outcome.Floundering = &score
		if score.Fired {
			outcome.AnyFired = true
		}
	}

	// Run plugins (operator-declared, in any runtime). Each plugin
	// is its own subprocess; failures fall through (logged to
	// stderr; treated as no-signal). Concurrent execution caps total
	// plugin wall time at the slowest plugin's timeout.
	var pluginResults []router.NamedHeuristicScore
	if len(policy.Plugins) > 0 {
		pluginResults = router.RunPlugins(context.Background(), policy.Plugins, policy.PluginsTrust, hookInput, errOut)
		for _, r := range pluginResults {
			if r.Score.Fired {
				outcome.AnyFired = true
			}
		}
	}

	// Pre-action analysis short-circuit: if any plugin fired with
	// block=true, deny immediately with the plugin's reason. The
	// advisor is NOT consulted — pre-action plugins have
	// authoritative verdict (e.g., "no commit until tests pass" is
	// a deterministic check, not a judgment call).
	for _, r := range pluginResults {
		if r.Score.Fired && r.Block {
			composed := map[string]string{
				"decision": "block",
				"reason":   "pre-action-analysis (" + r.Name + "): " + r.Score.Reason,
			}
			body, _ := json.Marshal(composed)
			_, _ = out.Write(body)
			_, _ = out.Write([]byte{'\n'})
			writeRouterTelemetry(errOut, "pre-action-block", outcome, false, false)
			return claudecode.ExitBlock
		}
	}

	// Decide whether to invoke the advisor
	kernelDeny := kernelCode == claudecode.ExitBlock
	wantAdvisor := false
	if policy.Advisor.Enabled {
		for _, trigger := range policy.Advisor.When {
			switch trigger {
			case "blast_radius_above_threshold":
				if outcome.BlastRadius != nil && outcome.BlastRadius.Fired {
					wantAdvisor = true
				}
			case "floundering_detected":
				if outcome.Floundering != nil && outcome.Floundering.Fired {
					wantAdvisor = true
				}
			case "kernel_denied":
				if kernelDeny {
					wantAdvisor = true
				}
			case "plugin_fired":
				for _, r := range pluginResults {
					if r.Score.Fired {
						wantAdvisor = true
						break
					}
				}
			}
			if wantAdvisor {
				break
			}
		}
	}

	if !wantAdvisor {
		// No advisor needed → emit kernel verdict (fast)
		if kernelOut.Len() > 0 {
			_, _ = out.Write(kernelOut.Bytes())
		}
		// Log heuristic outcome to stderr for telemetry even if advisor skipped
		if outcome.AnyFired {
			writeRouterTelemetry(errOut, "heuristic-fired-no-advisor", outcome, kernelDeny, false)
		}
		return kernelCode
	}

	// Step 3: call advisor
	question := "Heuristic flagged this action. Is the agent on track, or should it pause/escalate?"
	if kernelDeny {
		var krDecision struct {
			Reason string `json:"reason"`
		}
		_ = json.Unmarshal(kernelOut.Bytes(), &krDecision)
		question = fmt.Sprintf(
			"The kernel denied this action: %s. Should the agent be re-routed or is this denial correct?",
			krDecision.Reason,
		)
	}
	advisorReq := router.AdvisorRequest{
		Question:         question,
		Context:          fmt.Sprintf("Session: %s. Heuristic outcome attached.", payload.SessionID),
		ProposedAction:   hookInput,
		HeuristicOutcome: outcome,
		ChainDepth:       0,
	}
	ctx := context.Background()
	advice, advErr := router.CallAdvisor(ctx, advisorReq, "", 60*time.Second)
	if advErr != nil || advice == nil {
		writeJSONLine(errOut, map[string]string{
			"warning":         "router_advisor_failed",
			"err":             errStringFor(advErr),
			"kernel_decision": kernelDecisionLabel(kernelDeny),
		})
		// Fall through to kernel verdict
		if kernelOut.Len() > 0 {
			_, _ = out.Write(kernelOut.Bytes())
		}
		return kernelCode
	}

	// Step 4: compose
	if advice.Verdict == "takeover" {
		// Force a deny with the advisor's nudge as the reason. If
		// the advisor also flipped Escalate, attach an
		// `escalation_requested: true` marker so downstream
		// consumers (the Temporal activity, the dispatcher's tier
		// ladder) can spawn a higher-tier driver instead of
		// surfacing the deny to a human.
		composed := map[string]interface{}{"decision": "block", "reason": advice.Nudge}
		if advice.Escalate {
			composed["escalation_requested"] = true
		}

		// Per docs/design/2026-05-06-kernel-gate-escalation.md (step 4):
		// when chitin-routes.yaml has escalation.enabled=true AND the
		// gate would have escalated, ALSO synchronously spawn a peer
		// CLI and return its output as the deny message body.
		// CHITIN_NO_ESCALATE=1 in the spawned env prevents recursive
		// peer-spawn (peer can be gated/denied/advised but cannot
		// itself trigger another peer).
		// Fail-open: ANY error in the spawn path falls back to today's
		// deny+escalation_requested behavior. The kernel never bricks.
		if advice.Escalate && os.Getenv("CHITIN_NO_ESCALATE") != "1" {
			tryInGateSpawn(out, errOut, &composed, payload, advice, outcome, cwd)
		}

		body, _ := json.Marshal(composed)
		_, _ = out.Write(body)
		_, _ = out.Write([]byte{'\n'})
		writeRouterTelemetry(errOut, "advisor-takeover", outcome, kernelDeny, advice.Escalate)
		return claudecode.ExitBlock
	}

	// continue: append nudge to the kernel output's reason field
	if kernelOut.Len() > 0 {
		var kernelObj map[string]interface{}
		if err := json.Unmarshal(kernelOut.Bytes(), &kernelObj); err == nil {
			existing, _ := kernelObj["reason"].(string)
			if existing != "" {
				kernelObj["reason"] = advice.Nudge + "\n\nKernel: " + existing
			} else {
				kernelObj["reason"] = advice.Nudge
			}
			if advice.Escalate {
				kernelObj["escalation_requested"] = true
			}
			body, _ := json.Marshal(kernelObj)
			_, _ = out.Write(body)
			_, _ = out.Write([]byte{'\n'})
		} else {
			// Kernel output not JSON — emit as-is
			_, _ = out.Write(kernelOut.Bytes())
		}
	} else if kernelCode == claudecode.ExitAllow {
		// Kernel was silent (allow). Emit the advisor's nudge as a hint
		// via the documented hookSpecificOutput channel. Note: the
		// escalation_requested marker is intentionally NOT emitted
		// here — the only consumer (Temporal activity) reads it from
		// the deny-path chain envelope, and adding it to the allow
		// stdout risks polluting Claude Code's tool response with a
		// field it doesn't expect. Telemetry still records the flag
		// in stderr below.
		composed := map[string]interface{}{"hookSpecificOutput": advice.Nudge}
		body, _ := json.Marshal(composed)
		_, _ = out.Write(body)
		_, _ = out.Write([]byte{'\n'})
	}
	writeRouterTelemetry(errOut, "advisor-allow", outcome, kernelDeny, advice.Escalate)
	return kernelCode
}

// writeRouterTelemetry emits one structured JSONL line per
// router-hook invocation. The escalate field is always present
// (false on paths that don't involve the advisor) so downstream
// consumers see a stable schema across emit kinds.
func writeRouterTelemetry(errOut io.Writer, kind string, outcome router.HeuristicOutcome, kernelDeny, escalate bool) {
	out := map[string]interface{}{
		"ts":                time.Now().UTC().Format(time.RFC3339),
		"level":             "info",
		"component":         "router-hook",
		"msg":               kind,
		"kernel_denied":     kernelDeny,
		"heuristic_outcome": outcome,
		"escalate":          escalate,
	}
	body, _ := json.Marshal(out)
	_, _ = errOut.Write(body)
	_, _ = errOut.Write([]byte{'\n'})
}

func errStringFor(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func kernelDecisionLabel(deny bool) string {
	if deny {
		return "deny"
	}
	return "allow"
}

// cmdRouter dispatches `chitin-kernel router <subcommand>`. Today only
// `evaluate --hook-stdin` is supported.
func cmdRouter(args []string) {
	if len(args) < 1 {
		exitErr("router_no_subcommand", "usage: chitin-kernel router <evaluate> [flags]")
	}
	switch args[0] {
	case "evaluate":
		cmdRouterEvaluate(args[1:])
	default:
		exitErr("router_unknown_subcommand", args[0])
	}
}

func cmdRouterEvaluate(args []string) {
	// Minimal flag set — mirrors the gate evaluate hook flags
	agent := "claude-code"
	envelopeFlag := ""
	policyFile := ""
	requirePolicy := false
	hookStdin := false
	noRecord := false
	for _, a := range args {
		switch {
		case a == "--hook-stdin":
			hookStdin = true
		case strings.HasPrefix(a, "--agent="):
			agent = a[len("--agent="):]
		case strings.HasPrefix(a, "--envelope="):
			envelopeFlag = a[len("--envelope="):]
		case strings.HasPrefix(a, "--policy-file="):
			policyFile = a[len("--policy-file="):]
		case a == "--require-policy":
			requirePolicy = true
		case a == "--no-record":
			noRecord = true
		}
	}
	if !hookStdin {
		exitErr("router_evaluate_missing_args", "--hook-stdin required (other modes deferred)")
	}
	runRouterHookStdin(agent, envelopeFlag, policyFile, requirePolicy, noRecord)
}
