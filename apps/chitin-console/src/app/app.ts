import { Component, inject, signal, OnInit, OnDestroy, ChangeDetectionStrategy } from '@angular/core';
import { CommonModule } from '@angular/common';
import { RouterModule, Router, NavigationEnd } from '@angular/router';
import { Subscription } from 'rxjs';
import { filter, startWith } from 'rxjs/operators';
import { ApiService } from './api.service';

interface NavItem { path: string; label: string; icon: string; }

@Component({
  imports: [CommonModule, RouterModule],
  selector: 'app-root',
  templateUrl: './app.html',
  styleUrl: './app.css',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class App implements OnInit, OnDestroy {
  private readonly api = inject(ApiService);
  private readonly router = inject(Router);
  private routerSub?: Subscription;
  private healthTimer: ReturnType<typeof setInterval> | null = null;

  readonly health = signal<{ ok: boolean; board?: string; lastTs?: number } | null>(null);
  readonly currentPath = signal<string>('overview');

  readonly nav: NavItem[] = [
    { path: '/overview',    label: 'Overview',    icon: 'overview' },
    { path: '/sessions',    label: 'Sessions',    icon: 'sessions' },
    { path: '/threads',     label: 'Threads',     icon: 'threads' },
    { path: '/tickets',     label: 'Tickets',     icon: 'tickets' },
    { path: '/elo',         label: 'Swarm ELO',   icon: 'elo' },
    { path: '/argus',       label: 'Argus',       icon: 'argus' },
    { path: '/policy',      label: 'Policy',      icon: 'policy' },
    { path: '/suggestions', label: 'Suggestions', icon: 'suggestions' },
    { path: '/sdlc',        label: 'Swarm × SDLC', icon: 'sdlc' },
  ];

  ngOnInit() {
    this.pollHealth();
    this.healthTimer = setInterval(() => this.pollHealth(), 15_000);
    this.routerSub = this.router.events
      .pipe(filter(e => e instanceof NavigationEnd), startWith(null))
      .subscribe(() => {
        const url = this.router.url.split('?')[0].replace(/^\//, '') || 'overview';
        this.currentPath.set(url.split('/')[0]);
      });
  }

  ngOnDestroy() {
    if (this.healthTimer) clearInterval(this.healthTimer);
    this.routerSub?.unsubscribe();
  }

  private pollHealth() {
    this.api.health().subscribe({
      next: (r) => this.health.set({ ok: r.ok, board: r.board, lastTs: r.ts }),
      error: () => this.health.set({ ok: false }),
    });
  }
}
