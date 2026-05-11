package driver

import (
	"bytes"
	"strings"
	"testing"

	"github.com/chitinhq/chitin/go/execution-kernel/internal/gov"
)

func TestFormatFor_ClaudeCode_DenyEmitsStdoutOnly(t *testing.T) {
	f := FormatFor("claude-code")
	var stdout, stderr bytes.Buffer
	code := f(gov.Decision{Allowed: false, RuleID: "demo-deny", Reason: "no"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("exit code: got %d want 2", code)
	}
	if !strings.Contains(stdout.String(), `"decision":"block"`) {
		t.Errorf("stdout missing block JSON: %q", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr should be empty for claude-code, got: %q", stderr.String())
	}
}

func TestFormatFor_Codex_DenyEmitsStderrAndStdout(t *testing.T) {
	f := FormatFor("codex")
	var stdout, stderr bytes.Buffer
	code := f(gov.Decision{Allowed: false, RuleID: "demo-deny", Reason: "no"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("exit code: got %d want 2", code)
	}
	if !strings.Contains(stdout.String(), `"decision":"block"`) {
		t.Errorf("stdout missing block JSON: %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "chitin:") {
		t.Errorf("stderr must contain chitin reason for codex ABI, got: %q", stderr.String())
	}
	if !strings.Contains(stderr.String(), "demo-deny") {
		t.Errorf("stderr must include rule_id, got: %q", stderr.String())
	}
}

func TestFormatFor_Gemini_DenyEmitsStderrAndStdout(t *testing.T) {
	f := FormatFor("gemini")
	var stdout, stderr bytes.Buffer
	code := f(gov.Decision{Allowed: false, RuleID: "demo-deny", Reason: "no"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("exit code: got %d want 2", code)
	}
	if !strings.Contains(stderr.String(), "chitin:") {
		t.Errorf("stderr must contain chitin reason for gemini, got: %q", stderr.String())
	}
}

func TestFormatFor_Unknown_FallsBackToClaudeCode(t *testing.T) {
	f := FormatFor("unknown-driver")
	var stdout, stderr bytes.Buffer
	code := f(gov.Decision{Allowed: false, RuleID: "demo-deny", Reason: "no"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("exit code: got %d want 2", code)
	}
	if stderr.Len() != 0 {
		t.Errorf("unknown driver falls back to claude-code shape (stdout-only); stderr should be empty, got: %q", stderr.String())
	}
}

func TestFormatFor_Allow_NoOutput(t *testing.T) {
	for _, agent := range []string{"claude-code", "codex", "gemini", "unknown"} {
		t.Run(agent, func(t *testing.T) {
			f := FormatFor(agent)
			var stdout, stderr bytes.Buffer
			code := f(gov.Decision{Allowed: true}, &stdout, &stderr)
			if code != 0 {
				t.Errorf("exit code: got %d want 0", code)
			}
			if stdout.Len() != 0 || stderr.Len() != 0 {
				t.Errorf("allow should produce no output, got stdout=%q stderr=%q", stdout.String(), stderr.String())
			}
		})
	}
}
