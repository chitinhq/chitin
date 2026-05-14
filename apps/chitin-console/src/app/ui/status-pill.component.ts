import { ChangeDetectionStrategy, Component, Input } from '@angular/core';
import { CommonModule } from '@angular/common';

@Component({
  selector: 'cc-status-pill',
  standalone: true,
  imports: [CommonModule],
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <span class="pill" [ngClass]="'pill-status-' + (status || 'unknown')">
      <span class="dot" aria-hidden="true"></span>
      <span>{{ display() }}</span>
    </span>
  `,
})
export class StatusPillComponent {
  @Input() status: string | null | undefined = null;
  @Input() label: string | null | undefined = null;

  display() {
    return this.label ?? this.status ?? 'unknown';
  }
}
