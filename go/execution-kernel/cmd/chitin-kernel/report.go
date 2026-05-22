package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/chitinhq/chitin/go/execution-kernel/internal/report"
)

// cmdReport dispatches `chitin-kernel report {heartbeat|digest}` — the
// operator-report composition command (spec 085). It only reads telemetry and
// prints the composed message to stdout; it never posts or sends anything, so
// the kernel stays within the Constitution §1 side-effect boundary. Delivery
// is the job of swarm/bin/deliver-operator-report.sh.
func cmdReport(args []string) {
	if len(args) < 1 {
		exitErr("report_no_kind", "usage: chitin-kernel report {heartbeat|digest} [flags]")
	}
	kind, rest := args[0], args[1:]
	switch kind {
	case "heartbeat":
		cmdReportHeartbeat(rest)
	case "digest":
		cmdReportDigest(rest)
	default:
		exitErr("report_unknown_kind", fmt.Sprintf("unknown report kind %q (want heartbeat|digest)", kind))
	}
}

// cmdReportHeartbeat composes the liveness heartbeat (spec 085 US1) and prints
// it. It exits 0 even when components are degraded/unknown — a heartbeat is
// always composable; a non-zero exit is reserved for an internal error that
// prevented composing anything at all.
func cmdReportHeartbeat(args []string) {
	fs := flag.NewFlagSet("report heartbeat", flag.ExitOnError)
	dir := fs.String("dir", defaultChitinStateDir(), "kernel state dir (default: $CHITIN_STATE_DIR, else ~/.chitin)")
	repo := fs.String("repo", "", "chitin source repo (default: $CHITIN_REPO, else discovered from cwd)")
	kernelBin := fs.String("kernel-bin", "", "installed kernel binary to check (default: this executable)")
	gatewayUnit := fs.String("gateway-unit", "openclaw-gateway.service", "systemd --user unit name for the gateway")
	windowHours := fs.Int("window-hours", 1, "agent-activity window in hours")
	fs.Parse(args)

	if *windowHours <= 0 {
		exitErr("report_invalid_window", "--window-hours must be > 0")
	}
	absDir, err := filepath.Abs(*dir)
	if err != nil {
		exitErr("report_abs", err.Error())
	}
	binPath := *kernelBin
	if binPath == "" {
		if exe, exeErr := os.Executable(); exeErr == nil {
			binPath = exe
		}
	}

	hb := report.GatherHeartbeat(report.HeartbeatConfig{
		ChitinDir:   absDir,
		KernelBin:   binPath,
		RepoDir:     resolveKernelRepo(*repo),
		InstallLog:  installKernelLogPath(),
		DeliveryLog: operatorReportLogPath(),
		GatewayUnit: *gatewayUnit,
		Window:      time.Duration(*windowHours) * time.Hour,
	})
	fmt.Println(report.Render(report.HeartbeatMessage(hb), report.DefaultMaxLen))
}

// cmdReportDigest composes the daily telemetry digest (spec 085 US2) and
// prints it. Like the heartbeat it is side-effect-free and exits 0 on a
// partial digest — every section self-degrades rather than aborting.
//
// The orchestration section reads event-pipeline activity (`chitin health`),
// not the kanban, so this command takes no --board flag (Constitution §5
// applies to kanban-touching scripts); a future kanban-backed section would
// add it then.
func cmdReportDigest(args []string) {
	fs := flag.NewFlagSet("report digest", flag.ExitOnError)
	dir := fs.String("dir", defaultChitinStateDir(), "kernel state dir (default: $CHITIN_STATE_DIR, else ~/.chitin)")
	repo := fs.String("repo", "", "chitin source repo (default: $CHITIN_REPO, else discovered from cwd)")
	kernelBin := fs.String("kernel-bin", "", "installed kernel binary to check (default: this executable)")
	windowHours := fs.Int("window-hours", 24, "digest window in hours")
	consoleBase := fs.String("console-base", "", "chitin-console base URL for click-through links")
	onDemand := fs.Bool("on-demand", false, "mark the digest operator-requested rather than scheduled")
	fs.Parse(args)

	if *windowHours <= 0 {
		exitErr("report_invalid_window", "--window-hours must be > 0")
	}
	absDir, err := filepath.Abs(*dir)
	if err != nil {
		exitErr("report_abs", err.Error())
	}
	binPath := *kernelBin
	if binPath == "" {
		if exe, exeErr := os.Executable(); exeErr == nil {
			binPath = exe
		}
	}

	d := report.GatherDigest(report.DigestSources{
		ChitinDir:   absDir,
		KernelBin:   binPath,
		RepoDir:     resolveKernelRepo(*repo),
		InstallLog:  installKernelLogPath(),
		Window:      time.Duration(*windowHours) * time.Hour,
		ConsoleBase: *consoleBase,
	}, *onDemand)
	fmt.Println(report.Render(report.DigestMessage(d), report.DefaultMaxLen))
}

// defaultChitinStateDir is where the kernel keeps its central runtime state —
// the gov-decisions log and event files the reports read. It is $CHITIN_STATE_DIR
// when set, else ~/.chitin. The reports default here rather than to a
// cwd-relative `.chitin` so a heartbeat or digest run from any directory (or by
// the Temporal job) reads the real telemetry, not an empty project-local dir.
func defaultChitinStateDir() string {
	if env := os.Getenv("CHITIN_STATE_DIR"); env != "" {
		return env
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".chitin"
	}
	return filepath.Join(home, ".chitin")
}

// operatorReportLogPath mirrors deliver-operator-report.sh's resolution of the
// delivery audit log: $CHITIN_OPERATOR_REPORT_LOG, else
// ~/.cache/chitin/operator-report.jsonl.
func operatorReportLogPath() string {
	if env := os.Getenv("CHITIN_OPERATOR_REPORT_LOG"); env != "" {
		return env
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".cache", "chitin", "operator-report.jsonl")
}
