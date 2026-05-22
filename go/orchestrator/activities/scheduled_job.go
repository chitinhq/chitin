package activities

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"go.temporal.io/sdk/activity"

	"github.com/chitinhq/chitin/go/orchestrator/schedules"
)

// scheduledJobHeartbeatInterval is how often RunScheduledJob beats while a
// migrated cron's script is in flight. A migrated audit/ingest script can run
// for minutes; a single heartbeat would let a run longer than the workflow's
// HeartbeatTimeout be killed mid-cycle. 20 seconds matches driver/invoke.go's
// interval and leaves a wide margin under any reasonable HeartbeatTimeout.
const scheduledJobHeartbeatInterval = 20 * time.Second

// ScheduledJobResult is the typed outcome of one RunScheduledJob run — the
// result of running a migrated cron's script once.
//
// A non-zero script exit is carried here as Succeeded=false, NOT as an
// activity error — exactly as DeterministicStepResult carries a failed
// mechanical step. The activity error return is reserved for an input the
// activity cannot act on at all (an empty Command).
type ScheduledJobResult struct {
	// JobName echoes the migrated job (JobSpec.Name), for correlation in
	// Temporal history and the returned explanation.
	JobName string `json:"job_name"`
	// Succeeded is true iff the script exited zero.
	Succeeded bool `json:"succeeded"`
	// ExitCode is the script's process exit code; -1 when the script never
	// ran (empty Command, or the binary could not be started).
	ExitCode int `json:"exit_code"`
	// Output is the trimmed combined stdout of the script — the work-product
	// reference for telemetry.
	Output string `json:"output"`
	// Explanation is a human-readable account of the outcome.
	Explanation string `json:"explanation"`
}

// ScheduledJob is the RunScheduledJob activity (spec 081 US2, FR-004): it runs
// one migrated cron's existing script. Running a script is a SIDE EFFECT —
// subprocess and filesystem I/O — so it MUST run in an activity, never in
// workflow code. The activity carries no startup-bound dependency: a migrated
// job is a self-contained command, so a zero-value ScheduledJob is usable.
//
// The activity wraps the job's existing script; it does not reimplement the
// job (spec 081 assumption: a migration replicates what the cron did). The
// script's process inherits the worker host's environment (os.Environ()), so
// it sees the same PATH and config a systemd-run process would, keeping it
// governed by the chitin kernel exactly as the cron's process was (FR-008).
type ScheduledJob struct{}

// NewScheduledJob returns a RunScheduledJob activity. It takes no dependencies
// — a migrated job is a self-contained command.
func NewScheduledJob() *ScheduledJob { return &ScheduledJob{} }

// ActivityName is the stable Temporal activity name RunScheduledJob registers
// under and ScheduledJobWorkflow dispatches to.
func (a *ScheduledJob) ActivityName() string { return "RunScheduledJob" }

// Execute runs one migrated cron's script and returns a typed result. It is
// the activity function registered with the Temporal worker.
//
// A non-zero exit code is NOT an activity error — it is a normal failed run:
// the result carries Succeeded=false and the workflow returns a failed
// JobResult, identically to DeterministicStep.Execute. The error return is
// reserved for an input the activity cannot act on at all — an empty Command —
// surfaced as a non-success result so the schedule's next cycle still fires.
func (a *ScheduledJob) Execute(ctx context.Context, spec schedules.JobSpec) (ScheduledJobResult, error) {
	if strings.TrimSpace(spec.Command) == "" {
		// A migrated job with no command cannot run. Settle it failed — never
		// silently skip it.
		return ScheduledJobResult{
			JobName:   spec.Name,
			Succeeded: false,
			ExitCode:  -1,
			Explanation: fmt.Sprintf(
				"scheduled job %q has no command — cannot run the migrated cron", spec.Name),
		}, nil
	}

	// Heartbeat for the WHOLE run, not just once, so a long-running migrated
	// script stays visible and the activity's StartToClose timeout — not the
	// shorter HeartbeatTimeout — governs liveness. A background ticker beats
	// while the script runs and stops the instant it returns. The heartbeat
	// is recover-guarded: activity.RecordHeartbeat PANICS when ctx is not a
	// Temporal activity context (e.g. a direct unit-test call), so a recover
	// makes the beat a silent no-op rather than crashing this goroutine
	// (mirrors driver/invoke.go).
	stopHeartbeat := make(chan struct{})
	defer close(stopHeartbeat)
	go func() {
		beat := func() {
			defer func() { _ = recover() }()
			activity.RecordHeartbeat(ctx, fmt.Sprintf("running scheduled job %s", spec.Name))
		}
		beat() // an immediate first beat — never wait a full interval to be seen.
		ticker := time.NewTicker(scheduledJobHeartbeatInterval)
		defer ticker.Stop()
		for {
			select {
			case <-stopHeartbeat:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				beat()
			}
		}
	}()

	cmd := exec.CommandContext(ctx, spec.Command, spec.Args...)
	cmd.Env = os.Environ()

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()

	out := strings.TrimSpace(stdout.String())
	errOut := strings.TrimSpace(stderr.String())
	line := scheduledJobCommandLine(spec.Command, spec.Args)

	if runErr != nil {
		exitCode := -1
		var exitErr *exec.ExitError
		if ok := asExitError(runErr, &exitErr); ok {
			exitCode = exitErr.ExitCode()
		}
		explanation := fmt.Sprintf("scheduled job %q: %q exited %d", spec.Name, line, exitCode)
		if errOut != "" {
			explanation += ": " + errOut
		}
		return ScheduledJobResult{
			JobName:     spec.Name,
			Succeeded:   false,
			ExitCode:    exitCode,
			Output:      out,
			Explanation: explanation,
		}, nil
	}

	explanation := fmt.Sprintf("scheduled job %q: %q completed", spec.Name, line)
	if errOut != "" {
		explanation += "; stderr: " + errOut
	}
	return ScheduledJobResult{
		JobName:     spec.Name,
		Succeeded:   true,
		ExitCode:    0,
		Output:      out,
		Explanation: explanation,
	}, nil
}

// scheduledJobCommandLine renders a command and its arguments as a single
// readable string for the result explanation.
func scheduledJobCommandLine(command string, args []string) string {
	if len(args) == 0 {
		return command
	}
	return command + " " + strings.Join(args, " ")
}
