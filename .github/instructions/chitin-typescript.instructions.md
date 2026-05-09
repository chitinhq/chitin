---
applyTo: "apps/**,libs/**,scripts/hermes/**,*.ts,*.tsx,*.mts,*.cts,*.mjs"
---

Respect the Nx layer boundaries in `eslint.config.mjs`: contracts has no
internal deps, telemetry depends only on contracts, adapters and CLI depend
only on contracts plus telemetry, and the Go kernel stays separate.

Use existing workspace package links through the package manager instead of
patching around resolution with ad hoc `tsconfig` paths.

Keep TypeScript strict and ESM-friendly. Avoid CommonJS assumptions, unused
locals, and broad refactors unrelated to the task.

Hermes orchestration, approvals, kanban, and scheduling belong downstream
from chitin. Chitin-side TS packages should be analytics, contracts,
telemetry readers, adapters, plugins, or the operator CLI.
