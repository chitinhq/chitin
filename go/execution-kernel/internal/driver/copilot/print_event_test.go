package copilot

import (
	"bytes"
	"strings"
	"testing"

	copilotsdk "github.com/github/copilot-sdk/go"
)

func TestPrintEvent_AssistantMessage_NonEmpty(t *testing.T) {
	evt := copilotsdk.SessionEvent{
		Type: copilotsdk.SessionEventTypeAssistantMessage,
		Data: &copilotsdk.AssistantMessageData{Content: "hello world"},
	}
	var buf bytes.Buffer
	if !PrintEvent(&buf, evt) {
		t.Fatalf("expected true for non-empty assistant message")
	}
	if !strings.Contains(buf.String(), "hello world") {
		t.Fatalf("missing content, got %q", buf.String())
	}
}

func TestPrintEvent_AssistantMessage_EmptyContentSkipped(t *testing.T) {
	// Tool-request-only messages carry no text; they must not print a blank line.
	evt := copilotsdk.SessionEvent{
		Type: copilotsdk.SessionEventTypeAssistantMessage,
		Data: &copilotsdk.AssistantMessageData{Content: ""},
	}
	var buf bytes.Buffer
	if PrintEvent(&buf, evt) {
		t.Fatalf("expected false for empty content")
	}
	if buf.Len() != 0 {
		t.Fatalf("expected no output, got %q", buf.String())
	}
}

func TestPrintEvent_ToolExecutionStart_BashCommand(t *testing.T) {
	evt := copilotsdk.SessionEvent{
		Type: copilotsdk.SessionEventTypeToolExecutionStart,
		Data: &copilotsdk.ToolExecutionStartData{
			ToolName:  "bash",
			Arguments: map[string]any{"command": "ls /tmp", "description": "list tmp"},
		},
	}
	var buf bytes.Buffer
	if !PrintEvent(&buf, evt) {
		t.Fatalf("expected true for tool start")
	}
	out := buf.String()
	if !strings.Contains(out, "bash") || !strings.Contains(out, "ls /tmp") {
		t.Fatalf("missing tool banner, got %q", out)
	}
}

func TestPrintEvent_ToolExecutionStart_NoArgs(t *testing.T) {
	evt := copilotsdk.SessionEvent{
		Type: copilotsdk.SessionEventTypeToolExecutionStart,
		Data: &copilotsdk.ToolExecutionStartData{ToolName: "list_skills"},
	}
	var buf bytes.Buffer
	if !PrintEvent(&buf, evt) {
		t.Fatalf("expected true for argless tool start")
	}
	if !strings.Contains(buf.String(), "list_skills") {
		t.Fatalf("missing tool name, got %q", buf.String())
	}
}

func TestPrintEvent_ToolExecutionStart_PathArg(t *testing.T) {
	evt := copilotsdk.SessionEvent{
		Type: copilotsdk.SessionEventTypeToolExecutionStart,
		Data: &copilotsdk.ToolExecutionStartData{
			ToolName:  "read",
			Arguments: map[string]any{"path": "/etc/hosts"},
		},
	}
	var buf bytes.Buffer
	PrintEvent(&buf, evt)
	if !strings.Contains(buf.String(), "/etc/hosts") {
		t.Fatalf("expected path surfaced, got %q", buf.String())
	}
}

func TestPrintEvent_ToolExecutionComplete_Success(t *testing.T) {
	evt := copilotsdk.SessionEvent{
		Type: copilotsdk.SessionEventTypeToolExecutionComplete,
		Data: &copilotsdk.ToolExecutionCompleteData{
			Success: true,
			Result:  &copilotsdk.ToolExecutionCompleteDataResult{Content: "file-a\nfile-b\n"},
		},
	}
	var buf bytes.Buffer
	if !PrintEvent(&buf, evt) {
		t.Fatalf("expected true for successful result")
	}
	out := buf.String()
	if !strings.Contains(out, "file-a") || !strings.Contains(out, "file-b") {
		t.Fatalf("missing result content, got %q", out)
	}
}

func TestPrintEvent_ToolExecutionComplete_DetailedContentPreferred(t *testing.T) {
	detailed := "full diff with all hunks\n+changed line\n"
	evt := copilotsdk.SessionEvent{
		Type: copilotsdk.SessionEventTypeToolExecutionComplete,
		Data: &copilotsdk.ToolExecutionCompleteData{
			Success: true,
			Result: &copilotsdk.ToolExecutionCompleteDataResult{
				Content:         "truncated summary",
				DetailedContent: &detailed,
			},
		},
	}
	var buf bytes.Buffer
	PrintEvent(&buf, evt)
	out := buf.String()
	if !strings.Contains(out, "+changed line") {
		t.Fatalf("expected detailed content preferred, got %q", out)
	}
	if strings.Contains(out, "truncated summary") {
		t.Fatalf("expected truncated summary suppressed when detailed present")
	}
}

func TestPrintEvent_ToolExecutionComplete_SuccessEmptyContentSkipped(t *testing.T) {
	evt := copilotsdk.SessionEvent{
		Type: copilotsdk.SessionEventTypeToolExecutionComplete,
		Data: &copilotsdk.ToolExecutionCompleteData{
			Success: true,
			Result:  &copilotsdk.ToolExecutionCompleteDataResult{Content: ""},
		},
	}
	var buf bytes.Buffer
	if PrintEvent(&buf, evt) {
		t.Fatalf("expected false for empty result content")
	}
	if buf.Len() != 0 {
		t.Fatalf("expected no output, got %q", buf.String())
	}
}

func TestPrintEvent_ToolExecutionComplete_FailureWithError(t *testing.T) {
	evt := copilotsdk.SessionEvent{
		Type: copilotsdk.SessionEventTypeToolExecutionComplete,
		Data: &copilotsdk.ToolExecutionCompleteData{
			Success: false,
			Error:   &copilotsdk.ToolExecutionCompleteDataError{Message: "permission denied"},
		},
	}
	var buf bytes.Buffer
	if !PrintEvent(&buf, evt) {
		t.Fatalf("expected true for failure with message")
	}
	if !strings.Contains(buf.String(), "permission denied") {
		t.Fatalf("missing error message, got %q", buf.String())
	}
}

func TestPrintEvent_ToolExecutionComplete_FailureNilErrorStillAnnounces(t *testing.T) {
	evt := copilotsdk.SessionEvent{
		Type: copilotsdk.SessionEventTypeToolExecutionComplete,
		Data: &copilotsdk.ToolExecutionCompleteData{Success: false},
	}
	var buf bytes.Buffer
	if !PrintEvent(&buf, evt) {
		t.Fatalf("expected true for failure even without error payload")
	}
	if buf.Len() == 0 {
		t.Fatalf("expected fallback failure message, got empty")
	}
}

func TestPrintEvent_UnrecognizedEventSuppressed(t *testing.T) {
	// Session-protocol events (turn markers, usage info, etc.) must not
	// appear on stage — the runbook demo would drown in them.
	for _, ty := range []copilotsdk.SessionEventType{
		copilotsdk.SessionEventTypeAssistantTurnStart,
		copilotsdk.SessionEventTypeAssistantTurnEnd,
		copilotsdk.SessionEventTypeAssistantUsage,
		copilotsdk.SessionEventTypeAssistantStreamingDelta,
		copilotsdk.SessionEventTypeAssistantReasoning,
	} {
		var buf bytes.Buffer
		if PrintEvent(&buf, copilotsdk.SessionEvent{Type: ty}) {
			t.Fatalf("expected false for %s", ty)
		}
		if buf.Len() != 0 {
			t.Fatalf("expected silence for %s, got %q", ty, buf.String())
		}
	}
}

func TestPrintEvent_NilDataDefensive(t *testing.T) {
	// Should never happen in practice, but a nil Data must not panic.
	for _, ty := range []copilotsdk.SessionEventType{
		copilotsdk.SessionEventTypeAssistantMessage,
		copilotsdk.SessionEventTypeToolExecutionStart,
		copilotsdk.SessionEventTypeToolExecutionComplete,
	} {
		var buf bytes.Buffer
		if PrintEvent(&buf, copilotsdk.SessionEvent{Type: ty}) {
			t.Fatalf("expected false for nil-data %s", ty)
		}
	}
}

func TestSummarizeArgs_TruncatesLongJSON(t *testing.T) {
	// Anything beyond 120 chars gets an ellipsis so the stage banner stays on one line.
	args := map[string]any{"payload": strings.Repeat("x", 500)}
	s := summarizeArgs(args)
	if len(s) > 130 {
		t.Fatalf("expected truncation, got len=%d", len(s))
	}
	if !strings.HasSuffix(s, "…") {
		t.Fatalf("expected ellipsis suffix, got %q", s)
	}
}

func TestSummarizeArgs_NilYieldsEmpty(t *testing.T) {
	if s := summarizeArgs(nil); s != "" {
		t.Fatalf("expected empty string for nil args, got %q", s)
	}
}
