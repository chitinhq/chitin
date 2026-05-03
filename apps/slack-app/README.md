# chitin Slack App

Two-way Slack integration for the chitin governance kernel. Operator can query envelope status, grant budget, and reset lockdowns directly from Slack.

## Commands

| Command | Action |
|---|---|
| `/chitin envelope-status` | List budget envelopes |
| `/chitin envelope-grant <id> <calls>` | Grant additional tool calls to an envelope |
| `/chitin gate-reset <agent>` | Clear agent lockdown |
| `/chitin gate-status <agent>` | Show agent gate state |
| `/chitin chain-info <session_id>` | Show chain head info |

Notification messages also carry action buttons (Reset lockdown, Grant +500 calls, Approve PR).

## Required Env Vars

| Variable | Purpose |
|---|---|
| `SLACK_SIGNING_SECRET` | From your Slack app's Basic Information page |
| `PORT` | HTTP listen port (default: 3000) |
| `CHITIN_KERNEL_BINARY` | Path to `chitin-kernel` binary (default: `chitin-kernel` on PATH) |

## Dev Setup (ngrok)

Slack needs a public HTTPS URL to POST to. Use ngrok for local dev:

```bash
# Terminal 1 – start the app
SLACK_SIGNING_SECRET=<your-secret> pnpm exec tsx apps/slack-app/src/main.ts

# Terminal 2 – expose it
ngrok http 3000
```

Copy the ngrok URL, then in your Slack app settings:
- **Slash Commands → Request URL**: `https://<ngrok-host>/slack/commands`
- **Interactivity → Request URL**: `https://<ngrok-host>/slack/actions`

## Prod Hosting Story

Two options:

**Cloudflare Tunnel (recommended for operator use):**
```bash
cloudflared tunnel run --url http://localhost:3000 <tunnel-name>
```
No open inbound ports; the tunnel keeps the connection alive. Run with systemd alongside `temporal-worker`.

**Small VPS (e.g. Hetzner CAX11):**
Deploy the binary behind nginx (TLS via Certbot), set `SLACK_SIGNING_SECRET` in environment. The binary has no external dependencies beyond `chitin-kernel` on PATH.

## Slack App Setup

1. Go to https://api.slack.com/apps → Create New App → From scratch
2. **Slash Commands** → Create `/chitin` → Request URL: `https://<host>/slack/commands`
3. **Interactivity & Shortcuts** → Request URL: `https://<host>/slack/actions`
4. **OAuth & Permissions** → Scopes: `commands`, `chat:write` (if app posts messages)
5. Install to workspace

## Architecture

- Raw `node:http` server — no framework dependencies
- Slack request verification via HMAC-SHA256 (`node:crypto`)
- Command handlers shell out to `chitin-kernel` (same pattern as `libs/mcp-chitin` targets)
- Block actions map to the same chitin operations, triggered by button clicks in notification messages
