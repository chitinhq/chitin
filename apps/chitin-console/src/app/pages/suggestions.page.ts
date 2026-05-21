import { ChangeDetectionStrategy, Component, OnInit, inject, signal } from '@angular/core';
import { CommonModule } from '@angular/common';
import { ApiService } from '../api.service';
import type { SuggestionsResponse } from '../api.types';
import { LoaderComponent } from '../ui/loader.component';
import { EmptyStateComponent } from '../ui/empty-state.component';

@Component({
  selector: 'cc-suggestions',
  standalone: true,
  imports: [CommonModule, LoaderComponent, EmptyStateComponent],
  changeDetection: ChangeDetectionStrategy.OnPush,
  styles: [`
    :host { display: block; }
    .head { display: flex; justify-content: space-between; gap: 16px; align-items: flex-start; margin-bottom: 16px; }
    .title { margin: 0; font-size: 20px; font-weight: 600; }
    .subtitle { margin: 4px 0 0; color: var(--muted); font-size: 11.5px; }
    .actions { display: flex; gap: 10px; align-items: center; }
    .btn {
      border: 1px solid var(--line);
      background: rgba(212,165,116,0.12);
      color: var(--bone);
      padding: 8px 12px;
      border-radius: 6px;
      cursor: pointer;
      font-size: 12px;
    }
    .btn[disabled] { opacity: 0.5; cursor: default; }
    .controls {
      display: grid;
      grid-template-columns: repeat(3, minmax(0, 1fr));
      gap: 10px;
      margin-bottom: 14px;
    }
    .controls label {
      display: flex;
      flex-direction: column;
      gap: 6px;
      font-size: 11px;
      color: var(--muted);
      text-transform: uppercase;
      letter-spacing: 0.08em;
    }
    .controls input, .controls select {
      background: rgba(17,22,31,0.7);
      color: var(--bone);
      border: 1px solid var(--line);
      border-radius: 6px;
      padding: 9px 10px;
      font-size: 13px;
    }
    .note { margin-bottom: 12px; color: var(--muted); font-size: 12.5px; }
    .status { margin-bottom: 12px; color: var(--chitin); font-size: 12px; }
    .list { display: grid; gap: 12px; }
    .card {
      border: 1px solid var(--line-soft);
      border-radius: 8px;
      background: rgba(17,22,31,0.55);
      padding: 14px;
    }
    .meta { display: flex; flex-wrap: wrap; gap: 8px; margin-bottom: 10px; align-items: center; }
    .pill {
      display: inline-flex;
      align-items: center;
      border: 1px solid var(--line);
      border-radius: 999px;
      padding: 2px 8px;
      font-size: 10.5px;
      text-transform: uppercase;
      letter-spacing: 0.08em;
      font-family: var(--font-mono);
    }
    .target { font-family: var(--font-mono); font-size: 11px; color: var(--bone); }
    .stamp { margin-left: auto; font-size: 11px; color: var(--muted); }
    pre {
      margin: 0;
      white-space: pre-wrap;
      word-break: break-word;
      color: var(--bone);
      font-size: 12px;
      line-height: 1.55;
      font-family: var(--font-mono);
    }
    .rationale { margin-top: 10px; color: var(--muted); font-size: 12.5px; line-height: 1.5; }
  `],
  template: `
    <header class="head">
      <div>
        <h1 class="title">Analyzer Suggestions</h1>
        <p class="subtitle mono">Recent prompt, skill, policy, routing, and drop proposals</p>
      </div>
      <div class="actions">
        <button class="btn" type="button" (click)="runAnalysis()" [disabled]="loading() || running()">
          {{ running() ? 'Analyzing…' : 'Analyze last 24h' }}
        </button>
      </div>
    </header>

    @if (loading()) {
      <cc-loader message="Querying analyzer_suggestions"></cc-loader>
    } @else {
      <div class="plate">
        <div class="controls">
          <label>
            Type
            <select [value]="filterType()" (change)="onTypeChange($event)">
              <option value="">all</option>
              <option value="prompt_edit">prompt_edit</option>
              <option value="new_skill">new_skill</option>
              <option value="policy_rule">policy_rule</option>
              <option value="route_tweak">route_tweak</option>
              <option value="drop">drop</option>
            </select>
          </label>
          <label>
            Target
            <input [value]="filterTarget()" (input)="onTargetInput($event)" placeholder="claude-code, _pick_driver.py, rule id" />
          </label>
          <label>
            Sort
            <select [value]="sort()" (change)="onSortChange($event)">
              <option value="created_at_desc">newest first</option>
              <option value="created_at_asc">oldest first</option>
            </select>
          </label>
        </div>

        @if (status()) {
          <div class="status">{{ status() }}</div>
        }
        @if (data()?.note) {
          <div class="note">{{ data()?.note }}</div>
        }

        @if (!data()?.suggestions?.length) {
          <cc-empty-state
            title="No suggestions"
            [body]="data()?.enabled ? 'Run the analyzer or widen the filters.' : 'Analyzer storage is not available.'"></cc-empty-state>
        } @else {
          <div class="list">
            @for (item of data()?.suggestions ?? []; track item.id) {
              <article class="card">
                <div class="meta">
                  <span class="pill">{{ item.type }}</span>
                  <span class="target">{{ item.target }}</span>
                  <span class="pill">{{ item.applied ? 'applied' : 'pending' }}</span>
                  <span class="stamp">{{ item.created_at }}</span>
                </div>
                <pre>{{ item.diff }}</pre>
                <div class="rationale">{{ item.rationale }}</div>
              </article>
            }
          </div>
        }
      </div>
    }
  `,
})
export class SuggestionsPage implements OnInit {
  private readonly api = inject(ApiService);
  readonly loading = signal(true);
  readonly running = signal(false);
  readonly status = signal('');
  readonly data = signal<SuggestionsResponse | null>(null);
  readonly filterType = signal('');
  readonly filterTarget = signal('');
  readonly sort = signal('created_at_desc');

  ngOnInit() {
    this.reload();
  }

  reload() {
    this.loading.set(true);
    this.api.suggestions({
      type: this.filterType() || undefined,
      target: this.filterTarget() || undefined,
      sort: this.sort(),
    }).subscribe({
      next: (d) => {
        this.data.set(d);
        this.loading.set(false);
      },
      error: () => {
        this.status.set('Failed to load suggestions.');
        this.loading.set(false);
      },
    });
  }

  runAnalysis() {
    this.running.set(true);
    this.status.set('');
    this.api.analyze({ window: '24h' }).subscribe({
      next: (result) => {
        const written = Number(result.summary?.['suggestions_written'] || 0);
        this.status.set(`Analyzer completed. ${written} suggestion${written === 1 ? '' : 's'} written.`);
        this.running.set(false);
        this.reload();
      },
      error: () => {
        this.status.set('Analyzer run failed.');
        this.running.set(false);
      },
    });
  }

  onTypeChange(event: Event) {
    this.filterType.set((event.target as HTMLSelectElement).value);
    this.reload();
  }

  onTargetInput(event: Event) {
    this.filterTarget.set((event.target as HTMLInputElement).value);
    this.reload();
  }

  onSortChange(event: Event) {
    this.sort.set((event.target as HTMLSelectElement).value);
    this.reload();
  }
}
