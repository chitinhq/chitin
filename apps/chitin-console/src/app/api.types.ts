// Typed shapes that mirror the chitin-console-api JSON responses.

export interface LaneCounts {
  triage?: number;
  todo?: number;
  ready?: number;
  in_progress?: number;
  done?: number;
  archived?: number;
  [k: string]: number | undefined;
}

export interface Stats {
  board: string;
  lanes: LaneCounts;
  completedLast7Days: number;
  inFlight: number;
  medianAgeSecondsActive: number;
  successRate7d: number | null;
  runsLast24: number;
  runsCompleted24: number;
  generatedAt: number;
}

export interface Task {
  id: string;
  title: string;
  status: string;
  assignee: string | null;
  priority: number;
  created_at: number;
  started_at: number | null;
  completed_at: number | null;
  workspace_kind: string;
  workspace_path: string | null;
  tenant: string | null;
  current_run_id: number | null;
  workflow_template_id: string | null;
  current_step_key: string | null;
  consecutive_failures: number;
  max_retries: number | null;
  last_heartbeat_at: number | null;
  idempotency_key: string | null;
  has_body: number;
}

export interface TaskListResponse {
  board: string;
  count: number;
  tasks: Task[];
}

export interface TaskRun {
  id: number;
  profile: string | null;
  step_key: string | null;
  status: string;
  started_at: number;
  ended_at: number | null;
  outcome: string | null;
  summary: string | null;
  error: string | null;
}

export interface RecentRun extends TaskRun {
  task_id: string;
  task_title: string | null;
  task_assignee: string | null;
}

export interface TaskEvent {
  id: number;
  run_id: number | null;
  kind: string;
  payload: string | null;
  created_at: number;
}

export interface TaskComment {
  id: number;
  author: string;
  body: string;
  created_at: number;
}

export interface ClawtaDecision {
  id: number;
  ticket_id?: string;
  driver: string;
  model: string;
  selection_mode: string;
  reasoning: string;
  ts: string;
}

export interface TaskDetail {
  task: Task & { body: string | null };
  runs: TaskRun[];
  events: TaskEvent[];
  comments: TaskComment[];
  links: { id: number; rel: string; ref: string; created_at: number }[];
  clawtaDecisions: ClawtaDecision[];
}

export interface AssigneeRow { assignee: string; n: number; }

export interface EloRow {
  id: number;
  driver: string;
  model: string;
  role: string;
  task_class: string;
  complexity_bucket: string;
  elo_score: number;
  dispatches_count: number;
  last_dispatch_id: string | null;
  first_scored_at: number;
  last_updated: number;
}

export interface SessionSummary {
  chain_id: string;
  driver?: string;
  agent?: string;
  role?: string;
  firstTs?: string;
  lastTs?: string;
  events: number;
  allowed: number;
  denied: number;
  heuristic: number;
  costUsd: number;
  inputBytes: number;
  tools: string[];
  tickets: string[];
  actions: Record<string, number>;
}

export interface ChainEvent {
  ts?: string;
  chain_id?: string;
  session_id?: string;
  driver?: string;
  agent?: string;
  role?: string;
  action_type?: string;
  action_target?: string;
  tool_name?: string;
  rule_id?: string;
  reason?: string;
  decision?: string;
  effect?: string;
  allowed?: boolean;
  cost_usd?: number;
  input_bytes?: number;
  ticket_id?: string;
  workflow_id?: string;
  authority?: string;
  predicted_blast?: string;
  floundering_score?: number;
  [k: string]: unknown;
}

export interface SessionDetail {
  chain_id: string;
  eventCount: number;
  firstTs?: string;
  lastTs?: string;
  driver?: string;
  agent?: string;
  role?: string;
  costUsd: number;
  inputBytes: number;
  allowed: number;
  denied: number;
  heuristic: number;
  toolCounts: Record<string, number>;
  ruleCounts: Record<string, number>;
  events: ChainEvent[];
}

export interface Policy {
  path: string;
  size?: number;
  modified?: number;
  content?: string;
  error?: string;
}

export interface SuggestionsResponse {
  enabled: boolean;
  note: string;
  filters?: {
    type: string;
    target: string;
    sort: string;
  };
  suggestions: AnalyzerSuggestion[];
}

export interface AnalyzerSuggestion {
  id: string;
  type: 'prompt_edit' | 'new_skill' | 'policy_rule' | 'route_tweak' | 'drop';
  target: string;
  diff: string;
  rationale: string;
  applied: number;
  created_at: string;
}

export interface ArgusInfo {
  available: boolean;
  tables?: string[];
  counts?: Record<string, number>;
  error?: string;
}

export interface ArgusFindingsResponse {
  findings: Record<string, unknown>[];
  tables?: string[];
  note?: string;
  error?: string;
}

export interface CostHistogram {
  bins: number[];
  totalUsd: number;
}
