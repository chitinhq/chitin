// Package schedules holds the Schedule-backed cron migration pattern for the
// Chitin Orchestrator (spec 081 US2, FR-004..FR-008).
//
// A migrated cron is a Temporal Schedule (a cron expression) that triggers a
// generic ScheduledJobWorkflow, which runs the job's existing script in a
// RunScheduledJob activity. The migration does NOT reimplement a job — it
// wraps the existing script and moves the trigger from a systemd timer to a
// durable, inspectable Temporal Schedule (spec 081 FR-005, FR-008).
//
// JobSpec lives in this package — not workflows — so workflows can import it
// without a cycle: EnsureSchedules names the workflow by its registered string
// type name ScheduledJobWorkflowName rather than the function symbol, so
// schedules never imports workflows.
package schedules

import (
	"context"
	"errors"
	"fmt"
	"log"

	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
)

// TaskQueue is the single task queue the orchestrator polls — the queue a
// scheduled job's workflow and activity run on. It MUST match the worker
// host's task queue (cmd/chitin-orchestrator/main.go TaskQueue).
const TaskQueue = "chitin"

// ScheduledJobWorkflowName is the registered Temporal type name of the generic
// scheduled-job workflow (workflows.ScheduledJobWorkflow). EnsureSchedules
// references the workflow by this string — not the function symbol — so the
// schedules package never imports the workflows package, breaking what would
// otherwise be an import cycle (workflows imports schedules for JobSpec).
const ScheduledJobWorkflowName = "ScheduledJobWorkflow"

// schedulePrefix is prepended to a JobSpec.Name to form the stable Temporal
// Schedule ID and the started workflow's ID. A stable, deterministic ID is
// what makes EnsureSchedules idempotent: a second Create with the same ID is
// an already-exists no-op, never a duplicate Schedule.
const schedulePrefix = "chitin-job-"

// JobSpec describes one migrated cron: the existing script to run, the cadence
// to run it on, and the time zone that cadence is read in. A JobSpec is the
// durable input both to the Temporal Schedule (the cron) and to every workflow
// run the Schedule triggers (the command). It is plain data — serializable as
// a workflow argument — and carries no behavior.
type JobSpec struct {
	// Name is the job's stable identifier — the former systemd unit's base
	// name (e.g. "swarm-audit"). It forms the Schedule ID; it MUST be unique
	// across the Registry.
	Name string `json:"name"`
	// Command is the absolute path of the program to run — the job's existing
	// script. The migration wraps this script; it does not reimplement it.
	Command string `json:"command"`
	// Args are the arguments passed to Command. Most migrated audit/ingest
	// scripts take none.
	Args []string `json:"args"`
	// Cron is the standard cron expression for the job's cadence — the same
	// cadence as the retired systemd timer (spec 081 FR-005).
	Cron string `json:"cron"`
	// TimeZone is the IANA time zone name the Cron expression is evaluated in
	// (e.g. "America/Detroit"). An empty value means UTC.
	TimeZone string `json:"time_zone"`
	// Description is a one-line human-readable account of the job, surfaced as
	// the Schedule's note.
	Description string `json:"description"`
}

// ScheduleID is the stable Temporal Schedule ID for this job. It is a pure
// function of the job's Name, so EnsureSchedules is idempotent: re-running it
// for an already-registered job collides on this ID and is a no-op.
func (s JobSpec) ScheduleID() string { return schedulePrefix + s.Name }

// Registry returns every cron migrated to a Temporal Schedule. Spec 081 US2
// migrates the periodic read-mostly crons; with T010–T012 landed it holds all
// seven US2 jobs — the two audits and the five telemetry ingesters. Later US3
// tasks (the watchdog, mutation, ops, and bench jobs) append to this slice.
//
// The slice is returned fresh on each call (never a shared mutable global) so
// no caller can mutate the canonical inventory.
func Registry() []JobSpec {
	return []JobSpec{
		// T009 — the daily/weekly audits.
		swarmAuditSpec(),
		architectureAuditSpec(),
		// T011 — the Argus telemetry ingesters.
		argusIngestBeliefsSpec(),
		argusIngestGitSpec(),
		argusIngestLogsSpec(),
		// T012 — the codex telemetry jobs.
		codexChainIngestSpec(),
		codexUsageFeedSpec(),
		// spec 085 US1 — the hourly operator heartbeat.
		operatorHeartbeatSpec(),
		// spec 085 US2 — the daily operator telemetry digest.
		operatorDigestSpec(),
	}
}

// EnsureSchedules creates a Temporal Schedule for every JobSpec in the
// Registry, at worker-host startup. It is IDEMPOTENT (spec 081 US2): a job
// whose Schedule already exists is treated as success — a restarted worker
// host re-runs EnsureSchedules every boot and must not fail or duplicate.
//
// The invariant: after EnsureSchedules returns nil, every Registry job has
// exactly one Temporal Schedule registered under its ScheduleID, and no
// Schedule was created twice. An already-exists error from Create
// (temporal.ErrScheduleAlreadyRunning, or a serviceerror.AlreadyExists /
// *serviceerror.WorkflowExecutionAlreadyStarted underneath) is the expected
// steady-state outcome and is NOT propagated.
//
// Any other Create error IS returned — the caller logs it and continues
// (the worker host must come up even if a Schedule cannot be registered;
// the systemd-timer retirement is gated on the Schedule being proven, so a
// missing Schedule is visible, not silent).
func EnsureSchedules(ctx context.Context, c client.Client) error {
	for _, spec := range Registry() {
		if err := ensureOne(ctx, c, spec); err != nil {
			return fmt.Errorf("ensuring schedule for job %q: %w", spec.Name, err)
		}
	}
	return nil
}

// ensureOne creates the Temporal Schedule for a single JobSpec, treating an
// already-exists outcome as success.
func ensureOne(ctx context.Context, c client.Client, spec JobSpec) error {
	_, err := c.ScheduleClient().Create(ctx, client.ScheduleOptions{
		ID: spec.ScheduleID(),
		Spec: client.ScheduleSpec{
			CronExpressions: []string{spec.Cron},
			TimeZoneName:    spec.TimeZone,
		},
		Action: &client.ScheduleWorkflowAction{
			ID: spec.ScheduleID(),
			// Reference the workflow by its registered type name — not the
			// function symbol — so this package never imports workflows.
			Workflow:  ScheduledJobWorkflowName,
			Args:      []any{spec},
			TaskQueue: TaskQueue,
		},
		// SKIP: never run a job concurrently with itself — a migrated cron is
		// read-mostly and idempotent per cycle, not per overlap (spec 081
		// FR-007).
		Overlap: enumspb.SCHEDULE_OVERLAP_POLICY_SKIP,
		Note:    spec.Description,
	})
	if err != nil {
		if isAlreadyExists(err) {
			// The Schedule is already registered — the steady state on every
			// worker-host restart. Idempotent success, not a failure.
			log.Printf("schedules: schedule %q already exists — leaving it in place", spec.ScheduleID())
			return nil
		}
		return err
	}
	log.Printf("schedules: created schedule %q (cron %q %s) for job %q",
		spec.ScheduleID(), spec.Cron, tzOrUTC(spec.TimeZone), spec.Name)
	return nil
}

// isAlreadyExists reports whether err means the Schedule already exists — the
// idempotent-success case. The Temporal Go SDK returns the sentinel
// temporal.ErrScheduleAlreadyRunning from ScheduleClient.Create for a
// duplicate ID; older/other paths may surface a serviceerror.AlreadyExists or
// a *serviceerror.WorkflowExecutionAlreadyStarted. All three are accepted so
// EnsureSchedules never fails startup on a re-registration.
func isAlreadyExists(err error) bool {
	if errors.Is(err, temporal.ErrScheduleAlreadyRunning) {
		return true
	}
	var alreadyExists *serviceerror.AlreadyExists
	if errors.As(err, &alreadyExists) {
		return true
	}
	var alreadyStarted *serviceerror.WorkflowExecutionAlreadyStarted
	return errors.As(err, &alreadyStarted)
}

// tzOrUTC renders a time zone for a log line, naming UTC when the JobSpec
// left TimeZone empty.
func tzOrUTC(tz string) string {
	if tz == "" {
		return "UTC"
	}
	return tz
}
