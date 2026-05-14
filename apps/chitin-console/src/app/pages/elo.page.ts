import { ChangeDetectionStrategy, Component, OnInit, inject, signal, computed } from '@angular/core';
import { CommonModule } from '@angular/common';
import { FormsModule } from '@angular/forms';
import { ApiService } from '../api.service';
import type { EloRow } from '../api.types';
import { LoaderComponent } from '../ui/loader.component';
import { EmptyStateComponent } from '../ui/empty-state.component';
import { fmtTs, shortenId } from '../utils';

@Component({
  selector: 'cc-elo',
  standalone: true,
  imports: [CommonModule, FormsModule, LoaderComponent, EmptyStateComponent],
  changeDetection: ChangeDetectionStrategy.OnPush,
  styles: [`
    :host { display: block; }
    .head { display: flex; justify-content: space-between; align-items: end; margin-bottom: 16px; flex-wrap: wrap; gap: 10px; }
    .title { margin: 0; font-size: 20px; font-weight: 600; }
    .subtitle { margin: 4px 0 0; color: var(--muted); font-size: 11.5px; }
    .filters { display: flex; gap: 8px; }
    .filters .input { width: 160px; }
    .num { text-align: right; }
    .elo-bar {
      position: relative;
      width: 120px;
      height: 6px;
      background: rgba(212,165,116,0.10);
      border-radius: 3px;
      display: inline-block;
      vertical-align: middle;
    }
    .elo-fill {
      position: absolute;
      inset: 0 auto 0 0;
      background: linear-gradient(90deg, var(--chitin), var(--glow));
      border-radius: 3px;
      box-shadow: 0 0 6px rgba(245,192,136,0.35);
    }
    .rank {
      display: inline-flex;
      align-items: center;
      justify-content: center;
      width: 22px; height: 22px;
      font-family: var(--font-mono);
      font-size: 11px;
      background: rgba(212,165,116,0.10);
      border: 1px solid var(--line-strong);
      border-radius: 4px;
      color: var(--glow);
    }
    .rank.top { color: var(--bg); background: var(--glow); border-color: var(--glow); }
  `],
  template: `
    <header class="head">
      <div>
        <h1 class="title">Swarm ELO</h1>
        <p class="subtitle mono">
          {{ filtered().length }} of {{ rows().length }} rows · scope chitin
        </p>
      </div>
      <div class="filters">
        <select class="input" [(ngModel)]="classFilter" (ngModelChange)="set('classFilter', $event)">
          <option value="">all classes</option>
          @for (c of classes(); track c) {
            <option [value]="c">{{ c }}</option>
          }
        </select>
        <select class="input" [(ngModel)]="driverFilter" (ngModelChange)="set('driverFilter', $event)">
          <option value="">all drivers</option>
          @for (d of drivers(); track d) {
            <option [value]="d">{{ d }}</option>
          }
        </select>
      </div>
    </header>

    @if (loading()) {
      <cc-loader message="Reading swarm_elo"></cc-loader>
    } @else if (filtered().length === 0) {
      <div class="plate">
        <cc-empty-state title="No ELO data" body="swarm_elo table is empty or filtered out."></cc-empty-state>
      </div>
    } @else {
      <div class="plate panel">
        <div style="overflow-x: auto;">
          <table class="tbl">
            <thead>
              <tr>
                <th>rank</th>
                <th>driver / model</th>
                <th>class</th>
                <th>complexity</th>
                <th class="num">ELO</th>
                <th>distribution</th>
                <th class="num">dispatches</th>
                <th>last dispatch</th>
                <th>updated</th>
              </tr>
            </thead>
            <tbody>
              @for (e of filtered(); track e.id; let idx = $index) {
                <tr>
                  <td>
                    <span class="rank" [class.top]="idx < 3">{{ idx + 1 }}</span>
                  </td>
                  <td>
                    <span class="mono small">{{ e.driver }}</span>
                    <span class="text-dim mono small"> · </span>
                    <span class="mono small text-glow">{{ e.model }}</span>
                  </td>
                  <td class="mono small text-muted">{{ e.task_class || '–' }}</td>
                  <td class="mono small text-muted">{{ e.complexity_bucket || '–' }}</td>
                  <td class="num mono small text-chitin">{{ e.elo_score.toFixed(1) }}</td>
                  <td>
                    <div class="elo-bar" [attr.title]="'ELO ' + e.elo_score">
                      <div class="elo-fill" [style.width.%]="eloPct(e.elo_score)"></div>
                    </div>
                  </td>
                  <td class="num mono small">{{ e.dispatches_count }}</td>
                  <td class="mono small text-muted">{{ e.last_dispatch_id ? shortenId(e.last_dispatch_id, 8, 4) : '–' }}</td>
                  <td class="mono small text-dim">{{ fmtTs(e.last_updated) }}</td>
                </tr>
              }
            </tbody>
          </table>
        </div>
      </div>
    }
  `,
})
export class EloPage implements OnInit {
  private readonly api = inject(ApiService);
  readonly loading = signal(true);
  readonly rows = signal<EloRow[]>([]);
  classFilter = '';
  driverFilter = '';
  private readonly classSignal  = signal('');
  private readonly driverSignal = signal('');

  readonly fmtTs = fmtTs;
  readonly shortenId = shortenId;

  readonly minMax = computed(() => {
    const r = this.rows();
    if (!r.length) return { min: 1500, max: 1500 };
    return {
      min: Math.min(...r.map(x => x.elo_score)),
      max: Math.max(...r.map(x => x.elo_score)),
    };
  });

  readonly classes = computed(() => {
    const set = new Set<string>();
    for (const r of this.rows()) if (r.task_class) set.add(r.task_class);
    return Array.from(set).sort();
  });
  readonly drivers = computed(() => {
    const set = new Set<string>();
    for (const r of this.rows()) if (r.driver) set.add(r.driver);
    return Array.from(set).sort();
  });

  readonly filtered = computed(() => {
    const c = this.classSignal();
    const d = this.driverSignal();
    return this.rows().filter(r =>
      (!c || r.task_class === c) &&
      (!d || r.driver === d)
    );
  });

  ngOnInit() {
    this.api.elo().subscribe({
      next: (r) => { this.rows.set(r.rows); this.loading.set(false); },
      error: () => this.loading.set(false),
    });
  }

  eloPct(elo: number): number {
    const { min, max } = this.minMax();
    if (max === min) return 50;
    return Math.min(100, Math.max(0, ((elo - min) / (max - min)) * 100));
  }

  set(field: 'classFilter' | 'driverFilter', v: string) {
    if (field === 'classFilter') this.classSignal.set(v);
    else this.driverSignal.set(v);
  }
}
