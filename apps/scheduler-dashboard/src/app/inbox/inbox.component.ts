import { Component, inject, signal } from '@angular/core';
import { CommonModule } from '@angular/common';
import { FormsModule } from '@angular/forms';
import { RouterLink } from '@angular/router';
import { SchedulerService } from '../shared/services/scheduler.service';
import type { Item } from '../shared/types/item.types';

type RecorderState = 'idle' | 'recording' | 'uploading';

@Component({
  selector: 'app-inbox',
  standalone: true,
  imports: [CommonModule, FormsModule, RouterLink],
  styles: [`
    .container { max-width: 700px; margin: 0 auto; padding: 1.5rem; }
    h2 { margin: 0 0 1.25rem; font-size: 1.25rem; }
    .input-area {
      background: #fff;
      border-radius: 8px;
      padding: 1rem;
      box-shadow: 0 1px 3px rgba(0,0,0,0.07);
      margin-bottom: 1.5rem;
    }
    textarea {
      width: 100%;
      min-height: 120px;
      border: 1px solid #e0e0e0;
      border-radius: 6px;
      padding: 0.75rem;
      font: inherit;
      resize: vertical;
      margin-bottom: 0.75rem;
    }
    textarea:focus { outline: none; border-color: #6c63ff; }
    .actions { display: flex; gap: 0.75rem; align-items: center; }
    .btn {
      padding: 0.5rem 1.25rem;
      border: none;
      border-radius: 6px;
      font-weight: 500;
      transition: background 0.15s;
    }
    .btn-primary { background: #6c63ff; color: #fff; }
    .btn-primary:hover { background: #5a52e0; }
    .btn-primary:disabled { background: #c5c2f5; cursor: default; }
    .btn-mic {
      background: #fff;
      border: 1.5px solid #6c63ff;
      color: #6c63ff;
      display: flex;
      align-items: center;
      gap: 0.4rem;
    }
    .btn-mic.recording { border-color: #e53935; color: #e53935; }
    .btn-mic:hover { background: #f5f4ff; }
    .status { font-size: 0.85rem; color: #666; }
    .results { margin-top: 1rem; }
    .result-item {
      background: #fff;
      border-radius: 6px;
      padding: 0.75rem 1rem;
      margin-bottom: 0.5rem;
      border-left: 3px solid #6c63ff;
      box-shadow: 0 1px 3px rgba(0,0,0,0.05);
      display: flex;
      align-items: center;
      justify-content: space-between;
    }
    .result-title { font-weight: 500; }
    .result-meta { font-size: 0.8rem; color: #888; }
    .error { color: #c0392b; font-size: 0.9rem; }
  `],
  template: `
    <div class="container">
      <h2>Inbox</h2>
      <div class="input-area">
        <textarea
          [(ngModel)]="text"
          placeholder="Paste or type tasks here — one per line, or free-form prose…"
          [disabled]="submitting()"
        ></textarea>
        <div class="actions">
          <button
            class="btn btn-primary"
            (click)="submit()"
            [disabled]="submitting() || !text.trim()"
          >
            {{ submitting() ? 'Parsing…' : 'Parse & Add' }}
          </button>
          <button
            class="btn btn-mic"
            [class.recording]="recorderState() === 'recording'"
            (click)="toggleRecording()"
            [disabled]="recorderState() === 'uploading'"
          >
            {{ micLabel() }}
          </button>
          @if (statusMsg()) {
            <span class="status">{{ statusMsg() }}</span>
          }
        </div>
        @if (error()) {
          <p class="error">{{ error() }}</p>
        }
      </div>

      @if (results().length > 0) {
        <div class="results">
          <h3 style="font-size:0.85rem;text-transform:uppercase;letter-spacing:0.08em;color:#666;margin:0 0 0.75rem">
            Added ({{ results().length }})
          </h3>
          @for (item of results(); track item.id) {
            <div class="result-item">
              <div>
                <div class="result-title">{{ item.title }}</div>
                <div class="result-meta">{{ item.item_type }}{{ itemMeta(item) }}</div>
              </div>
              <a [routerLink]="['/edit', item.id]" style="font-size:0.8rem;color:#6c63ff">Edit →</a>
            </div>
          }
        </div>
      }
    </div>
  `,
})
export class InboxComponent {
  private svc = inject(SchedulerService);

  text = '';
  submitting = signal(false);
  recorderState = signal<RecorderState>('idle');
  statusMsg = signal('');
  error = signal('');
  results = signal<Item[]>([]);

  private recorder: MediaRecorder | null = null;
  private chunks: BlobPart[] = [];

  get micLabel(): () => string {
    return () => {
      switch (this.recorderState()) {
        case 'recording': return '⏹ Stop';
        case 'uploading': return 'Uploading…';
        default: return '🎤 Dictate';
      }
    };
  }

  itemMeta(item: Item): string {
    if (item.item_type === 'task' && item.deadline) return ` · due ${item.deadline}`;
    if (item.item_type === 'event') return ` · ${item.start}`;
    return '';
  }

  submit(): void {
    if (!this.text.trim() || this.submitting()) return;
    this.submitting.set(true);
    this.error.set('');
    this.svc.ingest(this.text).subscribe({
      next: (res) => {
        this.results.set([...res.items, ...this.results()]);
        this.text = '';
        this.submitting.set(false);
      },
      error: (err) => {
        this.error.set(err?.message ?? 'Ingest failed');
        this.submitting.set(false);
      },
    });
  }

  toggleRecording(): void {
    if (this.recorderState() === 'recording') {
      this.recorder?.stop();
      return;
    }
    this.chunks = [];
    this.error.set('');
    navigator.mediaDevices
      .getUserMedia({ audio: true })
      .then((stream) => {
        this.recorder = new MediaRecorder(stream);
        this.recorder.ondataavailable = (e) => this.chunks.push(e.data);
        this.recorder.onstop = () => {
          stream.getTracks().forEach((t) => t.stop());
          const blob = new Blob(this.chunks, { type: 'audio/webm' });
          this.upload(blob);
        };
        this.recorder.start();
        this.recorderState.set('recording');
        this.statusMsg.set('Recording…');
      })
      .catch(() => {
        this.error.set('Microphone access denied');
      });
  }

  private upload(blob: Blob): void {
    this.recorderState.set('uploading');
    this.statusMsg.set('Transcribing…');
    this.svc.transcribe(blob).subscribe({
      next: (res) => {
        this.text = this.text ? `${this.text}\n${res.text}` : res.text;
        this.recorderState.set('idle');
        this.statusMsg.set('Transcribed. Review and press "Parse & Add".');
      },
      error: (err) => {
        this.error.set(err?.message ?? 'Transcription failed');
        this.recorderState.set('idle');
        this.statusMsg.set('');
      },
    });
  }
}
