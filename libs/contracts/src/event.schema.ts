import { z } from 'zod';
import { EnvelopeSchema } from './envelope.schema';
import {
  SessionStartPayloadSchema,
  UserPromptPayloadSchema,
  AssistantTurnPayloadSchema,
  CompactionPayloadSchema,
  SessionEndPayloadSchema,
  IntendedPayloadSchema,
  ExecutedPayloadSchema,
  FailedPayloadSchema,
  ModelTurnPayloadSchema,
} from './payloads.schema';

const envShape = EnvelopeSchema.shape;

export const EventSchema = z.discriminatedUnion('event_type', [
  z.object({ ...envShape, event_type: z.literal('session_start'), payload: SessionStartPayloadSchema }),
  z.object({ ...envShape, event_type: z.literal('user_prompt'), payload: UserPromptPayloadSchema }),
  z.object({ ...envShape, event_type: z.literal('assistant_turn'), payload: AssistantTurnPayloadSchema }),
  z.object({ ...envShape, event_type: z.literal('compaction'), payload: CompactionPayloadSchema }),
  z.object({ ...envShape, event_type: z.literal('session_end'), payload: SessionEndPayloadSchema }),
  z.object({ ...envShape, event_type: z.literal('intended'), payload: IntendedPayloadSchema }),
  z.object({ ...envShape, event_type: z.literal('executed'), payload: ExecutedPayloadSchema }),
  z.object({ ...envShape, event_type: z.literal('failed'), payload: FailedPayloadSchema }),
  z.object({ ...envShape, event_type: z.literal('model_turn'), payload: ModelTurnPayloadSchema }),
]);

export type Event = z.infer<typeof EventSchema>;

// Reserved for Phase 2 (documented, not emitted):
// - policy_decided
// - rewritten
// - denied
