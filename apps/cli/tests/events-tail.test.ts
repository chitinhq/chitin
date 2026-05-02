import { describe, expect, it, beforeEach } from 'vitest';
import { writeFileSync, appendFileSync, mkdirSync, statSync } from 'node:fs';
import { mkdtempSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { drainOnce } from '../src/commands/events-tail';

let dir: string;
let file: string;

const ev = (idx: number) =>
  `{"ts":"2026-05-02T00:00:0${idx}Z","surface":"claude-code","event_type":"pre_tool_use","chain_id":"c-${idx}"}\n`;

beforeEach(() => {
  dir = mkdtempSync(join(tmpdir(), 'events-tail-test-'));
  mkdirSync(dir, { recursive: true });
  file = join(dir, 'events-test.jsonl');
});

describe('drainOnce', () => {
  it('reads complete lines and advances offset to the newline boundary', async () => {
    writeFileSync(file, ev(1) + ev(2) + ev(3));
    const size = statSync(file).size;

    const result = await drainOnce(file, 0, size, Buffer.alloc(0));

    expect(result.events.map((e) => e.chain_id)).toEqual(['c-1', 'c-2', 'c-3']);
    expect(result.nextOffset).toBe(size);
    expect(result.nextCarry.length).toBe(0);
  });

  it('holds a trailing partial line in nextCarry without advancing past it (#6)', async () => {
    // Two complete lines + a third line missing its newline.
    const partial = `{"ts":"X","surface":"x","event_type":"y","chain_id":"c-3"}`;
    writeFileSync(file, ev(1) + ev(2) + partial);
    const size = statSync(file).size;

    const result = await drainOnce(file, 0, size, Buffer.alloc(0));

    expect(result.events.map((e) => e.chain_id)).toEqual(['c-1', 'c-2']);
    // Offset advanced to the size we read up to (file end at drain start).
    // The partial bytes live in nextCarry — they belong to exactly one
    // place and the file offset moves past them.
    expect(result.nextOffset).toBe(size);
    expect(result.nextCarry.toString('utf8')).toBe(partial);
  });

  it('reunites the partial line with its remainder on the next drain (#6)', async () => {
    // Drain 1: two complete + partial third.
    const partial = `{"ts":"2026","surface":"x","event_type":"y","chain_id":"c-3"}`;
    writeFileSync(file, ev(1) + ev(2) + partial);
    const size1 = statSync(file).size;
    const r1 = await drainOnce(file, 0, size1, Buffer.alloc(0));

    // Writer flushes the remainder of line 3 + a fresh line 4.
    appendFileSync(file, '\n' + ev(4));
    const size2 = statSync(file).size;
    const r2 = await drainOnce(file, r1.nextOffset, size2, r1.nextCarry);

    expect(r2.events.map((e) => e.chain_id)).toEqual(['c-3', 'c-4']);
    expect(r2.nextOffset).toBe(size2);
    expect(r2.nextCarry.length).toBe(0);
  });

  it('survives a file that grows DURING the drain — appended bytes picked up by next drain (#18)', async () => {
    // Drain 1: file has 2 lines. We snapshot size, run drain.
    // Simulate growth: append a 3rd line BEFORE setting up the next drain
    // (in the real tail loop, the watch event triggers the next drain;
    // here we simulate by running drainOnce again).
    writeFileSync(file, ev(1) + ev(2));
    const size1 = statSync(file).size;
    const r1 = await drainOnce(file, 0, size1, Buffer.alloc(0));
    expect(r1.events.map((e) => e.chain_id)).toEqual(['c-1', 'c-2']);

    // Now the file grows.
    appendFileSync(file, ev(3));
    const size2 = statSync(file).size;
    const r2 = await drainOnce(file, r1.nextOffset, size2, r1.nextCarry);

    // The append was NOT skipped — drain 2 picks it up. Pre-fix, the
    // first drain set offsets[filename]=size_at_call_start which
    // would have been size1 even after the append, but if drain
    // 1's stream had read the append (timing-dependent), offset=size1
    // would have skipped it on next drain.
    expect(r2.events.map((e) => e.chain_id)).toEqual(['c-3']);
    expect(r2.nextOffset).toBe(size2);
  });

  it('handles UTF-8 multi-byte chars without splitting them across drains', async () => {
    // Multi-byte char in the JSON value forces byte-aware buffering.
    const line = `{"ts":"é","surface":"x","event_type":"y","chain_id":"c-1"}\n`;
    writeFileSync(file, line);
    const size = statSync(file).size;

    const result = await drainOnce(file, 0, size, Buffer.alloc(0));

    expect(result.events).toHaveLength(1);
    expect(result.events[0].ts).toBe('é');
    expect(result.nextOffset).toBe(size);
  });

  it('returns no events and does not advance when start >= size', async () => {
    writeFileSync(file, ev(1));
    const size = statSync(file).size;

    const result = await drainOnce(file, size, size, Buffer.alloc(0));

    expect(result.events).toEqual([]);
    expect(result.nextOffset).toBe(size);
    expect(result.nextCarry.length).toBe(0);
  });

  it('skips malformed JSON lines without advancing carry', async () => {
    writeFileSync(file, ev(1) + 'not-json\n' + ev(3));
    const size = statSync(file).size;

    const result = await drainOnce(file, 0, size, Buffer.alloc(0));

    // Both valid lines emit; bad line is silently skipped (offset still advances past it).
    expect(result.events.map((e) => e.chain_id)).toEqual(['c-1', 'c-3']);
    expect(result.nextOffset).toBe(size);
  });
});
