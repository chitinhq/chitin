import {
  ChangeDetectionStrategy, Component, OnInit, OnDestroy,
  ViewChild, ElementRef, inject, signal, AfterViewChecked,
} from '@angular/core';
import { CommonModule } from '@angular/common';
import { RouterModule, ActivatedRoute, Router } from '@angular/router';
import { FormsModule } from '@angular/forms';
import { Subscription } from 'rxjs';
import { ApiService, type BusThread, type BusThreadDetail } from '../api.service';
import { LoaderComponent } from '../ui/loader.component';
import { EmptyStateComponent } from '../ui/empty-state.component';
import { fmtTs, shortenId } from '../utils';

const PAGE_SIZE = 50;
/** Distance from the bottom (px) below which we treat the user as "pinned". */
const PIN_THRESHOLD_PX = 80;

@Component({
  selector: 'cc-threads',
  standalone: true,
  imports: [CommonModule, RouterModule, FormsModule, LoaderComponent, EmptyStateComponent],
  changeDetection: ChangeDetectionStrategy.OnPush,
  styleUrl: './threads.page.css',
  templateUrl: './threads.page.html',
})
export class ThreadsPage implements OnInit, OnDestroy, AfterViewChecked {
  private readonly api = inject(ApiService);
  private readonly route = inject(ActivatedRoute);
  private readonly router = inject(Router);

  readonly loading = signal(true);
  readonly threads = signal<BusThread[]>([]);
  readonly selected = signal<BusThreadDetail | null>(null);
  readonly detailLoading = signal(false);
  readonly loadingOlder = signal(false);
  readonly posting = signal(false);
  readonly postError = signal<string | null>(null);
  readonly discordChannels = signal<{ id: string; name: string }[]>([]);
  /** Count of new messages received while scrolled up. */
  readonly newSince = signal(0);

  composer = '';
  authorOverride = '';
  boardFilter = '';
  statusFilter = 'open';
  composerChannelId = '';

  readonly fmtTs = fmtTs;
  readonly shortenId = shortenId;

  @ViewChild('messageList') messageList?: ElementRef<HTMLElement>;

  /** True when we should auto-scroll-to-bottom on next viewChecked. */
  private pendingScrollToBottom = false;
  /** Saved scroll metrics for prepend (lazy-load older). */
  private prependAnchor: { prevScrollTop: number; prevScrollHeight: number } | null = null;
  /** Whether the user is currently pinned to the bottom of the feed. */
  private pinnedToBottom = true;

  private pollTimer: ReturnType<typeof setInterval> | null = null;
  private queryParamSub?: Subscription;

  ngOnInit() {
    this.reload();
    this.api.discordChannels().subscribe(r => this.discordChannels.set(r.channels));
    // Subscribe to route params (RxJS observable, not snapshot) so we
    // react to deep-links and back/forward navigation, but only when
    // the value actually changes — avoids the constructor-effect feedback
    // loop where router.navigate would re-trigger the loader.
    this.queryParamSub = this.route.queryParamMap.subscribe(p => {
      const id = p.get('thread');
      const sel = this.selected();
      if (id) {
        const n = Number(id);
        if (!sel || sel.thread.id !== n) this.openThread(n);
      } else if (sel) {
        this.selected.set(null);
      }
    });
    // Poll for new messages only.
    this.pollTimer = setInterval(() => {
      this.reload(/*silent=*/true);
      this.pollNewer();
    }, 8_000);
  }

  ngOnDestroy() {
    if (this.pollTimer) clearInterval(this.pollTimer);
    this.queryParamSub?.unsubscribe();
  }

  ngAfterViewChecked() {
    if (this.pendingScrollToBottom && this.messageList) {
      const el = this.messageList.nativeElement;
      el.scrollTop = el.scrollHeight;
      this.pendingScrollToBottom = false;
      this.pinnedToBottom = true;
      this.newSince.set(0);
    }
    if (this.prependAnchor && this.messageList) {
      const el = this.messageList.nativeElement;
      // Preserve the relative position so the viewport doesn't jump
      // when older messages prepend.
      el.scrollTop = el.scrollHeight - this.prependAnchor.prevScrollHeight + this.prependAnchor.prevScrollTop;
      this.prependAnchor = null;
    }
  }

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

  openThread(id: number) {
    this.detailLoading.set(true);
    this.selected.set(null);
    this.postError.set(null);
    this.newSince.set(0);
    // Update the URL without triggering our own queryParamMap subscription
    // into a re-open: the subscription does an equality check.
    this.router.navigate([], {
      queryParams: { thread: id },
      queryParamsHandling: 'merge',
      replaceUrl: false,
    });
    this.api.thread(id, { limit: PAGE_SIZE }).subscribe({
      next: (d) => {
        this.selected.set(d);
        this.detailLoading.set(false);
        this.pendingScrollToBottom = true;  // snap to newest
      },
      error: () => this.detailLoading.set(false),
    });
  }

  /** Polls for messages newer than the last we have loaded. */
  private pollNewer() {
    const sel = this.selected();
    if (!sel || sel.messages.length === 0) return;
    const lastId = sel.messages[sel.messages.length - 1].id;
    this.api.thread(sel.thread.id, { after_id: lastId, limit: 100 }).subscribe({
      next: (d) => {
        if (!d.messages.length) return;
        const merged: BusThreadDetail = {
          ...sel,
          thread: d.thread,
          messages: [...sel.messages, ...d.messages],
          has_more_older: sel.has_more_older,
          total: d.total ?? sel.total,
        };
        this.selected.set(merged);
        if (this.pinnedToBottom) {
          this.pendingScrollToBottom = true;
        } else {
          this.newSince.update(n => n + d.messages.length);
        }
      },
      error: () => { /* swallow — next tick retries */ },
    });
  }

  /** Lazy-load older messages when the user scrolls near the top. */
  onScroll() {
    const el = this.messageList?.nativeElement;
    if (!el) return;
    const distanceFromBottom = el.scrollHeight - (el.scrollTop + el.clientHeight);
    this.pinnedToBottom = distanceFromBottom < PIN_THRESHOLD_PX;
    if (this.pinnedToBottom) this.newSince.set(0);
    if (el.scrollTop < 120 && !this.loadingOlder()) {
      const sel = this.selected();
      if (!sel || !sel.has_more_older || sel.messages.length === 0) return;
      this.loadingOlder.set(true);
      const oldestId = sel.messages[0].id;
      this.prependAnchor = { prevScrollTop: el.scrollTop, prevScrollHeight: el.scrollHeight };
      this.api.thread(sel.thread.id, { before_id: oldestId, limit: PAGE_SIZE }).subscribe({
        next: (d) => {
          this.loadingOlder.set(false);
          if (d.messages.length === 0) return;
          const merged: BusThreadDetail = {
            ...sel,
            thread: d.thread,
            messages: [...d.messages, ...sel.messages],
            has_more_older: d.has_more_older,
            total: d.total ?? sel.total,
          };
          this.selected.set(merged);
        },
        error: () => { this.loadingOlder.set(false); this.prependAnchor = null; },
      });
    }
  }

  jumpToLatest() {
    this.pendingScrollToBottom = true;
    this.newSince.set(0);
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
        // Append only the new message(s) to preserve our paginated state.
        if (r.thread && r.thread.messages?.length) {
          const lastLocalId = sel.messages.length ? sel.messages[sel.messages.length - 1].id : 0;
          const newOnes = r.thread.messages.filter(m => m.id > lastLocalId);
          if (newOnes.length) {
            this.selected.set({ ...sel, thread: r.thread.thread, messages: [...sel.messages, ...newOnes] });
          }
        }
        this.pendingScrollToBottom = true;
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
