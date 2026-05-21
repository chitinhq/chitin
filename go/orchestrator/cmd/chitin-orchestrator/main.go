// Command chitin-orchestrator is the Temporal worker host for the Chitin
// Orchestrator (spec 070-chitin-orchestrator).
//
// It dials the Temporal service, registers every workflow and activity, and
// polls the "chitin" task queue. One former cron/script becomes one durable
// workflow; this binary is the single process that runs them all.
package main

import (
	"log"
	"os"
	"path/filepath"

	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"

	"github.com/chitinhq/chitin/go/orchestrator/activities"
	"github.com/chitinhq/chitin/go/orchestrator/driver"
	"github.com/chitinhq/chitin/go/orchestrator/driver/claudecode"
	"github.com/chitinhq/chitin/go/orchestrator/driver/codex"
	"github.com/chitinhq/chitin/go/orchestrator/driver/hermes"
	"github.com/chitinhq/chitin/go/orchestrator/driver/local"
	"github.com/chitinhq/chitin/go/orchestrator/driver/openclaw"
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

	w := worker.New(c, TaskQueue, worker.Options{})
	workflows.Register(w)
	activities.Register(w)
	activities.RegisterSchedulerActivities(w, activities.SchedulerActivityDeps{
		Registry:  registry,
		Worktrees: worktrees,
	})

	log.Printf("chitin-orchestrator: worker host up — task queue %q at %s — %d drivers, worktrees at %s",
		TaskQueue, hostPort, registry.Len(), worktreeRoot)
	if err := w.Run(worker.InterruptCh()); err != nil {
		log.Fatalf("chitin-orchestrator: worker stopped: %v", err)
	}
}
