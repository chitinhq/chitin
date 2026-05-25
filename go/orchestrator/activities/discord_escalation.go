package activities

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

// discordEscalationEnv names the environment variable that carries the
// operator's Discord incoming-webhook URL for escalation messages. When
// set, notifyDiscordEscalation POSTs to it; when unset, the helper falls
// back to reading $HOME/.chitin/discord-webhook.secret. Empty in BOTH
// places means "no Discord configured" — the helper logs a warning to
// stderr and returns; never panics, never propagates.
const discordEscalationEnv = "CHITIN_DISCORD_WEBHOOK"

// discordWebhookSecretFile is the operator-host secret-file fallback when
// the env var is unset. Matches the placement convention of
// ~/.chitin/factory-webhook.secret. The file is expected to contain the
// webhook URL (no JSON wrapping, no leading "URL=", just the bare URL).
const discordWebhookSecretFile = ".chitin/discord-webhook.secret"

// discordPostTimeout caps a single Discord POST. Webhooks usually answer in
// <500ms; 5s lets a transient slow response through without blocking the
// caller for the activity's full timeout window.
const discordPostTimeout = 5 * time.Second

// EscalationSeverity classifies an escalation for visual marker selection.
// Closed taxonomy: alert (something needs operator attention) vs ready
// (positive ping — autopilot reached a definitive good state).
type EscalationSeverity int

const (
	// SeverityAlert is the default — operator action likely required.
	// Renders with 🚨.
	SeverityAlert EscalationSeverity = iota
	// SeverityReady is a positive ping — the loop reached a clean state
	// awaiting operator merge click. Renders with 🟢.
	SeverityReady
)

// EscalationNotice is the typed payload for one Discord escalation post.
// Closed shape so future escalation reasons stay consistent across the
// codebase (spec 112 US2, spec 113, spec 116, ...).
type EscalationNotice struct {
	// EventType matches the chain event_type that triggered the
	// notification — sibling_rebase_failed, pr_iteration_escalated,
	// internal_rereview_low_confidence, etc. Goes into the message
	// title for operator filtering / grep.
	EventType string
	// Severity is the visual marker class.
	Severity EscalationSeverity
	// PRNumber is the pull request involved.
	PRNumber int
	// PRTitle is the PR's title. Optional — if empty, the message uses
	// the PR number alone.
	PRTitle string
	// PRURL is the GitHub HTML URL the operator can click. Required for
	// the notice to be useful; an empty URL drops the notification with
	// a warning (the whole point is a clickable link to GitHub).
	PRURL string
	// Reason is the closed-taxonomy escalation reason kind
	// (sibling_rebase_failed, iteration_cap_hit, low_confidence, etc.).
	Reason string
	// Detail is a one-line human-readable explanation surfaced after the
	// reason kind. Keep it under ~200 chars — Discord truncates long
	// content lines.
	Detail string
}

// notifyDiscordEscalation posts one EscalationNotice to the operator's
// Discord webhook. Fail-soft: every error path (no webhook configured,
// network fault, non-2xx response, malformed input) logs a warning to
// stderr and returns nil. The chain event remains the source-of-truth for
// "this escalation happened"; the Discord post is the courtesy ping.
//
// The notice's content is rendered as plain Discord markdown — no
// embed/component complexity, so the message survives any future Discord
// webhook API change that breaks the structured-embed API.
func notifyDiscordEscalation(ctx context.Context, n EscalationNotice) {
	if n.PRURL == "" {
		warnDiscordEscalation("dropping notice: empty PRURL (no operator-clickable link to send)")
		return
	}
	webhookURL := resolveDiscordWebhookURL()
	if webhookURL == "" {
		// No webhook configured — fall through silently. This is the
		// expected state for a fresh operator-host until the secret is
		// populated; logging on every event would be noisy.
		return
	}

	body := buildDiscordEscalationBody(n)
	payload, err := json.Marshal(map[string]any{"content": body})
	if err != nil {
		warnDiscordEscalation("marshal payload: %v", err)
		return
	}

	postCtx, cancel := context.WithTimeout(ctx, discordPostTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(postCtx, http.MethodPost, webhookURL, bytes.NewReader(payload))
	if err != nil {
		warnDiscordEscalation("new request: %v", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		warnDiscordEscalation("post: %v", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		warnDiscordEscalation("non-2xx response: %d", resp.StatusCode)
	}
}

// buildDiscordEscalationBody renders the notice as a Discord markdown
// message. Pure function — exported via the discord_escalation_test.go
// test pattern (declared here as a top-level fn so the test can call it
// without driving the full POST path).
func buildDiscordEscalationBody(n EscalationNotice) string {
	marker := "🚨"
	if n.Severity == SeverityReady {
		marker = "🟢"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s **chitin %s** — %s\n", marker, n.EventType, n.Reason)
	if n.PRTitle != "" {
		fmt.Fprintf(&b, "PR #%d: %s\n", n.PRNumber, n.PRTitle)
	} else {
		fmt.Fprintf(&b, "PR #%d\n", n.PRNumber)
	}
	if n.Detail != "" {
		fmt.Fprintf(&b, "%s\n", n.Detail)
	}
	fmt.Fprintf(&b, "👉 %s", n.PRURL)
	return b.String()
}

// resolveDiscordWebhookURL returns the operator's webhook URL — env var
// first, then the secret file at $HOME/.chitin/discord-webhook.secret.
// Empty string means "no Discord configured"; the helper degrades silently.
func resolveDiscordWebhookURL() string {
	if env := strings.TrimSpace(os.Getenv(discordEscalationEnv)); env != "" {
		return env
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	raw, err := os.ReadFile(home + "/" + discordWebhookSecretFile)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(raw))
}

// warnDiscordEscalation logs a Discord-notify warning to stderr — matches
// the spec 080 contract that notifications never propagate as workflow
// errors.
func warnDiscordEscalation(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "warning: discord escalation notify: "+format+"\n", args...)
}
