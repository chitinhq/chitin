#!/usr/bin/env bash
# Spec 024 AC2: check that every active repo carries the 4-piece doc bundle.
#
# spec: 024-active-repo-doc-bundle
#
# Active repos are listed in chitinhq/workspace/roadmap.md under the
# "## The N truly-active repos" section (a markdown table; the first
# column is the repo slug as a fenced ID).
#
# Bundle pieces (per repo):
#   1. README.md
#   2. AGENTS.md OR CLAUDE.md
#   3. docs/roadmap.md
#   4. .specify/specs/INDEX.md
#
# Exit codes:
#   0 = all active repos compliant
#   1 = one or more bundle pieces missing
#   2 = workspace roadmap unreadable / unparseable
#   3 = an active-listed repo is GitHub-archived (mismatch — AC4)
#
# Env:
#   WORKSPACE_ROADMAP — path to roadmap.md (default ~/workspace/roadmap.md)
#   WORKSPACE_ROOT    — root for active-repo checkouts (default ~/workspace)

set -uo pipefail

WORKSPACE_ROADMAP="${WORKSPACE_ROADMAP:-${HOME}/workspace/roadmap.md}"
WORKSPACE_ROOT="${WORKSPACE_ROOT:-${HOME}/workspace}"

if [[ ! -f "$WORKSPACE_ROADMAP" ]]; then
    echo "ERROR: workspace roadmap not found at $WORKSPACE_ROADMAP" >&2
    exit 2
fi

# Extract active repo slugs from the roadmap. Spec 024 mandates the
# header "## The N truly-active repos" — we parse the table rows
# under it. Repo slug is in the first column, wrapped in backticks.
ACTIVE_REPOS=$(python3 - "$WORKSPACE_ROADMAP" <<'PY'
import re, sys
text = open(sys.argv[1]).read()
# Find the "truly-active" section
m = re.search(r"## The \d+ truly-active repos.*?(?=\n## )", text, re.DOTALL)
if not m:
    sys.stderr.write("ERROR: no '## The N truly-active repos' section\n")
    sys.exit(2)
section = m.group(0)
# Match table rows: | `org/repo` | ... |
for row in re.finditer(r"^\|\s*`([^`]+)`\s*\|", section, re.M):
    print(row.group(1))
PY
)
rc=$?
if [[ $rc -ne 0 ]]; then
    exit 2
fi

if [[ -z "$ACTIVE_REPOS" ]]; then
    echo "ERROR: no active repos parsed from $WORKSPACE_ROADMAP" >&2
    exit 2
fi

missing=0
for repo_slug in $ACTIVE_REPOS; do
    # Resolve checkout: workspace/<basename> for chitinhq/* and
    # wjcmurphy/*; ~/.hermes/<basename> for jpleva91/hermes-agent
    base=$(basename "$repo_slug")
    if [[ "$repo_slug" == "jpleva91/hermes-agent" ]]; then
        checkout="${HOME}/.hermes/hermes-agent"
    else
        checkout="${WORKSPACE_ROOT}/${base}"
    fi

    if [[ ! -d "$checkout" ]]; then
        echo "WARN: active repo $repo_slug not checked out at $checkout (skipped)" >&2
        continue
    fi

    # Optional archive check (AC4) — only if gh + auth available
    if command -v gh >/dev/null 2>&1; then
        archived=$(gh repo view "$repo_slug" --json isArchived --jq '.isArchived' 2>/dev/null || echo "unknown")
        if [[ "$archived" == "true" ]]; then
            echo "FAIL: $repo_slug is GitHub-archived but listed as active in $WORKSPACE_ROADMAP" >&2
            exit 3
        fi
    fi

    # Check the 4 bundle pieces
    for piece_check in \
        "README.md:$checkout/README.md" \
        "AGENTS-or-CLAUDE.md:$checkout/AGENTS.md:$checkout/CLAUDE.md" \
        "docs/roadmap.md:$checkout/docs/roadmap.md" \
        ".specify/specs/INDEX.md:$checkout/.specify/specs/INDEX.md"; do
        label="${piece_check%%:*}"
        rest="${piece_check#*:}"
        found=0
        IFS=':' read -ra paths <<< "$rest"
        for p in "${paths[@]}"; do
            if [[ -f "$p" ]]; then
                found=1
                break
            fi
        done
        if [[ $found -eq 0 ]]; then
            echo "MISS: $repo_slug missing $label (looked at: $rest)" >&2
            missing=$((missing + 1))
        fi
    done
done

if [[ $missing -gt 0 ]]; then
    echo "FAIL: $missing bundle piece(s) missing across active repos" >&2
    exit 1
fi

echo "OK: all active repos have all 4 bundle pieces"
exit 0
