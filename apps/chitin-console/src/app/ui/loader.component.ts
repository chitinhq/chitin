import { ChangeDetectionStrategy, Component, Input } from '@angular/core';

@Component({
  selector: 'cc-loader',
  standalone: true,
  changeDetection: ChangeDetectionStrategy.OnPush,
  styles: [`
    :host { display: block; padding: 24px; text-align: center; }
    .skeleton {
      height: 14px;
      background: linear-gradient(90deg, rgba(212,165,116,0.06), rgba(245,192,136,0.14), rgba(212,165,116,0.06));
      background-size: 200% 100%;
      animation: shimmer 1.4s ease-in-out infinite;
      border-radius: 3px;
      margin: 0 auto 10px;
      max-width: 240px;
    }
    .label {
      font-family: var(--font-mono);
      font-size: 11px;
      color: var(--muted);
      letter-spacing: 0.12em;
      text-transform: uppercase;
    }
    @keyframes shimmer {
      0% { background-position: 200% 0; }
      100% { background-position: -200% 0; }
    }
  `],
  template: `
    <div class="skeleton"></div>
    <div class="skeleton" style="width:60%"></div>
    <div class="label">{{ message }}</div>
  `,
})
export class LoaderComponent {
  @Input() message = 'Loading…';
}
