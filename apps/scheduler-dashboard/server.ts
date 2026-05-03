/**
 * Scheduler Dashboard — Express API server
 *
 * Wraps @chitin/scheduler library calls and serves the Angular SPA.
 * Binds to localhost:3737 (single-user, no auth needed).
 *
 * @chitin/scheduler is imported lazily so the server boots with a
 * stub implementation when the library isn't built yet; replace stubs
 * with real calls once scheduler-lib-foundation + scheduler-rank-ingest-notify
 * entries are merged.
 */

import express, { Request, Response, NextFunction } from 'express';
import { createServer } from 'node:http';
import { existsSync } from 'node:fs';
import { join, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';
import { execFile } from 'node:child_process';
import { promisify } from 'node:util';
import multer from 'multer';
import cors from 'cors';

const execFileAsync = promisify(execFile);
const __dirname = dirname(fileURLToPath(import.meta.url));
const distDir = join(__dirname, 'dist');
const PORT = 3737;

// ---------------------------------------------------------------------------
// Stub implementations — replace with real @chitin/scheduler imports once
// the library is available (scheduler-lib-foundation entry).
// ---------------------------------------------------------------------------

type ItemType = 'task' | 'event' | 'backlog';
type ItemStatus = 'open' | 'scheduled' | 'in_progress' | 'completed' | 'cancelled';

interface ItemBase {
  id: string;
  title: string;
  status: ItemStatus;
  created_at: string;
  item_type: ItemType;
  source_url?: string;
  tags?: string[];
}

interface StubStore {
  getAll(status?: string): ItemBase[];
  get(id: string): ItemBase | undefined;
  insert(item: ItemBase): void;
  update(id: string, changes: Partial<ItemBase>): ItemBase | undefined;
  remove(id: string): boolean;
}

function createInMemoryStore(): StubStore {
  const items = new Map<string, ItemBase>();
  return {
    getAll(status) {
      const all = Array.from(items.values());
      return status ? all.filter((i) => i.status === status) : all;
    },
    get(id) { return items.get(id); },
    insert(item) { items.set(item.id, item); },
    update(id, changes) {
      const existing = items.get(id);
      if (!existing) return undefined;
      const updated = { ...existing, ...changes };
      items.set(id, updated);
      return updated;
    },
    remove(id) { return items.delete(id); },
  };
}

// Singleton store — swap for sqlite.ts once library ships
const store = createInMemoryStore();

function stubRankNext(items: ItemBase[]): {
  events: ItemBase[];
  ranked_tasks: ItemBase[];
  slots: Array<{ item_id: string; start: string; end: string; rationale: string }>;
} {
  const now = new Date();
  const events = items.filter((i) => i.item_type === 'event');
  const tasks = items
    .filter((i) => i.item_type === 'task' && i.status === 'open')
    .sort((a, b) => a.title.localeCompare(b.title));
  const slots = tasks.slice(0, 3).map((t, idx) => {
    const start = new Date(now);
    start.setHours(9 + idx * 2, 0, 0, 0);
    const end = new Date(start);
    end.setHours(start.getHours() + 1);
    return {
      item_id: t.id,
      start: start.toISOString(),
      end: end.toISOString(),
      rationale: 'stub heuristic — replace with rank.next() once library ships',
    };
  });
  return { events, ranked_tasks: tasks, slots };
}

async function stubIngest(text: string): Promise<ItemBase[]> {
  // Minimal parse: each non-empty line becomes a task item
  const lines = text.split('\n').map((l) => l.trim()).filter(Boolean);
  return lines.map((title) => ({
    id: crypto.randomUUID(),
    item_type: 'task' as const,
    title,
    status: 'open' as const,
    created_at: new Date().toISOString(),
  }));
}

// ---------------------------------------------------------------------------
// App
// ---------------------------------------------------------------------------

const app = express();
app.use(cors({ origin: `http://localhost:${PORT}` }));
app.use(express.json());

const upload = multer({ storage: multer.memoryStorage(), limits: { fileSize: 25 * 1024 * 1024 } });

// GET /api/today
app.get('/api/today', (_req: Request, res: Response) => {
  const allItems = store.getAll();
  res.json(stubRankNext(allItems));
});

// GET /api/items
app.get('/api/items', (req: Request, res: Response) => {
  const status = typeof req.query['status'] === 'string' ? req.query['status'] : undefined;
  res.json(store.getAll(status));
});

// GET /api/items/:id
app.get('/api/items/:id', (req: Request, res: Response) => {
  const item = store.get(req.params['id']!);
  if (!item) { res.status(404).json({ error: 'Not found' }); return; }
  res.json(item);
});

// POST /api/items/ingest
app.post('/api/items/ingest', async (req: Request, res: Response, next: NextFunction) => {
  const { text } = req.body as { text?: string };
  if (!text) { res.status(400).json({ error: 'text is required' }); return; }
  try {
    const items = await stubIngest(text);
    items.forEach((i) => store.insert(i));
    res.json({ items });
  } catch (err) { next(err); }
});

// PUT /api/items/:id
app.put('/api/items/:id', (req: Request, res: Response) => {
  const updated = store.update(req.params['id']!, req.body as Partial<ItemBase>);
  if (!updated) { res.status(404).json({ error: 'Not found' }); return; }
  res.json(updated);
});

// DELETE /api/items/:id
app.delete('/api/items/:id', (req: Request, res: Response) => {
  const ok = store.remove(req.params['id']!);
  if (!ok) { res.status(404).json({ error: 'Not found' }); return; }
  res.status(204).end();
});

// POST /api/items/:id/complete
app.post('/api/items/:id/complete', (req: Request, res: Response) => {
  const updated = store.update(req.params['id']!, { status: 'completed' });
  if (!updated) { res.status(404).json({ error: 'Not found' }); return; }
  res.json({ ok: true });
});

// POST /api/voice/transcribe  (multipart audio → whisper.cpp)
app.post(
  '/api/voice/transcribe',
  upload.single('audio'),
  async (req: Request, res: Response, next: NextFunction) => {
    if (!req.file) { res.status(400).json({ error: 'audio file is required' }); return; }
    try {
      const whisper = process.env['WHISPER_BIN'] ?? 'whisper-cpp';
      const tmpFile = `/tmp/sched-audio-${Date.now()}.webm`;
      await import('node:fs/promises').then((fs) => fs.writeFile(tmpFile, req.file!.buffer));
      const { stdout } = await execFileAsync(whisper, ['-m', 'base', '-f', tmpFile, '--output-txt', '-']);
      const text = stdout.trim();
      res.json({ text });
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : String(err);
      // Return a clear error rather than crashing — whisper may not be installed
      res.status(503).json({ error: `Transcription unavailable: ${msg}` });
    }
  },
);

// Serve Angular SPA in production
if (existsSync(distDir)) {
  app.use(express.static(distDir));
  app.get('*', (_req: Request, res: Response) => {
    res.sendFile(join(distDir, 'index.html'));
  });
} else {
  app.get('/', (_req, res) => res.json({ status: 'API ready', ui: 'run nx serve scheduler-dashboard for the UI' }));
}

// Error handler
app.use((err: unknown, _req: Request, res: Response, _next: NextFunction) => {
  console.error(err);
  res.status(500).json({ error: 'Internal server error' });
});

const server = createServer(app);
server.listen(PORT, '127.0.0.1', () => {
  console.log(`scheduler-dashboard listening on http://localhost:${PORT}`);
});
