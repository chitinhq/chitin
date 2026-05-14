import { ChangeDetectionStrategy, Component, OnInit, inject, signal } from '@angular/core';
import { CommonModule } from '@angular/common';
import { RouterModule } from '@angular/router';
import { forkJoin } from 'rxjs';
import { ApiService } from '../api.service';
import type {
  Stats, RecentRun, SessionSummary, EloRow, CostHistogram,
} from '../api.types';
import { KpiCardComponent } from '../ui/kpi-card.component';
import { SparkbarComponent } from '../ui/sparkbar.component';
import { StatusPillComponent } from '../ui/status-pill.component';
import { LoaderComponent } from '../ui/loader.component';
import { EmptyStateComponent } from '../ui/empty-state.component';
import { formatAge, fmtUsd, fmtPct, fmtTs, shortenId, ageFromEpochSeconds, ageFromEpochMs } from '../utils';

@Component({
  selector: 'cc-overview',
  standalone: true,
  imports: [
    CommonModule, RouterModule,
    KpiCardComponent, SparkbarComponent, StatusPillComponent, LoaderComponent, EmptyStateComponent,
  ],
  changeDetection: ChangeDetectionStrategy.OnPush,
  styleUrl: './overview.page.css',
  templateUrl: './overview.page.html',
})
export class OverviewPage implements OnInit {
  private readonly api = inject(ApiService);
  readonly loading = signal(true);
  readonly stats = signal<Stats | null>(null);
  readonly runs = signal<RecentRun[]>([]);
  readonly sessions = signal<SessionSummary[]>([]);
  readonly elo = signal<EloRow[]>([]);
  readonly cost = signal<CostHistogram | null>(null);

  readonly fmtAge = formatAge;
  readonly fmtUsd = fmtUsd;
  readonly fmtPct = fmtPct;
  readonly fmtTs = fmtTs;
  readonly shortenId = shortenId;
  readonly ageFromEpochSeconds = ageFromEpochSeconds;
  readonly ageFromEpochMs = ageFromEpochMs;

  ngOnInit() {
    forkJoin({
      stats: this.api.stats(),
      runs: this.api.recentRuns(12),
      sessions: this.api.sessions(10),
      elo: this.api.elo(),
      cost: this.api.costHistogram(),
    }).subscribe({
      next: ({ stats, runs, sessions, elo, cost }) => {
        this.stats.set(stats);
        this.runs.set(runs.runs);
        this.sessions.set(sessions.sessions);
        this.elo.set(elo.rows.slice(0, 8));
        this.cost.set(cost);
        this.loading.set(false);
      },
      error: () => this.loading.set(false),
    });
  }

  ageFromMsAgo(secondsAgo: number): string {
    return formatAge(secondsAgo);
  }
}
