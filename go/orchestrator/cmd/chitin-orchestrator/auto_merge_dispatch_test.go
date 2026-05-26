package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"go.temporal.io/sdk/client"
)

func TestDispatchAutoMerge_DedupContracts(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CHITIN_DIR", dir)
	bin, _ := fakeKernelBin(t, 0)
	t.Setenv("CHITIN_KERNEL_BIN", bin)

	starter := &stubStarter{
		ExecuteWorkflowFn: func(context.Context, client.StartWorkflowOptions, any, ...any) (client.WorkflowRun, error) {
			return nil, errors.New("WorkflowExecutionAlreadyStarted")
		},
	}
	dialer := func(context.Context, string) (workflowStarter, string, error) {
		return starter, "test", nil
	}
	in := autoMergeDispatchInput{Repo: "chitinhq/chitin", PRNumber: 1135, LabelName: "chitin/ready-to-merge", TriggerEventID: "delivery-1"}
	out := dispatchAutoMerge(context.Background(), in, dialer, nil)
	if out.FailureKind != "already_running" || out.WorkflowID != "auto-merge-pr-1135-delivery-1" {
		t.Fatalf("in-flight duplicate out=%+v", out)
	}

	writeAutoMergeFixture(t, dir, "delivery-1", "auto_merge_succeeded")
	starter = &stubStarter{}
	out = dispatchAutoMerge(context.Background(), in, func(context.Context, string) (workflowStarter, string, error) {
		t.Fatal("dialer should not be called for post-completion redelivery")
		return starter, "test", nil
	}, nil)
	if out.FailureKind != "already_settled" {
		t.Fatalf("settled duplicate out=%+v", out)
	}

	in.TriggerEventID = "delivery-2"
	out = dispatchAutoMerge(context.Background(), in, func(context.Context, string) (workflowStarter, string, error) {
		return starter, "test", nil
	}, nil)
	if !out.Dispatched || out.WorkflowID != "auto-merge-pr-1135-delivery-2" {
		t.Fatalf("relabel retry out=%+v", out)
	}
}

func writeAutoMergeFixture(t *testing.T, dir, deliveryID, terminal string) {
	t.Helper()
	workflowID := "auto-merge-pr-1135-" + deliveryID
	rows := []map[string]any{
		{
			"ts": "2026-05-26T00:00:00Z", "event_type": "auto_merge_triggered", "run_id": workflowID,
			"payload": map[string]any{"repo": "chitinhq/chitin", "pr_number": 1135, "trigger_event_id": deliveryID},
		},
		{
			"ts": "2026-05-26T00:00:01Z", "event_type": terminal, "run_id": workflowID,
			"payload": map[string]any{"repo": "chitinhq/chitin", "pr_number": 1135},
		},
	}
	f, err := os.Create(filepath.Join(dir, "events-auto-merge.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for _, row := range rows {
		if err := enc.Encode(row); err != nil {
			t.Fatal(err)
		}
	}
}
