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
    .head { margin-bottom: 16px; }
    .title { margin: 0; font-size: 20px; font-weight: 600; }
    .subtitle { margin: 4px 0 0; color: var(--muted); font-size: 11.5px; }
    .note {
      padding: 14px 16px;
      color: var(--muted);
      font-size: 12.5px;
      line-height: 1.6;
    }
    .badge {
      display: inline-flex;
      align-items: center;
      height: 20px;
      padding: 0 10px;
      border-radius: 999px;
      font-family: var(--font-mono);
      font-size: 10.5px;
      letter-spacing: 0.04em;
      text-transform: uppercase;
      background: rgba(245,158,11,0.10);
      color: var(--warn);
      border: 1px solid rgba(245,158,11,0.30);
      margin-left: 10px;
    }
    .rubric {
      display: grid;
      grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
      gap: 10px;
      margin-top: 12px;
    }
    .rubric .item {
      padding: 12px 14px;
      background: rgba(17,22,31,0.6);
      border: 1px solid var(--line-soft);
      border-radius: 4px;
    }
    .rubric h3 {
      margin: 0 0 4px;
      font-size: 11px;
      letter-spacing: 0.14em;
      text-transform: uppercase;
      color: var(--chitin);
    }
    .rubric p { margin: 0; font-size: 12px; color: var(--muted); }
  `],
  template: `
    <header class="head">
      <h1 class="title">
        Analyzer Suggestions
        @if (!data()?.enabled) {
          <span class="badge">slice 5 pending</span>
        }
      </h1>
      <p class="subtitle mono">LLM-driven prompt / skill / policy recommendations</p>
    </header>

    @if (loading()) {
      <cc-loader message="Querying analyzer_suggestions"></cc-loader>
    } @else {
      <div class="plate">
        <cc-empty-state
          title="Not yet wired"
          [body]="data()?.note || 'Slice 5 (analyzer-cron.lobster) has not shipped.'"></cc-empty-state>
        <div class="note">
          When ready, the analyzer rubric will surface:
          <div class="rubric">
            <div class="item">
              <h3>wasted denials</h3>
              <p>Same rule denies same agent 3+ times in N hours.</p>
            </div>
            <div class="item">
              <h3>cost outliers</h3>
              <p>Session cost &gt; 2× median for its task class.</p>
            </div>
            <div class="item">
              <h3>tool thrashing</h3>
              <p>5+ similar tool calls (e.g. file rewrites) in one session.</p>
            </div>
            <div class="item">
              <h3>routing failures</h3>
              <p>claude-code dispatched to a task that is clearly codex-shaped.</p>
            </div>
            <div class="item">
              <h3>stale rules</h3>
              <p>No fire in 30 days — propose a drop.</p>
            </div>
          </div>
        </div>
      </div>
    }
  `,
})
export class SuggestionsPage implements OnInit {
  private readonly api = inject(ApiService);
  readonly loading = signal(true);
  readonly data = signal<SuggestionsResponse | null>(null);

  ngOnInit() {
    this.api.suggestions().subscribe({
      next: (d) => { this.data.set(d); this.loading.set(false); },
      error: () => this.loading.set(false),
    });
  }
}
