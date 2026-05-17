#!/usr/bin/env bash
# pre-push-protected-branch-guard.sh — refuse pushes to protected branches
# from non-PR contexts.
#
# Protected branches by default: main, master, swarm, develop.
# Override via PROTECTED_BRANCHES env (space-separated).
#
# Installs as a git pre-push hook OR can be invoked manually before a push.
#
# Background: 2026-05-17 day-0 portal MVP retro found that an agent
# direct-pushed to chitin/main because the local working tree had drifted
# to main (via stash/checkout interactions) without the agent noticing.
# `git push origin HEAD` then went to main. This guard catches that
# mistake before it lands.
#
# How it works:
# - In hook mode: stdin gives "local_ref local_sha remote_ref remote_sha"
#   per ref being pushed; we reject if remote_ref matches a protected
#   pattern AND the local branch isn't a PR branch (PR branches start
#   with feat/, fix/, chore/, docs/, refactor/, test/, retro/, review/,
#   spec-kit/).
# - Override with PUSH_TO_PROTECTED=1 env var (operator escape hatch).
set -euo pipefail

PROTECTED_BRANCHES="${PROTECTED_BRANCHES:-main master swarm develop}"
PR_BRANCH_PREFIXES_REGEX='^(refs/heads/)?(feat|fix|chore|docs|refactor|test|retro|review|spec-kit|agent)/'

if [[ "${PUSH_TO_PROTECTED:-}" == "1" ]]; then
    # Operator explicitly opted in; let it through (with a loud notice).
    echo "[pre-push-guard] PUSH_TO_PROTECTED=1 — bypass enabled." >&2
    exit 0
fi

violations=()

while read -r local_ref local_sha remote_ref remote_sha; do
    # Skip deletes (local_sha is all zeros).
    if [[ "$local_sha" =~ ^0+$ ]]; then
        continue
    fi

    # Extract the remote branch name from remote_ref (refs/heads/<name>).
    remote_branch="${remote_ref#refs/heads/}"

    # Check if the remote branch is in the protected list.
    is_protected=0
    for prot in $PROTECTED_BRANCHES; do
        if [[ "$remote_branch" == "$prot" ]]; then
            is_protected=1
            break
        fi
    done

    if [[ $is_protected -eq 0 ]]; then
        continue
    fi

    # Protected branch. Check if the local ref looks like a PR branch.
    # If we're on a feature-ish branch and pushing TO a protected branch,
    # that's a clear misconfiguration (the user meant to push to the
    # feature branch, not the protected one).
    if [[ "$local_ref" =~ $PR_BRANCH_PREFIXES_REGEX ]]; then
        violations+=("$local_ref → $remote_ref (feature branch pushed to protected branch — almost certainly a mistake)")
        continue
    fi

    # Local branch IS the protected branch (e.g., local 'main' → remote 'main').
    # This is the worst case — direct work on the protected branch.
    violations+=("$local_ref → $remote_ref (direct push to protected branch from local working copy)")
done

if [[ ${#violations[@]} -gt 0 ]]; then
    echo "" >&2
    echo "[pre-push-guard] REFUSING push — protected-branch violation:" >&2
    for v in "${violations[@]}"; do
        echo "  - $v" >&2
    done
    echo "" >&2
    echo "Recovery:" >&2
    echo "  1. If you meant to push to a PR branch, switch first:" >&2
    echo "       git checkout -b feat/<name>  # or fix/, chore/, etc." >&2
    echo "       git push -u origin feat/<name>" >&2
    echo "  2. If you genuinely intend to push to the protected branch," >&2
    echo "     set PUSH_TO_PROTECTED=1 and retry. This bypass is logged." >&2
    echo "" >&2
    exit 1
fi

exit 0
