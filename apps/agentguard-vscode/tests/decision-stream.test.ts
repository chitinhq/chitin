import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { createServer } from 'node:net';
import { afterEach, describe, it } from 'vitest';
import { DecisionStream } from '../src/decision-stream';

const cleanupPaths: string[] = [];

afterEach(() => {
  for (const path of cleanupPaths.splice(0)) {
    rmSync(path, { recursive: true, force: true });
  }
});

function tempChitinDir(): string {
  const root = mkdtempSync(join(tmpdir(), 'agentguard-vscode-'));
  const chitinDir = join(root, '.chitin');
  mkdirSync(chitinDir, { recursive: true });
  cleanupPaths.push(root);
  return chitinDir;
}

function waitFor(check: () => boolean, timeoutMs = 1000): Promise<void> {
  const started = Date.now();
  return new Promise((resolve, reject) => {
    const timer = setInterval(() => {
      if (check()) {
        clearInterval(timer);
        resolve();
        return;
      }
      if (Date.now() - started > timeoutMs) {
        clearInterval(timer);
        reject(new Error('timed out'));
      }
    }, 10);
  });
}

const sampleDecision = JSON.stringify({
  event_type: 'decision',
  ts: '2026-05-14T12:00:00Z',
  payload: {
    event_id: 'evt-stream',
    decision: 'deny',
    rule_id: 'no-write',
    action_type: 'file.write',
    action_target: '/tmp/a',
  },
});

describe('DecisionStream', () => {
  it('tails JSONL decision files when no socket is available', async () => {
    const chitinDir = tempChitinDir();
    const chainFile = join(chitinDir, 'events-run-1.jsonl');
    writeFileSync(chainFile, `${sampleDecision}\n`, 'utf8');

    const received: string[] = [];
    const stream = new DecisionStream({
      chitinDir,
      pollMs: 20,
      onDecision: (decision) => received.push(decision.eventId),
    });

    await stream.start();
    await waitFor(() => received.includes('evt-stream'));

    writeFileSync(
      chainFile,
      `${sampleDecision}\n${sampleDecision.replace('evt-stream', 'evt-stream-2')}\n`,
      'utf8',
    );
    await waitFor(() => received.includes('evt-stream-2'));
    stream.stop();
  });

  it('buffers a partial trailing line until its newline arrives', async () => {
    const chitinDir = tempChitinDir();
    const chainFile = join(chitinDir, 'events-run-1.jsonl');
    const partial = sampleDecision.replace('evt-stream', 'evt-stream-partial');
    // A complete record + a partial second line (no trailing newline).
    writeFileSync(chainFile, `${sampleDecision}\n${partial.slice(0, 40)}`, 'utf8');

    const received: string[] = [];
    const stream = new DecisionStream({
      chitinDir,
      pollMs: 20,
      onDecision: (decision) => received.push(decision.eventId),
    });
    await stream.start();
    await waitFor(() => received.includes('evt-stream'));
    // The partial line must not have been consumed nor skipped — completing
    // it must still deliver the record.
    writeFileSync(chainFile, `${sampleDecision}\n${partial}\n`, 'utf8');
    await waitFor(() => received.includes('evt-stream-partial'));
    stream.stop();
  });

  it('does not restart the tailer after stop()', async () => {
    const chitinDir = tempChitinDir();
    const chainFile = join(chitinDir, 'events-run-1.jsonl');
    writeFileSync(chainFile, `${sampleDecision}\n`, 'utf8');

    const received: string[] = [];
    const stream = new DecisionStream({
      chitinDir,
      pollMs: 20,
      onDecision: (decision) => received.push(decision.eventId),
    });
    await stream.start();
    await waitFor(() => received.includes('evt-stream'));
    stream.stop();
    writeFileSync(
      chainFile,
      `${sampleDecision}\n${sampleDecision.replace('evt-stream', 'evt-after-stop')}\n`,
      'utf8',
    );
    await new Promise((resolve) => setTimeout(resolve, 80));
    if (received.includes('evt-after-stop')) {
      throw new Error('tailer kept running after stop()');
    }
  });

  it('prefers a socket stream when one is available', async () => {
    const chitinDir = tempChitinDir();
    const socketPath = join(chitinDir, 'decisions.sock');
    const received: string[] = [];

    const server = createServer((socket) => {
      socket.write(`${sampleDecision}\n`);
    });
    await new Promise<void>((resolve) => server.listen(socketPath, resolve));

    const stream = new DecisionStream({
      chitinDir,
      socketPaths: [socketPath],
      onDecision: (decision) => received.push(decision.eventId),
    });

    await stream.start();
    await waitFor(() => received.includes('evt-stream'));
    stream.stop();
    server.close();
  });
});
