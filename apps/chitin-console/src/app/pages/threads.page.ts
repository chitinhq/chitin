import { ChangeDetectionStrategy, Component, OnDestroy, OnInit, inject, signal } from '@angular/core';
import { CommonModule } from '@angular/common';
import { FormsModule } from '@angular/forms';
import { ActivatedRoute, Router, RouterModule } from '@angular/router';
import { Subscription } from 'rxjs';
import { ApiService } from '../api.service';
import type { ThreadDetail, ThreadSummary } from '../api.types';
import { AttachmentBadgeComponent } from '../ui/attachment-badge.component';
import { EmptyStateComponent } from '../ui/empty-state.component';
import { LoaderComponent } from '../ui/loader.component';
import { StatusPillComponent } from '../ui/status-pill.component';
import { ageFromEpochSeconds, fmtTs } from '../utils';

@Component({
  selector: 'cc-threads',
  standalone: true,
  imports: [
    CommonModule,
    FormsModule,
    RouterModule,
    AttachmentBadgeComponent,
    EmptyStateComponent,
    LoaderComponent,
    StatusPillComponent,
  ],
  changeDetection: ChangeDetectionStrategy.OnPush,
  styleUrl: './threads.page.css',
  templateUrl: './threads.page.html',
})
export class ThreadsPage implements OnInit, OnDestroy {
  private readonly api = inject(ApiService);
  private readonly route = inject(ActivatedRoute);
  private readonly router = inject(Router);
  private routeSub?: Subscription;

  readonly loading = signal(true);
  readonly drawerLoading = signal(false);
  readonly threads = signal<ThreadSummary[]>([]);
  readonly selectedDetail = signal<ThreadDetail | null>(null);

  board = '';
  status = 'open';
  audience = '';
  q = '';

  readonly fmtTs = fmtTs;
  readonly ageFromEpochSeconds = ageFromEpochSeconds;
  readonly statusOptions = [
    { label: 'open', value: 'open' },
    { label: 'resolved', value: 'resolved' },
    { label: 'archived', value: 'archived' },
    { label: 'all', value: '' },
  ];

  ngOnInit() {
    this.reload();
    this.routeSub = this.route.queryParamMap.subscribe((params) => {
      const rawId = params.get('id');
      if (!rawId) {
        this.selectedDetail.set(null);
        this.drawerLoading.set(false);
        return;
      }
      const id = Number(rawId);
      if (!Number.isFinite(id) || id <= 0) return;
      if (this.selectedDetail()?.thread.id === id) return;
      this.loadThread(id);
    });
  }

  ngOnDestroy() {
    this.routeSub?.unsubscribe();
  }

  reload() {
    this.loading.set(true);
    this.api.threads({
      board: this.board || undefined,
      status: this.status || undefined,
      audience: this.audience || undefined,
      q: this.q || undefined,
      limit: 500,
    }).subscribe({
      next: (r) => {
        this.threads.set(r.threads);
        this.loading.set(false);
      },
      error: () => this.loading.set(false),
    });
  }

  openThread(id: number) {
    this.router.navigate([], { queryParams: { id }, queryParamsHandling: 'merge' });
  }

  closeDrawer() {
    this.selectedDetail.set(null);
    this.router.navigate([], { queryParams: { id: null }, queryParamsHandling: 'merge' });
  }

  private loadThread(id: number) {
    this.drawerLoading.set(true);
    this.selectedDetail.set(null);
    this.api.thread(id).subscribe({
      next: (r) => {
        this.selectedDetail.set(r);
        this.drawerLoading.set(false);
      },
      error: () => {
        this.selectedDetail.set(null);
        this.drawerLoading.set(false);
      },
    });
  }
}
