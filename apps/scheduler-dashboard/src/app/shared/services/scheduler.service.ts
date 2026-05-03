import { Injectable, inject } from '@angular/core';
import { HttpClient } from '@angular/common/http';
import { Observable } from 'rxjs';
import type {
  Item,
  TodayResult,
  IngestResult,
  TranscribeResult,
} from '../types/item.types';

@Injectable({ providedIn: 'root' })
export class SchedulerService {
  private http = inject(HttpClient);

  getToday(): Observable<TodayResult> {
    return this.http.get<TodayResult>('/api/today');
  }

  getItems(status?: string): Observable<Item[]> {
    const params = status ? { status } : {};
    return this.http.get<Item[]>('/api/items', { params });
  }

  getItem(id: string): Observable<Item> {
    return this.http.get<Item>(`/api/items/${id}`);
  }

  updateItem(id: string, changes: Partial<Item>): Observable<Item> {
    return this.http.put<Item>(`/api/items/${id}`, changes);
  }

  deleteItem(id: string): Observable<void> {
    return this.http.delete<void>(`/api/items/${id}`);
  }

  completeItem(id: string): Observable<{ ok: boolean }> {
    return this.http.post<{ ok: boolean }>(`/api/items/${id}/complete`, {});
  }

  ingest(text: string): Observable<IngestResult> {
    return this.http.post<IngestResult>('/api/items/ingest', { text });
  }

  transcribe(audio: Blob): Observable<TranscribeResult> {
    const form = new FormData();
    form.append('audio', audio, 'recording.webm');
    return this.http.post<TranscribeResult>('/api/voice/transcribe', form);
  }
}
