import { ChangeDetectionStrategy, Component, OnInit, inject, signal, computed } from '@angular/core';
import { CommonModule } from '@angular/common';
import { RouterModule, ActivatedRoute } from '@angular/router';
import { ApiService } from '../api.service';
import type { SessionDetail, ChainEvent } from '../api.types';
import { LoaderComponent } from '../ui/loader.component';
import { EmptyStateComponent } from '../ui/empty-state.component';
import { KpiCardComponent } from '../ui/kpi-card.component';
import { fmtUsd, fmtTs, shortenId } from '../utils';

interface Lane {
  key: string;
  label: string;
  rows: ChainEvent[];
}

@Component({
  selector: 'cc-session-detail',
  standalone: true,
  imports: [CommonModule, RouterModule, LoaderComponent, EmptyStateComponent, KpiCardComponent],
  changeDetection: ChangeDetectionStrategy.OnPush,
  styleUrl: './session-detail.page.css',
  templateUrl: './session-detail.page.html',
})
export class SessionDetailPage implements OnInit {
  private readonly api = inject(ApiService);
  private readonly route = inject(ActivatedRoute);

  readonly loading = signal(true);
  readonly detail = signal<SessionDetail | null>(null);
  readonly selected = signal<ChainEvent | null>(null);

  readonly fmtUsd = fmtUsd;
  readonly fmtTs = fmtTs;
  readonly shortenId = shortenId;

  // Group events into lanes for the timeline visualization
  readonly lanes = computed<Lane[]>(() => {
    const d = this.detail();
    if (!d) return [];
    const map = new Map<string, ChainEvent[]>();
    for (const e of d.events) {
      const k = e.action_type || e.tool_name || 'other';
      const arr = map.get(k) || [];
      arr.push(e);
      map.set(k, arr);
    }
    return Array.from(map.entries())
      .sort((a, b) => b[1].length - a[1].length)
      .map(([key, rows]) => ({ key, label: key, rows }));
  });

  readonly topTools = computed(() => {
    const d = this.detail();
    if (!d) return [];
    return Object.entries(d.toolCounts).slice(0, 8);
  });

  readonly topRules = computed(() => {
    const d = this.detail();
    if (!d) return [];
    return Object.entries(d.ruleCounts).slice(0, 8);
  });

  // Map event ts to x-position % within the session window
  positionFor(e: ChainEvent): number {
    const d = this.detail();
    if (!d || !d.firstTs || !d.lastTs || !e.ts) return 0;
    const t = Date.parse(e.ts);
    const f = Date.parse(d.firstTs);
    const l = Date.parse(d.lastTs);
    const span = Math.max(1, l - f);
    return Math.min(100, Math.max(0, ((t - f) / span) * 100));
  }

  classFor(e: ChainEvent): string {
    if (e.allowed === false || e.decision === 'deny') return 'cell deny';
    if (e.decision === 'heuristic-allow' || e.effect === 'heuristic') return 'cell heuristic';
    if (e.allowed === true || e.decision === 'allow') return 'cell allow';
    return 'cell other';
  }

  prettyJson(o: unknown): string {
    try { return JSON.stringify(o, null, 2); } catch { return String(o); }
  }

  ngOnInit() {
    const chainId = this.route.snapshot.paramMap.get('chainId') ?? '';
    if (!chainId) { this.loading.set(false); return; }
    this.api.session(chainId).subscribe({
      next: (r) => { this.detail.set(r); this.loading.set(false); },
      error: () => this.loading.set(false),
    });
  }

  selectEvent(e: ChainEvent) { this.selected.set(e); }
  closeDetail() { this.selected.set(null); }
}
