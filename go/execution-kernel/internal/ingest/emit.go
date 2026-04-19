// Package ingest — emit.go bridges parsed assistant turns to the event chain.
//
// EmitTurns validates an envelope template, clones it per turn, populates the
// per-turn fields (EventType, Ts, Payload), and calls em.Emit for each event.
//
// Preconditions:
//   - template must have schema_version == "2"
//   - template must have non-empty: run_id, session_id, surface, chain_id
//   - template must have chain_type == "session"
//   - Payload, Seq, PrevHash, ThisHash in template are ignored (Emitter fills them)
//
// Postconditions:
//   - Returns the count of events emitted (== len(turns)) on success.
//   - On error, emission is aborted; some events may have been written already.
//     The chain index and JSONL remain consistent because Emitter is transactional.
//
// Invariant: any illegal template state (missing required fields) is caught
// before the first Emit call — we never emit with a partial envelope.
package ingest

import (
	"encoding/json"
	"fmt"

	"github.com/chitinhq/chitin/go/execution-kernel/internal/emit"
	"github.com/chitinhq/chitin/go/execution-kernel/internal/event"
)

// assistantTurnPayload is the typed shape of the assistant_turn event payload.
// Fields tagged omitempty are only included when non-empty / non-nil.
type assistantTurnPayload struct {
	Text     string            `json:"text"`
	Thinking string            `json:"thinking,omitempty"`
	ModelUsed modelUsed        `json:"model_used"`
	Usage    assistantUsage    `json:"usage"`
	TsStart  string            `json:"ts_start"`
	TsEnd    string            `json:"ts_end"`
}

type modelUsed struct {
	Name     string `json:"name"`
	Provider string `json:"provider"`
}

type assistantUsage struct {
	InputTokens              int64  `json:"input_tokens"`
	OutputTokens             int64  `json:"output_tokens"`
	CacheCreationInputTokens *int64 `json:"cache_creation_input_tokens,omitempty"`
	CacheReadInputTokens     *int64 `json:"cache_read_input_tokens,omitempty"`
	ThinkingTokens           *int64 `json:"thinking_tokens,omitempty"`
}

// ValidateEnvelopeTemplate checks that tmpl satisfies the required-field
// contract for use as an envelope template.  It returns a descriptive error
// naming exactly which field is missing so callers can surface it directly.
//
// Illegal states made unrepresentable here:
//   - schema_version != "2"           → silent silent version skew impossible
//   - run_id / session_id / surface / chain_id empty → orphaned events impossible
//   - chain_type != "session"         → wrong-chain-type events impossible
func ValidateEnvelopeTemplate(tmpl *event.Event) error {
	if tmpl.SchemaVersion != "2" {
		return fmt.Errorf("schema_version must be \"2\", got %q", tmpl.SchemaVersion)
	}
	if tmpl.RunID == "" {
		return fmt.Errorf("run_id is required")
	}
	if tmpl.SessionID == "" {
		return fmt.Errorf("session_id is required")
	}
	if tmpl.Surface == "" {
		return fmt.Errorf("surface is required")
	}
	if tmpl.ChainID == "" {
		return fmt.Errorf("chain_id is required")
	}
	if tmpl.ChainType != "session" {
		return fmt.Errorf("chain_type must be \"session\", got %q", tmpl.ChainType)
	}
	return nil
}

// EmitTurns emits one assistant_turn event per parsed turn using the given
// envelope template.  The template is validated before any emission begins.
// Returns the number of events emitted, or an error if validation or any
// individual emit fails.
func EmitTurns(em *emit.Emitter, tmpl *event.Event, turns []AssistantTurn) (int, error) {
	// Precondition: validate template before touching the chain.
	if err := ValidateEnvelopeTemplate(tmpl); err != nil {
		return 0, fmt.Errorf("invalid_envelope_template: %w", err)
	}

	for i, turn := range turns {
		ev := cloneTemplate(tmpl)
		ev.EventType = "assistant_turn"
		ev.Ts = turn.Ts

		payload := assistantTurnPayload{
			Text:    turn.Text,
			Thinking: turn.Thinking,
			ModelUsed: modelUsed{
				Name:     turn.ModelName,
				Provider: "anthropic",
			},
			Usage: assistantUsage{
				InputTokens:              turn.Usage.InputTokens,
				OutputTokens:             turn.Usage.OutputTokens,
				CacheCreationInputTokens: turn.Usage.CacheCreationInputTokens,
				CacheReadInputTokens:     turn.Usage.CacheReadInputTokens,
				ThinkingTokens:           turn.Usage.ThinkingTokens,
			},
			TsStart: turn.Ts,
			TsEnd:   turn.Ts,
		}

		raw, err := json.Marshal(payload)
		if err != nil {
			return i, fmt.Errorf("marshal payload for turn %d: %w", i, err)
		}
		ev.Payload = json.RawMessage(raw)

		if err := em.Emit(&ev); err != nil {
			return i, fmt.Errorf("emit turn %d: %w", i, err)
		}
	}

	return len(turns), nil
}

// cloneTemplate returns a shallow copy of tmpl with chain/hash fields zeroed
// so Emitter.Emit can fill them fresh for each event.
func cloneTemplate(tmpl *event.Event) event.Event {
	ev := *tmpl // copy all fields
	// Per-event fields reset so Emitter fills them.
	ev.EventType = ""
	ev.Ts = ""
	ev.Payload = nil
	ev.Seq = 0
	ev.PrevHash = nil
	ev.ThisHash = ""
	return ev
}
