import { createServer, type IncomingMessage, type ServerResponse } from 'node:http';
import { verifySlackSignature } from './verify.ts';
import { handleSlashCommand, handleBlockAction, isDestructiveAction } from './handlers.ts';
import type { SlackResponse } from './format.ts';

const SIGNING_SECRET = process.env.SLACK_SIGNING_SECRET ?? '';
const PORT = parseInt(process.env.PORT ?? '3000', 10);

// Comma-separated allowlist of Slack user IDs permitted to invoke
// destructive actions (envelope-grant, gate-reset, approve_pr).
// Read-only commands (envelope-status, gate-status, chain-info) are
// available to any signed Slack user — verification of the signing
// secret is sufficient there since they don't change state.
//
// Empty allowlist = NO user is permitted to invoke destructive
// actions. The daemon ships read-only by default; admins must
// explicitly opt their Slack user IDs into destructive permissions.
const ADMIN_USER_IDS = new Set(
  (process.env.SLACK_ADMIN_USER_IDS ?? '')
    .split(',')
    .map((s) => s.trim())
    .filter(Boolean),
);

function readBody(req: IncomingMessage): Promise<string> {
  return new Promise((resolve, reject) => {
    const chunks: Buffer[] = [];
    req.on('data', (chunk: Buffer) => chunks.push(chunk));
    req.on('end', () => resolve(Buffer.concat(chunks).toString('utf8')));
    req.on('error', reject);
  });
}

function parseFormEncoded(body: string): Record<string, string> {
  const params = new URLSearchParams(body);
  const out: Record<string, string> = {};
  for (const [k, v] of params.entries()) out[k] = v;
  return out;
}

function jsonResponse(res: ServerResponse, status: number, body: SlackResponse): void {
  const payload = JSON.stringify(body);
  res.writeHead(status, {
    'content-type': 'application/json',
    'content-length': Buffer.byteLength(payload),
  });
  res.end(payload);
}

function plainResponse(res: ServerResponse, status: number, body: string): void {
  res.writeHead(status, { 'content-type': 'text/plain' });
  res.end(body);
}

async function handleRequest(req: IncomingMessage, res: ServerResponse): Promise<void> {
  const url = req.url ?? '/';

  // Liveness probe — must be reachable WITHOUT Slack signature, so
  // standard k8s/systemd health checks don't get 401. /health does
  // not return any state-bearing information; safe to expose.
  if (url === '/health') {
    res.writeHead(200, { 'content-type': 'application/json' });
    res.end(JSON.stringify({ ok: true }));
    return;
  }

  const rawBody = await readBody(req);

  const headers = {
    'x-slack-signature': req.headers['x-slack-signature'] as string | undefined,
    'x-slack-request-timestamp': req.headers['x-slack-request-timestamp'] as string | undefined,
  };

  if (!verifySlackSignature(SIGNING_SECRET, headers, rawBody)) {
    plainResponse(res, 401, 'Unauthorized');
    return;
  }

  if (url === '/slack/commands' && req.method === 'POST') {
    const params = parseFormEncoded(rawBody);
    const text = params['text'] ?? '';
    const userId = params['user_id'] ?? '';
    const sub = text.trim().split(/\s+/)[0] ?? '';
    if (isDestructiveAction(sub) && !ADMIN_USER_IDS.has(userId)) {
      jsonResponse(res, 200, {
        response_type: 'ephemeral',
        text:
          `:no_entry: \`${sub}\` is a destructive command and your Slack user (\`${userId}\`) ` +
          `is not on the SLACK_ADMIN_USER_IDS allowlist. ` +
          `Ask an operator to add your user ID to the daemon's env.`,
      });
      return;
    }
    const result = handleSlashCommand(text);
    jsonResponse(res, 200, result);
    return;
  }

  if (url === '/slack/actions' && req.method === 'POST') {
    const params = parseFormEncoded(rawBody);
    const payloadStr = params['payload'];
    if (!payloadStr) {
      plainResponse(res, 400, 'Missing payload');
      return;
    }
    let payload: {
      actions?: Array<{ action_id: string; value?: string }>;
      user?: { id?: string };
    };
    try {
      payload = JSON.parse(payloadStr) as typeof payload;
    } catch {
      plainResponse(res, 400, 'Invalid JSON payload');
      return;
    }
    const action = payload.actions?.[0];
    if (!action) {
      plainResponse(res, 400, 'No actions in payload');
      return;
    }
    const userId = payload.user?.id ?? '';
    // Block actions are always state-changing — every one of them is
    // destructive (gate_reset / grant_500_calls / approve_pr). Gate
    // them all on the admin allowlist.
    if (!ADMIN_USER_IDS.has(userId)) {
      jsonResponse(res, 200, {
        response_type: 'ephemeral',
        text:
          `:no_entry: button actions require admin permission; your Slack user ` +
          `(\`${userId}\`) is not on the SLACK_ADMIN_USER_IDS allowlist.`,
      });
      return;
    }
    const result = handleBlockAction(action.action_id, action.value ?? '');
    jsonResponse(res, 200, result);
    return;
  }

  plainResponse(res, 404, 'Not found');
}

export function createSlackServer() {
  return createServer((req, res) => {
    handleRequest(req, res).catch((err: unknown) => {
      console.error(JSON.stringify({
        ts: new Date().toISOString(),
        level: 'error',
        component: 'slack-app',
        msg: 'unhandled request error',
        error: err instanceof Error ? err.message : String(err),
      }));
      if (!res.headersSent) {
        plainResponse(res, 500, 'Internal server error');
      }
    });
  });
}

export function startServer(port: number = PORT): void {
  if (!SIGNING_SECRET) {
    console.error(JSON.stringify({
      ts: new Date().toISOString(),
      level: 'error',
      component: 'slack-app',
      msg: 'SLACK_SIGNING_SECRET is not set — all requests will be rejected',
    }));
  }
  if (ADMIN_USER_IDS.size === 0) {
    console.error(JSON.stringify({
      ts: new Date().toISOString(),
      level: 'warn',
      component: 'slack-app',
      msg: 'SLACK_ADMIN_USER_IDS is empty — destructive actions will be rejected',
    }));
  }
  const server = createSlackServer();
  server.listen(port, () => {
    console.log(JSON.stringify({
      ts: new Date().toISOString(),
      level: 'info',
      component: 'slack-app',
      msg: `listening on port ${port}`,
      port,
      admin_user_ids_count: ADMIN_USER_IDS.size,
    }));
  });
}
