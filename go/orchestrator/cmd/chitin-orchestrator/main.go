// Command chitin-orchestrator is the Temporal worker host for the Chitin
// Orchestrator (spec 070-chitin-orchestrator).
//
// It dials the Temporal service, registers every workflow and activity, and
// polls the "chitin" task queue. One former cron/script becomes one durable
// workflow; this binary is the single process that runs them all.
//
// Spec 097 added three operator-facing subcommands on the same binary —
// `schedule`, `status`, `cancel` — that take a spec ref, compile it through
// the spec-077 adapter, and start / inspect / cancel the SchedulerWorkflow
// (spec 076). When invoked with no subcommand (or any argv shape that does
// not match a known subcommand) the binary runs the worker-host path the
// chitin-orchestrator.service systemd unit has always relied on; the
// dispatcher is purely additive.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"

	"github.com/chitinhq/chitin/go/orchestrator/activities"
	"github.com/chitinhq/chitin/go/orchestrator/activities/review"
	"github.com/chitinhq/chitin/go/orchestrator/driver"
	"github.com/chitinhq/chitin/go/orchestrator/driver/claudecode"
	"github.com/chitinhq/chitin/go/orchestrator/driver/codex"
	"github.com/chitinhq/chitin/go/orchestrator/driver/copilot"
	"github.com/chitinhq/chitin/go/orchestrator/driver/gemini"
	"github.com/chitinhq/chitin/go/orchestrator/driver/hermes"
	"github.com/chitinhq/chitin/go/orchestrator/driver/local"
	"github.com/chitinhq/chitin/go/orchestrator/driver/openclaw"
	"github.com/chitinhq/chitin/go/orchestrator/ingest"
	"github.com/chitinhq/chitin/go/orchestrator/loop"
	"github.com/chitinhq/chitin/go/orchestrator/schedules"
	"github.com/chitinhq/chitin/go/orchestrator/workflows"
	"github.com/chitinhq/chitin/go/orchestrator/worktree"
)

// TaskQueue is the single task queue the orchestrator polls.
const TaskQueue = "chitin"

// Exit codes for subcommand handlers — spec 097 FR-011.
const (
	exitSuccess      = 0
	exitUserError    = 1 // bad ref, ambiguous ref, missing artifact, terminal-state cancel
	exitRuntimeError = 2 // Temporal unreachable, IO failure, kernel-binary missing
)

func main() {
	os.Exit(runMain(os.Args))
}

// runMain is the testable entry point — caller-provided argv so tests can
// invoke the subcommand handlers without spawning a process. Returns the
// process exit code rather than calling os.Exit directly.
func runMain(args []string) int {
	if len(args) >= 2 {
		switch args[1] {
		case "schedule":
			return cmdSchedule(args[2:])
		case "status":
			return cmdStatus(args[2:])
		case "cancel":
			return cmdCancel(args[2:])
		case "factory-listen":
			return cmdFactoryListen(args[2:])
		case "simulate-webhook":
			return cmdSimulateWebhook(args[2:])
		case "pr-review":
			return cmdPRReview(args[2:])
		case "validate-driver-coverage":
			return cmdValidateDriverCoverage(args[2:])
		case "tasks-lint":
			return cmdTasksLint(args[2:])
		case "queue":
			return cmdQueue(args[2:])
		case "spec-lint":
			return cmdSpecLint(args[2:])
		case "-h", "--help", "help":
			printUsage(os.Stderr)
			return exitSuccess
		}
	}
	// No recognized subcommand → run the worker host the systemd unit
	// has always invoked. Argv shape preserved byte-identically.
	return runWorkerHost(context.Background())
}

// runWorkerHost is the existing worker-host entry point, factored out so the
// subcommand dispatcher in runMain can route to it as the no-args default.
// Behavior is byte-identical to the pre-spec-097 main: dial Temporal, build
// the driver registry, register workflows + activities, run the worker.
//
// Returns exitRuntimeError on any fatal startup error (factored from the
// previous log.Fatalf path — fatal still logs, but bubbles the exit code
// through runMain rather than calling os.Exit directly).
func runWorkerHost(ctx context.Context) int {
	hostPort := os.Getenv("TEMPORAL_HOSTPORT")
	if hostPort == "" {
		hostPort = client.DefaultHostPort // 127.0.0.1:7233
	}

	c, err := client.Dial(client.Options{HostPort: hostPort})
	if err != nil {
		log.Printf("chitin-orchestrator: cannot reach Temporal at %s: %v", hostPort, err)
		return exitRuntimeError
	}
	defer c.Close()

	// Build the spec-075 driver registry and register the concrete agent
	// drivers. Registration is a startup-time act; the registry is
	// read-only once the worker host is up.
	implRegistry, err := buildRegistry("impl")
	if err != nil {
		log.Printf("chitin-orchestrator: building impl driver registry: %v", err)
		return exitRuntimeError
	}
	reviewRegistry, err := buildRegistry("review")
	if err != nil {
		log.Printf("chitin-orchestrator: building review driver registry: %v", err)
		return exitRuntimeError
	}

	// Build the spec-070 worktree Manager — every work unit gets a fresh
	// worktree under this root.
	worktreeRoot := os.Getenv("CHITIN_WORKTREE_ROOT")
	if worktreeRoot == "" {
		worktreeRoot = filepath.Join(os.TempDir(), "chitin-worktrees")
	}
	worktrees, err := worktree.NewManager(worktreeRoot)
	if err != nil {
		log.Printf("chitin-orchestrator: cannot build worktree manager at %s: %v", worktreeRoot, err)
		return exitRuntimeError
	}

	// Build the spec-070 FR-008 telemetry sink — the OTLP/HTTP exporter for
	// per-tick scheduler telemetry. It is a no-op when no collector is
	// configured (OTEL_EXPORTER_OTLP_* unset); Emit then logs and drops.
	telemetrySink := activities.NewOTLPTickTelemetrySinkFromEnv()

	// Build the spec-080 human notification surface — the Discord webhook
	// notifier. It is write-only and no-ops when CHITIN_DISCORD_WEBHOOK_URL is
	// unset, so the orchestrator runs notification-disabled rather than failing.
	notifier := activities.NewDiscordNotifierFromEnv()

	w := worker.New(c, TaskQueue, worker.Options{})
	workflows.Register(w)
	activities.Register(w)
	activities.RegisterSchedulerActivities(w, activities.SchedulerActivityDeps{
		Registry:  implRegistry,
		Worktrees: worktrees,
		Telemetry: telemetrySink,
		Notifier:  notifier,
	})

	// Register the spec-094 dialectic review activities — SelectReviewers,
	// CapturePRSnapshot, DispatchMachineReviewer, EmitReviewTelemetry. The
	// PRReviewWorkflow itself is registered by workflows.Register above.
	// Gh is left nil so CapturePRSnapshot uses the default real `gh` CLI
	// shell-out (production path).
	if err := review.Register(w, review.RegisterDeps{Registry: reviewRegistry}); err != nil {
		log.Printf("chitin-orchestrator: registering dialectic review activities: %v", err)
		return exitRuntimeError
	}

	// Register the spec-078 self-improvement loop — the ImprovementLoopWorkflow
	// and its IngestTelemetry / ProjectProposalQueue activities. Every loop.Deps
	// field is optional and degrades to a safe log-based default: a nil Readers
	// slice yields empty cycles (every telemetry layer unreachable) and a nil
	// ProposalQueue logs each proposal. main supplies the concrete telemetry
	// readers and the proposal-queue sink once those surfaces exist.
	loop.Register(w, loop.Deps{
		Readers:       nil,
		ProposalQueue: nil,
	})

	// Register the spec-079 ingestion pipeline — the IngestionWorkflow and its
	// FetchAndRead / SurfaceKnowledgeItem activities. Every ingest.RegisterDeps
	// field is optional and degrades to a documented dev fallback: a nil Egress
	// gets the development allow-all gate, a nil KnowledgeBase logs surfaced
	// items. Production must bind the real kernel egress gate and knowledge base
	// (ingest/fetch.go, ingest/knowledge_base.go TODOs).
	ingest.Register(w, ingest.RegisterDeps{
		Egress:        nil,
		HTTP:          nil,
		KnowledgeBase: nil,
	})

	// Register the spec-081 US2 Schedule-backed cron migrations — create a
	// Temporal Schedule for every migrated job (currently swarm-audit). This
	// is IDEMPOTENT: a Schedule that already exists is left in place, so a
	// restarted worker host re-runs EnsureSchedules every boot harmlessly.
	// A failure here is logged, not fatal — the worker host must come up even
	// if a Schedule cannot be registered; a missing Schedule is visible (the
	// systemd-timer retirement is gated on the Schedule being proven), never
	// silent.
	if err := schedules.EnsureSchedules(ctx, c); err != nil {
		log.Printf("chitin-orchestrator: ensuring Temporal Schedules: %v", err)
	}

	log.Printf("chitin-orchestrator: worker host up — task queue %q at %s — %d impl drivers, %d review drivers, worktrees at %s",
		TaskQueue, hostPort, implRegistry.Len(), reviewRegistry.Len(), worktreeRoot)
	if err := w.Run(worker.InterruptCh()); err != nil {
		log.Printf("chitin-orchestrator: worker stopped: %v", err)
		return exitRuntimeError
	}
	return exitSuccess
}

// buildRegistry constructs the spec-075 driver registry for one driver
// selection role. Drift prevention per spec 097 plan.md R2 still applies
// within each role: the SelectDriver implementation path and the schedule
// subcommand's pre-validation both use role "impl"; dialectic review
// activities use role "review".
//
// Optional filters are comma-or-space separated driver IDs. Role-specific
// env wins first (`CHITIN_DRIVER_ALLOW_IMPL` or `CHITIN_DRIVER_ALLOW_REVIEW`);
// if that role env is unset, `CHITIN_DRIVER_ALLOW` is the backward-compatible
// fallback. Empty / unset resolved input = all drivers register.
func buildRegistry(role string) (*driver.Registry, error) {
	allowEnv, err := driverAllowEnvForRole(role)
	if err != nil {
		return nil, err
	}
	allowSet := parseDriverAllowEnv(allowEnv)
	// CHITIN_CODEX_MODEL overrides the codex driver's default model
	// (which is hard-coded to "gpt-5.x-codex" in driver/codex/driver.go).
	// Some operator accounts can't reach that model (e.g. ChatGPT-account
	// codex CLI rejects gpt-5.x-codex but accepts gpt-5.5). Empty / unset
	// preserves the driver default.
	codexOpts := []codex.Option{}
	if m := os.Getenv("CHITIN_CODEX_MODEL"); m != "" {
		codexOpts = append(codexOpts, codex.WithModel(m))
	}
	registry := driver.NewRegistry()
	for _, d := range []driver.AgentDriver{
		claudecode.New(),
		codex.New(codexOpts...),
		copilot.New(),
		gemini.New(),
		hermes.New(),
		openclaw.New(),
		local.New(),
	} {
		if len(allowSet) > 0 && !allowSet[d.ID()] {
			continue
		}
		if err := registry.Register(d); err != nil {
			return nil, fmt.Errorf("registering driver %q: %w", d.ID(), err)
		}
	}
	return registry, nil
}

func driverAllowEnvForRole(role string) (string, error) {
	var roleEnv string
	switch role {
	case "impl":
		roleEnv = "CHITIN_DRIVER_ALLOW_IMPL"
	case "review":
		roleEnv = "CHITIN_DRIVER_ALLOW_REVIEW"
	default:
		return "", fmt.Errorf("unknown driver registry role %q (want impl or review)", role)
	}
	if v, ok := os.LookupEnv(roleEnv); ok {
		return v, nil
	}
	return os.Getenv("CHITIN_DRIVER_ALLOW"), nil
}

// parseDriverAllowEnv tokenizes driver allowlist env values. Accepts comma
// or whitespace separators so the operator can write either
// `codex,copilot` or `codex copilot`. Empty input returns an empty
// map (= no filter applied).
func parseDriverAllowEnv(s string) map[string]bool {
	out := map[string]bool{}
	for _, tok := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' }) {
		if tok != "" {
			out[tok] = true
		}
	}
	return out
}

// printUsage writes a one-screen reference of the binary's invocation modes
// to the given writer. It is invoked by `chitin-orchestrator help`,
// `--help`, or `-h`.
func printUsage(w *os.File) {
	fmt.Fprintln(w, `chitin-orchestrator — Temporal worker host + operator CLI for the spec-DAG scheduler

USAGE
  chitin-orchestrator                              # run the worker host (default; what chitin-orchestrator.service invokes)
  chitin-orchestrator schedule <spec-ref> [opts]   # compile a spec and start a SchedulerWorkflow run
  chitin-orchestrator status [-run-id <id>] [--text]    # list active runs OR inspect one
  chitin-orchestrator cancel -run-id <id> [-reason <text>]  # cancel a running scheduler
  chitin-orchestrator factory-listen [opts]                 # run the webhook trigger surface (spec 098)
  chitin-orchestrator simulate-webhook --spec-ref <ref>     # POST a synthetic push at the local listener
  chitin-orchestrator pr-review <PR#> [opts]       # dispatch a dialectic review for a GitHub PR (spec 094)
  chitin-orchestrator tasks-lint <spec-ref> [opts] # validate tasks.md capability classification
  chitin-orchestrator queue [opts]                 # show open PRs that need operator attention (spec 114)
  chitin-orchestrator spec-lint <spec-dir> [opts]  # run the spec PR consistency linter L01-L07 (spec 115)

ENVIRONMENT
  TEMPORAL_HOSTPORT                Temporal frontend (default 127.0.0.1:7233)
  CHITIN_REPO_ROOT                 Default repo root for the schedule subcommand
  CHITIN_REPO                      Default OWNER/NAME for the queue subcommand
  CHITIN_KERNEL_BIN                Path to chitin-kernel (for chain emit; defaults to PATH lookup)
  CHITIN_WORKTREE_ROOT             Worktree root for the worker host (default /tmp/chitin-worktrees)

See specs/097-operator-scheduler-entrypoint/ for the contract.`)
}
