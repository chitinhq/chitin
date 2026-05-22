// Command chitin-orchestrator is the Temporal worker host for the Chitin
// Orchestrator (spec 070-chitin-orchestrator).
//
// It dials the Temporal service, registers every workflow and activity, and
// polls the "chitin" task queue. One former cron/script becomes one durable
// workflow; this binary is the single process that runs them all.
package main

import (
	"context"
	"log"
	"os"
	"path/filepath"

	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"

	"github.com/chitinhq/chitin/go/orchestrator/activities"
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

func main() {
	hostPort := os.Getenv("TEMPORAL_HOSTPORT")
	if hostPort == "" {
		hostPort = client.DefaultHostPort // 127.0.0.1:7233
	}

	c, err := client.Dial(client.Options{HostPort: hostPort})
	if err != nil {
		log.Fatalf("chitin-orchestrator: cannot reach Temporal at %s: %v", hostPort, err)
	}
	defer c.Close()

	// Build the spec-075 driver registry and register the concrete agent
	// drivers. Registration is a startup-time act; the registry is
	// read-only once the worker host is up.
	registry := driver.NewRegistry()
	for _, d := range []driver.AgentDriver{
		claudecode.New(),
		codex.New(),
		copilot.New(),
		gemini.New(),
		hermes.New(),
		openclaw.New(),
		local.New(),
	} {
		if err := registry.Register(d); err != nil {
			log.Fatalf("chitin-orchestrator: registering driver %q: %v", d.ID(), err)
		}
	}

	// Build the spec-070 worktree Manager — every work unit gets a fresh
	// worktree under this root.
	worktreeRoot := os.Getenv("CHITIN_WORKTREE_ROOT")
	if worktreeRoot == "" {
		worktreeRoot = filepath.Join(os.TempDir(), "chitin-worktrees")
	}
	worktrees, err := worktree.NewManager(worktreeRoot)
	if err != nil {
		log.Fatalf("chitin-orchestrator: cannot build worktree manager at %s: %v", worktreeRoot, err)
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
		Registry:  registry,
		Worktrees: worktrees,
		Telemetry: telemetrySink,
		Notifier:  notifier,
	})

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
	if err := schedules.EnsureSchedules(context.Background(), c); err != nil {
		log.Printf("chitin-orchestrator: ensuring Temporal Schedules: %v", err)
	}

	log.Printf("chitin-orchestrator: worker host up — task queue %q at %s — %d drivers, worktrees at %s",
		TaskQueue, hostPort, registry.Len(), worktreeRoot)
	if err := w.Run(worker.InterruptCh()); err != nil {
		log.Fatalf("chitin-orchestrator: worker stopped: %v", err)
	}
}
