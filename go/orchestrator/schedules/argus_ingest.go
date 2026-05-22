package schedules

import "os"

// The three Argus telemetry ingesters — migrated to Temporal Schedules by spec
// 081 US2, task T011. They are interval-based, read-mostly ingesters: each
// snapshots a source (agent beliefs, git history, structured logs) into the
// Argus store on a fixed cadence.
//
// Their systemd units (~/.config/systemd/user/argus-ingest-*.{service,timer})
// are NOT tracked in this repo — they are installed-only. The migrating change
// therefore cannot `git rm` them; the operator MUST disable the installed
// timers at deploy so each job does not run from both a timer and a Schedule
// (spec 081 FR-006).
//
// The retired timers fire on a fixed interval, not a wall-clock calendar:
//
//	argus-ingest-beliefs.timer  OnUnitActiveSec=30min  → cron "*/30 * * * *"
//	argus-ingest-git.timer      OnUnitActiveSec=10min  → cron "*/10 * * * *"
//	argus-ingest-logs.timer     OnUnitActiveSec=2min   → cron "*/2 * * * *"
//
// A fixed-interval timer has no calendar anchor, so the cron expression is the
// closest faithful equivalent: a step over the minute field. A pure interval
// drifts from the wall clock; the cron form pins the cadence to clock minutes,
// which for an idempotent read-mostly ingester is an acceptable, more
// inspectable cadence. UTC is used — these jobs have no calendar dependence,
// so the evaluation zone does not affect their behavior.
//
// Command path: the retired .service units ran ExecStart=%h/.local/bin/argus.
// That install symlink resolves not to a repo script but to a uv tool install
// (~/.local/share/uv/tools/chitin-argus/bin/argus) — argus ships as the
// chitin-argus Python package, not a standalone file. A %h/.local/bin pointer
// can also be left dangling by a culled worktree, the failure loop
// verification caught for the sibling swarm-audit symlink. So each Command
// runs argus from its tracked repo source, python/argus/, via
// `uv run --project <repo>/python/argus argus …` — reproducible, no install
// symlink, and the same console-script entry point (argus.cli:main) the
// retired units invoked.

// argusIngestBeliefsSpec is the JobSpec for the Argus beliefs ingester (agent
// cards + clawta_elo), formerly argus-ingest-beliefs.timer (every 30 minutes).
func argusIngestBeliefsSpec() JobSpec {
	return JobSpec{
		Name: "argus-ingest-beliefs",
		// Retired unit: ExecStart=%h/.local/bin/argus ingest-beliefs.
		// Run the tracked repo package via uv instead of the install symlink.
		Command:     os.ExpandEnv("$HOME/.local/bin/uv"),
		Args:        []string{"run", "--project", os.ExpandEnv("$HOME/workspace/chitin/python/argus"), "argus", "ingest-beliefs"},
		Cron:        "*/30 * * * *",
		TimeZone:    "",
		Description: "Argus beliefs ingester (agent cards + clawta_elo)",
	}
}

// argusIngestGitSpec is the JobSpec for the Argus git ingester (read-only
// snapshot of commits + PR metadata), formerly argus-ingest-git.timer (every
// 10 minutes).
func argusIngestGitSpec() JobSpec {
	return JobSpec{
		Name: "argus-ingest-git",
		// Retired unit: ExecStart=%h/.local/bin/argus ingest-git --root %h/workspace.
		// Run the tracked repo package via uv instead of the install symlink.
		Command: os.ExpandEnv("$HOME/.local/bin/uv"),
		Args: []string{
			"run", "--project", os.ExpandEnv("$HOME/workspace/chitin/python/argus"),
			"argus", "ingest-git", "--root", os.ExpandEnv("$HOME/workspace"),
		},
		Cron:        "*/10 * * * *",
		TimeZone:    "",
		Description: "Argus git ingester (read-only snapshot of commits + PR metadata)",
	}
}

// argusIngestLogsSpec is the JobSpec for the Argus log ingester (hermes +
// openclaw structured logs), formerly argus-ingest-logs.timer (every 2
// minutes, tail-and-checkpoint).
func argusIngestLogsSpec() JobSpec {
	return JobSpec{
		Name: "argus-ingest-logs",
		// Retired unit: ExecStart=%h/.local/bin/argus ingest-logs.
		// Run the tracked repo package via uv instead of the install symlink.
		Command:     os.ExpandEnv("$HOME/.local/bin/uv"),
		Args:        []string{"run", "--project", os.ExpandEnv("$HOME/workspace/chitin/python/argus"), "argus", "ingest-logs"},
		Cron:        "*/2 * * * *",
		TimeZone:    "",
		Description: "Argus log ingester (hermes + openclaw structured logs)",
	}
}
