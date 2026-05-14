package main

import (
	"strings"
	"testing"
)

func TestResolveCopilotPrompt_UsesArgsWhenPresent(t *testing.T) {
	prompt, err := resolveCopilotPrompt([]string{"fix", "the", "bug"}, false, strings.NewReader("ignored"))
	if err != nil {
		t.Fatalf("resolveCopilotPrompt returned error: %v", err)
	}
	if prompt != "fix the bug" {
		t.Fatalf("got %q, want joined argv prompt", prompt)
	}
}

func TestResolveCopilotPrompt_ReadsStdinWhenArgMissing(t *testing.T) {
	prompt, err := resolveCopilotPrompt(nil, false, strings.NewReader("stdin prompt"))
	if err != nil {
		t.Fatalf("resolveCopilotPrompt returned error: %v", err)
	}
	if prompt != "stdin prompt" {
		t.Fatalf("got %q, want stdin prompt", prompt)
	}
}

func TestResolveCopilotPrompt_RejectsEmptyNonInteractivePrompt(t *testing.T) {
	_, err := resolveCopilotPrompt(nil, false, strings.NewReader("   \n"))
	if err == nil {
		t.Fatal("expected empty stdin to be rejected")
	}
}

func TestResolveCopilotPrompt_AllowsInteractiveWithoutPrompt(t *testing.T) {
	prompt, err := resolveCopilotPrompt(nil, true, strings.NewReader(""))
	if err != nil {
		t.Fatalf("resolveCopilotPrompt returned error: %v", err)
	}
	if prompt != "" {
		t.Fatalf("got %q, want empty interactive prompt", prompt)
	}
}
