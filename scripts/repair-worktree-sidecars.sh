#!/usr/bin/env bash
# repair-worktree-sidecars.sh — one-shot repair for existing worktrees missing
# governance sidecars (chitin.yaml.sig). See ticket t_4317ae81.
#
# Usage: scripts/repair-worktree-sidecars.sh [--dry-run]
#
# Scans all linked worktrees for chitin.yaml without a matching .sig sidecar,
# and copies the missing sidecar from the main repo checkout.

set -euo pipefail

dry_run=0
if [[ "${1:-}" == "--dry-run" ]]; then
  dry_run=1
fi

repo_root="$(git rev-parse --show-toplevel 2>/dev/null)" || {
  echo "error: not inside a chitin repository" >&2
  exit 1
}

repaired=0
skipped=0

while IFS= read -r worktree_dir; do
  # Skip the main checkout
  [[ "$worktree_dir" == "$repo_root" ]] && continue

  if [[ ! -f "$worktree_dir/chitin.yaml" ]]; then
    continue
  fi

  if [[ -f "$worktree_dir/chitin.yaml.sig" ]]; then
    skipped=$((skipped + 1))
    continue
  fi

  if [[ ! -f "$repo_root/chitin.yaml.sig" ]]; then
    echo "repair: source missing chitin.yaml.sig — cannot repair $worktree_dir" >&2
    continue
  fi

  if [[ "$dry_run" -eq 1 ]]; then
    echo "repair (dry-run): would copy chitin.yaml.sig -> $worktree_dir"
  else
    cp -p "$repo_root/chitin.yaml.sig" "$worktree_dir/chitin.yaml.sig"
    echo "repair: copied chitin.yaml.sig -> $worktree_dir"
  fi
  repaired=$((repaired + 1))
done < <(git -C "$repo_root" worktree list --porcelain | sed -n 's/^worktree //p')

echo "repair: $repaired repaired, $skipped already had sidecar" >&2
exit 0
