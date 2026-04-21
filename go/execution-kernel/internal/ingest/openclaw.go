// Package ingest — openclaw.go is the openclaw-dialect translator.
//
// Pinned to @openclaw/diagnostics-otel@2026.4.15-beta.1 (see
// docs/observations/2026-04-20-openclaw-otel-capture.md for the source
// inventory that defines this mapping). A future openclaw version may
// add attrs or rename fields; SP-2 will re-verify.
package ingest

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/chitinhq/chitin/go/execution-kernel/internal/emit"
	"github.com/chitinhq/chitin/go/execution-kernel/internal/event"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/encoding/protojson"
)

// Quarantine records an unmappable span for audit.
type Quarantine struct {
	Reason   string
	SpanName string
	TraceID  string
	SpanID   string
	SpanRaw  json.RawMessage
}

// buildChainID is the single source of truth for OTEL-ingest chain-id
// construction. Every translator calls this helper; no other code
// assembles "otel:..." strings. Uniform across event types so chain-ids
// never collide across spans in a trace.
//
// Invariant: the returned string has exactly one ":" prefix after "otel",
// one full-length (32 hex char) trace portion, and one full-length
// (16 hex char) span portion. Callers must have already validated
// trace/span length; this function does not re-check.
func buildChainID(traceID, spanID []byte) string {
	return "otel:" + hex.EncodeToString(traceID) + ":" + hex.EncodeToString(spanID)
}

// buildOtelLabels constructs the label map every OTEL-ingest event gets.
// Single source of truth for the label vocabulary: source, dialect,
// otel_trace_id, otel_span_id, and (when non-empty) otel_parent_span_id.
// parentSpanIDHex is "" when the span has no parent (root span).
func buildOtelLabels(traceIDHex, spanIDHex, parentSpanIDHex string) map[string]string {
	m := map[string]string{
		"source":        "otel",
		"dialect":       "openclaw",
		"otel_trace_id": traceIDHex,
		"otel_span_id":  spanIDHex,
	}
	if parentSpanIDHex != "" {
		m["otel_parent_span_id"] = parentSpanIDHex
	}
	return m
}

// ParseOpenClawSpans classifies every span into either turns (mappable
// openclaw.model.usage spans) or quarantined (everything else). Never
// errors mid-walk; a returned error is reserved for structural failures.
func ParseOpenClawSpans(rs []*tracepb.ResourceSpans) ([]ModelTurn, []Quarantine, error) {
	var turns []ModelTurn
	var quarantined []Quarantine

	IterSpans(rs, func(resource *resourcepb.Resource, span *tracepb.Span) {
		if span.Name != openclawModelUsageSpanName {
			quarantined = append(quarantined, makeQuarantine(
				fmt.Sprintf("unmapped_span_name:%s", span.Name), span,
			))
			return
		}
		mt, reason := translateModelUsage(resource, span)
		if reason != "" {
			quarantined = append(quarantined, makeQuarantine(reason, span))
			return
		}
		turns = append(turns, mt)
	})

	// Deterministic order: timestamp ascending, span_id tie-break.
	sort.SliceStable(turns, func(i, j int) bool {
		if turns[i].TsStr != turns[j].TsStr {
			return turns[i].TsStr < turns[j].TsStr
		}
		return turns[i].SpanIDHex < turns[j].SpanIDHex
	})

	return turns, quarantined, nil
}

// --- attribute helpers ---

func getResourceStringAttr(r *resourcepb.Resource, key string) string {
	if r == nil {
		return ""
	}
	return getStringAttr(r.Attributes, key)
}

func getSpanStringAttr(s *tracepb.Span, key string) string {
	return getStringAttr(s.Attributes, key)
}

func getSpanIntAttr(s *tracepb.Span, key string) (int64, bool) {
	var found bool
	var last int64
	// Duplicate-key handling: last write wins (per spec §Data flow tie-breakers).
	for _, kv := range s.Attributes {
		if kv.Key != key || kv.Value == nil {
			continue
		}
		if v, ok := kv.Value.GetValue().(*commonpb.AnyValue_IntValue); ok {
			last = v.IntValue
			found = true
		}
	}
	return last, found
}

func getStringAttr(attrs []*commonpb.KeyValue, key string) string {
	// Duplicate-key handling: last write wins.
	var last string
	for _, kv := range attrs {
		if kv.Key != key || kv.Value == nil {
			continue
		}
		if v, ok := kv.Value.GetValue().(*commonpb.AnyValue_StringValue); ok {
			last = v.StringValue
		}
	}
	return last
}

func isAllZero(b []byte) bool {
	for _, x := range b {
		if x != 0 {
			return false
		}
	}
	return true
}

// --- quarantine serialization ---

func makeQuarantine(reason string, span *tracepb.Span) Quarantine {
	raw, err := protojson.MarshalOptions{Multiline: false}.Marshal(span)
	if err != nil {
		raw = []byte(fmt.Sprintf(`{"__marshal_error":%q}`, err.Error()))
	}
	return Quarantine{
		Reason:   reason,
		SpanName: span.Name,
		TraceID:  hex.EncodeToString(span.TraceId),
		SpanID:   hex.EncodeToString(span.SpanId),
		SpanRaw:  json.RawMessage(raw),
	}
}

// EmitModelTurns validates the envelope template, writes every quarantine
// side-car first (crash-safety: quarantine complete before any event is
// emitted), then emits one model_turn event per ModelTurn through the
// transactional Emitter. Returns the number of NEW events emitted.
//
// Invariant: ValidateEnvelopeTemplate is called before any side-effects.
// Invariant: quarantine files are written before the first Emit call.
// Invariant: a turn whose chain_id already exists in the index is skipped
//
//	(idempotent replay — re-emitting the same trace produces no new events).
//
// Chain-id scheme: every turn's chain_id is built by buildChainID, which
// produces "otel:<trace_id_hex>:<span_id_hex>". Because span_id is unique
// within a trace, multiple openclaw.model.usage spans sharing a trace_id
// (e.g. a retry or a batch-of-model-calls in one trace) each get their
// own chain_id and are emitted independently. Collisions only occur on
// true replay of the same (trace_id, span_id), which is exactly what the
// idempotency check catches.
//
// Not safe for concurrent invocation: the em.Index.Get / em.Emit pair is
// not atomic, so overlapping calls with the same chain_id may race. SP-1
// uses single-process, sequential invocation only; concurrency safety is
// deferred to a follow-up if SP-3's push receiver requires it.
func EmitModelTurns(em *emit.Emitter, dir string, tmpl *event.Event, turns []ModelTurn, quarantined []Quarantine) (int, error) {
	if err := ValidateEnvelopeTemplate(tmpl); err != nil {
		return 0, fmt.Errorf("invalid_envelope_template: %w", err)
	}
	// Write quarantine side-cars BEFORE any event is emitted — crash-safety.
	for _, q := range quarantined {
		if err := WriteQuarantine(dir, q); err != nil {
			return 0, fmt.Errorf("write_quarantine: %w", err)
		}
	}

	emitted := 0
	for i, turn := range turns {
		chainID := buildChainID(turn.TraceIDBytes, turn.SpanIDBytes)

		// Idempotency: if this chain already has any event, skip it.
		existing, err := em.Index.Get(chainID)
		if err != nil {
			return emitted, fmt.Errorf("index lookup for turn %d: %w", i, err)
		}
		if existing != nil {
			// Chain already populated — this turn was already emitted.
			continue
		}

		ev := cloneTemplate(tmpl)
		ev.EventType = "model_turn"
		ev.Ts = turn.TsStr
		ev.Surface = turn.SurfaceStr
		ev.ChainID = chainID
		if ev.Labels == nil {
			ev.Labels = map[string]string{}
		}
		for k, v := range buildOtelLabels(turn.TraceID, turn.SpanIDHex, turn.ParentSpanIDHex) {
			ev.Labels[k] = v
		}

		raw, err := buildModelTurnPayload(turn)
		if err != nil {
			return emitted, fmt.Errorf("marshal payload for turn %d: %w", i, err)
		}
		ev.Payload = raw

		if err := em.Emit(&ev); err != nil {
			return emitted, fmt.Errorf("emit turn %d: %w", i, err)
		}
		emitted++
	}
	return emitted, nil
}

// WriteQuarantine persists one unmappable span to
// <dir>/otel-quarantine/<span_name>-<trace_id>-<span_id>.json.
// Idempotent: overwriting the same path with the same span produces
// identical content.
func WriteQuarantine(dir string, q Quarantine) error {
	qdir := filepath.Join(dir, "otel-quarantine")
	if err := os.MkdirAll(qdir, 0o755); err != nil {
		return err
	}
	name := fmt.Sprintf("%s-%s-%s.json", sanitizeFilename(q.SpanName), q.TraceID, q.SpanID)
	data, err := json.MarshalIndent(struct {
		Reason   string          `json:"reason"`
		SpanName string          `json:"span_name"`
		TraceID  string          `json:"trace_id"`
		SpanID   string          `json:"span_id"`
		SpanRaw  json.RawMessage `json:"span_raw"`
	}{
		Reason:   q.Reason,
		SpanName: q.SpanName,
		TraceID:  q.TraceID,
		SpanID:   q.SpanID,
		SpanRaw:  q.SpanRaw,
	}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(qdir, name), data, 0o644)
}

// sanitizeFilename replaces filesystem-problematic chars in span names like
// ":", "/", "\" so they are safe as filename fragments. Dots are kept.
func sanitizeFilename(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '/' || c == '\\' || c == ':' {
			out = append(out, '_')
		} else {
			out = append(out, c)
		}
	}
	return string(out)
}
