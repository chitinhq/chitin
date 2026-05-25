package activities

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

// NotificationKind classifies a NotificationEvent — the closed set of
// orchestrator events worth surfacing to a human (spec 080 FR-005).
type NotificationKind string

const (
	// NotifyWorkUnitSettled — a work unit finished (done or failed).
	NotifyWorkUnitSettled NotificationKind = "work-unit-settled"
	// NotifyPROpened — a work unit opened a pull request.
	NotifyPROpened NotificationKind = "pr-opened"
	// NotifyNodeBlocked — a node became blocked-unroutable.
	NotifyNodeBlocked NotificationKind = "node-blocked"
	// NotifyRunTerminal — a scheduler run reached a terminal state.
	NotifyRunTerminal NotificationKind = "run-terminal"
	// NotifyOperatorDigest — a scheduled rollup post whose Summary is already
	// fully-formatted Discord markdown. line() returns it verbatim so the
	// markdown table renders intact (spec 114 US2 FR-009).
	NotifyOperatorDigest NotificationKind = "operator-digest"
)

// NotificationEvent is one write-only event the orchestrator surfaces to the
// human notification channel (spec 080 US2). It is purely descriptive — posted
// out, never read back — and carries no scheduling state.
type NotificationEvent struct {
	// Kind is the event class.
	Kind NotificationKind `json:"kind"`
	// RunID is the scheduler run the event belongs to.
	RunID string `json:"run_id"`
	// NodeID is the work unit / node the event concerns, when applicable.
	NodeID string `json:"node_id,omitempty"`
	// Summary is the human-readable one-line account of the event.
	Summary string `json:"summary"`
	// URL is a reference the event points at — a PR URL — when applicable.
	URL string `json:"url,omitempty"`
}

// line renders the event as a single human-readable notification line.
func (e NotificationEvent) line() string {
	if e.Kind == NotifyOperatorDigest {
		// Digest mode: Summary is already a complete Discord markdown body
		// (the spec 114 queue render). Post it verbatim so the table renders
		// — a "[chitin] operator-digest — ..." prefix would corrupt the
		// markdown.
		return e.Summary
	}
	var b strings.Builder
	fmt.Fprintf(&b, "[chitin] %s", e.Kind)
	if e.RunID != "" {
		fmt.Fprintf(&b, " · run %s", e.RunID)
	}
	if e.NodeID != "" {
		fmt.Fprintf(&b, " · %s", e.NodeID)
	}
	if e.Summary != "" {
		fmt.Fprintf(&b, " — %s", e.Summary)
	}
	if e.URL != "" {
		fmt.Fprintf(&b, "\n%s", e.URL)
	}
	return b.String()
}

// Notifier is the write-only sink for orchestrator notification events. It is
// an INTERFACE so the workflows never hard-depend on Discord; the default sink
// logs.
type Notifier interface {
	// Notify posts one event. It MUST NOT return an error that could fail a
	// workflow — a notification is strictly best-effort (spec 080 FR-007).
	Notify(ctx context.Context, ev NotificationEvent) error
}

// logNotifier is the fallback Notifier — it logs each event rather than posting
// to Discord. It is the safe default when no webhook is configured.
type logNotifier struct{}

// Notify logs one event. It never returns an error.
func (logNotifier) Notify(_ context.Context, ev NotificationEvent) error {
	log.Printf("notify: (no webhook) %s", strings.ReplaceAll(ev.line(), "\n", " "))
	return nil
}

// NewLogNotifier returns the fallback logging Notifier.
func NewLogNotifier() Notifier { return logNotifier{} }

// discordContentLimit is Discord's hard cap on a webhook message's content,
// counted in Unicode characters.
const discordContentLimit = 2000

// DiscordNotifier posts notification events to a Discord incoming webhook
// (spec 080 US2). It is write-only: Notify POSTs and never reads back. Every
// failure mode — an unset, malformed, unreachable, or rate-limited webhook —
// degrades to a logged no-op; Notify always returns nil, so a Discord outage
// can never fail or stall a workflow (spec 080 FR-007).
type DiscordNotifier struct {
	webhookURL string
	client     *http.Client
}

// NewDiscordNotifier returns a Discord Notifier posting to webhookURL. An empty
// URL yields a notifier whose Notify logs and drops — the orchestrator runs
// notification-disabled rather than failing.
func NewDiscordNotifier(webhookURL string) *DiscordNotifier {
	return &DiscordNotifier{
		webhookURL: strings.TrimSpace(webhookURL),
		client:     &http.Client{Timeout: 10 * time.Second},
	}
}

// NewDiscordNotifierFromEnv builds a Discord notifier from the
// CHITIN_DISCORD_WEBHOOK_URL environment variable. An unset variable yields the
// logged-no-op notifier — the standard wiring used by main.
func NewDiscordNotifierFromEnv() *DiscordNotifier {
	return NewDiscordNotifier(os.Getenv("CHITIN_DISCORD_WEBHOOK_URL"))
}

// Notify posts one event to the Discord webhook. It always returns nil: a
// missing webhook, an encoding fault, an unreachable endpoint, or a non-2xx
// response is logged and dropped (spec 080 FR-007).
func (d *DiscordNotifier) Notify(ctx context.Context, ev NotificationEvent) error {
	if d == nil || d.webhookURL == "" {
		log.Printf("notify: (no webhook) %s", strings.ReplaceAll(ev.line(), "\n", " "))
		return nil
	}

	// Discord caps content at discordContentLimit characters; truncate by rune
	// so a multi-byte character is never split.
	content := ev.line()
	if r := []rune(content); len(r) > discordContentLimit {
		content = string(r[:discordContentLimit-1]) + "…"
	}

	payload, err := json.Marshal(map[string]string{"content": content})
	if err != nil {
		log.Printf("notify: encoding event %s: %v", ev.Kind, err)
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.webhookURL, bytes.NewReader(payload))
	if err != nil {
		log.Printf("notify: building request for %s: %v", ev.Kind, err)
		return nil
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := d.client.Do(req)
	if err != nil {
		log.Printf("notify: posting %s to Discord: %v", ev.Kind, err)
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		log.Printf("notify: Discord returned HTTP %d for event %s", resp.StatusCode, ev.Kind)
	}
	return nil
}

// DiscordNotifyActivityName is the stable Temporal activity name DiscordNotify
// registers under. Exported so workflows dispatch by reference instead of
// duplicating the string literal — same convention as the review subpackage's
// SelectReviewersActivityName et al.
const DiscordNotifyActivityName = "DiscordNotify"

// DiscordNotify is the notification activity (spec 080 US2, FR-005). Posting to
// Discord is network I/O — a SIDE EFFECT — so it MUST run in an activity, never
// in workflow code.
type DiscordNotify struct {
	notifier Notifier
}

// NewDiscordNotify returns a DiscordNotify activity bound to notifier. A nil
// notifier falls back to the logging notifier so the activity is always usable.
func NewDiscordNotify(notifier Notifier) *DiscordNotify {
	if notifier == nil {
		notifier = NewLogNotifier()
	}
	return &DiscordNotify{notifier: notifier}
}

// ActivityName is the stable Temporal activity name DiscordNotify registers
// under and the workflows dispatch to.
func (a *DiscordNotify) ActivityName() string { return DiscordNotifyActivityName }

// Execute posts one notification event. It ALWAYS returns nil — a notification
// fault must never fail the calling workflow (spec 080 FR-007).
func (a *DiscordNotify) Execute(ctx context.Context, ev NotificationEvent) error {
	if a.notifier == nil {
		log.Printf("notify: DiscordNotify has no notifier bound; dropping %s", ev.Kind)
		return nil
	}
	_ = a.notifier.Notify(ctx, ev)
	return nil
}
