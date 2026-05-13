#!/usr/bin/env bash
# sync-kanban-dispatch.sh
# Verifies that the mirrored kanban-dispatch.lobster matches the canonical source.

set -euo pipefail

CANONICAL="swarm/workflows/kanban-dispatch.lobster"
MIRROR="docs/governance-setup-extras/kanban-dispatch.lobster"

diff -q "$CANONICAL" "$MIRROR" || {
  echo "ERROR: $CANONICAL and $MIRROR differ. Please sync them." >&2
  exit 1
}

echo "kanban-dispatch.lobster is in sync."
