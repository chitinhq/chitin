import { afterAll, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest';
import { AddressInfo } from 'node:net';
import { createHmac } from 'node:crypto';
import type { Server } from 'node:http';

// Mock the chitin tool layer so the server tests don't actually spawn
// chitin-kernel — these tests are about the HTTP plumbing, not the
// downstream tools.
vi.mock('../src/chitin.ts', () => ({
  envelopeList: vi.fn(() => []),
  envelopeGrant: vi.fn(),
  gateReset: vi.fn(),
  chainInfo: vi.fn(() => ({ exists: false })),
  gateStatus: vi.fn(() => ({ agent: 'a', escalation: 0 })),
}));

const SIGNING_SECRET = 'test-signing-secret';

function slackSign(secret: string, ts: string, body: string): string {
  const base = `v0:${ts}:${body}`;
  return 'v0=' + createHmac('sha256', secret).update(base).digest('hex');
}

async function postRaw(
  baseUrl: string,
  path: string,
  body: string,
  headers: Record<string, string> = {},
): Promise<{ status: number; body: string }> {
  const res = await fetch(`${baseUrl}${path}`, {
    method: 'POST',
    headers: { 'content-type': 'application/x-www-form-urlencoded', ...headers },
    body,
  });
  return { status: res.status, body: await res.text() };
}

async function getRaw(baseUrl: string, path: string): Promise<{ status: number; body: string }> {
  const res = await fetch(`${baseUrl}${path}`);
  return { status: res.status, body: await res.text() };
}

describe('createSlackServer', () => {
  let server: Server;
  let baseUrl: string;

  beforeAll(async () => {
    process.env.SLACK_SIGNING_SECRET = SIGNING_SECRET;
    process.env.SLACK_ADMIN_USER_IDS = 'UADMIN1,UADMIN2';
    // re-import after env set so the module-level constants pick up
    vi.resetModules();
    const { createSlackServer } = await import('../src/server.ts');
    server = createSlackServer();
    await new Promise<void>((resolve) => server.listen(0, resolve));
    const addr = server.address() as AddressInfo;
    baseUrl = `http://127.0.0.1:${addr.port}`;
  });

  afterAll(async () => {
    await new Promise<void>((resolve) => server.close(() => resolve()));
    delete process.env.SLACK_SIGNING_SECRET;
    delete process.env.SLACK_ADMIN_USER_IDS;
  });

  beforeEach(() => vi.clearAllMocks());

  describe('/health', () => {
    it('returns 200 without Slack signature (liveness probes work)', async () => {
      const r = await getRaw(baseUrl, '/health');
      expect(r.status).toBe(200);
      expect(JSON.parse(r.body)).toEqual({ ok: true });
    });
  });

  describe('signature verification', () => {
    it('rejects requests without a valid signature', async () => {
      const ts = String(Math.floor(Date.now() / 1000));
      const r = await postRaw(baseUrl, '/slack/commands', 'text=envelope-status', {
        'x-slack-signature': 'v0=invalid',
        'x-slack-request-timestamp': ts,
      });
      expect(r.status).toBe(401);
    });

    it('accepts requests with a valid signature', async () => {
      const ts = String(Math.floor(Date.now() / 1000));
      const body = 'text=envelope-status&user_id=UANY';
      const r = await postRaw(baseUrl, '/slack/commands', body, {
        'x-slack-signature': slackSign(SIGNING_SECRET, ts, body),
        'x-slack-request-timestamp': ts,
      });
      expect(r.status).toBe(200);
    });
  });

  describe('admin allowlist for destructive actions', () => {
    it('rejects envelope-grant from non-admin user with ephemeral message', async () => {
      const ts = String(Math.floor(Date.now() / 1000));
      const body = 'text=envelope-grant 01HZ 100&user_id=UNOTADMIN';
      const r = await postRaw(baseUrl, '/slack/commands', body, {
        'x-slack-signature': slackSign(SIGNING_SECRET, ts, body),
        'x-slack-request-timestamp': ts,
      });
      expect(r.status).toBe(200);
      const parsed = JSON.parse(r.body) as { response_type: string; text: string };
      expect(parsed.text).toContain('not on the SLACK_ADMIN_USER_IDS allowlist');
      expect(parsed.text).toContain('UNOTADMIN');
    });

    it('allows envelope-grant from an admin user', async () => {
      const ts = String(Math.floor(Date.now() / 1000));
      const body = 'text=envelope-grant 01HZ 100&user_id=UADMIN1';
      const r = await postRaw(baseUrl, '/slack/commands', body, {
        'x-slack-signature': slackSign(SIGNING_SECRET, ts, body),
        'x-slack-request-timestamp': ts,
      });
      expect(r.status).toBe(200);
      const parsed = JSON.parse(r.body) as { text: string };
      expect(parsed.text).toMatch(/Granted/);
    });

    it('allows envelope-status from any signed user (read-only)', async () => {
      const ts = String(Math.floor(Date.now() / 1000));
      const body = 'text=envelope-status&user_id=UREADER';
      const r = await postRaw(baseUrl, '/slack/commands', body, {
        'x-slack-signature': slackSign(SIGNING_SECRET, ts, body),
        'x-slack-request-timestamp': ts,
      });
      expect(r.status).toBe(200);
      const parsed = JSON.parse(r.body) as { text: string };
      expect(parsed.text).toContain('No envelopes');
    });

    it('rejects block actions from non-admin user', async () => {
      const ts = String(Math.floor(Date.now() / 1000));
      const payload = JSON.stringify({
        actions: [{ action_id: 'gate_reset', value: 'claude-code' }],
        user: { id: 'UNOTADMIN' },
      });
      const body = 'payload=' + encodeURIComponent(payload);
      const r = await postRaw(baseUrl, '/slack/actions', body, {
        'x-slack-signature': slackSign(SIGNING_SECRET, ts, body),
        'x-slack-request-timestamp': ts,
      });
      expect(r.status).toBe(200);
      const parsed = JSON.parse(r.body) as { text: string };
      expect(parsed.text).toContain('admin permission');
      expect(parsed.text).toContain('UNOTADMIN');
    });
  });

  describe('routes', () => {
    it('returns 404 for unknown paths (after sig check)', async () => {
      const ts = String(Math.floor(Date.now() / 1000));
      const body = '';
      const r = await postRaw(baseUrl, '/no-such-route', body, {
        'x-slack-signature': slackSign(SIGNING_SECRET, ts, body),
        'x-slack-request-timestamp': ts,
      });
      expect(r.status).toBe(404);
    });
  });
});
