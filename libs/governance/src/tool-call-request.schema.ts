import { z } from 'zod';
import { BlastVectorSchema } from './blast-vector.schema.js';
import { SemanticEnvelopeSchema } from './semantic-envelope.schema.js';

// ToolCallRequest — the canonical tool-call adjudication contract.
// Spec: docs/superpowers/specs/2026-05-03-predictive-execution-policy-design.md §2.
//
// Naming: distinct from libs/contracts ExecutionRequest, which is the
// *workflow dispatch* contract (what task should this agent run?).
// ToolCallRequest is at a different layer: tool-call-level
// adjudication (should this specific tool call be allowed?).
//
// Slice 1 carries the raw call + derived envelope + blast vector, plus
// classifier metadata. Slices 2+ extend with predicted_blast,
// task_declaration_id, trust_score, etc.

export const IngressSchema = z.enum([
  'claude_code_pretooluse',
  'openclaw_before_tool_call',
  'mcp',
  'copilot_intercept',
  'temporal_worker',
  'unknown',
]);

export const AgentTierSchema = z.enum(['T0', 'T1', 'T2', 'T3', 'T4', 'T5']);

export const ToolCallRequestSchema = z.object({
  // Schema versioning — bump on breaking changes so the chain can
  // re-interpret old events under historical schemas (spec §10).
  schema_version: z.literal('1'),

  // Identity
  request_id: z
    .string()
    .min(1)
    .describe('ULID; chain-linkable; primary key on the request'),
  session_id: z.string().min(1),
  agent_id: z.string().min(1),
  agent_tier: AgentTierSchema.optional(),
  parent_event_hash: z
    .string()
    .optional()
    .describe('Hash of the preceding chain event, if any. Establishes ordering.'),

  // Raw call (verbatim, immutable)
  ingress: IngressSchema,
  tool_name: z.string().min(1),
  tool_args: z.record(z.string(), z.unknown()),
  tool_metadata: z
    .record(z.string(), z.unknown())
    .optional()
    .describe('Schema, server identity, declared capabilities (when MCP, etc.).'),

  // Derived (classifier output)
  semantic_envelope: SemanticEnvelopeSchema,
  blast_vector: BlastVectorSchema,
  classifier_confidence: z.number().min(0).max(1),
  classifier_version: z
    .string()
    .min(1)
    .describe(
      'Identifies the classifier rules that produced this envelope. Used by ' +
      'audit + counterfactual replay to re-classify against historical versions.',
    ),
});

export type Ingress = z.infer<typeof IngressSchema>;
export type AgentTier = z.infer<typeof AgentTierSchema>;
export type ToolCallRequest = z.infer<typeof ToolCallRequestSchema>;
