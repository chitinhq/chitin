package activities

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestDiscordNotifier_PostsWhenConfigured proves a configured webhook receives
// the event as a Discord content payload.
func TestDiscordNotifier_PostsWhenConfigured(t *testing.T) {
	var gotContent string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var payload map[string]string
		_ = json.Unmarshal(body, &payload)
		gotContent = payload["content"]
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	err := NewDiscordNotifier(srv.URL).Notify(context.Background(), NotificationEvent{
		Kind: NotifyPROpened, RunID: "run-1", NodeID: "n1",
		Summary: "opened a PR", URL: "https://example.com/pr/1",
	})
	if err != nil {
		t.Fatalf("Notify: %v", err)
	}
	if !strings.Contains(gotContent, "pr-opened") || !strings.Contains(gotContent, "run-1") {
		t.Errorf("posted content = %q, want it to carry the event", gotContent)
	}
	if !strings.Contains(gotContent, "https://example.com/pr/1") {
		t.Errorf("posted content = %q, want it to carry the URL", gotContent)
	}
}

// TestDiscordNotifier_UnconfiguredIsNoOp proves an unset webhook degrades to a
// logged no-op that never errors.
func TestDiscordNotifier_UnconfiguredIsNoOp(t *testing.T) {
	if err := NewDiscordNotifier("").Notify(context.Background(),
		NotificationEvent{Kind: NotifyRunTerminal, RunID: "r"}); err != nil {
		t.Fatalf("Notify with no webhook returned %v, want nil", err)
	}
}

// TestDiscordNotifier_UnreachableEndpointNeverFaults proves an unreachable
// webhook is logged and dropped — Notify still returns nil (spec 080 FR-007).
func TestDiscordNotifier_UnreachableEndpointNeverFaults(t *testing.T) {
	// A server that is immediately closed: the URL is well-formed but dead.
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	deadURL := srv.URL
	srv.Close()

	if err := NewDiscordNotifier(deadURL).Notify(context.Background(),
		NotificationEvent{Kind: NotifyNodeBlocked, RunID: "r"}); err != nil {
		t.Fatalf("Notify to an unreachable endpoint returned %v, want nil", err)
	}
}

// TestDiscordNotifier_TruncatesOversizedContent proves content beyond Discord's
// character limit is truncated before posting.
func TestDiscordNotifier_TruncatesOversizedContent(t *testing.T) {
	gotLen := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var payload map[string]string
		_ = json.Unmarshal(body, &payload)
		gotLen = len([]rune(payload["content"]))
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	_ = NewDiscordNotifier(srv.URL).Notify(context.Background(), NotificationEvent{
		Kind: NotifyWorkUnitSettled, RunID: "r", Summary: strings.Repeat("x", 5000),
	})
	if gotLen == 0 || gotLen > discordContentLimit {
		t.Errorf("posted content = %d chars, want 0 < n <= %d", gotLen, discordContentLimit)
	}
}

// TestDiscordNotify_ExecuteAlwaysSucceeds proves the activity wrapper never
// returns an error, even with the logging fallback notifier.
func TestDiscordNotify_ExecuteAlwaysSucceeds(t *testing.T) {
	if err := NewDiscordNotify(nil).Execute(context.Background(),
		NotificationEvent{Kind: NotifyRunTerminal, RunID: "r"}); err != nil {
		t.Fatalf("Execute returned %v, want nil", err)
	}
}
