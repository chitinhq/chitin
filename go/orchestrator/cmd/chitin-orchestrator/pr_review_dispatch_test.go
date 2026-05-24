// pr_review_dispatch_test.go — spec 099 slice 4 tests for dedup +
// PRReviewWorkflow dispatch from the /webhook/pr route. Tests drive
// dispatchPRReview directly with an injected temporalDialer + stub
// workflowStarter; the HTTP-route end-to-end is covered by
// factory_listen_pr_test.go.

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"go.temporal.io/sdk/client"
)

// stubStarter is a minimal workflowStarter for tests. ExecuteWorkflowFn
// is invoked if set; if nil, ExecuteWorkflow returns (nil, nil) — the
// happy path. Set CloseCalled to assert Close() is invoked.
type stubStarter struct {
	ExecuteWorkflowFn func(ctx context.Context, options client.StartWorkflowOptions, workflow any, args ...any) (client.WorkflowRun, error)
	CloseCalled       bool
}

func (s *stubStarter) ExecuteWorkflow(ctx context.Context, options client.StartWorkflowOptions, workflow any, args ...any) (client.WorkflowRun, error) {
	if s.ExecuteWorkflowFn != nil {
		return s.ExecuteWorkflowFn(ctx, options, workflow, args...)
	}
	return nil, nil
}

func (s *stubStarter) Close() { s.CloseCalled = true }

// installChainFixture writes a one-event jsonl file simulating a prior
// copilot_pr_detected for (repo, pr). Returns the chain dir path so
// callers can point CHITIN_DIR at it.
func installChainFixture(t *testing.T, repo string, pr int) string {
	t.Helper()
	dir := t.TempDir()
	ev := map[string]any{
		"event_type": "copilot_pr_detected",
		"payload": map[string]any{
			"repo":      repo,
			"pr_number": pr,
		},
	}
	body, _ := json.Marshal(ev)
	path := filepath.Join(dir, "events-fixture.jsonl")
	if err := os.WriteFile(path, append(body, '\n'), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return dir
}

func TestDispatchPRReview_DedupSkipsWhenPriorDetectionExists(t *testing.T) {
	dir := installChainFixture(t, "owner/name", 42)
	t.Setenv("CHITIN_DIR", dir)
	bin, _ := fakeKernelBin(t, 0)
	t.Setenv("CHITIN_KERNEL_BIN", bin)

	dialer := func(ctx context.Context, host string) (workflowStarter, string, error) {
		t.Errorf("dialer should NOT be called when deduped")
		return &stubStarter{}, host, nil
	}
	var stderr bytes.Buffer
	out := dispatchPRReview(context.Background(), prDispatchInput{
		Repo:     "owner/name",
		PRNumber: 42,
	}, dialer, &stderr)

	if !out.DedupSkipped {
		t.Errorf("DedupSkipped=false; want true (chain has prior detection)")
	}
	if out.ReviewStarted {
		t.Errorf("ReviewStarted=true; want false (deduped)")
	}
}

func TestDispatchPRReview_StartsWorkflowWhenNoPriorDetection(t *testing.T) {
	dir := t.TempDir() // empty — no fixture, no prior events
	t.Setenv("CHITIN_DIR", dir)
	bin, sentinel := fakeKernelBin(t, 0)
	t.Setenv("CHITIN_KERNEL_BIN", bin)

	starter := &stubStarter{}
	dialer := func(ctx context.Context, host string) (workflowStarter, string, error) {
		return starter, host, nil
	}

	var stderr bytes.Buffer
	out := dispatchPRReview(context.Background(), prDispatchInput{
		Repo:        "owner/name",
		PRNumber:    100,
		PRURL:       "https://github.com/owner/name/pull/100",
		SpecRef:     "099-github-native-dispatch",
		IssueNumber: 42,
		Commits:     3,
	}, dialer, &stderr)

	if out.DedupSkipped {
		t.Errorf("DedupSkipped=true; want false")
	}
	if !out.ReviewStarted {
		t.Errorf("ReviewStarted=false; want true (no prior detection); stderr=%q", stderr.String())
	}
	if out.ReviewRunID == "" {
		t.Errorf("ReviewRunID empty; want UUID")
	}
	if !starter.CloseCalled {
		t.Errorf("starter.Close() not called; want defer Close on successful dispatch")
	}

	// Verify the emitted copilot_pr_detected event landed.
	captured, err := os.ReadFile(sentinel)
	if err != nil {
		t.Fatalf("read sentinel: %v", err)
	}
	var ev map[string]any
	if err := json.Unmarshal(captured, &ev); err != nil {
		t.Fatalf("event JSON malformed: %v", err)
	}
	if ev["event_type"] != "copilot_pr_detected" {
		t.Errorf("event_type=%v; want copilot_pr_detected", ev["event_type"])
	}
	payload, _ := ev["payload"].(map[string]any)
	if payload["repo"] != "owner/name" {
		t.Errorf("payload.repo=%v; want owner/name", payload["repo"])
	}
	if n, _ := payload["pr_number"].(float64); n != 100 {
		t.Errorf("payload.pr_number=%v; want 100", payload["pr_number"])
	}
	if payload["spec_ref"] != "099-github-native-dispatch" {
		t.Errorf("payload.spec_ref=%v; want 099-github-native-dispatch", payload["spec_ref"])
	}
	if payload["review_workflow_run_id"] != out.ReviewRunID {
		t.Errorf("payload.review_workflow_run_id=%v; want %s", payload["review_workflow_run_id"], out.ReviewRunID)
	}
}

func TestDispatchPRReview_TemporalUnreachable_EmitsReviewFailed(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CHITIN_DIR", dir)
	bin, sentinel := fakeKernelBin(t, 0)
	t.Setenv("CHITIN_KERNEL_BIN", bin)

	failingDialer := func(ctx context.Context, host string) (workflowStarter, string, error) {
		return nil, host, errors.New("dial tcp 127.0.0.1:1: connection refused")
	}

	var stderr bytes.Buffer
	out := dispatchPRReview(context.Background(), prDispatchInput{
		Repo:     "owner/name",
		PRNumber: 200,
	}, failingDialer, &stderr)

	if out.ReviewStarted {
		t.Errorf("ReviewStarted=true; want false (Temporal unreachable)")
	}
	if out.FailureKind != "temporal_unreachable" {
		t.Errorf("FailureKind=%q; want temporal_unreachable", out.FailureKind)
	}

	captured, err := os.ReadFile(sentinel)
	if err != nil {
		t.Fatalf("read sentinel: %v", err)
	}
	var ev map[string]any
	_ = json.Unmarshal(captured, &ev)
	if ev["event_type"] != "copilot_review_failed" {
		t.Errorf("event_type=%v; want copilot_review_failed", ev["event_type"])
	}
	payload, _ := ev["payload"].(map[string]any)
	if payload["failure_kind"] != "temporal_unreachable" {
		t.Errorf("payload.failure_kind=%v; want temporal_unreachable", payload["failure_kind"])
	}
}

func TestDispatchPRReview_ExecuteWorkflowError_EmitsReviewFailedWithDispatchKind(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CHITIN_DIR", dir)
	bin, sentinel := fakeKernelBin(t, 0)
	t.Setenv("CHITIN_KERNEL_BIN", bin)

	starter := &stubStarter{
		ExecuteWorkflowFn: func(ctx context.Context, options client.StartWorkflowOptions, workflow any, args ...any) (client.WorkflowRun, error) {
			return nil, errors.New("workflow already started")
		},
	}
	dialer := func(ctx context.Context, host string) (workflowStarter, string, error) {
		return starter, host, nil
	}

	var stderr bytes.Buffer
	out := dispatchPRReview(context.Background(), prDispatchInput{
		Repo:     "owner/name",
		PRNumber: 300,
	}, dialer, &stderr)

	if out.FailureKind != "dispatch_error" {
		t.Errorf("FailureKind=%q; want dispatch_error", out.FailureKind)
	}
	if out.ReviewRunID == "" {
		t.Errorf("ReviewRunID empty; want UUID even on dispatch failure")
	}

	captured, _ := os.ReadFile(sentinel)
	var ev map[string]any
	_ = json.Unmarshal(captured, &ev)
	if ev["event_type"] != "copilot_review_failed" {
		t.Errorf("event_type=%v; want copilot_review_failed", ev["event_type"])
	}
	payload, _ := ev["payload"].(map[string]any)
	if payload["failure_kind"] != "dispatch_error" {
		t.Errorf("payload.failure_kind=%v; want dispatch_error", payload["failure_kind"])
	}
	if payload["review_run_id"] != out.ReviewRunID {
		t.Errorf("payload.review_run_id=%v; want %s", payload["review_run_id"], out.ReviewRunID)
	}
}

func TestHasPriorPRDetection_NoChainDir_ReturnsFalse(t *testing.T) {
	out, err := hasPriorPRDetection(t.TempDir()+"/nonexistent", "owner/name", 42)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if out {
		t.Errorf("expected false for empty chain dir")
	}
}

func TestHasPriorPRDetection_MatchesOnlyExactRepoAndPR(t *testing.T) {
	dir := t.TempDir()
	ev1 := `{"event_type":"copilot_pr_detected","payload":{"repo":"owner/a","pr_number":1}}` + "\n"
	ev2 := `{"event_type":"copilot_pr_detected","payload":{"repo":"owner/b","pr_number":2}}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, "events-x.jsonl"), []byte(ev1+ev2), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	found, err := hasPriorPRDetection(dir, "owner/c", 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found {
		t.Errorf("found=true; expected false (no match for owner/c#3)")
	}
	found, _ = hasPriorPRDetection(dir, "owner/a", 1)
	if !found {
		t.Errorf("expected exact-match hit for owner/a#1")
	}
}

func TestHasPriorPRDetection_IgnoresOtherEventTypes(t *testing.T) {
	dir := t.TempDir()
	ev := `{"event_type":"copilot_pr_activity","payload":{"repo":"owner/a","pr_number":1}}` + "\n"
	_ = os.WriteFile(filepath.Join(dir, "events-y.jsonl"), []byte(ev), 0o644)

	found, _ := hasPriorPRDetection(dir, "owner/a", 1)
	if found {
		t.Errorf("found=true; expected false (event_type mismatch)")
	}
}
