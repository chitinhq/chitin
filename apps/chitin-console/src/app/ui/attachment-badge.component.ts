import { ChangeDetectionStrategy, Component, Input, OnChanges, SimpleChanges, inject, signal } from '@angular/core';
import { CommonModule } from '@angular/common';
import { ApiService } from '../api.service';
import type { AttachmentEnrichment, ThreadAttachment } from '../api.types';
import { StatusPillComponent } from './status-pill.component';

@Component({
  selector: 'cc-attachment-badge',
  standalone: true,
  imports: [CommonModule, StatusPillComponent],
  changeDetection: ChangeDetectionStrategy.OnPush,
  styleUrl: './attachment-badge.component.css',
  templateUrl: './attachment-badge.component.html',
})
export class AttachmentBadgeComponent implements OnChanges {
  private readonly api = inject(ApiService);

  @Input({ required: true }) threadId!: number;
  @Input({ required: true }) attachment!: ThreadAttachment;

  readonly loading = signal(true);
  readonly expanded = signal(false);
  readonly enrichment = signal<AttachmentEnrichment | null>(null);

  ngOnChanges(changes: SimpleChanges) {
    if (changes['threadId'] || changes['attachment']) this.reload();
  }

  reload() {
    if (!this.threadId || !this.attachment?.id) return;
    this.loading.set(true);
    this.expanded.set(false);
    this.enrichment.set(null);
    this.api.attachmentEnrichment(this.threadId, this.attachment.id).subscribe({
      next: (r) => {
        this.enrichment.set(r);
        this.loading.set(false);
      },
      error: () => {
        this.enrichment.set({
          kind: this.attachment.kind,
          ref: this.attachment.ref,
          status: 'error',
          label: this.attachment.kind,
          title: this.attachment.display || this.attachment.ref,
          subtitle: '(missing)',
        });
        this.loading.set(false);
      },
    });
  }

  toggleExpanded() {
    this.expanded.update((value) => !value);
  }
}
