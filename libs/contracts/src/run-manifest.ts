import { z } from 'zod';
import { DriverIdentitySchema } from './envelope.schema.js';

const Sha256Hex = z.string().regex(/^[a-f0-9]{64}$/, 'must be 64 lowercase hex chars');

export const RunManifestSchema = z.object({
  schema_version: z.literal('2'),
  run_id: z.string().uuid(),
  session_id: z.string().uuid(),
  surface: z.string().min(1),
  driver_identity: DriverIdentitySchema,
  agent_instance_id: z.string().uuid(),
  parent_agent_id: z.string().uuid().nullable(),
  agent_fingerprint: Sha256Hex,
  labels: z.record(z.string(), z.string()),
});

export type RunManifest = z.infer<typeof RunManifestSchema>;

export type RunManifestInput = Omit<
  RunManifest,
  'schema_version' | 'run_id' | 'session_id' | 'agent_instance_id' | 'parent_agent_id' | 'labels'
> & {
  run_id?: string;
  session_id?: string;
  agent_instance_id?: string;
  parent_agent_id?: string | null;
  labels?: Record<string, string>;
};
