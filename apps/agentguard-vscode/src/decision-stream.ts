import { createConnection } from 'node:net';
import { readFileSync } from 'node:fs';
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
          this.startTailer();
        });
      });

      if (connected) {
        return true;
      }
    }
    return false;
  }

  private startTailer(): void {
    if (this.stopTimer) {
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
      const prior = this.cursors.get(fullPath) ?? { offset: 0 };
      let content: string;
      try {
        content = readFileSync(fullPath, 'utf8');
      } catch {
        continue;
      }
      const unread = content.slice(prior.offset);
      if (!unread) {
        this.cursors.set(fullPath, { offset: content.length });
        continue;
      }

      for (const line of unread.split('\n')) {
        const trimmed = line.trim();
        if (!trimmed) {
          continue;
        }
        const decision = parseDecisionEventLine(trimmed);
        if (decision) {
          this.onDecision(decision);
        }
      }
      this.cursors.set(fullPath, { offset: content.length });
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
