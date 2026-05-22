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
