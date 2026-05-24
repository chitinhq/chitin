package claudecode

import (
	"errors"
	"fmt"
	"strings"

	"github.com/chitinhq/chitin/go/orchestrator/driver"
)

// reviewToolName is the value the orchestrator sets on WorkUnit.Tool when
// dispatching a review-mode invocation. Spec 094 FR-002 / spec 109 — the
// driver self-declares the same string in its CapabilityCard.ReviewMode.
const reviewToolName = "review"

// reviewPromptFor builds the review-mode prompt for one PR snapshot. It
// embeds the StructuredVerdict JSON schema and an example so the model
// commits to a single output shape, and ends with the explicit
// "Return ONLY a JSON document …" instruction. The driver author owns
// this prompt per spec 094 FR-003; the orchestrator does not prescribe it.
//
// T001 may land a richer template; the contract (schema + example +
// closing instruction + spec 094 cite) is the load-bearing part.
func reviewPromptFor(wu driver.WorkUnit) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Chitin review request: work unit %s\n", wu.ID)
	if wu.SpecID != "" {
		fmt.Fprintf(&b, "Spec: %s\n", wu.SpecID)
	}
	if wu.TaskID != "" {
		fmt.Fprintf(&b, "Task: %s\n", wu.TaskID)
	}
	if wu.WorktreePath != "" {
		fmt.Fprintf(&b, "Worktree: %s\n", wu.WorktreePath)
	}
	b.WriteString("\nReview context:\n")
	b.WriteString(wu.Context)
	b.WriteString("\n\n")
	b.WriteString("Your output MUST conform to the StructuredVerdict contract ")
	b.WriteString("defined in specs/094-pr-review-mechanism/contracts/structured-verdict-schema.md.\n\n")
	b.WriteString("JSON schema:\n")
	b.WriteString("{\n")
	b.WriteString("  \"verdict\": \"approve | approve-with-comments | request-changes | abstain\",\n")
	b.WriteString("  \"concerns\": [\"<non-empty string>\", ...],\n")
	b.WriteString("  \"recommendations\": [\"<non-empty string>\", ...],\n")
	b.WriteString("  \"blockers\": [\"<non-empty string>\", ...],\n")
	b.WriteString("  \"reason\": \"<optional free-text, used with abstain>\"\n")
	b.WriteString("}\n\n")
	b.WriteString("Per-verdict invariants (rejected if violated):\n")
	b.WriteString("- approve              ⇒ blockers MUST be empty.\n")
	b.WriteString("- approve-with-comments ⇒ blockers empty AND at least one concern or recommendation.\n")
	b.WriteString("- request-changes      ⇒ blockers MUST be non-empty.\n")
	b.WriteString("- abstain              ⇒ concerns, recommendations, and blockers ALL empty.\n\n")
	b.WriteString("Example:\n")
	b.WriteString("{\"verdict\":\"approve-with-comments\",\"concerns\":[\"The new lint rule may false-positive on legacy specs\"],\"recommendations\":[\"Add an opt-out marker for grandfathered specs\"],\"blockers\":[]}\n\n")
	b.WriteString("Return ONLY a JSON document matching this schema. No markdown, no prose, no commentary outside the JSON.\n")
	return b.String()
}

// extractVerdictJSON pulls the JSON document out of a model's raw stdout.
// Strategy (in order):
//
//  1. Strip surrounding markdown code fences (```json ... ``` or ``` ... ```).
//  2. Extract the largest balanced {...} block in the remaining text.
//  3. If no balanced {...} block exists, return the raw input — the caller
//     surfaces it through json.Unmarshal as a malformed_verdict.
//
// Returns an error only when the input is entirely empty; in every other
// case it returns the best-effort JSON substring so the caller can run
// json.Unmarshal + verdict.Validate and surface a typed failure.
func extractVerdictJSON(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", errors.New("empty output")
	}
	stripped := stripCodeFences(trimmed)
	if block, ok := largestBalancedObject(stripped); ok {
		return block, nil
	}
	return stripped, nil
}

// stripCodeFences removes a single surrounding ```…``` fence if present.
// It handles the common ```json prefix and a bare ``` prefix, and tolerates
// trailing whitespace after the closing fence.
func stripCodeFences(s string) string {
	t := strings.TrimSpace(s)
	if !strings.HasPrefix(t, "```") {
		return t
	}
	// Drop the opening fence line (e.g. "```json\n" or "```\n").
	t = strings.TrimPrefix(t, "```")
	if nl := strings.IndexByte(t, '\n'); nl >= 0 {
		t = t[nl+1:]
	}
	// Drop the closing fence and anything trailing it.
	if end := strings.LastIndex(t, "```"); end >= 0 {
		t = t[:end]
	}
	return strings.TrimSpace(t)
}

// largestBalancedObject scans s for top-level balanced {…} runs and
// returns the longest one. Brace counting honors JSON string literals so a
// "{" inside a string does not unbalance the scan. Backslash escapes inside
// strings are honored.
func largestBalancedObject(s string) (string, bool) {
	var best string
	var found bool
	for i := 0; i < len(s); i++ {
		if s[i] != '{' {
			continue
		}
		end, ok := scanBalancedObject(s, i)
		if !ok {
			continue
		}
		block := s[i:end]
		if len(block) > len(best) {
			best = block
			found = true
		}
		i = end - 1
	}
	return best, found
}

// scanBalancedObject returns the index one past the matching '}' for the
// '{' at s[start]. Returns ok=false if no match is found.
func scanBalancedObject(s string, start int) (int, bool) {
	depth := 0
	inString := false
	escaped := false
	for i := start; i < len(s); i++ {
		c := s[i]
		if inString {
			if escaped {
				escaped = false
				continue
			}
			switch c {
			case '\\':
				escaped = true
			case '"':
				inString = false
			}
			continue
		}
		switch c {
		case '"':
			inString = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i + 1, true
			}
		}
	}
	return 0, false
}
