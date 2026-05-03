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
| `SLACK_SIGNING_SECRET` | From your Slack app's Basic Information page (mandatory) |
| `SLACK_ADMIN_USER_IDS` | Comma-separated allowlist of Slack user IDs permitted to invoke destructive actions (`envelope-grant`, `gate-reset`, `approve_pr`). Empty allowlist = nobody can invoke them. |
| `PORT` | HTTP listen port (default: 3000) |
| `CHITIN_KERNEL_BINARY` | Path to `chitin-kernel` binary (default: `chitin-kernel` on PATH) |
| `SLACK_APP_REPO` | Optional `owner/repo` override passed to `gh pr merge --repo`. When unset, `gh` infers the repo from the daemon's working directory. |

### Authorization model

Slack's signing-secret check proves the request came from your workspace, but does **not** identify which user submitted it. The daemon executes destructive actions only when the requesting `user_id` (from the Slack payload) is on the `SLACK_ADMIN_USER_IDS` allowlist. Read-only commands (`envelope-status`, `gate-status`, `chain-info`) are available to any signed Slack user since they don't change state.

Block actions (button clicks on notification messages — `gate_reset`, `grant_500_calls`, `approve_pr`) are always state-changing; every block action is gated on the admin allowlist.

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
This app ships as TypeScript run via `tsx` (no build/binary in this slice). Deploy by checking out the repo on the VPS and running `pnpm exec tsx apps/slack-app/src/main.ts` under systemd, behind nginx (TLS via Certbot). Required runtime: Node.js 22+, `chitin-kernel` on `PATH`, and `gh` CLI authenticated for the target repo (used by the `approve_pr` button). A pre-bundled binary distribution is a follow-up.

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
