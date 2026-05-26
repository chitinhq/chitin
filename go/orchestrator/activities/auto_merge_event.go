package activities

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

const (
	AutoMergeTriggered      = "auto_merge_triggered"
	AutoMergeWaiting        = "auto_merge_waiting"
	AutoMergeCanceled       = "auto_merge_canceled"
	AutoMergeSucceeded      = "auto_merge_succeeded"
	AutoMergeFailed         = "auto_merge_failed"
	AutoMergeCIFailed       = "auto_merge_ci_failed"
	AutoMergeConflict       = "auto_merge_conflict"
	AutoMergeCITimeout      = "auto_merge_ci_timeout"
	AutoMergeAlreadyRunning = "auto_merge_already_running"
	AutoMergeAlreadySettled = "auto_merge_already_settled"
)

type AutoMergeEventInput struct {
	EventType  string         `json:"event_type"`
	WorkflowID string         `json:"workflow_id"`
	Payload    map[string]any `json:"payload"`
}

type EmitAutoMergeEvent struct{}

func NewEmitAutoMergeEvent() *EmitAutoMergeEvent { return &EmitAutoMergeEvent{} }

func (a *EmitAutoMergeEvent) ActivityName() string { return "EmitAutoMergeEvent" }

func (a *EmitAutoMergeEvent) Execute(ctx context.Context, in AutoMergeEventInput) error {
	EmitAutoMergeChainEvent(ctx, in.EventType, in.WorkflowID, in.Payload)
	return nil
}

func EmitAutoMergeChainEvent(ctx context.Context, eventType, workflowID string, payload any) {
	if os.Getenv("CHITIN_DISABLE_CHAIN_EMIT") == "1" {
		return
	}
	binPath := os.Getenv("CHITIN_KERNEL_BIN")
	if binPath == "" {
		binPath = "chitin-kernel"
	}
	envelope := map[string]any{
		"schema_version":    "2",
		"event_type":        eventType,
		"run_id":            workflowID,
		"session_id":        "chitin-orchestrator-auto-merge",
		"surface":           "chitin-orchestrator",
		"agent_instance_id": "chitin-orchestrator",
		"chain_type":        "auto-merge",
		"ts":                time.Now().UTC().Format(time.RFC3339Nano),
		"payload":           payload,
	}
	body, err := json.Marshal(envelope)
	if err != nil {
		warnAutoMergeEmit("marshal: %v", err)
		return
	}
	tmp, err := os.CreateTemp("", "chitin-auto-merge-emit-*.json")
	if err != nil {
		warnAutoMergeEmit("temp file: %v", err)
		return
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.Write(body); err != nil {
		tmp.Close()
		warnAutoMergeEmit("temp write: %v", err)
		return
	}
	if err := tmp.Close(); err != nil {
		warnAutoMergeEmit("temp close: %v", err)
		return
	}
	chitinDir := os.Getenv("CHITIN_DIR")
	if chitinDir == "" {
		if home, err := os.UserHomeDir(); err == nil {
			chitinDir = home + "/.chitin"
		} else {
			chitinDir = ".chitin"
		}
	}
	emitCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(emitCtx, binPath, "emit", "-dir", chitinDir, "-event-file", tmpPath)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		tail := strings.TrimSpace(stderr.String())
		if len(tail) > 200 {
			tail = tail[len(tail)-200:]
		}
		warnAutoMergeEmit("kernel emit failed: %v (stderr: %s)", err, tail)
	}
}

func warnAutoMergeEmit(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "warning: auto-merge chain emit: "+format+"\n", args...)
}
