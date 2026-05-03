import { createServer, type IncomingMessage, type ServerResponse } from 'node:http';
import { verifySlackSignature } from './verify.ts';
import { handleSlashCommand, handleBlockAction } from './handlers.ts';
import type { SlackResponse } from './format.ts';

const SIGNING_SECRET = process.env.SLACK_SIGNING_SECRET ?? '';
const PORT = parseInt(process.env.PORT ?? '3000', 10);

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

async function handleRequest(req: IncomingMessage, res: ServerResponse): Promise<void> {
  const rawBody = await readBody(req);

  const headers = {
    'x-slack-signature': req.headers['x-slack-signature'] as string | undefined,
    'x-slack-request-timestamp': req.headers['x-slack-request-timestamp'] as string | undefined,
  };

  if (!verifySlackSignature(SIGNING_SECRET, headers, rawBody)) {
    res.writeHead(401, { 'content-type': 'text/plain' });
    res.end('Unauthorized');
    return;
  }

  const url = req.url ?? '/';

  if (url === '/slack/commands' && req.method === 'POST') {
    const params = parseFormEncoded(rawBody);
    const text = params['text'] ?? '';
    const result = handleSlashCommand(text);
    jsonResponse(res, 200, result);
    return;
  }

  if (url === '/slack/actions' && req.method === 'POST') {
    const params = parseFormEncoded(rawBody);
    const payloadStr = params['payload'];
    if (!payloadStr) {
      res.writeHead(400, { 'content-type': 'text/plain' });
      res.end('Missing payload');
      return;
    }
    let payload: { actions?: Array<{ action_id: string; value?: string }> };
    try {
      payload = JSON.parse(payloadStr) as typeof payload;
    } catch {
      res.writeHead(400, { 'content-type': 'text/plain' });
      res.end('Invalid JSON payload');
      return;
    }
    const action = payload.actions?.[0];
    if (!action) {
      res.writeHead(400, { 'content-type': 'text/plain' });
      res.end('No actions in payload');
      return;
    }
    const result = handleBlockAction(action.action_id, action.value ?? '');
    jsonResponse(res, 200, result);
    return;
  }

  if (url === '/health') {
    res.writeHead(200, { 'content-type': 'application/json' });
    res.end(JSON.stringify({ ok: true }));
    return;
  }

  res.writeHead(404, { 'content-type': 'text/plain' });
  res.end('Not found');
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
        res.writeHead(500, { 'content-type': 'text/plain' });
        res.end('Internal server error');
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
  const server = createSlackServer();
  server.listen(port, () => {
    console.log(JSON.stringify({
      ts: new Date().toISOString(),
      level: 'info',
      component: 'slack-app',
      msg: `listening on port ${port}`,
      port,
    }));
  });
}
