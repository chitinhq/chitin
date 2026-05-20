import { z } from 'zod';

// ── Sub-types ──────────────────────────────────────────────────────────────────

export const RequirementSchema = z.object({
  id: z.string().min(1),       // R1, R2, ...
  text: z.string().min(1),
});

export const AcceptanceCriterionSchema = z.object({
  id: z.string().min(1),      // AC1, AC2, ...
  text: z.string().min(1),
});

export const SliceSchema = z.object({
  id: z.string().min(1),      // Slice 1, Slice 2, ...
  scope: z.string().min(1),
  requirement_ids: z.array(z.string()),
});

export const QuestionSchema = z.object({
  id: z.string().min(1),      // Q1, Q2, ...
  text: z.string().min(1),
  proposed: z.string().nullable(),
});

// ── Enums ─────────────────────────────────────────────────────────────────────

export const SpecStatusSchema = z.enum(['draft', 'ratified', 'superseded']);
export const SourceFrameworkSchema = z.enum([
  'spec-kit',
  'openspec',
  'superpowers',
  'house',
]);

// ── UnifiedSpec ───────────────────────────────────────────────────────────────

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

export type UnifiedSpec = z.infer<typeof UnifiedSpecSchema>;
export type Requirement = z.infer<typeof RequirementSchema>;
export type AcceptanceCriterion = z.infer<typeof AcceptanceCriterionSchema>;
export type Slice = z.infer<typeof SliceSchema>;
export type Question = z.infer<typeof QuestionSchema>;
export type SpecStatus = z.infer<typeof SpecStatusSchema>;
export type SourceFramework = z.infer<typeof SourceFrameworkSchema>;