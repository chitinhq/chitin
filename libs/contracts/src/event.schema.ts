import { z } from 'zod';

export const ActionTypeSchema = z.enum([
  'read',
  'write',
  'exec',
  'git',
  'net',
  'dangerous',
]);

export const ResultSchema = z.enum(['success', 'error', 'denied']);

export const EventSchema = z.object({
  run_id: z.string().uuid(),
  session_id: z.string().uuid(),
  surface: z.string().min(1),
  driver: z.string().min(1),
  agent_id: z.string(),
  tool_name: z.string(),
  raw_input: z.record(z.string(), z.any()),
  canonical_form: z.record(z.string(), z.any()),
  action_type: ActionTypeSchema,
  result: ResultSchema,
  duration_ms: z.number().int().nonnegative(),
  error: z.string().nullable(),
  ts: z.string().datetime({ offset: true }),
  metadata: z.record(z.string(), z.any()),
});
