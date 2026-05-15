import { Injectable, inject } from '@angular/core';
import { HttpClient, HttpParams } from '@angular/common/http';
import { Observable } from 'rxjs';
import type {
  Stats, TaskListResponse, TaskDetail, AssigneeRow,
  RecentRun, EloRow, SessionSummary, SessionDetail,
  Policy, SuggestionsResponse, ArgusInfo, ArgusFindingsResponse,
  CostHistogram, ClawtaDecision,
} from './api.types';

const API_BASE = (window as { __CHITIN_API__?: string }).__CHITIN_API__ ?? '/api';

@Injectable({ providedIn: 'root' })
export class ApiService {
  private http = inject(HttpClient);

  health(): Observable<{ ok: boolean; board: string; ts: number }> {
    return this.http.get<{ ok: boolean; board: string; ts: number }>(`${API_BASE}/health`);
  }
  stats(): Observable<Stats> {
    return this.http.get<Stats>(`${API_BASE}/stats`);
  }
  tasks(opts: { status?: string; assignee?: string; q?: string; limit?: number } = {}): Observable<TaskListResponse> {
    let params = new HttpParams();
    if (opts.status) params = params.set('status', opts.status);
    if (opts.assignee) params = params.set('assignee', opts.assignee);
    if (opts.q) params = params.set('q', opts.q);
    if (opts.limit) params = params.set('limit', String(opts.limit));
    return this.http.get<TaskListResponse>(`${API_BASE}/tasks`, { params });
  }
  task(id: string): Observable<TaskDetail> {
    return this.http.get<TaskDetail>(`${API_BASE}/tasks/${encodeURIComponent(id)}`);
  }
  assignees(): Observable<{ assignees: AssigneeRow[] }> {
    return this.http.get<{ assignees: AssigneeRow[] }>(`${API_BASE}/assignees`);
  }
  recentRuns(limit = 25): Observable<{ runs: RecentRun[] }> {
    return this.http.get<{ runs: RecentRun[] }>(`${API_BASE}/runs/recent`, {
      params: new HttpParams().set('limit', String(limit)),
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
    return this.http.post<TaskStatusUpdateResponse>(`${API_BASE}/tasks/${encodeURIComponent(id)}/status`, body);
  }

  /** Add a comment to a ticket. Persists in task_comments + emits a comment_added event. */
  addTaskComment(id: string, body: { body: string; author?: string }): Observable<TaskCommentResponse> {
    return this.http.post<TaskCommentResponse>(`${API_BASE}/tasks/${encodeURIComponent(id)}/comment`, body);
  }
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
