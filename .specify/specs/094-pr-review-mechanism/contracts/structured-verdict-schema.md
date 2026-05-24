# Contract: StructuredVerdict JSON schema

**Spec reference**: FR-013, FR-014 | **Code reference**: `go/orchestrator/activities/review/verdict/`

## Schema (JSON Schema draft 2020-12)

```json
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "$id": "https://chitinhq.github.io/chitin/schemas/structured-verdict-v1.json",
  "title": "StructuredVerdict",
  "description": "Output schema for every reviewer driver and operator-arbiter invocation. Version 1.",
  "type": "object",
  "additionalProperties": false,
  "required": ["verdict", "concerns", "recommendations", "blockers"],
  "properties": {
    "verdict": {
      "type": "string",
      "enum": ["approve", "approve-with-comments", "request-changes", "abstain"]
    },
    "concerns": {
      "type": "array",
      "items": { "type": "string", "minLength": 1 },
      "description": "Free-text observations that are not blockers — non-empty strings only."
    },
    "recommendations": {
      "type": "array",
      "items": { "type": "string", "minLength": 1 },
      "description": "Free-text suggestions for improvement — non-empty strings only."
    },
    "blockers": {
      "type": "array",
      "items": { "type": "string", "minLength": 1 },
      "description": "Free-text reasons the verdict is request-changes — non-empty strings only."
    },
    "reason": {
      "type": "string",
      "description": "Optional free-text rationale, used only with verdict=abstain."
    }
  },

  "allOf": [
    {
      "if":   { "properties": { "verdict": { "const": "approve" } } },
      "then": {
        "properties": { "blockers": { "maxItems": 0 } }
      }
    },
    {
      "if":   { "properties": { "verdict": { "const": "approve-with-comments" } } },
      "then": {
        "properties": { "blockers": { "maxItems": 0 } },
        "anyOf": [
          { "properties": { "concerns":        { "minItems": 1 } } },
          { "properties": { "recommendations": { "minItems": 1 } } }
        ]
      }
    },
    {
      "if":   { "properties": { "verdict": { "const": "request-changes" } } },
      "then": {
        "properties": { "blockers": { "minItems": 1 } }
      }
    },
    {
      "if":   { "properties": { "verdict": { "const": "abstain" } } },
      "then": {
        "properties": {
          "concerns":        { "maxItems": 0 },
          "recommendations": { "maxItems": 0 },
          "blockers":        { "maxItems": 0 }
        }
      }
    }
  ]
}
```

## Invariants (FR-014, restated)

1. `verdict == approve` ⇒ `blockers` is empty.
2. `verdict == approve-with-comments` ⇒ `blockers` is empty AND (`concerns` non-empty OR `recommendations` non-empty).
3. `verdict == request-changes` ⇒ `blockers` is non-empty.
4. `verdict == abstain` ⇒ `concerns`, `recommendations`, `blockers` are all empty.
5. The `reason` field is optional everywhere; in practice it is only meaningful for `abstain`.

## Worked examples — valid

```json
{ "verdict": "approve", "concerns": [], "recommendations": [], "blockers": [] }
```

```json
{
  "verdict": "approve-with-comments",
  "concerns": ["The new RPC has no rate limit"],
  "recommendations": ["Consider adding a token bucket in v1.1"],
  "blockers": []
}
```

```json
{
  "verdict": "request-changes",
  "concerns": ["High-cardinality OTLP labels on the new event"],
  "recommendations": [],
  "blockers": ["Hard-coded URL in producer.go:42 should be config", "No tests for the new branch"]
}
```

```json
{ "verdict": "abstain", "concerns": [], "recommendations": [], "blockers": [], "reason": "I do not have context on the chitin orchestrator's policy table to judge this PR's class shifts" }
```

## Worked examples — invalid (rejected by `Validate`)

| Failure | Why |
|---|---|
| `{"verdict": "approve", "blockers": ["something"]}` | Invariant 1 violated. |
| `{"verdict": "approve-with-comments", "concerns": [], "recommendations": [], "blockers": []}` | Invariant 2 violated (must have ≥1 concern or recommendation). |
| `{"verdict": "request-changes", "blockers": []}` | Invariant 3 violated. |
| `{"verdict": "abstain", "concerns": ["something"]}` | Invariant 4 violated. |
| `{"verdict": "approve"}` | Required fields `concerns`/`recommendations`/`blockers` missing. |
| `{"verdict": "maybe"}` | Not one of the four enum values. |

## Versioning

This is implicit version 1. A future schema change is either backwards-compatible (additive, ignored by v1 readers) or carries an explicit `schema_version: 2` field added at the time the breaking change ships. Workflow history records preserve their original schema by definition — older verdicts continue to validate against v1 forever.
