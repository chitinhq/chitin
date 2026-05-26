package workflows

import (
	"fmt"
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/chitinhq/chitin/go/orchestrator/activities"
)

const LabelRemovedSignal = "LabelRemoved"

type AutoMergeInput struct {
	Repo           string `json:"repo"`
	PRNumber       int    `json:"pr_number"`
	LabelName      string `json:"label_name"`
	TriggerEventID string `json:"trigger_event_id"`
	ManualLabel    bool   `json:"manual_label"`
	TimeoutSeconds int    `json:"merge_timeout_seconds,omitempty"`
}

type AutoMergeResult struct {
	Outcome       string `json:"outcome"`
	MergeSHA      string `json:"merge_sha,omitempty"`
	FailureReason string `json:"failure_reason,omitempty"`
}

const autoMergeActivityTimeout = 2 * time.Minute

func AutoMergeWorkflow(ctx workflow.Context, in AutoMergeInput) (AutoMergeResult, error) {
	if in.PRNumber <= 0 || in.Repo == "" || in.TriggerEventID == "" {
		return AutoMergeResult{}, temporal.NewNonRetryableApplicationError(
			"auto-merge: Repo, PRNumber, and TriggerEventID are required",
			"InvalidAutoMergeInput", nil)
	}
	if in.LabelName == "" {
		in.LabelName = activities.ReadyToMergeLabel
	}
	timeout := in.TimeoutSeconds
	if timeout <= 0 {
		timeout = 3600
	}
	workflowID := workflow.GetInfo(ctx).WorkflowExecution.ID
	actx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: autoMergeActivityTimeout,
		RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 1},
	})

	emit(ctx, actx, activities.AutoMergeTriggered, workflowID, map[string]any{
		"repo": in.Repo, "pr_number": in.PRNumber, "workflow_id": workflowID,
		"trigger_event_id": in.TriggerEventID, "manual_label": in.ManualLabel,
	})

	backoff := 60
	elapsed := 0
	for {
		var report activities.MergeabilityReport
		if err := workflow.ExecuteActivity(actx, "CheckPRMergeability", activities.CheckPRMergeabilityInput{
			Repo: in.Repo, PRNumber: in.PRNumber, LabelName: in.LabelName,
		}).Get(ctx, &report); err != nil {
			return mergeFailure(ctx, actx, in, workflowID, "auto_merge_failed", err.Error())
		}

		if !report.HasLabel {
			reason := "label_removed_pre_flight"
			if elapsed > 0 {
				reason = "label_removed_mid_wait"
			}
			emit(ctx, actx, activities.AutoMergeCanceled, workflowID, map[string]any{
				"repo": in.Repo, "pr_number": in.PRNumber, "reason": reason,
			})
			return AutoMergeResult{Outcome: "canceled", FailureReason: reason}, nil
		}
		if !report.IsOpen {
			emit(ctx, actx, activities.AutoMergeCanceled, workflowID, map[string]any{
				"repo": in.Repo, "pr_number": in.PRNumber, "reason": "pr_closed_pre_flight",
			})
			return AutoMergeResult{Outcome: "canceled", FailureReason: "pr_closed_pre_flight"}, nil
		}
		if report.IsDraft {
			emit(ctx, actx, activities.AutoMergeCanceled, workflowID, map[string]any{
				"repo": in.Repo, "pr_number": in.PRNumber, "reason": "pr_is_draft",
			})
			return AutoMergeResult{Outcome: "canceled", FailureReason: "pr_is_draft"}, nil
		}
		if report.CIStatus == activities.CIStatusFailed {
			emit(ctx, actx, activities.AutoMergeCIFailed, workflowID, map[string]any{
				"repo": in.Repo, "pr_number": in.PRNumber, "failed_checks": report.FailedChecks,
			})
			return failureBranch(ctx, actx, in, workflowID, "auto_merge_ci_failed", activities.CommentTemplateCIFailed, report, elapsed)
		}
		if report.CIStatus == activities.CIStatusGreen && !report.IsMergeable {
			emit(ctx, actx, activities.AutoMergeConflict, workflowID, map[string]any{
				"repo": in.Repo, "pr_number": in.PRNumber, "base_ref": report.BaseRef,
				"conflict_file_count": report.ConflictFileCount,
			})
			return failureBranch(ctx, actx, in, workflowID, "auto_merge_conflict", activities.CommentTemplateMergeConflict, report, elapsed)
		}
		if report.CIStatus == activities.CIStatusGreen {
			var mr activities.MergeResult
			if err := workflow.ExecuteActivity(actx, "MergePR", activities.MergePRInput{Repo: in.Repo, PRNumber: in.PRNumber}).Get(ctx, &mr); err != nil {
				return mergeFailure(ctx, actx, in, workflowID, "auto_merge_failed", err.Error())
			}
			if !mr.Succeeded {
				return mergeFailure(ctx, actx, in, workflowID, "auto_merge_failed", mr.StderrTail)
			}
			emit(ctx, actx, activities.AutoMergeSucceeded, workflowID, map[string]any{
				"repo": in.Repo, "pr_number": in.PRNumber, "merge_sha": mr.MergeSHA,
				"head_branch_deleted": mr.HeadBranchDeleted,
			})
			return AutoMergeResult{Outcome: "succeeded", MergeSHA: mr.MergeSHA}, nil
		}

		if elapsed >= timeout {
			emit(ctx, actx, activities.AutoMergeCITimeout, workflowID, map[string]any{
				"repo": in.Repo, "pr_number": in.PRNumber, "waited_seconds": elapsed,
				"timeout_seconds": timeout, "last_ci_state": "pending",
			})
			return failureBranch(ctx, actx, in, workflowID, "auto_merge_ci_timeout", activities.CommentTemplateCITimeout, report, elapsed)
		}

		emit(ctx, actx, activities.AutoMergeWaiting, workflowID, map[string]any{
			"repo": in.Repo, "pr_number": in.PRNumber, "elapsed_seconds": elapsed, "ci_state": "pending",
		})
		sleepFor := backoff
		if elapsed+sleepFor > timeout {
			sleepFor = timeout - elapsed
		}
		if sleepFor <= 0 {
			sleepFor = 1
		}
		canceled, err := sleepOrLabelRemoved(ctx, time.Duration(sleepFor)*time.Second)
		if err != nil {
			return AutoMergeResult{}, err
		}
		if canceled {
			emit(ctx, actx, activities.AutoMergeCanceled, workflowID, map[string]any{
				"repo": in.Repo, "pr_number": in.PRNumber, "reason": "label_removed_mid_wait",
			})
			return AutoMergeResult{Outcome: "canceled", FailureReason: "label_removed_mid_wait"}, nil
		}
		elapsed += sleepFor
		if backoff < 480 {
			backoff *= 2
			if backoff > 480 {
				backoff = 480
			}
		}
	}
}

func sleepOrLabelRemoved(ctx workflow.Context, d time.Duration) (bool, error) {
	timer := workflow.NewTimer(ctx, d)
	sig := workflow.GetSignalChannel(ctx, LabelRemovedSignal)
	var canceled bool
	selector := workflow.NewSelector(ctx)
	selector.AddFuture(timer, func(workflow.Future) {})
	selector.AddReceive(sig, func(c workflow.ReceiveChannel, more bool) {
		var v any
		c.Receive(ctx, &v)
		canceled = true
	})
	selector.Select(ctx)
	return canceled, nil
}

func failureBranch(ctx, actx workflow.Context, in AutoMergeInput, workflowID, reason string, template activities.CommentTemplateID, report activities.MergeabilityReport, elapsed int) (AutoMergeResult, error) {
	_ = workflow.ExecuteActivity(actx, "UnlabelPR", activities.UnlabelPRInput{Repo: in.Repo, PRNumber: in.PRNumber, LabelName: in.LabelName}).Get(ctx, nil)
	_ = workflow.ExecuteActivity(actx, "CommentPR", activities.CommentPRInput{
		Repo: in.Repo, PRNumber: in.PRNumber, WorkflowID: workflowID, TemplateID: template,
		FailedChecks: report.FailedChecks, ConflictFileCount: report.ConflictFileCount, ElapsedSeconds: elapsed,
	}).Get(ctx, nil)
	notify(ctx, actx, in, reason)
	return AutoMergeResult{Outcome: "failed", FailureReason: reason}, nil
}

func mergeFailure(ctx, actx workflow.Context, in AutoMergeInput, workflowID, reason, detail string) (AutoMergeResult, error) {
	if len(detail) > 1024 {
		detail = detail[len(detail)-1024:]
	}
	emit(ctx, actx, activities.AutoMergeFailed, workflowID, map[string]any{
		"repo": in.Repo, "pr_number": in.PRNumber, "stderr_tail": detail,
	})
	notify(ctx, actx, in, reason)
	return AutoMergeResult{Outcome: "failed", FailureReason: reason}, nil
}

func notify(ctx, actx workflow.Context, in AutoMergeInput, reason string) {
	_ = workflow.ExecuteActivity(actx, "DiscordNotify", activities.NotificationEvent{
		Kind:    activities.NotifyNodeBlocked,
		NodeID:  fmt.Sprintf("pr-%d", in.PRNumber),
		Summary: fmt.Sprintf("%s on %s#%d", reason, in.Repo, in.PRNumber),
		URL:     fmt.Sprintf("https://github.com/%s/pull/%d", in.Repo, in.PRNumber),
	}).Get(ctx, nil)
}

func emit(ctx, actx workflow.Context, eventType, workflowID string, payload map[string]any) {
	_ = workflow.ExecuteActivity(actx, "EmitAutoMergeEvent", activities.AutoMergeEventInput{
		EventType: eventType, WorkflowID: workflowID, Payload: payload,
	}).Get(ctx, nil)
}
