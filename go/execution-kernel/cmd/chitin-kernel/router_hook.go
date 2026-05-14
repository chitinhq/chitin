package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/chitinhq/chitin/go/execution-kernel/internal/chain"
	kdrift "github.com/chitinhq/chitin/go/execution-kernel/internal/drift"
	"github.com/chitinhq/chitin/go/execution-kernel/internal/driver/claudecode"
	"github.com/chitinhq/chitin/go/execution-kernel/internal/emit"
	"github.com/chitinhq/chitin/go/execution-kernel/internal/event"
	"github.com/chitinhq/chitin/go/execution-kernel/internal/gov"
	"github.com/chitinhq/chitin/go/execution-kernel/internal/router"
)

// runRouterHookStdin is the production entry point for the
// router-augmented PreToolUse hook. Wires real stdin/stdout/os.Exit
// around evalRouterHookStdin (the pure core).
//
// Pipeline (post-cull, audit Tier 6 — 2026-05-08):
//  1. Run kernel verdict via evalHookStdin (unchanged).
//  2. Run pure-Go heuristics (blast-radius, floundering, drift) and
//     operator-declared plugins.
//  3. Pre-action plugin block short-circuits with a deny when a
//     plugin fired with block=true (still authoritative — pre-action
//     checks are deterministic, not judgment calls).
//  4. Stamp the heuristic-signal scores onto a gov.Decision row in
//     ~/.chitin/gov-decisions-<utc-date>.jsonl when at least one
//     signal is non-zero, so chain consumers can pick them up.
//  5. Emit the kernel verdict + heuristic telemetry. The kernel
//     never spawns an LLM in-line.
//
// The in-gate `claude -p` advisor was removed: chitin's moat is
// signal computation + the gate, not LLM-running. Hermes ships
// `approvals.mode: smart` for that already; operator-wired chain
// consumers handle the same role for non-hermes drivers. Chitin
// emits; chitin does not consult. See
// docs/decisions/2026-05-08-cull-advisor-out-of-kernel-hot-path.md.
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
	routesPolicy, _ := router.LoadRoutesPolicy(absCwd)

	// Step 1: kernel verdict via evalHookStdin's pure core
	var kernelOut bytes.Buffer
	kernelCode := evalHookStdin(bytes.NewReader(in), &kernelOut, errOut, agent, envelopeFlag, policyFile, requirePolicy, noRecord, false)

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
	var chainEvents []router.ChainEvent
	chainEventsLoaded := false
	if cfg, ok := policy.Heuristics["blast_radius"]; ok && cfg.Enabled {
		score := router.ScoreBlastRadius(hookInput, cfg.Threshold)
		outcome.BlastRadius = &score
		if score.Fired {
			outcome.AnyFired = true
		}
	}
	if cfg, ok := policy.Heuristics["floundering"]; ok && cfg.Enabled {
		chainEvents = router.ReadChainEvents(payload.SessionID)
		chainEventsLoaded = true
		score := router.DetectFloundering(chainEvents, router.FlounderingThresholds{
			MaxLoopCount:    cfg.MaxLoopCount,
			MaxStallSeconds: cfg.MaxStallSeconds,
		}, time.Now())
		outcome.Floundering = &score
		if score.Fired {
			outcome.AnyFired = true
		}
	}
	// Drift is gated on router.heuristics.drift.enabled — when an operator
	// disables it, the detector must not run, score, route, or emit.
	// driftResult stays zero-valued in that case (Eval.Action == ActionNone,
	// Score == 0), which the downstream kill/stamp paths already treat as
	// "no drift signal".
	driftCfg := policy.Heuristics["drift"]
	var driftResult router.DriftResult
	var driftScore router.HeuristicScore
	var driftRoutingDecision string
	if driftCfg.Enabled {
		if !chainEventsLoaded {
			chainEvents = router.ReadChainEvents(payload.SessionID)
		}
		driftResult = router.EvaluateDrift(hookInput, chainEvents, driftCfg)
		driftScore = driftResult.Score
		if driftScore.Fired {
			outcome.AnyFired = true
		}
		driftRoutingDecision = resolveDriftRoutingDecision(routesPolicy, hookInput, driftResult.Eval)
		emitDriftEvents(errOut, agent, hookInput, driftResult.Eval, driftRoutingDecision, noRecord)
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

	kernelVerdict := parseKernelVerdict(kernelOut.Bytes(), kernelCode)

	if driftResult.Eval.Action == kdrift.ActionKill {
		composed := map[string]string{
			"decision": "block",
			"reason":   "drift-kill: " + driftResult.Eval.Reason,
		}
		body, _ := json.Marshal(composed)
		_, _ = out.Write(body)
		_, _ = out.Write([]byte{'\n'})
		stampHeuristicSignals(errOut, agent, hookInput, outcome, driftScore, true, "drift-kill", driftRoutingDecision, noRecord)
		writeRouterTelemetry(errOut, "drift-kill", outcome, kernelVerdict)
		return claudecode.ExitBlock
	}

	// Pre-action analysis short-circuit: if any plugin fired with
	// block=true, deny immediately with the plugin's reason. Pre-
	// action plugins have authoritative verdict (e.g., "no commit
	// until tests pass" is a deterministic check, not a judgment
	// call).
	for _, r := range pluginResults {
		if r.Score.Fired && r.Block {
			composed := map[string]string{
				"decision": "block",
				"reason":   "pre-action-analysis (" + r.Name + "): " + r.Score.Reason,
			}
			body, _ := json.Marshal(composed)
			_, _ = out.Write(body)
			_, _ = out.Write([]byte{'\n'})
			stampHeuristicSignals(errOut, agent, hookInput, outcome, driftScore, true, "pre-action-block:"+r.Name, driftRoutingDecision, noRecord)
			writeRouterTelemetry(errOut, "pre-action-block", outcome, kernelVerdict)
			return claudecode.ExitBlock
		}
	}

	// Stamp the heuristic-signal metadata onto the chain so consumers
	// (hermes' approvals.mode: smart, operator-written cron, custom
	// kanban-dispatched profile, etc.) can read it without spawning
	// any LLM in the gate. Only stamp when at least one signal is
	// non-zero — read-only tools whose scores are trivially zero
	// don't need a stamping row.
	if hasNonZeroSignal(outcome, driftScore) {
		stampHeuristicSignals(errOut, agent, hookInput, outcome, driftScore, kernelVerdict.Denied, kernelDecisionLabel(kernelVerdict.Denied), driftRoutingDecision, noRecord)
	}

	// Emit kernel verdict (deny or allow) — never altered by
	// heuristics. Heuristic signals are advisory; the kernel + plugin
	// pre-block are the only authoritative verdicts.
	if kernelOut.Len() > 0 {
		_, _ = out.Write(kernelOut.Bytes())
	}
	if outcome.AnyFired {
		writeRouterTelemetry(errOut, "heuristic-fired", outcome, kernelVerdict)
	}
	return kernelCode
}

// KernelVerdict is the router's parsed view of the authoritative
// kernel decision. The router remains advisory, but its telemetry must
// preserve the kernel rule and reason so heuristic lines never obscure
// why an action was actually blocked.
type KernelVerdict struct {
	Denied bool
	RuleID string
	Reason string
}

func parseKernelVerdict(body []byte, code int) KernelVerdict {
	v := KernelVerdict{Denied: code == claudecode.ExitBlock}
	if !v.Denied || len(bytes.TrimSpace(body)) == 0 {
		return v
	}
	var parsed struct {
		Decision string `json:"decision"`
		Reason   string `json:"reason"`
		RuleID   string `json:"rule_id"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(body), &parsed); err != nil {
		return v
	}
	if parsed.Decision == "block" {
		v.RuleID = parsed.RuleID
		v.Reason = parsed.Reason
	}
	return v
}

// hasNonZeroSignal returns true if any heuristic produced a non-zero
// score (even sub-threshold) so the router stamps the signal on the
// chain. Sub-threshold scores are still useful: they're the training
// signal for tuning thresholds in the next analysis pass.
func hasNonZeroSignal(o router.HeuristicOutcome, drift router.HeuristicScore) bool {
	if o.BlastRadius != nil && o.BlastRadius.Score > 0 {
		return true
	}
	if o.Floundering != nil && o.Floundering.Score > 0 {
		return true
	}
	if drift.Score > 0 {
		return true
	}
	return false
}

// stampHeuristicSignals writes a router-stamped gov.Decision row to
// the audit log carrying the heuristic-signal scores. This is a
// SECOND row per tool call (the kernel's own row was already written
// by gate.Evaluate inside evalHookStdin); chain consumers join the
// two via (ts, action_target). Suppressed under noRecord so smoke
// probes don't pollute the log.
//
// Mode is "monitor" because the row is advisory: the kernel + plugin
// pre-block produced the authoritative verdict already on the
// preceding row; this row is signal metadata, not enforcement.
func stampHeuristicSignals(errOut io.Writer, agent string, hookInput router.HookInput, outcome router.HeuristicOutcome, drift router.HeuristicScore, kernelDeny bool, ruleID, routingDecision string, noRecord bool) {
	if noRecord {
		return
	}
	d := gov.Decision{
		Allowed: !kernelDeny,
		Mode:    "monitor",
		RuleID:  "router-heuristic:" + ruleID,
		Agent:   agent,
		Ts:      time.Now().UTC().Format(time.RFC3339),
		Action: gov.Action{
			Type:   gov.ActionType("router.signal"),
			Target: hookInput.ToolName + ":" + targetForSignal(hookInput),
		},
	}
	if outcome.BlastRadius != nil {
		d.PredictedBlast = outcome.BlastRadius.Score
	}
	if outcome.Floundering != nil {
		d.FlounderingScore = outcome.Floundering.Score
	}
	d.DriftScore = drift.Score
	d.RoutingDecision = routingDecision

	if err := gov.WriteLog(d, chitinDir()); err != nil {
		writeJSONLine(errOut, map[string]string{
			"warning": "router_heuristic_stamp_failed",
			"err":     err.Error(),
		})
	}
}

func resolveDriftRoutingDecision(routesPolicy router.RoutesPolicy, hookInput router.HookInput, eval kdrift.Evaluation) string {
	switch eval.Action {
	case kdrift.ActionKill:
		return "drift.kill"
	case kdrift.ActionDemote:
		// Demote is route-only in the kernel hook: chitin stamps the routing
		// decision and emits drift events, while substrates decide how to move
		// or resize a running worker.
		if d, err := router.RouteFor(router.RouteRequest{
			Signal:           "drift",
			Severity:         "score>=" + formatScore(eval.Score),
			ToolCall:         hookInput.ToolInput,
			WorkerWorkflowID: "",
		}, routesPolicy); err == nil {
			return "drift.demote:" + d.Rationale
		}
		return "drift.demote"
	default:
		return ""
	}
}

func formatScore(score float64) string {
	return strings.TrimRight(strings.TrimRight(strconvFormatFloat(score), "0"), ".")
}

func strconvFormatFloat(v float64) string {
	return strconv.FormatFloat(v, 'f', 2, 64)
}

func emitDriftEvents(errOut io.Writer, agent string, hookInput router.HookInput, eval kdrift.Evaluation, routingDecision string, noRecord bool) {
	if noRecord || hookInput.SessionID == "" || !eval.Detected {
		return
	}
	detectionPayload := map[string]any{
		"phase":              "detection",
		"tool_name":          hookInput.ToolName,
		"target_path":        eval.Target,
		"score":              eval.Score,
		"reason":             eval.Reason,
		"entry_id":           eval.Intent.EntryID,
		"task_class":         eval.Intent.TaskClass,
		"file_paths":         eval.Intent.FilePaths,
		"turn_count":         eval.State.TurnCount,
		"paths_total":        eval.State.PathsTotal,
		"paths_out_of_scope": eval.State.PathsOutOfScope,
	}
	if err := emitRouterEvent(hookInput.SessionID, agent, detectionPayload); err != nil {
		writeJSONLine(errOut, map[string]string{"warning": "drift_detection_emit_failed", "err": err.Error()})
	}
	if eval.Action == kdrift.ActionNone {
		return
	}
	actionPayload := map[string]any{
		"phase":            "action",
		"action":           string(eval.Action),
		"tool_name":        hookInput.ToolName,
		"target_path":      eval.Target,
		"score":            eval.Score,
		"reason":           eval.Reason,
		"entry_id":         eval.Intent.EntryID,
		"task_class":       eval.Intent.TaskClass,
		"routing_decision": routingDecision,
	}
	if err := emitRouterEvent(hookInput.SessionID, agent, actionPayload); err != nil {
		writeJSONLine(errOut, map[string]string{"warning": "drift_action_emit_failed", "err": err.Error()})
	}
}

func emitRouterEvent(sessionID, surface string, payload map[string]any) error {
	if sessionID == "" {
		return nil
	}
	cdir := chitinDir()
	if err := os.MkdirAll(cdir, 0o755); err != nil {
		return err
	}
	idx, err := chain.OpenIndex(filepath.Join(cdir, "chain_index.sqlite"))
	if err != nil {
		return err
	}
	defer idx.Close()
	if err := idx.RebuildFromJSONL(cdir); err != nil {
		return err
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	em := &emit.Emitter{
		LogPath: filepath.Join(cdir, "events-"+sessionID+".jsonl"),
		Index:   idx,
	}
	ev := &event.Event{
		SchemaVersion:    "2",
		RunID:            sessionID,
		SessionID:        sessionID,
		Surface:          surface,
		AgentInstanceID:  surface,
		AgentFingerprint: hashString(surface),
		EventType:        "drift",
		ChainID:          sessionID,
		ChainType:        "session",
		Ts:               time.Now().UTC().Format(time.RFC3339Nano),
		Labels:           map[string]string{"agent": surface},
		Payload:          body,
	}
	return em.Emit(ev)
}

func hashString(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// targetForSignal extracts a short identifier of what the action
// targets for the signal-row's action_target field. Best-effort —
// chain consumers join on (ts, tool_name) when this is too lossy.
func targetForSignal(input router.HookInput) string {
	if v, ok := input.ToolInput["file_path"].(string); ok && v != "" {
		return v
	}
	if v, ok := input.ToolInput["notebook_path"].(string); ok && v != "" {
		return v
	}
	if v, ok := input.ToolInput["command"].(string); ok && v != "" {
		// Truncate so the audit row stays bounded.
		if len(v) > 200 {
			return v[:200]
		}
		return v
	}
	return ""
}

// writeRouterTelemetry emits one structured JSONL line per
// router-hook invocation when at least one heuristic produced
// signal. The escalate field was removed alongside the in-gate LLM
// advisor (audit Tier 6 cull, 2026-05-08); chain consumers stamp
// any escalation intent themselves when they read the signals.
func writeRouterTelemetry(errOut io.Writer, kind string, outcome router.HeuristicOutcome, kernel KernelVerdict) {
	out := map[string]interface{}{
		"ts":                time.Now().UTC().Format(time.RFC3339),
		"level":             "info",
		"component":         "router-hook",
		"msg":               kind,
		"kernel_denied":     kernel.Denied,
		"heuristic_outcome": outcome,
	}
	if kernel.RuleID != "" {
		out["kernel_rule_id"] = kernel.RuleID
	}
	if kernel.Reason != "" {
		out["kernel_reason"] = kernel.Reason
	}
	body, _ := json.Marshal(out)
	_, _ = errOut.Write(body)
	_, _ = errOut.Write([]byte{'\n'})
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
