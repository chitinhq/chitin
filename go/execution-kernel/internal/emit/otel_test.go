package emit

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/chitinhq/chitin/go/execution-kernel/internal/chain"
	"github.com/chitinhq/chitin/go/execution-kernel/internal/event"
)

// ptr returns a pointer to v. Test helper for nullable event fields.
func ptr[T any](v T) *T { return &v }

// openTestIndex opens a fresh chain.Index in a temp dir.
func openTestIndex(t *testing.T) *chain.Index {
	t.Helper()
	idx, err := chain.OpenIndex(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open index: %v", err)
	}
	t.Cleanup(func() { _ = idx.Close() })
	return idx
}

// TestProjectToSpan_Mapping covers all 4 in-scope event types from the spec.
// Asserts the locked field mappings: chain_id → traceId, this_hash → spanId,
// event_type → name, agent_instance_id → attributes["agent.id"], plus the
// payload-derived attributes (tool.name on pre/post_tool_use, decision.type
// on decision events).
func TestProjectToSpan_Mapping(t *testing.T) {
	idx := openTestIndex(t)
	const (
		chainID  = "550e8400-e29b-41d4-a716-446655440000"
		thisHash = "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"
		ts       = "2026-04-30T01:39:37.647Z"
	)
	wantTraceID := "550e8400e29b41d4a716446655440000"
	wantSpanID := "abcdef0123456789"

	cases := []struct {
		name         string
		ev           *event.Event
		wantName     string
		wantAttrs    map[string]string // stringValue attrs
		wantIntAttrs map[string]string // intValue attrs (decimal string per OTLP)
	}{
		{
			name: "session_start",
			ev: &event.Event{
				EventType: "session_start", ChainID: chainID, ThisHash: thisHash,
				AgentInstanceID: "claude-code-1", Ts: ts,
				Payload: json.RawMessage(`{}`),
			},
			wantName:  "session_start",
			wantAttrs: map[string]string{"agent.id": "claude-code-1"},
		},
		{
			name: "pre_tool_use",
			ev: &event.Event{
				EventType: "pre_tool_use", ChainID: chainID, ThisHash: thisHash,
				AgentInstanceID: "claude-code-1", Ts: ts,
				Payload: json.RawMessage(`{"tool_name":"Bash"}`),
			},
			wantName: "pre_tool_use",
			wantAttrs: map[string]string{
				"agent.id":  "claude-code-1",
				"tool.name": "Bash",
			},
		},
		{
			name: "decision",
			ev: &event.Event{
				EventType: "decision", ChainID: chainID, ThisHash: thisHash,
				AgentInstanceID: "claude-code-1", Ts: ts,
				Payload: json.RawMessage(`{"decision":"deny"}`),
			},
			wantName: "decision",
			wantAttrs: map[string]string{
				"agent.id":      "claude-code-1",
				"decision.type": "deny",
			},
		},
		{
			name: "post_tool_use",
			ev: &event.Event{
				EventType: "post_tool_use", ChainID: chainID, ThisHash: thisHash,
				AgentInstanceID: "claude-code-1", Ts: ts,
				Payload: json.RawMessage(`{"tool_name":"Bash"}`),
			},
			wantName: "post_tool_use",
			wantAttrs: map[string]string{
				"agent.id":  "claude-code-1",
				"tool.name": "Bash",
			},
		},
		{
			name: "post_tool_use with duration_ms (spec attribute)",
			ev: &event.Event{
				EventType: "post_tool_use", ChainID: chainID, ThisHash: thisHash,
				AgentInstanceID: "claude-code-1", Ts: ts,
				Payload: json.RawMessage(`{"tool_name":"Bash","duration_ms":42}`),
			},
			wantName: "post_tool_use",
			wantAttrs: map[string]string{
				"agent.id":  "claude-code-1",
				"tool.name": "Bash",
			},
			wantIntAttrs: map[string]string{
				"duration_ms": "42",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			span, err := projectToSpan(tc.ev, idx)
			if err != nil {
				t.Fatalf("projectToSpan: %v", err)
			}
			if span.TraceID != wantTraceID {
				t.Errorf("TraceID: got %q, want %q", span.TraceID, wantTraceID)
			}
			if span.SpanID != wantSpanID {
				t.Errorf("SpanID: got %q, want %q", span.SpanID, wantSpanID)
			}
			if span.Name != tc.wantName {
				t.Errorf("Name: got %q, want %q", span.Name, tc.wantName)
			}
			if span.Kind != 1 {
				t.Errorf("Kind: got %d, want 1 (SPAN_KIND_INTERNAL)", span.Kind)
			}
			// startTime == endTime for v1 (point-in-time)
			if span.StartTimeUnixNano != span.EndTimeUnixNano {
				t.Errorf("start != end (v1 expects point-in-time): %s vs %s",
					span.StartTimeUnixNano, span.EndTimeUnixNano)
			}
			// nano string is decimal int per OTLP/HTTP+JSON
			if span.StartTimeUnixNano == "" {
				t.Errorf("StartTimeUnixNano empty")
			}
			gotStrAttrs := map[string]string{}
			gotIntAttrs := map[string]string{}
			for _, a := range span.Attributes {
				if a.Value.IntValue != nil {
					gotIntAttrs[a.Key] = *a.Value.IntValue
				} else {
					gotStrAttrs[a.Key] = a.Value.StringValue
				}
			}
			for k, want := range tc.wantAttrs {
				if got := gotStrAttrs[k]; got != want {
					t.Errorf("string attr %s: got %q, want %q", k, got, want)
				}
			}
			for k, want := range tc.wantIntAttrs {
				if got := gotIntAttrs[k]; got != want {
					t.Errorf("int attr %s: got %q, want %q", k, got, want)
				}
			}
		})
	}
}

// TestParentSpanIdRules covers the three parent branches from the spec:
// within-chain (prev_hash), cross-chain (parent_chain_id), and root (neither).
func TestParentSpanIdRules(t *testing.T) {
	idx := openTestIndex(t)

	const parentChainID = "11111111-1111-1111-1111-111111111111"
	const parentLastHash = "deadbeef0123456789abcdef0123456789abcdef0123456789abcdef01234567"
	if err := idx.Upsert(parentChainID, 5, parentLastHash); err != nil {
		t.Fatalf("seed parent chain: %v", err)
	}

	cases := []struct {
		name string
		ev   *event.Event
		want string
	}{
		{
			name: "within-chain (prev_hash takes precedence)",
			ev: &event.Event{
				ChainID:  "child-a",
				PrevHash: ptr("11112222333344445555666677778888aaaabbbbccccddddeeeeffff00001111"),
				// parent_chain_id should be ignored in this branch
				ParentChainID: ptr(parentChainID),
			},
			want: "1111222233334444",
		},
		{
			name: "cross-chain (parent_chain_id, prev_hash nil)",
			ev: &event.Event{
				ChainID:       "child-b",
				ParentChainID: ptr(parentChainID),
			},
			want: "deadbeef01234567",
		},
		{
			name: "root event of root chain (neither set)",
			ev:   &event.Event{ChainID: "root"},
			want: "",
		},
		{
			name: "cross-chain with unknown parent (parent_chain_id set but not in index)",
			ev: &event.Event{
				ChainID:       "orphan",
				ParentChainID: ptr("99999999-9999-9999-9999-999999999999"),
			},
			want: "", // graceful: empty parent rather than error
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parentSpanID(tc.ev, idx)
			if err != nil {
				t.Fatalf("parentSpanID: %v", err)
			}
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// TestEncodeRequest_Shape sanity-checks the OTLP/HTTP JSON body shape against
// the spec: lowerCamelCase field names, hex traceId/spanId not base64, decimal
// nano string. We marshal a single span and parse the JSON back to assert keys.
func TestEncodeRequest_Shape(t *testing.T) {
	durStr := "42"
	span := otlpSpan{
		TraceID:           "550e8400e29b41d4a716446655440000",
		SpanID:            "abcdef0123456789",
		ParentSpanID:      "1111222233334444",
		Name:              "post_tool_use",
		Kind:              1,
		StartTimeUnixNano: "1714521577000000000",
		EndTimeUnixNano:   "1714521577000000000",
		Attributes: []otlpAttr{
			{Key: "agent.id", Value: otlpValue{StringValue: "claude-code-1"}},
			{Key: "tool.name", Value: otlpValue{StringValue: "Bash"}},
			{Key: "duration_ms", Value: otlpValue{IntValue: &durStr}},
		},
	}
	body, err := encodeRequest([]otlpSpan{span})
	if err != nil {
		t.Fatalf("encodeRequest: %v", err)
	}

	// Hex IDs MUST appear unmodified — base64 encoding would mangle them.
	bodyStr := string(body)
	for _, want := range []string{
		`"traceId":"550e8400e29b41d4a716446655440000"`,
		`"spanId":"abcdef0123456789"`,
		`"parentSpanId":"1111222233334444"`,
		`"startTimeUnixNano":"1714521577000000000"`,
		`"resourceSpans":`,
		`"scopeSpans":`,
		`"service.name"`,
		// IntValue: per OTLP/HTTP+JSON, int64 attributes encode as decimal string
		// inside an "intValue" key — distinct from "stringValue".
		`"intValue":"42"`,
		`"key":"duration_ms"`,
	} {
		if !strings.Contains(bodyStr, want) {
			t.Errorf("body missing %q\nbody: %s", want, bodyStr)
		}
	}

	// Forbidden: snake_case (proto field name) instead of lowerCamelCase
	for _, forbidden := range []string{
		`"trace_id"`,
		`"span_id"`,
		`"parent_span_id"`,
		`"start_time_unix_nano"`,
	} {
		if strings.Contains(bodyStr, forbidden) {
			t.Errorf("body contains forbidden snake_case key %q", forbidden)
		}
	}
}

// TestEndpointFromEnv covers all three branches: traces-specific env, generic
// env (with /v1/traces appended), and unset (returns "" → OTEL disabled).
func TestEndpointFromEnv(t *testing.T) {
	t.Run("traces-specific env wins", func(t *testing.T) {
		t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "http://traces.example/x/v1/traces")
		t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://generic.example")
		got := endpointFromEnv()
		if got != "http://traces.example/x/v1/traces" {
			t.Errorf("got %q", got)
		}
	})
	t.Run("generic env appends /v1/traces", func(t *testing.T) {
		t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "")
		t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://generic.example")
		got := endpointFromEnv()
		if got != "http://generic.example/v1/traces" {
			t.Errorf("got %q", got)
		}
	})
	t.Run("trims trailing slash on generic env", func(t *testing.T) {
		t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "")
		t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://generic.example/")
		got := endpointFromEnv()
		if got != "http://generic.example/v1/traces" {
			t.Errorf("got %q", got)
		}
	})
	t.Run("unset returns empty (OTEL disabled)", func(t *testing.T) {
		t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "")
		t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
		if got := endpointFromEnv(); got != "" {
			t.Errorf("got %q, want empty", got)
		}
	})
}

// TestProjectAndPost_RoundtripToTestServer verifies the happy path: spin up
// an httptest server, point the exporter at it, fire a span, assert the server
// received a body containing the expected hex traceId.
func TestProjectAndPost_RoundtripToTestServer(t *testing.T) {
	var (
		mu      sync.Mutex
		gotBody string
		gotPath string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("server read body: %v", err)
		}
		mu.Lock()
		gotBody = string(buf)
		gotPath = r.URL.Path
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", srv.URL+"/v1/traces")
	x := newOTELExporter()
	if x == nil {
		t.Fatal("expected exporter, got nil")
	}

	idx := openTestIndex(t)
	ev := &event.Event{
		EventType:       "pre_tool_use",
		ChainID:         "550e8400-e29b-41d4-a716-446655440000",
		ThisHash:        "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789",
		AgentInstanceID: "claude-code-1",
		Ts:              "2026-04-30T01:39:37.647Z",
		Payload:         json.RawMessage(`{"tool_name":"Bash"}`),
	}

	x.ProjectAndPost(ev, idx) // sync — returns after the POST completes

	mu.Lock()
	body, path := gotBody, gotPath
	mu.Unlock()

	if path != "/v1/traces" {
		t.Errorf("path: got %q, want /v1/traces", path)
	}
	if !strings.Contains(body, `"traceId":"550e8400e29b41d4a716446655440000"`) {
		t.Errorf("body missing expected traceId\nbody: %s", body)
	}
	if !strings.Contains(body, `"tool.name"`) {
		t.Errorf("body missing tool.name attr\nbody: %s", body)
	}
}

// TestProjectAndPost_NilExporterIsNoOp ensures the nil-exporter path doesn't
// panic. This is the "OTEL disabled" branch — newOTELExporter returns nil
// when env is unset, and the kernel must continue to function.
func TestProjectAndPost_NilExporterIsNoOp(t *testing.T) {
	idx := openTestIndex(t)
	ev := &event.Event{
		EventType: "session_start", ChainID: "x", ThisHash: "y",
		AgentInstanceID: "z", Ts: "2026-04-30T01:39:37Z",
	}
	var x *otelExporter // nil
	x.ProjectAndPost(ev, idx)
	// Pass = no panic
}

// TestKernelSurvivesOTELFailure verifies the F4 failure invariant: when the
// configured OTEL endpoint is unreachable, Emit must still return nil, the
// JSONL line must be written, and the chain index must be updated. OTEL
// export happens synchronously after the durable JSONL/index commit; export
// errors are logged and dropped.
//
// This is the "kernel-write-survives-OTEL-failure" invariant from the spec.
func TestKernelSurvivesOTELFailure(t *testing.T) {
	dir, idx := newEnv(t)

	// Refused port (no listener at :1) makes the synchronous OTEL POST fail fast.
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "http://127.0.0.1:1/v1/traces")

	logPath := filepath.Join(dir, "events.jsonl")
	em := Emitter{LogPath: logPath, Index: idx}
	em.EnableOTELFromEnv()
	if em.OTEL == nil {
		t.Fatal("expected OTEL enabled from env, got nil exporter")
	}

	ev := minimalSessionStart("550e8400-e29b-41d4-a716-446655440000", 0)
	// Emit blocks for the OTEL POST (sync v1) — the http.Client timeout caps
	// this at 2s. Connection refused is faster than that, so this returns
	// quickly. The chain commit ran before the OTEL POST began.
	if err := em.Emit(ev); err != nil {
		t.Fatalf("Emit must return nil even when OTEL fails: got %v", err)
	}

	// JSONL line written?
	lines := readLines(t, logPath)
	if len(lines) != 1 {
		t.Fatalf("expected 1 JSONL line, got %d", len(lines))
	}
	if got, _ := lines[0]["event_type"].(string); got != "session_start" {
		t.Errorf("event_type: got %q, want session_start", got)
	}

	// Chain index updated?
	info, err := idx.Get(ev.ChainID)
	if err != nil {
		t.Fatalf("idx.Get: %v", err)
	}
	if info == nil || info.LastSeq != 0 || info.LastHash != ev.ThisHash {
		t.Errorf("chain index not updated: %+v want seq=0 hash=%q", info, ev.ThisHash)
	}
}
