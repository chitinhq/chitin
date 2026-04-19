import type { z } from 'zod';
import type { EventSchema, ActionTypeSchema, ResultSchema } from './event.schema.js';

export type Event = z.infer<typeof EventSchema>;
export type ActionType = z.infer<typeof ActionTypeSchema>;
export type Result = z.infer<typeof ResultSchema>;
