import { ChangeDetectionStrategy, Component, OnInit, OnDestroy, inject, signal, effect } from '@angular/core';
import { CommonModule } from '@angular/common';
import { RouterModule, ActivatedRoute, Router } from '@angular/router';
import { FormsModule } from '@angular/forms';
import { ApiService, type BusThread, type BusThreadDetail } from '../api.service';
import { LoaderComponent } from '../ui/loader.component';
import { EmptyStateComponent } from '../ui/empty-state.component';
import { fmtTs, shortenId } from '../utils';

@Component({
  selector: 'cc-threads',
  standalone: true,
  imports: [CommonModule, RouterModule, FormsModule, LoaderComponent, EmptyStateComponent],
  changeDetection: ChangeDetectionStrategy.OnPush,
  styleUrl: './threads.page.css',
  templateUrl: './threads.page.html',
})
export class ThreadsPage implements OnInit, OnDestroy {
  private readonly api = inject(ApiService);
  private readonly route = inject(ActivatedRoute);
  private readonly router = inject(Router);

  readonly loading = signal(true);
  readonly threads = signal<BusThread[]>([]);
  readonly selected = signal<BusThreadDetail | null>(null);
  readonly detailLoading = signal(false);
  readonly posting = signal(false);
  readonly postError = signal<string | null>(null);
  readonly discordChannels = signal<{ id: string; name: string }[]>([]);

  composer = '';
  authorOverride = '';
  boardFilter = '';
  statusFilter = 'open';
  composerChannelId = '';  // empty = thread.discord_thread_id or first known

  readonly fmtTs = fmtTs;
  readonly shortenId = shortenId;

  private pollTimer: ReturnType<typeof setInterval> | null = null;

  constructor() {
    effect(() => {
      const id = this.route.snapshot.queryParamMap.get('thread');
      if (id) this.openThread(Number(id));
    });
  }

  ngOnInit() {
    this.reload();
    this.api.discordChannels().subscribe(r => this.discordChannels.set(r.channels));
    const initial = this.route.snapshot.queryParamMap.get('thread');
    if (initial) this.openThread(Number(initial));
    // Light polling so new Discord-mirrored messages show up automatically.
    this.pollTimer = setInterval(() => {
      this.reload(/*silent=*/true);
      const sel = this.selected();
      if (sel) this.openThread(sel.thread.id, /*silent=*/true);
    }, 15_000);
  }

  ngOnDestroy() { if (this.pollTimer) clearInterval(this.pollTimer); }

  reload(silent = false) {
    if (!silent) this.loading.set(true);
    this.api.threads({
      board: this.boardFilter || undefined,
      status: this.statusFilter || undefined,
      limit: 200,
    }).subscribe({
      next: (r) => { this.threads.set(r.threads); this.loading.set(false); },
      error: () => this.loading.set(false),
    });
  }

  openThread(id: number, silent = false) {
    if (!silent) {
      this.detailLoading.set(true);
      this.selected.set(null);
      this.postError.set(null);
    }
    this.router.navigate([], { queryParams: { thread: id }, queryParamsHandling: 'merge' });
    this.api.thread(id).subscribe({
      next: (d) => { this.selected.set(d); this.detailLoading.set(false); },
      error: () => this.detailLoading.set(false),
    });
  }

  closeThread() {
    this.selected.set(null);
    this.composer = '';
    this.router.navigate([], { queryParams: { thread: null }, queryParamsHandling: 'merge' });
  }

  send() {
    const sel = this.selected();
    const text = this.composer.trim();
    if (!sel || !text) return;
    this.posting.set(true);
    this.postError.set(null);
    this.api.postThreadReply(sel.thread.id, {
      body: text,
      author: this.authorOverride.trim() || undefined,
      channel_id: this.composerChannelId || undefined,
    }).subscribe({
      next: (r) => {
        this.posting.set(false);
        this.composer = '';
        if (r.thread) this.selected.set(r.thread);
        // Refresh list so the thread's updated_at moves to the top.
        this.reload(true);
      },
      error: (err) => {
        this.posting.set(false);
        const msg = err?.error?.detail || err?.error?.error || err?.message || 'failed to send';
        this.postError.set(String(msg));
      },
    });
  }
}
