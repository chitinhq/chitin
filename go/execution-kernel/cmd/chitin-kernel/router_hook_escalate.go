package main

// In-gate peer-escalation glue. Step 4 of
// docs/design/2026-05-06-kernel-gate-escalation.md.
//
// tryInGateSpawn is called when the advisor wants to escalate AND
// the operator has chitin-routes.yaml with `enabled: true`. It loads
// the routes policy, calls routeFor() to pick a candidate, then
// spawnPeer() to run the peer CLI synchronously. On success it
// rewrites the deny composition so the worker sees the peer's output
// as the deny reason — claude-code-compatible (the worker reads the
// reason text, can adapt accordingly).
//
// Fail-open: ANY error in this path returns silently. The caller's
// existing deny+escalation_requested behavior is preserved. The
// kernel never bricks because of an in-gate spawn failure — operator
// safety is paramount.
//
// Telemetry is written to errOut so /mine + the conformance loop
// can attribute peer outcomes back to the worker workflow.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/chitinhq/chitin/go/execution-kernel/internal/driver/claudecode"
	"github.com/chitinhq/chitin/go/execution-kernel/internal/router"
)

// tryInGateSpawn — best-effort peer-escalation. Mutates `composed` to
// include the peer's output when spawn succeeds. No-op on any failure.
//
// Return value: nothing — caller doesn't change behavior based on
// success/failure (the deny composition is mutated in-place when
// successful).
func tryInGateSpawn(
	out, errOut io.Writer,
	composed *map[string]interface{},
	payload claudecode.HookInput,
	advice *router.AdvisorResponse,
	outcome router.HeuristicOutcome,
	cwd string,
) {
	policy, err := router.LoadRoutesPolicy(cwd)
	if err != nil {
		writeJSONLine(errOut, map[string]string{
			"warning": "in_gate_spawn_skipped_bad_policy",
			"err":     err.Error(),
		})
		return
	}
	if !policy.Enabled {
		// Operator has not opted in. Silent skip; today's
		// escalation_requested-based ladder still fires.
		return
	}

	// Determine which signal triggered. Priority: floundering >
	// blast_radius > drift (most-likely-actionable first). The
	// advisor's escalation indicates SOME signal fired; we surface
	// the heuristic's name when present, else fall back to the
	// generic "advisor_takeover" signal.
	signal := "advisor_takeover"
	severity := "advisor.verdict=takeover"
	if outcome.Floundering != nil && outcome.Floundering.Fired {
		signal = "floundering"
		severity = outcome.Floundering.Reason
	} else if outcome.BlastRadius != nil && outcome.BlastRadius.Fired {
		signal = "blast_radius"
		severity = outcome.BlastRadius.Reason
	}

	req := router.RouteRequest{
		Signal:           signal,
		Severity:         severity,
		WorkerWorkflowID: payload.SessionID,
	}
	decision, err := router.RouteFor(req, policy)
	if err != nil {
		writeJSONLine(errOut, map[string]string{
			"warning": "in_gate_spawn_no_route",
			"signal":  signal,
			"err":     err.Error(),
		})
		return
	}

	// Compose the prompt the peer sees. Includes the worker's pending
	// tool call, the advisor's nudge, and the heuristic context. The
	// peer is asked to provide the corrective action / answer the
	// worker should adopt.
	prompt := fmt.Sprintf(
		`You are escalating a stuck worker. The worker (a chitin agent) was about to run a tool call but our heuristics + advisor flagged it.

Context:
  Tool: %s
  Tool input: %s
  Heuristic signal: %s
  Severity: %s
  Advisor nudge: %s

Your job: think about what the worker SHOULD do instead, and respond with your guidance. Be concrete (specific tool names, file paths, code snippets). The worker will read your response as a deny-reason and adapt.

You must NOT yourself spawn additional peers — your CHITIN_NO_ESCALATE=1 env var enforces this. If you need help yourself, say so in your response and stop.

Respond now.`,
		payload.ToolName,
		toolInputSummary(payload.ToolInput),
		signal,
		severity,
		advice.Nudge,
	)

	cfg := router.SpawnConfig{
		Decision:            decision,
		Request:             req,
		PromptText:          prompt,
		SpawnTimeoutSeconds: policy.SpawnTimeoutSeconds,
		// Spawner left nil → SpawnPeer uses production execSpawner.
	}

	ctx, cancel := context.WithTimeout(context.Background(),
		time.Duration(policy.SpawnTimeoutSeconds+5)*time.Second)
	defer cancel()
	res, err := router.SpawnPeer(ctx, cfg)
	if err != nil {
		writeJSONLine(errOut, map[string]string{
			"warning":     "in_gate_spawn_peer_failed",
			"escalation":  decision.Rule.Name,
			"driver":      decision.Candidate.Driver,
			"model":       decision.Candidate.Model,
			"err":         err.Error(),
		})
		return
	}

	// Success — rewrite the deny composition so the worker sees the
	// peer's output as the reason. Telemetry: full provenance line so
	// the conformance loop can join.
	peerText, _ := res.Content.(string)
	(*composed)["reason"] = fmt.Sprintf(
		"%s\n\n[Peer escalation %s/%s says]\n%s",
		advice.Nudge,
		decision.Candidate.Driver,
		decision.Candidate.Model,
		peerText,
	)
	(*composed)["peer_escalation_id"] = res.Provenance.EscalationID
	(*composed)["peer_driver"] = res.Provenance.Candidate.Driver
	(*composed)["peer_model"] = res.Provenance.Candidate.Model

	// Telemetry — keys/values are stringified to fit writeJSONLine's
	// map[string]string contract. The values flow into /mine + the
	// conformance loop where they're parsed back to typed fields.
	writeJSONLine(errOut, map[string]string{
		"event":             "peer_escalation",
		"escalation_id":     res.Provenance.EscalationID,
		"worker_workflow":   res.Provenance.WorkerWorkflowID,
		"trigger_signal":    res.Provenance.TriggerSignal,
		"severity":          res.Provenance.Severity,
		"route":             res.Provenance.Route,
		"driver":            res.Provenance.Candidate.Driver,
		"model":             res.Provenance.Candidate.Model,
		"duration_ms":       fmt.Sprintf("%d", res.Provenance.DurationMs),
		"peer_exit_code":    fmt.Sprintf("%d", res.Provenance.PeerExitCode),
		"peer_stdout_bytes": fmt.Sprintf("%d", len(res.RawPeerStdout)),
		"peer_stderr_bytes": fmt.Sprintf("%d", len(res.RawPeerStderr)),
	})
	_ = out // intentional: out is the worker-facing stream; the caller writes the composed body
}

// toolInputSummary — short string rendering of the worker's tool
// input for the peer's prompt. JSON marshaled, truncated to fit a
// reasonable prompt budget (4000 chars). Long file contents in tool
// inputs (e.g., Edit's new_string) get clipped — the peer doesn't
// need byte-perfect reproduction, just enough context.
func toolInputSummary(toolInput map[string]interface{}) string {
	if toolInput == nil {
		return "(none)"
	}
	body, err := json.Marshal(toolInput)
	if err != nil {
		return fmt.Sprintf("(unparseable: %v)", err)
	}
	const maxLen = 4000
	if len(body) > maxLen {
		return string(body[:maxLen]) + " …(truncated)"
	}
	return string(body)
}
