import { randomUUID } from 'node:crypto';
import {
  RunManifestSchema,
  type RunManifest,
  type RunManifestInput,
} from '@chitin/contracts';

export function createRunManifest(input: RunManifestInput): RunManifest {
  return RunManifestSchema.parse({
    schema_version: '2',
    run_id: input.run_id ?? randomUUID(),
    session_id: input.session_id ?? randomUUID(),
    surface: input.surface,
    driver_identity: input.driver_identity,
    agent_instance_id: input.agent_instance_id ?? randomUUID(),
    parent_agent_id: input.parent_agent_id ?? null,
    agent_fingerprint: input.agent_fingerprint,
    labels: input.labels ?? {},
  });
}
