package workflows

import (
	"context"
	"errors"
	"testing"

	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"

	"github.com/chitinhq/chitin/go/orchestrator/activities"
	"github.com/chitinhq/chitin/go/orchestrator/schedules"
)

// Spec 081 US2 tests for ScheduledJobWorkflow — the generic workflow a Temporal
// Schedule triggers for a migrated cron. The workflow runs the job once via the
// RunScheduledJob activity and returns a typed JobResult; the activity is
// mocked so the workflow runs hermetically and replays deterministically. The
// TestWorkflowEnvironment panics on any non-determinism.

// runScheduledJob executes ScheduledJobWorkflow once over spec, mocking
// RunScheduledJob to return mockResult / mockErr. It returns the workflow's
// JobResult and error, and records the JobSpec the workflow dispatched to the
// activity — the proof the workflow ran the job it was given.
func runScheduledJob(
	t *testing.T,
	spec schedules.JobSpec,
	mockResult activities.ScheduledJobResult,
	mockErr error,
) (JobResult, error, schedules.JobSpec) {
	t.Helper()
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	// Mock RunScheduledJob: capture the spec the workflow dispatched and serve
	// the configured result. Registered under the same name the workflow
	// dispatches to.
	var dispatched schedules.JobSpec
	env.RegisterActivityWithOptions(
		func(_ context.Context, in schedules.JobSpec) (activities.ScheduledJobResult, error) {
			dispatched = in
			return mockResult, mockErr
		},
		activity.RegisterOptions{Name: (&activities.ScheduledJob{}).ActivityName()},
	)

	env.ExecuteWorkflow(ScheduledJobWorkflow, spec)

	if !env.IsWorkflowCompleted() {
		t.Fatalf("ScheduledJobWorkflow did not complete")
	}
	wfErr := env.GetWorkflowError()
	var result JobResult
	if wfErr == nil {
		if err := env.GetWorkflowResult(&result); err != nil {
			t.Fatalf("decoding JobResult: %v", err)
		}
	}
	return result, wfErr, dispatched
}

// TestScheduledJobWorkflow_RunsJobAndReturnsResult is the US2 happy path: the
// workflow dispatches the JobSpec to RunScheduledJob and returns the activity's
// result as a typed JobResult.
func TestScheduledJobWorkflow_RunsJobAndReturnsResult(t *testing.T) {
	spec := schedules.JobSpec{Name: "swarm-audit", Command: "/usr/bin/true", Cron: "0 8 * * *"}
	result, err, dispatched := runScheduledJob(t, spec, activities.ScheduledJobResult{
		JobName:     "swarm-audit",
		Succeeded:   true,
		ExitCode:    0,
		Output:      "audit complete",
		Explanation: "scheduled job \"swarm-audit\": completed",
	}, nil)

	if err != nil {
		t.Fatalf("workflow errored on a successful job run: %v", err)
	}
	// The workflow ran the job it was given.
	if dispatched.Name != "swarm-audit" {
		t.Errorf("workflow dispatched job %q, want swarm-audit", dispatched.Name)
	}
	// The result is the activity's result, typed.
	if !result.Succeeded {
		t.Errorf("JobResult.Succeeded = false, want true; result=%+v", result)
	}
	if result.JobName != "swarm-audit" {
		t.Errorf("JobResult.JobName = %q, want swarm-audit", result.JobName)
	}
	if result.ExitCode != 0 {
		t.Errorf("JobResult.ExitCode = %d, want 0", result.ExitCode)
	}
	if result.Output != "audit complete" {
		t.Errorf("JobResult.Output = %q, want %q", result.Output, "audit complete")
	}
}

// TestScheduledJobWorkflow_CarriesFailedRun proves a non-zero script exit — a
// failed RESULT from the activity, not an activity error — flows through as a
// JobResult with Succeeded=false. The workflow itself does NOT error: a failed
// cycle is a completed workflow with a failed result, so the Schedule's next
// cycle still fires.
func TestScheduledJobWorkflow_CarriesFailedRun(t *testing.T) {
	spec := schedules.JobSpec{Name: "swarm-audit", Command: "/usr/bin/false"}
	result, err, _ := runScheduledJob(t, spec, activities.ScheduledJobResult{
		JobName:     "swarm-audit",
		Succeeded:   false,
		ExitCode:    1,
		Explanation: "scheduled job \"swarm-audit\": exited 1",
	}, nil)

	if err != nil {
		t.Fatalf("a non-zero script exit must be a completed workflow with a failed result, not a workflow error: %v", err)
	}
	if result.Succeeded {
		t.Errorf("JobResult.Succeeded = true, want false for a non-zero-exit script")
	}
	if result.ExitCode != 1 {
		t.Errorf("JobResult.ExitCode = %d, want 1", result.ExitCode)
	}
}

// TestScheduledJobWorkflow_PropagatesActivityFault proves a transport/infra
// fault running the activity — an activity ERROR, distinct from a non-zero
// script exit — surfaces as a workflow error after the retry policy is
// exhausted, rather than being swallowed.
func TestScheduledJobWorkflow_PropagatesActivityFault(t *testing.T) {
	spec := schedules.JobSpec{Name: "swarm-audit", Command: "/usr/bin/true"}
	_, err, _ := runScheduledJob(t, spec, activities.ScheduledJobResult{},
		errors.New("activity worker unreachable"))

	if err == nil {
		t.Fatal("an activity fault must surface as a workflow error, not be swallowed")
	}
}
