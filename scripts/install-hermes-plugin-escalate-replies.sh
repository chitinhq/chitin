#!/usr/bin/env bash
# install-hermes-plugin-escalate-replies.sh — install the
# chitin-escalate-replies hermes plugin from the canonical source in
# docs/governance-setup-extras/escalate-replies/ into the operator's
# ~/.hermes/plugins/chitin-escalate-replies/ directory.
#
# What the plugin does:
#   Intercepts incoming messaging-platform replies (whatsapp, etc.)
#   matching `approve` / `approve <duration>` / `deny [reason]` and
#   resolves the matching chitin pending_approvals row via
#   `chitin-kernel pending approve|deny`. Closes the inbound-side gap
#   of chitin's operator-approval escalate flow — without it, the
#   operator's "approve" reply on whatsapp goes straight to the LLM
#   (which doesn't know about the kanban task).
#
# Hooks: pre_gateway_dispatch (fired BEFORE the LLM sees the message;
#   plugin returns {"action":"skip"} to consume the reply).
#
# Sender verification: only acts when the source chat_id matches the
# notify_chat_id from ~/.chitin/operator.yaml. Other chats fall
# through to normal LLM dispatch.
#
# Idempotent: re-runs overwrite the installed plugin files in place.
# After install:
#   - run `hermes plugins enable chitin-escalate-replies` (one time)
#   - restart hermes-gateway: `systemctl --user restart hermes-gateway.service`

set -euo pipefail

REPO="${CHITIN_REPO:-$HOME/workspace/chitin}"
PLUGIN_SRC_DIR="$REPO/libs/hermes-plugins/chitin-escalate-replies"
PLUGIN_DST_DIR="${HERMES_PLUGINS_DIR:-$HOME/.hermes/plugins}/chitin-escalate-replies"

if [[ ! -f "$PLUGIN_SRC_DIR/__init__.py" ]]; then
  echo "install-hermes-plugin-escalate-replies: source not found at $PLUGIN_SRC_DIR/__init__.py" >&2
  exit 1
fi
if [[ ! -f "$PLUGIN_SRC_DIR/plugin.yaml" ]]; then
  echo "install-hermes-plugin-escalate-replies: manifest not found at $PLUGIN_SRC_DIR/plugin.yaml" >&2
  exit 1
fi

mkdir -p "$PLUGIN_DST_DIR"
install -m 0644 "$PLUGIN_SRC_DIR/__init__.py"  "$PLUGIN_DST_DIR/__init__.py"
install -m 0644 "$PLUGIN_SRC_DIR/plugin.yaml" "$PLUGIN_DST_DIR/plugin.yaml"

echo "installed: $PLUGIN_DST_DIR"
echo "  next: hermes plugins enable chitin-escalate-replies"
echo "  next: systemctl --user restart hermes-gateway.service"
