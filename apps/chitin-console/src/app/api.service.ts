import { Injectable, inject } from '@angular/core';
import { HttpClient, HttpParams } from '@angular/common/http';
import { Observable } from 'rxjs';
import type {
  Stats, TaskListResponse, TaskDetail, AssigneeRow,
  RecentRun, EloRow, SessionSummary, SessionDetail,
  Policy, SuggestionsResponse, ArgusInfo, ArgusFindingsResponse,
  CostHistogram, ClawtaDecision,
} from './api.types';
import { BoardService, type Board } from './board.service';

const API_BASE = (window as { __CHITIN_API__?: string }).__CHITIN_API__ ?? '/api';

@Injectable({ providedIn: 'root' })
export class ApiService {
  private http = inject(HttpClient);
  private boardSvc = inject(BoardService);

  /** Returns an HttpParams (or appends to existing) seeded with the current board. */
  private withBoard(params: HttpParams = new HttpParams()): HttpParams {
    const b = this.boardSvc.current();
    return b ? params.set('board', b) : params;
  }

  health(): Observable<{ ok: boolean; board: string; ts: number }> {
    return this.http.get<{ ok: boolean; board: string; ts: number }>(`${API_BASE}/health`);
  }
  boards(): Observable<{ boards: Board[]; current: string }> {
    return this.http.get<{ boards: Board[]; current: string }>(`${API_BASE}/boards`);
  }
  stats(): Observable<Stats> {
    return this.http.get<Stats>(`${API_BASE}/stats`, { params: this.withBoard() });
  }
  tasks(opts: { status?: string; assignee?: string; q?: string; limit?: number } = {}): Observable<TaskListResponse> {
    let params = this.withBoard();
    if (opts.status) params = params.set('status', opts.status);
    if (opts.assignee) params = params.set('assignee', opts.assignee);
    if (opts.q) params = params.set('q', opts.q);
    if (opts.limit) params = params.set('limit', String(opts.limit));
    return this.http.get<TaskListResponse>(`${API_BASE}/tasks`, { params });
  }
  task(id: string): Observable<TaskDetail> {
    return this.http.get<TaskDetail>(`${API_BASE}/tasks/${encodeURIComponent(id)}`, { params: this.withBoard() });
  }
  assignees(): Observable<{ assignees: AssigneeRow[] }> {
    return this.http.get<{ assignees: AssigneeRow[] }>(`${API_BASE}/assignees`, { params: this.withBoard() });
  }
  recentRuns(limit = 25): Observable<{ runs: RecentRun[] }> {
    return this.http.get<{ runs: RecentRun[] }>(`${API_BASE}/runs/recent`, {
      params: this.withBoard().set('limit', String(limit)),
    });
  }
  elo(): Observable<{ rows: EloRow[] }> {
    return this.http.get<{ rows: EloRow[] }>(`${API_BASE}/elo`);
  }
  sessions(limit = 50): Observable<{ sessions: SessionSummary[]; totalSeen: number }> {
    return this.http.get<{ sessions: SessionSummary[]; totalSeen: number }>(`${API_BASE}/sessions`, {
      params: new HttpParams().set('limit', String(limit)),
    });
  }
  session(chainId: string): Observable<SessionDetail> {
    return this.http.get<SessionDetail>(`${API_BASE}/sessions/${encodeURIComponent(chainId)}`);
  }
  policy(): Observable<Policy> {
    return this.http.get<Policy>(`${API_BASE}/policy`);
  }
  suggestions(): Observable<SuggestionsResponse> {
    return this.http.get<SuggestionsResponse>(`${API_BASE}/suggestions`);
  }
  argusInfo(): Observable<ArgusInfo> {
    return this.http.get<ArgusInfo>(`${API_BASE}/argus/info`);
  }
  argusFindings(limit = 100): Observable<ArgusFindingsResponse> {
    return this.http.get<ArgusFindingsResponse>(`${API_BASE}/argus/findings`, {
      params: new HttpParams().set('limit', String(limit)),
    });
  }
  costHistogram(): Observable<CostHistogram> {
    return this.http.get<CostHistogram>(`${API_BASE}/cost/histogram`);
  }
  clawtaDecisions(limit = 50): Observable<{ decisions: ClawtaDecision[] }> {
    return this.http.get<{ decisions: ClawtaDecision[] }>(`${API_BASE}/clawta/decisions`, {
      params: new HttpParams().set('limit', String(limit)),
    });
  }

  /**
   * Transition a ticket. `status` is the kanban-flow verb
   * (start | ready | unblock | block | demote | done). `reason` is
   * required for block, demote, done.
   */
  updateTaskStatus(id: string, body: { status: string; author?: string; reason?: string }): Observable<TaskStatusUpdateResponse> {
    return this.http.post<TaskStatusUpdateResponse>(`${API_BASE}/tasks/${encodeURIComponent(id)}/status`, { ...body, board: this.boardSvc.current() });
  }

  /** Add a comment to a ticket. Persists in task_comments + emits a comment_added event. */
  addTaskComment(id: string, body: { body: string; author?: string }): Observable<TaskCommentResponse> {
    return this.http.post<TaskCommentResponse>(`${API_BASE}/tasks/${encodeURIComponent(id)}/comment`, { ...body, board: this.boardSvc.current() });
  }

  /** Create a new ticket on the current board. */
  createTask(body: { title: string; body?: string; assignee?: string; priority?: number; triage?: boolean; idempotency_key?: string }): Observable<TaskCreateResponse> {
    return this.http.post<TaskCreateResponse>(`${API_BASE}/tasks`, { ...body, board: this.boardSvc.current() });
  }

  /** Parsed industry-scan-latest.html — arXiv research scan with paper cards. */
  industryScan(): Observable<IndustryScanReport | null> {
    return this.http.get<IndustryScanReport | null>(`${API_BASE}/reports/industry-scan`);
  }

  /** Agent-bus threads (Discord-mirrored). */
  threads(opts: { board?: string; status?: string; limit?: number } = {}): Observable<{ threads: BusThread[] }> {
    let params = new HttpParams();
    if (opts.board) params = params.set('board', opts.board);
    if (opts.status) params = params.set('status', opts.status);
    if (opts.limit) params = params.set('limit', String(opts.limit));
    return this.http.get<{ threads: BusThread[] }>(`${API_BASE}/threads`, { params });
  }
  thread(id: number): Observable<BusThreadDetail> {
    return this.http.get<BusThreadDetail>(`${API_BASE}/threads/${id}`);
  }
  postThreadReply(id: number, body: { body: string; author?: string; parent_id?: number; kind?: string; audience?: string; channel_id?: string }): Observable<BusReplyResponse> {
    return this.http.post<BusReplyResponse>(`${API_BASE}/threads/${id}/reply`, body);
  }

  /** Discord channels the bot can see in its guild. */
  discordChannels(): Observable<{ channels: { id: string; name: string }[] }> {
    return this.http.get<{ channels: { id: string; name: string }[] }>(`${API_BASE}/discord/channels`);
  }
}

export interface BusThread {
  id: number;
  board: string | null;
  task_id: string | null;
  title: string;
  author: string;
  audience: string | null;
  status: 'open' | 'resolved' | 'archived';
  discord_thread_id: string | null;
  created_at: number;
  updated_at: number;
  message_count: number;
  last_message_body: string | null;
  last_message_author: string | null;
}
export interface BusMessage {
  id: number;
  parent_id: number | null;
  author: string;
  audience: string | null;
  body: string;
  kind: 'message' | 'directive' | 'ack' | 'system';
  discord_message_id: string | null;
  ack_required: number;
  created_at: number;
}
export interface BusAttachment {
  id: number;
  kind: 'spec' | 'pr' | 'task' | 'discord' | 'url' | 'file';
  ref: string;
  display: string | null;
  created_at: number;
}
export interface BusThreadDetail {
  thread: BusThread;
  messages: BusMessage[];
  attachments: BusAttachment[];
}
export interface BusReplyResponse {
  ok: boolean;
  message_id: number;
  thread: BusThreadDetail | null;
}

export interface IndustryPaper {
  title: string;
  url: string;
  authors: string | null;
  stars: number;
  tags: { kind: string; label: string }[];
  insight: string | null;
  summary: string | null;
}

export interface IndustryScanReport {
  file: string;
  date: string | null;
  generatedAt: number;
  telemetry: { value: string; label: string }[];
  sections: { title: string; papers: IndustryPaper[] }[];
  actions: string[];
}

export interface TaskStatusUpdateResponse {
  ok: boolean;
  task_id: string;
  status: string;
  flow_stdout: string;
  refreshed: boolean;
  refresh_error: string | null;
  task: TaskDetail | null;
}

export interface TaskCommentResponse {
  ok: boolean;
  task_id: string;
  author: string;
  refreshed: boolean;
  refresh_error: string | null;
  task: TaskDetail | null;
}

export interface TaskCreateResponse {
  ok: boolean;
  task_id: string | null;
  title: string;
  board: string;
  refreshed: boolean;
  created: { id?: string } | null;
}
