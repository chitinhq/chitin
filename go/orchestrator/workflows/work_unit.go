package workflows

import (
	"fmt"
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/chitinhq/chitin/go/orchestrator/activities"
	"github.com/chitinhq/chitin/go/orchestrator/dag"
	"github.com/chitinhq/chitin/go/orchestrator/driver"
)

// WorkUnitInput is the typed input to WorkUnitWorkflow — one DAG node, plus
// the driver the scheduler already selected for it. The scheduler runs driver
// selection in its own tick (an activity) and passes the result down here, so
// the work unit's only job is the worktree → invoke → teardown lifecycle.
type WorkUnitInput struct {
	// Node is the DAG node this work unit executes. It carries the routing
	// inputs — target repo, base ref, capability, worktree requirement
	// (spec 076 FR-013, Key Entities: DAG Node).
	Node dag.Node `json:"node"`
	// DriverID is the id of the driver the scheduler selected for this node
	// (spec 076 FR-007). The work unit invokes exactly this driver via the
	// per-driver InvokeDriver:<id> activity.
	DriverID string `json:"driver_id"`
	// SchedulerRunID identifies the parent scheduler run, for correlation in
	// telemetry and the chitin chain.
	SchedulerRunID string `json:"scheduler_run_id"`
}

// WorkUnitResult is the typed output of WorkUnitWorkflow — the node id and
// the driver's typed outcome. The scheduler reads Succeeded to decide the
// node's terminal status (done vs failed).
type WorkUnitResult struct {
	// NodeID echoes the executed node, for correlation.
	NodeID string `json:"node_id"`
	// DriverID is the driver that ran the work unit.
	DriverID string `json:"driver_id"`
	// Succeeded is true iff the driver reported StatusSucceeded — the
	// scheduler maps this to dag.StatusDone, and false to dag.StatusFailed.
	Succeeded bool `json:"succeeded"`
	// Status is the driver's typed outcome string (succeeded / failed /
	// timeout / quota_exhausted), carried for telemetry.
	Status string `json:"status"`
	// OutputRef is a reference to the work product — a branch, a PR URL, an
	// artifact path.
	OutputRef string `json:"output_ref"`
	// Explanation is the driver's human-readable account of the outcome.
	Explanation string `json:"explanation"`
}

// workUnitActivityTimeouts are the StartToClose timeouts for each leg of the
// work unit. Worktree create/teardown are short shell-outs; the driver
// invocation is the long leg and heartbeats so its timeout governs liveness.
const (
	worktreeActivityTimeout = 5 * time.Minute
	invokeActivityTimeout   = 2 * time.Hour
)

// WorkUnitWorkflow is the per-node child workflow (spec 076 FR-008): it
// creates a FRESH dedicated git worktree from the node's target repo at its
// base ref, invokes the scheduler-selected driver in that worktree, and tears
// the worktree down — always, success or failure.
//
// Determinism: WorkUnitWorkflow is a Temporal workflow and therefore
// strictly deterministic. It reads no wall clock and performs no I/O
// directly; the worktree create/teardown and the driver invocation are all
// activities. Each side effect is exactly-once for a given workflow
// execution: Temporal records each activity's result in history, so a replay
// re-uses the recorded result rather than re-running the side effect.
//
// The worktree is torn down via a deferred activity so it is reclaimed even
// when the driver invocation fails — a failed work unit never leaks its
// worktree (spec 070 FR-013/14). Teardown is idempotent in the worktree
// Manager, so the deferred call is safe under Temporal's at-least-once
// activity semantics.
func WorkUnitWorkflow(ctx workflow.Context, in WorkUnitInput) (WorkUnitResult, error) {
	logger := workflow.GetLogger(ctx)
	node := in.Node

	if node.ID == "" {
		return WorkUnitResult{}, temporal.NewNonRetryableApplicationError(
			"work unit has an empty node id", "InvalidWorkUnit", nil)
	}
	if in.DriverID == "" {
		return WorkUnitResult{NodeID: node.ID}, temporal.NewNonRetryableApplicationError(
			fmt.Sprintf("work unit %s has no driver selected", node.ID), "InvalidWorkUnit", nil)
	}

	// --- create a fresh worktree -------------------------------------------
	worktreeCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: worktreeActivityTimeout,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts: 3,
		},
	})

	var created activities.CreateWorktreeResult
	createErr := workflow.ExecuteActivity(worktreeCtx, "CreateWorktree", activities.CreateWorktreeInput{
		WorkUnitID: node.ID,
		TargetRepo: node.TargetRepo,
		BaseRef:    node.BaseRef,
	}).Get(ctx, &created)
	if createErr != nil {
		// No worktree was produced — nothing to tear down. Fail the work unit.
		logger.Error("work unit: worktree creation failed", "node", node.ID, "err", createErr)
		return WorkUnitResult{
			NodeID:      node.ID,
			DriverID:    in.DriverID,
			Succeeded:   false,
			Status:      driver.StatusFailed.String(),
			Explanation: fmt.Sprintf("worktree creation failed: %v", createErr),
		}, nil
	}

	// Teardown ALWAYS runs — success or failure — via a deferred activity.
	// A disconnected context lets teardown still run even if ctx is being
	// cancelled; the worktree Manager's Teardown is idempotent.
	defer func() {
		teardownCtx, _ := workflow.NewDisconnectedContext(ctx)
		teardownCtx = workflow.WithActivityOptions(teardownCtx, workflow.ActivityOptions{
			StartToCloseTimeout: worktreeActivityTimeout,
			RetryPolicy: &temporal.RetryPolicy{
				MaximumAttempts: 3,
			},
		})
		if err := workflow.ExecuteActivity(teardownCtx, "TeardownWorktree",
			activities.TeardownWorktreeInput{Path: created.Path},
		).Get(teardownCtx, nil); err != nil {
			// A failed teardown is logged, not fatal — GC reclaims the orphan.
			logger.Error("work unit: worktree teardown failed", "node", node.ID,
				"path", created.Path, "err", err)
		}
	}()

	// --- invoke the selected driver in the fresh worktree ------------------
	invokeCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: invokeActivityTimeout,
		HeartbeatTimeout:    2 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts: 1, // an agent invocation is not blindly retried.
		},
	})

	wu := driver.WorkUnit{
		ID:           node.ID,
		SpecID:       node.SpecRef,
		TaskID:       node.TaskRef,
		Context:      node.SpecRef + " " + node.TaskRef,
		WorktreePath: created.Path,
	}

	var res driver.Result
	invokeErr := workflow.ExecuteActivity(invokeCtx, "InvokeDriver:"+in.DriverID, wu).Get(ctx, &res)
	if invokeErr != nil {
		logger.Error("work unit: driver invocation faulted", "node", node.ID,
			"driver", in.DriverID, "err", invokeErr)
		return WorkUnitResult{
			NodeID:      node.ID,
			DriverID:    in.DriverID,
			Succeeded:   false,
			Status:      driver.StatusFailed.String(),
			Explanation: fmt.Sprintf("driver %s invocation faulted: %v", in.DriverID, invokeErr),
		}, nil
	}

	return WorkUnitResult{
		NodeID:      node.ID,
		DriverID:    in.DriverID,
		Succeeded:   res.Status == driver.StatusSucceeded,
		Status:      res.Status.String(),
		OutputRef:   res.OutputRef,
		Explanation: res.Explanation,
	}, nil
}
