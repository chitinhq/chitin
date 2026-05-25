// pr_iteration_dispatch_test.go — spec 113 US1 tests for the
// /webhook/pr pull_request_review trigger + dispatcher.

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"testing"

	"go.temporal.io/api/serviceerror"
)

// TestDispatchPRIteration_HappyPath asserts the basic dispatch flow:
// given a valid input, fires PRIterationWorkflow with the deterministic
// WorkflowID `iteration-pr-<N>-review-<M>` and returns Dispatched=true.
func TestDispatchPRIteration_HappyPath(t *testing.T) {
	starter := &recordingStarter{}
	dialer := func(_ context.Context, host string) (workflowStarter, string, error) {
		return starter, host, nil
	}

	res := dispatchPRIteration(context.Background(), prIterationDispatchInput{
		Repo:         "chitinhq/chitin",
		PRNumber:     1234,
		PRBranch:     "chitin/wu/113-pr-comment-respond-loop-t005-abc",
		ReviewID:     999888,
		DriverID:     "claudecode",
		TargetRepo:   "/path/to/repo",
		TemporalHost: "localhost:7233",
	}, dialer, nil)

	if !res.Dispatched {
		t.Fatalf("expected Dispatched=true, got %+v", res)
	}
	if res.WorkflowID != "iteration-pr-1234-review-999888" {
		t.Errorf("WorkflowID = %q, want %q", res.WorkflowID, "iteration-pr-1234-review-999888")
	}
	if len(starter.calls) != 1 {
		t.Fatalf("expected 1 ExecuteWorkflow call, got %d", len(starter.calls))
	}
	if starter.calls[0].opts.ID != "iteration-pr-1234-review-999888" {
		t.Errorf("start opts ID = %q", starter.calls[0].opts.ID)
	}
	if !starter.closed {
		t.Error("expected client.Close() to be called")
	}
}

// TestDispatchPRIteration_AlreadyStartedCountsAsDispatched asserts the
// dedup-success path: a redelivered webhook for the same (PR, review)
// hits WorkflowExecutionAlreadyStarted and counts as Dispatched.
func TestDispatchPRIteration_AlreadyStartedCountsAsDispatched(t *testing.T) {
	starter := &recordingStarter{
		returnErr: &serviceerror.WorkflowExecutionAlreadyStarted{
			Message: "already started",
		},
	}
	dialer := func(_ context.Context, host string) (workflowStarter, string, error) {
		return starter, host, nil
	}

	res := dispatchPRIteration(context.Background(), prIterationDispatchInput{
		Repo:       "chitinhq/chitin",
		PRNumber:   2000,
		PRBranch:   "chitin/wu/some-spec-t001-xyz",
		ReviewID:   42,
		DriverID:   "claudecode",
		TargetRepo: "/tmp/repo",
	}, dialer, nil)

	if !res.Dispatched {
		t.Fatalf("AlreadyStarted should count as Dispatched, got %+v", res)
	}
	if res.FailureKind != "" {
		t.Errorf("expected no FailureKind on AlreadyStarted, got %q", res.FailureKind)
	}
}

// TestDispatchPRIteration_NonFactoryBranchRejected asserts the eligibility
// guard: a branch that doesn't match chitin/wu/* is rejected even if the
// caller misroutes a non-factory PR here.
func TestDispatchPRIteration_NonFactoryBranchRejected(t *testing.T) {
	called := false
	dialer := func(_ context.Context, host string) (workflowStarter, string, error) {
		called = true
		return nil, host, nil
	}

	res := dispatchPRIteration(context.Background(), prIterationDispatchInput{
		Repo:     "chitinhq/chitin",
		PRNumber: 3000,
		PRBranch: "feat/some-human-authored-branch",
		ReviewID: 1,
		DriverID: "claudecode",
	}, dialer, nil)

	if called {
		t.Error("dialer should NOT be called for a non-factory branch")
	}
	if res.Dispatched {
		t.Errorf("expected Dispatched=false for non-factory branch, got %+v", res)
	}
	if res.FailureKind != "non_factory_branch" {
		t.Errorf("FailureKind = %q, want non_factory_branch", res.FailureKind)
	}
}

// TestDispatchPRIteration_InvalidInputRejected asserts the input guard.
func TestDispatchPRIteration_InvalidInputRejected(t *testing.T) {
	res := dispatchPRIteration(context.Background(), prIterationDispatchInput{
		Repo:     "chitinhq/chitin",
		PRNumber: 0, // invalid
		PRBranch: "chitin/wu/x",
		ReviewID: 1,
	}, nil, nil)
	if res.Dispatched || res.FailureKind != "invalid_input" {
		t.Errorf("expected invalid_input failure, got %+v", res)
	}
}

// TestDispatchPRIteration_NilStderrDoesNotPanic asserts the nil-writer
// guard — warnIterationDispatch tolerates a nil io.Writer just like spec
// 112 US2's warnDispatch.
func TestDispatchPRIteration_NilStderrDoesNotPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("unexpected panic with nil stderr: %v", r)
		}
	}()
	failingDialer := func(_ context.Context, host string) (workflowStarter, string, error) {
		return nil, host, errors.New("simulated dial failure")
	}
	res := dispatchPRIteration(context.Background(), prIterationDispatchInput{
		Repo:     "chitinhq/chitin",
		PRNumber: 1,
		PRBranch: "chitin/wu/x-t001-y",
		ReviewID: 1,
		DriverID: "claudecode",
	}, failingDialer, nil)
	if res.FailureKind != "temporal_unreachable" {
		t.Errorf("expected temporal_unreachable failure, got %q", res.FailureKind)
	}
}

// TestIsCopilotReviewer asserts the allowlist's case-insensitive match +
// [bot] suffix handling.
func TestIsCopilotReviewer(t *testing.T) {
	cases := map[string]bool{
		"copilot-pull-request-reviewer": true,
		"Copilot-Pull-Request-Reviewer": true,
		"copilot":                       true,
		"Copilot":                       true,
		"github-copilot[bot]":           true,
		"some-other-bot":                false,
		"jared":                         false,
		"":                              false,
	}
	for login, want := range cases {
		if got := isCopilotReviewer(login); got != want {
			t.Errorf("isCopilotReviewer(%q) = %v, want %v", login, got, want)
		}
	}
}

// TestHandlePR_CopilotReviewTriggersIteration asserts the HTTP-route
// end-to-end: a pull_request_review.submitted event on a chitin/wu/*
// branch with a Copilot reviewer fires PRIterationWorkflow via the
// injected dialer.
func TestHandlePR_CopilotReviewTriggersIteration(t *testing.T) {
	secret := []byte("test-secret-for-pr-route!!!!")
	starter := &recordingStarter{}
	dialer := func(_ context.Context, host string) (workflowStarter, string, error) {
		return starter, host, nil
	}

	h := &factoryHandler{
		secret:         secret,
		logFile:        t.TempDir() + "/log.jsonl",
		temporalDialer: dialer,
	}

	body := reviewSubmittedPRWebhookBody(t, 500, "chitin/wu/113-x-t001-y",
		"copilot-pull-request-reviewer", "commented", 9999)
	req := signedPRRequest(t, secret, "pull_request_review", "delivery-review-1", body)
	w := httptest.NewRecorder()
	h.handlePR(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	var resp prResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !resp.PRIterationDispatched {
		t.Errorf("expected PRIterationDispatched=true, got %+v", resp)
	}
	if resp.PRIterationWorkflowID != "iteration-pr-500-review-9999" {
		t.Errorf("WorkflowID = %q", resp.PRIterationWorkflowID)
	}
	if len(starter.calls) != 1 {
		t.Errorf("expected 1 workflow start, got %d", len(starter.calls))
	}
}

// TestHandlePR_HumanReviewSkipsIteration asserts that a non-Copilot
// reviewer on a chitin/wu/* PR does NOT trigger the iteration loop. (Spec
// 113 US3 will add explicit human-escalation; v1 just no-ops.)
func TestHandlePR_HumanReviewSkipsIteration(t *testing.T) {
	secret := []byte("test-secret-for-pr-route!!!!")
	called := false
	dialer := func(_ context.Context, host string) (workflowStarter, string, error) {
		called = true
		return &recordingStarter{}, host, nil
	}

	h := &factoryHandler{
		secret:         secret,
		logFile:        t.TempDir() + "/log.jsonl",
		temporalDialer: dialer,
	}

	body := reviewSubmittedPRWebhookBody(t, 501, "chitin/wu/113-x-t001-y",
		"jared", "commented", 9999)
	req := signedPRRequest(t, secret, "pull_request_review", "delivery-review-2", body)
	w := httptest.NewRecorder()
	h.handlePR(w, req)

	if called {
		t.Error("temporal dialer should NOT fire for a human reviewer")
	}
}

// TestHandlePR_NonFactoryBranchSkipsIteration asserts that a Copilot
// review on a NON-factory branch (e.g. a human's feature branch) does
// NOT trigger the iteration loop.
func TestHandlePR_NonFactoryBranchSkipsIteration(t *testing.T) {
	secret := []byte("test-secret-for-pr-route!!!!")
	called := false
	dialer := func(_ context.Context, host string) (workflowStarter, string, error) {
		called = true
		return &recordingStarter{}, host, nil
	}

	h := &factoryHandler{
		secret:         secret,
		logFile:        t.TempDir() + "/log.jsonl",
		temporalDialer: dialer,
	}

	body := reviewSubmittedPRWebhookBody(t, 502, "feat/my-human-feature",
		"copilot-pull-request-reviewer", "commented", 9999)
	req := signedPRRequest(t, secret, "pull_request_review", "delivery-review-3", body)
	w := httptest.NewRecorder()
	h.handlePR(w, req)

	if called {
		t.Error("temporal dialer should NOT fire for a non-factory branch")
	}
}

// --- helpers ---------------------------------------------------------------

// reviewSubmittedPRWebhookBody builds a pull_request_review.submitted
// payload with the given branch / reviewer / state / review id.
func reviewSubmittedPRWebhookBody(t *testing.T, prNumber int, headRef, reviewerLogin, reviewState string, reviewID int64) []byte {
	t.Helper()
	payload := map[string]any{
		"action": "submitted",
		"number": prNumber,
		"pull_request": map[string]any{
			"html_url": fmt.Sprintf("https://github.com/chitinhq/chitin/pull/%d", prNumber),
			"base":     map[string]any{"ref": "main"},
			"head":     map[string]any{"ref": headRef, "sha": "deadbeef"},
			"labels":   []map[string]any{},
		},
		"review": map[string]any{
			"id":    reviewID,
			"state": reviewState,
			"body":  "Looks good with a few comments",
			"user":  map[string]any{"login": reviewerLogin},
		},
		"repository": map[string]any{"full_name": "chitinhq/chitin"},
	}
	b, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return b
}
