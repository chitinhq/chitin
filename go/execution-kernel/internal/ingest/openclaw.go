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
	"time"

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

// validateOpenClawEnvelope runs the shared envelope-validation prefix that
// every openclaw translator needs: trace_id + span_id length/zero checks,
// service.name presence, start_time_unix_nano presence, and RFC3339 ts
// formatting. Returns (surface, ts, "") on success; ("", "", reason) with
// a typed quarantine reason on any failure.
//
// This is the single source of truth for the "valid openclaw span envelope"
// invariant. Translators call this first, then validate their own
// dialect-specific attributes.
func validateOpenClawEnvelope(resource *resourcepb.Resource, span *tracepb.Span) (surface string, ts string, reason string) {
	if len(span.TraceId) != 16 {
		return "", "", "invalid_trace_id_length"
	}
	if isAllZero(span.TraceId) {
		return "", "", "invalid_trace_id_zero"
	}
	if len(span.SpanId) != 8 {
		return "", "", "invalid_span_id_length"
	}
	if isAllZero(span.SpanId) {
		return "", "", "invalid_span_id_zero"
	}

	surface = getResourceStringAttr(resource, "service.name")
	if surface == "" {
		return "", "", "missing_required_attr:service.name"
	}

	if span.StartTimeUnixNano == 0 {
		return "", "", "missing_required_attr:start_time_unix_nano"
	}
	ts = time.Unix(0, int64(span.StartTimeUnixNano)).UTC().Format(time.RFC3339)

	return surface, ts, ""
}

// parentSpanIDHex returns the hex-encoded parent_span_id when the span has
// a real parent (8-byte non-zero). Returns "" for root spans (missing or
// all-zero parent_span_id). Used by every openclaw translator when
// populating ParentSpanIDHex.
func parentSpanIDHex(span *tracepb.Span) string {
	if len(span.ParentSpanId) == 8 && !isAllZero(span.ParentSpanId) {
		return hex.EncodeToString(span.ParentSpanId)
	}
	return ""
}

// TranslatedSpan is the polymorphic output of every per-span translator.
// EmitEvents walks []TranslatedSpan without branching on concrete type.
// The interface is deliberately narrow: envelope fields + payload bytes
// + labels. Chain-id is always built via buildChainID; the interface
// hides the trace_id/span_id bytes behind ChainID().
type TranslatedSpan interface {
	EventType() string // "model_turn" | "webhook_received" | "webhook_failed" | "session_stuck"
	ChainID() string
	Ts() string
	Surface() string
	SpanID() string // hex, used for the deterministic sort tie-breaker
	Payload() (json.RawMessage, error)
	Labels() map[string]string
}

// seenKey is the (trace_id, span_id) pair used to detect duplicates within
// a single ingest batch. The pair — not just span_id — preserves the
// possibility that two distinct traces share a span_id by coincidence
// (OTEL only guarantees span_id uniqueness within a trace).
type seenKey struct {
	trace [16]byte
	span  [8]byte
}

// ParseOpenClawSpans classifies every span into either translated
// (mappable openclaw spans) or quarantined (everything else). Never
// errors mid-walk; a returned error is reserved for structural failures.
// The returned slice is unsorted; EmitEvents is responsible for the
// deterministic (ts asc, span_id asc) ordering before emission.
//
// Dispatch invariant: every span lands in exactly one of translated or
// quarantined. The switch on span.Name routes well-formed openclaw spans
// to their per-type translator; unknown names quarantine with
// unmapped_span_name:<name>. Within a single batch, the second (and any
// later) occurrence of a (trace_id, span_id) pair quarantines with
// duplicate_span_id — dedup runs BEFORE translation so malformed
// envelopes quarantine with their length-specific reason rather than
// the duplicate reason.
func ParseOpenClawSpans(rs []*tracepb.ResourceSpans) ([]TranslatedSpan, []Quarantine, error) {
	var translated []TranslatedSpan
	var quarantined []Quarantine
	seen := map[seenKey]bool{}

	IterSpans(rs, func(resource *resourcepb.Resource, span *tracepb.Span) {
		// Duplicate detection runs BEFORE translation so malformed spans
		// (wrong trace/span length) quarantine with the length reason
		// rather than the duplicate reason. Only well-formed pairs enter
		// the seen set.
		if len(span.TraceId) == 16 && len(span.SpanId) == 8 {
			var key seenKey
			copy(key.trace[:], span.TraceId)
			copy(key.span[:], span.SpanId)
			if seen[key] {
				quarantined = append(quarantined, makeQuarantine("duplicate_span_id", span))
				return
			}
			seen[key] = true
		}

		switch span.Name {
		case openclawModelUsageSpanName:
			mt, reason := translateModelUsage(resource, span)
			if reason != "" {
				quarantined = append(quarantined, makeQuarantine(reason, span))
				return
			}
			translated = append(translated, mt)
		case openclawWebhookProcessedSpanName:
			w, reason := translateWebhookProcessed(resource, span)
			if reason != "" {
				quarantined = append(quarantined, makeQuarantine(reason, span))
				return
			}
			translated = append(translated, w)
		case openclawWebhookErrorSpanName:
			w, reason := translateWebhookError(resource, span)
			if reason != "" {
				quarantined = append(quarantined, makeQuarantine(reason, span))
				return
			}
			translated = append(translated, w)
		case openclawSessionStuckSpanName:
			s, reason := translateSessionStuck(resource, span)
			if reason != "" {
				quarantined = append(quarantined, makeQuarantine(reason, span))
				return
			}
			translated = append(translated, s)
		default:
			quarantined = append(quarantined, makeQuarantine(
				fmt.Sprintf("unmapped_span_name:%s", span.Name), span,
			))
		}
	})

	return translated, quarantined, nil
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

// getSpanIntOrDoubleAsIntAttr returns the attribute value as int64.
// Accepts IntValue or DoubleValue (if the double has no fractional part).
// Returns (0, false) for missing, wrong-type, or fractional doubles.
// Duplicate-key handling: last write wins (matches getSpanIntAttr).
//
// OTEL JS SDK can emit numeric attributes as either IntValue or
// DoubleValue depending on whether the JS number has a fractional part.
// openclaw uses JS, so ageMs/queueDepth may arrive either way; this
// helper unifies the read path.
func getSpanIntOrDoubleAsIntAttr(s *tracepb.Span, key string) (int64, bool) {
	var found bool
	var last int64
	for _, kv := range s.Attributes {
		if kv.Key != key || kv.Value == nil {
			continue
		}
		switch v := kv.Value.GetValue().(type) {
		case *commonpb.AnyValue_IntValue:
			last = v.IntValue
			found = true
		case *commonpb.AnyValue_DoubleValue:
			if v.DoubleValue == float64(int64(v.DoubleValue)) {
				last = int64(v.DoubleValue)
				found = true
			} else {
				found = false
			}
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

// EmitEvents validates the envelope template, writes every quarantine
// side-car first (crash-safety), then emits one chitin event per
// TranslatedSpan in deterministic order.
//
// Invariants: ValidateEnvelopeTemplate called first; quarantine written
// before the first Emit call; events sorted by (ts asc, span_id asc)
// before emit; a chain_id already present in the index is skipped
// (idempotent replay).
//   - Each event gets a fresh labels map; template labels merge with
//     span-provided OTEL labels, with OTEL labels taking precedence on
//     key collision.
//
// Not safe for concurrent invocation: the em.Index.Get / em.Emit pair
// is not atomic. SP-2 preserves SP-1's single-process sequential
// assumption; push-mode concurrency is SP-3's problem.
func EmitEvents(em *emit.Emitter, dir string, tmpl *event.Event, spans []TranslatedSpan, quarantined []Quarantine) (int, error) {
	if err := ValidateEnvelopeTemplate(tmpl); err != nil {
		return 0, fmt.Errorf("invalid_envelope_template: %w", err)
	}

	for _, q := range quarantined {
		if err := WriteQuarantine(dir, q); err != nil {
			return 0, fmt.Errorf("write_quarantine: %w", err)
		}
	}

	sort.SliceStable(spans, func(i, j int) bool {
		if spans[i].Ts() != spans[j].Ts() {
			return spans[i].Ts() < spans[j].Ts()
		}
		return spans[i].SpanID() < spans[j].SpanID()
	})

	emitted := 0
	for i, span := range spans {
		chainID := span.ChainID()

		existing, err := em.Index.Get(chainID)
		if err != nil {
			return emitted, fmt.Errorf("index lookup for span %d: %w", i, err)
		}
		if existing != nil {
			continue
		}

		ev := cloneTemplate(tmpl)
		ev.EventType = span.EventType()
		ev.Ts = span.Ts()
		ev.Surface = span.Surface()
		ev.ChainID = chainID

		// Allocate a fresh labels map per event so we never mutate the caller's
		// template. OTEL labels (source, dialect, otel_*) take precedence over
		// template labels on key collision — documented in the EmitEvents
		// invariants above.
		otelLabels := span.Labels()
		ev.Labels = make(map[string]string, len(tmpl.Labels)+len(otelLabels))
		for k, v := range tmpl.Labels {
			ev.Labels[k] = v
		}
		for k, v := range otelLabels {
			ev.Labels[k] = v
		}

		raw, err := span.Payload()
		if err != nil {
			return emitted, fmt.Errorf("payload for span %d: %w", i, err)
		}
		ev.Payload = raw

		if err := em.Emit(&ev); err != nil {
			return emitted, fmt.Errorf("emit span %d: %w", i, err)
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
