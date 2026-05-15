import { ChangeDetectionStrategy, Component, OnInit, inject, signal } from '@angular/core';
import { CommonModule } from '@angular/common';
import { forkJoin } from 'rxjs';
import { ApiService } from '../api.service';
import type { ArgusInfo, ArgusFindingsResponse } from '../api.types';
import { LoaderComponent } from '../ui/loader.component';
import { EmptyStateComponent } from '../ui/empty-state.component';

@Component({
  selector: 'cc-argus',
  standalone: true,
  imports: [CommonModule, LoaderComponent, EmptyStateComponent],
  changeDetection: ChangeDetectionStrategy.OnPush,
  styles: [`
    :host { display: block; }
    .head { margin-bottom: 16px; }
    .title { margin: 0; font-size: 20px; font-weight: 600; }
    .subtitle { margin: 4px 0 0; color: var(--muted); font-size: 11.5px; }
    .grid-2 { display: grid; grid-template-columns: 1fr 2fr; gap: 14px; }
    @media (max-width: 1000px) { .grid-2 { grid-template-columns: 1fr; } }
    .panel-head { padding: 12px 14px; border-bottom: 1px solid var(--line-soft); }
    .info-row {
      display: flex; justify-content: space-between;
      padding: 8px 14px;
      border-bottom: 1px solid var(--line-soft);
      font-family: var(--font-mono);
      font-size: 11.5px;
    }
    .info-row:last-child { border-bottom: none; }
    .info-row .k { color: var(--muted); }
    .info-row .v { color: var(--bone); }
    .pre {
      background: rgba(10,14,21,0.6);
      border: 1px solid var(--line-soft);
      border-radius: 4px;
      padding: 12px;
      font-size: 11.5px;
      color: var(--bone);
      margin: 12px 14px;
      max-height: 460px;
      overflow: auto;
      white-space: pre-wrap;
      word-break: break-word;
    }
    .badge {
      display: inline-flex;
      align-items: center;
      height: 18px;
      padding: 0 8px;
      border-radius: 999px;
      font-family: var(--font-mono);
      font-size: 10px;
      letter-spacing: 0.04em;
      text-transform: uppercase;
      background: rgba(34,197,94,0.10);
      color: var(--run);
      border: 1px solid rgba(34,197,94,0.30);
    }
    .badge.off { background: rgba(100,116,139,0.10); color: var(--muted); border-color: var(--line); }
  `],
  template: `
    <header class="head">
      <h1 class="title">Argus Observatory</h1>
      <p class="subtitle mono">read-only view of ~/.argus/index.db</p>
    </header>

    @if (loading()) {
      <cc-loader message="Reading argus index"></cc-loader>
    } @else {
      <section class="grid-2">
        <div class="plate panel">
          <header class="panel-head">
            <h2 class="section-title">
              index status
              @if (info()?.available) {
                <span class="badge">online</span>
              } @else {
                <span class="badge off">offline</span>
              }
            </h2>
          </header>
          <div>
            <div class="info-row">
              <span class="k">tables</span>
              <span class="v">{{ info()?.tables?.length ?? 0 }}</span>
            </div>
            @if (info()?.counts) {
              @for (k of (info()?.tables ?? []); track k) {
                <div class="info-row">
                  <span class="k">{{ k }}</span>
                  <span class="v">{{ info()?.counts?.[k] ?? 0 }} rows</span>
                </div>
              }
            }
          </div>
        </div>

        <div class="plate panel">
          <header class="panel-head"><h2 class="section-title">findings (latest)</h2></header>
          @if (findings()?.findings?.length === 0) {
            <cc-empty-state
              title="No findings table"
              body="The argus index lacks a 'findings' table — cross-source detectors (Slice 2) are not yet wired up."></cc-empty-state>
          } @else {
            <pre class="pre mono">{{ findingsJson() }}</pre>
          }
        </div>
      </section>
    }
  `,
})
export class ArgusPage implements OnInit {
  private readonly api = inject(ApiService);
  readonly loading = signal(true);
  readonly info = signal<ArgusInfo | null>(null);
  readonly findings = signal<ArgusFindingsResponse | null>(null);

  ngOnInit() {
    forkJoin({
      info: this.api.argusInfo(),
      findings: this.api.argusFindings(50),
    }).subscribe({
      next: ({ info, findings }) => {
        this.info.set(info);
        this.findings.set(findings);
        this.loading.set(false);
      },
      error: () => this.loading.set(false),
    });
  }

  findingsJson(): string {
    const f = this.findings();
    if (!f) return '';
    return JSON.stringify(f.findings || [], null, 2);
  }
}
