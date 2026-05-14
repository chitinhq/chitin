import { ChangeDetectionStrategy, Component, Input } from '@angular/core';
import { CommonModule } from '@angular/common';

@Component({
  selector: 'cc-kpi-card',
  standalone: true,
  imports: [CommonModule],
  changeDetection: ChangeDetectionStrategy.OnPush,
  styles: [`
    :host { display: block; }
    .kpi {
      position: relative;
      padding: 14px 16px;
      min-height: 86px;
      display: flex;
      flex-direction: column;
      justify-content: space-between;
      gap: 8px;
    }
    .label {
      display: flex; align-items: center; gap: 8px;
      font-size: 10.5px;
      text-transform: uppercase;
      letter-spacing: 0.16em;
      color: var(--muted);
      font-weight: 600;
    }
    .label .icon {
      width: 8px; height: 8px;
      transform: rotate(45deg);
      background: var(--chitin);
      opacity: 0.6;
    }
    .value {
      font-family: var(--font-mono);
      font-size: 26px;
      font-weight: 700;
      color: var(--bone);
      letter-spacing: -0.02em;
      line-height: 1.1;
    }
    .value.run    { color: var(--run); }
    .value.warn   { color: var(--warn); }
    .value.danger { color: var(--danger); }
    .value.glow   { color: var(--glow); }
    .delta {
      font-family: var(--font-mono);
      font-size: 11px;
      color: var(--muted);
    }
    .delta.up { color: var(--run); }
    .delta.down { color: var(--danger); }
  `],
  template: `
    <div class="plate kpi">
      <div class="label">
        <span class="icon" aria-hidden="true"></span>
        <span>{{ label }}</span>
      </div>
      <div class="value" [ngClass]="tone || ''">{{ value }}</div>
      @if (delta) {
        <div class="delta" [ngClass]="deltaTone">{{ delta }}</div>
      }
    </div>
  `,
})
export class KpiCardComponent {
  @Input() label = '';
  @Input() value: string | number = '';
  @Input() tone: 'run' | 'warn' | 'danger' | 'glow' | '' = '';
  @Input() delta: string | null = null;
  @Input() deltaTone: 'up' | 'down' | '' = '';
}
