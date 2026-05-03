import { z } from 'zod';

export const ItemTypeSchema = z.enum(['task', 'event', 'backlog']);
export type ItemType = z.infer<typeof ItemTypeSchema>;

export const WindowPrefSchema = z.enum(['morning', 'deep', 'shallow', 'evening', 'any']);
export type WindowPref = z.infer<typeof WindowPrefSchema>;

export const ItemStatusSchema = z.enum(['open', 'scheduled', 'in_progress', 'completed', 'cancelled']);
export type ItemStatus = z.infer<typeof ItemStatusSchema>;

const ItemBaseSchema = z.object({
  id: z.string().min(1),
  title: z.string().min(1),
  status: ItemStatusSchema,
  created_at: z.string().datetime(),
  source_url: z.string().optional(),
  tags: z.array(z.string()).optional(),
});

export const TaskItemSchema = ItemBaseSchema.extend({
  item_type: z.literal('task'),
  est_min: z.number().int().positive().optional(),
  deadline: z.string().datetime().optional(),
  window_pref: WindowPrefSchema.optional(),
  priority: z.union([z.literal(1), z.literal(2), z.literal(3), z.literal(4), z.literal(5)]).optional(),
  scheduled_start: z.string().datetime().optional(),
});

export const EventItemSchema = ItemBaseSchema.extend({
  item_type: z.literal('event'),
  start: z.string().datetime(),
  duration_min: z.number().int().positive().optional(),
  source_calendar: z.enum(['personal', 'readybench', 'manual']).optional(),
});

export const BacklogItemSchema = ItemBaseSchema.extend({
  item_type: z.literal('backlog'),
  tier: z.enum(['T0', 'T1', 'T2', 'T3', 'T4', 'T5']).optional(),
  blocks: z.array(z.string()).optional(),
  file_scope: z.array(z.string()).optional(),
  estimated_loc: z.number().int().positive().optional(),
});

export const ItemSchema = z.discriminatedUnion('item_type', [
  TaskItemSchema,
  EventItemSchema,
  BacklogItemSchema,
]);

export type TaskItem = z.infer<typeof TaskItemSchema>;
export type EventItem = z.infer<typeof EventItemSchema>;
export type BacklogItem = z.infer<typeof BacklogItemSchema>;
export type Item = z.infer<typeof ItemSchema>;

export interface ItemDecision {
  event_type: 'item_decision';
  item_id: string;
  rationale: string;
  scheduled_start?: string;
  ts: string;
}

export interface Telemetry {
  consumer: 'personal' | 'swarm';
  item_decisions: ItemDecision[];
}
