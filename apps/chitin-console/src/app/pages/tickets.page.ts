import { ChangeDetectionStrategy, Component, OnInit, inject, signal, computed, effect } from '@angular/core';
import { CommonModule } from '@angular/common';
import { RouterModule, ActivatedRoute, Router } from '@angular/router';
import { FormsModule } from '@angular/forms';
import { ApiService } from '../api.service';
import type { Task, TaskDetail, AssigneeRow } from '../api.types';
import { StatusPillComponent } from '../ui/status-pill.component';
import { LoaderComponent } from '../ui/loader.component';
import { EmptyStateComponent } from '../ui/empty-state.component';
import { ageFromEpochSeconds, fmtTs, shortenId, priorityBarWidth } from '../utils';

@Component({
  selector: 'cc-tickets',
  standalone: true,
  imports: [
    CommonModule, RouterModule, FormsModule,
    StatusPillComponent, LoaderComponent, EmptyStateComponent,
  ],
  changeDetection: ChangeDetectionStrategy.OnPush,
  styleUrl: './tickets.page.css',
  templateUrl: './tickets.page.html',
})
export class TicketsPage implements OnInit {
  private readonly api = inject(ApiService);
  private readonly route = inject(ActivatedRoute);
  private readonly router = inject(Router);

  readonly loading = signal(true);
  readonly tasks = signal<Task[]>([]);
  readonly assignees = signal<AssigneeRow[]>([]);
  readonly selectedDetail = signal<TaskDetail | null>(null);
  readonly drawerLoading = signal(false);

  // Status-mutation UI state. Lives on the page (not the drawer) so
  // the form resets cleanly when the drawer reopens for a new ticket.
  readonly mutating = signal(false);
  readonly mutationError = signal<string | null>(null);
  pendingStatus = '';
  pendingReason = '';

  readonly mutationOptions: { value: string; label: string; needsReason: boolean }[] = [
    { value: '',        label: '—',                       needsReason: false },
    { value: 'start',   label: 'start (→ in_progress)',   needsReason: false },
    { value: 'ready',   label: 'ready (→ ready)',         needsReason: false },
    { value: 'unblock', label: 'unblock (blocked → ready)', needsReason: false },
    { value: 'block',   label: 'block (→ blocked)',       needsReason: true  },
    { value: 'demote',  label: 'demote (→ triage)',       needsReason: true  },
    { value: 'done',    label: 'done (→ done)',           needsReason: true  },
  ];

  // Comment composer state.
  readonly commenting = signal(false);
  readonly commentError = signal<string | null>(null);
  pendingComment = '';

  status = 'in_progress,triage,ready,todo';
  assignee = '';
  q = '';

  readonly statusOptions: { label: string; value: string }[] = [
    { label: 'active (default)', value: 'in_progress,triage,ready,todo' },
    { label: 'in_progress',      value: 'in_progress' },
    { label: 'triage',           value: 'triage' },
    { label: 'ready',            value: 'ready' },
    { label: 'todo',             value: 'todo' },
    { label: 'done',             value: 'done' },
    { label: 'archived',         value: 'archived' },
    { label: 'all',              value: '' },
  ];

  readonly ageFromEpochSeconds = ageFromEpochSeconds;
  readonly fmtTs = fmtTs;
  readonly shortenId = shortenId;
  readonly priorityBarWidth = priorityBarWidth;

  readonly summary = computed(() => {
    const list = this.tasks();
    const total = list.length;
    const byStatus: Record<string, number> = {};
    for (const t of list) byStatus[t.status] = (byStatus[t.status] || 0) + 1;
    return { total, byStatus };
  });

  constructor() {
    // React to query param "id" — open drawer for that ticket
    effect(() => {
      const params = this.route.snapshot.queryParamMap;
      const id = params.get('id');
      if (id) this.openTicket(id);
    });
  }

  ngOnInit() {
    this.api.assignees().subscribe(r => this.assignees.set(r.assignees));
    this.reload();
    const initialId = this.route.snapshot.queryParamMap.get('id');
    if (initialId) this.openTicket(initialId);
  }

  reload() {
    this.loading.set(true);
    this.api.tasks({
      status: this.status || undefined,
      assignee: this.assignee || undefined,
      q: this.q || undefined,
      limit: 500,
    }).subscribe({
      next: (r) => { this.tasks.set(r.tasks); this.loading.set(false); },
      error: () => this.loading.set(false),
    });
  }

  openTicket(id: string) {
    this.drawerLoading.set(true);
    this.selectedDetail.set(null);
    this.pendingStatus = '';
    this.pendingReason = '';
    this.pendingComment = '';
    this.mutationError.set(null);
    this.commentError.set(null);
    this.router.navigate([], { queryParams: { id }, queryParamsHandling: 'merge' });
    this.api.task(id).subscribe({
      next: (r) => { this.selectedDetail.set(r); this.drawerLoading.set(false); },
      error: () => this.drawerLoading.set(false),
    });
  }

  closeDrawer() {
    this.selectedDetail.set(null);
    this.pendingStatus = '';
    this.pendingReason = '';
    this.pendingComment = '';
    this.mutationError.set(null);
    this.commentError.set(null);
    this.router.navigate([], { queryParams: { id: null }, queryParamsHandling: 'merge' });
  }

  selectedNeedsReason(): boolean {
    return this.mutationOptions.find(o => o.value === this.pendingStatus)?.needsReason ?? false;
  }

  submitStatusChange() {
    const detail = this.selectedDetail();
    if (!detail || !this.pendingStatus) return;
    if (this.selectedNeedsReason() && !this.pendingReason.trim()) {
      this.mutationError.set(`${this.pendingStatus} requires a reason`);
      return;
    }
    this.mutating.set(true);
    this.mutationError.set(null);
    this.api.updateTaskStatus(detail.task.id, {
      status: this.pendingStatus,
      reason: this.pendingReason.trim() || undefined,
    }).subscribe({
      next: (r) => {
        this.mutating.set(false);
        this.pendingStatus = '';
        this.pendingReason = '';
        if (r.task) this.selectedDetail.set(r.task);
        // Reload list so the ticket's new status is reflected in the table.
        this.reload();
      },
      error: (err) => {
        this.mutating.set(false);
        const detail = err?.error?.detail || err?.error?.stderr || err?.message || 'unknown error';
        this.mutationError.set(String(detail));
      },
    });
  }

  submitComment() {
    const detail = this.selectedDetail();
    const body = this.pendingComment.trim();
    if (!detail || !body) return;
    this.commenting.set(true);
    this.commentError.set(null);
    this.api.addTaskComment(detail.task.id, { body }).subscribe({
      next: (r) => {
        this.commenting.set(false);
        this.pendingComment = '';
        if (r.task) this.selectedDetail.set(r.task);
      },
      error: (err) => {
        this.commenting.set(false);
        const msg = err?.error?.detail || err?.message || 'failed to post comment';
        this.commentError.set(String(msg));
      },
    });
  }

  prettyJson(s: string | null | undefined): string {
    if (!s) return '';
    try { return JSON.stringify(JSON.parse(s), null, 2); } catch { return s; }
  }
}
