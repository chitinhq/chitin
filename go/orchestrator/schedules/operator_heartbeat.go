package schedules

import "os"

// operatorHeartbeatSpec is the JobSpec for the hourly operator heartbeat
// (spec 085 US1). The Temporal Schedule triggers the report delivery script,
// which composes the heartbeat via `chitin-kernel report heartbeat` and posts
// it to the operator's Discord.
//
// The Command points at the tracked repo script, not an install symlink —
// the same dangling-symlink lesson swarmAuditSpec records: a symlink into a
// since-deleted worktree makes a triggered run exit before it starts.
func operatorHeartbeatSpec() JobSpec {
	return JobSpec{
		Name:        "operator-heartbeat",
		Command:     os.ExpandEnv("$HOME/workspace/chitin/swarm/bin/deliver-operator-report.sh"),
		Args:        []string{"heartbeat"},
		Cron:        "0 * * * *",
		TimeZone:    "America/Detroit",
		Description: "operator heartbeat — gateway/kernel/agent liveness to Discord (spec 085 US1)",
	}
}
