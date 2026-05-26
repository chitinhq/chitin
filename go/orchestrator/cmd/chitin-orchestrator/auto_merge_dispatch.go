package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/sdk/client"

	"github.com/chitinhq/chitin/go/orchestrator/activities"
	"github.com/chitinhq/chitin/go/orchestrator/workflows"
)

type autoMergeDispatchInput struct {
	Repo           string
	PRNumber       int
	LabelName      string
	TriggerEventID string
	ActorLogin     string
	TemporalHost   string
}

type autoMergeDispatchResult struct {
	Dispatched  bool
	WorkflowID  string
	Signaled    bool
	FailureKind string
	Detail      string
}

type workflowSignaler interface {
	SignalWorkflow(ctx context.Context, workflowID string, runID string, signalName string, arg any) error
}

func dispatchAutoMerge(ctx context.Context, in autoMergeDispatchInput, dialer temporalDialer, stderr io.Writer) autoMergeDispatchResult {
	workflowID := fmt.Sprintf("auto-merge-pr-%d-%s", in.PRNumber, in.TriggerEventID)
	if in.Repo == "" || in.PRNumber <= 0 || in.TriggerEventID == "" {
		return autoMergeDispatchResult{WorkflowID: workflowID, FailureKind: "invalid_input"}
	}
	if prior, found, err := findAutoMergeByTrigger("", in.TriggerEventID); err == nil && found {
		activities.EmitAutoMergeChainEvent(ctx, activities.AutoMergeAlreadySettled, workflowID, map[string]any{
			"repo": in.Repo, "pr_number": in.PRNumber, "prior_outcome": prior,
		})
		return autoMergeDispatchResult{WorkflowID: workflowID, FailureKind: "already_settled"}
	} else if err != nil {
		fmt.Fprintf(stderr, "warning: auto-merge chain dedup scan failed (proceeding fail-open): %v\n", err)
	}
	if dialer == nil {
		dialer = dialTemporalAsStarter
	}
	c, host, err := dialer(ctx, in.TemporalHost)
	if err != nil {
		return autoMergeDispatchResult{WorkflowID: workflowID, FailureKind: "temporal_unreachable", Detail: fmt.Sprintf("dial %s: %v", host, err)}
	}
	defer c.Close()
	opts := client.StartWorkflowOptions{
		ID:                    workflowID,
		TaskQueue:             TaskQueue,
		WorkflowIDReusePolicy: enumspb.WORKFLOW_ID_REUSE_POLICY_REJECT_DUPLICATE,
	}
	label := in.LabelName
	if label == "" {
		label = activities.ReadyToMergeLabel
	}
	if _, err := c.ExecuteWorkflow(ctx, opts, workflows.AutoMergeWorkflow, workflows.AutoMergeInput{
		Repo: in.Repo, PRNumber: in.PRNumber, LabelName: label,
		TriggerEventID: in.TriggerEventID, ManualLabel: isManualLabelActor(in.ActorLogin),
	}); err != nil {
		if isAlreadyStartedErr(err) || isAutoMergeAlreadyStartedErr(err) {
			activities.EmitAutoMergeChainEvent(ctx, activities.AutoMergeAlreadyRunning, workflowID, map[string]any{
				"repo": in.Repo, "pr_number": in.PRNumber, "conflicting_workflow_id": workflowID,
			})
			return autoMergeDispatchResult{WorkflowID: workflowID, FailureKind: "already_running"}
		}
		return autoMergeDispatchResult{WorkflowID: workflowID, FailureKind: "dispatch_error", Detail: err.Error()}
	}
	return autoMergeDispatchResult{Dispatched: true, WorkflowID: workflowID}
}

func signalAutoMergeLabelRemoved(ctx context.Context, repo string, prNumber int, temporalHost string, dialer temporalDialer, stderr io.Writer) autoMergeDispatchResult {
	workflowID, found, err := findLatestRunningAutoMerge("", repo, prNumber)
	if err != nil {
		fmt.Fprintf(stderr, "warning: auto-merge running scan failed: %v\n", err)
		return autoMergeDispatchResult{FailureKind: "chain_scan_failed", Detail: err.Error()}
	}
	if !found {
		return autoMergeDispatchResult{}
	}
	if dialer == nil {
		dialer = dialTemporalAsStarter
	}
	c, host, err := dialer(ctx, temporalHost)
	if err != nil {
		return autoMergeDispatchResult{WorkflowID: workflowID, FailureKind: "temporal_unreachable", Detail: fmt.Sprintf("dial %s: %v", host, err)}
	}
	defer c.Close()
	signaler, ok := c.(workflowSignaler)
	if !ok {
		return autoMergeDispatchResult{WorkflowID: workflowID, FailureKind: "signal_unsupported"}
	}
	if err := signaler.SignalWorkflow(ctx, workflowID, "", workflows.LabelRemovedSignal, true); err != nil {
		return autoMergeDispatchResult{WorkflowID: workflowID, FailureKind: "signal_error", Detail: err.Error()}
	}
	return autoMergeDispatchResult{WorkflowID: workflowID, Signaled: true}
}

func isAutoMergeDisabled() bool {
	return os.Getenv("CHITIN_AUTO_MERGE_DISABLED") == "1"
}

func isManualLabelActor(login string) bool {
	l := strings.ToLower(login)
	return !(strings.Contains(l, "bot") || strings.Contains(l, "chitin") || strings.Contains(l, "github-actions"))
}

func isAutoMergeAlreadyStartedErr(err error) bool {
	if err == nil {
		return false
	}
	var aErr *serviceerror.WorkflowExecutionAlreadyStarted
	if errors.As(err, &aErr) {
		return true
	}
	return strings.Contains(err.Error(), "WorkflowExecutionAlreadyStarted") ||
		strings.Contains(err.Error(), "already started")
}
