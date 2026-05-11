package gemini

import (
	"bytes"
	"strings"
	"testing"

	"github.com/chitinhq/chitin/go/execution-kernel/internal/gov"
)

func TestFormat_DenyDelegatesToCodexShape(t *testing.T) {
	// Gemini's Format delegates to codex.Format — verify the wire shape
	// matches (stdout JSON + stderr text on deny). If gemini ever needs
	// its own shape, this test will catch the divergence.
	var stdout, stderr bytes.Buffer
	code := Format(gov.Decision{
		Allowed: false,
		RuleID:  "test-rule",
		Reason:  "test reason",
	}, &stdout, &stderr)

	if code != 2 {
		t.Fatalf("exit code: got %d want 2", code)
	}
	if !strings.Contains(stdout.String(), `"decision":"block"`) {
		t.Errorf("stdout must contain block JSON, got: %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "chitin:") {
		t.Errorf("stderr must contain chitin reason, got: %q", stderr.String())
	}
}

func TestFormat_AllowEmitsNothing(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Format(gov.Decision{Allowed: true}, &stdout, &stderr)
	if code != 0 {
		t.Errorf("exit code: got %d want 0", code)
	}
	if stdout.Len() != 0 || stderr.Len() != 0 {
		t.Errorf("allow must produce no output, got stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}
