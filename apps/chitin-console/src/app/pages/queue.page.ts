import { ChangeDetectionStrategy, Component, OnInit, inject, signal, computed } from '@angular/core';
import { CommonModule } from '@angular/common';
import { RouterModule } from '@angular/router';
import { FormsModule } from '@angular/forms';
import { ApiService } from '../api.service';
import type { Task, TaskComment } from '../api.types';
import { StatusPillComponent } from '../ui/status-pill.component';
import { LoaderComponent } from '../ui/loader.component';
import { EmptyStateComponent } from '../ui/empty-state.component';
import { ageFromEpochSeconds, fmtTs, shortenId } from '../utils';

const AGENT_ASSIGNEES = new Set<string>([
  'codex', 'claude-code', 'copilot', 'gemini', 'clawta', 'chitin-worker', 'hermes',
]);

@Component({
  selector: 'cc-queue',
  standalone: true,
  imports: [CommonModule, RouterModule, FormsModule, StatusPillComponent, LoaderComponent, EmptyStateComponent],
  changeDetection: ChangeDetectionStrategy.OnPush,
  styleUrl: './queue.page.css',
  templateUrl: './queue.page.html',
})
export class QueuePage implements OnInit {
  private readonly api = inject(ApiService);

  readonly loading = signal(true);
  readonly tasks = signal<Task[]>([]);
  // detail map keyed by task id, populated lazily as rows expand
  readonly commentsMap = signal<Record<string, TaskComment[]>>({});
  readonly expanded = signal<Record<string, boolean>>({});
  readonly copiedId = signal<string | null>(null);
  readonly mutating = signal<string | null>(null);
  readonly mutationError = signal<string | null>(null);

  reasonFilter: 'all' | 'blocked' | 'unassigned' | 'operator' = 'all';
  assigneeFilter = '';

  readonly ageFromEpochSeconds = ageFromEpochSeconds;
  readonly fmtTs = fmtTs;
  readonly shortenId = shortenId;

  /** All HITL tickets — blocked OR non-agent assignee. */
  readonly hitlTickets = computed<Task[]>(() => {
    return this.tasks().filter(t => {
      if (t.status === 'done' || t.status === 'archived') return false;
      if (t.status === 'blocked') return true;
      if (!t.assignee) return true;  // unassigned = operator queue
      return !AGENT_ASSIGNEES.has(t.assignee);
    });
  });

  readonly filteredTickets = computed<Task[]>(() => {
    return this.hitlTickets().filter(t => {
      if (this.reasonFilter === 'blocked' && t.status !== 'blocked') return false;
      if (this.reasonFilter === 'unassigned' && t.assignee) return false;
      if (this.reasonFilter === 'operator') {
        if (t.status === 'blocked') return false;  // operator-lane only
        if (!t.assignee) return false;
        if (AGENT_ASSIGNEES.has(t.assignee)) return false;
      }
      if (this.assigneeFilter && (t.assignee || '') !== this.assigneeFilter) return false;
      return true;
    }).sort((a, b) => {
      // Sort: blocked first, then by age desc (oldest highest)
      if (a.status === 'blocked' && b.status !== 'blocked') return -1;
      if (b.status === 'blocked' && a.status !== 'blocked') return 1;
      return a.created_at - b.created_at;
    });
  });

  /** Distinct non-agent assignees present in the queue, for the filter dropdown. */
  readonly availableAssignees = computed<string[]>(() => {
    const set = new Set<string>();
    for (const t of this.hitlTickets()) {
      if (t.assignee) set.add(t.assignee);
    }
    return [...set].sort();
  });

  readonly summary = computed(() => {
    const all = this.hitlTickets();
    return {
      total: all.length,
      blocked: all.filter(t => t.status === 'blocked').length,
      unassigned: all.filter(t => !t.assignee).length,
      operator: all.filter(t => t.assignee && !AGENT_ASSIGNEES.has(t.assignee) && t.status !== 'blocked').length,
    };
  });

  ngOnInit() { this.reload(); }

  reload() {
    this.loading.set(true);
    this.api.tasks({ status: 'blocked,ready,in_progress,triage', limit: 1000 }).subscribe({
      next: (r) => { this.tasks.set(r.tasks); this.loading.set(false); },
      error: () => this.loading.set(false),
    });
  }

  toggleExpanded(id: string) {
    const e = { ...this.expanded() };
    e[id] = !e[id];
    this.expanded.set(e);
    // Lazily fetch comments on first expand.
    if (e[id] && !(id in this.commentsMap())) {
      this.api.task(id).subscribe(d => {
        const cm = { ...this.commentsMap() };
        cm[id] = d.comments || [];
        this.commentsMap.set(cm);
      });
    }
  }

  buildPrompt(t: Task, comments: TaskComment[]): string {
    const blockReason = (t as { block_reason?: string | null }).block_reason || '(none — check comments/events)';
    const bodyExcerpt = ((t as Task & { body?: string | null }).body || '').slice(0, 1200);
    const recent = comments.slice(-3).map(c => `  - ${c.author}: ${c.body.slice(0, 200)}`).join('\n') || '  (none)';
    const author = t.assignee || 'red';
    return `I need help with a human-in-the-loop ticket on the chitin board.

**Ticket:** ${t.id} — ${t.title}
**Status:** ${t.status}
**Assignee:** ${t.assignee || '(unassigned)'}
**Priority:** ${t.priority}
**Block reason:** ${blockReason}
**Repo:** ~/workspace/chitin
**Console:** http://100.115.89.9:7878/tickets?id=${t.id}

**Body:**
${bodyExcerpt || '(empty)'}${(((t as Task & { body?: string | null }).body) || '').length > 1200 ? '\n…[truncated; see /tickets?id=' + t.id + ' for full body]' : ''}

**Recent comments:**
${recent}

Please investigate, then take one of these actions:
- Add a comment with the next step (via the console at \`/tickets?id=${t.id}\`)
- Unblock it: \`kanban-flow unblock ${t.id} --author ${author}\`
- Mark done: \`kanban-flow done ${t.id} --result "<summary>" --author ${author}\`
- Re-block: \`kanban-flow block ${t.id} "<reason>" --author ${author}\`

Use any chitin / hermes / clawta tooling available. Report what you found and what you changed.`;
  }

  promptFor(t: Task): string {
    const comments = this.commentsMap()[t.id] || [];
    return this.buildPrompt(t, comments);
  }

  copyPrompt(t: Task) {
    const text = this.promptFor(t);
    void navigator.clipboard.writeText(text).then(
      () => {
        this.copiedId.set(t.id);
        setTimeout(() => { if (this.copiedId() === t.id) this.copiedId.set(null); }, 1800);
      },
      () => { this.copiedId.set(null); },
    );
  }

  quickUnblock(t: Task) {
    this.mutating.set(t.id);
    this.mutationError.set(null);
    this.api.updateTaskStatus(t.id, { status: 'unblock' }).subscribe({
      next: () => { this.mutating.set(null); this.reload(); },
      error: (err) => {
        this.mutating.set(null);
        const msg = err?.error?.detail || err?.error?.stderr || err?.message || 'unblock failed';
        this.mutationError.set(`${t.id}: ${msg}`);
      },
    });
  }
}
