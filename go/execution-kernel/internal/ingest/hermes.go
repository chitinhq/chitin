// Package ingest — hermes.go is the hermes-dialect translator.
//
// Hermes emits no OTEL telemetry. Its plugin-hook API (post_api_request in
// run_agent.py) exposes per-LLM-call data with model, provider, token usage,
// and session correlation — strictly superior to what the openclaw OTLP
// capture provides for per-call observability.
//
// The source-side plugin (~/chitin-sink/ in this repo's first real capture,
// or ~/.hermes/chitin-sink/ per the design spec — Phase A ships the former,
// see docs/observations/2026-04-21-hermes-post-api-request-capture.md)
// dumps each hook event as one JSON line to a daily-rotated file. This
// translator parses that JSONL; v1 maps only post_api_request to ModelTurn
// and quarantines every other event_type with Reason="v1-scope".
//
// Spec: docs/superpowers/specs/2026-04-21-hermes-dialect-adapter-v1-design.md
package ingest

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/chitinhq/chitin/go/execution-kernel/internal/emit"
	"github.com/chitinhq/chitin/go/execution-kernel/internal/event"
)

// HermesEvent is one line in a chitin-sink JSONL stream. Kwargs is
// intentionally a generic map — the translator inspects it per event_type.
type HermesEvent struct {
	EventType string                 `json:"event_type"`
	Ts        string                 `json:"ts"`
	Kwargs    map[string]interface{} `json:"kwargs"`
}

// ParseHermesEvents classifies every line of a chitin-sink JSONL stream
// into parseable ModelTurns (v1: only post_api_request) and Quarantine
// records (v1-scope for other event_types, parse_error for malformed JSON,
// missing_fields:<list> for required-attr failures).
//
// Never errors mid-walk; a returned error is reserved for structural
// failures like a scanner I/O error. Blank lines are skipped.
func ParseHermesEvents(raw []byte) ([]ModelTurn, []Quarantine, error) {
	var turns []ModelTurn
	var quarantined []Quarantine

	scanner := bufio.NewScanner(bytes.NewReader(raw))
	// post_api_request lines with large usage/prompt-details dicts can exceed
	// the 64 KiB default scanner buffer; give it 1 MiB headroom.
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		// Copy — scanner buffer is reused on next Scan().
		lineCopy := append([]byte(nil), line...)

		var ev HermesEvent
		if err := json.Unmarshal(lineCopy, &ev); err != nil {
			quarantined = append(quarantined, Quarantine{
				Reason:  "parse_error",
				SpanRaw: json.RawMessage(lineCopy),
			})
			continue
		}
		if ev.EventType != "post_api_request" {
			quarantined = append(quarantined, Quarantine{
				Reason:   "v1-scope",
				SpanName: ev.EventType,
				SpanRaw:  json.RawMessage(lineCopy),
			})
			continue
		}
		mt, reason := translatePostAPIRequest(&ev)
		if reason != "" {
			quarantined = append(quarantined, Quarantine{
				Reason:   reason,
				SpanName: ev.EventType,
				SpanRaw:  json.RawMessage(lineCopy),
			})
			continue
		}
		turns = append(turns, mt)
	}
	if err := scanner.Err(); err != nil {
		return nil, nil, fmt.Errorf("scan: %w", err)
	}

	// Deterministic order: timestamp ascending, span_id tie-break — same
	// pattern as openclaw.ParseOpenClawSpans. Source JSONL is typically in
	// arrival order already, but sorting cements determinism across
	// re-ingests and concatenated-file inputs.
	sort.SliceStable(turns, func(i, j int) bool {
		if turns[i].Ts != turns[j].Ts {
			return turns[i].Ts < turns[j].Ts
		}
		return turns[i].SpanID < turns[j].SpanID
	})
	sort.SliceStable(quarantined, func(i, j int) bool {
		// Quarantined entries may lack Ts if JSON parsing failed; fall back
		// to SpanRaw for a stable total ordering.
		if quarantined[i].SpanName != quarantined[j].SpanName {
			return quarantined[i].SpanName < quarantined[j].SpanName
		}
		return bytes.Compare(quarantined[i].SpanRaw, quarantined[j].SpanRaw) < 0
	})

	return turns, quarantined, nil
}

// translatePostAPIRequest extracts a ModelTurn from one post_api_request
// event. Returns (ModelTurn, "") on success, or (zero, reason) where reason
// is either "missing_fields:<comma-list>" or a typed error.
//
// Required kwargs (quarantine if missing): session_id, api_call_count.
// Optional kwargs (default to zero-value): usage (→ tokens), model/
// response_model (→ ModelName), provider, api_duration, cache_read_tokens.
//
// Token-key tolerance (matches the real 2026-04-21 capture, see
// docs/observations/2026-04-21-hermes-post-api-request-capture.md):
//   - prompt_tokens preferred; input_tokens fallback.
//   - completion_tokens preferred; output_tokens fallback. (Real hermes emits
//     output_tokens only — so in practice the fallback always wins — but
//     accepting both keeps the translator stable if hermes adds OpenAI-compat
//     keys later.)
//   - cache_read_tokens at the top level of usage (hermes native), with
//     prompt_tokens_details.cached_tokens accepted as a fallback for any
//     future OpenAI-compat variant.
func translatePostAPIRequest(ev *HermesEvent) (ModelTurn, string) {
	sessionID, sOK := getKwargString(ev.Kwargs, "session_id")
	_, cOK := getKwargInt(ev.Kwargs, "api_call_count")

	var missing []string
	if !sOK || sessionID == "" {
		missing = append(missing, "session_id")
	}
	if !cOK {
		missing = append(missing, "api_call_count")
	}
	// ts is carried on the event line (not in kwargs) and drives the span
	// ID — without it, all calls in a session collide. Enforce it here so
	// a malformed plugin writer cannot silently break dedup/idempotency.
	if ev.Ts == "" {
		missing = append(missing, "ts")
	}
	if len(missing) > 0 {
		return ModelTurn{}, "missing_fields:" + strings.Join(missing, ",")
	}

	// api_call_count is required as a malformed-output signal but is not
	// part of the span-ID input — the hermes counter resets across turns,
	// so it is not unique within a session (see the Phase B observation
	// doc). ev.Ts is microsecond-resolution and unique per call.
	traceHex := hermesSyntheticTraceID(sessionID)
	spanHex := hermesSyntheticSpanID(sessionID, ev.Ts)

	// response_model is what the LLM server actually used; prefer it over
	// model (which keeps the `:cloud` routing suffix). Non-empty required
	// for parity with openclaw's model_turn invariant.
	modelName, _ := getKwargString(ev.Kwargs, "response_model")
	if modelName == "" {
		modelName, _ = getKwargString(ev.Kwargs, "model")
	}
	if modelName == "" {
		return ModelTurn{}, "missing_fields:model_name"
	}

	provider, _ := getKwargString(ev.Kwargs, "provider")
	if provider == "" {
		return ModelTurn{}, "missing_fields:provider"
	}

	var durationMs int64
	if dur, ok := getKwargFloat(ev.Kwargs, "api_duration"); ok {
		if dur < 0 {
			return ModelTurn{}, "invalid_value:api_duration"
		}
		durationMs = int64(dur*1000 + 0.5)
	}

	var inputTokens, outputTokens, cacheRead, cacheWrite int64
	if usage, ok := ev.Kwargs["usage"].(map[string]interface{}); ok && usage != nil {
		// Fallbacks check key PRESENCE, not zero-value. A legitimate
		// prompt_tokens: 0 (e.g. cache-only call, empty-prompt edge case)
		// must win over a present-but-different input_tokens alias.
		if v, ok := getKwargInt(usage, "prompt_tokens"); ok {
			inputTokens = v
		} else {
			inputTokens, _ = getKwargInt(usage, "input_tokens")
		}
		if v, ok := getKwargInt(usage, "completion_tokens"); ok {
			outputTokens = v
		} else {
			outputTokens, _ = getKwargInt(usage, "output_tokens")
		}
		if v, ok := getKwargInt(usage, "cache_read_tokens"); ok {
			cacheRead = v
		} else if details, ok := usage["prompt_tokens_details"].(map[string]interface{}); ok && details != nil {
			cacheRead, _ = getKwargInt(details, "cached_tokens")
		}
		if v, ok := getKwargInt(usage, "cache_write_tokens"); ok {
			cacheWrite = v
		} else if details, ok := usage["prompt_tokens_details"].(map[string]interface{}); ok && details != nil {
			cacheWrite, _ = getKwargInt(details, "cache_write_tokens")
		}
	}
	// Non-negative invariant — mirrors openclaw's quarantine at translation
	// time so a buggy plugin output can't leak into a chain event that
	// downstream validation will reject.
	if inputTokens < 0 {
		return ModelTurn{}, "invalid_value:input_tokens"
	}
	if outputTokens < 0 {
		return ModelTurn{}, "invalid_value:output_tokens"
	}
	if cacheRead < 0 {
		return ModelTurn{}, "invalid_value:cache_read_tokens"
	}
	if cacheWrite < 0 {
		return ModelTurn{}, "invalid_value:cache_write_tokens"
	}

	return ModelTurn{
		TraceID:           traceHex,
		SpanID:            spanHex,
		Ts:                ev.Ts,
		Surface:           "hermes",
		Provider:          provider,
		ModelName:         modelName,
		InputTokens:       inputTokens,
		OutputTokens:      outputTokens,
		SessionIDExternal: sessionID,
		DurationMs:        durationMs,
		CacheReadTokens:   cacheRead,
		CacheWriteTokens:  cacheWrite,
	}, ""
}

// getKwargString reads a string-valued kwarg. Returns (value, true) on hit,
// ("", false) otherwise. Missing and non-string both return false.
func getKwargString(m map[string]interface{}, key string) (string, bool) {
	v, ok := m[key]
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}

// getKwargInt reads an integer-valued kwarg. JSON unmarshals numbers to
// float64 by default; we accept that and float32 as well so fixtures can
// use either.
func getKwargInt(m map[string]interface{}, key string) (int64, bool) {
	v, ok := m[key]
	if !ok {
		return 0, false
	}
	switch n := v.(type) {
	case float64:
		return int64(n), true
	case float32:
		return int64(n), true
	case int:
		return int64(n), true
	case int64:
		return n, true
	}
	return 0, false
}

// getKwargFloat reads a float64-valued kwarg.
func getKwargFloat(m map[string]interface{}, key string) (float64, bool) {
	v, ok := m[key]
	if !ok {
		return 0, false
	}
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	}
	return 0, false
}

// EmitHermesTurns is the hermes-dialect analogue of EmitModelTurns. Same
// shape: validate template, write quarantine side-cars first (crash-safety),
// then emit one model_turn event per ModelTurn via the transactional emitter.
// Differences from EmitModelTurns:
//
//   - chain_id format is "hermes:<trace>:<span>" (per-api-call), not
//     "otel:<trace>" (per-trace). Each post_api_request is its own chain.
//   - Labels["source"] = "hermes-plugin", Labels["dialect"] = "hermes".
//   - Quarantines land in <dir>/hermes-quarantine/ — a sibling of the
//     otel-quarantine dir so a mixed-dialect .chitin tree doesn't commingle.
//
// Returns the number of NEW events emitted (idempotent replay: a turn whose
// chain_id already exists is skipped).
func EmitHermesTurns(em *emit.Emitter, dir string, tmpl *event.Event, turns []ModelTurn, quarantined []Quarantine) (int, error) {
	if err := ValidateEnvelopeTemplate(tmpl); err != nil {
		return 0, fmt.Errorf("invalid_envelope_template: %w", err)
	}
	for _, q := range quarantined {
		if err := WriteHermesQuarantine(dir, q); err != nil {
			return 0, fmt.Errorf("write_quarantine: %w", err)
		}
	}

	emitted := 0
	for i, turn := range turns {
		chainID := buildHermesChainID(turn.TraceID, turn.SpanID)

		existing, err := em.Index.Get(chainID)
		if err != nil {
			return emitted, fmt.Errorf("index lookup for turn %d: %w", i, err)
		}
		if existing != nil {
			continue
		}

		ev := cloneTemplate(tmpl)
		ev.EventType = "model_turn"
		ev.Ts = turn.Ts
		ev.Surface = turn.Surface
		ev.ChainID = chainID
		if ev.Labels == nil {
			ev.Labels = map[string]string{}
		}
		ev.Labels["source"] = "hermes-plugin"
		ev.Labels["dialect"] = "hermes"

		payload := modelTurnPayload{
			ModelName:         turn.ModelName,
			Provider:          turn.Provider,
			InputTokens:       turn.InputTokens,
			OutputTokens:      turn.OutputTokens,
			SessionIDExternal: turn.SessionIDExternal,
			DurationMs:        turn.DurationMs,
			CacheReadTokens:   turn.CacheReadTokens,
			CacheWriteTokens:  turn.CacheWriteTokens,
		}
		raw, err := json.Marshal(payload)
		if err != nil {
			return emitted, fmt.Errorf("marshal payload for turn %d: %w", i, err)
		}
		ev.Payload = json.RawMessage(raw)

		if err := em.Emit(&ev); err != nil {
			return emitted, fmt.Errorf("emit turn %d: %w", i, err)
		}
		emitted++
	}
	return emitted, nil
}

// WriteHermesQuarantine persists one hermes-dialect quarantine record under
// <dir>/hermes-quarantine/<reason>-<span_name>-<raw-sha256-prefix>.json.
//
// Hermes quarantines typically lack trace/span IDs (v1-scope events and
// parse_errors carry neither), so the filename-uniqueness input is a
// sha256 prefix of SpanRaw. This makes replays idempotent: the same
// byte-identical line overwrites its own file rather than creating
// monotonically-numbered duplicates that would grow the dir unbounded
// across re-ingests.
func WriteHermesQuarantine(dir string, q Quarantine) error {
	qdir := filepath.Join(dir, "hermes-quarantine")
	if err := os.MkdirAll(qdir, 0o755); err != nil {
		return err
	}
	reason := sanitizeFilename(q.Reason)
	if reason == "" {
		reason = "unknown"
	}
	span := sanitizeFilename(q.SpanName)
	if span == "" {
		span = "nospan"
	}
	sum := sha256.Sum256(q.SpanRaw)
	rawHash := hex.EncodeToString(sum[:8])
	name := fmt.Sprintf("%s-%s-%s.json", reason, span, rawHash)

	data, err := json.MarshalIndent(struct {
		Reason   string          `json:"reason"`
		SpanName string          `json:"span_name"`
		SpanRaw  json.RawMessage `json:"span_raw"`
	}{
		Reason:   q.Reason,
		SpanName: q.SpanName,
		SpanRaw:  q.SpanRaw,
	}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(qdir, name), data, 0o644)
}

// buildHermesChainID mirrors the tripartite shape SP-2 adopted for OTEL
// ingest ("otel:<trace>:<span>"), with "hermes:" as an honest-about-source
// prefix. The chain_id is deterministic from the synthetic trace/span IDs
// below: trace from session_id, and span from (session_id, ts). Re-ingest
// of the same JSONL is therefore idempotent at the emit layer.
func buildHermesChainID(traceHex, spanHex string) string {
	return "hermes:" + traceHex + ":" + spanHex
}

// hermesSyntheticTraceID derives a deterministic 128-bit (32 hex char)
// trace ID from the hermes session_id. All API calls within one session
// share a trace ID — consistent with OTEL trace semantics (a trace is a
// logical session of work).
func hermesSyntheticTraceID(sessionID string) string {
	sum := sha256.Sum256([]byte("hermes-trace:" + sessionID))
	return hex.EncodeToString(sum[:16])
}

// hermesSyntheticSpanID derives a deterministic 64-bit (16 hex char) span
// ID from (session_id, ts). Unique per API call within a session; stable
// across re-ingests of the same JSONL.
//
// The design spec originally proposed (session_id, api_call_count) as the
// span key. The 2026-04-21 real capture showed that api_call_count resets
// to 1 across turns within a session (8 distinct calls share call=1 in one
// session). Timestamps are microsecond-resolution and unique per post_api_request
// line, so they are the stable disambiguator. See
// docs/observations/2026-04-21-hermes-post-api-request-capture.md.
func hermesSyntheticSpanID(sessionID, ts string) string {
	key := "hermes-span:" + sessionID + ":" + ts
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:8])
}
