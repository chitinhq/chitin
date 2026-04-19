import { z } from 'zod';

const Sha256Hex = z.string().regex(/^[a-f0-9]{64}$/, 'must be 64 lowercase hex chars');

export const DriverIdentitySchema = z.object({
  user: z.string(),
  machine_id: z.string(),
  machine_fingerprint: Sha256Hex,
});

export const ChainTypeSchema = z.enum(['session', 'tool_call']);

export const EnvelopeSchema = z.object({
  schema_version: z.literal('2'),
  run_id: z.string().uuid(),
  session_id: z.string().uuid(),
  surface: z.string().min(1),
  driver_identity: DriverIdentitySchema,
  agent_instance_id: z.string().uuid(),
  parent_agent_id: z.string().uuid().nullable(),
  agent_fingerprint: Sha256Hex,
  event_type: z.string().min(1),
  chain_id: z.string().min(1),
  chain_type: ChainTypeSchema,
  parent_chain_id: z.string().min(1).nullable(),
  seq: z.number().int().nonnegative(),
  prev_hash: Sha256Hex.nullable(),
  this_hash: Sha256Hex,
  ts: z.string().datetime(),
  labels: z.record(z.string(), z.string()),
});

export type Envelope = z.infer<typeof EnvelopeSchema>;
export type DriverIdentity = z.infer<typeof DriverIdentitySchema>;
export type ChainType = z.infer<typeof ChainTypeSchema>;
