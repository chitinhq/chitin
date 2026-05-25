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
)

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

// TestHandlePR_PullRequestReview_AllowlistedReviewer_DispatchesIteration
// covers spec 113 US1 happy path: a pull_request_review.submitted event
// on a chitin/wu/* branch from a copilot allowlist reviewer flips
// PRIterationEligible=true and populates the deterministic WorkflowID.
// The stub dispatcher currently surfaces a workflow_not_implemented
// failure (T004 lands the workflow); the test pins the WorkflowID +
// review-id wiring regardless.
func TestHandlePR_PullRequestReview_AllowlistedReviewer_DispatchesIteration(t *testing.T) {
	secret := []byte("test-secret-for-pr-route!!!!")
	bin, _ := fakeKernelBin(t, 0)
	t.Setenv("CHITIN_KERNEL_BIN", bin)
	h := &factoryHandler{secret: secret, logFile: t.TempDir() + "/log.jsonl"}

	body := []byte(`{
		"action": "submitted",
		"number": 800,
		"pull_request": {
			"html_url": "https://github.com/o/r/pull/800",
			"head": {"ref": "chitin/wu/spec-113-T001-aaa"},
			"base": {"ref": "main"},
			"labels": [{"name": "chitin-dispatch"}, {"name": "sched/run/run-it-1"}]
		},
		"review": {
			"id": 99001,
			"state": "changes_requested",
			"user": {"login": "copilot-pull-request-reviewer"}
		},
		"repository": {"full_name": "o/r"}
	}`)

	req := signedPRRequest(t, secret, "pull_request_review", "delivery-iter-1", body)
	w := httptest.NewRecorder()
	h.handlePR(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d want 200; body=%s", w.Code, w.Body.String())
	}
	var resp prResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	// FR-007 contract preserved: pull_request_review remains
	// event_type_ignored under the spec 099 eligibility surface.
	if resp.Eligible {
		t.Errorf("Eligible=true; want false (FR-007 contract: review events are event_type_ignored)")
	}
	if !resp.PRIterationEligible {
		t.Errorf("PRIterationEligible=false; want true for allowlisted reviewer on chitin/wu/* branch")
	}
	if resp.PRIterationReviewID != 99001 {
		t.Errorf("PRIterationReviewID=%d want 99001", resp.PRIterationReviewID)
	}
	if want := "iteration-pr-800-review-99001"; resp.PRIterationWorkflowID != want {
		t.Errorf("PRIterationWorkflowID=%q want %q", resp.PRIterationWorkflowID, want)
	}
}

// TestHandlePR_PullRequestReview_HumanReviewer_NotEligible covers spec
// 113 FR-009: a non-allowlisted reviewer marks PRIterationEligible=false
// with the canonical reason "human_reviewer_present" so the operator
// queue (spec 114) and US3 escalation path get a stable signal. No
// dispatch fires.
func TestHandlePR_PullRequestReview_HumanReviewer_NotEligible(t *testing.T) {
	secret := []byte("test-secret-for-pr-route!!!!")
	bin, _ := fakeKernelBin(t, 0)
	t.Setenv("CHITIN_KERNEL_BIN", bin)
	h := &factoryHandler{secret: secret, logFile: t.TempDir() + "/log.jsonl"}

	body := []byte(`{
		"action": "submitted",
		"number": 801,
		"pull_request": {
			"html_url": "https://github.com/o/r/pull/801",
			"head": {"ref": "chitin/wu/spec-113-T001-aaa"},
			"base": {"ref": "main"},
			"labels": [{"name": "chitin-dispatch"}]
		},
		"review": {
			"id": 99002,
			"state": "changes_requested",
			"user": {"login": "some-human-operator"}
		},
		"repository": {"full_name": "o/r"}
	}`)

	req := signedPRRequest(t, secret, "pull_request_review", "delivery-iter-2", body)
	w := httptest.NewRecorder()
	h.handlePR(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d want 200; body=%s", w.Code, w.Body.String())
	}
	var resp prResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.PRIterationEligible {
		t.Errorf("PRIterationEligible=true; want false (human reviewer)")
	}
	if resp.PRIterationSkippedReason != "human_reviewer_present" {
		t.Errorf("PRIterationSkippedReason=%q want human_reviewer_present", resp.PRIterationSkippedReason)
	}
	if resp.PRIterationDispatched || resp.PRIterationWorkflowID != "" {
		t.Errorf("non-allowlisted reviewer must not dispatch; got Dispatched=%v WorkflowID=%q",
			resp.PRIterationDispatched, resp.PRIterationWorkflowID)
	}
}

// TestHandlePR_PullRequestReview_NonChitinBranch_NotEligible covers spec
// 113 FR-001: only chitin/wu/* branches participate in the iteration
// loop, even when the reviewer is allowlisted.
func TestHandlePR_PullRequestReview_NonChitinBranch_NotEligible(t *testing.T) {
	secret := []byte("test-secret-for-pr-route!!!!")
	bin, _ := fakeKernelBin(t, 0)
	t.Setenv("CHITIN_KERNEL_BIN", bin)
	h := &factoryHandler{secret: secret, logFile: t.TempDir() + "/log.jsonl"}

	body := []byte(`{
		"action": "submitted",
		"number": 802,
		"pull_request": {
			"head": {"ref": "feat/operator-branch"},
			"base": {"ref": "main"},
			"labels": []
		},
		"review": {
			"id": 99003,
			"state": "commented",
			"user": {"login": "copilot-pull-request-reviewer"}
		},
		"repository": {"full_name": "o/r"}
	}`)

	req := signedPRRequest(t, secret, "pull_request_review", "delivery-iter-3", body)
	w := httptest.NewRecorder()
	h.handlePR(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d want 200; body=%s", w.Code, w.Body.String())
	}
	var resp prResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.PRIterationEligible {
		t.Errorf("PRIterationEligible=true; want false (non-chitin/wu branch)")
	}
	if resp.PRIterationSkippedReason != "non_chitin_branch" {
		t.Errorf("PRIterationSkippedReason=%q want non_chitin_branch", resp.PRIterationSkippedReason)
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
