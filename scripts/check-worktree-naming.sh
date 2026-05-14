#!/usr/bin/env bash
set -euo pipefail

tmp="$(mktemp)"
trap 'rm -f "$tmp"' EXIT

if ! git worktree list --porcelain >"$tmp"; then
  echo "warning: unable to inspect git worktrees" >&2
  exit 0
fi

current_path=""
while IFS= read -r line; do
  case "$line" in
    worktree\ *)
      current_path="${line#worktree }"
      ;;
    branch\ refs/heads/*)
      branch="${line#branch refs/heads/}"
      if [[ "$branch" =~ ^(clawta|codex|copilot|claude-code|gemini|human)/[^/]+$ ]]; then
        echo "warning: legacy worktree branch '$branch' at '$current_path'; use swarm/<lane>-<ticket-short> for swarm work" >&2
      fi
      ;;
  esac
done <"$tmp"

exit 0
