#!/usr/bin/env bash
# copy-governance-sidecars.sh — copy gitignored governance signature
# sidecars (chitin.yaml.sig) from a source chitin checkout into a
# freshly-created git worktree.
#
# Why this exists (ticket t_5b665efe):
# `git worktree add` does not copy gitignored files, and chitin.yaml.sig
# is gitignored — it is the operator's local, detached Ed25519 signature
# of chitin.yaml. A dispatched autonomous worker scrubs the operator-
# presence bypass (CHITIN_GOV_OPERATOR_AUTHORIZED) and therefore runs in
# sig-required mode: the kernel policy gate refuses to load chitin.yaml
# without a verifying chitin.yaml.sig and rejects EVERY tool call with
# `policy_signature_missing`. The worker is silently dead — it burns its
# whole iteration budget doing nothing.
#
# The .sig is a PUBLIC detached signature, not a secret. Copying it lets
# the worker VERIFY the genuine operator chitin.yaml against the pinned
# operator public key; the worker gains no authority and still cannot
# tamper — a modified chitin.yaml fails verification (no private key).
# This preserves the trust boundary; it does not subvert it.
#
# Bash counterpart of swarm/mini/_internal/worktree.py:
# copy_governance_sidecars (Mini sessions use the Python implementation;
# the kanban dispatch lobster workflow uses this one).
#
# Usage: copy-governance-sidecars.sh <source-repo> <worktree-dir>
#
# Idempotent: a sidecar already present in the worktree is left
# untouched. A sidecar missing from the source repo is skipped silently
# — the kernel policy gate surfaces a clear error if it is genuinely
# required and absent.

set -euo pipefail

# Gitignored governance sidecars that the kernel policy gate needs but
# `git worktree add` does not carry. Keep in sync with GOVERNANCE_SIDECARS
# in swarm/mini/_internal/worktree.py.
GOVERNANCE_SIDECARS=(chitin.yaml.sig)

if [[ $# -ne 2 ]]; then
  echo "usage: copy-governance-sidecars.sh <source-repo> <worktree-dir>" >&2
  exit 2
fi

SRC_REPO="$1"
WORKTREE_DIR="$2"

if [[ ! -d "$WORKTREE_DIR" ]]; then
  echo "copy-governance-sidecars: worktree dir does not exist: $WORKTREE_DIR" >&2
  exit 1
fi

copied=0
for sidecar in "${GOVERNANCE_SIDECARS[@]}"; do
  src="$SRC_REPO/$sidecar"
  dst="$WORKTREE_DIR/$sidecar"
  if [[ ! -f "$src" ]]; then
    continue  # source missing — silent skip; gate surfaces it if required
  fi
  if [[ -e "$dst" ]]; then
    continue  # already present — idempotent
  fi
  cp -p "$src" "$dst"
  echo "copy-governance-sidecars: copied $sidecar into $WORKTREE_DIR" >&2
  copied=$((copied + 1))
done

echo "copy-governance-sidecars: $copied sidecar(s) copied into $WORKTREE_DIR" >&2
exit 0
