package schedules

import "os"

// operatorDigestSpec is the JobSpec for the daily operator telemetry digest
// (spec 085 US2). The Temporal Schedule triggers the report delivery script,
// which composes the digest via `chitin-kernel report digest` and posts it to
// the operator's Discord.
//
// The cadence is once daily at 08:00 America/Detroit — a morning wrap-up,
// matching the swarm-audit slot. The operator can also request a digest
// off-schedule via `deliver-operator-report.sh digest --on-demand`.
//
// The Command points at the tracked repo script, not an install symlink — the
// same dangling-symlink lesson swarmAuditSpec records.
func operatorDigestSpec() JobSpec {
	return JobSpec{
		Name:        "operator-digest",
		Command:     os.ExpandEnv("$HOME/workspace/chitin/swarm/bin/deliver-operator-report.sh"),
		Args:        []string{"digest"},
		Cron:        "0 8 * * *",
		TimeZone:    "America/Detroit",
		Description: "operator telemetry digest — daily orchestration/kernel/driver/PR wrap-up to Discord (spec 085 US2)",
	}
}

// OperatorQueueDigestWorkflowName is the registered Temporal type name of the
// workflow operatorQueueDigestSpec triggers. The schedules package references
// it by string so it never imports workflows; the workflows package registers
// OperatorQueueDigestWorkflow under exactly this name.
const OperatorQueueDigestWorkflowName = "OperatorQueueDigestWorkflow"

// operatorQueueDigestSpec is the JobSpec for the daily operator PR-queue
// digest (spec 114 US2, FR-009). At 09:00 America/Detroit it triggers the
// OperatorQueueDigestWorkflow, which renders `queue --since 24h --format md`
// IN-PROCESS — not via a subprocess hop — and posts the rendered markdown to
// the operator's Discord through the same DiscordNotify activity that
// surfaces single-event escalations (spec 080).
//
// The Command and Args fields stay empty: this is the first JobSpec that
// names a purpose-built Workflow instead of falling back to the generic
// subprocess-runner. The 09:00 cadence is one hour after the spec 085
// telemetry digest so the two morning posts arrive in order: kernel/driver
// health first, then the PR queue that needs operator attention.
func operatorQueueDigestSpec() JobSpec {
	return JobSpec{
		Name:        "operator-queue-digest",
		Cron:        "0 9 * * *",
		TimeZone:    "America/Detroit",
		Description: "operator PR-queue digest — daily \"what needs operator attention\" markdown to Discord (spec 114 US2)",
		Workflow:    OperatorQueueDigestWorkflowName,
	}
}
