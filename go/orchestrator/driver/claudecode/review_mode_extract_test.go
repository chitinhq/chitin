package claudecode

import (
	"errors"
	"strings"
	"testing"
)

func TestExtractVerdictJSON_CleanJSON(t *testing.T) {
	in := `{"verdict":"approve","blockers":[],"comments":[],"summary":"lgtm"}`
	got, err := extractVerdictJSON(in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != in {
		t.Fatalf("got %q, want %q", got, in)
	}
}

func TestExtractVerdictJSON_StripsMarkdownFence(t *testing.T) {
	body := `{"verdict":"approve","blockers":[],"comments":[],"summary":"ok"}`
	in := "```json\n" + body + "\n```"
	got, err := extractVerdictJSON(in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != body {
		t.Fatalf("got %q, want %q", got, body)
	}
}

func TestExtractVerdictJSON_StripsFenceWithoutLanguageTag(t *testing.T) {
	body := `{"verdict":"abstain","blockers":[],"comments":[],"summary":""}`
	in := "```\n" + body + "\n```"
	got, err := extractVerdictJSON(in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != body {
		t.Fatalf("got %q, want %q", got, body)
	}
}

func TestExtractVerdictJSON_LargestOfMultipleBlocks(t *testing.T) {
	small := `{"a":1}`
	large := `{"verdict":"request_changes","blockers":[{"id":"b1","message":"x"}],"comments":[],"summary":"changes"}`
	in := "Some prose. " + small + " more prose. " + large + " trailing."
	got, err := extractVerdictJSON(in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != large {
		t.Fatalf("got %q, want largest %q", got, large)
	}
}

func TestExtractVerdictJSON_HandlesNestedAndStrings(t *testing.T) {
	// Braces inside string literals must not unbalance the scanner.
	body := `{"verdict":"approve","summary":"contains } and { chars","nested":{"a":1,"b":{"c":2}},"blockers":[],"comments":[]}`
	got, err := extractVerdictJSON(body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != body {
		t.Fatalf("got %q, want %q", got, body)
	}
}

func TestExtractVerdictJSON_EscapedQuoteInString(t *testing.T) {
	// Escaped quote inside a string must not close the string early
	// (which would expose a stray { in the prose to the scanner).
	body := `{"verdict":"approve","summary":"he said \"go\" {","blockers":[],"comments":[]}`
	got, err := extractVerdictJSON(body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != body {
		t.Fatalf("got %q, want %q", got, body)
	}
}

func TestExtractVerdictJSON_NoJSONReturnsRawWithError(t *testing.T) {
	in := "This is prose. The PR looks fine to me, ship it."
	got, err := extractVerdictJSON(in)
	if !errors.Is(err, errNoJSONFound) {
		t.Fatalf("err = %v, want errNoJSONFound", err)
	}
	if got != in {
		t.Fatalf("got %q, want raw %q", got, in)
	}
}

func TestExtractVerdictJSON_UnbalancedBracesReturnsRawWithError(t *testing.T) {
	in := `here is an open { brace with no close`
	got, err := extractVerdictJSON(in)
	if !errors.Is(err, errNoJSONFound) {
		t.Fatalf("err = %v, want errNoJSONFound", err)
	}
	if got != in {
		t.Fatalf("got %q, want raw %q", got, in)
	}
}

func TestExtractVerdictJSON_EmptyInput(t *testing.T) {
	got, err := extractVerdictJSON("")
	if !errors.Is(err, errNoJSONFound) {
		t.Fatalf("err = %v, want errNoJSONFound", err)
	}
	if got != "" {
		t.Fatalf("got %q, want empty", got)
	}
}

func TestExtractVerdictJSON_FenceWithProseAfterClose(t *testing.T) {
	// Closing fence midway through the buffer — content after it should
	// be dropped; the JSON inside the fence should still be extracted.
	body := `{"verdict":"approve","blockers":[],"comments":[],"summary":""}`
	in := "Here's my verdict:\n```json\n" + body + "\n```\nLet me know."
	got, err := extractVerdictJSON(in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// extractVerdictJSON only strips fences when they wrap the trimmed
	// input; otherwise it just runs the brace scanner on the original.
	// Either path should recover the body verbatim.
	if !strings.Contains(got, `"verdict":"approve"`) {
		t.Fatalf("got %q, missing verdict body", got)
	}
}
