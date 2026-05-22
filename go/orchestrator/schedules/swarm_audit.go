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
// migration runs that same script in the RunScheduledJob activity — only the
// trigger moves from a systemd timer to a Temporal Schedule.
//
// The Command points at the tracked repo script, not the %h/.local/bin
// install symlink: loop verification found that symlink dangling (it pointed
// into a deleted git worktree, chitin-swarm-audit/), so a triggered run exited
// -1 — the binary could not start. The repo path swarm/bin/swarm-audit is the
// real, executable source the install symlink was only ever a pointer to.
func swarmAuditSpec() JobSpec {
	return JobSpec{
		Name: "swarm-audit",
		// The tracked repo script (#!/usr/bin/env python3, executable). The
		// retired swarm-audit.service ran ExecStart=%h/.local/bin/swarm-audit,
		// but that path is a symlink into a worktree that no longer exists;
		// the migration runs the source directly so it cannot dangle.
		Command:     os.ExpandEnv("$HOME/workspace/chitin/swarm/bin/swarm-audit"),
		Args:        nil,
		Cron:        "0 8 * * *",
		TimeZone:    "America/Detroit",
		Description: "daily swarm audit",
	}
}
