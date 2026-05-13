# Governance Setup Extras

## Mirrored Workflow: kanban-dispatch.lobster

This directory contains a mirrored copy of the canonical `kanban-dispatch.lobster` workflow from the `swarm` repository.

- **Canonical location:** `swarm/workflows/kanban-dispatch.lobster`
- **Mirror location:** `docs/governance-setup-extras/kanban-dispatch.lobster`

### Sync Policy

The canonical source of truth is the file in `swarm/workflows/`. The copy in this directory is maintained for governance documentation and must always match the canonical version byte-for-byte.

A sync script and/or CI check ensures these files remain identical. If you update the workflow, update both copies and run the sync check.

---

For more details on the dispatch workflow, see the main repo README and `swarm/workflows/kanban-dispatch.lobster`.
