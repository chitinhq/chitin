// Helpers that wire gov.Gate's OnDecision callback into the canonical
// chain emit path. Lives in the cmd layer because gov is a leaf package
// (must not import emit) and the construction of an Event from a
// Decision is a CLI-layer composition concern.
package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/chitinhq/chitin/go/execution-kernel/internal/chain"
	"github.com/chitinhq/chitin/go/execution-kernel/internal/emit"
	"github.com/chitinhq/chitin/go/execution-kernel/internal/event"
	"github.com/chitinhq/chitin/go/execution-kernel/internal/gov"
)

// decisionEmitter packages the chain index and emitter needed to project
// a gov.Decision onto a canonical `decision` chain event. Single-shot
// per gate-evaluate invocation; the caller closes the returned closer.
type decisionEmitter struct {
	em *emit.Emitter
	// chainIDFn yields the chain_id this decision belongs to. For the
	// Claude Code hook path: SessionID from HookInput. For the bare
	// gate-evaluate CLI path (openclaw acpx, operator dry-run): a fresh
	// per-call UUID — chain length 1, no parent. Both render as valid
	// OTEL spans via F4's existing parent-rule branches.
	chainIDFn func() string
	surface   string
}

// newDecisionEmitter constructs the emit + index pair for a gate-evaluate
// invocation. Returns (nil, nil, no-op) when the chitin dir is unwritable
// or the index can't be opened — the gate itself should still run, the
// audit log is the durable record, the chain emit is best-effort.
func newDecisionEmitter(chitinDirPath, runID, surface string, chainIDFn func() string) (*decisionEmitter, func(), error) {
	if err := os.MkdirAll(chitinDirPath, 0o755); err != nil {
		return nil, func() {}, err
	}
	idx, err := chain.OpenIndex(filepath.Join(chitinDirPath, "chain_index.sqlite"))
	if err != nil {
		return nil, func() {}, err
	}
	if err := idx.RebuildFromJSONL(chitinDirPath); err != nil {
		_ = idx.Close()
		return nil, func() {}, err
	}
	em := &emit.Emitter{
		LogPath: filepath.Join(chitinDirPath, fmt.Sprintf("events-%s.jsonl", runID)),
		Index:   idx,
	}
	em.EnableOTELFromEnv() // F4: opt-in via OTEL_EXPORTER_OTLP_TRACES_ENDPOINT
	return &decisionEmitter{em: em, chainIDFn: chainIDFn, surface: surface}, func() { _ = idx.Close() }, nil
}

// emitDecision is the gov.Gate.OnDecision callback. Builds a v2 Event,
// emits via the canonical path (which also fires F4 OTEL projection if
// configured), and logs+drops any error. Never propagates — the gate
// has already returned its Decision, the audit log is durable, the
// chain emit is best-effort additive.
func (e *decisionEmitter) emitDecision(d *gov.Decision) {
	if e == nil || e.em == nil {
		return
	}
	chainID := e.chainIDFn()
	if chainID == "" {
		chainID = newChainID()
	}
	if chainID == "" {
		// crypto/rand failed; skip emit rather than write a degenerate event.
		log.Printf("decision-emit: skipped (no chain_id available)")
		return
	}
	ev := buildDecisionEvent(d, chainID, e.surface)
	if err := e.em.Emit(ev); err != nil {
		log.Printf("decision-emit: %v", err)
	}
}

// buildDecisionEvent constructs a v2 Event for a gate decision. The
// payload carries the fields F4's projectToSpan can pick up (decision,
// tool.name via action_type) plus the audit-relevant fields (rule_id,
// reason, suggestion, corrected_command).
func buildDecisionEvent(d *gov.Decision, chainID, surface string) *event.Event {
	decisionStr := "allow"
	if !d.Allowed {
		decisionStr = "deny"
		if d.Mode == "guide" {
			decisionStr = "guide"
		}
	}
	payload := map[string]any{
		"decision":    decisionStr,
		"rule_id":     d.RuleID,
		"action_type": string(d.Action.Type),
		// F4 projector reads `tool_name` for the OTEL `tool.name` attribute.
		// For decision events, the closed-enum action_type IS the most
		// useful tool identity — original tool name was lost in normalize.
		"tool_name": string(d.Action.Type),
	}
	if d.Reason != "" {
		payload["reason"] = d.Reason
	}
	if d.Suggestion != "" {
		payload["suggestion"] = d.Suggestion
	}
	if d.CorrectedCommand != "" {
		payload["corrected_command"] = d.CorrectedCommand
	}
	if d.Action.Target != "" {
		payload["action_target"] = d.Action.Target
	}
	if d.Escalation != "" {
		payload["escalation"] = d.Escalation
	}
	payloadJSON, _ := json.Marshal(payload)
	return &event.Event{
		SchemaVersion:   "2",
		RunID:           chainID,
		SessionID:       chainID,
		Surface:         surface,
		AgentInstanceID: d.Agent,
		EventType:       "decision",
		ChainID:         chainID,
		ChainType:       "session",
		Ts:              d.Ts,
		Labels:          map[string]string{},
		Payload:         payloadJSON,
	}
}

// newChainID returns a fresh UUIDv4-shaped string for chain_id of
// callers that have no session context (gate-evaluate CLI, openclaw
// acpx without session passthrough). The format matches Phase 1.5 chain
// IDs so traceID encoding (32 hex without hyphens) works unchanged.
//
// Returns "" if crypto/rand fails — caller treats empty as "skip OTEL/
// decision-emit wiring" rather than risk a deterministic all-zero
// collision-prone UUID across processes.
func newChainID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return ""
	}
	// Set version (4) and variant (10) per RFC 4122
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%s-%s-%s-%s-%s",
		hex.EncodeToString(b[0:4]),
		hex.EncodeToString(b[4:6]),
		hex.EncodeToString(b[6:8]),
		hex.EncodeToString(b[8:10]),
		hex.EncodeToString(b[10:16]),
	)
}
