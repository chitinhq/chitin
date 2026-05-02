# OpenClaw Tool Coverage Audit (May 2026)

This document tracks tool registration coverage in OpenClaw, focusing on mapping between tool-registration call sites and governance normalization logic.

## Audit Script

Below is a script to:
- Grep OpenClaw's dist for tool-registration call sites with `name: "[a-z_]+"`
- Extract all tool names found in registration calls
- Parse `gov.Normalize`'s switch cases to collect mapped tool names
- Diff the two sets to find unmapped tool names
- Output a report listing missing mappings

```bash
#!/usr/bin/env bash
set -euo pipefail

# 1. Find all tool-registration call sites with name: "[a-z_]+"
TOOL_NAMES=$(grep -rhoP 'name: "([a-z_]+)"' dist/ | sed -E 's/name: "([a-z_]+)"/\1/' | sort | uniq)

# 2. Extract mapped tool names from gov.Normalize's switch cases
NORMALIZED_NAMES=$(grep -P 'case "[a-z_]+":' go/execution-kernel/internal/gov/normalize.go | sed -E 's/.*case "([a-z_]+)":.*/\1/' | sort | uniq)

# 3. Diff the two sets to find unmapped tool names
MISSING=$(comm -23 <(echo "$TOOL_NAMES") <(echo "$NORMALIZED_NAMES"))

# 4. Output report
if [[ -z "$MISSING" ]]; then
  echo "All tool-registration names are mapped in gov.Normalize."
else
  echo "Missing mappings in gov.Normalize for the following tool names:"
  echo "$MISSING"
fi
```

## Next Steps
- Integrate this script as a CI check to ensure coverage remains complete.
- Review and update mappings as new tools are registered.
