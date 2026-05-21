package workflows

import (
	"fmt"
	"time"

	"go.temporal.io/sdk/log"
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
//
// For a NodeKindDeterministic node DriverID is empty: the work unit runs the
// node's mechanical command via the RunDeterministicStep activity instead of
// invoking a driver (spec 076 FR-017). The worktree → run → teardown shape is
// identical; only the middle leg differs.
type WorkUnitInput struct {
	// Node is the DAG node this work unit executes. It carries the routing
	// inputs — target repo, base ref, capability, worktree requirement
	// (spec 076 FR-013, Key Entities: DAG Node).
	Node dag.Node `json:"node"`
	// DriverID is the id of the driver the scheduler selected for this node
	// (spec 076 FR-007). The work unit invokes exactly this driver via the
	// per-driver InvokeDriver:<id> activity. It is empty for a
	// NodeKindDeterministic node, which runs a deterministic step instead.
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
// base ref, runs the node's work in that worktree, and tears the worktree
// down — always, success or failure.
//
// The middle leg depends on the node's kind (spec 076 FR-017): a
// NodeKindAgent node invokes the scheduler-selected driver; a
// NodeKindDeterministic node runs its mechanical command via the
// RunDeterministicStep activity — no driver, no token cost. The worktree
// create/teardown shape is identical for both kinds.
//
// Determinism: WorkUnitWorkflow is a Temporal workflow and therefore
// strictly deterministic. It reads no wall clock and performs no I/O
// directly; the worktree create/teardown, the driver invocation, and the
// deterministic step are all activities. Each side effect is exactly-once for
// a given workflow execution: Temporal records each activity's result in
// history, so a replay re-uses the recorded result rather than re-running the
// side effect.
//
// The worktree is torn down via a deferred activity so it is reclaimed even
// when the work fails — a failed work unit never leaks its worktree
// (spec 070 FR-013/14). Teardown is idempotent in the worktree Manager, so
// the deferred call is safe under Temporal's at-least-once activity semantics.
func WorkUnitWorkflow(ctx workflow.Context, in WorkUnitInput) (WorkUnitResult, error) {
	logger := workflow.GetLogger(ctx)
	node := in.Node

	if node.ID == "" {
		return WorkUnitResult{}, temporal.NewNonRetryableApplicationError(
			"work unit has an empty node id", "InvalidWorkUnit", nil)
	}
	// An agent node MUST carry a scheduler-selected driver; a deterministic
	// node MUST NOT — it runs a mechanical step instead (spec 076 FR-017).
	deterministic := node.Kind == dag.NodeKindDeterministic
	if !deterministic && in.DriverID == "" {
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

	// --- run the node's work in the fresh worktree -------------------------
	// A deterministic node runs a mechanical command via RunDeterministicStep;
	// an agent node invokes the scheduler-selected driver (spec 076 FR-017).
	if deterministic {
		return runDeterministicStep(ctx, logger, node, created.Path)
	}
	return invokeDriver(ctx, logger, node, in.DriverID, created.Path)
}

// invokeDriver runs the agent-node middle leg: it invokes the scheduler-
// selected driver in the fresh worktree via the per-driver InvokeDriver:<id>
// activity. A driver fault settles the work unit failed, never crashes it.
func invokeDriver(
	ctx workflow.Context, logger log.Logger,
	node dag.Node, driverID, worktreePath string,
) (WorkUnitResult, error) {
	invokeCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: invokeActivityTimeout,
		HeartbeatTimeout:    2 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts: 1, // an agent invocation is not blindly retried.
		},
	})

	// Context is the work unit's instructions — the driver builds the agent
	// prompt from it. Carry the node's task description (the agent-node
	// analogue of a deterministic node's command); fall back to the spec/task
	// ids so the agent is never invoked with an empty instruction.
	instructions := node.Description
	if instructions == "" {
		instructions = "spec " + node.SpecRef + " task " + node.TaskRef
	}
	wu := driver.WorkUnit{
		ID:           node.ID,
		SpecID:       node.SpecRef,
		TaskID:       node.TaskRef,
		Context:      instructions,
		WorktreePath: worktreePath,
	}

	var res driver.Result
	invokeErr := workflow.ExecuteActivity(invokeCtx, "InvokeDriver:"+driverID, wu).Get(ctx, &res)
	if invokeErr != nil {
		logger.Error("work unit: driver invocation faulted", "node", node.ID,
			"driver", driverID, "err", invokeErr)
		return WorkUnitResult{
			NodeID:      node.ID,
			DriverID:    driverID,
			Succeeded:   false,
			Status:      driver.StatusFailed.String(),
			Explanation: fmt.Sprintf("driver %s invocation faulted: %v", driverID, invokeErr),
		}, nil
	}

	return WorkUnitResult{
		NodeID:      node.ID,
		DriverID:    driverID,
		Succeeded:   res.Status == driver.StatusSucceeded,
		Status:      res.Status.String(),
		OutputRef:   res.OutputRef,
		Explanation: res.Explanation,
	}, nil
}

// runDeterministicStep runs the deterministic-node middle leg: it executes the
// node's mechanical command in the fresh worktree via the RunDeterministicStep
// activity — no driver is selected and no agent tokens are spent
// (spec 076 FR-017). The step's exit code settles the node done or failed,
// identically to an agent node's success or failure (FR-017 scenario 3).
func runDeterministicStep(
	ctx workflow.Context, logger log.Logger,
	node dag.Node, worktreePath string,
) (WorkUnitResult, error) {
	stepCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: invokeActivityTimeout,
		RetryPolicy: &temporal.RetryPolicy{
			// A mechanical step is not blindly retried — a failed gofmt or
			// `go test` is a real result, not a transient fault.
			MaximumAttempts: 1,
		},
	})

	var res activities.DeterministicStepResult
	stepErr := workflow.ExecuteActivity(stepCtx, "RunDeterministicStep",
		activities.DeterministicStepInput{
			NodeID:       node.ID,
			Command:      node.Command,
			Args:         node.Args,
			WorktreePath: worktreePath,
		}).Get(ctx, &res)
	if stepErr != nil {
		logger.Error("work unit: deterministic step faulted", "node", node.ID,
			"command", node.Command, "err", stepErr)
		return WorkUnitResult{
			NodeID:      node.ID,
			Succeeded:   false,
			Status:      driver.StatusFailed.String(),
			Explanation: fmt.Sprintf("deterministic step %q faulted: %v", node.Command, stepErr),
		}, nil
	}

	status := driver.StatusSucceeded
	if !res.Succeeded {
		status = driver.StatusFailed
	}
	return WorkUnitResult{
		NodeID:      node.ID,
		Succeeded:   res.Succeeded,
		Status:      status.String(),
		OutputRef:   res.Output,
		Explanation: res.Explanation,
	}, nil
}
