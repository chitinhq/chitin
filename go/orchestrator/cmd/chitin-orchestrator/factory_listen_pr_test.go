// factory_listen_pr_test.go — spec 099 slice 3 HTTP-layer tests for
// the /webhook/pr route.

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"go.temporal.io/sdk/client"

	"github.com/chitinhq/chitin/go/orchestrator/activities"
	"github.com/chitinhq/chitin/go/orchestrator/workflows"
	enumspb "go.temporal.io/api/enums/v1"
)

type autoMergeRouteStarter struct {
	starts  []client.StartWorkflowOptions
	inputs  []workflows.AutoMergeInput
	signals []string
}

func (s *autoMergeRouteStarter) ExecuteWorkflow(_ context.Context, opts client.StartWorkflowOptions, _ any, args ...any) (client.WorkflowRun, error) {
	s.starts = append(s.starts, opts)
	if len(args) == 1 {
		if in, ok := args[0].(workflows.AutoMergeInput); ok {
			s.inputs = append(s.inputs, in)
		}
	}
	return nil, nil
}

func (s *autoMergeRouteStarter) SignalWorkflow(_ context.Context, workflowID string, _ string, signalName string, _ any) error {
	s.signals = append(s.signals, workflowID+":"+signalName)
	return nil
}

func (s *autoMergeRouteStarter) Close() {}

func autoMergeDialer(s *autoMergeRouteStarter) temporalDialer {
	return func(context.Context, string) (workflowStarter, string, error) {
		return s, "test", nil
	}
}

func readyLabelPayload(action, label string) []byte {
	body, _ := json.Marshal(map[string]any{
		"action": action,
		"number": 1135,
		"label":  map[string]any{"name": label},
		"pull_request": map[string]any{
			"html_url": "https://github.com/chitinhq/chitin/pull/1135",
			"labels":   []map[string]any{{"name": label}},
		},
		"repository": map[string]any{"full_name": "chitinhq/chitin"},
		"sender":     map[string]any{"login": "chitin-orchestrator[bot]"},
	})
	return body
}

func TestHandlePR_ReadyToMergeLabelStartsAutoMerge(t *testing.T) {
	secret := []byte("test-secret-for-pr-route!!!!")
	starter := &autoMergeRouteStarter{}
	t.Setenv("CHITIN_DIR", t.TempDir())
	h := &factoryHandler{secret: secret, logFile: t.TempDir() + "/log.jsonl", temporalDialer: autoMergeDialer(starter)}

	req := signedPRRequest(t, secret, "pull_request", "delivery-ready", readyLabelPayload("labeled", activities.ReadyToMergeLabel))
	w := httptest.NewRecorder()
	h.handlePR(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d want 200", w.Code)
	}
	if len(starter.starts) != 1 {
		t.Fatalf("starts=%d want 1", len(starter.starts))
	}
	if starter.starts[0].ID != "auto-merge-pr-1135-delivery-ready" {
		t.Fatalf("workflow id=%q", starter.starts[0].ID)
	}
	if starter.starts[0].WorkflowIDReusePolicy != enumspb.WORKFLOW_ID_REUSE_POLICY_REJECT_DUPLICATE {
		t.Fatalf("reuse policy=%v", starter.starts[0].WorkflowIDReusePolicy)
	}
	if starter.inputs[0].Repo != "chitinhq/chitin" || starter.inputs[0].TriggerEventID != "delivery-ready" {
		t.Fatalf("input=%+v", starter.inputs[0])
	}
}

func TestHandlePR_OtherLabelDoesNotStartAutoMerge(t *testing.T) {
	secret := []byte("test-secret-for-pr-route!!!!")
	starter := &autoMergeRouteStarter{}
	h := &factoryHandler{secret: secret, logFile: t.TempDir() + "/log.jsonl", temporalDialer: autoMergeDialer(starter)}
	req := signedPRRequest(t, secret, "pull_request", "delivery-other", readyLabelPayload("labeled", "other"))
	w := httptest.NewRecorder()
	h.handlePR(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d want 200", w.Code)
	}
	if len(starter.starts) != 0 {
		t.Fatalf("starts=%d want 0", len(starter.starts))
	}
}

func TestHandlePR_AutoMergeBreakGlassSkipsStart(t *testing.T) {
	secret := []byte("test-secret-for-pr-route!!!!")
	starter := &autoMergeRouteStarter{}
	t.Setenv("CHITIN_AUTO_MERGE_DISABLED", "1")
	h := &factoryHandler{secret: secret, logFile: t.TempDir() + "/log.jsonl", temporalDialer: autoMergeDialer(starter)}
	req := signedPRRequest(t, secret, "pull_request", "delivery-disabled", readyLabelPayload("labeled", activities.ReadyToMergeLabel))
	w := httptest.NewRecorder()
	h.handlePR(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d want 200", w.Code)
	}
	if len(starter.starts) != 0 {
		t.Fatalf("starts=%d want 0", len(starter.starts))
	}
}

func TestHandlePR_ReadyToMergeUnlabeledSignalsAutoMerge(t *testing.T) {
	secret := []byte("test-secret-for-pr-route!!!!")
	starter := &autoMergeRouteStarter{}
	dir := t.TempDir()
	t.Setenv("CHITIN_DIR", dir)
	fixture := map[string]any{
		"ts": "2026-05-26T00:00:00Z", "event_type": "auto_merge_triggered", "run_id": "auto-merge-pr-1135-delivery-ready",
		"payload": map[string]any{"repo": "chitinhq/chitin", "pr_number": 1135},
	}
	b, _ := json.Marshal(fixture)
	if err := os.WriteFile(dir+"/events-test.jsonl", append(b, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	h := &factoryHandler{secret: secret, logFile: t.TempDir() + "/log.jsonl", temporalDialer: autoMergeDialer(starter)}
	req := signedPRRequest(t, secret, "pull_request", "delivery-unlabel", readyLabelPayload("unlabeled", activities.ReadyToMergeLabel))
	w := httptest.NewRecorder()
	h.handlePR(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d want 200", w.Code)
	}
	if len(starter.starts) != 0 || len(starter.signals) != 1 {
		t.Fatalf("starts=%d signals=%v", len(starter.starts), starter.signals)
	}
	if starter.signals[0] != "auto-merge-pr-1135-delivery-ready:"+workflows.LabelRemovedSignal {
		t.Fatalf("signal=%q", starter.signals[0])
	}
}

// signedPRRequest constructs a signed POST to /webhook/pr with the
// given event type / delivery id / body.
func signedPRRequest(t *testing.T, secret []byte, eventType, deliveryID string, body []byte) *http.Request {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/webhook/pr", bytes.NewReader(body))
	req.Header.Set("X-Hub-Signature-256", signPayload(secret, body))
	req.Header.Set("X-GitHub-Event", eventType)
	req.Header.Set("X-GitHub-Delivery", deliveryID)
	req.Header.Set("Content-Type", "application/json")
	return req
}

func TestHandlePR_MissingSignature_401(t *testing.T) {
	secret := []byte("test-secret-for-pr-route!!!!")
	h := &factoryHandler{secret: secret, logFile: t.TempDir() + "/log.jsonl"}

	body := []byte(`{"action":"opened"}`)
	req := httptest.NewRequest(http.MethodPost, "/webhook/pr", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.handlePR(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status=%d want 401", w.Code)
	}
}

func TestHandlePR_WrongMethod_405(t *testing.T) {
	h := &factoryHandler{secret: []byte("x"), logFile: t.TempDir() + "/log.jsonl"}
	req := httptest.NewRequest(http.MethodGet, "/webhook/pr", nil)
	w := httptest.NewRecorder()
	h.handlePR(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status=%d want 405", w.Code)
	}
}

func TestHandlePR_EligiblePR_Returns200WithEligibleTrue(t *testing.T) {
	secret := []byte("test-secret-for-pr-route!!!!")
	// chain emit shells out — install a fake kernel that swallows it.
	bin, _ := fakeKernelBin(t, 0)
	t.Setenv("CHITIN_KERNEL_BIN", bin)
	h := &factoryHandler{secret: secret, logFile: t.TempDir() + "/log.jsonl"}

	body := []byte(`{
		"action": "opened",
		"number": 100,
		"pull_request": {
			"html_url": "https://github.com/owner/name/pull/100",
			"draft": true,
			"body": "Closes #42\n\nWork done.",
			"commits": 3,
			"additions": 120,
			"deletions": 45,
			"changed_files": 8,
			"labels": [{"name": "chitin-dispatch"}, {"name": "driver:copilot"}]
		},
		"repository": {"full_name": "owner/name"}
	}`)

	req := signedPRRequest(t, secret, "pull_request", "delivery-123", body)
	w := httptest.NewRecorder()
	h.handlePR(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d want 200", w.Code)
	}
	var resp prResponse
	respBody, _ := io.ReadAll(w.Body)
	if err := json.Unmarshal(respBody, &resp); err != nil {
		t.Fatalf("unmarshal: %v body=%s", err, string(respBody))
	}
	if !resp.Received || !resp.Eligible || resp.PRNumber != 100 {
		t.Errorf("resp=%+v; want Received+Eligible+PRNumber=100", resp)
	}
	if resp.EventType != "pull_request" || resp.Action != "opened" {
		t.Errorf("resp event/action = %q/%q; want pull_request/opened", resp.EventType, resp.Action)
	}
}

func TestHandlePR_MissingLabel_Returns200WithEligibleFalseAndReason(t *testing.T) {
	secret := []byte("test-secret-for-pr-route!!!!")
	bin, _ := fakeKernelBin(t, 0)
	t.Setenv("CHITIN_KERNEL_BIN", bin)
	h := &factoryHandler{secret: secret, logFile: t.TempDir() + "/log.jsonl"}

	body := []byte(`{
		"action": "opened",
		"number": 101,
		"pull_request": {
			"html_url": "https://github.com/owner/name/pull/101",
			"body": "Closes #42",
			"labels": [{"name": "kind/feat"}]
		},
		"repository": {"full_name": "owner/name"}
	}`)

	req := signedPRRequest(t, secret, "pull_request", "delivery-124", body)
	w := httptest.NewRecorder()
	h.handlePR(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d want 200", w.Code)
	}
	var resp prResponse
	respBody, _ := io.ReadAll(w.Body)
	_ = json.Unmarshal(respBody, &resp)
	if resp.Eligible {
		t.Errorf("Eligible=true; want false (label missing)")
	}
	if resp.SkippedReason != "missing_label" {
		t.Errorf("SkippedReason=%q; want missing_label", resp.SkippedReason)
	}
}

func TestHandlePR_IgnoredAction_NotEligible(t *testing.T) {
	secret := []byte("test-secret-for-pr-route!!!!")
	bin, _ := fakeKernelBin(t, 0)
	t.Setenv("CHITIN_KERNEL_BIN", bin)
	h := &factoryHandler{secret: secret, logFile: t.TempDir() + "/log.jsonl"}

	body := []byte(`{
		"action": "closed",
		"number": 102,
		"pull_request": {
			"labels": [{"name": "chitin-dispatch"}]
		},
		"repository": {"full_name": "owner/name"}
	}`)

	req := signedPRRequest(t, secret, "pull_request", "d125", body)
	w := httptest.NewRecorder()
	h.handlePR(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d want 200", w.Code)
	}
	var resp prResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Eligible {
		t.Errorf("Eligible=true; want false (action=closed)")
	}
	if resp.SkippedReason != "not_draft_or_ready" {
		t.Errorf("SkippedReason=%q; want not_draft_or_ready", resp.SkippedReason)
	}
}

func TestHandlePR_EmitsCopilotPRActivity_ForLabeledPR(t *testing.T) {
	secret := []byte("test-secret-for-pr-route!!!!")
	bin, sentinel := fakeKernelBin(t, 0)
	t.Setenv("CHITIN_KERNEL_BIN", bin)
	h := &factoryHandler{secret: secret, logFile: t.TempDir() + "/log.jsonl"}

	body := []byte(`{
		"action": "synchronize",
		"number": 200,
		"pull_request": {
			"html_url": "https://github.com/o/r/pull/200",
			"body": "Closes #42",
			"labels": [{"name": "chitin-dispatch"}]
		},
		"repository": {"full_name": "o/r"}
	}`)

	req := signedPRRequest(t, secret, "pull_request", "delivery-XYZ", body)
	w := httptest.NewRecorder()
	h.handlePR(w, req.WithContext(context.Background()))
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d want 200", w.Code)
	}

	captured, err := os.ReadFile(sentinel)
	if err != nil {
		t.Fatalf("read sentinel: %v", err)
	}
	var ev map[string]any
	if err := json.Unmarshal(captured, &ev); err != nil {
		t.Fatalf("event JSON malformed: %v", err)
	}
	if ev["event_type"] != "copilot_pr_activity" {
		t.Errorf("event_type=%v want copilot_pr_activity", ev["event_type"])
	}
	payload, _ := ev["payload"].(map[string]any)
	if payload["repo"] != "o/r" {
		t.Errorf("payload.repo=%v want o/r", payload["repo"])
	}
	if n, _ := payload["pr_number"].(float64); n != 200 {
		t.Errorf("payload.pr_number=%v want 200", payload["pr_number"])
	}
	if payload["event_type"] != "pull_request" {
		t.Errorf("payload.event_type=%v want pull_request", payload["event_type"])
	}
	if payload["event_action"] != "synchronize" {
		t.Errorf("payload.event_action=%v want synchronize", payload["event_action"])
	}
	if payload["delivery_id"] != "delivery-XYZ" {
		t.Errorf("payload.delivery_id=%v want delivery-XYZ", payload["delivery_id"])
	}
}

func TestHandlePR_SkipsActivityEmit_ForUnlabeledPR(t *testing.T) {
	// FR-013 only fires for PRs carrying chitin-dispatch. An unlabeled
	// PR should produce no chain event (sentinel file stays empty).
	secret := []byte("test-secret-for-pr-route!!!!")
	bin, sentinel := fakeKernelBin(t, 0)
	t.Setenv("CHITIN_KERNEL_BIN", bin)
	h := &factoryHandler{secret: secret, logFile: t.TempDir() + "/log.jsonl"}

	body := []byte(`{
		"action": "opened",
		"number": 300,
		"pull_request": {
			"html_url": "https://github.com/o/r/pull/300",
			"body": "Nothing to see",
			"labels": []
		},
		"repository": {"full_name": "o/r"}
	}`)

	req := signedPRRequest(t, secret, "pull_request", "d300", body)
	w := httptest.NewRecorder()
	h.handlePR(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d want 200", w.Code)
	}
	if _, err := os.Stat(sentinel); err == nil {
		t.Errorf("sentinel file exists; copilot_pr_activity should NOT fire for unlabeled PR")
	} else if !os.IsNotExist(err) {
		t.Errorf("sentinel stat: %v", err)
	}
}

func TestHandlePR_IssueComment_RoutesViaIssueLabels(t *testing.T) {
	// issue_comment events put labels on .issue, not .pull_request.
	// Eligibility logic should pick them up there.
	secret := []byte("test-secret-for-pr-route!!!!")
	bin, _ := fakeKernelBin(t, 0)
	t.Setenv("CHITIN_KERNEL_BIN", bin)
	h := &factoryHandler{secret: secret, logFile: t.TempDir() + "/log.jsonl"}

	body := []byte(`{
		"action": "created",
		"issue": {
			"number": 400,
			"body": "Closes #42",
			"labels": [{"name": "chitin-dispatch"}]
		},
		"repository": {"full_name": "o/r"}
	}`)

	req := signedPRRequest(t, secret, "issue_comment", "d400", body)
	w := httptest.NewRecorder()
	h.handlePR(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d want 200", w.Code)
	}
	var resp prResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if !resp.Eligible {
		t.Errorf("Eligible=false; want true (label on .issue)")
	}
	if resp.PRNumber != 400 {
		t.Errorf("PRNumber=%d want 400", resp.PRNumber)
	}
}
