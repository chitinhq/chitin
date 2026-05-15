import { ChangeDetectionStrategy, Component, OnInit, inject, signal, computed } from '@angular/core';
import { CommonModule } from '@angular/common';
import { RouterModule } from '@angular/router';
import { FormsModule } from '@angular/forms';
import { ApiService } from '../api.service';
import type { SessionSummary } from '../api.types';
import { LoaderComponent } from '../ui/loader.component';
import { EmptyStateComponent } from '../ui/empty-state.component';
import { fmtUsd, fmtTs, shortenId } from '../utils';

@Component({
  selector: 'cc-sessions-list',
  standalone: true,
  imports: [CommonModule, RouterModule, FormsModule, LoaderComponent, EmptyStateComponent],
  changeDetection: ChangeDetectionStrategy.OnPush,
  styles: [`
    :host { display: block; }
    .head { display: flex; justify-content: space-between; align-items: end; margin-bottom: 16px; gap: 12px; }
    .head h1 { margin: 0; font-size: 20px; font-weight: 600; }
    .filters { display: flex; gap: 8px; align-items: center; }
    .filters .input { width: 240px; }
    .panel-body { padding: 0; }
    .ellipsis { max-width: 380px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
    .stat-row {
      display: flex; align-items: center; gap: 6px;
      font-family: var(--font-mono); font-size: 11px;
    }
    .driver-chip {
      display: inline-flex; align-items: center;
      height: 18px; padding: 0 8px;
      background: rgba(212,165,116,0.08);
      border: 1px solid var(--line);
      border-radius: 999px;
      font-size: 10.5px;
      color: var(--chitin);
    }
    .num { text-align: right; }
  `],
  template: `
    <header class="head">
      <div>
        <h1>Chain Sessions</h1>
        <p class="text-muted mono" style="font-size:11.5px;margin:4px 0 0;">
          {{ filtered().length }} of {{ all().length }} sessions · last 14d
        </p>
      </div>
      <div class="filters">
        <input class="input" type="text" placeholder="filter by driver / chain id…"
               [(ngModel)]="search" (ngModelChange)="onSearch($event)"
               aria-label="Search sessions"/>
      </div>
    </header>

    @if (loading()) {
      <cc-loader message="Reading gov-decisions ledger"></cc-loader>
    } @else if (filtered().length === 0) {
      <div class="plate">
        <cc-empty-state title="No sessions" body="No chain sessions match the filter."></cc-empty-state>
      </div>
    } @else {
      <div class="plate panel">
        <div class="panel-body" style="overflow-x: auto;">
          <table class="tbl">
            <thead>
              <tr>
                <th>chain</th>
                <th>driver / agent</th>
                <th>tickets</th>
                <th class="num">events</th>
                <th class="num">allow / deny</th>
                <th class="num">cost</th>
                <th>first</th>
                <th>last</th>
              </tr>
            </thead>
            <tbody>
              @for (s of filtered(); track s.chain_id) {
                <tr class="row-hover" [routerLink]="['/sessions', s.chain_id]">
                  <td class="mono small">{{ shortenId(s.chain_id, 10, 4) }}</td>
                  <td>
                    <div class="stat-row">
                      <span class="driver-chip">{{ s.driver || 'unknown' }}</span>
                      @if (s.agent && s.agent !== s.driver) {
                        <span class="text-muted mono small">{{ s.agent }}</span>
                      }
                    </div>
                  </td>
                  <td class="mono small text-muted ellipsis">
                    {{ s.tickets.length ? s.tickets.join(', ') : '–' }}
                  </td>
                  <td class="num mono small">{{ s.events }}</td>
                  <td class="num mono small">
                    <span class="text-run">{{ s.allowed }}</span>
                    <span class="text-dim"> / </span>
                    <span class="text-danger">{{ s.denied }}</span>
                  </td>
                  <td class="num mono small text-glow">{{ fmtUsd(s.costUsd, 4) }}</td>
                  <td class="mono small text-muted">{{ fmtTs(s.firstTs) }}</td>
                  <td class="mono small text-muted">{{ fmtTs(s.lastTs) }}</td>
                </tr>
              }
            </tbody>
          </table>
        </div>
      </div>
    }
  `,
})
export class SessionsListPage implements OnInit {
  private readonly api = inject(ApiService);
  readonly loading = signal(true);
  readonly all = signal<SessionSummary[]>([]);
  search = '';
  private readonly searchSignal = signal('');

  readonly filtered = computed(() => {
    const q = this.searchSignal().toLowerCase().trim();
    if (!q) return this.all();
    return this.all().filter(s =>
      s.chain_id.toLowerCase().includes(q) ||
      (s.driver || '').toLowerCase().includes(q) ||
      (s.agent || '').toLowerCase().includes(q) ||
      s.tickets.some(t => t.toLowerCase().includes(q)),
    );
  });

  readonly fmtUsd = fmtUsd;
  readonly fmtTs = fmtTs;
  readonly shortenId = shortenId;

  ngOnInit() {
    this.api.sessions(200).subscribe({
      next: (r) => { this.all.set(r.sessions); this.loading.set(false); },
      error: () => this.loading.set(false),
    });
  }

  onSearch(v: string) { this.searchSignal.set(v); }
}
