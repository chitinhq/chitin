package schedules

import "os"

// architectureAuditSpec is the JobSpec for the weekly architecture audit —
// migrated to a Temporal Schedule by spec 081 US2, task T010.
//
// Source of truth — the retired swarm/systemd/architecture-audit.timer:
//
//	OnCalendar=Sun *-*-* 06:00:00 America/Detroit
//
// which is the cron expression "0 6 * * 0" evaluated in America/Detroit (cron
// day-of-week 0 = Sunday). The retired architecture-audit.service ran
// ExecStart=%h/.local/bin/architecture-audit; the migration runs that same
// installed script in the RunScheduledJob activity — only the trigger moves
// from a systemd timer to a Temporal Schedule.
func architectureAuditSpec() JobSpec {
	return JobSpec{
		Name: "architecture-audit",
		// The installed location of swarm/bin/architecture-audit — the same
		// path the retired architecture-audit.service ran
		// (ExecStart=%h/.local/bin/architecture-audit, where %h is the
		// operator's home).
		Command:     os.ExpandEnv("$HOME/.local/bin/architecture-audit"),
		Args:        nil,
		Cron:        "0 6 * * 0",
		TimeZone:    "America/Detroit",
		Description: "weekly architecture audit — Sunday 6am ET",
	}
}
