import { ChangeDetectionStrategy, Component, Input, computed, signal } from '@angular/core';
import { CommonModule } from '@angular/common';

@Component({
  selector: 'cc-sparkbar',
  standalone: true,
  imports: [CommonModule],
  changeDetection: ChangeDetectionStrategy.OnPush,
  styles: [`
    :host { display: block; }
    .sparkbar { display: flex; align-items: flex-end; gap: 2px; height: 32px; }
    .bar {
      flex: 1;
      background: linear-gradient(180deg, var(--glow) 0%, var(--chitin) 100%);
      border-radius: 1px;
      min-height: 1px;
      opacity: 0.9;
      transition: opacity 200ms ease;
    }
    .bar:hover { opacity: 1; }
    .bar.empty { background: rgba(212,165,116,0.10); }
  `],
  template: `
    <div class="sparkbar" role="img" [attr.aria-label]="ariaLabel">
      @for (b of bars(); track $index) {
        <div class="bar" [class.empty]="b === 0" [style.height.%]="b"></div>
      }
    </div>
  `,
})
export class SparkbarComponent {
  private readonly _data = signal<number[]>([]);
  @Input() set data(v: number[]) { this._data.set(v || []); }
  @Input() ariaLabel = 'sparkbar';

  readonly bars = computed(() => {
    const d = this._data();
    if (!d.length) return [];
    const max = Math.max(...d);
    if (max <= 0) return d.map(() => 0);
    return d.map(v => Math.max(2, (v / max) * 100));
  });
}
