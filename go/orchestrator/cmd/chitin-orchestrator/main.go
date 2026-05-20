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

	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"

	"github.com/chitinhq/chitin/go/orchestrator/activities"
	"github.com/chitinhq/chitin/go/orchestrator/workflows"
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

	w := worker.New(c, TaskQueue, worker.Options{})
	workflows.Register(w)
	activities.Register(w)

	log.Printf("chitin-orchestrator: worker host up — task queue %q at %s", TaskQueue, hostPort)
	if err := w.Run(worker.InterruptCh()); err != nil {
		log.Fatalf("chitin-orchestrator: worker stopped: %v", err)
	}
}
