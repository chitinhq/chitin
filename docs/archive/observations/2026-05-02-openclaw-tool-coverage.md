# OpenClaw Tool Coverage Audit (2026-05-02)

This document tracks tool registration coverage in OpenClaw, focusing on
mapping between OpenClaw's tool-registration call sites and chitin's
`gov.Normalize` switch in `go/execution-kernel/internal/gov/normalize.go`.

## Inputs

- `OPENCLAW_DIST` — path to an installed OpenClaw package's `dist/`
  directory. The audit greps it for tool-registration call sites
  (`name: "<tool_name>"`).
- `CHITIN_REPO` — path to a chitin repo checkout. The audit reads
  `go/execution-kernel/internal/gov/normalize.go` to enumerate the
  switch's mapped tool names.

The two inputs live in different trees, so the script takes both as
arguments rather than assuming a single cwd.

## Audit Script

```bash
#!/usr/bin/env bash
# Usage: audit.sh /path/to/openclaw/dist /path/to/chitin
set -euo pipefail

OPENCLAW_DIST="${1:?usage: audit.sh OPENCLAW_DIST CHITIN_REPO}"
CHITIN_REPO="${2:?usage: audit.sh OPENCLAW_DIST CHITIN_REPO}"

NORMALIZE_GO="$CHITIN_REPO/go/execution-kernel/internal/gov/normalize.go"

# 1. Tool names declared at OpenClaw registration call sites.
TOOL_NAMES=$(grep -rhoE 'name:\s*"[a-z_]+"' "$OPENCLAW_DIST" \
  | sed -E 's/.*name:\s*"([a-z_]+)".*/\1/' \
  | sort -u)

# 2. Tool names mapped by gov.Normalize. Each `case` line can carry one or
#    many comma-separated string literals (e.g. `case "exec", "process":`).
#    Extract every quoted identifier on every case line so multi-label
#    cases aren't dropped.
NORMALIZED_NAMES=$(grep -E '^\s*case "' "$NORMALIZE_GO" \
  | grep -oE '"[a-z_]+"' \
  | tr -d '"' \
  | sort -u)

# 3. Diff: tool names present in OpenClaw but not in gov.Normalize.
MISSING=$(comm -23 <(echo "$TOOL_NAMES") <(echo "$NORMALIZED_NAMES"))

# 4. Report.
if [[ -z "$MISSING" ]]; then
  echo "All OpenClaw tool-registration names are mapped in gov.Normalize."
else
  echo "Missing mappings in gov.Normalize for the following tool names:"
  echo "$MISSING"
fi
```

## Next Steps

- Wire this as a CI check (with both paths injected via env vars) so
  coverage drift gets caught when a new OpenClaw release adds tools.
- Review and update `gov.Normalize` mappings as new tools are
  registered.
