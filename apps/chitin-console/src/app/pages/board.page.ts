import { ChangeDetectionStrategy, Component, OnInit, inject, signal, computed } from '@angular/core';
import { CommonModule } from '@angular/common';
import { RouterModule } from '@angular/router';
import { FormsModule } from '@angular/forms';
import { ApiService } from '../api.service';
import type { Task } from '../api.types';
import { StatusPillComponent } from '../ui/status-pill.component';
import { LoaderComponent } from '../ui/loader.component';
import { ageFromEpochSeconds, shortenId } from '../utils';

interface Column {
  status: string;
  label: string;
  /** Verb to pass to POST /api/tasks/:id/status when dropping into this column. */
  flowVerb: string;
  /** Status transitions kanban-flow won't accept — disables dropping. */
  forbiddenFrom?: string[];
}

const COLUMNS: Column[] = [
  { status: 'triage',      label: 'Triage',      flowVerb: 'demote',  forbiddenFrom: ['triage', 'in_progress', 'done', 'archived'] },
  { status: 'ready',       label: 'Ready',       flowVerb: 'ready',   forbiddenFrom: ['ready', 'in_progress', 'done', 'archived'] },
  { status: 'in_progress', label: 'In progress', flowVerb: 'start',   forbiddenFrom: ['in_progress', 'blocked', 'done', 'archived'] },
  { status: 'blocked',     label: 'Blocked',     flowVerb: 'block',   forbiddenFrom: ['blocked', 'done', 'archived'] },
  { status: 'done',        label: 'Done',        flowVerb: 'done',    forbiddenFrom: ['done', 'archived'] },
];

@Component({
  selector: 'cc-board',
  standalone: true,
  imports: [CommonModule, RouterModule, FormsModule, StatusPillComponent, LoaderComponent],
  changeDetection: ChangeDetectionStrategy.OnPush,
  styleUrl: './board.page.css',
  templateUrl: './board.page.html',
})
export class BoardPage implements OnInit {
  private readonly api = inject(ApiService);

  readonly loading = signal(true);
  readonly tasks = signal<Task[]>([]);
  readonly assignees = signal<{ assignee: string; n: number }[]>([]);
  readonly mutating = signal<string | null>(null);
  readonly mutationError = signal<string | null>(null);
  readonly draggingId = signal<string | null>(null);
  readonly dragOverColumn = signal<string | null>(null);

  assigneeFilter = '';
  searchFilter = '';

  readonly columns = COLUMNS;
  readonly shortenId = shortenId;
  readonly ageFromEpochSeconds = ageFromEpochSeconds;

  readonly filteredTasks = computed(() => {
    const a = this.assigneeFilter;
    const q = this.searchFilter.trim().toLowerCase();
    return this.tasks().filter(t => {
      if (a && t.assignee !== a) return false;
      if (q && !(t.id.toLowerCase().includes(q) || t.title.toLowerCase().includes(q))) return false;
      return true;
    });
  });

  readonly tasksByColumn = computed(() => {
    const out: Record<string, Task[]> = {};
    for (const c of COLUMNS) out[c.status] = [];
    for (const t of this.filteredTasks()) {
      if (out[t.status]) out[t.status].push(t);
    }
    // Sort each column: in_progress by started_at desc; others by priority desc
    // then created_at desc — same precedence the list page uses.
    for (const c of COLUMNS) {
      out[c.status].sort((a, b) => {
        if (c.status === 'in_progress') return (b.started_at ?? 0) - (a.started_at ?? 0);
        const pri = (b.priority ?? 0) - (a.priority ?? 0);
        return pri !== 0 ? pri : (b.created_at ?? 0) - (a.created_at ?? 0);
      });
    }
    return out;
  });

  readonly columnCounts = computed(() => {
    const out: Record<string, number> = {};
    for (const c of COLUMNS) out[c.status] = this.tasksByColumn()[c.status].length;
    return out;
  });

  ngOnInit() {
    this.api.assignees().subscribe(r => this.assignees.set(r.assignees));
    this.reload();
  }

  reload() {
    this.loading.set(true);
    // Pull every active lane in one shot — the board needs all of them.
    this.api.tasks({ status: 'triage,ready,in_progress,blocked,done', limit: 1000 }).subscribe({
      next: (r) => { this.tasks.set(r.tasks); this.loading.set(false); },
      error: () => this.loading.set(false),
    });
  }

  // --- Drag and drop ---

  onDragStart(ev: DragEvent, task: Task) {
    if (!ev.dataTransfer) return;
    ev.dataTransfer.setData('text/plain', task.id);
    ev.dataTransfer.effectAllowed = 'move';
    this.draggingId.set(task.id);
  }

  onDragEnd() {
    this.draggingId.set(null);
    this.dragOverColumn.set(null);
  }

  onDragOver(ev: DragEvent, column: Column) {
    const id = this.draggingId();
    if (!id) return;
    const task = this.tasks().find(t => t.id === id);
    if (!task) return;
    if (column.forbiddenFrom?.includes(task.status)) return;
    if (task.status === column.status) return;
    ev.preventDefault();
    this.dragOverColumn.set(column.status);
  }

  onDragLeave(column: Column) {
    if (this.dragOverColumn() === column.status) this.dragOverColumn.set(null);
  }

  async onDrop(ev: DragEvent, column: Column) {
    ev.preventDefault();
    const id = ev.dataTransfer?.getData('text/plain') || this.draggingId();
    this.dragOverColumn.set(null);
    this.draggingId.set(null);
    if (!id) return;
    const task = this.tasks().find(t => t.id === id);
    if (!task || task.status === column.status) return;
    if (column.forbiddenFrom?.includes(task.status)) return;

    // block/done/demote require a reason — prompt the operator inline.
    let reason: string | undefined;
    if (column.flowVerb === 'block' || column.flowVerb === 'demote') {
      const r = window.prompt(`${column.flowVerb} ${task.id} — reason?`);
      if (!r || !r.trim()) return;
      reason = r.trim();
    } else if (column.flowVerb === 'done') {
      const r = window.prompt(`done ${task.id} — result summary?`);
      if (!r || !r.trim()) return;
      reason = r.trim();
    }

    // Optimistic update — flip the card's status before the request settles.
    const prev = task.status;
    this.tasks.update(list => list.map(t => t.id === id ? { ...t, status: column.status } : t));
    this.mutating.set(id);
    this.mutationError.set(null);

    this.api.updateTaskStatus(id, { status: column.flowVerb, reason }).subscribe({
      next: (r) => {
        this.mutating.set(null);
        if (r.task?.task) {
          this.tasks.update(list => list.map(t => t.id === id ? { ...t, ...r.task!.task } : t));
        }
      },
      error: (err) => {
        this.mutating.set(null);
        // Roll back the optimistic update.
        this.tasks.update(list => list.map(t => t.id === id ? { ...t, status: prev } : t));
        const detail = err?.error?.detail || err?.error?.stderr || err?.message || 'transition rejected';
        this.mutationError.set(`${id}: ${detail}`);
      },
    });
  }
}
