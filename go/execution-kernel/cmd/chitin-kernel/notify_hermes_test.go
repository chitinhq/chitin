package main

import (
	"bytes"
	"os"
	"path/filepath"
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
		// First call (kanban create) returns a task id. Hermes
		// uses field name `id` (not `task_id` — see PR #391 dogfood).
		if len(calls) == 0 {
			c.out = []byte(`{"id":"t_FAKE001"}`)
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

func TestParseHermesReply(t *testing.T) {
	cases := []struct {
		in            string
		wantApproved  bool
		wantWindowSec int
		wantDenied    bool
		wantReason    string
		wantUnparsed  bool
	}{
		{in: "approve", wantApproved: true},
		{in: "  Approve  ", wantApproved: true}, // case-insensitive, trim
		{in: "approve 30m", wantApproved: true, wantWindowSec: 1800},
		{in: "approve 1h", wantApproved: true, wantWindowSec: 3600},
		{in: "deny", wantDenied: true},
		{in: "deny no thank you", wantDenied: true, wantReason: "no thank you"},
		{in: "lol what", wantUnparsed: true},
		{in: "", wantUnparsed: true},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got, err := parseHermesReply(tc.in)
			if tc.wantUnparsed {
				if err == nil {
					t.Errorf("expected unparsed error, got %+v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("err: %v", err)
			}
			if got.Approved != tc.wantApproved {
				t.Errorf("Approved = %v, want %v", got.Approved, tc.wantApproved)
			}
			if got.Denied != tc.wantDenied {
				t.Errorf("Denied = %v, want %v", got.Denied, tc.wantDenied)
			}
			if got.WindowSeconds != tc.wantWindowSec {
				t.Errorf("WindowSeconds = %d, want %d", got.WindowSeconds, tc.wantWindowSec)
			}
			if got.Reason != tc.wantReason {
				t.Errorf("Reason = %q, want %q", got.Reason, tc.wantReason)
			}
		})
	}
}

func TestWatchHermesOnce_ApprovesOnApproveComment(t *testing.T) {
	dir := t.TempDir()
	store, _ := gov.OpenEscalateStore(filepath.Join(dir, "p.sqlite"))
	defer store.Close()

	// Seed an unresolved row with a hermes_task_id.
	_ = store.InsertPending(gov.PendingApproval{
		ID: "01ESC", Agent: "claude-code", RuleID: "r", ActionType: "file.write",
		ActionTarget: "/etc/hostname", Cwd: "/tmp", Reason: "test",
		Channel: "hermes", TimeoutSeconds: 600, RememberWindowSeconds: 0,
		CreatedTs: 1700000000,
	})
	_, _ = store.DB().Exec(`UPDATE pending_approvals SET hermes_task_id = ? WHERE id = ?`, "t_FAKE001", "01ESC")

	prev := execHermes
	execHermes = func(bin string, args []string) ([]byte, error) {
		// Must be a `kanban show --json <task_id>` invocation.
		joined := strings.Join(args, " ")
		if !strings.Contains(joined, "kanban show") {
			t.Fatalf("expected kanban show, got %q", joined)
		}
		if !strings.Contains(joined, "t_FAKE001") {
			t.Fatalf("expected task id in args, got %q", joined)
		}
		return []byte(`{
			"task": {"id": "t_FAKE001", "title": "test"},
			"comments": [
				{"author": "system", "body": "task created", "created_at": 1700000010},
				{"author": "operator", "body": "approve 30m", "created_at": 1700000020}
			],
			"events": []
		}`), nil
	}
	defer func() { execHermes = prev }()

	cfg := operatorConfig{HermesBin: "hermes"}
	count, err := watchHermesOnce(store, cfg)
	if err != nil {
		t.Fatalf("watch: %v", err)
	}
	if count != 1 {
		t.Errorf("count = %d, want 1", count)
	}

	got, _ := store.GetPending("01ESC")
	if got.Resolution != "approved" {
		t.Errorf("resolution = %q, want approved", got.Resolution)
	}
	if got.RememberGrantSeconds == nil || *got.RememberGrantSeconds != 1800 {
		t.Errorf("granted = %v, want 1800 (30m)", got.RememberGrantSeconds)
	}
}

func TestWatchHermesOnce_SkipsResolvedAndRowsWithoutTaskID(t *testing.T) {
	dir := t.TempDir()
	store, _ := gov.OpenEscalateStore(filepath.Join(dir, "p.sqlite"))
	defer store.Close()

	// Three rows: resolved, no-task-id, watchable. Only watchable should be queried.
	mk := func(id string, withTaskID bool, resolved bool) {
		_ = store.InsertPending(gov.PendingApproval{
			ID: id, Agent: "a", RuleID: "r", ActionType: "shell.exec",
			ActionTarget: "x", Cwd: "/tmp", Reason: "x",
			Channel: "hermes", TimeoutSeconds: 60, RememberWindowSeconds: 0,
			CreatedTs: 1700000000,
		})
		if withTaskID {
			_, _ = store.DB().Exec(`UPDATE pending_approvals SET hermes_task_id = ? WHERE id = ?`, "t_"+id, id)
		}
		if resolved {
			_ = store.ResolveApprove(id, "operator-cli", 0)
		}
	}
	mk("01R", true, true)   // resolved (skip)
	mk("01N", false, false) // no task id (skip)
	mk("01W", true, false)  // watchable (query)

	var queriedTaskIDs []string
	prev := execHermes
	execHermes = func(bin string, args []string) ([]byte, error) {
		joined := strings.Join(args, " ")
		for _, id := range []string{"t_01R", "t_01W"} {
			if strings.Contains(joined, id) {
				queriedTaskIDs = append(queriedTaskIDs, id)
			}
		}
		return []byte(`{"task":{"id":"x"},"comments":[]}`), nil
	}
	defer func() { execHermes = prev }()

	cfg := operatorConfig{HermesBin: "hermes"}
	_, _ = watchHermesOnce(store, cfg)

	if len(queriedTaskIDs) != 1 || queriedTaskIDs[0] != "t_01W" {
		t.Errorf("queried = %v, want [t_01W] only", queriedTaskIDs)
	}
}

func TestWatchHermesOnce_IdempotentOnAlreadyResolved(t *testing.T) {
	// If a row is resolved between when ListUnresolved() runs and when
	// the watch loop tries to ResolveApprove, the resolve fails silently
	// (ErrAlreadyResolved). The watch loop must not crash or count it.
	dir := t.TempDir()
	store, _ := gov.OpenEscalateStore(filepath.Join(dir, "p.sqlite"))
	defer store.Close()
	_ = store.InsertPending(gov.PendingApproval{
		ID: "01I", Agent: "a", RuleID: "r", ActionType: "shell.exec",
		ActionTarget: "x", Cwd: "/tmp", Reason: "x",
		Channel: "hermes", TimeoutSeconds: 600, RememberWindowSeconds: 0,
		CreatedTs: 1700000000,
	})
	_, _ = store.DB().Exec(`UPDATE pending_approvals SET hermes_task_id = ? WHERE id = ?`, "t_01I", "01I")

	prev := execHermes
	execHermes = func(bin string, args []string) ([]byte, error) {
		return []byte(`{"task":{"id":"t_01I"},"comments":[{"author":"o","body":"approve","created_at":1700000010}]}`), nil
	}
	defer func() { execHermes = prev }()

	cfg := operatorConfig{HermesBin: "hermes"}

	// First tick: resolves.
	count1, _ := watchHermesOnce(store, cfg)
	if count1 != 1 {
		t.Errorf("first tick count = %d, want 1", count1)
	}

	// Second tick: re-fetches the same comment. Should not crash, should
	// return count=0 (resolve was a silent no-op).
	count2, err := watchHermesOnce(store, cfg)
	if err != nil {
		t.Fatalf("second tick err: %v", err)
	}
	if count2 != 0 {
		t.Errorf("second tick count = %d, want 0 (already resolved)", count2)
	}
}

func TestWatchHermesOnce_UnparsedCommentsIgnored(t *testing.T) {
	dir := t.TempDir()
	store, _ := gov.OpenEscalateStore(filepath.Join(dir, "p.sqlite"))
	defer store.Close()
	_ = store.InsertPending(gov.PendingApproval{
		ID: "01U", Agent: "a", RuleID: "r", ActionType: "shell.exec",
		ActionTarget: "x", Cwd: "/tmp", Reason: "x",
		Channel: "hermes", TimeoutSeconds: 600, RememberWindowSeconds: 0,
		CreatedTs: 1700000000,
	})
	_, _ = store.DB().Exec(`UPDATE pending_approvals SET hermes_task_id = ? WHERE id = ?`, "t_01U", "01U")

	prev := execHermes
	execHermes = func(bin string, args []string) ([]byte, error) {
		return []byte(`{"task":{"id":"t_01U"},"comments":[{"author":"o","body":"lol whatever","created_at":1700000010}]}`), nil
	}
	defer func() { execHermes = prev }()

	cfg := operatorConfig{HermesBin: "hermes"}
	count, _ := watchHermesOnce(store, cfg)
	if count != 0 {
		t.Errorf("count = %d, want 0 (unparsed)", count)
	}

	got, _ := store.GetPending("01U")
	if got.ResolvedTs != nil {
		t.Errorf("should not be resolved")
	}
}

func TestLoadOperatorConfigFrom_FullConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "operator.yaml")
	contents := `hermes_bin: /usr/local/bin/hermes
notify_platform: whatsapp
notify_chat_id: "+15555550100"
assignee_profile: operator
channel: ops-approvals
`
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := loadOperatorConfigFrom(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.HermesBin != "/usr/local/bin/hermes" {
		t.Errorf("HermesBin = %q", cfg.HermesBin)
	}
	if cfg.NotifyPlatform != "whatsapp" {
		t.Errorf("NotifyPlatform = %q", cfg.NotifyPlatform)
	}
	if cfg.NotifyChatID != "+15555550100" {
		t.Errorf("NotifyChatID = %q", cfg.NotifyChatID)
	}
	if cfg.AssigneeProfile != "operator" {
		t.Errorf("AssigneeProfile = %q", cfg.AssigneeProfile)
	}
	if cfg.Channel != "ops-approvals" {
		t.Errorf("Channel = %q", cfg.Channel)
	}
}

func TestLoadOperatorConfigFrom_DefaultsAppliedForOptional(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "operator.yaml")
	// Minimal config: only required fields.
	contents := `notify_platform: whatsapp
notify_chat_id: "+1"
assignee_profile: operator
`
	_ = os.WriteFile(path, []byte(contents), 0o600)

	cfg, err := loadOperatorConfigFrom(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.HermesBin != "hermes" {
		t.Errorf("HermesBin default = %q, want \"hermes\"", cfg.HermesBin)
	}
	// Channel is optional and may be empty for v1.
	if cfg.Channel != "" {
		t.Errorf("Channel = %q, want empty (optional)", cfg.Channel)
	}
}

func TestLoadOperatorConfigFrom_RejectsMissingRequired(t *testing.T) {
	cases := []struct {
		name      string
		yaml      string
		wantSubst string
	}{
		{
			name:      "missing notify_platform",
			yaml:      "notify_chat_id: x\nassignee_profile: o\n",
			wantSubst: "notify_platform",
		},
		{
			name:      "missing notify_chat_id",
			yaml:      "notify_platform: whatsapp\nassignee_profile: o\n",
			wantSubst: "notify_chat_id",
		},
		{
			name:      "missing assignee_profile",
			yaml:      "notify_platform: whatsapp\nnotify_chat_id: x\n",
			wantSubst: "assignee_profile",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "operator.yaml")
			_ = os.WriteFile(path, []byte(tc.yaml), 0o600)
			_, err := loadOperatorConfigFrom(path)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tc.wantSubst) {
				t.Errorf("err = %v, want substring %q", err, tc.wantSubst)
			}
		})
	}
}

func TestLoadOperatorConfigFrom_MissingFile(t *testing.T) {
	_, err := loadOperatorConfigFrom("/nonexistent/path/operator.yaml")
	if err == nil {
		t.Error("expected error, got nil")
	}
}

func TestLoadOperatorConfigFrom_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "operator.yaml")
	_ = os.WriteFile(path, []byte("not: valid: yaml: at: all"), 0o600)
	_, err := loadOperatorConfigFrom(path)
	if err == nil {
		t.Error("expected parse error, got nil")
	}
}
