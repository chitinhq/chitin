# Nx Workspace Conventions

Source: Nx project dependency and folder-structure docs recommend a small
library type vocabulary and naming convention:

- `feature` when nested, or `feature-*` when not nested.
- `data-access` when nested, or `data-access-*` when not nested.
- `ui` when nested, or `ui-*` when not nested.
- `util` when nested, or `util-*` when not nested.

Nx can also enforce the same architecture with project tags such as
`type:feature`, `type:data-access`, `type:ui`, and `type:util`.

## Chitin Target Shape

Prefer scope folders plus type-named projects:

```text
apps/
  cli/

libs/
  telemetry/
    data-access/
  analysis/
    data-access/
  drivers/
    adapter-claude-code/
    adapter-openclaw/
    adapter-ollama-local/
  tooling/
    util-generators/
    util-lint/

go/
  execution-kernel/
```

Interim project names may use the non-nested prefix form until code is moved,
for example `data-access-analysis` at `python/analysis`.

## Tags

Every real Nx project should carry:

- `type:*`
- `scope:*`
- `lang:*`

Chitin-specific types are allowed only when the Nx baseline types do not fit:

- `type:app` for runnable entrypoints.
- `type:adapter` for driver/substrate adapters.
- `type:contract` for canonical cross-language contracts.
- `type:tooling` for repo maintenance tools.

When possible, the visible project name or folder should still include the
type prefix (`data-access-*`, `util-*`, `adapter-*`, `contract-*`).

