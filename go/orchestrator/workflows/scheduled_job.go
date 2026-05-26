package workflows

import (
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/chitinhq/chitin/go/orchestrator/activities"
	"github.com/chitinhq/chitin/go/orchestrator/schedules"
)

// JobResult is the typed outcome of one ScheduledJobWorkflow run — a durable,
// inspectable record of a migrated cron's cycle, returned into Temporal
// history.
type JobResult struct {
	// JobName echoes the migrated job (schedules.JobSpec.Name).
	JobName string `json:"job_name"`
	// Succeeded is true iff the job's script exited zero.
	Succeeded bool `json:"succeeded"`
	// ExitCode is the script's process exit code; -1 when the script never ran.
	ExitCode int `json:"exit_code"`
	// Output is the trimmed combined stdout of the script.
	Output string `json:"output"`
	// Explanation is a human-readable account of the outcome.
	Explanation string `json:"explanation"`
}

// ScheduledJobWorkflow is the generic workflow a Temporal Schedule triggers
// for a migrated cron (spec 081 US2, FR-004). It is the durable replacement
// for a systemd timer's one-shot service: a Schedule fires it on the job's
// cron cadence; it runs the job's existing script once via the RunScheduledJob
// activity and returns a typed JobResult into Temporal history.
//
// The workflow is small and deterministic — it derives nothing from a clock or
// from randomness; the only inputs are the durable JobSpec argument and the
// activity's recorded result, so it replays identically. The script execution
// — a subprocess, a side effect — happens entirely inside the activity.
//
// It is registered under the type name "ScheduledJobWorkflow"; schedules.
// EnsureSchedules references the Schedule's action workflow by exactly that
// string, so the schedules package need not import this one.
func ScheduledJobWorkflow(ctx workflow.Context, spec schedules.JobSpec) (JobResult, error) {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		// A generous ceiling: a migrated audit/ingest script can run for
		// minutes. 30m bounds a stuck run without truncating a healthy one.
		StartToCloseTimeout: 30 * time.Minute,
		// The activity heartbeats every 20s; a 2-minute HeartbeatTimeout
		// detects a wedged script with a 6x margin while still letting the
		// StartToClose timeout — not the heartbeat — govern a slow-but-live run.
		HeartbeatTimeout: 2 * time.Minute,
		// Modest retry: a migrated cron is read-mostly and idempotent per
		// cycle, so a transient failure is worth a few quick retries, but a
		// persistently failing job should surface on the next scheduled cycle
		// rather than retry forever.
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    10 * time.Second,
			BackoffCoefficient: 2.0,
			MaximumInterval:    2 * time.Minute,
			MaximumAttempts:    3,
		},
	})

	if spec.ActivityName != "" {
		if err := workflow.ExecuteActivity(ctx, spec.ActivityName, spec.ActivityInput).Get(ctx, nil); err != nil {
			return JobResult{JobName: spec.Name, ExitCode: -1}, err
		}
		return JobResult{
			JobName:     spec.Name,
			Succeeded:   true,
			ExitCode:    0,
			Explanation: "orchestrator-native activity completed",
		}, nil
	}

	job := activities.NewScheduledJob()
	var res activities.ScheduledJobResult
	err := workflow.ExecuteActivity(ctx, job.ActivityName(), spec).Get(ctx, &res)
	if err != nil {
		// A transport/infra fault running the activity (not a non-zero script
		// exit — that is carried in res). Surface it; the Schedule's next
		// cycle still fires.
		return JobResult{JobName: spec.Name, ExitCode: -1}, err
	}

	return JobResult{
		JobName:     res.JobName,
		Succeeded:   res.Succeeded,
		ExitCode:    res.ExitCode,
		Output:      res.Output,
		Explanation: res.Explanation,
	}, nil
}
