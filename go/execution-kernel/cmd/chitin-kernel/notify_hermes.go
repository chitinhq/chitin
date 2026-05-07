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
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"
	"time"

	"github.com/chitinhq/chitin/go/execution-kernel/internal/gov"
	"gopkg.in/yaml.v3"
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

// HermesReplyParse is what parseHermesReply returns on a successful parse.
//
// Invariant: on a non-error return, exactly one of Approved or Denied is
// true; the other two fields (WindowSeconds, Reason) are optional metadata
// pulled from the suffix. WindowSeconds==0 means "use rule default" (the
// caller — Task 19 watch-hermes — applies grant.window from the rule).
type HermesReplyParse struct {
	Approved      bool
	Denied        bool
	WindowSeconds int    // optional; 0 means "use rule default"
	Reason        string // optional; only set on deny
}

// parseHermesReply turns a chat reply body into a structured parse.
// Returns an error for unparseable input (caller ignores those —
// the operator may have replied with prose unrelated to approval).
//
// Grammar (case-insensitive verb, outer whitespace trimmed):
//   - "approve"             -> Approved
//   - "approve <duration>"  -> Approved + WindowSeconds (Go time.ParseDuration)
//   - "deny"                -> Denied
//   - "deny <reason>"       -> Denied + Reason (free text after the verb)
//
// Anything else (including empty after trim) returns an error so the
// watcher can skip the message without acting on it.
func parseHermesReply(body string) (HermesReplyParse, error) {
	trimmed := strings.TrimSpace(body)
	if trimmed == "" {
		return HermesReplyParse{}, fmt.Errorf("empty reply")
	}
	lower := strings.ToLower(trimmed)
	switch {
	case lower == "approve":
		return HermesReplyParse{Approved: true}, nil
	case strings.HasPrefix(lower, "approve "):
		rest := strings.TrimSpace(trimmed[len("approve "):])
		dur, err := time.ParseDuration(rest)
		if err != nil {
			return HermesReplyParse{}, fmt.Errorf("approve <duration>: %w", err)
		}
		return HermesReplyParse{Approved: true, WindowSeconds: int(dur.Seconds())}, nil
	case lower == "deny":
		return HermesReplyParse{Denied: true}, nil
	case strings.HasPrefix(lower, "deny "):
		reason := strings.TrimSpace(trimmed[len("deny "):])
		return HermesReplyParse{Denied: true, Reason: reason}, nil
	}
	return HermesReplyParse{}, fmt.Errorf("unparsed reply: %q", trimmed)
}

// watchHermesOnce iterates unresolved pending_approvals rows that have
// a hermes_task_id, queries `hermes kanban show --json` per row, and
// scans ALL comments via parseHermesReply. The first parseable
// approve/deny wins; the resolve operation is idempotent (silent no-
// op on already-resolved rows), so re-parsing the same comment on
// subsequent ticks is harmless.
//
// No cursor is tracked. The previous design used last_event_seq but
// hermes comments have no monotonic per-comment seq (Task 19
// investigation). With 1-5 comments per task and 30s tick intervals,
// the redundant work is trivial.
//
// Returns the count of rows resolved on this tick.
func watchHermesOnce(store *gov.EscalateStore, cfg operatorConfig) (int, error) {
	rows, err := store.ListUnresolved()
	if err != nil {
		return 0, err
	}

	resolved := 0
	for _, row := range rows {
		if row.HermesTaskID == "" {
			continue // no kanban task to poll
		}

		out, err := execHermes(cfg.HermesBin, []string{
			"kanban", "show", "--json", row.HermesTaskID,
		})
		if err != nil {
			// Hermes failed (network, missing task, etc.). Skip; next
			// tick retries. Don't fail the whole watch loop.
			continue
		}

		var task struct {
			Task struct {
				ID string `json:"id"`
			} `json:"task"`
			Comments []struct {
				Author    string `json:"author"`
				Body      string `json:"body"`
				CreatedAt int64  `json:"created_at"`
			} `json:"comments"`
		}
		if err := json.Unmarshal(out, &task); err != nil {
			continue // malformed response; skip
		}

		// First parseable approve/deny in the comments wins.
		for _, c := range task.Comments {
			parsed, perr := parseHermesReply(c.Body)
			if perr != nil {
				continue // not an approval reply
			}
			if parsed.Approved {
				if err := store.ResolveApprove(row.ID, "hermes-reply", parsed.WindowSeconds); err == nil {
					resolved++
				}
				// If err != nil (likely ErrAlreadyResolved), silently absorb.
				break // row is decided; skip remaining comments
			}
			if parsed.Denied {
				if err := store.ResolveDeny(row.ID, "hermes-reply", parsed.Reason); err == nil {
					resolved++
				}
				break
			}
		}
	}

	return resolved, nil
}

// loadOperatorConfig loads the operator config from the conventional
// location (~/.chitin/operator.yaml) using chitinDir() to resolve
// $CHITIN_HOME. Returns a populated operatorConfig with defaults
// applied, or an error if required fields are missing.
func loadOperatorConfig() (operatorConfig, error) {
	return loadOperatorConfigFrom(filepath.Join(chitinDir(), "operator.yaml"))
}

// loadOperatorConfigFrom is the testable form. Reads the YAML at
// path, validates required fields, applies defaults, returns the
// populated operatorConfig.
//
// Required fields (no default — error if absent):
//   - notify_platform: hermes-side platform identifier (e.g. "whatsapp")
//   - notify_chat_id:  hermes-side chat id on that platform
//   - assignee_profile: hermes profile to assign the kanban task to
//
// Optional fields:
//   - hermes_bin: defaults to "hermes" (assumes on PATH)
//   - channel: vestigial; reserved for future flat-channel support
//
// Spec: docs/superpowers/specs/2026-05-07-operator-approval-escalation-design.md
// (operator config section), updated post-Task-17 to match the
// kanban-create + notify-subscribe transport.
func loadOperatorConfigFrom(path string) (operatorConfig, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return operatorConfig{}, fmt.Errorf("read %s: %w", path, err)
	}
	var raw struct {
		HermesBin       string `yaml:"hermes_bin"`
		NotifyPlatform  string `yaml:"notify_platform"`
		NotifyChatID    string `yaml:"notify_chat_id"`
		AssigneeProfile string `yaml:"assignee_profile"`
		Channel         string `yaml:"channel"`
	}
	if err := yaml.Unmarshal(b, &raw); err != nil {
		return operatorConfig{}, fmt.Errorf("parse %s: %w", path, err)
	}

	// Required-field validation.
	var missing []string
	if raw.NotifyPlatform == "" {
		missing = append(missing, "notify_platform")
	}
	if raw.NotifyChatID == "" {
		missing = append(missing, "notify_chat_id")
	}
	if raw.AssigneeProfile == "" {
		missing = append(missing, "assignee_profile")
	}
	if len(missing) > 0 {
		return operatorConfig{}, fmt.Errorf("%s: missing required fields: %v", path, missing)
	}

	// Default-fill.
	if raw.HermesBin == "" {
		raw.HermesBin = "hermes"
	}

	return operatorConfig{
		HermesBin:       raw.HermesBin,
		NotifyPlatform:  raw.NotifyPlatform,
		NotifyChatID:    raw.NotifyChatID,
		AssigneeProfile: raw.AssigneeProfile,
		Channel:         raw.Channel, // optional; empty OK
	}, nil
}
