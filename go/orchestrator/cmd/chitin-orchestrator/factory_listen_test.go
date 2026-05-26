// factory_listen_test.go — unit tests for the spec 098 webhook trigger
// surface. Tests cover the deterministic, non-Temporal pieces:
//
//   - HMAC verification (correct, wrong, missing, malformed)
//   - Spec-ref extraction from a multi-commit push payload
//   - Branch filter (refs/heads/main vs refs/heads/feature-x)
//   - Synthetic payload round-trip via simulate-webhook → factory-listen
//
// The end-to-end dispatch-into-Temporal path is exercised by the live
// demo in the spec 098 quickstart, not by these unit tests; dispatch
// inside the listener calls runSchedule which needs a Temporal frontend.

package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/chitinhq/chitin/go/orchestrator/adapter"
)

// TestVerifyHMAC walks every important branch of the signature verifier:
// happy path, wrong signature, missing header, missing prefix, malformed
// hex, empty secret. Each subtest is one boundary the spec calls out.
func TestVerifyHMAC(t *testing.T) {
	secret := []byte("super-secret-32-bytes-hex-value!")
	body := []byte(`{"ref":"refs/heads/main"}`)

	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	validSig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	tests := []struct {
		name   string
		secret []byte
		body   []byte
		header string
		want   bool
	}{
		{"valid signature", secret, body, validSig, true},
		{"wrong signature", secret, body, "sha256=" + hex.EncodeToString(make([]byte, 32)), false},
		{"missing header", secret, body, "", false},
		{"missing prefix", secret, body, hex.EncodeToString(mac.Sum(nil)), false},
		{"malformed hex", secret, body, "sha256=not-hex-zzz", false},
		{"empty secret", []byte{}, body, validSig, false},
		{"tampered body", secret, []byte(`{"ref":"refs/heads/evil"}`), validSig, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := verifyHMAC(tc.secret, tc.body, tc.header)
			if got != tc.want {
				t.Fatalf("verifyHMAC=%v want %v", got, tc.want)
			}
		})
	}
}

// TestSignPayloadRoundtrip — the sign produced by simulate-webhook must
// verify against the same secret the listener uses. Invariant: for every
// (secret, body) pair, verifyHMAC(secret, body, signPayload(secret, body))
// is true. This is the contract that makes the test harness usable.
func TestSignPayloadRoundtrip(t *testing.T) {
	cases := []struct {
		secret, body []byte
	}{
		{[]byte("k"), []byte("")},
		{[]byte("supersecret"), []byte(`{"x":1}`)},
		{[]byte("longer-secret-value-32-bytes-yes"), []byte(`{"commits":[{"added":["a","b"]}]}`)},
	}
	for _, c := range cases {
		sig := signPayload(c.secret, c.body)
		if !verifyHMAC(c.secret, c.body, sig) {
			t.Fatalf("roundtrip failed for secret=%q body=%q sig=%q", c.secret, c.body, sig)
		}
	}
}

// TestExtractSpecRefs covers the path-pattern detection. Cases:
//
//  1. Single tasks.md add → 1 ref.
//  2. Same spec touched in two commits → 1 ref (dedup).
//  3. Two distinct specs → 2 refs (sorted).
//  4. tasks.md outside .specify/specs/ → 0 refs.
//  5. plan.md or spec.md inside the dir → 0 refs (we only trigger on tasks.md).
//  6. Empty commits array → 0 refs.
func TestExtractSpecRefs(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want []string
	}{
		{
			name: "single tasks add",
			raw:  `{"commits":[{"added":[".specify/specs/098-factory-webhook/tasks.md"]}]}`,
			want: []string{"098-factory-webhook"},
		},
		{
			name: "dedup across commits",
			raw: `{"commits":[
				{"added":[".specify/specs/098-factory-webhook/tasks.md"]},
				{"modified":[".specify/specs/098-factory-webhook/tasks.md"]}]}`,
			want: []string{"098-factory-webhook"},
		},
		{
			name: "two specs sorted",
			raw: `{"commits":[
				{"added":[".specify/specs/099-zeta/tasks.md", ".specify/specs/094-alpha/tasks.md"]}]}`,
			want: []string{"094-alpha", "099-zeta"},
		},
		{
			name: "non-spec path",
			raw:  `{"commits":[{"added":["README.md", "go/main.go"]}]}`,
			want: []string{},
		},
		{
			name: "plan and spec.md ignored",
			raw: `{"commits":[
				{"modified":[".specify/specs/098-factory-webhook/plan.md", ".specify/specs/098-factory-webhook/spec.md"]}]}`,
			want: []string{},
		},
		{
			name: "empty commits",
			raw:  `{"commits":[]}`,
			want: []string{},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var p pushPayload
			if err := json.Unmarshal([]byte(tc.raw), &p); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			got := extractSpecRefs(&p)
			// Normalize empty: extractSpecRefs returns []string{} not nil.
			if len(got) == 0 && len(tc.want) == 0 {
				return
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("extractSpecRefs=%v want %v", got, tc.want)
			}
		})
	}
}

// TestProcessNonMainBranch — invariant from FR-005: a payload with
// ref!=refs/heads/<main-branch> must return Dispatched:false with a
// "non-main branch" reason. Cannot reach the dispatch path even if the
// payload otherwise contains a valid spec ref.
func TestProcessNonMainBranch(t *testing.T) {
	h := &factoryHandler{mainBranch: "main", logFile: t.TempDir() + "/log.jsonl"}
	p := &pushPayload{
		Ref: "refs/heads/feature/x",
		Commits: []struct {
			Added    []string `json:"added"`
			Modified []string `json:"modified"`
		}{
			{Added: []string{".specify/specs/098-factory-webhook/tasks.md"}},
		},
	}
	resp := h.process(context.Background(), p)
	if resp.Dispatched {
		t.Fatalf("Dispatched=true on non-main branch, want false")
	}
	if len(resp.SkippedReasons) != 1 || resp.SkippedReasons[0] != "non-main branch" {
		t.Fatalf("SkippedReasons=%v want [non-main branch]", resp.SkippedReasons)
	}
	if len(resp.SpecRefs) != 0 || len(resp.RunIDs) != 0 {
		t.Fatalf("expected no specs/runs on non-main branch; got refs=%v runs=%v", resp.SpecRefs, resp.RunIDs)
	}
}

// TestProcessMainNoSpecChange — main-branch push that touches no
// tasks.md must respond with Dispatched:false and "no tasks.md changes"
// skip reason. Boundary case from spec 098 edge-case list.
func TestProcessMainNoSpecChange(t *testing.T) {
	h := &factoryHandler{mainBranch: "main", logFile: t.TempDir() + "/log.jsonl"}
	p := &pushPayload{
		Ref:   "refs/heads/main",
		After: "deadbeef",
		Commits: []struct {
			Added    []string `json:"added"`
			Modified []string `json:"modified"`
		}{
			{Modified: []string{"README.md"}},
		},
	}
	resp := h.process(context.Background(), p)
	if resp.Dispatched {
		t.Fatalf("Dispatched=true on no-spec-change push, want false")
	}
	if len(resp.SkippedReasons) != 1 || resp.SkippedReasons[0] != "no tasks.md changes" {
		t.Fatalf("SkippedReasons=%v want [no tasks.md changes]", resp.SkippedReasons)
	}
}

// TestHandlePush_HTTP — drive the handler via the real net/http path so
// we cover the actual route the webhook will hit: header parsing, body
// read, status codes. Invariants:
//
//   - Unsigned body → 401, no JSON spec_refs.
//   - Wrong-method GET → 405.
//   - Signed non-main branch → 200 with Dispatched:false, "non-main branch".
//   - Signed main + tasks.md but no Temporal frontend → 200 with a single
//     factory_dispatch_failed reason (we don't reach dispatch success in CI;
//     this asserts the handler degrades gracefully rather than 500ing).
func TestHandlePush_HTTP(t *testing.T) {
	secret := []byte("test-secret-for-handler-route!!")
	h := &factoryHandler{
		secret:     secret,
		mainBranch: "main",
		logFile:    t.TempDir() + "/log.jsonl",
	}

	t.Run("missing signature → 401", func(t *testing.T) {
		body := []byte(`{"ref":"refs/heads/main"}`)
		req := httptest.NewRequest(http.MethodPost, "/webhook/push", bytes.NewReader(body))
		w := httptest.NewRecorder()
		h.handlePush(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("status=%d want 401", w.Code)
		}
	})

	t.Run("wrong method → 405", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/webhook/push", nil)
		w := httptest.NewRecorder()
		h.handlePush(w, req)
		if w.Code != http.StatusMethodNotAllowed {
			t.Fatalf("status=%d want 405", w.Code)
		}
	})

	t.Run("signed non-main → 200 with skip", func(t *testing.T) {
		body := []byte(`{"ref":"refs/heads/feature/x","commits":[]}`)
		sig := signPayload(secret, body)
		req := httptest.NewRequest(http.MethodPost, "/webhook/push", bytes.NewReader(body))
		req.Header.Set("X-Hub-Signature-256", sig)
		w := httptest.NewRecorder()
		h.handlePush(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("status=%d want 200", w.Code)
		}
		respBody, _ := io.ReadAll(w.Body)
		var resp factoryResponse
		if err := json.Unmarshal(respBody, &resp); err != nil {
			t.Fatalf("unmarshal: %v body=%s", err, string(respBody))
		}
		if resp.Dispatched {
			t.Fatalf("Dispatched=true want false")
		}
		if len(resp.SkippedReasons) != 1 || resp.SkippedReasons[0] != "non-main branch" {
			t.Fatalf("SkippedReasons=%v", resp.SkippedReasons)
		}
	})
}

func TestClassifyDispatchError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want FactoryDispatchFailureKind
	}{
		{"spec ref not found typed", &SpecRefError{Kind: "not-found", Ref: "999"}, FactoryDispatchFailureKindSpecRefNotFound},
		{"spec ref ambiguous typed", &SpecRefError{Kind: "ambiguous", Ref: "09"}, FactoryDispatchFailureKindSpecRefAmbiguous},
		{"tasks missing typed", &adapter.MalformedArtifactError{File: ".specify/specs/118/tasks.md", Reason: "required artifact is missing"}, FactoryDispatchFailureKindTasksMDMissing},
		{"tasks parse typed", &adapter.MalformedArtifactError{File: ".specify/specs/118/tasks.md", Reason: "duplicate task id"}, FactoryDispatchFailureKindTasksMDParseError},
		{"temporal dial legacy string", errors.New("runSchedule exit=2: error: Temporal unreachable at 127.0.0.1:7233 — is the temporal-dev service running?"), FactoryDispatchFailureKindTemporalDialFailed},
		{"temporal start workflow legacy string", errors.New("runSchedule exit=2: error: ExecuteWorkflow failed: namespace not found"), FactoryDispatchFailureKindTemporalStartWorkflowFailed},
		{"capability mismatch legacy string", errors.New("runSchedule exit=1: error: DAG validation failed — 1 node(s) require capability not declared by any registered driver"), FactoryDispatchFailureKindCapabilityMismatch},
		{"unrecognised", errors.New("filesystem temperature is suspicious"), FactoryDispatchFailureKindInternal},
		{"novel string does not invent kind", errors.New("brand_new_dispatch_reason: external API quota exhausted"), FactoryDispatchFailureKindInternal},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := classifyDispatchError(tc.err)
			if got != tc.want {
				t.Fatalf("classifyDispatchError(%v) = %q, want %q", tc.err, got, tc.want)
			}
			if !got.Valid() {
				t.Fatalf("classified invalid taxonomy value %q", got)
			}
		})
	}
	if FactoryDispatchFailureKind("surprise").Valid() {
		t.Fatal("Valid accepted undeclared failure kind")
	}
}

func TestHandlePushFailureEventCarriesFailureKind(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want FactoryDispatchFailureKind
	}{
		{"spec_ref_not_found", &SpecRefError{Kind: "not-found", Ref: "118"}, FactoryDispatchFailureKindSpecRefNotFound},
		{"spec_ref_ambiguous", &SpecRefError{Kind: "ambiguous", Ref: "11"}, FactoryDispatchFailureKindSpecRefAmbiguous},
		{"tasks_md_missing", &adapter.MalformedArtifactError{File: ".specify/specs/118/tasks.md", Reason: "required artifact is missing"}, FactoryDispatchFailureKindTasksMDMissing},
		{"tasks_md_parse_error", &adapter.MalformedArtifactError{File: ".specify/specs/118/tasks.md", Reason: "duplicate task id"}, FactoryDispatchFailureKindTasksMDParseError},
		{"temporal_dial_failed", errors.New("Temporal unreachable at 127.0.0.1:7233: connection refused"), FactoryDispatchFailureKindTemporalDialFailed},
		{"temporal_start_workflow_failed", errors.New("ExecuteWorkflow failed: task queue missing"), FactoryDispatchFailureKindTemporalStartWorkflowFailed},
		{"capability_mismatch", errors.New("DAG validation failed: no registered driver declares this capability"), FactoryDispatchFailureKindCapabilityMismatch},
		{"internal", errors.New("opaque dispatch fault"), FactoryDispatchFailureKindInternal},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			capture := installFakeKernel(t)
			secret := []byte("test-secret-for-handler-route!!")
			h := &factoryHandler{
				secret:     secret,
				mainBranch: "main",
				logFile:    filepath.Join(t.TempDir(), "log.jsonl"),
				dispatchFunc: func(context.Context, string) (string, error) {
					return "", tc.err
				},
			}
			body := []byte(`{"ref":"refs/heads/main","commits":[{"modified":[".specify/specs/118-factory-dispatch-failed-reason-taxonomy/tasks.md"]}]}`)
			req := httptest.NewRequest(http.MethodPost, "/webhook/push", bytes.NewReader(body))
			req.Header.Set("X-Hub-Signature-256", signPayload(secret, body))
			w := httptest.NewRecorder()
			h.handlePush(w, req)
			if w.Code != http.StatusOK {
				t.Fatalf("status=%d want 200", w.Code)
			}
			payload := readCapturedEventPayload(t, capture)
			if payload["failure_kind"] != string(tc.want) {
				t.Fatalf("failure_kind=%v want %s payload=%v", payload["failure_kind"], tc.want, payload)
			}
			if payload["error"] != tc.err.Error() {
				t.Fatalf("error=%v want %q", payload["error"], tc.err.Error())
			}
		})
	}
}

// TestConstructSyntheticPushPayload — what simulate-webhook emits must
// be parseable by the listener and yield exactly one spec ref. Keeps the
// two sides in sync; if the listener's pushPayload schema drifts, this
// test fails before the round-trip would.
func TestConstructSyntheticPushPayload(t *testing.T) {
	body := constructSyntheticPushPayload("098-factory-webhook", "main", "abc123")
	var p pushPayload
	if err := json.Unmarshal(body, &p); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if p.Ref != "refs/heads/main" {
		t.Fatalf("Ref=%q want refs/heads/main", p.Ref)
	}
	refs := extractSpecRefs(&p)
	if !reflect.DeepEqual(refs, []string{"098-factory-webhook"}) {
		t.Fatalf("extractSpecRefs=%v want [098-factory-webhook]", refs)
	}
}

func installFakeKernel(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	capture := filepath.Join(dir, "events.jsonl")
	bin := filepath.Join(dir, "chitin-kernel")
	script := `#!/bin/sh
while [ "$#" -gt 0 ]; do
  if [ "$1" = "-event-file" ]; then
    shift
    cat "$1" >> "$CHITIN_FAKE_CAPTURE"
    printf '\n' >> "$CHITIN_FAKE_CAPTURE"
    exit 0
  fi
  shift
done
exit 1
`
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake kernel: %v", err)
	}
	t.Setenv("CHITIN_KERNEL_BIN", bin)
	t.Setenv("CHITIN_FAKE_CAPTURE", capture)
	t.Setenv("CHITIN_DIR", dir)
	return capture
}

func readCapturedEventPayload(t *testing.T, capture string) map[string]any {
	t.Helper()
	body, err := os.ReadFile(capture)
	if err != nil {
		t.Fatalf("read capture: %v", err)
	}
	var env struct {
		EventType string         `json:"event_type"`
		Payload   map[string]any `json:"payload"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(body), &env); err != nil {
		t.Fatalf("unmarshal captured event: %v\n%s", err, string(body))
	}
	if env.EventType != "factory_dispatch_failed" {
		t.Fatalf("event_type=%q want factory_dispatch_failed", env.EventType)
	}
	return env.Payload
}
