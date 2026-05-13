#!/usr/bin/env python3
"""lobster-discord-runner — wrap `lobster run` so approval gates fire on Discord.

Flow:
  1. Runs `lobster run --file <workflow> --args-json <args>`
  2. If the workflow halts with status=needs_approval, posts the prompt
     to Discord (Clawta bot, configured channel) with ✅/❌ reactions
  3. Polls Discord REST API for the operator's reaction
  4. Resumes with `lobster resume --token <X> --approve yes|no`
  5. Repeats until the workflow completes (status != needs_approval)

Why REST polling: avoids the discord.py WebSocket lifecycle complexity for
a script that needs to make one-off interactions. Polling every 3s is well
under Discord's rate limits.

Read at startup:
  - ~/.openclaw/openclaw.json → channels.discord.token (Clawta bot token)
  - ~/.openclaw/openclaw.json → gateway.auth.token (lobster→openclaw auth)

Env it sets for lobster:
  - OPENCLAW_URL=http://127.0.0.1:18789
  - OPENCLAW_TOKEN=<gateway auth token>

Usage:
  python3 _lobster_discord_runner.py \
    --file ~/.openclaw/workflows/kanban-dispatch.lobster \
    --args-json '{"ticket_id":"t_XXXXXXXX"}'
"""

from __future__ import annotations

import argparse
import json
import os
import subprocess
import sys
import time
import urllib.error
import urllib.parse
import urllib.request

DISCORD_API = "https://discord.com/api/v10"
OPENCLAW_CONFIG = os.path.expanduser("~/.openclaw/openclaw.json")
DEFAULT_CHANNEL = "1503842348897931375"  # #swarm in ChitinHQ
APPROVE_EMOJI = "✅"
REJECT_EMOJI = "❌"
POLL_INTERVAL_S = 3
APPROVAL_TIMEOUT_S = 24 * 3600  # 24h
MAX_GATES_PER_RUN = 20  # safety cap to prevent infinite loop


def log(msg: str) -> None:
    print(f"[runner] {msg}", file=sys.stderr, flush=True)


def load_config() -> dict:
    with open(OPENCLAW_CONFIG) as f:
        return json.load(f)


def discord_request(method: str, path: str, token: str, body: dict | None = None, retries: int = 5) -> dict:
    """Call Discord REST with token. Handles 429 with Retry-After backoff."""
    url = f"{DISCORD_API}{path}"
    data = json.dumps(body).encode() if body else None
    headers = {
        "Authorization": f"Bot {token}",
        "User-Agent": "lobster-discord-runner (chitin, 0.1)",
    }
    if body:
        headers["Content-Type"] = "application/json"
    for attempt in range(retries):
        req = urllib.request.Request(url, data=data, method=method, headers=headers)
        try:
            with urllib.request.urlopen(req, timeout=15) as r:
                raw = r.read()
                return json.loads(raw) if raw else {}
        except urllib.error.HTTPError as e:
            if e.code == 429 and attempt < retries - 1:
                retry_after = float(e.headers.get("Retry-After", "1"))
                # Cap so a rogue header doesn't park us forever
                time.sleep(min(retry_after, 10) + 0.25)
                continue
            raise


def post_message(token: str, channel_id: str, content: str) -> dict:
    return discord_request("POST", f"/channels/{channel_id}/messages", token, {"content": content})


def add_reaction(token: str, channel_id: str, message_id: str, emoji: str) -> None:
    encoded = urllib.parse.quote(emoji)
    discord_request("PUT", f"/channels/{channel_id}/messages/{message_id}/reactions/{encoded}/@me", token)


def list_reactors(token: str, channel_id: str, message_id: str, emoji: str) -> list[dict]:
    encoded = urllib.parse.quote(emoji)
    result = discord_request("GET", f"/channels/{channel_id}/messages/{message_id}/reactions/{encoded}", token)
    return result if isinstance(result, list) else []


def wait_for_approval(token: str, channel_id: str, message_id: str) -> str | None:
    """Poll until a non-bot user reacts. Returns 'yes', 'no', or None on timeout."""
    deadline = time.time() + APPROVAL_TIMEOUT_S
    while time.time() < deadline:
        try:
            for emoji, decision in [(APPROVE_EMOJI, "yes"), (REJECT_EMOJI, "no")]:
                reactors = list_reactors(token, channel_id, message_id, emoji)
                for u in reactors:
                    if not u.get("bot"):
                        log(f"operator reacted with {emoji} (user={u.get('username')})")
                        return decision
        except Exception as e:
            log(f"poll error (will retry): {e}")
        time.sleep(POLL_INTERVAL_S)
    return None


def run_lobster(workflow_file: str, args_json: str, env: dict) -> dict:
    cmd = ["pnpm", "exec", "lobster", "run", "--file", workflow_file, "--args-json", args_json]
    return _run_lobster_cmd(cmd, env)


def resume_lobster(resume_token: str, approve: str, env: dict) -> dict:
    cmd = ["pnpm", "exec", "lobster", "resume", "--token", resume_token, "--approve", approve]
    return _run_lobster_cmd(cmd, env)


def _run_lobster_cmd(cmd: list[str], env: dict) -> dict:
    log(f"$ {' '.join(cmd[:4])} ...")
    proc = subprocess.run(cmd, capture_output=True, text=True, env=env, cwd="/home/red/workspace/chitin")
    if proc.returncode != 0 and not proc.stdout:
        return {"error": "lobster failed", "stderr": proc.stderr[-500:], "returncode": proc.returncode}
    try:
        return json.loads(proc.stdout)
    except json.JSONDecodeError:
        return {"error": "non-json output", "stdout": proc.stdout[-500:], "stderr": proc.stderr[-500:]}


def build_env() -> dict:
    env = dict(os.environ)
    cfg = load_config()
    env.setdefault("OPENCLAW_URL", "http://127.0.0.1:18789")
    env.setdefault("OPENCLAW_TOKEN", cfg["gateway"]["auth"]["token"])
    return env


def main() -> int:
    p = argparse.ArgumentParser()
    p.add_argument("--file", required=True, help="Path to .lobster workflow file")
    p.add_argument("--args-json", default="{}", help="JSON args for the workflow")
    p.add_argument("--channel", default=DEFAULT_CHANNEL, help="Discord channel ID for approval prompts")
    args = p.parse_args()

    cfg = load_config()
    bot_token = cfg["channels"]["discord"]["token"]
    env = build_env()
    channel = args.channel

    log(f"running workflow {args.file} with args {args.args_json}")
    result = run_lobster(args.file, args.args_json, env)

    for gate_idx in range(MAX_GATES_PER_RUN):
        if result.get("status") != "needs_approval":
            break
        approval = result.get("requiresApproval", {})
        prompt = approval.get("prompt", "Approval needed").strip()
        resume_token = approval.get("resumeToken")
        approval_id = approval.get("approvalId", "")
        if not resume_token:
            log("workflow halted but no resumeToken in result; bailing")
            print(json.dumps(result, indent=2))
            return 3

        # Post prompt to Discord
        body = (
            f"**🦞 Approval needed** `{approval_id}`\n\n"
            f"{prompt}\n\n"
            f"React {APPROVE_EMOJI} to approve, {REJECT_EMOJI} to reject. "
            f"Timeout: 24h."
        )
        log(f"posting approval gate {gate_idx+1} to channel {channel}")
        msg = post_message(bot_token, channel, body)
        message_id = msg["id"]
        add_reaction(bot_token, channel, message_id, APPROVE_EMOJI)
        time.sleep(0.4)  # space out reactions to avoid 429 burst
        add_reaction(bot_token, channel, message_id, REJECT_EMOJI)

        # Poll for operator reaction
        decision = wait_for_approval(bot_token, channel, message_id)
        if decision is None:
            log("approval timed out")
            post_message(bot_token, channel, f"⏱️ Approval `{approval_id}` timed out after 24h — workflow aborted.")
            return 4

        # Post acknowledgement
        ack = "Approved ✅" if decision == "yes" else "Rejected ❌"
        post_message(bot_token, channel, f"**{ack}** `{approval_id}` — resuming workflow with --approve {decision}")

        # Resume
        result = resume_lobster(resume_token, decision, env)

    # Final result
    print(json.dumps(result, indent=2))
    if result.get("ok") is False:
        return 1
    if result.get("error"):
        return 2
    return 0


if __name__ == "__main__":
    sys.exit(main())
