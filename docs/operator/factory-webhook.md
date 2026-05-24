# Factory webhook operator runbook

The factory webhook is the automatic trigger surface for spec-driven
implementation work. `chitin-orchestrator factory-listen` accepts signed
GitHub `push` webhooks, detects added or modified
`.specify/specs/NNN-slug/tasks.md` files on the main branch, and dispatches
each matching spec through the same `chitin-orchestrator schedule` path used
by the manual operator CLI.

It does not clone, fetch, or mutate repositories. It only reads the webhook
payload and schedules against the operator's existing checkout.

## Start the listener

```bash
chitin-orchestrator factory-listen \
  --port 8765 \
  --repo-root /home/red/workspace/chitin \
  --main-branch main
```

Useful flags:

| Flag | Default | Purpose |
|---|---|---|
| `--port` | `8765` | Local HTTP port. The listener binds `127.0.0.1`. |
| `--secret-file` | `~/.chitin/factory-webhook.secret` | HMAC secret used to verify GitHub signatures. |
| `--repo-root` | `$CHITIN_REPO_ROOT`, then cwd repo discovery | Checkout passed through to `schedule`. |
| `--main-branch` | `main` | Only pushes to this branch dispatch. |
| `--temporal-host` | `$TEMPORAL_HOSTPORT` | Temporal frontend used by the schedule call. |
| `--target-repo` | same as `--repo-root` | Target repo passed through to `schedule`. |
| `--base-ref` | `main` | Base ref passed through to `schedule`. |
| `--log-file` | `~/.cache/chitin/factory-listen.jsonl` | Per-request JSONL log path. |

Health check:

```bash
curl -fsS http://127.0.0.1:8765/health
```

Expected response:

```json
{"ok":true}
```

## Generate the HMAC secret

Create one shared secret for GitHub and the local listener:

```bash
mkdir -p ~/.chitin
openssl rand -hex 32 > ~/.chitin/factory-webhook.secret
chmod 600 ~/.chitin/factory-webhook.secret
```

Use the file contents as the GitHub webhook secret. The listener verifies
GitHub's `X-Hub-Signature-256` header before parsing the payload. A missing,
empty, or mismatched secret returns HTTP 401 and does not schedule anything.

## Expose it with Cloudflare Tunnel

The listener binds localhost, so expose it through a tunnel rather than
binding directly to a public interface. Named tunnel example:

```bash
cloudflared tunnel create chitin-factory
cloudflared tunnel route dns chitin-factory factory.example.com
```

`~/.cloudflared/config.yml`:

```yaml
tunnel: chitin-factory
credentials-file: /home/red/.cloudflared/<tunnel-id>.json

ingress:
  - hostname: factory.example.com
    service: http://127.0.0.1:8765
  - service: http_status:404
```

Run it:

```bash
cloudflared tunnel run chitin-factory
```

The GitHub webhook URL for this example is:

```text
https://factory.example.com/webhook/push
```

## Configure the GitHub webhook

In the repository settings:

| GitHub field | Value |
|---|---|
| Payload URL | `https://factory.example.com/webhook/push` |
| Content type | `application/json` |
| Secret | contents of `~/.chitin/factory-webhook.secret` |
| SSL verification | enabled |
| Events | Just the `push` event |
| Active | checked |

After saving, use GitHub's "Recent Deliveries" panel to redeliver a payload
if the first attempt races listener startup or tunnel configuration.

## Local simulation

Use `simulate-webhook` before wiring GitHub. It constructs a synthetic push
payload, signs it with the same secret file, and posts to the local listener.

Terminal 1:

```bash
chitin-orchestrator factory-listen \
  --port 8765 \
  --repo-root /home/red/workspace/chitin
```

Terminal 2:

```bash
chitin-orchestrator simulate-webhook \
  --port 8765 \
  --spec-ref 100-factory-webhook-runbook \
  --branch main
```

Successful output is a JSON object shaped like:

```json
{
  "dispatched": true,
  "spec_refs": ["100-factory-webhook-runbook"],
  "run_ids": ["<uuid>"]
}
```

The simulator only proves the webhook path. The referenced spec must still be
valid for `chitin-orchestrator schedule <spec-ref>`: `tasks.md` must exist,
compile through the spec-kit adapter, and pass scheduler validation.

## Logs and audit trail

Every received request is appended to:

```text
~/.cache/chitin/factory-listen.jsonl
```

Override with:

```bash
CHITIN_FACTORY_LOG=/tmp/factory-listen.jsonl chitin-orchestrator factory-listen
```

Inspect recent requests:

```bash
tail -n 20 ~/.cache/chitin/factory-listen.jsonl | jq
```

Successful dispatches also emit `factory_triggered` chain events. Schedule
failures emit `factory_dispatch_failed` events. The underlying scheduler still
emits its normal `scheduler_started` event when the workflow is created.

```bash
jq 'select(.event_type=="factory_triggered" or .event_type=="factory_dispatch_failed")' \
  ~/.chitin/events-*.jsonl
```

## Troubleshooting

### 401 from GitHub

The HMAC signature failed. Confirm GitHub's webhook secret exactly matches
the contents of `~/.chitin/factory-webhook.secret`, with no extra whitespace
copied into the GitHub UI. If you rotate the file, update GitHub before
redelivering.

### 502 from GitHub or Cloudflare

The tunnel reached no healthy local listener. Check:

```bash
curl -fsS http://127.0.0.1:8765/health
pgrep -af 'chitin-orchestrator factory-listen'
cloudflared tunnel info chitin-factory
```

If `factory-listen` is not running, start it and redeliver from GitHub.

### Push returns 200 but `dispatched` is false

The listener intentionally returns HTTP 200 for skipped, well-formed payloads.
Check `skipped_reasons` in the response or JSONL log.

`non-main branch` means the payload `ref` did not match
`refs/heads/<main-branch>`. Either merge the spec work to main or start the
listener with the intended `--main-branch`.

`no tasks.md changes` means none of the pushed commits added or modified a
path matching `.specify/specs/NNN-slug/tasks.md`. Changes to `spec.md`,
`plan.md`, or docs do not dispatch by themselves.

### Dispatch failed for a detected spec

The webhook path worked, but the schedule call returned nonzero. Look for the
`factory_dispatch_failed` chain event and the JSONL `skipped_reasons` entry,
then run the same schedule command by hand:

```bash
chitin-orchestrator schedule \
  --repo-root /home/red/workspace/chitin \
  100-factory-webhook-runbook
```

Common causes are missing `tasks.md`, malformed task syntax, Temporal being
unreachable, or a DAG validation failure from an unclassified capability.
