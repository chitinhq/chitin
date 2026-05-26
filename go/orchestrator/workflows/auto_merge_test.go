package workflows

import (
	"context"
	"testing"
	"time"

	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"

	"github.com/chitinhq/chitin/go/orchestrator/activities"
)

type autoMergeMocks struct {
	reports       []activities.MergeabilityReport
	events        []activities.AutoMergeEventInput
	mergeCalls    int
	unlabelCalls  int
	commentCalls  int
	notifyCalls   int
	commentIDs    []activities.CommentTemplateID
	notifySummary []string
}

func runAutoMergeWorkflow(t *testing.T, in AutoMergeInput, mocks *autoMergeMocks, beforeStart func(*testsuite.TestWorkflowEnvironment)) AutoMergeResult {
	t.Helper()
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.RegisterActivityWithOptions(func(_ context.Context, _ activities.CheckPRMergeabilityInput) (activities.MergeabilityReport, error) {
		if len(mocks.reports) == 0 {
			return activities.MergeabilityReport{CIStatus: activities.CIStatusPending, IsOpen: true, HasLabel: true}, nil
		}
		r := mocks.reports[0]
		if len(mocks.reports) > 1 {
			mocks.reports = mocks.reports[1:]
		}
		return r, nil
	}, activity.RegisterOptions{Name: "CheckPRMergeability"})
	env.RegisterActivityWithOptions(func(_ context.Context, in activities.AutoMergeEventInput) error {
		mocks.events = append(mocks.events, in)
		return nil
	}, activity.RegisterOptions{Name: "EmitAutoMergeEvent"})
	env.RegisterActivityWithOptions(func(_ context.Context, _ activities.MergePRInput) (activities.MergeResult, error) {
		mocks.mergeCalls++
		return activities.MergeResult{Succeeded: true, MergeSHA: "0123456789012345678901234567890123456789", HeadBranchDeleted: true}, nil
	}, activity.RegisterOptions{Name: "MergePR"})
	env.RegisterActivityWithOptions(func(_ context.Context, _ activities.UnlabelPRInput) (activities.UnlabelPRResult, error) {
		mocks.unlabelCalls++
		return activities.UnlabelPRResult{Succeeded: true, WasPresent: true}, nil
	}, activity.RegisterOptions{Name: "UnlabelPR"})
	env.RegisterActivityWithOptions(func(_ context.Context, in activities.CommentPRInput) (activities.CommentPRResult, error) {
		mocks.commentCalls++
		mocks.commentIDs = append(mocks.commentIDs, in.TemplateID)
		return activities.CommentPRResult{Posted: true}, nil
	}, activity.RegisterOptions{Name: "CommentPR"})
	env.RegisterActivityWithOptions(func(_ context.Context, ev activities.NotificationEvent) error {
		mocks.notifyCalls++
		mocks.notifySummary = append(mocks.notifySummary, ev.Summary)
		return nil
	}, activity.RegisterOptions{Name: "DiscordNotify"})
	if beforeStart != nil {
		beforeStart(env)
	}
	env.ExecuteWorkflow(AutoMergeWorkflow, in)
	if !env.IsWorkflowCompleted() {
		t.Fatal("AutoMergeWorkflow did not complete")
	}
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	var out AutoMergeResult
	if err := env.GetWorkflowResult(&out); err != nil {
		t.Fatalf("GetWorkflowResult: %v", err)
	}
	return out
}

func validAutoMergeInput() AutoMergeInput {
	return AutoMergeInput{Repo: "chitinhq/chitin", PRNumber: 1135, LabelName: activities.ReadyToMergeLabel, TriggerEventID: "delivery-1", TimeoutSeconds: 300}
}

func TestAutoMergeWorkflow_GreenMergesImmediately(t *testing.T) {
	m := &autoMergeMocks{reports: []activities.MergeabilityReport{{CIStatus: activities.CIStatusGreen, IsMergeable: true, IsOpen: true, HasLabel: true}}}
	out := runAutoMergeWorkflow(t, validAutoMergeInput(), m, nil)
	if out.Outcome != "succeeded" || m.mergeCalls != 1 {
		t.Fatalf("out=%+v mergeCalls=%d, want succeeded + one merge", out, m.mergeCalls)
	}
	want := []string{activities.AutoMergeTriggered, activities.AutoMergeSucceeded}
	assertEventTypes(t, m.events, want)
}

func TestAutoMergeWorkflow_PendingThenGreenWaits(t *testing.T) {
	m := &autoMergeMocks{reports: []activities.MergeabilityReport{
		{CIStatus: activities.CIStatusPending, IsOpen: true, HasLabel: true},
		{CIStatus: activities.CIStatusPending, IsOpen: true, HasLabel: true},
		{CIStatus: activities.CIStatusGreen, IsMergeable: true, IsOpen: true, HasLabel: true},
	}}
	out := runAutoMergeWorkflow(t, validAutoMergeInput(), m, nil)
	if out.Outcome != "succeeded" || m.mergeCalls != 1 {
		t.Fatalf("out=%+v mergeCalls=%d", out, m.mergeCalls)
	}
	assertEventTypes(t, m.events, []string{activities.AutoMergeTriggered, activities.AutoMergeWaiting, activities.AutoMergeWaiting, activities.AutoMergeSucceeded})
	if intFieldAny(m.events[1].Payload["elapsed_seconds"]) >= intFieldAny(m.events[2].Payload["elapsed_seconds"]) {
		t.Fatalf("elapsed seconds not increasing: %+v %+v", m.events[1].Payload, m.events[2].Payload)
	}
}

func TestAutoMergeWorkflow_CIFailedEscalates(t *testing.T) {
	m := &autoMergeMocks{reports: []activities.MergeabilityReport{{CIStatus: activities.CIStatusFailed, IsOpen: true, HasLabel: true, FailedChecks: []string{"test", "Analyze (go)"}}}}
	out := runAutoMergeWorkflow(t, validAutoMergeInput(), m, nil)
	if out.FailureReason != "auto_merge_ci_failed" || m.mergeCalls != 0 || m.unlabelCalls != 1 || m.commentCalls != 1 || m.notifyCalls != 1 {
		t.Fatalf("out=%+v merge=%d unlabel=%d comment=%d notify=%d", out, m.mergeCalls, m.unlabelCalls, m.commentCalls, m.notifyCalls)
	}
	if m.commentIDs[0] != activities.CommentTemplateCIFailed {
		t.Fatalf("template=%q want ci_failed", m.commentIDs[0])
	}
	assertEventTypes(t, m.events, []string{activities.AutoMergeTriggered, activities.AutoMergeCIFailed})
}

func TestAutoMergeWorkflow_ConflictEscalates(t *testing.T) {
	m := &autoMergeMocks{reports: []activities.MergeabilityReport{{CIStatus: activities.CIStatusGreen, IsMergeable: false, IsOpen: true, HasLabel: true, MergeStateStatus: "DIRTY"}}}
	out := runAutoMergeWorkflow(t, validAutoMergeInput(), m, nil)
	if out.FailureReason != "auto_merge_conflict" || m.unlabelCalls != 1 || m.commentIDs[0] != activities.CommentTemplateMergeConflict || m.notifyCalls != 1 {
		t.Fatalf("out=%+v unlabel=%d templates=%v notify=%d", out, m.unlabelCalls, m.commentIDs, m.notifyCalls)
	}
	assertEventTypes(t, m.events, []string{activities.AutoMergeTriggered, activities.AutoMergeConflict})
}

func TestAutoMergeWorkflow_CITimeoutEscalatesOnce(t *testing.T) {
	in := validAutoMergeInput()
	in.TimeoutSeconds = 60
	m := &autoMergeMocks{reports: []activities.MergeabilityReport{{CIStatus: activities.CIStatusPending, IsOpen: true, HasLabel: true}}}
	out := runAutoMergeWorkflow(t, in, m, nil)
	if out.FailureReason != "auto_merge_ci_timeout" || m.unlabelCalls != 1 || m.commentCalls != 1 || m.notifyCalls != 1 {
		t.Fatalf("out=%+v unlabel=%d comment=%d notify=%d", out, m.unlabelCalls, m.commentCalls, m.notifyCalls)
	}
	assertEventTypes(t, m.events, []string{activities.AutoMergeTriggered, activities.AutoMergeWaiting, activities.AutoMergeCITimeout})
}

func TestAutoMergeWorkflow_LabelRemovedMidWaitCancels(t *testing.T) {
	m := &autoMergeMocks{reports: []activities.MergeabilityReport{{CIStatus: activities.CIStatusPending, IsOpen: true, HasLabel: true}}}
	out := runAutoMergeWorkflow(t, validAutoMergeInput(), m, func(env *testsuite.TestWorkflowEnvironment) {
		env.RegisterDelayedCallback(func() {
			env.SignalWorkflow(LabelRemovedSignal, true)
		}, 10*time.Second)
	})
	if out.FailureReason != "label_removed_mid_wait" || m.mergeCalls != 0 || m.unlabelCalls != 0 || m.commentCalls != 0 || m.notifyCalls != 0 {
		t.Fatalf("out=%+v merge=%d unlabel=%d comment=%d notify=%d", out, m.mergeCalls, m.unlabelCalls, m.commentCalls, m.notifyCalls)
	}
	assertEventTypes(t, m.events, []string{activities.AutoMergeTriggered, activities.AutoMergeWaiting, activities.AutoMergeCanceled})
}

func assertEventTypes(t *testing.T, got []activities.AutoMergeEventInput, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("event count=%d want %d events=%+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i].EventType != want[i] {
			t.Fatalf("event[%d]=%q want %q all=%+v", i, got[i].EventType, want[i], got)
		}
	}
}

func intFieldAny(v any) int {
	switch x := v.(type) {
	case int:
		return x
	case int64:
		return int(x)
	case float64:
		return int(x)
	default:
		return 0
	}
}
