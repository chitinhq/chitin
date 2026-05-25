package codex

import (
	"errors"
	"fmt"
	"strings"

	"github.com/chitinhq/chitin/go/orchestrator/driver"
)

// reviewArgvFor builds the argv passed to the codex CLI for a review-mode
// invocation (spec 110 FR-001). The mandatory --skip-git-repo-check flag lets
// codex exec run inside the work unit's worktree without tripping the CLI's
// trusted-directory safety check, which otherwise fails the subprocess in
// ~130ms before any model call (spec 110 §Why).
//
// Non-review-mode invocations build argv inline in Driver.Invoke and MUST NOT
// pass this flag (FR-002): the trust check is the expected safety behaviour on
// local-driver implementation work.
func reviewArgvFor(wu driver.WorkUnit, model string) []string {
	return []string{"exec", "--skip-git-repo-check", "--model", model, reviewPromptFor(wu)}
}

// structuredVerdictContractURL is the canonical, repo-stable URL for the
// spec 094 StructuredVerdict JSON contract the model must emit. Inlined in
// the review-mode prompt so the model can self-reference the authoritative
// schema when it needs to disambiguate edge cases.
const structuredVerdictContractURL = "https://github.com/chitinhq/chitin/blob/main/.specify/specs/094-pr-review-mechanism/contracts/structured-verdict-schema.md"

// structuredVerdictSchema is the JSON Schema (draft 2020-12) that every
// reviewer driver's output MUST validate against. Kept verbatim in sync
// with .specify/specs/094-pr-review-mechanism/contracts/structured-verdict-schema.md.
// Embedded in the prompt so the model sees the exact closed shape it must
// produce — not a paraphrase.
const structuredVerdictSchema = `{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "$id": "https://chitinhq.github.io/chitin/schemas/structured-verdict-v1.json",
  "title": "StructuredVerdict",
  "type": "object",
  "additionalProperties": false,
  "required": ["verdict", "concerns", "recommendations", "blockers"],
  "properties": {
    "verdict": {
      "type": "string",
      "enum": ["approve", "approve-with-comments", "request-changes", "abstain"]
    },
    "concerns":        { "type": "array", "items": { "type": "string", "minLength": 1 } },
    "recommendations": { "type": "array", "items": { "type": "string", "minLength": 1 } },
    "blockers":        { "type": "array", "items": { "type": "string", "minLength": 1 } },
    "reason":          { "type": "string" }
  },
  "allOf": [
    { "if": { "properties": { "verdict": { "const": "approve" } } },
      "then": { "properties": { "blockers": { "maxItems": 0 } } } },
    { "if": { "properties": { "verdict": { "const": "approve-with-comments" } } },
      "then": {
        "properties": { "blockers": { "maxItems": 0 } },
        "anyOf": [
          { "properties": { "concerns":        { "minItems": 1 } } },
          { "properties": { "recommendations": { "minItems": 1 } } }
        ]
      } },
    { "if": { "properties": { "verdict": { "const": "request-changes" } } },
      "then": { "properties": { "blockers": { "minItems": 1 } } } },
    { "if": { "properties": { "verdict": { "const": "abstain" } } },
      "then": {
        "properties": {
          "concerns":        { "maxItems": 0 },
          "recommendations": { "maxItems": 0 },
          "blockers":        { "maxItems": 0 }
        }
      } }
  ]
}`

// structuredVerdictExample is a worked example of a valid
// approve-with-comments verdict, taken from the spec 094 contract.
const structuredVerdictExample = `{
  "verdict": "approve-with-comments",
  "concerns": ["The new RPC has no rate limit"],
  "recommendations": ["Consider adding a token bucket in v1.1"],
  "blockers": []
}`

// reviewPromptFor renders the review-mode prompt for a single work unit.
// Embeds the StructuredVerdict JSON schema, a worked example, and the
// explicit "Return ONLY a JSON document matching this schema" instruction
// required by spec 110 FR-003 (parity with spec 109 FR-001 / FR-002).
func reviewPromptFor(wu driver.WorkUnit) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Chitin review-mode work unit: %s\n", wu.ID)
	if wu.SpecID != "" {
		fmt.Fprintf(&b, "Spec: %s\n", wu.SpecID)
	}
	if wu.TaskID != "" {
		fmt.Fprintf(&b, "Task: %s\n", wu.TaskID)
	}
	if wu.WorktreePath != "" {
		fmt.Fprintf(&b, "Worktree: %s\n", wu.WorktreePath)
	}
	fmt.Fprintf(&b, "Contract: %s\n", structuredVerdictContractURL)

	b.WriteString("\nReview context:\n")
	b.WriteString(wu.Context)

	b.WriteString("\n\nOutput contract — StructuredVerdict (spec 094, JSON Schema draft 2020-12):\n")
	b.WriteString(structuredVerdictSchema)

	b.WriteString("\n\nExample of a valid StructuredVerdict:\n")
	b.WriteString(structuredVerdictExample)

	b.WriteString("\n\nReturn ONLY a JSON document matching this schema. No markdown, no prose, no commentary outside the JSON.")
	return b.String()
}

// errNoJSONFound signals that extractVerdictJSON could not locate a balanced
// {...} block in the input. Per spec 110 FR-004 (parity with spec 109
// FR-003 (c)), callers receive the original raw string alongside this error
// and surface it to the verdict activity as malformed output.
var errNoJSONFound = errors.New("no JSON-shaped substring in driver output")

// extractVerdictJSON pulls the StructuredVerdict body out of a model's raw
// stdout. Per spec 110 FR-004, applies in order:
//
//	(a) strip a surrounding ```json ... ``` (or ``` ... ```) markdown fence,
//	(b) return the LARGEST balanced {...} block found in the remaining text,
//	(c) fall back to the unmodified raw string when no balanced block exists.
//
// On (a)+(b) success the returned string is the extracted JSON document and
// err is nil. On (c) the returned string is the original raw input and err
// wraps errNoJSONFound.
func extractVerdictJSON(raw string) (string, error) {
	body := stripMarkdownFence(raw)
	if block, ok := largestBalancedBraces(body); ok {
		return block, nil
	}
	return raw, errNoJSONFound
}

// stripMarkdownFence removes a single outer ``` ... ``` (optionally
// language-tagged, e.g. ```json) fence if one wraps the trimmed input.
// Returns the input untouched when no such fence is present so the brace
// scanner can still find inline JSON.
func stripMarkdownFence(s string) string {
	trimmed := strings.TrimSpace(s)
	if !strings.HasPrefix(trimmed, "```") {
		return s
	}
	rest := strings.TrimPrefix(trimmed, "```")
	if nl := strings.IndexByte(rest, '\n'); nl >= 0 {
		firstLine := strings.TrimSpace(rest[:nl])
		if firstLine == "" || isLangTag(firstLine) {
			rest = rest[nl+1:]
		}
	}
	if idx := strings.LastIndex(rest, "```"); idx >= 0 {
		rest = rest[:idx]
	}
	return strings.TrimSpace(rest)
}

func isLangTag(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '-', r == '+':
		default:
			return false
		}
	}
	return true
}

// largestBalancedBraces scans s for top-level balanced {...} blocks,
// ignoring braces inside double-quoted JSON string literals, and returns
// the longest such block. Returns ("", false) when no balanced block exists.
func largestBalancedBraces(s string) (string, bool) {
	var (
		best     string
		depth    int
		startIdx = -1
		inString bool
		escape   bool
	)
	for i := 0; i < len(s); i++ {
		c := s[i]
		if escape {
			escape = false
			continue
		}
		if inString {
			switch c {
			case '\\':
				escape = true
			case '"':
				inString = false
			}
			continue
		}
		switch c {
		case '"':
			inString = true
		case '{':
			if depth == 0 {
				startIdx = i
			}
			depth++
		case '}':
			if depth == 0 {
				continue
			}
			depth--
			if depth == 0 && startIdx >= 0 {
				candidate := s[startIdx : i+1]
				if len(candidate) > len(best) {
					best = candidate
				}
				startIdx = -1
			}
		}
	}
	if best == "" {
		return "", false
	}
	return best, true
}
