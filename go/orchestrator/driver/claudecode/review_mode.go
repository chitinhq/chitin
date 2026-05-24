package claudecode

import (
	"fmt"
	"strings"

	"github.com/chitinhq/chitin/go/orchestrator/driver"
)

// structuredVerdictSchema is the JSON Schema (draft 2020-12) the model is
// instructed to satisfy. It is the contract recorded in spec 094 at
// specs/094-pr-review-mechanism/contracts/structured-verdict-schema.md;
// the schema is duplicated here verbatim so the prompt is self-contained
// and the model never has to chase a link to learn the output shape.
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
      "then": { "properties": {
        "concerns":        { "maxItems": 0 },
        "recommendations": { "maxItems": 0 },
        "blockers":        { "maxItems": 0 }
      } } }
  ]
}`

// structuredVerdictExample is one worked example from the spec 094 contract
// — the approve-with-comments shape covers the most common reviewer move
// (endorse merging, flag follow-ups). The model sees a concrete instance
// in addition to the schema so its first response is structurally close to
// what the post-processor expects.
const structuredVerdictExample = `{
  "verdict": "approve-with-comments",
  "concerns": ["The new RPC has no rate limit"],
  "recommendations": ["Consider adding a token bucket in v1.1"],
  "blockers": []
}`

// structuredVerdictContractURL points at the authoritative spec 094 contract
// the prompt is built against. Cited per FR-002 so the model can self-
// reference the source-of-truth document if it has tool access to read it.
const structuredVerdictContractURL = "https://github.com/chitinhq/chitin/blob/main/specs/094-pr-review-mechanism/contracts/structured-verdict-schema.md"

// reviewPromptFor builds the review-mode prompt for a Claude Code
// invocation. The returned string carries the work unit's context first
// (what to review), then the StructuredVerdict contract: the spec 094
// reference URL, the JSON schema, one worked example, and the closing
// instruction that the response must be JSON only — no markdown, no prose.
//
// The closing instruction is the FR-001 verbatim string. The post-processor
// (T002) still strips fences and extracts the largest balanced object as a
// belt-and-braces defence, but the prompt's job is to make that defence
// rarely necessary.
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
	b.WriteString("\nReview context:\n")
	b.WriteString(wu.Context)
	b.WriteString("\n\n")

	b.WriteString("Output contract — StructuredVerdict (spec 094):\n")
	fmt.Fprintf(&b, "Contract: %s\n\n", structuredVerdictContractURL)

	b.WriteString("JSON Schema (draft 2020-12):\n")
	b.WriteString(structuredVerdictSchema)
	b.WriteString("\n\n")

	b.WriteString("Example response:\n")
	b.WriteString(structuredVerdictExample)
	b.WriteString("\n\n")

	b.WriteString("Return ONLY a JSON document matching this schema. No markdown, no prose, no commentary outside the JSON.\n")
	return b.String()
}
