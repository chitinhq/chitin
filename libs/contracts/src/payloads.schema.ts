import { z } from 'zod';

const Sha256Hex = z.string().regex(/^[a-f0-9]{64}$/);

const ModelSchema = z.object({
  name: z.string(),
  provider: z.string(),
  version: z.string().optional(),
  context_window: z.number().int().positive().optional(),
});

const ClientInfoSchema = z.object({
  name: z.string(),
  version: z.string(),
});

const UsageSchema = z.object({
  input_tokens: z.number().int().nonnegative(),
  output_tokens: z.number().int().nonnegative(),
  cache_creation_input_tokens: z.number().int().nonnegative().optional(),
  cache_read_input_tokens: z.number().int().nonnegative().optional(),
  thinking_tokens: z.number().int().nonnegative().optional(),
});

const TotalsSchema = z.object({
  turn_count: z.number().int().nonnegative(),
  tool_call_count: z.number().int().nonnegative(),
  total_input_tokens: z.number().int().nonnegative(),
  total_output_tokens: z.number().int().nonnegative(),
  total_duration_ms: z.number().int().nonnegative(),
});

export const ActionTypeSchema = z.enum(['read', 'write', 'exec', 'git', 'net', 'dangerous']);

export const SessionStartPayloadSchema = z.object({
  cwd: z.string(),
  client_info: ClientInfoSchema,
  model: ModelSchema,
  system_prompt_hash: Sha256Hex,
  tool_allowlist_hash: Sha256Hex,
  soul_id: z.string().optional(),
  soul_hash: Sha256Hex.optional(),
  agent_version: z.string(),
  spawning_tool_call_id: z.string().optional(),
  task_description: z.string().optional(),
});

const AttachmentSchema = z.object({
  kind: z.string(),
  path: z.string().optional(),
  data: z.string().optional(),
});

export const UserPromptPayloadSchema = z.object({
  text: z.string(),
  attachments: z.array(AttachmentSchema).optional(),
});

export const AssistantTurnPayloadSchema = z.object({
  text: z.string(),
  thinking: z.string().optional(),
  model_used: ModelSchema,
  usage: UsageSchema,
  ts_start: z.string().datetime(),
  ts_end: z.string().datetime(),
});

export const CompactionPayloadSchema = z.object({
  reason: z.string(),
  pre_token_count: z.number().int().nonnegative().optional(),
  post_token_count: z.number().int().nonnegative().optional(),
  summary: z.string().optional(),
});

export const SessionEndPayloadSchema = z.object({
  reason: z.string(),
  totals: TotalsSchema,
});

export const IntendedPayloadSchema = z.object({
  tool_name: z.string(),
  raw_input: z.record(z.string(), z.unknown()),
  canonical_form: z.record(z.string(), z.unknown()).optional(),
  action_type: ActionTypeSchema,
});

export const ExecutedPayloadSchema = z.object({
  duration_ms: z.number().int().nonnegative(),
  output_preview: z.string().optional(),
  output_bytes_total: z.number().int().nonnegative().optional(),
});

export const FailedPayloadSchema = z.object({
  duration_ms: z.number().int().nonnegative(),
  error_kind: z.string(),
  error: z.string(),
  output_preview: z.string().optional(),
});

export const ModelTurnPayloadSchema = z.object({
  model_name: z.string().min(1),
  provider: z.string().min(1),
  input_tokens: z.number().int().nonnegative(),
  output_tokens: z.number().int().nonnegative(),
  session_id_external: z.string().optional(),
  duration_ms: z.number().int().nonnegative().optional(),
  cache_read_tokens: z.number().int().nonnegative().optional(),
  cache_write_tokens: z.number().int().nonnegative().optional(),
});

export type SessionStartPayload = z.infer<typeof SessionStartPayloadSchema>;
export type UserPromptPayload = z.infer<typeof UserPromptPayloadSchema>;
export type AssistantTurnPayload = z.infer<typeof AssistantTurnPayloadSchema>;
export type CompactionPayload = z.infer<typeof CompactionPayloadSchema>;
export type SessionEndPayload = z.infer<typeof SessionEndPayloadSchema>;
export type IntendedPayload = z.infer<typeof IntendedPayloadSchema>;
export type ExecutedPayload = z.infer<typeof ExecutedPayloadSchema>;
export type FailedPayload = z.infer<typeof FailedPayloadSchema>;
export type ModelTurnPayload = z.infer<typeof ModelTurnPayloadSchema>;
export type ActionType = z.infer<typeof ActionTypeSchema>;
