export type ItemType = 'task' | 'event' | 'backlog';
export type ItemStatus = 'open' | 'scheduled' | 'in_progress' | 'completed' | 'cancelled';
export type WindowPref = 'morning' | 'deep' | 'shallow' | 'evening' | 'any';

export interface ItemBase {
  id: string;
  title: string;
  status: ItemStatus;
  created_at: string;
  source_url?: string;
  tags?: string[];
}

export interface TaskItem extends ItemBase {
  item_type: 'task';
  est_min?: number;
  deadline?: string;
  window_pref?: WindowPref;
  priority?: 1 | 2 | 3 | 4 | 5;
  scheduled_start?: string;
}

export interface EventItem extends ItemBase {
  item_type: 'event';
  start: string;
  duration_min?: number;
  source_calendar?: 'personal' | 'readybench' | 'manual';
}

export interface BacklogItem extends ItemBase {
  item_type: 'backlog';
  tier: 'T0' | 'T1' | 'T2' | 'T3' | 'T4' | 'T5';
  blocks?: string[];
  file_scope?: string[];
  estimated_loc?: number;
}

export type Item = TaskItem | EventItem | BacklogItem;

export interface TodayResult {
  events: EventItem[];
  ranked_tasks: TaskItem[];
  slots: Array<{ item_id: string; start: string; end: string; rationale: string }>;
}

export interface IngestResult {
  items: Item[];
}

export interface TranscribeResult {
  text: string;
}
