package gov

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// WriteLog appends a Decision as one JSON line to
// <dir>/gov-decisions-<utc-date>.jsonl. Daily-rotated; append-only.
// Tolerates ENOSPC specifically (logs to stderr, drops the line,
// returns nil). Other errors are propagated so permission/path/etc
// problems don't silently vanish from audit.
func WriteLog(d Decision, dir string) error {
	if d.Ts == "" {
		d.Ts = time.Now().UTC().Format(time.RFC3339)
	}
	date := strings.Split(d.Ts, "T")[0]
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir log dir: %w", err)
	}
	path := filepath.Join(dir, "gov-decisions-"+date+".jsonl")

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open log: %w", err)
	}
	defer f.Close()

	line, err := json.Marshal(struct {
		Allowed          bool    `json:"allowed"`
		Mode             string  `json:"mode"`
		RuleID           string  `json:"rule_id"`
		Reason           string  `json:"reason,omitempty"`
		Suggestion       string  `json:"suggestion,omitempty"`
		CorrectedCommand string  `json:"corrected_command,omitempty"`
		Escalation       string  `json:"escalation,omitempty"`
		Agent            string  `json:"agent,omitempty"`
		ActionType       string  `json:"action_type"`
		ActionTarget     string  `json:"action_target"`
		Ts               string  `json:"ts"`
		EnvelopeID       string  `json:"envelope_id,omitempty"`
		Tier             Tier    `json:"tier,omitempty"`
		CostUSD          float64 `json:"cost_usd,omitempty"`
		InputBytes       int64   `json:"input_bytes,omitempty"`
		OutputBytes      int64   `json:"output_bytes,omitempty"`
		ToolCalls        int64   `json:"tool_calls,omitempty"`
		CallerOrigin     string  `json:"caller_origin,omitempty"`
		// Typed agent identity dimensions. All are optional with
		// omitempty so older readers and non-identity dispatches keep
		// working. Fingerprint is retained as the legacy alias for
		// AgentFingerprint.
		AgentInstanceID   string `json:"agent_instance_id,omitempty"`
		AgentFingerprint  string `json:"agent_fingerprint,omitempty"`
		Driver            string `json:"driver,omitempty"`
		Model             string `json:"model,omitempty"`
		Role              string `json:"role,omitempty"`
		StationPromptHash string `json:"station_prompt_hash,omitempty"`
		SkillsToolsHash   string `json:"skills_tools_hash,omitempty"`
		SoulLens          string `json:"soul_lens,omitempty"`
		ClaimedAuthority  string `json:"claimed_authority,omitempty"`
		Authority         string `json:"authority,omitempty"`
		WorkflowID        string `json:"workflow_id,omitempty"`
		Fingerprint       string `json:"fingerprint,omitempty"`
		// Router-heuristic signal metadata (audit Tier 6 cull,
		// 2026-05-08). Stamped by router-hook when its policy is
		// enabled and at least one signal is non-zero; absent on
		// plain gate.Evaluate rows. Consumers (hermes' approvals.mode:
		// smart, operator-wired chain readers) read these to route
		// follow-ups without the kernel running any LLM in-line. See
		// docs/decisions/2026-05-08-cull-advisor-out-of-kernel-hot-path.md.
		PredictedBlast   float64 `json:"predicted_blast,omitempty"`
		FlounderingScore float64 `json:"floundering_score,omitempty"`
		DriftScore       float64 `json:"drift_score,omitempty"`
		RoutingDecision  string  `json:"routing_decision,omitempty"`
		// Audit-only worktree diagnostic. This is metadata on the existing
		// decision row, not an enforcement rule result.
		WorktreeDiagnosticRuleID string `json:"worktree_diagnostic_rule_id,omitempty"`
		WorktreeStatus           string `json:"worktree_status,omitempty"`
		WorktreeReason           string `json:"worktree_reason,omitempty"`
	}{
		Allowed: d.Allowed, Mode: d.Mode, RuleID: d.RuleID,
		Reason: d.Reason, Suggestion: d.Suggestion,
		CorrectedCommand: d.CorrectedCommand, Escalation: d.Escalation,
		Agent: d.Agent, ActionType: string(d.Action.Type), ActionTarget: d.Action.Target,
		Ts:         d.Ts,
		EnvelopeID: d.EnvelopeID, Tier: d.Tier, CostUSD: d.CostUSD,
		InputBytes: d.InputBytes, OutputBytes: d.OutputBytes, ToolCalls: d.ToolCalls,
		CallerOrigin:             d.CallerOrigin,
		AgentInstanceID:          d.AgentInstanceID,
		AgentFingerprint:         d.AgentFingerprint,
		Driver:                   d.Driver,
		Model:                    d.Model,
		Role:                     d.Role,
		StationPromptHash:        d.StationPromptHash,
		SkillsToolsHash:          d.SkillsToolsHash,
		SoulLens:                 d.SoulLens,
		ClaimedAuthority:         d.ClaimedAuthority,
		Authority:                d.Authority,
		WorkflowID:               d.WorkflowID,
		Fingerprint:              d.Fingerprint,
		PredictedBlast:           d.PredictedBlast,
		FlounderingScore:         d.FlounderingScore,
		DriftScore:               d.DriftScore,
		RoutingDecision:          d.RoutingDecision,
		WorktreeDiagnosticRuleID: d.WorktreeDiagnosticRuleID,
		WorktreeStatus:           d.WorktreeStatus,
		WorktreeReason:           d.WorktreeReason,
	})
	if err != nil {
		return fmt.Errorf("marshal decision: %w", err)
	}
	if _, err := f.Write(append(line, '\n')); err != nil {
		if errors.Is(err, syscall.ENOSPC) {
			// Best-effort on disk-full — don't fail the gate call.
			fmt.Fprintf(os.Stderr, "gov: decision log write skipped (ENOSPC): %v\n", err)
			return nil
		}
		return fmt.Errorf("write decision: %w", err)
	}
	return nil
}
