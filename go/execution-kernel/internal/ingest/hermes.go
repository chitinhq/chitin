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
		// Task 9 replaces this branch with translatePostAPIRequest.
		quarantined = append(quarantined, Quarantine{
			Reason:   "not_yet_implemented",
			SpanName: ev.EventType,
			SpanRaw:  json.RawMessage(lineCopy),
		})
	}
	if err := scanner.Err(); err != nil {
		return nil, nil, fmt.Errorf("scan: %w", err)
	}
	return turns, quarantined, nil
}

// buildHermesChainID mirrors the tripartite shape SP-2 adopted for OTEL
// ingest ("otel:<trace>:<span>"), with "hermes:" as an honest-about-source
// prefix. The chain_id is deterministic from (session_id, api_call_count)
// via the synthetic-ID helpers below — so re-ingest of the same JSONL is
// idempotent at the emit layer.
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
// ID from (session_id, api_call_count). Unique per API call within a
// session; stable across re-ingests of the same JSONL.
func hermesSyntheticSpanID(sessionID string, apiCallCount int64) string {
	key := fmt.Sprintf("hermes-span:%s:%d", sessionID, apiCallCount)
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:8])
}
