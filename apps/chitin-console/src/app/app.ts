import { Component, inject, signal, OnInit, OnDestroy, ChangeDetectionStrategy } from '@angular/core';
import { CommonModule } from '@angular/common';
import { RouterModule, Router, NavigationEnd } from '@angular/router';
import { FormsModule } from '@angular/forms';
import { Subscription } from 'rxjs';
import { filter, startWith } from 'rxjs/operators';
import { ApiService } from './api.service';
import { BoardService } from './board.service';

interface NavItem {
  path: string;
  label: string;
  icon: string;
  external?: boolean;
}

@Component({
  imports: [CommonModule, RouterModule, FormsModule],
  selector: 'app-root',
  templateUrl: './app.html',
  styleUrl: './app.css',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class App implements OnInit, OnDestroy {
  private readonly api = inject(ApiService);
  private readonly router = inject(Router);
  readonly boardSvc = inject(BoardService);
  private routerSub?: Subscription;
  private healthTimer: ReturnType<typeof setInterval> | null = null;

  readonly health = signal<{ ok: boolean; board?: string; lastTs?: number } | null>(null);
  readonly currentPath = signal<string>('queue');
  readonly moreOpen = signal<boolean>(false);

  // Primary surfaces — bottom tab bar on mobile, topbar on desktop.
  // Four items + a More button = five thumb-reachable tabs.
  readonly primaryNav: NavItem[] = [
    { path: '/queue',    label: 'Inbox',    icon: 'inbox' },
    { path: '/board',    label: 'Board',    icon: 'board' },
    { path: '/threads',  label: 'Chat',     icon: 'chat' },
    { path: '/reports',  label: 'Insights', icon: 'insights' },
  ];

  // Secondary — accessible via the "More" overflow.
  readonly secondaryNav: NavItem[] = [
    { path: '/overview',    label: 'Overview',    icon: 'overview' },
    { path: '/tickets',     label: 'All tickets', icon: 'tickets' },
    { path: '/sessions',    label: 'Sessions',    icon: 'sessions' },
    { path: '/elo',         label: 'Swarm ELO',   icon: 'elo' },
    { path: '/argus',       label: 'Argus',       icon: 'argus' },
    { path: '/policy',      label: 'Policy',      icon: 'policy' },
    { path: '/suggestions', label: 'Suggestions', icon: 'suggestions' },
    { path: '/sdlc',        label: 'Swarm × SDLC', icon: 'sdlc' },
  ];

  ngOnInit() {
    this.boardSvc.loadBoards();
    this.pollHealth();
    this.healthTimer = setInterval(() => this.pollHealth(), 15_000);
    this.routerSub = this.router.events
      .pipe(filter(e => e instanceof NavigationEnd), startWith(null))
      .subscribe(() => {
        const url = this.router.url.split('?')[0].replace(/^\//, '') || 'queue';
        this.currentPath.set(url.split('/')[0]);
        this.moreOpen.set(false);
      });
  }

  ngOnDestroy() {
    if (this.healthTimer) clearInterval(this.healthTimer);
    this.routerSub?.unsubscribe();
  }

  onBoardChange(slug: string) {
    this.boardSvc.setCurrent(slug);
    window.location.reload();
  }

  toggleMore() { this.moreOpen.update(v => !v); }
  closeMore()  { this.moreOpen.set(false); }

  private pollHealth() {
    this.api.health().subscribe({
      next: (r) => this.health.set({ ok: r.ok, board: r.board, lastTs: r.ts }),
      error: () => this.health.set({ ok: false }),
    });
  }
}
