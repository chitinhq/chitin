package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/chitinhq/chitin/go/execution-kernel/internal/gov"
)

// TestNotifyHermes_TwoCallSequence locks in the kanban-shaped outbound
// path documented in observation 2026-05-07-hermes-cli-surface-for-
// pending-approvals.md (Task 16):
//
//  1. `hermes kanban create --idempotency-key <id> --json --body <tpl>
//     --assignee <profile>` returns {"task_id":"..."}
//  2. `hermes kanban notify-subscribe --platform <p> --chat-id <c>
//     <task_id>` routes the task into the operator's chat.
//
// The returned task_id is what the caller stamps on the
// pending_approvals row's hermes_task_id column.
func TestNotifyHermes_TwoCallSequence(t *testing.T) {
	type call struct {
		bin  string
		args []string
		out  []byte
	}
	var calls []call
	prev := execHermes
	execHermes = func(bin string, args []string) ([]byte, error) {
		c := call{bin: bin, args: args}
		// First call (kanban create) returns a task id.
		if len(calls) == 0 {
			c.out = []byte(`{"task_id":"t_FAKE001"}`)
		} else {
			// Second call (notify-subscribe) returns ok.
			c.out = []byte(`{"ok":true}`)
		}
		calls = append(calls, c)
		return c.out, nil
	}
	defer func() { execHermes = prev }()

	row := gov.PendingApproval{
		ID: "01TEST", Agent: "claude-code", ActionType: "file.write",
		ActionTarget: "/etc/hostname", Reason: "system path write",
		Channel: "hermes", TimeoutSeconds: 600,
	}
	cfg := operatorConfig{
		HermesBin: "hermes", NotifyPlatform: "whatsapp",
		NotifyChatID: "1234567890", AssigneeProfile: "operator",
	}

	taskID, err := notifyHermes("01TEST", row, cfg)
	if err != nil {
		t.Fatalf("notify: %v", err)
	}
	if taskID != "t_FAKE001" {
		t.Errorf("taskID = %q, want t_FAKE001", taskID)
	}
	if len(calls) != 2 {
		t.Fatalf("wanted 2 calls, got %d", len(calls))
	}

	// Validate first call shape: hermes kanban create with idempotency-key.
	create := strings.Join(calls[0].args, " ")
	if !strings.Contains(create, "kanban create") {
		t.Errorf("call 1 missing 'kanban create': %q", create)
	}
	if !strings.Contains(create, "01TEST") {
		t.Errorf("call 1 missing escalation id: %q", create)
	}
	if !strings.Contains(create, "--json") {
		t.Errorf("call 1 missing --json: %q", create)
	}
	if !strings.Contains(create, "--idempotency-key") {
		t.Errorf("call 1 missing --idempotency-key: %q", create)
	}

	// Validate second call shape: hermes kanban notify-subscribe with platform + chat-id + task_id.
	sub := strings.Join(calls[1].args, " ")
	if !strings.Contains(sub, "kanban notify-subscribe") {
		t.Errorf("call 2 missing 'kanban notify-subscribe': %q", sub)
	}
	if !strings.Contains(sub, "whatsapp") {
		t.Errorf("call 2 missing platform: %q", sub)
	}
	if !strings.Contains(sub, "1234567890") {
		t.Errorf("call 2 missing chat-id: %q", sub)
	}
	if !strings.Contains(sub, "t_FAKE001") {
		t.Errorf("call 2 missing task_id from call 1: %q", sub)
	}
}

// TestRenderNotifyTemplate_BuiltinDefault confirms the built-in body
// surfaces the four operator-relevant fields (id, agent, action,
// target) plus the approve/deny grammar.
func TestRenderNotifyTemplate_BuiltinDefault(t *testing.T) {
	row := gov.PendingApproval{
		ID: "01TEST", Agent: "claude-code", ActionType: "file.write",
		ActionTarget: "/etc/hostname", Reason: "system path write",
		TimeoutSeconds: 600,
	}
	var buf bytes.Buffer
	if err := renderNotifyTemplate(&buf, "", row); err != nil {
		t.Fatalf("render: %v", err)
	}
	body := buf.String()
	for _, want := range []string{"01TEST", "claude-code", "file.write", "/etc/hostname", "approve", "deny"} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q: %q", want, body)
		}
	}
}

// TestRenderNotifyTemplate_Override confirms a per-rule template
// overrides the built-in and has access to the row's fields.
func TestRenderNotifyTemplate_Override(t *testing.T) {
	row := gov.PendingApproval{ID: "01X", Agent: "a", ActionType: "shell.exec", ActionTarget: "ls"}
	tpl := "ID={{.ID}} TARGET={{.ActionTarget}}"
	var buf bytes.Buffer
	if err := renderNotifyTemplate(&buf, tpl, row); err != nil {
		t.Fatalf("render: %v", err)
	}
	want := "ID=01X TARGET=ls"
	if buf.String() != want {
		t.Errorf("got %q, want %q", buf.String(), want)
	}
}
