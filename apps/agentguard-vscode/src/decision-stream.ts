import { createConnection } from 'node:net';
import { closeSync, fstatSync, openSync, readSync } from 'node:fs';
import { join } from 'node:path';
import { listEventChainFiles } from './chitin-locator';
import { parseDecisionEventLine, type DecisionRecord } from './decision-types';

export interface DecisionStreamOptions {
  readonly chitinDir: string;
  readonly socketPaths?: readonly string[];
  readonly pollMs?: number;
  readonly onDecision: (decision: DecisionRecord) => void;
  readonly onModeChange?: (mode: 'socket' | 'tail') => void;
}

interface TailCursor {
  offset: number;
  // Bytes read past the last newline — a JSONL record still mid-write.
  // Held until the rest of the line arrives so it is never dropped.
  pending: string;
}

export class DecisionStream {
  private readonly chitinDir: string;
  private readonly onDecision: (decision: DecisionRecord) => void;
  private readonly onModeChange?: (mode: 'socket' | 'tail') => void;
  private readonly socketPaths: readonly string[];
  private readonly pollMs: number;
  private readonly cursors = new Map<string, TailCursor>();
  private stopTimer: NodeJS.Timeout | null = null;
  private socket: ReturnType<typeof createConnection> | null = null;
  private currentMode: 'socket' | 'tail' | null = null;
  private stopped = false;

  constructor(options: DecisionStreamOptions) {
    this.chitinDir = options.chitinDir;
    this.socketPaths = options.socketPaths ?? [];
    this.pollMs = options.pollMs ?? 250;
    this.onDecision = options.onDecision;
    this.onModeChange = options.onModeChange;
  }

  async start(): Promise<void> {
    if (this.socketPaths.length > 0 && await this.trySocket()) {
      return;
    }
    this.startTailer();
  }

  stop(): void {
    // Mark stopped first: destroying the socket below fires its 'close'
    // handler, which would otherwise restart the tailer.
    this.stopped = true;
    if (this.stopTimer) {
      clearInterval(this.stopTimer);
      this.stopTimer = null;
    }
    if (this.socket) {
      this.socket.destroy();
      this.socket = null;
    }
  }

  private async trySocket(): Promise<boolean> {
    for (const socketPath of this.socketPaths) {
      const connected = await new Promise<boolean>((resolve) => {
        const socket = createConnection(socketPath);
        let settled = false;
        let buffer = '';
        const fallback = () => {
          if (settled) {
            return;
          }
          settled = true;
          socket.destroy();
          resolve(false);
        };

        socket.once('connect', () => {
          if (settled) {
            return;
          }
          settled = true;
          this.socket = socket;
          this.setMode('socket');
          resolve(true);
        });

        socket.on('data', (chunk) => {
          buffer += chunk.toString('utf8');
          let newlineIndex = buffer.indexOf('\n');
          while (newlineIndex >= 0) {
            const line = buffer.slice(0, newlineIndex).trim();
            buffer = buffer.slice(newlineIndex + 1);
            const decision = line ? parseDecisionEventLine(line) : null;
            if (decision) {
              this.onDecision(decision);
            }
            newlineIndex = buffer.indexOf('\n');
          }
        });

        socket.once('error', fallback);
        socket.once('close', () => {
          if (!settled) {
            fallback();
            return;
          }
          this.socket = null;
          // Only fall back to tailing if the stream is still live — a
          // close triggered by stop()/dispose must not restart ingestion.
          if (!this.stopped) {
            this.startTailer();
          }
        });
      });

      if (connected) {
        return true;
      }
    }
    return false;
  }

  private startTailer(): void {
    if (this.stopped || this.stopTimer) {
      return;
    }
    this.setMode('tail');
    this.pollTailFiles();
    this.stopTimer = setInterval(() => {
      this.pollTailFiles();
    }, this.pollMs);
  }

  private pollTailFiles(): void {
    for (const file of listEventChainFiles(this.chitinDir)) {
      const fullPath = join(this.chitinDir, file);
      let fd: number;
      try {
        fd = openSync(fullPath, 'r');
      } catch {
        continue;
      }
      try {
        const size = fstatSync(fd).size;
        const prior = this.cursors.get(fullPath) ?? { offset: 0, pending: '' };
        // A shrunk file was rotated/truncated — restart from the top.
        let offset = prior.offset > size ? 0 : prior.offset;
        let pending = prior.offset > size ? '' : prior.pending;
        if (size === offset) {
          this.cursors.set(fullPath, { offset, pending });
          continue;
        }
        // Read only the bytes past the cursor, not the whole file.
        const buf = Buffer.alloc(size - offset);
        const bytesRead = readSync(fd, buf, 0, buf.length, offset);
        const chunk = pending + buf.subarray(0, bytesRead).toString('utf8');
        const lastNewline = chunk.lastIndexOf('\n');
        if (lastNewline < 0) {
          // No complete line yet — keep buffering the partial record.
          this.cursors.set(fullPath, { offset: offset + bytesRead, pending: chunk });
          continue;
        }
        const complete = chunk.slice(0, lastNewline);
        pending = chunk.slice(lastNewline + 1);
        for (const line of complete.split('\n')) {
          const trimmed = line.trim();
          if (!trimmed) {
            continue;
          }
          const decision = parseDecisionEventLine(trimmed);
          if (decision) {
            this.onDecision(decision);
          }
        }
        this.cursors.set(fullPath, { offset: offset + bytesRead, pending });
      } finally {
        closeSync(fd);
      }
    }
  }

  private setMode(mode: 'socket' | 'tail'): void {
    if (this.currentMode === mode) {
      return;
    }
    this.currentMode = mode;
    this.onModeChange?.(mode);
  }
}
