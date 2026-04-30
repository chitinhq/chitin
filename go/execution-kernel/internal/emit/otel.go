// F4 OTEL emit MVP. Projects canonical chain events onto OTLP/HTTP JSON spans
// and POSTs them to a configured collector. One-way bridge — chain is canonical,
// OTEL is non-authoritative projection.
//
// Failure invariant: OTEL emit failure must not affect kernel JSONL/index commit.
// All errors are logged and dropped; ProjectAndPost is detached.
//
// Spec: docs/superpowers/specs/2026-04-29-otel-emit-mvp-design.md
package emit

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/chitinhq/chitin/go/execution-kernel/internal/chain"
	"github.com/chitinhq/chitin/go/execution-kernel/internal/event"
)

// otelExporter is the F4 OTEL emit projector. Nil when OTEL is not configured.
type otelExporter struct {
	endpoint string
	client   *http.Client
}

// newOTELExporter constructs an exporter from the OTEL_EXPORTER_OTLP_TRACES_ENDPOINT
// (or OTEL_EXPORTER_OTLP_ENDPOINT) env var. Returns nil if neither is set —
// caller treats nil as "OTEL disabled" and skips projection.
func newOTELExporter() *otelExporter {
	ep := endpointFromEnv()
	if ep == "" {
		return nil
	}
	return &otelExporter{
		endpoint: ep,
		client:   &http.Client{Timeout: 2 * time.Second},
	}
}

// ProjectAndPost projects ev to a span and POSTs it asynchronously. Returns
// immediately; the goroutine logs and drops any errors. Safe to call when x
// is nil — it's a no-op.
func (x *otelExporter) ProjectAndPost(ev *event.Event, idx *chain.Index) {
	if x == nil {
		return
	}
	go func() {
		span, err := projectToSpan(ev, idx)
		if err != nil {
			log.Printf("otel: project: %v", err)
			return
		}
		body, err := encodeRequest([]otlpSpan{span})
		if err != nil {
			log.Printf("otel: encode: %v", err)
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := x.post(ctx, body); err != nil {
			log.Printf("otel: post: %v", err)
		}
	}()
}

// projectToSpan maps a canonical event onto an OTLP/HTTP JSON span.
// Caller is responsible for ev being non-nil.
func projectToSpan(ev *event.Event, idx *chain.Index) (otlpSpan, error) {
	parent, err := parentSpanID(ev, idx)
	if err != nil {
		return otlpSpan{}, fmt.Errorf("parent span id: %w", err)
	}
	nano, err := tsToUnixNano(ev.Ts)
	if err != nil {
		return otlpSpan{}, fmt.Errorf("parse ts: %w", err)
	}
	nanoStr := strconv.FormatInt(nano, 10)

	attrs := []otlpAttr{
		{Key: "agent.id", Value: otlpValue{StringValue: ev.AgentInstanceID}},
	}

	if len(ev.Payload) > 0 {
		var payload map[string]any
		if err := json.Unmarshal(ev.Payload, &payload); err == nil {
			if tn, ok := payload["tool_name"].(string); ok && tn != "" {
				attrs = append(attrs, otlpAttr{Key: "tool.name", Value: otlpValue{StringValue: tn}})
			}
			if dt, ok := payload["decision"].(string); ok && dt != "" {
				attrs = append(attrs, otlpAttr{Key: "decision.type", Value: otlpValue{StringValue: dt}})
			}
		}
	}

	return otlpSpan{
		TraceID:           traceIDFromChainID(ev.ChainID),
		SpanID:            spanIDFromHash(ev.ThisHash),
		ParentSpanID:      parent,
		Name:              ev.EventType,
		Kind:              1, // SPAN_KIND_INTERNAL
		StartTimeUnixNano: nanoStr,
		EndTimeUnixNano:   nanoStr, // point-in-time for v1; post_tool_use carries duration_ms in attrs (deferred)
		Attributes:        attrs,
	}, nil
}

// parentSpanID implements the three-branch rule (see spec §"Parent rules"):
//   1. prev_hash != nil    → within-chain parent (prev event in same chain)
//   2. parent_chain_id set → cross-chain parent (last event of parent chain)
//   3. otherwise           → root event of root chain (empty string)
func parentSpanID(ev *event.Event, idx *chain.Index) (string, error) {
	if ev.PrevHash != nil {
		return spanIDFromHash(*ev.PrevHash), nil
	}
	if ev.ParentChainID != nil {
		info, err := idx.Get(*ev.ParentChainID)
		if err != nil {
			return "", err
		}
		if info != nil && info.LastHash != "" {
			return spanIDFromHash(info.LastHash), nil
		}
	}
	return "", nil
}

// traceIDFromChainID encodes a UUID chain_id as a 32-hex-char traceId by
// stripping hyphens. OTLP/HTTP JSON traceId is a case-insensitive hex string.
func traceIDFromChainID(id string) string {
	return strings.ReplaceAll(id, "-", "")
}

// spanIDFromHash takes the first 16 hex chars of a SHA-256 chain hash.
// OTLP/HTTP JSON spanId is 16 hex chars (8 bytes).
func spanIDFromHash(hash string) string {
	if len(hash) < 16 {
		return hash
	}
	return hash[:16]
}

// tsToUnixNano parses an RFC3339 timestamp string into nanoseconds since epoch.
func tsToUnixNano(ts string) (int64, error) {
	t, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		return 0, err
	}
	return t.UnixNano(), nil
}

// endpointFromEnv resolves the OTLP traces endpoint. Returns empty string when
// neither env var is set — caller treats that as "OTEL disabled".
func endpointFromEnv() string {
	if e := os.Getenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT"); e != "" {
		return e
	}
	if e := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"); e != "" {
		return strings.TrimRight(e, "/") + "/v1/traces"
	}
	return ""
}

// post sends body as application/json to x.endpoint. Returns error on transport
// failure or HTTP status >= 400. Caller (ProjectAndPost) logs and drops.
func (x *otelExporter) post(ctx context.Context, body []byte) error {
	req, err := http.NewRequestWithContext(ctx, "POST", x.endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := x.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("otlp http %d", resp.StatusCode)
	}
	return nil
}

// encodeRequest builds the OTLP/HTTP JSON ExportTraceServiceRequest body.
// Per OTLP/HTTP+JSON spec: lowerCamelCase field names, hex-encoded trace/span
// IDs (not base64), int64 nanos as decimal strings.
func encodeRequest(spans []otlpSpan) ([]byte, error) {
	req := otlpRequest{
		ResourceSpans: []otlpResourceSpans{{
			Resource: otlpResource{
				Attributes: []otlpAttr{
					{Key: "service.name", Value: otlpValue{StringValue: "chitin-kernel"}},
					{Key: "service.version", Value: otlpValue{StringValue: "0.0.0"}},
				},
			},
			ScopeSpans: []otlpScopeSpans{{
				Scope: otlpScope{Name: "chitin"},
				Spans: spans,
			}},
		}},
	}
	return json.Marshal(req)
}

// OTLP/HTTP JSON wire types. Field tags use lowerCamelCase per the proto3 → JSON
// mapping the OpenTelemetry Protocol Specification mandates. Reference:
// https://github.com/open-telemetry/opentelemetry-proto/blob/main/docs/specification.md

type otlpRequest struct {
	ResourceSpans []otlpResourceSpans `json:"resourceSpans"`
}

type otlpResourceSpans struct {
	Resource   otlpResource     `json:"resource"`
	ScopeSpans []otlpScopeSpans `json:"scopeSpans"`
}

type otlpResource struct {
	Attributes []otlpAttr `json:"attributes"`
}

type otlpScopeSpans struct {
	Scope otlpScope  `json:"scope"`
	Spans []otlpSpan `json:"spans"`
}

type otlpScope struct {
	Name string `json:"name"`
}

type otlpSpan struct {
	TraceID           string     `json:"traceId"`
	SpanID            string     `json:"spanId"`
	ParentSpanID      string     `json:"parentSpanId,omitempty"`
	Name              string     `json:"name"`
	Kind              int        `json:"kind"`
	StartTimeUnixNano string     `json:"startTimeUnixNano"`
	EndTimeUnixNano   string     `json:"endTimeUnixNano"`
	Attributes        []otlpAttr `json:"attributes,omitempty"`
}

type otlpAttr struct {
	Key   string    `json:"key"`
	Value otlpValue `json:"value"`
}

type otlpValue struct {
	StringValue string `json:"stringValue,omitempty"`
}
