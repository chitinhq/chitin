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
// script in the RunScheduledJob activity — only the trigger moves from a
// systemd timer to a Temporal Schedule.
//
// The Command points at the tracked repo script rather than the
// %h/.local/bin install symlink. That symlink currently resolves (it points
// straight at swarm/bin/architecture-audit), but a %h/.local/bin entry is
// just a pointer that can be left dangling by a culled worktree — as loop
// verification found for the sibling swarm-audit symlink. Naming the repo
// source directly removes that whole failure mode.
func architectureAuditSpec() JobSpec {
	return JobSpec{
		Name: "architecture-audit",
		// The tracked repo script (#!/usr/bin/env bash, executable). The
		// retired architecture-audit.service ran
		// ExecStart=%h/.local/bin/architecture-audit, which is a symlink to
		// exactly this file; the migration runs the source directly so the
		// command path cannot dangle.
		Command:     os.ExpandEnv("$HOME/workspace/chitin/swarm/bin/architecture-audit"),
		Args:        nil,
		Cron:        "0 6 * * 0",
		TimeZone:    "America/Detroit",
		Description: "weekly architecture audit — Sunday 6am ET",
	}
}
