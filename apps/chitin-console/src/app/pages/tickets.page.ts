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
    this.router.navigate([], { queryParams: { id }, queryParamsHandling: 'merge' });
    this.api.task(id).subscribe({
      next: (r) => { this.selectedDetail.set(r); this.drawerLoading.set(false); },
      error: () => this.drawerLoading.set(false),
    });
  }

  closeDrawer() {
    this.selectedDetail.set(null);
    this.router.navigate([], { queryParams: { id: null }, queryParamsHandling: 'merge' });
  }

  prettyJson(s: string | null | undefined): string {
    if (!s) return '';
    try { return JSON.stringify(JSON.parse(s), null, 2); } catch { return s; }
  }
}
