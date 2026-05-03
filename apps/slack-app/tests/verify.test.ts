import { describe, it, expect } from 'vitest';
import { createHmac } from 'node:crypto';
import { verifySlackSignature } from '../src/verify.ts';

const SECRET = 'test-signing-secret';

function makeSignature(secret: string, ts: string, body: string): string {
  return 'v0=' + createHmac('sha256', secret).update(`v0:${ts}:${body}`).digest('hex');
}

const nowS = () => String(Math.floor(Date.now() / 1000));

describe('verifySlackSignature', () => {
  it('accepts a valid signature', () => {
    const ts = nowS();
    const body = 'command=%2Fchitin&text=envelope-status';
    const sig = makeSignature(SECRET, ts, body);
    expect(verifySlackSignature(SECRET, { 'x-slack-signature': sig, 'x-slack-request-timestamp': ts }, body)).toBe(true);
  });

  it('rejects a tampered body', () => {
    const ts = nowS();
    const body = 'command=%2Fchitin&text=envelope-status';
    const sig = makeSignature(SECRET, ts, body);
    expect(verifySlackSignature(SECRET, { 'x-slack-signature': sig, 'x-slack-request-timestamp': ts }, body + 'x')).toBe(false);
  });

  it('rejects a wrong secret', () => {
    const ts = nowS();
    const body = 'text=hi';
    const sig = makeSignature('wrong-secret', ts, body);
    expect(verifySlackSignature(SECRET, { 'x-slack-signature': sig, 'x-slack-request-timestamp': ts }, body)).toBe(false);
  });

  it('rejects a stale timestamp (> 5 min)', () => {
    const ts = String(Math.floor(Date.now() / 1000) - 400);
    const body = 'text=hi';
    const sig = makeSignature(SECRET, ts, body);
    expect(verifySlackSignature(SECRET, { 'x-slack-signature': sig, 'x-slack-request-timestamp': ts }, body)).toBe(false);
  });

  it('rejects missing headers', () => {
    expect(verifySlackSignature(SECRET, {}, 'body')).toBe(false);
  });

  it('rejects non-numeric timestamp', () => {
    expect(verifySlackSignature(SECRET, { 'x-slack-signature': 'v0=abc', 'x-slack-request-timestamp': 'nan' }, 'body')).toBe(false);
  });
});
