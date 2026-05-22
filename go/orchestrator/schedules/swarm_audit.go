package schedules

import "os"

// swarmAuditSpec is the JobSpec for the daily swarm audit — the first cron
// migrated to a Temporal Schedule (spec 081 US2, task T009).
//
// Source of truth — the retired swarm/systemd/swarm-audit.timer:
//
//	OnCalendar=*-*-* 08:00:00 America/Detroit
//
// which is the cron expression "0 8 * * *" evaluated in America/Detroit. The
// retired swarm-audit.service ran ExecStart=%h/.local/bin/swarm-audit; the
// migration runs that same installed script in the RunScheduledJob activity —
// only the trigger moves from a systemd timer to a Temporal Schedule.
func swarmAuditSpec() JobSpec {
	return JobSpec{
		Name: "swarm-audit",
		// The installed location of swarm/bin/swarm-audit — the same path the
		// retired swarm-audit.service ran (ExecStart=%h/.local/bin/swarm-audit,
		// where %h is the operator's home).
		Command:     os.ExpandEnv("$HOME/.local/bin/swarm-audit"),
		Args:        nil,
		Cron:        "0 8 * * *",
		TimeZone:    "America/Detroit",
		Description: "daily swarm audit",
	}
}
