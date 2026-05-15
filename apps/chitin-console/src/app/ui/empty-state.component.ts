import { ChangeDetectionStrategy, Component, Input } from '@angular/core';

@Component({
  selector: 'cc-empty-state',
  standalone: true,
  changeDetection: ChangeDetectionStrategy.OnPush,
  styles: [`
    :host { display: block; }
    .empty {
      padding: 40px 24px;
      text-align: center;
      color: var(--muted);
    }
    .title {
      font-family: var(--font-mono);
      font-size: 12px;
      letter-spacing: 0.16em;
      text-transform: uppercase;
      color: var(--chitin);
      margin-bottom: 8px;
    }
    .body { color: var(--muted); font-size: 13px; max-width: 480px; margin: 0 auto; line-height: 1.6; }
    .glyph {
      display: inline-flex;
      width: 36px; height: 36px;
      align-items: center; justify-content: center;
      border: 1px solid var(--line);
      border-radius: 50%;
      margin-bottom: 12px;
      color: var(--chitin);
    }
  `],
  template: `
    <div class="empty">
      <div class="glyph" aria-hidden="true">
        <svg viewBox="0 0 16 16" width="16" height="16">
          <polygon points="4,2 12,2 15,8 12,14 4,14 1,8" fill="none" stroke="currentColor" stroke-width="1.4"/>
        </svg>
      </div>
      <div class="title">{{ title }}</div>
      <div class="body">{{ body }}</div>
    </div>
  `,
})
export class EmptyStateComponent {
  @Input() title = 'Nothing here';
  @Input() body = '';
}
