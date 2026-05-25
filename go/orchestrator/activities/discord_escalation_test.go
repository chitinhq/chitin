package activities

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
)

// TestBuildDiscordEscalationBody_AlertShape asserts the markdown content
// for a typical alert: 🚨 marker, event_type + reason in the header line,
// PR number + title, detail line, clickable link.
func TestBuildDiscordEscalationBody_AlertShape(t *testing.T) {
	body := buildDiscordEscalationBody(EscalationNotice{
		EventType: "sibling_rebase_failed",
		Severity:  SeverityAlert,
		PRNumber:  1234,
		PRTitle:   "spec(110) T002 codex review-mode prompt",
		PRURL:     "https://github.com/chitinhq/chitin/pull/1234",
		Reason:    "sibling_rebase_failed",
		Detail:    "rebase onto origin/main produced 1 conflicting file(s)",
	})

	for _, want := range []string{
		"🚨",                                                          // alert marker
		"sibling_rebase_failed",                                      // event type AND reason
		"PR #1234",                                                   // PR number
		"spec(110) T002 codex review-mode prompt",                    // title
		"rebase onto origin/main produced 1 conflicting file(s)",     // detail
		"https://github.com/chitinhq/chitin/pull/1234",               // link
		"👉",                                                         // clickable marker
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q\nbody:\n%s", want, body)
		}
	}
}

// TestBuildDiscordEscalationBody_ReadyShape asserts the positive ping —
// 🟢 marker instead of 🚨, same surrounding shape.
func TestBuildDiscordEscalationBody_ReadyShape(t *testing.T) {
	body := buildDiscordEscalationBody(EscalationNotice{
		EventType: "ready_to_merge",
		Severity:  SeverityReady,
		PRNumber:  9999,
		PRTitle:   "feat(some): thing",
		PRURL:     "https://github.com/chitinhq/chitin/pull/9999",
		Reason:    "all comments addressed",
		Detail:    "internal re-review approved with high confidence",
	})
	if !strings.Contains(body, "🟢") {
		t.Errorf("ready notice missing 🟢 marker, got:\n%s", body)
	}
	if strings.Contains(body, "🚨") {
		t.Errorf("ready notice should NOT carry 🚨 alert marker, got:\n%s", body)
	}
}

// TestBuildDiscordEscalationBody_OmitsBlankTitleAndDetail asserts the
// rendering gracefully omits sections when fields are empty.
func TestBuildDiscordEscalationBody_OmitsBlankTitleAndDetail(t *testing.T) {
	body := buildDiscordEscalationBody(EscalationNotice{
		EventType: "x",
		PRNumber:  1,
		PRURL:     "https://example.com/x",
		Reason:    "y",
	})
	// Should NOT contain a colon-and-empty-title line
	if strings.Contains(body, "PR #1: ") {
		t.Errorf("body should not render an empty title section, got:\n%s", body)
	}
	if !strings.Contains(body, "PR #1\n") {
		t.Errorf("body should render bare 'PR #1' line when title is empty, got:\n%s", body)
	}
}

// TestNotifyDiscordEscalation_PostsToWebhook asserts the helper actually
// POSTs to the configured webhook with the expected JSON body shape.
// Uses httptest.Server in place of Discord; env var overrides the secret
// file lookup so the test is hermetic.
func TestNotifyDiscordEscalation_PostsToWebhook(t *testing.T) {
	var (
		mu           sync.Mutex
		receivedBody []byte
		receivedCT   string
		callCount    int
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		callCount++
		receivedCT = r.Header.Get("Content-Type")
		receivedBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	t.Setenv(discordEscalationEnv, srv.URL)

	notifyDiscordEscalation(context.Background(), EscalationNotice{
		EventType: "sibling_rebase_failed",
		Severity:  SeverityAlert,
		PRNumber:  1234,
		PRTitle:   "test title",
		PRURL:     "https://github.com/o/r/pull/1234",
		Reason:    "sibling_rebase_failed",
		Detail:    "test detail",
	})

	mu.Lock()
	defer mu.Unlock()
	if callCount != 1 {
		t.Fatalf("expected exactly 1 POST, got %d", callCount)
	}
	if receivedCT != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", receivedCT)
	}
	var payload struct {
		Content string `json:"content"`
	}
	if err := json.Unmarshal(receivedBody, &payload); err != nil {
		t.Fatalf("body not valid JSON: %v\nbody: %s", err, receivedBody)
	}
	if !strings.Contains(payload.Content, "PR #1234") {
		t.Errorf("payload.content missing PR #1234, got: %s", payload.Content)
	}
}

// TestNotifyDiscordEscalation_EmptyPRURLDrops asserts the guard: a notice
// without a PRURL is logged and dropped (no HTTP call).
func TestNotifyDiscordEscalation_EmptyPRURLDrops(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	t.Setenv(discordEscalationEnv, srv.URL)

	notifyDiscordEscalation(context.Background(), EscalationNotice{
		EventType: "x",
		PRNumber:  1,
		// PRURL deliberately empty
		Reason: "y",
	})

	if called {
		t.Error("notify should drop the message when PRURL is empty")
	}
}

// TestNotifyDiscordEscalation_NoWebhookConfiguredSilent asserts that with
// no env var AND no secret file, the helper degrades silently (no panic,
// no crash). Note: this test relies on $HOME not pointing at a real
// secret file path; we override HOME to t.TempDir() to guarantee no
// stray file at ~/.chitin/discord-webhook.secret leaks in.
func TestNotifyDiscordEscalation_NoWebhookConfiguredSilent(t *testing.T) {
	t.Setenv(discordEscalationEnv, "")
	t.Setenv("HOME", t.TempDir())
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("notify panicked with no webhook configured: %v", r)
		}
	}()
	notifyDiscordEscalation(context.Background(), EscalationNotice{
		EventType: "x",
		PRNumber:  1,
		PRURL:     "https://example.com",
		Reason:    "y",
	})
}

// TestNotifyDiscordEscalation_FromSecretFile asserts the secret-file
// fallback when the env var is unset. Writes the webhook URL to a fake
// HOME's .chitin/discord-webhook.secret and verifies the helper picks it up.
func TestNotifyDiscordEscalation_FromSecretFile(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	fakeHome := t.TempDir()
	if err := os.MkdirAll(fakeHome+"/.chitin", 0o700); err != nil {
		t.Fatalf("mkdir fake home/.chitin: %v", err)
	}
	if err := os.WriteFile(fakeHome+"/.chitin/discord-webhook.secret", []byte(srv.URL), 0o600); err != nil {
		t.Fatalf("write secret: %v", err)
	}
	t.Setenv(discordEscalationEnv, "") // ensure env-var path is skipped
	t.Setenv("HOME", fakeHome)

	notifyDiscordEscalation(context.Background(), EscalationNotice{
		EventType: "x",
		PRNumber:  1,
		PRURL:     "https://example.com",
		Reason:    "y",
	})

	if !called {
		t.Error("notify should have posted using the secret-file fallback")
	}
}
