package schedules

import "os"

// The spec 081 US3 watchdog / mutation / ops jobs — the higher-blast-radius
// tranche that reuses the US2 Schedule-backed migration pattern. Four have
// tracked infra/systemd units; openclaw-gateway-restart's source of truth is
// spec 036's hourly recovery-doctor invariant plus the tracked executable
// swarm/bin/dispatch-recovery-doctor.sh.
//
// All five cadences are fixed intervals rather than wall-clock calendars:
//
//	chitin-chain-watch         OnUnitActiveSec=1min   → cron "* * * * *"
//	chitin-agent-unlock        OnUnitActiveSec=15min  → cron "*/15 * * * *"
//	chitin-envelope-rotate     OnUnitActiveSec=5min   → cron "*/5 * * * *"
//	chitin-kernel-redeploy     OnUnitActiveSec=15min  → cron "*/15 * * * *"
//	openclaw-gateway-restart   hourly cron            → cron "0 * * * *"
//
// These jobs have no calendar dependence, so UTC is an acceptable evaluation
// zone: the cadence matters, not the wall-clock label.

// chitinChainWatchSpec is the JobSpec for the runaway-lockdown detector,
// formerly infra/systemd/chitin-chain-watch.{service,timer}.
//
// The retired service ran ExecStart=%h/workspace/chitin/scripts/chitin-chain-watch.sh.
// The migration names the tracked repo script directly rather than any
// install-time symlink under ~/.local/bin.
func chitinChainWatchSpec() JobSpec {
	return JobSpec{
		Name:        "chitin-chain-watch",
		Command:     os.ExpandEnv("$HOME/workspace/chitin/scripts/chitin-chain-watch.sh"),
		Args:        nil,
		Cron:        "* * * * *",
		TimeZone:    "",
		Description: "runaway-lockdown detector for the governance chain",
	}
}

// chitinAgentUnlockSpec is the JobSpec for the automated lockdown age-out
// recovery, formerly infra/systemd/chitin-agent-unlock.{service,timer}.
//
// The retired service ran ExecStart=%h/workspace/chitin/scripts/chitin-agent-unlock.sh.
// The migration runs that tracked repo script directly.
func chitinAgentUnlockSpec() JobSpec {
	return JobSpec{
		Name:        "chitin-agent-unlock",
		Command:     os.ExpandEnv("$HOME/workspace/chitin/scripts/chitin-agent-unlock.sh"),
		Args:        nil,
		Cron:        "*/15 * * * *",
		TimeZone:    "",
		Description: "auto-unlock agents locked by infrastructure failures",
	}
}

// chitinEnvelopeRotateSpec is the JobSpec for the budget-envelope rotator,
// formerly infra/systemd/chitin-envelope-rotate.{service,timer}.
//
// The retired service ran ExecStart=%h/workspace/chitin/scripts/chitin-envelope-rotate.sh
// with a WorkingDirectory and PATH/env-file tweaks, but the script itself uses
// only absolute paths / command lookups and the orchestrator worker already
// carries %h/.local/bin on PATH. So the migration runs the tracked repo script
// directly and changes only the trigger.
func chitinEnvelopeRotateSpec() JobSpec {
	return JobSpec{
		Name:        "chitin-envelope-rotate",
		Command:     os.ExpandEnv("$HOME/workspace/chitin/scripts/chitin-envelope-rotate.sh"),
		Args:        nil,
		Cron:        "*/5 * * * *",
		TimeZone:    "",
		Description: "rotate current envelope when the active budget closes",
	}
}

// chitinKernelRedeploySpec is the JobSpec for the kernel redeployer, formerly
// infra/systemd/chitin-kernel-redeploy.{service,timer}.
//
// The retired service ran ExecStart=%h/workspace/chitin/scripts/install-kernel.sh.
// The migration runs that tracked repo script directly so the command path is a
// real on-disk executable, never an install symlink.
func chitinKernelRedeploySpec() JobSpec {
	return JobSpec{
		Name:        "chitin-kernel-redeploy",
		Command:     os.ExpandEnv("$HOME/workspace/chitin/scripts/install-kernel.sh"),
		Args:        nil,
		Cron:        "*/15 * * * *",
		TimeZone:    "",
		Description: "rebuild and reinstall chitin-kernel from main",
	}
}

// openclawGatewayRestartSpec is the JobSpec for the hourly gateway recovery
// doctor, whose invariant is specified in
// .specify/specs/036-dispatch-fault-tolerance-invariants/spec.md.
//
// There is no tracked systemd unit in this repo for this job. The source of
// truth is spec 036's invariant: cron runs the recovery doctor hourly so an
// openclaw-gateway crash self-heals within an hour. The executable is the
// tracked repo script swarm/bin/dispatch-recovery-doctor.sh; the
// --gateway-only flag makes this schedule own just the gateway-restart slice,
// not the stale-branch diagnostics that the full doctor also performs.
func openclawGatewayRestartSpec() JobSpec {
	return JobSpec{
		Name:        "openclaw-gateway-restart",
		Command:     os.ExpandEnv("$HOME/workspace/chitin/swarm/bin/dispatch-recovery-doctor.sh"),
		Args:        []string{"--gateway-only"},
		Cron:        "0 * * * *",
		TimeZone:    "",
		Description: "hourly openclaw gateway recovery doctor",
	}
}
