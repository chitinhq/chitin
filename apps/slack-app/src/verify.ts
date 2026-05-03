import { createHmac, timingSafeEqual } from 'node:crypto';

export interface SlackHeaders {
  'x-slack-signature'?: string;
  'x-slack-request-timestamp'?: string;
}

// Slack signs requests with HMAC-SHA256 using the signing secret.
// Reject requests older than 5 minutes to prevent replay attacks.
const REPLAY_WINDOW_S = 300;

export function verifySlackSignature(
  signingSecret: string,
  headers: SlackHeaders,
  rawBody: string,
): boolean {
  const sig = headers['x-slack-signature'];
  const ts = headers['x-slack-request-timestamp'];
  if (!sig || !ts) return false;

  const tsNum = parseInt(ts, 10);
  if (Number.isNaN(tsNum)) return false;
  const nowS = Math.floor(Date.now() / 1000);
  if (Math.abs(nowS - tsNum) > REPLAY_WINDOW_S) return false;

  const baseString = `v0:${ts}:${rawBody}`;
  const expected = 'v0=' + createHmac('sha256', signingSecret).update(baseString).digest('hex');
  const expectedBuf = Buffer.from(expected, 'ascii');
  const sigBuf = Buffer.from(sig, 'ascii');
  if (expectedBuf.length !== sigBuf.length) return false;
  return timingSafeEqual(expectedBuf, sigBuf);
}
