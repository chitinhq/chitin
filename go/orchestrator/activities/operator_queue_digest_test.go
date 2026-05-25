package activities

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// stubRenderedDigest is the body the no-renderer fallback produces. The
// "✅"-style benign framing matches spec 114's "no PRs need attention" edge
// case so an operator seeing it during wire-up reads it as informational.
const stubDigestMarker = "queue rendering pending"

// TestOperatorQueueDigest_NilRendererUsesStub proves a nil renderer falls
// back to the in-package stub — the schedule + Discord plumbing remain
// verifiable before the spec 114 US1 queue subcommand lands.
func TestOperatorQueueDigest_NilRendererUsesStub(t *testing.T) {
	act := NewOperatorQueueDigest(nil)
	res, err := act.Execute(context.Background(), OperatorQueueDigestInput{Since: 24 * time.Hour})
	if err != nil {
		t.Fatalf("Execute returned %v, want nil", err)
	}
	if res.Window != 24*time.Hour {
		t.Errorf("res.Window = %v, want 24h", res.Window)
	}
	if !strings.Contains(res.Markdown, stubDigestMarker) {
		t.Errorf("stub markdown = %q, want it to mention %q", res.Markdown, stubDigestMarker)
	}
	if !strings.Contains(res.Markdown, "24h") {
		t.Errorf("stub markdown = %q, want it to echo the 24h window", res.Markdown)
	}
}

// fakeQueueRenderer is a concrete QueueRenderer for the activity tests — it
// records the duration the activity passed and returns a canned body or
// error so the call site is exercised end-to-end without importing the
// (not-yet-extant) queue package.
type fakeQueueRenderer struct {
	gotSince time.Duration
	body     string
	err      error
}

func (f *fakeQueueRenderer) Render(_ context.Context, since time.Duration) (string, error) {
	f.gotSince = since
	if f.err != nil {
		return "", f.err
	}
	return f.body, nil
}

// TestOperatorQueueDigest_PassesSinceToRenderer proves the activity threads
// the Since field from its input into QueueRenderer.Render unchanged — the
// workflow's 24h request reaches the renderer as 24h.
func TestOperatorQueueDigest_PassesSinceToRenderer(t *testing.T) {
	fake := &fakeQueueRenderer{body: "## queue\nrow\n"}
	act := NewOperatorQueueDigest(fake)

	res, err := act.Execute(context.Background(), OperatorQueueDigestInput{Since: 24 * time.Hour})
	if err != nil {
		t.Fatalf("Execute returned %v, want nil", err)
	}
	if fake.gotSince != 24*time.Hour {
		t.Errorf("renderer received since = %v, want 24h", fake.gotSince)
	}
	if res.Markdown != "## queue\nrow\n" {
		t.Errorf("res.Markdown = %q, want the renderer's body", res.Markdown)
	}
}

// TestOperatorQueueDigest_PropagatesRendererError proves a renderer fault is
// returned as an activity error — Temporal then retries per the workflow's
// RetryPolicy. The activity error return is reserved for genuine fault
// (network, IO), not for empty queues (which the renderer reports as the
// "✅ no PRs need attention" body, not an error).
func TestOperatorQueueDigest_PropagatesRendererError(t *testing.T) {
	want := errors.New("gh api: 502 bad gateway")
	fake := &fakeQueueRenderer{err: want}
	act := NewOperatorQueueDigest(fake)

	_, err := act.Execute(context.Background(), OperatorQueueDigestInput{Since: time.Hour})
	if err == nil {
		t.Fatal("Execute returned nil, want a wrapped renderer error")
	}
	if !errors.Is(err, want) {
		t.Errorf("Execute returned %v, want errors.Is == %v", err, want)
	}
}

// TestOperatorQueueDigest_ActivityName pins the stable Temporal activity
// name. The workflow's ExecuteActivity dispatches to this string, so a
// rename here without a matching workflow change would silently break the
// schedule's dispatch path.
func TestOperatorQueueDigest_ActivityName(t *testing.T) {
	if got := (&OperatorQueueDigest{}).ActivityName(); got != RenderOperatorQueueDigestActivityName {
		t.Errorf("ActivityName = %q, want %q", got, RenderOperatorQueueDigestActivityName)
	}
}

// TestOperatorQueueDigest_RejectsEmptyMarkdown proves the activity enforces
// the QueueRenderer contract: Render MUST NOT return an empty string on
// success. A buggy renderer that returned "" (or whitespace only) would
// otherwise reach DiscordNotify and post a blank message; the activity
// converts that into a wrapped error so Temporal retries and the bug is
// visible in workflow history.
func TestOperatorQueueDigest_RejectsEmptyMarkdown(t *testing.T) {
	for _, body := range []string{"", "   \n\t  "} {
		fake := &fakeQueueRenderer{body: body}
		act := NewOperatorQueueDigest(fake)

		_, err := act.Execute(context.Background(), OperatorQueueDigestInput{Since: time.Hour})
		if err == nil {
			t.Errorf("Execute(body=%q) returned nil error, want a contract-violation error", body)
		}
	}
}
