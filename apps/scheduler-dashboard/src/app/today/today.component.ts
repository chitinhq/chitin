import { Component, OnInit, inject } from '@angular/core';
import { CommonModule } from '@angular/common';
import { RouterLink } from '@angular/router';
import { SchedulerService } from '../shared/services/scheduler.service';
import type { EventItem, TaskItem, TodayResult } from '../shared/types/item.types';

@Component({
  selector: 'app-today',
  standalone: true,
  imports: [CommonModule, RouterLink],
  styles: [`
    .container {
      max-width: 800px;
      margin: 0 auto;
      padding: 1.5rem;
    }
    h2 { margin: 0 0 1.25rem; font-size: 1.25rem; }
    .section { margin-bottom: 2rem; }
    .section h3 { font-size: 0.85rem; text-transform: uppercase; letter-spacing: 0.08em; color: #666; margin: 0 0 0.75rem; }
    .timeline { display: flex; flex-direction: column; gap: 0.5rem; }
    .slot {
      display: flex;
      align-items: flex-start;
      gap: 1rem;
      padding: 0.75rem 1rem;
      background: #fff;
      border-radius: 6px;
      border-left: 3px solid #6c63ff;
      box-shadow: 0 1px 3px rgba(0,0,0,0.06);
    }
    .slot.event { border-color: #e07b3c; }
    .slot-time { font-size: 0.8rem; color: #666; min-width: 5rem; padding-top: 0.1rem; }
    .slot-body { flex: 1; }
    .slot-title { font-weight: 500; }
    .slot-rationale { font-size: 0.8rem; color: #888; margin-top: 0.2rem; }
    .tag { display: inline-block; font-size: 0.7rem; padding: 0.1rem 0.4rem; background: #eee; border-radius: 3px; margin-right: 0.3rem; }
    .edit-link { font-size: 0.8rem; color: #6c63ff; margin-top: 0.25rem; display: inline-block; }
    .empty { color: #888; font-size: 0.9rem; }
    .loading { color: #888; }
    .error { color: #c0392b; }
  `],
  template: `
    @if (loading) {
      <div class="container"><p class="loading">Loading today's schedule…</p></div>
    } @else if (error) {
      <div class="container"><p class="error">{{ error }}</p></div>
    } @else if (data) {
      <div class="container">
        <h2>{{ today }}</h2>

        <div class="section">
          <h3>Events</h3>
          @if (data.events.length === 0) {
            <p class="empty">No events today.</p>
          } @else {
            <div class="timeline">
              @for (ev of data.events; track ev.id) {
                <div class="slot event">
                  <span class="slot-time">{{ formatTime(ev.start) }}</span>
                  <div class="slot-body">
                    <div class="slot-title">{{ ev.title }}</div>
                    @if (ev.duration_min) {
                      <div class="slot-rationale">{{ ev.duration_min }} min</div>
                    }
                  </div>
                </div>
              }
            </div>
          }
        </div>

        <div class="section">
          <h3>Slotted Tasks</h3>
          @if (data.slots.length === 0 && data.ranked_tasks.length === 0) {
            <p class="empty">No tasks scheduled.</p>
          } @else {
            <div class="timeline">
              @for (slot of data.slots; track slot.item_id) {
                <div class="slot">
                  <span class="slot-time">{{ formatTime(slot.start) }}</span>
                  <div class="slot-body">
                    <div class="slot-title">{{ titleFor(slot.item_id) }}</div>
                    <div class="slot-rationale">{{ slot.rationale }}</div>
                    <a class="edit-link" [routerLink]="['/edit', slot.item_id]">Edit →</a>
                  </div>
                </div>
              }
              @for (task of unslottedTasks; track task.id) {
                <div class="slot">
                  <span class="slot-time">—</span>
                  <div class="slot-body">
                    <div class="slot-title">{{ task.title }}</div>
                    @if (task.tags?.length) {
                      <div>
                        @for (tag of task.tags; track tag) {
                          <span class="tag">{{ tag }}</span>
                        }
                      </div>
                    }
                    <a class="edit-link" [routerLink]="['/edit', task.id]">Edit →</a>
                  </div>
                </div>
              }
            </div>
          }
        </div>
      </div>
    }
  `,
})
export class TodayComponent implements OnInit {
  private svc = inject(SchedulerService);

  loading = true;
  error: string | null = null;
  data: TodayResult | null = null;

  get today(): string {
    return new Date().toLocaleDateString('en-US', {
      weekday: 'long',
      month: 'long',
      day: 'numeric',
    });
  }

  get unslottedTasks(): TaskItem[] {
    if (!this.data) return [];
    const slottedIds = new Set(this.data.slots.map((s) => s.item_id));
    return this.data.ranked_tasks.filter((t) => !slottedIds.has(t.id));
  }

  titleFor(itemId: string): string {
    if (!this.data) return itemId;
    const task = this.data.ranked_tasks.find((t) => t.id === itemId);
    return task?.title ?? itemId;
  }

  formatTime(iso: string): string {
    return new Date(iso).toLocaleTimeString('en-US', {
      hour: 'numeric',
      minute: '2-digit',
      hour12: true,
    });
  }

  ngOnInit(): void {
    this.svc.getToday().subscribe({
      next: (data) => {
        this.data = data;
        this.loading = false;
      },
      error: (err) => {
        this.error = err?.message ?? 'Failed to load today's schedule';
        this.loading = false;
      },
    });
  }
}
