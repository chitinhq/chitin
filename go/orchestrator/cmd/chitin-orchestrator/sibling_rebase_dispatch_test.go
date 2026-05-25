// sibling_rebase_dispatch_test.go — spec 112 US2 tests for the
// /webhook/pr merged-PR sibling-rebase trigger. Covers the dispatch
// orchestration directly (dispatchSiblingRebase) and the HTTP route
// end-to-end (handlePR with injected lister + dialer).

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"sync"
	"testing"

	"go.temporal.io/api/serviceerror"
	"go.temporal.io/sdk/client"
)

// TestDispatchSiblingRebase_FiresOneWorkflowPerSibling asserts the happy
// path: lister returns 3 siblings → 3 ExecuteWorkflow calls, each with the
// deterministic WorkflowID format. The dispatcher dedups via Temporal's
// REJECT_DUPLICATE policy at the server side; this test only proves the
// per-sibling fan-out.
func TestDispatchSiblingRebase_FiresOneWorkflowPerSibling(t *testing.T) {
	starter := &recordingStarter{}
	dialer := func(_ context.Context, host string) (workflowStarter, string, error) {
		return starter, host, nil
	}
	lister := func(_ context.Context, _, _ string, exclude int) ([]siblingListEntry, error) {
		return []siblingListEntry{
			{Number: 200, HeadRefName: "chitin/wu/spec-110-T002-x"},
			{Number: 201, HeadRefName: "chitin/wu/spec-110-T003-y"},
			{Number: 202, HeadRefName: "chitin/wu/spec-110-T005-z"},
		}, nil
	}

	res := dispatchSiblingRebase(context.Background(), siblingRebaseDispatchInput{
		Repo:           "chitinhq/chitin",
		SourcePRNumber: 199,
		SchedulerRunID: "run-abc",
		BaseBranch:     "main",
		TargetRepo:     "/path/to/repo",
		TemporalHost:   "localhost:7233",
	}, lister, dialer, nil)

	if res.Siblings != 3 {
		t.Fatalf("expected Siblings=3, got %d", res.Siblings)
	}
	if res.Dispatched != 3 {
		t.Fatalf("expected Dispatched=3, got %d (failure=%s detail=%s)",
			res.Dispatched, res.FailureKind, res.Detail)
	}
	if want := []int{200, 201, 202}; !equalInts(res.PRNumbers, want) {
		t.Fatalf("expected PRNumbers=%v, got %v", want, res.PRNumbers)
	}
	if len(starter.calls) != 3 {
		t.Fatalf("expected 3 ExecuteWorkflow calls, got %d", len(starter.calls))
	}
	if !starter.closed {
		t.Fatal("expected starter.Close() to be called")
	}

	// Deterministic IDs — one per (sibling PR, source PR).
	wantIDs := []string{
		"sibling-rebase-pr-200-from-199",
		"sibling-rebase-pr-201-from-199",
		"sibling-rebase-pr-202-from-199",
	}
	for i, want := range wantIDs {
		if starter.calls[i].opts.ID != want {
			t.Errorf("call %d: want WorkflowID %q, got %q", i, want, starter.calls[i].opts.ID)
		}
	}
}

// TestDispatchSiblingRebase_AlreadyStartedCountsAsDispatched asserts the
// dedup-success path: a redelivered webhook hits Temporal's
// WorkflowExecutionAlreadyStarted; the dispatcher counts it as dispatched
// (not as a failure) so GitHub redeliveries don't surface as faults.
func TestDispatchSiblingRebase_AlreadyStartedCountsAsDispatched(t *testing.T) {
	alreadyStarted := &serviceerror.WorkflowExecutionAlreadyStarted{
		Message: "workflow already started",
	}
	starter := &recordingStarter{returnErr: alreadyStarted}
	dialer := func(_ context.Context, host string) (workflowStarter, string, error) {
		return starter, host, nil
	}
	lister := func(_ context.Context, _, _ string, _ int) ([]siblingListEntry, error) {
		return []siblingListEntry{{Number: 300, HeadRefName: "feat/x"}}, nil
	}

	res := dispatchSiblingRebase(context.Background(), siblingRebaseDispatchInput{
		Repo:           "chitinhq/chitin",
		SourcePRNumber: 299,
		SchedulerRunID: "run-redelivery",
	}, lister, dialer, nil)

	if res.Dispatched != 1 {
		t.Fatalf("AlreadyStarted should count as dispatched, got Dispatched=%d failure=%s",
			res.Dispatched, res.FailureKind)
	}
	if res.FailureKind != "" {
		t.Fatalf("expected no failure on AlreadyStarted, got %s: %s", res.FailureKind, res.Detail)
	}
}

// TestDispatchSiblingRebase_NoSchedulerRunIDIsNoop asserts the guard: the
// dispatcher does nothing (no list call, no temporal dial) when called
// without a run id. handlePR is supposed to filter for this, but the
// guard is the safety net.
func TestDispatchSiblingRebase_NoSchedulerRunIDIsNoop(t *testing.T) {
	called := false
	lister := func(_ context.Context, _, _ string, _ int) ([]siblingListEntry, error) {
		called = true
		return nil, nil
	}
	res := dispatchSiblingRebase(context.Background(), siblingRebaseDispatchInput{
		Repo:           "chitinhq/chitin",
		SourcePRNumber: 1,
		SchedulerRunID: "",
	}, lister, nil, nil)
	if called {
		t.Fatal("lister must not be called when SchedulerRunID is empty")
	}
	if res.Siblings != 0 || res.Dispatched != 0 {
		t.Fatalf("expected zero result, got %+v", res)
	}
}

// TestDispatchSiblingRebase_NilStderrDoesNotPanic asserts the nil-writer
// guard: warnDispatch tolerates a nil stderr (test/direct callers may
// pass nil) instead of panicking on fmt.Fprintf.
func TestDispatchSiblingRebase_NilStderrDoesNotPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("unexpected panic with nil stderr: %v", r)
		}
	}()
	lister := func(_ context.Context, _, _ string, _ int) ([]siblingListEntry, error) {
		return nil, errors.New("simulated list failure")
	}
	res := dispatchSiblingRebase(context.Background(), siblingRebaseDispatchInput{
		Repo:           "chitinhq/chitin",
		SourcePRNumber: 1,
		SchedulerRunID: "run-x",
	}, lister, nil, nil)
	if res.FailureKind != "sibling_list_failed" {
		t.Fatalf("expected sibling_list_failed, got %q", res.FailureKind)
	}
}

// TestHandlePR_MergedSiblingTriggersRebaseDispatch asserts the HTTP route
// end-to-end: a pull_request.closed event with merged=true and a
// sched/run/<id> label results in dispatchSiblingRebase being called
// against injected lister + dialer, and the response body populates
// SiblingRebase* fields. No external binaries or services involved.
func TestHandlePR_MergedSiblingTriggersRebaseDispatch(t *testing.T) {
	secret := []byte("test-secret-for-pr-route!!!!")
	starter := &recordingStarter{}
	dialer := func(_ context.Context, host string) (workflowStarter, string, error) {
		return starter, host, nil
	}
	lister := func(_ context.Context, _, label string, exclude int) ([]siblingListEntry, error) {
		if label != "sched/run/run-merge-test" {
			t.Errorf("lister called with unexpected label %q", label)
		}
		if exclude != 500 {
			t.Errorf("lister called with unexpected exclude %d", exclude)
		}
		return []siblingListEntry{
			{Number: 501, HeadRefName: "chitin/wu/spec-x-T001-aaa"},
			{Number: 502, HeadRefName: "chitin/wu/spec-x-T002-bbb"},
		}, nil
	}

	h := &factoryHandler{
		secret:         secret,
		logFile:        t.TempDir() + "/log.jsonl",
		siblingLister:  lister,
		temporalDialer: dialer,
	}

	body := mergedPRWebhookBody(t, 500, "sched/run/run-merge-test")
	req := signedPRRequest(t, secret, "pull_request", "delivery-merge-1", body)
	w := httptest.NewRecorder()
	h.handlePR(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	var resp prResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.SiblingRebaseSiblings != 2 {
		t.Errorf("expected SiblingRebaseSiblings=2, got %d", resp.SiblingRebaseSiblings)
	}
	if resp.SiblingRebaseDispatched != 2 {
		t.Errorf("expected SiblingRebaseDispatched=2, got %d", resp.SiblingRebaseDispatched)
	}
	if want := []int{501, 502}; !equalInts(resp.SiblingRebasePRs, want) {
		t.Errorf("expected SiblingRebasePRs=%v, got %v", want, resp.SiblingRebasePRs)
	}
	if len(starter.calls) != 2 {
		t.Errorf("expected 2 workflow dispatches, got %d", len(starter.calls))
	}
}

// TestHandlePR_MergedWithoutSchedLabelSkipsRebase asserts the
// label-required path: a merged chitin-dispatch PR (Copilot lane) WITHOUT a
// sched/run/<id> label does not trigger sibling rebase.
func TestHandlePR_MergedWithoutSchedLabelSkipsRebase(t *testing.T) {
	secret := []byte("test-secret-for-pr-route!!!!")
	called := false
	lister := func(_ context.Context, _, _ string, _ int) ([]siblingListEntry, error) {
		called = true
		return nil, nil
	}
	h := &factoryHandler{
		secret:        secret,
		logFile:       t.TempDir() + "/log.jsonl",
		siblingLister: lister,
	}

	// Body carries chitin-dispatch but NOT sched/run/* — should not trigger.
	body := mergedPRWebhookBody(t, 600, "chitin-dispatch")
	req := signedPRRequest(t, secret, "pull_request", "delivery-merge-2", body)
	w := httptest.NewRecorder()
	h.handlePR(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if called {
		t.Fatal("sibling lister must not be called without a sched/run/* label")
	}
}

// TestHandlePR_ClosedNotMergedSkipsRebase asserts the merged-required path:
// a pull_request.closed event whose PR was NOT merged (closed without merge)
// does not trigger sibling rebase even if it carries a sched/run/<id>
// label.
func TestHandlePR_ClosedNotMergedSkipsRebase(t *testing.T) {
	secret := []byte("test-secret-for-pr-route!!!!")
	called := false
	lister := func(_ context.Context, _, _ string, _ int) ([]siblingListEntry, error) {
		called = true
		return nil, nil
	}
	h := &factoryHandler{
		secret:        secret,
		logFile:       t.TempDir() + "/log.jsonl",
		siblingLister: lister,
	}

	body := closedNotMergedPRWebhookBody(t, 700, "sched/run/run-closed")
	req := signedPRRequest(t, secret, "pull_request", "delivery-closed-1", body)
	w := httptest.NewRecorder()
	h.handlePR(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if called {
		t.Fatal("sibling lister must not be called on a non-merged close")
	}
}

// --- helpers ---------------------------------------------------------------

// recordingStarter is a workflowStarter that records every ExecuteWorkflow
// call. When returnErr is set, every call returns (nil, returnErr).
type recordingStarter struct {
	mu        sync.Mutex
	calls     []recordedCall
	closed    bool
	returnErr error
}

type recordedCall struct {
	opts     client.StartWorkflowOptions
	workflow any
	args     []any
}

func (s *recordingStarter) ExecuteWorkflow(_ context.Context, opts client.StartWorkflowOptions, wf any, args ...any) (client.WorkflowRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, recordedCall{opts: opts, workflow: wf, args: args})
	if s.returnErr != nil {
		return nil, s.returnErr
	}
	return nil, nil
}

func (s *recordingStarter) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
}

// mergedPRWebhookBody builds a pull_request.closed payload with merged=true
// and a single label, encoded JSON ready for signing.
func mergedPRWebhookBody(t *testing.T, prNumber int, labelName string) []byte {
	t.Helper()
	payload := map[string]any{
		"action": "closed",
		"number": prNumber,
		"pull_request": map[string]any{
			"html_url": fmt.Sprintf("https://github.com/chitinhq/chitin/pull/%d", prNumber),
			"merged":   true,
			"base":     map[string]any{"ref": "main"},
			"head":     map[string]any{"ref": "any-branch", "sha": "deadbeef"},
			"labels":   []map[string]any{{"name": labelName}},
		},
		"repository": map[string]any{"full_name": "chitinhq/chitin"},
	}
	b, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return b
}

// closedNotMergedPRWebhookBody builds a pull_request.closed payload with
// merged=false — closed without a merge.
func closedNotMergedPRWebhookBody(t *testing.T, prNumber int, labelName string) []byte {
	t.Helper()
	payload := map[string]any{
		"action": "closed",
		"number": prNumber,
		"pull_request": map[string]any{
			"html_url": fmt.Sprintf("https://github.com/chitinhq/chitin/pull/%d", prNumber),
			"merged":   false,
			"base":     map[string]any{"ref": "main"},
			"head":     map[string]any{"ref": "any-branch", "sha": "deadbeef"},
			"labels":   []map[string]any{{"name": labelName}},
		},
		"repository": map[string]any{"full_name": "chitinhq/chitin"},
	}
	b, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return b
}

// equalInts returns true iff a and b contain the same elements in the same
// order. Used for deterministic PRNumbers assertions.
func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// silenceBuf is a write-only io.Writer that drops everything — used to
// silence warnDispatch output when the test doesn't assert on it.
type silenceBuf struct{}

func (silenceBuf) Write(p []byte) (int, error) { return len(p), nil }

// (unused) keeps the import surface stable if a future test wants to assert
// on dispatch warnings.
var _ = bytes.NewBuffer
