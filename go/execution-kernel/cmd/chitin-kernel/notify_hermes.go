// notify_hermes.go: outbound notify path for the operator-approval
// escalation effect. The original spec assumed `hermes message send`,
// which does not exist (Task 16 investigation, observation
// docs/observations/2026-05-07-hermes-cli-surface-for-pending-
// approvals.md). The real shape is two CLI calls against
// `hermes kanban`:
//
//  1. `hermes kanban create --idempotency-key <id> --body <rendered>
//     --assignee <profile> --json` — creates a kanban task; the JSON
//     envelope returns {"task_id": "..."} which is the correlation
//     handle for inbound replies.
//  2. `hermes kanban notify-subscribe --platform <p> --chat-id <c>
//     <task_id>` — routes the task's events to the operator's
//     whatsapp/slack/etc. chat.
//
// The returned task_id is stamped on pending_approvals.hermes_task_id
// by the caller. Inbound watch-hermes (Task 19) polls the same id.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"text/template"

	"github.com/chitinhq/chitin/go/execution-kernel/internal/gov"
)

// execHermes is a mockable hook around the hermes CLI shell-out. Tests
// override this to assert call shapes without spawning a real process.
var execHermes = func(bin string, args []string) ([]byte, error) {
	return exec.Command(bin, args...).Output()
}

// operatorConfig is the resolved view of ~/.chitin/operator.yaml. It is
// the operator-managed config that bridges chitin's escalation lifecycle
// to hermes's kanban + notify-subscribe surfaces. Loaded at notify time
// by the caller; passed in here so this file stays unit-testable.
type operatorConfig struct {
	// Channel is reserved for future flat-channel messaging support.
	// Hermes today has no such surface; leave empty / unused for v1.
	// Kept as a struct field so a future hermes release that adds
	// `hermes message send` doesn't force a struct rename across
	// callers.
	Channel string

	// HermesBin is the path to the hermes CLI (default: "hermes").
	HermesBin string

	// NotifyPlatform: hermes-side platform identifier (e.g., "whatsapp",
	// "slack"). Used as the `--platform` arg to
	// `hermes kanban notify-subscribe`.
	NotifyPlatform string

	// NotifyChatID: hermes-side chat identifier on NotifyPlatform.
	// Used as the `--chat-id` arg to `hermes kanban notify-subscribe`.
	NotifyChatID string

	// AssigneeProfile: hermes profile name to assign the kanban task to.
	// Routes the task into the operator's view (e.g., "operator",
	// "default").
	AssigneeProfile string
}

// builtinNotifyTemplate is the default kanban-task body when the rule
// doesn't override notify_template. Operator sees this in the kanban
// task's body, surfaced via whatsapp/slack via notify-subscribe.
//
// Kept short (well under the spec's ~1000-char soft limit) so it
// renders cleanly on a phone screen.
const builtinNotifyTemplate = `Operator approval needed
  agent:    {{.Agent}}
  action:   {{.ActionType}} on {{.ActionTarget}}
  reason:   {{.Reason}}
  timeout:  {{.TimeoutSeconds}}s

Reply on this kanban task's thread:
  ` + "`approve`" + `              -> single call
  ` + "`approve 30m`" + `          -> approve + grant 30 min for this rule
  ` + "`deny`" + ` or ` + "`deny <reason>`" + ` -> deny

Or from a terminal: chitin-kernel pending approve {{.ID}} [--window 30m]`

// renderNotifyTemplate writes the body to w using either the provided
// template (per-rule override from EscalateConfig.NotifyTemplate) or
// the built-in default. The template is parsed against PendingApproval
// fields (.ID, .Agent, .ActionType, .ActionTarget, .Reason,
// .TimeoutSeconds, etc.).
func renderNotifyTemplate(w io.Writer, tpl string, row gov.PendingApproval) error {
	if tpl == "" {
		tpl = builtinNotifyTemplate
	}
	t, err := template.New("notify").Parse(tpl)
	if err != nil {
		return err
	}
	return t.Execute(w, row)
}

// notifyHermes runs the two-call kanban dance and returns the kanban
// task_id that the caller should stamp onto the pending_approvals row
// (column: hermes_task_id).
//
// Failure modes:
//   - Call 1 (`kanban create`) failure or non-JSON output → return
//     ("", err). Caller stamps notify_failed_reason on the row.
//   - Call 2 (`kanban notify-subscribe`) failure → return (taskID, err).
//     The task exists and the operator can still resolve via CLI; only
//     the chat-route failed. Caller decides whether to stamp
//     notify_failed_reason or proceed.
//
// Idempotency: re-running with the same `id` returns the same task_id
// from hermes (per `--idempotency-key` semantics). Safe to retry after
// a kernel crash.
//
// Spec: docs/observations/2026-05-07-hermes-cli-surface-for-pending-
// approvals.md (Task 16 investigation). The original
// `hermes message send` shape was fictional; this is the real surface.
func notifyHermes(id string, row gov.PendingApproval, cfg operatorConfig) (string, error) {
	var buf bytes.Buffer
	// Per-rule template override threading is the caller's
	// responsibility (read from the matched EscalateConfig); v1 always
	// uses the built-in default here.
	if err := renderNotifyTemplate(&buf, "", row); err != nil {
		return "", fmt.Errorf("render notify template: %w", err)
	}

	// Call 1: create the kanban task. --idempotency-key gives us
	// crash-safe retries; --json gives us the task_id back; --assignee
	// routes to the operator's view.
	createArgs := []string{
		"kanban", "create",
		"--idempotency-key", id,
		"--body", buf.String(),
		"--assignee", cfg.AssigneeProfile,
		"--json",
	}
	out, err := execHermes(cfg.HermesBin, createArgs)
	if err != nil {
		return "", fmt.Errorf("hermes kanban create: %w", err)
	}
	var created struct {
		TaskID string `json:"task_id"`
	}
	if err := json.Unmarshal(out, &created); err != nil {
		// Hermes returned non-JSON output. Without a task_id we can't
		// correlate the operator's reply back to this escalation.
		// Surface to caller; they stamp notify_failed_reason.
		return "", fmt.Errorf("hermes kanban create: parse JSON: %w (output: %s)", err, out)
	}
	if created.TaskID == "" {
		return "", fmt.Errorf("hermes kanban create: empty task_id (output: %s)", out)
	}

	// Call 2: subscribe operator's chat to task notifications. Failure
	// here is non-fatal — the task still exists and the operator can
	// resolve via CLI even if no whatsapp ping fires. Return the
	// task_id either way so the caller stamps it on the row.
	subArgs := []string{
		"kanban", "notify-subscribe",
		"--platform", cfg.NotifyPlatform,
		"--chat-id", cfg.NotifyChatID,
		created.TaskID,
	}
	if _, subErr := execHermes(cfg.HermesBin, subArgs); subErr != nil {
		return created.TaskID, fmt.Errorf("hermes kanban notify-subscribe: %w (task_id %s created OK)", subErr, created.TaskID)
	}
	return created.TaskID, nil
}
