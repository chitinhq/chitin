import { ChangeDetectionStrategy, Component, OnInit, inject, signal } from '@angular/core';
import { CommonModule } from '@angular/common';
import { ApiService } from '../api.service';
import type { Policy } from '../api.types';
import { LoaderComponent } from '../ui/loader.component';
import { EmptyStateComponent } from '../ui/empty-state.component';
import { fmtTs } from '../utils';

@Component({
  selector: 'cc-policy',
  standalone: true,
  imports: [CommonModule, LoaderComponent, EmptyStateComponent],
  changeDetection: ChangeDetectionStrategy.OnPush,
  styles: [`
    :host { display: block; }
    .head { display: flex; justify-content: space-between; align-items: end; margin-bottom: 16px; flex-wrap: wrap; gap: 10px; }
    .title { margin: 0; font-size: 20px; font-weight: 600; }
    .subtitle { margin: 4px 0 0; color: var(--muted); font-size: 11.5px; }
    .meta {
      display: flex; gap: 14px;
      font-family: var(--font-mono);
      font-size: 11px;
      color: var(--muted);
    }
    .meta .k { color: var(--dim); margin-right: 4px; }
    .yaml-wrap {
      position: relative;
      padding: 16px 18px;
      max-height: calc(100vh - 240px);
      overflow: auto;
    }
    .yaml {
      font-family: var(--font-mono);
      font-size: 12px;
      line-height: 1.6;
      white-space: pre;
      color: var(--bone);
    }
    .line { display: flex; }
    .ln {
      width: 36px;
      color: var(--dim);
      text-align: right;
      padding-right: 10px;
      flex-shrink: 0;
      user-select: none;
    }
    .lc { flex: 1; }
    .lc.key { color: var(--chitin); }
    .lc.section { color: var(--glow); }
    .lc.comment { color: var(--dim); font-style: italic; }
    .lc.val-num { color: #38BDF8; }
    .lc.val-str { color: var(--run); }
    .banner {
      padding: 10px 14px;
      background: rgba(245,192,136,0.06);
      border-bottom: 1px solid var(--line);
      color: var(--muted);
      font-size: 11.5px;
    }
    .banner strong { color: var(--glow); }
  `],
  template: `
    <header class="head">
      <div>
        <h1 class="title">Policy</h1>
        <p class="subtitle mono">read-only view of chitin.yaml · composer ships with slice 6</p>
      </div>
      @if (policy()?.path) {
        <div class="meta">
          <span><span class="k">path</span>{{ policy()?.path }}</span>
          @if (policy()?.size) {
            <span><span class="k">size</span>{{ policy()?.size }} B</span>
          }
          @if (policy()?.modified) {
            <span><span class="k">modified</span>{{ fmtTs(policy()?.modified) }}</span>
          }
        </div>
      }
    </header>

    @if (loading()) {
      <cc-loader message="Reading policy"></cc-loader>
    } @else if (policy()?.error) {
      <div class="plate">
        <cc-empty-state
          title="Cannot read policy"
          [body]="'Error: ' + (policy()?.error || 'unknown')"></cc-empty-state>
      </div>
    } @else if (!policy()?.content) {
      <div class="plate">
        <cc-empty-state title="Empty" body="Policy file is empty."></cc-empty-state>
      </div>
    } @else {
      <div class="plate">
        <div class="banner mono">
          <strong>Read-only.</strong> Edit + adopt flow planned for slice 6 of the dashboard epic
          (chain-replay preview · auto-PR · synthetic <code>policy.adopt</code> event).
        </div>
        <div class="yaml-wrap">
          <pre class="yaml">@for (l of lines(); track $index) {<div class="line"><span class="ln">{{ $index + 1 }}</span><span [class]="'lc ' + classify(l)">{{ l }}</span></div>}</pre>
        </div>
      </div>
    }
  `,
})
export class PolicyPage implements OnInit {
  private readonly api = inject(ApiService);
  readonly loading = signal(true);
  readonly policy = signal<Policy | null>(null);
  readonly fmtTs = fmtTs;

  lines(): string[] {
    return (this.policy()?.content || '').split('\n');
  }
  classify(l: string): string {
    const trimmed = l.trim();
    if (!trimmed) return '';
    if (trimmed.startsWith('#')) return 'comment';
    if (/^[a-zA-Z0-9_-]+:\s*$/.test(trimmed)) return 'section';
    if (/^- /.test(trimmed)) return 'key';
    if (/^[a-zA-Z0-9_-]+:/.test(trimmed)) return 'key';
    return '';
  }

  ngOnInit() {
    this.api.policy().subscribe({
      next: (p) => { this.policy.set(p); this.loading.set(false); },
      error: () => this.loading.set(false),
    });
  }
}
