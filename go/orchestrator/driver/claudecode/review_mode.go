package claudecode

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/chitinhq/chitin/go/orchestrator/driver"
)

// reviewModeToolName is the canonical review-mode discriminator a driver
// honors on inbound WorkUnits (matches Card.ReviewMode.ToolName per spec 094
// FR-002).
const reviewModeToolName = "review"

// isReviewMode reports whether wu is a review-mode dispatch (spec 094) the
// driver must answer with a StructuredVerdict JSON document.
func isReviewMode(wu driver.WorkUnit) bool {
	return wu.TaskID == reviewModeToolName
}

// reviewPromptFor builds the review-mode prompt embedding the spec 094
// StructuredVerdict JSON schema, an example, and the explicit
// "Return ONLY a JSON document matching this schema" instruction.
//
// The orchestrator's DispatchMachineReviewer activity (spec 094) marshals
// the PRSnapshot + policy_class_hint into wu.Context as JSON; the prompt
// surfaces that payload verbatim under "Review input".
func reviewPromptFor(wu driver.WorkUnit) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Chitin review-mode invocation: %s\n", wu.ID)
	b.WriteString("Spec: 094 (PR review mechanism)\n")
	b.WriteString("Contract: docs/specs/094-pr-review-mechanism/contracts/review-mode-driver-contract.md\n")
	b.WriteString("\n")
	b.WriteString("You are a code reviewer rendering a structured verdict on the PR snapshot below.\n")
	b.WriteString("Emit one StructuredVerdict JSON document per the spec 094 schema.\n")
	b.WriteString("\n")
	b.WriteString("Schema:\n")
	b.WriteString("{\n")
	b.WriteString("  \"verdict\": one of \"approve\" | \"approve-with-comments\" | \"request-changes\" | \"abstain\",\n")
	b.WriteString("  \"concerns\": [string, ...],         // free-text observations that are not blockers\n")
	b.WriteString("  \"recommendations\": [string, ...],  // free-text suggestions for follow-up\n")
	b.WriteString("  \"blockers\": [string, ...],         // free-text reasons the verdict is request-changes\n")
	b.WriteString("  \"reason\": string                    // optional; abstain rationale only\n")
	b.WriteString("}\n")
	b.WriteString("\n")
	b.WriteString("Per-verdict invariants (the driver wrapper enforces these):\n")
	b.WriteString("  - approve              → blockers MUST be empty\n")
	b.WriteString("  - approve-with-comments → blockers MUST be empty AND at least one concern or recommendation\n")
	b.WriteString("  - request-changes      → blockers MUST be non-empty\n")
	b.WriteString("  - abstain              → concerns, recommendations, blockers all empty\n")
	b.WriteString("\n")
	b.WriteString("Example:\n")
	b.WriteString("{\n")
	b.WriteString("  \"verdict\": \"approve-with-comments\",\n")
	b.WriteString("  \"concerns\": [\"the new lint rule may false-positive on legacy specs\"],\n")
	b.WriteString("  \"recommendations\": [\"add an opt-out marker for grandfathered specs\"],\n")
	b.WriteString("  \"blockers\": []\n")
	b.WriteString("}\n")
	b.WriteString("\n")
	b.WriteString("Review input (JSON; PRSnapshot + policy_class_hint):\n")
	b.WriteString(wu.Context)
	b.WriteString("\n\n")
	b.WriteString("Return ONLY a JSON document matching this schema. No markdown, no prose, no commentary outside the JSON.\n")
	return b.String()
}

// fencedJSON matches a Markdown fenced block, with or without a "json"
// language tag, capturing the inner payload. Non-greedy on the body so
// successive fenced blocks are matched separately.
var fencedJSON = regexp.MustCompile("(?s)```(?:json)?\\s*\\n?(.*?)\\n?```")

// extractVerdictJSON returns the JSON document inside raw, applying:
//
//  1. Strip surrounding markdown fences (```json ... ``` or ``` ... ```).
//     If multiple fenced blocks exist, pick the largest one whose body
//     parses to a balanced {...} block.
//  2. Otherwise, extract the largest balanced {...} substring.
//  3. Otherwise, return the raw input unchanged with errNoJSONShape so the
//     caller can surface the raw output in the failure detail per spec 109
//     FR-004.
//
// The function does not call json.Unmarshal — it only extracts the candidate
// string. The caller decides whether to attempt parsing.
func extractVerdictJSON(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", errEmptyOutput
	}

	// (1) Try fenced blocks first; pick the largest balanced inner payload.
	if matches := fencedJSON.FindAllStringSubmatch(trimmed, -1); len(matches) > 0 {
		var best string
		for _, m := range matches {
			body := strings.TrimSpace(m[1])
			if balanced, ok := largestBalancedObject(body); ok && len(balanced) > len(best) {
				best = balanced
			}
		}
		if best != "" {
			return best, nil
		}
	}

	// (2) Fall back to the largest balanced {...} block in the raw string.
	if balanced, ok := largestBalancedObject(trimmed); ok {
		return balanced, nil
	}

	// (3) No JSON-shaped substring — return raw with sentinel.
	return raw, errNoJSONShape
}

// errEmptyOutput marks an empty stdout payload (model emitted nothing).
var errEmptyOutput = errors.New("extractVerdictJSON: empty output")

// errNoJSONShape marks raw output that contained no balanced {...} block.
var errNoJSONShape = errors.New("extractVerdictJSON: no JSON-shaped substring")

// largestBalancedObject returns the largest substring of s that is a
// balanced {...} block (brace-matched, respecting JSON string-literal
// escapes). Returns (block, true) on success or ("", false) if no balanced
// block exists.
//
// "Largest" is defined by character length, so a wrapper { ... { ... } ... }
// outranks any inner block.
func largestBalancedObject(s string) (string, bool) {
	var best string
	for i := 0; i < len(s); i++ {
		if s[i] != '{' {
			continue
		}
		if end, ok := scanBalancedObject(s, i); ok {
			block := s[i:end]
			if len(block) > len(best) {
				best = block
			}
		}
	}
	if best == "" {
		return "", false
	}
	return best, true
}

// scanBalancedObject scans s starting at i (which must be '{') and returns
// the index just past the matching '}', or (0, false) if no balanced match.
// Tracks JSON string literals so braces inside quoted strings don't count;
// honors backslash escapes inside strings.
func scanBalancedObject(s string, i int) (int, bool) {
	depth := 0
	inString := false
	escaped := false
	for j := i; j < len(s); j++ {
		c := s[j]
		if inString {
			if escaped {
				escaped = false
				continue
			}
			if c == '\\' {
				escaped = true
				continue
			}
			if c == '"' {
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
				return j + 1, true
			}
		}
	}
	return 0, false
}
