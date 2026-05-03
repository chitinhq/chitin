import { Component, OnInit, inject, signal } from '@angular/core';
import { CommonModule } from '@angular/common';
import { FormsModule } from '@angular/forms';
import { ActivatedRoute, Router } from '@angular/router';
import { SchedulerService } from '../shared/services/scheduler.service';
import type { Item, TaskItem, ItemStatus, WindowPref } from '../shared/types/item.types';

@Component({
  selector: 'app-edit',
  standalone: true,
  imports: [CommonModule, FormsModule],
  styles: [`
    .container { max-width: 640px; margin: 0 auto; padding: 1.5rem; }
    h2 { margin: 0 0 1.25rem; font-size: 1.25rem; }
    .card { background: #fff; border-radius: 8px; padding: 1.25rem; box-shadow: 0 1px 3px rgba(0,0,0,0.07); }
    .field { margin-bottom: 1rem; }
    label { display: block; font-size: 0.8rem; font-weight: 600; color: #555; margin-bottom: 0.3rem; text-transform: uppercase; letter-spacing: 0.05em; }
    input, select, textarea {
      width: 100%;
      padding: 0.5rem 0.75rem;
      border: 1px solid #e0e0e0;
      border-radius: 6px;
      font: inherit;
    }
    input:focus, select:focus, textarea:focus { outline: none; border-color: #6c63ff; }
    .actions { display: flex; gap: 0.75rem; margin-top: 1.25rem; flex-wrap: wrap; }
    .btn { padding: 0.5rem 1.25rem; border: none; border-radius: 6px; font-weight: 500; transition: background 0.15s; }
    .btn-primary { background: #6c63ff; color: #fff; }
    .btn-primary:hover:not(:disabled) { background: #5a52e0; }
    .btn-success { background: #27ae60; color: #fff; }
    .btn-success:hover:not(:disabled) { background: #219a52; }
    .btn-danger { background: #fff; border: 1.5px solid #e53935; color: #e53935; }
    .btn-danger:hover:not(:disabled) { background: #fde8e8; }
    .btn-secondary { background: #f0f0f0; color: #333; }
    .btn-secondary:hover:not(:disabled) { background: #e4e4e4; }
    .btn:disabled { opacity: 0.5; cursor: default; }
    .error { color: #c0392b; font-size: 0.9rem; margin-top: 0.5rem; }
    .loading { color: #888; }
    .status-badge {
      display: inline-block;
      padding: 0.15rem 0.5rem;
      border-radius: 4px;
      font-size: 0.8rem;
      background: #eee;
      margin-bottom: 1rem;
    }
  `],
  template: `
    @if (loading()) {
      <div class="container"><p class="loading">Loading…</p></div>
    } @else if (loadError()) {
      <div class="container"><p class="error">{{ loadError() }}</p></div>
    } @else if (item()) {
      <div class="container">
        <h2>Edit Item</h2>
        <div class="card">
          <div class="status-badge">{{ item()!.status }} · {{ item()!.item_type }}</div>

          <div class="field">
            <label>Title</label>
            <input type="text" [(ngModel)]="draft.title" />
          </div>

          @if (item()!.item_type === 'task') {
            <div class="field">
              <label>Priority</label>
              <select [(ngModel)]="draft.priority">
                <option [ngValue]="undefined">—</option>
                <option [ngValue]="1">1 — Critical</option>
                <option [ngValue]="2">2 — High</option>
                <option [ngValue]="3">3 — Normal</option>
                <option [ngValue]="4">4 — Low</option>
                <option [ngValue]="5">5 — Someday</option>
              </select>
            </div>
            <div class="field">
              <label>Deadline</label>
              <input type="date" [(ngModel)]="draft.deadline" />
            </div>
            <div class="field">
              <label>Window preference</label>
              <select [(ngModel)]="draft.window_pref">
                <option value="">—</option>
                <option value="morning">Morning</option>
                <option value="deep">Deep work</option>
                <option value="shallow">Shallow</option>
                <option value="evening">Evening</option>
                <option value="any">Any</option>
              </select>
            </div>
            <div class="field">
              <label>Estimated minutes</label>
              <input type="number" [(ngModel)]="draft.est_min" min="1" />
            </div>
          }

          <div class="field">
            <label>Status</label>
            <select [(ngModel)]="draft.status">
              <option value="open">Open</option>
              <option value="scheduled">Scheduled</option>
              <option value="in_progress">In progress</option>
              <option value="completed">Completed</option>
              <option value="cancelled">Cancelled</option>
            </select>
          </div>

          @if (saveError()) {
            <p class="error">{{ saveError() }}</p>
          }

          <div class="actions">
            <button class="btn btn-primary" (click)="save()" [disabled]="saving()">
              {{ saving() ? 'Saving…' : 'Save' }}
            </button>
            @if (item()!.status !== 'completed') {
              <button class="btn btn-success" (click)="complete()" [disabled]="saving()">
                Mark complete
              </button>
            }
            <button class="btn btn-secondary" (click)="back()">Back</button>
            <button class="btn btn-danger" (click)="confirmDelete()" [disabled]="saving()">
              Delete
            </button>
          </div>
        </div>
      </div>
    }
  `,
})
export class EditComponent implements OnInit {
  private svc = inject(SchedulerService);
  private route = inject(ActivatedRoute);
  private router = inject(Router);

  loading = signal(true);
  loadError = signal('');
  item = signal<Item | null>(null);
  saving = signal(false);
  saveError = signal('');

  draft: {
    title: string;
    status: ItemStatus;
    priority?: 1 | 2 | 3 | 4 | 5;
    deadline?: string;
    window_pref?: WindowPref | '';
    est_min?: number;
  } = { title: '', status: 'open' };

  ngOnInit(): void {
    const id = this.route.snapshot.paramMap.get('id')!;
    this.svc.getItem(id).subscribe({
      next: (item) => {
        this.item.set(item);
        this.draft.title = item.title;
        this.draft.status = item.status;
        if (item.item_type === 'task') {
          this.draft.priority = item.priority;
          this.draft.deadline = item.deadline;
          this.draft.window_pref = item.window_pref ?? '';
          this.draft.est_min = item.est_min;
        }
        this.loading.set(false);
      },
      error: (err) => {
        this.loadError.set(err?.message ?? 'Failed to load item');
        this.loading.set(false);
      },
    });
  }

  save(): void {
    const id = this.item()!.id;
    const changes: Partial<TaskItem> = {
      title: this.draft.title,
      status: this.draft.status,
    };
    if (this.item()!.item_type === 'task') {
      Object.assign(changes, {
        priority: this.draft.priority,
        deadline: this.draft.deadline || undefined,
        window_pref: (this.draft.window_pref || undefined) as WindowPref | undefined,
        est_min: this.draft.est_min,
      });
    }
    this.saving.set(true);
    this.saveError.set('');
    this.svc.updateItem(id, changes).subscribe({
      next: (updated) => {
        this.item.set(updated);
        this.saving.set(false);
      },
      error: (err) => {
        this.saveError.set(err?.message ?? 'Save failed');
        this.saving.set(false);
      },
    });
  }

  complete(): void {
    const id = this.item()!.id;
    this.saving.set(true);
    this.svc.completeItem(id).subscribe({
      next: () => this.router.navigate(['/today']),
      error: (err) => {
        this.saveError.set(err?.message ?? 'Complete failed');
        this.saving.set(false);
      },
    });
  }

  confirmDelete(): void {
    if (!confirm('Delete this item? This cannot be undone.')) return;
    const id = this.item()!.id;
    this.saving.set(true);
    this.svc.deleteItem(id).subscribe({
      next: () => this.router.navigate(['/today']),
      error: (err) => {
        this.saveError.set(err?.message ?? 'Delete failed');
        this.saving.set(false);
      },
    });
  }

  back(): void {
    this.router.navigate(['/today']);
  }
}
