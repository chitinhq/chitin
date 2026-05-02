import { readdirSync, existsSync, statSync, watch, createReadStream } from 'node:fs';
import { join } from 'node:path';

export interface TailOpts {
  workspace?: string;
  surface?: string;
}

/** One JSONL event line, post-parse. */
export interface TailEvent {
  ts: string;
  surface: string;
  event_type: string;
  chain_id: string;
}

/** Result of a single drain pass over one file. */
export interface DrainResult {
  events: TailEvent[];
  nextOffset: number;
  // The trailing partial line (no \n) carried into the next drain.
  // Empty Buffer when the file ended exactly on a newline boundary.
  nextCarry: Buffer;
}

/**
 * drainOnce reads the byte range [start, size) of the JSONL file and
 * yields complete-line events plus the next-offset and trailing-partial
 * carry buffer.
 *
 * Invariants (the bypass-class closure for #6 and #18):
 *   - nextOffset always points at a NEWLINE BOUNDARY in the file.
 *     A trailing partial line is held in nextCarry and re-presented to
 *     the next drainOnce so it gets reunited with its remainder.
 *   - The read is bounded to `[start, size)`, where size is the snapshot
 *     captured before this call. Bytes appended to the file DURING this
 *     drain are never consumed — the next drain will read them starting
 *     from `nextOffset`.
 *
 * carry is the partial line saved by the previous drain. It's bytes
 * that were already past the file's previous offset; we hold them in
 * memory and prepend to this drain's input so a writer flushing the
 * remainder gets a complete line emitted.
 */
export async function drainOnce(
  filePath: string,
  start: number,
  size: number,
  carry: Buffer,
): Promise<DrainResult> {
  const events: TailEvent[] = [];
  if (size <= start) {
    return { events, nextOffset: start, nextCarry: carry };
  }
  const stream = createReadStream(filePath, { start, end: size - 1 });
  let buf = carry.length > 0 ? Buffer.from(carry) : Buffer.alloc(0);
  let emittedBytes = 0; // total bytes (line + \n) shifted out of buf

  for await (const chunk of stream as AsyncIterable<Buffer>) {
    buf = buf.length === 0 ? chunk : Buffer.concat([buf, chunk]);
    let nl: number;
    while ((nl = buf.indexOf(0x0a)) >= 0) {
      const lineBytes = buf.subarray(0, nl);
      buf = buf.subarray(nl + 1);
      emittedBytes += nl + 1;

      const line = lineBytes.toString('utf8').trim();
      if (!line) continue;
      try {
        const ev = JSON.parse(line) as TailEvent;
        events.push(ev);
      } catch {
        // Malformed line — skip.
      }
    }
  }

  // The stream consumed exactly [start, size). nextOffset advances to
  // `size` so the next drain reads from there — never re-reads bytes
  // we just consumed. The trailing partial line (if any) is held in
  // nextCarry; it lives in memory across drains until the writer
  // flushes the rest, at which point it gets prepended to the next
  // drain's stream and emitted as a complete line.
  //
  // Why not "advance only past complete-line bytes"? That would cause
  // the next drain's stream to RE-READ the partial bytes from the file
  // — and we'd ALSO have them in carry — producing a doubled line.
  // The partial bytes belong to exactly one place (carry), and the
  // file offset moves past them.
  return {
    events,
    nextOffset: size,
    nextCarry: buf.length > 0 ? Buffer.from(buf) : Buffer.alloc(0),
  };
}

export function eventsTailCommand(opts: TailOpts): void {
  const workspace = opts.workspace ?? process.cwd();
  const chitinDir = join(workspace, '.chitin');
  if (!existsSync(chitinDir)) {
    process.stderr.write(`no .chitin dir at ${chitinDir} — nothing to tail\n`);
    return;
  }

  const offsets = new Map<string, number>();
  const draining = new Set<string>();
  const carryBuffers = new Map<string, Buffer>();

  async function drain(filename: string): Promise<void> {
    if (draining.has(filename)) return;
    draining.add(filename);
    try {
      const full = join(chitinDir, filename);
      const start = offsets.get(filename) ?? 0;
      const size = statSync(full).size;
      const carry = carryBuffers.get(filename) ?? Buffer.alloc(0);

      const { events, nextOffset, nextCarry } = await drainOnce(full, start, size, carry);

      for (const ev of events) {
        if (opts.surface && ev.surface !== opts.surface) continue;
        process.stdout.write(
          `${ev.ts}  ${ev.surface.padEnd(14)} ${ev.event_type.padEnd(16)} ${ev.chain_id.slice(0, 12)}\n`,
        );
      }

      offsets.set(filename, nextOffset);
      if (nextCarry.length > 0) {
        carryBuffers.set(filename, nextCarry);
      } else {
        carryBuffers.delete(filename);
      }
    } finally {
      draining.delete(filename);
    }
  }

  for (const f of readdirSync(chitinDir)) {
    if (f.startsWith('events-') && f.endsWith('.jsonl')) void drain(f);
  }

  watch(chitinDir, { persistent: true }, (_event, filename) => {
    if (!filename) return;
    const name = String(filename);
    if (!name.startsWith('events-') || !name.endsWith('.jsonl')) return;
    void drain(name);
  });

  process.stdout.write(`tailing ${chitinDir}/events-*.jsonl (Ctrl-C to stop)\n`);
}
