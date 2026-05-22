package schedules

import "os"

// The two codex telemetry jobs — migrated to Temporal Schedules by spec 081
// US2, task T012. Both re-mine the codex CLI's session files
// (~/.codex/sessions/) into chitin's analysis surfaces.
//
// Their systemd units (infra/systemd/chitin-codex-*.{service,timer}, symlinked
// into ~/.config/systemd/user/) ARE tracked in this repo; the migrating change
// `git rm`s all four files (FR-010).
//
// Both retired timers fire on a fixed interval, not a wall-clock calendar:
//
//	chitin-codex-chain-ingest.timer  OnUnitActiveSec=1h    → cron "0 * * * *"
//	chitin-codex-usage-feed.timer    OnUnitActiveSec=10min → cron "*/10 * * * *"
//
// A fixed-interval timer has no calendar anchor; the cron expression pins the
// cadence to clock minutes (hourly on the hour; every tenth minute), which for
// these idempotent re-miners is an acceptable, more inspectable cadence. UTC is
// used — neither job has a calendar dependence.
//
// Both retired .service units set a WorkingDirectory and per-job environment
// (PYTHONPATH, PATH) that RunScheduledJob does not replicate — the activity
// runs the command with the worker's inherited os.Environ() and the worker's
// own working directory. So the migration wraps each job's ExecStart in
// `/bin/sh -c`, which re-establishes the cwd and env the systemd unit declared.
// chitin-codex-usage-feed already used a `/bin/sh -c` ExecStart; this faithfully
// carries that same shell line, and chitin-codex-chain-ingest is given the
// matching shell wrapper so its Python module imports resolve.

// codexChainIngestSpec is the JobSpec for the codex chain ingest — pulls the
// codex CLI's chain (~/.codex/sessions/) into chitin's analysis pipeline.
// Formerly chitin-codex-chain-ingest.timer (hourly).
//
// The retired .service ran, with WorkingDirectory=%h/workspace/chitin and
// PYTHONPATH=%h/workspace/chitin/python:
//
//	ExecStart=/usr/bin/python3 -m analysis.codex_mine ingest --out-dir %h/.chitin
//
// The migration reproduces that cwd + PYTHONPATH inside a `/bin/sh -c` wrapper,
// since RunScheduledJob does not set a per-job working directory or env.
func codexChainIngestSpec() JobSpec {
	home := os.ExpandEnv("$HOME")
	return JobSpec{
		Name:    "chitin-codex-chain-ingest",
		Command: "/bin/sh",
		Args: []string{
			"-c",
			"cd " + home + "/workspace/chitin && " +
				"PYTHONPATH=" + home + "/workspace/chitin/python " +
				"/usr/bin/python3 -m analysis.codex_mine ingest --out-dir " + home + "/.chitin",
		},
		Cron:        "0 * * * *",
		TimeZone:    "",
		Description: "Chitin codex chain ingest",
	}
}

// codexUsageFeedSpec is the JobSpec for the codex usage feed — re-mines
// ~/.codex/sessions/**/*.jsonl into the universal usage feed at
// ~/.cache/chitin/usage/codex.json. Formerly chitin-codex-usage-feed.timer
// (every 10 minutes).
//
// The retired .service ran a `/bin/sh -c` ExecStart with
// WorkingDirectory=%h/workspace/chitin; the shell line itself `cd`s into
// python/analysis, so this carries the exact same command.
func codexUsageFeedSpec() JobSpec {
	home := os.ExpandEnv("$HOME")
	return JobSpec{
		Name:    "chitin-codex-usage-feed",
		Command: "/bin/sh",
		Args: []string{
			"-c",
			"mkdir -p " + home + "/.cache/chitin/usage && " +
				"cd " + home + "/workspace/chitin/python/analysis && " +
				"uv run --project . python -m analysis.codex_mine usage " +
				"--write-feed=" + home + "/.cache/chitin/usage/codex.json",
		},
		Cron:        "*/10 * * * *",
		TimeZone:    "",
		Description: "Refresh codex usage feed (rate_limits + call counts)",
	}
}
