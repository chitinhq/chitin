package workflows

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"

	"github.com/chitinhq/chitin/go/orchestrator/activities"
	"github.com/chitinhq/chitin/go/orchestrator/driver"
)

// SpecIterationWorkflow hermetic tests (spec 115 T021). Each test mocks
// every side effect the workflow drives — driver selection, the spec-tuned
// iteration activity, and per-event chain emission — so the workflow's
// branching is a pure function of the activity outcomes returned by the
// mocks.
//
// The testsuite environment is the natural fit: the workflow's interleave
// of select → started → iterate → completed / failed / skipped is
// observable via the recorded telemetry calls and the workflow's return
// value, with no need to stand up a real driver registry, a real Temporal
// server, or a real `chitin-kernel` binary.

// specIterationActivityOpts mirrors activityOpts in scheduler_test.go and
// reviewActivityOpts in pr_review_test.go: an activity is registered under
// a stable string name so the workflow's ExecuteActivity(name, ...) call
// binds correctly in the testsuite env.
//
// (Kept distinct from activityOpts so two test files don't collide on the
// helper name during go test ./...; the underlying option shape is the same.)

// stubIterationDriver is the canonical happy-path driver outcome the test
// fixture returns from IterateSpecReview. The contents model spec 115
// FR-006's two driver disposition modes — Fix (driver edited the spec) and
// LintFix (driver patched the linter's allowlist instead of editing the
// spec) — plus a Reply for a comment the driver declined to act on. Skip
// stays 0 in the happy path; the dispatch-skip path is covered separately.
//
// FixupSHA is a stable 40-char hex so the workflow result's FixupSHA
// assertion does not depend on time or randomness.
var stubIterationDriver = iterateSpecReviewResult{
	PushedFixup:  true,
	FixupSHA:     "abc1234567890def1234567890abcdef12345678",
	CommentCount: 4,
	ActionCounts: SpecIterationActionCounts{
		Fix:     1, // one comment fixed by editing the spec
		Reply:   1, // one comment replied to without a code change
		Skip:    0, // none intentionally skipped
		LintFix: 2, // two lint violations resolved via allowlist patch
	},
	Explanation: "round 1: 1 fix + 1 reply + 2 allowlist patches, push abc12345",
}

// specIterationMocks captures every activity surface the workflow drives so
// each test can opt into a specific outcome per surface and assert on the
// recorded telemetry trace after the workflow settles.
type specIterationMocks struct {
	// selectResult is what the mock SelectDriver activity returns.
	selectResult activities.SelectDriverResult
	// selectErr is the error the mock SelectDriver activity returns. The
	// workflow's selection retry policy is MaximumAttempts=3, so a transient
	// error wired here will be retried; a permanent fault (e.g. nil
	// non-retryable error wrapped) will be observed by GetWorkflowError.
	selectErr error

	// iterateResult is what the mock IterateSpecReview activity returns
	// when iterateErr is nil. The default is stubIterationDriver.
	iterateResult iterateSpecReviewResult
	// iterateErr is the error the mock IterateSpecReview activity returns.
	// The workflow's iterate retry policy is MaximumAttempts=1; an error
	// here surfaces as a single activity fault.
	iterateErr error

	// iterateInput captures the input the workflow passed to IterateSpecReview
	// so tests can assert the workflow forwarded the lint violations and the
	// selected driver id verbatim.
	iterateInput iterateSpecReviewInput
	iterateCalls int

	// emitCalls records every EmitSpecIterationTelemetry invocation in
	// workflow-emission order. The test fixture's locking is needed because
	// the testsuite env executes activities on a worker goroutine.
	emitMu    sync.Mutex
	emitCalls []EmitSpecIterationTelemetryInput
}

// emittedEvents returns a snapshot of the captured emit calls under the
// mock's lock; safe to inspect after the workflow has settled.
func (m *specIterationMocks) emittedEvents() []EmitSpecIterationTelemetryInput {
	m.emitMu.Lock()
	defer m.emitMu.Unlock()
	out := make([]EmitSpecIterationTelemetryInput, len(m.emitCalls))
	copy(out, m.emitCalls)
	return out
}

// emittedTypes returns just the event-type strings from emittedEvents in
// emission order, making the canonical "did the workflow emit these in this
// order" assertion a one-liner.
func (m *specIterationMocks) emittedTypes() []string {
	events := m.emittedEvents()
	out := make([]string, len(events))
	for i, e := range events {
		out[i] = e.EventType
	}
	return out
}

// findEvent returns the first emitted event of the given type plus a
// "found" boolean. Used for the per-event assertions that don't depend on
// order (the order assertion is a separate check).
func (m *specIterationMocks) findEvent(eventType string) (EmitSpecIterationTelemetryInput, bool) {
	for _, e := range m.emittedEvents() {
		if e.EventType == eventType {
			return e, true
		}
	}
	return EmitSpecIterationTelemetryInput{}, false
}

// runSpecIteration drives one SpecIterationWorkflow execution in a fresh
// testsuite environment with the three activity surfaces mocked from the
// provided fixture. Mocks default to the happy-path values when the test
// leaves a field zero.
func runSpecIteration(
	t *testing.T,
	in SpecIterationInput,
	mocks *specIterationMocks,
) (SpecIterationResult, error) {
	t.Helper()
	if (mocks.iterateResult == iterateSpecReviewResult{}) && mocks.iterateErr == nil {
		mocks.iterateResult = stubIterationDriver
	}
	if mocks.selectResult.DriverID == "" && mocks.selectErr == nil && !mocks.selectResult.Unroutable {
		mocks.selectResult = activities.SelectDriverResult{
			DriverID: "claudecode",
			Reason:   "spec.author hit: claudecode",
		}
	}

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	env.RegisterActivityWithOptions(
		func(_ context.Context, in activities.SelectDriverInput) (activities.SelectDriverResult, error) {
			// Cheap sanity check: the workflow MUST request the
			// spec.author capability (FR-005) — otherwise selection would
			// route to a code-PR driver and produce a fixup commit with
			// the wrong prompt template.
			if in.Capability != string(driver.CapSpecAuthor) {
				return activities.SelectDriverResult{}, fmt.Errorf(
					"SelectDriver got capability %q, want %q",
					in.Capability, driver.CapSpecAuthor)
			}
			return mocks.selectResult, mocks.selectErr
		},
		specIterationActivityOpts("SelectDriver"),
	)

	env.RegisterActivityWithOptions(
		func(_ context.Context, in iterateSpecReviewInput) (iterateSpecReviewResult, error) {
			mocks.iterateInput = in
			mocks.iterateCalls++
			return mocks.iterateResult, mocks.iterateErr
		},
		specIterationActivityOpts("IterateSpecReview"),
	)

	env.RegisterActivityWithOptions(
		func(_ context.Context, in EmitSpecIterationTelemetryInput) error {
			mocks.emitMu.Lock()
			defer mocks.emitMu.Unlock()
			mocks.emitCalls = append(mocks.emitCalls, in)
			return nil
		},
		specIterationActivityOpts("EmitSpecIterationTelemetry"),
	)

	env.ExecuteWorkflow(SpecIterationWorkflow, in)
	if !env.IsWorkflowCompleted() {
		t.Fatal("SpecIterationWorkflow did not complete")
	}
	wfErr := env.GetWorkflowError()
	var out SpecIterationResult
	if wfErr == nil {
		if err := env.GetWorkflowResult(&out); err != nil {
			t.Fatalf("GetWorkflowResult: %v", err)
		}
	}
	return out, wfErr
}

// specIterationActivityOpts is the test-local activity register helper. The
// workflows package already declares `activityOpts` (scheduler_test.go) and
// `reviewActivityOpts` (pr_review_test.go); naming this one for spec
// iteration keeps the testsuite registrations grep-able per workflow file
// without colliding with the existing helpers.
func specIterationActivityOpts(name string) activity.RegisterOptions {
	return activity.RegisterOptions{Name: name}
}

// canonicalSpecInput is the standard input fixture used by every test that
// does not need a specific edge-case input. Mirrors the shape T015's
// dispatcher will produce.
func canonicalSpecInput() SpecIterationInput {
	return SpecIterationInput{
		PRNumber:   1057,
		PRBranch:   "spec/115-foo",
		TargetRepo: "/repos/chitinhq/chitin",
		Repo:       "chitinhq/chitin",
		ReviewID:   42,
		LintViolations: []SpecLintViolation{
			{Rule: "L05", File: "spec.md", Line: 130, Severity: "error",
				Message: "chitin-kernel events: unknown subcommand"},
			{Rule: "L05", File: "spec.md", Line: 145, Severity: "error",
				Message: "gh api /pulls/N/comments/M/replies returns 404"},
		},
	}
}

// --- Happy path: lint allowlist patch + spec fix ---

// TestSpecIteration_HappyPath_ActionCountsLintFix is the core T021
// assertion: with a stub driver that returns a fixup commit modeling one
// spec fix + one reply + two lint allowlist patches, the workflow's result
// surfaces those counts verbatim on `action_counts.lint_fix` (and the
// sibling fields), and emits the FR-009 round_started → completed chain
// event pair with the action counts attached to the completed event.
func TestSpecIteration_HappyPath_ActionCountsLintFix(t *testing.T) {
	mocks := &specIterationMocks{}
	in := canonicalSpecInput()
	out, err := runSpecIteration(t, in, mocks)
	if err != nil {
		t.Fatalf("workflow error: %v", err)
	}

	// --- Workflow result assertions ---

	if !out.PushedFixup {
		t.Errorf("PushedFixup = false, want true")
	}
	if out.FixupSHA != stubIterationDriver.FixupSHA {
		t.Errorf("FixupSHA = %q, want %q", out.FixupSHA, stubIterationDriver.FixupSHA)
	}
	if out.DriverID != "claudecode" {
		t.Errorf("DriverID = %q, want claudecode", out.DriverID)
	}
	if out.CommentCount != stubIterationDriver.CommentCount {
		t.Errorf("CommentCount = %d, want %d", out.CommentCount, stubIterationDriver.CommentCount)
	}
	if out.Unroutable {
		t.Errorf("Unroutable = true, want false on happy path")
	}

	// The headline T021 assertion: action_counts.lint_fix surfaces the two
	// allowlist patches the stub driver applied, and the sibling Fix /
	// Reply / Skip counts mirror the stub verbatim — the workflow does not
	// rewrite or aggregate counts en route.
	if out.ActionCounts.LintFix != 2 {
		t.Errorf("ActionCounts.LintFix = %d, want 2", out.ActionCounts.LintFix)
	}
	if out.ActionCounts.Fix != 1 {
		t.Errorf("ActionCounts.Fix = %d, want 1", out.ActionCounts.Fix)
	}
	if out.ActionCounts.Reply != 1 {
		t.Errorf("ActionCounts.Reply = %d, want 1", out.ActionCounts.Reply)
	}
	if out.ActionCounts.Skip != 0 {
		t.Errorf("ActionCounts.Skip = %d, want 0", out.ActionCounts.Skip)
	}

	// --- IterateSpecReview input assertions ---

	if mocks.iterateCalls != 1 {
		t.Errorf("IterateSpecReview called %d times, want 1 (v1 cap = 1 round)", mocks.iterateCalls)
	}
	if mocks.iterateInput.DriverID != "claudecode" {
		t.Errorf("activity received DriverID %q, want claudecode (selected upstream)",
			mocks.iterateInput.DriverID)
	}
	if got, want := len(mocks.iterateInput.LintViolations), len(in.LintViolations); got != want {
		t.Errorf("activity received %d lint violations, want %d (workflow must forward verbatim)",
			got, want)
	}
	if mocks.iterateInput.Round != 1 {
		t.Errorf("activity received Round %d, want 1", mocks.iterateInput.Round)
	}

	// --- Chain event assertions (FR-009) ---

	wantOrder := []string{
		SpecIterationEventRoundStarted,
		SpecIterationEventCompleted,
	}
	if got := mocks.emittedTypes(); !equalStrings(got, wantOrder) {
		t.Errorf("emitted events = %v, want %v", got, wantOrder)
	}

	started, ok := mocks.findEvent(SpecIterationEventRoundStarted)
	if !ok {
		t.Fatalf("no %s event emitted", SpecIterationEventRoundStarted)
	}
	if started.PRNumber != in.PRNumber {
		t.Errorf("round_started PRNumber = %d, want %d", started.PRNumber, in.PRNumber)
	}
	if started.Round != 1 {
		t.Errorf("round_started Round = %d, want 1", started.Round)
	}
	if started.ReviewID != in.ReviewID {
		t.Errorf("round_started ReviewID = %d, want %d", started.ReviewID, in.ReviewID)
	}
	if started.DriverID != "claudecode" {
		t.Errorf("round_started DriverID = %q, want claudecode", started.DriverID)
	}
	if got, want := started.LintViolationsCount, len(in.LintViolations); got != want {
		t.Errorf("round_started LintViolationsCount = %d, want %d", got, want)
	}

	completed, ok := mocks.findEvent(SpecIterationEventCompleted)
	if !ok {
		t.Fatalf("no %s event emitted", SpecIterationEventCompleted)
	}
	if completed.FixupSHA != stubIterationDriver.FixupSHA {
		t.Errorf("completed FixupSHA = %q, want %q", completed.FixupSHA, stubIterationDriver.FixupSHA)
	}
	if completed.ActionCounts.LintFix != 2 {
		t.Errorf("completed ActionCounts.LintFix = %d, want 2", completed.ActionCounts.LintFix)
	}
	if completed.ActionCounts.Fix != 1 {
		t.Errorf("completed ActionCounts.Fix = %d, want 1", completed.ActionCounts.Fix)
	}
	if completed.CommentCount != stubIterationDriver.CommentCount {
		t.Errorf("completed CommentCount = %d, want %d",
			completed.CommentCount, stubIterationDriver.CommentCount)
	}
}

// --- Skipped path: no spec-author driver available ---

// TestSpecIteration_Unroutable_EmitsSkipped covers the FR-005 + FR-010 path
// where SelectDriver returns Unroutable because no registered driver
// satisfies `spec.author`. The workflow MUST NOT dispatch the iteration
// activity, MUST emit `spec_iteration_skipped` (not _round_started or
// _completed, since no round ran), and MUST mark the result Unroutable so
// the dispatcher can surface the right operator escalation kind.
func TestSpecIteration_Unroutable_EmitsSkipped(t *testing.T) {
	mocks := &specIterationMocks{
		selectResult: activities.SelectDriverResult{
			Unroutable:        true,
			MissingCapability: string(driver.CapSpecAuthor),
			Reason:            "no driver satisfies spec.author",
		},
	}
	in := canonicalSpecInput()
	out, err := runSpecIteration(t, in, mocks)
	if err != nil {
		t.Fatalf("workflow error: %v", err)
	}

	if !out.Unroutable {
		t.Errorf("Unroutable = false, want true")
	}
	if out.PushedFixup {
		t.Errorf("PushedFixup = true, want false (no driver ran)")
	}
	if mocks.iterateCalls != 0 {
		t.Errorf("IterateSpecReview called %d times, want 0 (must short-circuit on Unroutable)",
			mocks.iterateCalls)
	}

	wantOrder := []string{SpecIterationEventSkipped}
	if got := mocks.emittedTypes(); !equalStrings(got, wantOrder) {
		t.Errorf("emitted events = %v, want %v", got, wantOrder)
	}
	skipped, _ := mocks.findEvent(SpecIterationEventSkipped)
	if skipped.Reason != "no_spec_author_driver" {
		t.Errorf("skipped Reason = %q, want no_spec_author_driver", skipped.Reason)
	}
	if got, want := skipped.LintViolationsCount, len(in.LintViolations); got != want {
		t.Errorf("skipped LintViolationsCount = %d, want %d (carry forward for dispatcher)",
			got, want)
	}
}

// --- Failed path: iteration activity faulted ---

// TestSpecIteration_ActivityFault_EmitsFailed covers the FR-009 path where
// the IterateSpecReview activity itself returns an error (genuine fault,
// not a graceful "no-op" result). The workflow MUST emit
// `spec_iteration_failed` with the failure kind and detail, MUST still
// surface the selected DriverID on the result so the operator can identify
// which driver faulted, and MUST propagate the error so the dispatcher's
// caller can decide whether to retry or escalate.
//
// The round_started event MUST still have fired before the activity ran —
// the chain trail must record the attempt regardless of outcome.
func TestSpecIteration_ActivityFault_EmitsFailed(t *testing.T) {
	wantErr := errors.New("driver worktree fetch refused: permission denied")
	mocks := &specIterationMocks{
		iterateErr: wantErr,
	}
	in := canonicalSpecInput()
	out, err := runSpecIteration(t, in, mocks)
	if err == nil {
		t.Fatalf("workflow error = nil, want non-nil (activity fault must propagate)")
	}

	if out.DriverID != "" {
		// On the error return path, the workflow does not call
		// GetWorkflowResult — out stays zero. This is documented testsuite
		// behaviour; the assertion is here to flag any future refactor
		// that starts returning a populated result on errors (which would
		// change the dispatcher's error-handling contract).
		t.Errorf("expected zero result on workflow error, got DriverID=%q", out.DriverID)
	}

	if mocks.iterateCalls != 1 {
		t.Errorf("IterateSpecReview called %d times, want 1", mocks.iterateCalls)
	}

	wantOrder := []string{
		SpecIterationEventRoundStarted,
		SpecIterationEventFailed,
	}
	if got := mocks.emittedTypes(); !equalStrings(got, wantOrder) {
		t.Errorf("emitted events = %v, want %v", got, wantOrder)
	}
	failed, _ := mocks.findEvent(SpecIterationEventFailed)
	if failed.FailureKind != "activity_fault" {
		t.Errorf("failed FailureKind = %q, want activity_fault", failed.FailureKind)
	}
	if failed.Detail == "" {
		t.Errorf("failed Detail empty; want the activity error string")
	}
	if failed.DriverID != "claudecode" {
		t.Errorf("failed DriverID = %q, want claudecode (selected upstream)", failed.DriverID)
	}
}

// --- Input validation: bad inputs return non-retryable error before emit ---

// TestSpecIteration_BadInput_NoEmits covers the workflow's input guard —
// missing PRBranch / TargetRepo / Repo or ReviewID <= 0 returns a
// NonRetryableApplicationError BEFORE any activity dispatch (so no
// SelectDriver, no IterateSpecReview, and crucially no telemetry emit
// either). This protects the chain from being polluted with events that
// describe a workflow run that never actually started its round.
func TestSpecIteration_BadInput_NoEmits(t *testing.T) {
	cases := []struct {
		name string
		in   SpecIterationInput
	}{
		{"missing PRBranch", SpecIterationInput{
			PRNumber: 1, PRBranch: "", TargetRepo: "/r", Repo: "o/r", ReviewID: 1,
		}},
		{"missing TargetRepo", SpecIterationInput{
			PRNumber: 1, PRBranch: "b", TargetRepo: "", Repo: "o/r", ReviewID: 1,
		}},
		{"missing Repo", SpecIterationInput{
			PRNumber: 1, PRBranch: "b", TargetRepo: "/r", Repo: "", ReviewID: 1,
		}},
		{"zero PRNumber", SpecIterationInput{
			PRNumber: 0, PRBranch: "b", TargetRepo: "/r", Repo: "o/r", ReviewID: 1,
		}},
		{"zero ReviewID", SpecIterationInput{
			PRNumber: 1, PRBranch: "b", TargetRepo: "/r", Repo: "o/r", ReviewID: 0,
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mocks := &specIterationMocks{}
			_, err := runSpecIteration(t, tc.in, mocks)
			if err == nil {
				t.Fatal("workflow error = nil, want NonRetryableApplicationError")
			}
			var appErr *temporal.ApplicationError
			if !errors.As(err, &appErr) {
				t.Fatalf("error = %v (type %T), want *temporal.ApplicationError", err, err)
			}
			if !appErr.NonRetryable() {
				t.Errorf("error.NonRetryable() = false, want true")
			}
			if appErr.Type() != "InvalidSpecIterationInput" {
				t.Errorf("error.Type() = %q, want InvalidSpecIterationInput", appErr.Type())
			}
			if mocks.iterateCalls != 0 {
				t.Errorf("IterateSpecReview called %d times, want 0", mocks.iterateCalls)
			}
			if got := len(mocks.emittedEvents()); got != 0 {
				t.Errorf("emitted %d telemetry events on bad input, want 0", got)
			}
		})
	}
}

// equalStrings is a tiny equality helper for ordered string-slice
// comparisons. Local to the test file to avoid pulling reflect.DeepEqual
// for what is essentially len + per-index compare.
func equalStrings(a, b []string) bool {
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
