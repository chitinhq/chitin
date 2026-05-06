#!/usr/bin/env bash
# install-hermes-plugin.sh — install the chitin-governance hermes plugin
# from the canonical source in docs/governance-setup-extras/ into the
# operator's ~/.hermes/plugins/chitin-governance/ directory.
#
# The plugin shells out to chitin-kernel on every hermes pre_tool_call.
# Two env vars (set by the swarm runner; can also be set in the
# operator's shell) make it cwd-independent and fail-closed:
#
#   CHITIN_POLICY_FILE=/path/to/chitin.yaml  → explicit policy path,
#       skips the kernel's cwd-walk-upward lookup
#   CHITIN_REQUIRE_POLICY=1                  → if the policy still cannot
#       be loaded, deny instead of silently allowing
#
# Without these the plugin falls back to legacy lenient behavior
# (cwd-walk + allow-on-no-policy) for backwards compat.
#
# Idempotent: re-runs overwrite the installed plugin files in place.

set -euo pipefail

REPO="${CHITIN_REPO:-$HOME/workspace/chitin}"
PLUGIN_SRC_DIR="$REPO/docs/governance-setup-extras"
PLUGIN_DST_DIR="${HERMES_PLUGINS_DIR:-$HOME/.hermes/plugins}/chitin-governance"

if [[ ! -f "$PLUGIN_SRC_DIR/hermes-plugin.py" ]]; then
  echo "install-hermes-plugin: source not found at $PLUGIN_SRC_DIR/hermes-plugin.py" >&2
  exit 1
fi
if [[ ! -f "$PLUGIN_SRC_DIR/hermes-plugin.yaml" ]]; then
  echo "install-hermes-plugin: source not found at $PLUGIN_SRC_DIR/hermes-plugin.yaml" >&2
  exit 1
fi

mkdir -p "$PLUGIN_DST_DIR"
cp "$PLUGIN_SRC_DIR/hermes-plugin.py"   "$PLUGIN_DST_DIR/__init__.py"
cp "$PLUGIN_SRC_DIR/hermes-plugin.yaml" "$PLUGIN_DST_DIR/plugin.yaml"
echo "install-hermes-plugin: installed to $PLUGIN_DST_DIR"

# Verify hermes sees it as enabled. If the user hasn't added
# chitin-governance to plugins.enabled in their hermes config, point
# them at the next step.
if command -v hermes >/dev/null 2>&1; then
  if hermes plugins list 2>/dev/null | grep -q 'chitin-governance.*enabled'; then
    echo "install-hermes-plugin: hermes reports chitin-governance enabled — done."
  else
    echo "install-hermes-plugin: hermes does not yet have chitin-governance in plugins.enabled."
    echo "  Add it to ~/.hermes/config.yaml (and any per-profile config you use):"
    echo "    plugins:"
    echo "      enabled:"
    echo "        - chitin-governance"
  fi
fi
