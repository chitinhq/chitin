import { z } from 'zod';

// ---------------------------------------------------------------------------
// Spec 061 — Unified spec model: canonical Zod schema + JSON Schema export.
// This is the L1 contract that all framework adapters produce and L2–L7
// consume. The Go binding in go/execution-kernel/internal/spec/ mirrors this
// shape; the Python binding in python/analysis/unified_spec.py mirrors it
// too. Both are validated against the JSON Schema in CI.
// ---------------------------------------------------------------------------

/** Status of a spec in its lifecycle. */
export const SpecStatusSchema = z.enum(['draft', 'ratified', 'superseded']);

/** Source framework the spec was adapted from. */
export const SourceFrameworkSchema = z.enum([
  'spec-kit',
  'openspec',
  'superpowers',
  'house',
]);

/** A single requirement within a spec (R1, R2, …). */
export const RequirementSchema = z.object({
  id: z.string().min(1),
  text: z.string().min(1),
});

/** A single acceptance criterion (AC1, AC2, …). */
export const AcceptanceCriterionSchema = z.object({
  id: z.string().min(1),
  text: z.string().min(1),
});

/** A delivery slice within the spec. */
export const SliceSchema = z.object({
  id: z.string().min(1),
  scope: z.string().min(1),
  requirement_ids: z.array(z.string().min(1)),
});

/** An open question within the spec (Q1, Q2, …). */
export const QuestionSchema = z.object({
  id: z.string().min(1),
  text: z.string().min(1),
  proposed: z.string().optional(),
});

/**
 * UnifiedSpec — the normalized shape that all framework adapters produce.
 *
 * Fields:
 * - spec_id: stable identifier (e.g. "060", "ic-001"); the join key for
 *   L2/L3 attribution and replay.
 * - title: human-readable spec title.
 * - status: lifecycle status.
 * - source_framework: provenance — which adapter produced this.
 * - source_path: file path the adapter read from (debugging / traceability).
 * - requirements: ordered list of R1, R2, …
 * - acceptance: ordered list of AC1, AC2, …
 * - boundaries: list of boundary-case descriptions (free text).
 * - slices: ordered list of delivery slices.
 * - open_questions: open / unresolved questions.
 */
export const UnifiedSpecSchema = z.object({
  spec_id: z.string().min(1),
  title: z.string().min(1),
  status: SpecStatusSchema,
  source_framework: SourceFrameworkSchema,
  source_path: z.string().min(1),
  requirements: z.array(RequirementSchema),
  acceptance: z.array(AcceptanceCriterionSchema),
  boundaries: z.array(z.string()),
  slices: z.array(SliceSchema),
  open_questions: z.array(QuestionSchema),
});

// ---------------------------------------------------------------------------
// Derived types
// ---------------------------------------------------------------------------

export type SpecStatus = z.infer<typeof SpecStatusSchema>;
export type SourceFramework = z.infer<typeof SourceFrameworkSchema>;
export type Requirement = z.infer<typeof RequirementSchema>;
export type AcceptanceCriterion = z.infer<typeof AcceptanceCriterionSchema>;
export type Slice = z.infer<typeof SliceSchema>;
export type Question = z.infer<typeof QuestionSchema>;
export type UnifiedSpec = z.infer<typeof UnifiedSpecSchema>;

// ---------------------------------------------------------------------------
// JSON Schema export for cross-language validation
// ---------------------------------------------------------------------------

/**
 * Produce the JSON Schema (draft-07) for UnifiedSpec.
 * Used in CI to validate Go and Python bindings against the canonical
 * contract.
 */
export function unifiedSpecJsonSchema(): object {
  return {
    $schema: 'http://json-schema.org/draft-07/schema#',
    $id: 'https://chitin.dev/schemas/unified-spec.schema.json',
    title: 'UnifiedSpec',
    description:
      'Normalized spec shape produced by all framework adapters (spec 061).',
    type: 'object',
    required: [
      'spec_id',
      'title',
      'status',
      'source_framework',
      'source_path',
      'requirements',
      'acceptance',
      'boundaries',
      'slices',
      'open_questions',
    ],
    properties: {
      spec_id: {
        type: 'string',
        minLength: 1,
        description:
          'Stable identifier (e.g. "060", "ic-001"). Join key for L2/L3.',
      },
      title: {
        type: 'string',
        minLength: 1,
        description: 'Human-readable spec title.',
      },
      status: {
        $ref: '#/$defs/SpecStatus',
      },
      source_framework: {
        $ref: '#/$defs/SourceFramework',
      },
      source_path: {
        type: 'string',
        minLength: 1,
        description:
          'File path the adapter read from (provenance / traceability).',
      },
      requirements: {
        type: 'array',
        items: { $ref: '#/$defs/Requirement' },
        description: 'Ordered list of requirements (R1, R2, …).',
      },
      acceptance: {
        type: 'array',
        items: { $ref: '#/$defs/AcceptanceCriterion' },
        description: 'Ordered list of acceptance criteria (AC1, AC2, …).',
      },
      boundaries: {
        type: 'array',
        items: { type: 'string' },
        description: 'Boundary-case descriptions (free text).',
      },
      slices: {
        type: 'array',
        items: { $ref: '#/$defs/Slice' },
        description: 'Ordered list of delivery slices.',
      },
      open_questions: {
        type: 'array',
        items: { $ref: '#/$defs/Question' },
        description: 'Open / unresolved questions.',
      },
    },
    additionalProperties: false,
    $defs: {
      SpecStatus: {
        type: 'string',
        enum: ['draft', 'ratified', 'superseded'],
      },
      SourceFramework: {
        type: 'string',
        enum: ['spec-kit', 'openspec', 'superpowers', 'house'],
      },
      Requirement: {
        type: 'object',
        required: ['id', 'text'],
        properties: {
          id: { type: 'string', minLength: 1 },
          text: { type: 'string', minLength: 1 },
        },
        additionalProperties: false,
      },
      AcceptanceCriterion: {
        type: 'object',
        required: ['id', 'text'],
        properties: {
          id: { type: 'string', minLength: 1 },
          text: { type: 'string', minLength: 1 },
        },
        additionalProperties: false,
      },
      Slice: {
        type: 'object',
        required: ['id', 'scope', 'requirement_ids'],
        properties: {
          id: { type: 'string', minLength: 1 },
          scope: { type: 'string', minLength: 1 },
          requirement_ids: {
            type: 'array',
            items: { type: 'string', minLength: 1 },
          },
        },
        additionalProperties: false,
      },
      Question: {
        type: 'object',
        required: ['id', 'text'],
        properties: {
          id: { type: 'string', minLength: 1 },
          text: { type: 'string', minLength: 1 },
          proposed: { type: 'string' },
        },
        additionalProperties: false,
      },
    },
  };
}