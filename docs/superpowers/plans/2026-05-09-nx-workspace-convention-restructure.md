# Implementation Plan: Nx Workspace Convention Restructure

Status: Phase 0/1 restarted in this checkout.

Date: 2026-05-09

## Objective

Make the Nx graph reflect chitin's actual architecture using Nx naming
conventions: scope folders plus type-named projects (`feature`,
`data-access`, `ui`, `util`, or prefixed forms such as
`data-access-analysis`).

## Current Findings

- `python/analysis` was not visible in the Nx graph because it had no
  `project.json` or package workspace metadata.
- Current TS package names mostly do not expose their type in the project
  name. Examples: `@chitin/telemetry` should eventually become a
  `data-access` project under a telemetry/analysis scope, and tooling
  packages should become `util-*` or `tooling-*` projects.
- Current metadata still uses stale `layer:*` tags.

## Target Naming

- Nested by scope: `libs/<scope>/data-access`, `libs/<scope>/feature-*`,
  `libs/<scope>/util-*`.
- Non-nested/interim: `data-access-*`, `feature-*`, `util-*`.
- Chitin custom types should still be visible in names when used:
  `adapter-*`, `contract-*`, `tooling-*`.

## Phases

1. Make missing projects visible in the graph without moving code.
2. Replace stale `layer:*` tags with `type:*`, `scope:*`, and `lang:*`.
3. Move code into scope folders with type-named projects.
4. Rename package/project names where imports and publishing rules allow it.

## First Slice

Add `python/analysis/project.json` as `data-access-analysis` so the analysis
library appears in the graph immediately. This is an interim graph fix before
the larger move to `libs/analysis/data-access`.

