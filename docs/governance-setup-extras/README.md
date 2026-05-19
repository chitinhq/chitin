# Governance Setup Extras

## Mirrored Workflows

This directory contains mirrored copies of canonical Lobster workflows from the `swarm` repository.

- `swarm/workflows/kanban-dispatch.lobster` → `docs/governance-setup-extras/kanban-dispatch.lobster`
- `swarm/workflows/analyzer-cron.lobster` → `docs/governance-setup-extras/analyzer-cron.lobster`

### Sync Policy

The canonical source of truth is the file in `swarm/workflows/`. The copy in this directory is maintained for governance documentation and must always match the canonical version byte-for-byte.

A sync script and/or CI check ensures these files remain identical. If you update the workflow, update both copies and run the sync check.

---

For more details, see the main repo README plus the corresponding files under `swarm/workflows/`.
