import { ChangeDetectionStrategy, Component, OnInit, inject, signal, computed } from '@angular/core';
import { CommonModule } from '@angular/common';
import { RouterModule } from '@angular/router';
import { forkJoin } from 'rxjs';
import { ApiService } from '../api.service';
import type { Task, SessionSummary, ArgusInfo } from '../api.types';
import { StatusPillComponent } from '../ui/status-pill.component';
import { LoaderComponent } from '../ui/loader.component';
import { EmptyStateComponent } from '../ui/empty-state.component';
import { ageFromEpochSeconds, fmtTs, fmtUsd, shortenId } from '../utils';

interface StandupBucket {
  assignee: string;
  total: number;
  in_progress: number;
  ready: number;
  blocked: number;
  triage: number;
  oldestActiveAgeSec: number;
  tickets: Task[];
}

interface ChainDayBucket {
  day: string;  // YYYY-MM-DD
  events: number;
  allowed: number;
  denied: number;
  costUsd: number;
}

interface ChainTool {
  tool: string;
  count: number;
}

@Component({
  selector: 'cc-reports',
  standalone: true,
  imports: [CommonModule, RouterModule, StatusPillComponent, LoaderComponent, EmptyStateComponent],
  changeDetection: ChangeDetectionStrategy.OnPush,
  styleUrl: './reports.page.css',
  templateUrl: './reports.page.html',
})
export class ReportsPage implements OnInit {
  private readonly api = inject(ApiService);

  readonly loading = signal(true);
  readonly tasks = signal<Task[]>([]);
  readonly sessions = signal<SessionSummary[]>([]);
  readonly argusInfo = signal<ArgusInfo | null>(null);
  readonly argusFindings = signal<Record<string, unknown>[]>([]);
  readonly generatedAt = signal<number>(0);

  // ---- Helpers ----
  readonly ageFromEpochSeconds = ageFromEpochSeconds;
  readonly fmtTs = fmtTs;
  readonly fmtUsd = fmtUsd;
  readonly shortenId = shortenId;

  // ---- Standup ----
  readonly standupBuckets = computed<StandupBucket[]>(() => {
    const active = this.tasks().filter(t =>
      t.status === 'in_progress' || t.status === 'ready' ||
      t.status === 'triage' || t.status === 'blocked',
    );
    const now = Math.floor(Date.now() / 1000);
    const map = new Map<string, StandupBucket>();
    for (const t of active) {
      const key = t.assignee || '(unassigned)';
      let b = map.get(key);
      if (!b) {
        b = { assignee: key, total: 0, in_progress: 0, ready: 0, blocked: 0, triage: 0,
              oldestActiveAgeSec: 0, tickets: [] };
        map.set(key, b);
      }
      b.total++;
      if (t.status === 'in_progress') b.in_progress++;
      else if (t.status === 'ready') b.ready++;
      else if (t.status === 'blocked') b.blocked++;
      else if (t.status === 'triage') b.triage++;
      const age = now - (t.started_at ?? t.created_at);
      if (age > b.oldestActiveAgeSec) b.oldestActiveAgeSec = age;
      b.tickets.push(t);
    }
    // Sort tickets within each bucket: in_progress first, then by priority+age.
    for (const b of map.values()) {
      b.tickets.sort((a, c) => {
        const order = (s: string) => s === 'in_progress' ? 0 : s === 'blocked' ? 1 : s === 'ready' ? 2 : 3;
        const o = order(a.status) - order(c.status);
        return o !== 0 ? o : (c.priority - a.priority) || (c.created_at - a.created_at);
      });
    }
    return [...map.values()].sort((a, b) => b.in_progress - a.in_progress || b.total - a.total);
  });

  readonly staleItems = computed<Task[]>(() => {
    const now = Math.floor(Date.now() / 1000);
    return this.tasks()
      .filter(t => {
        if (t.status === 'in_progress') {
          const age = now - (t.started_at ?? t.created_at);
          return age > 2 * 86400;  // in_progress > 2d
        }
        if (t.status === 'blocked') {
          return (now - t.created_at) > 7 * 86400;  // blocked > 7d
        }
        if (t.status === 'ready') {
          return (now - t.created_at) > 5 * 86400;
        }
        return false;
      })
      .sort((a, b) => (a.started_at ?? a.created_at) - (b.started_at ?? b.created_at));
  });

  // ---- Chain Summary ----
  readonly chainSummary = computed(() => {
    const ss = this.sessions();
    let total = 0, allowed = 0, denied = 0, heuristic = 0, costUsd = 0;
    const toolCounts = new Map<string, number>();
    const dayBuckets = new Map<string, ChainDayBucket>();
    for (const s of ss) {
      total += s.events;
      allowed += s.allowed;
      denied += s.denied;
      heuristic += s.heuristic;
      costUsd += s.costUsd;
      for (const t of s.tools || []) toolCounts.set(t, (toolCounts.get(t) || 0) + 1);
      const day = (s.lastTs || s.firstTs || '').slice(0, 10);
      if (day) {
        let b = dayBuckets.get(day);
        if (!b) { b = { day, events: 0, allowed: 0, denied: 0, costUsd: 0 }; dayBuckets.set(day, b); }
        b.events += s.events;
        b.allowed += s.allowed;
        b.denied += s.denied;
        b.costUsd += s.costUsd;
      }
    }
    const denyRate = total > 0 ? denied / total : 0;
    const topTools: ChainTool[] = [...toolCounts.entries()]
      .sort((a, b) => b[1] - a[1])
      .slice(0, 8)
      .map(([tool, count]) => ({ tool, count }));
    const byDay = [...dayBuckets.values()].sort((a, b) => a.day.localeCompare(b.day));
    return { total, allowed, denied, heuristic, costUsd, denyRate, sessionCount: ss.length, topTools, byDay };
  });

  // ---- Board Audit ----
  readonly boardAudit = computed(() => {
    const now = Math.floor(Date.now() / 1000);
    const blockedNoReason = this.tasks().filter(t =>
      t.status === 'blocked' && !(t as { block_reason?: string | null }).block_reason,
    );
    const longBlocked = this.tasks()
      .filter(t => t.status === 'blocked' && (now - t.created_at) > 14 * 86400)
      .sort((a, b) => a.created_at - b.created_at);
    const inProgressTooLong = this.tasks()
      .filter(t => t.status === 'in_progress' && (now - (t.started_at ?? t.created_at)) > 3 * 86400)
      .sort((a, b) => (a.started_at ?? a.created_at) - (b.started_at ?? b.created_at));
    // Duplicate-title heuristic — two active tickets with identical normalized titles.
    const norm = (s: string) => s.toLowerCase().replace(/\s+/g, ' ').trim();
    const titleMap = new Map<string, Task[]>();
    for (const t of this.tasks()) {
      if (t.status === 'done' || t.status === 'archived') continue;
      const k = norm(t.title);
      const list = titleMap.get(k) || [];
      list.push(t);
      titleMap.set(k, list);
    }
    const duplicates = [...titleMap.values()].filter(list => list.length > 1);
    const highConsecutiveFailures = this.tasks()
      .filter(t => (t.consecutive_failures || 0) >= 2)
      .sort((a, b) => b.consecutive_failures - a.consecutive_failures);
    return { blockedNoReason, longBlocked, inProgressTooLong, duplicates, highConsecutiveFailures };
  });

  // ---- Argus Research ----
  readonly argusBuckets = computed(() => {
    const findings = this.argusFindings();
    const bySource = new Map<string, Record<string, unknown>[]>();
    for (const f of findings) {
      const src = String((f as Record<string, unknown>)['source'] ?? (f as Record<string, unknown>)['kind'] ?? 'other');
      const list = bySource.get(src) || [];
      list.push(f);
      bySource.set(src, list);
    }
    return [...bySource.entries()].map(([source, items]) => ({ source, items })).sort((a, b) => b.items.length - a.items.length);
  });

  ngOnInit() { this.reload(); }

  reload() {
    this.loading.set(true);
    forkJoin({
      tasks: this.api.tasks({ status: 'in_progress,ready,triage,blocked,done', limit: 2000 }),
      sessions: this.api.sessions(200),
      argusInfo: this.api.argusInfo(),
      argusFindings: this.api.argusFindings(50),
    }).subscribe({
      next: ({ tasks, sessions, argusInfo, argusFindings }) => {
        this.tasks.set(tasks.tasks);
        this.sessions.set(sessions.sessions);
        this.argusInfo.set(argusInfo);
        this.argusFindings.set(argusFindings.findings);
        this.generatedAt.set(Date.now());
        this.loading.set(false);
      },
      error: () => this.loading.set(false),
    });
  }

  /** Epoch-seconds "now" — used by template duration computations. */
  now(): number { return Math.floor(Date.now() / 1000); }

  fmtPct(n: number, digits = 1): string {
    return (n * 100).toFixed(digits) + '%';
  }

  /** Convert seconds → short human duration (m/h/d). */
  fmtDur(sec: number): string {
    if (!sec || sec < 0) return '0s';
    if (sec < 60) return `${sec}s`;
    if (sec < 3600) return `${Math.floor(sec / 60)}m`;
    if (sec < 86400) return `${Math.floor(sec / 3600)}h`;
    return `${Math.floor(sec / 86400)}d`;
  }
}
