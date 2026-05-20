#!/usr/bin/env bash
# install-mini-mcp.sh — register the mini MCP server with Claude Code.
#
# The mini MCP server (services/mini-mcp/server.py) is a stdio JSON-RPC
# server Claude Code spawns on demand — it is not a daemon, so there is
# no cron entry. "Installing" it means registering it in the Claude Code
# MCP config so `mini_open`/`mini_nudge`/etc. are available as tools.
#
# Idempotent (constitution §6): re-running re-points the registration at
# the current repo path. Safe to run after moving the repo.
#
# Usage:
#   bash swarm/bin/install-mini-mcp.sh [--dry-run]

set -euo pipefail

DRY_RUN=0
while [[ $# -gt 0 ]]; do
  case "$1" in
    --dry-run) DRY_RUN=1 ;;
    -h|--help)
      echo "Usage: $(basename "$0") [--dry-run]"
      echo "  Registers the mini MCP server with Claude Code."
      exit 0 ;;
    *) echo "ERROR: unknown arg: $1" >&2; exit 2 ;;
  esac
  shift
done

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
SERVER="$REPO_ROOT/services/mini-mcp/server.py"

if [[ ! -f "$SERVER" ]]; then
  echo "ERROR: MCP server not found at $SERVER" >&2
  exit 1
fi

if ! command -v claude >/dev/null 2>&1; then
  echo "ERROR: claude CLI not on PATH — cannot register the MCP server" >&2
  exit 1
fi

if [[ $DRY_RUN -eq 1 ]]; then
  echo "[dry-run] would: claude mcp remove mini   (ignore-if-absent)"
  echo "[dry-run] would: claude mcp add mini python3 $SERVER"
  exit 0
fi

# Remove any stale registration first so the path is always current.
claude mcp remove mini >/dev/null 2>&1 || true
claude mcp add mini python3 "$SERVER"
echo "registered: mini MCP server -> $SERVER"
echo "tools available: mini_open, mini_nudge, mini_status, mini_stop, mini_list"
echo "NOTE: restart any running Claude Code session to pick up the new server."
