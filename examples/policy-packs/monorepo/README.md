# Monorepo Policy Pack

This pack is for multi-package repos such as Nx, Turborepo, or workspace-based monorepos. It keeps the common baseline, then adds stricter workspace-wide bounds, root-manifest guidance, and protections against recursive dependency churn and mass publish flows.

## Included policies

- Blocks destructive shell actions, protected-branch pushes, remote code execution, and direct writes to operator credential paths.
- Adds a guide rule for root manifests and workspace graph files such as `package.json`, `pnpm-lock.yaml`, and `nx.json`.
- Blocks recursive `--latest` dependency upgrades across the workspace.
- Blocks recursive publish/version commands that would affect many packages in one step.
- Blocks `nx run-many --all` style fan-out commands that can trigger broad unreviewed automation.

## Apply this pack

1. Copy [`chitin.yaml`](./chitin.yaml) into the monorepo root.
2. Adjust the root-manifest file list and workspace-tool regexes for your stack.
3. Review [`sample-violations.md`](./sample-violations.md) before rollout so engineers understand when a task should be split into smaller changes.

## Good fit

- Nx and Turborepo workspaces
- Yarn, pnpm, or npm workspace repos
- Any repo where a single command can cascade across many packages
