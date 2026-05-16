import { Injectable, inject, signal, computed } from '@angular/core';
import { HttpClient } from '@angular/common/http';

const API_BASE = (window as { __CHITIN_API__?: string }).__CHITIN_API__ ?? '/api';
const STORAGE_KEY = 'chitin-console:board';

export interface Board {
  slug: string;
  source: 'chitin' | 'hermes';
  path: string;
}

@Injectable({ providedIn: 'root' })
export class BoardService {
  private readonly http = inject(HttpClient);
  readonly boards = signal<Board[]>([]);
  readonly current = signal<string>(localStorage.getItem(STORAGE_KEY) || 'chitin');
  readonly ready = signal<boolean>(false);
  /** Current board as a `?board=` query suffix (`""` for default). */
  readonly suffix = computed(() => this.current() ? `&board=${encodeURIComponent(this.current())}` : '');

  loadBoards(): void {
    this.http.get<{ boards: Board[]; current: string }>(`${API_BASE}/boards`).subscribe({
      next: (r) => {
        this.boards.set(r.boards);
        // Use the persisted choice if it's a known board; otherwise fall back to the API's "current".
        const persisted = localStorage.getItem(STORAGE_KEY);
        const valid = r.boards.some(b => b.slug === persisted);
        this.current.set(valid && persisted ? persisted : r.current);
        this.ready.set(true);
      },
      error: () => this.ready.set(true),
    });
  }

  setCurrent(slug: string): void {
    if (this.current() === slug) return;
    this.current.set(slug);
    try { localStorage.setItem(STORAGE_KEY, slug); } catch { /* ignore quota */ }
  }
}
