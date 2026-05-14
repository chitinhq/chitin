# Node Policy Pack

This pack is a starting point for Node.js services, CLIs, and backend repos that depend on `npm`, `pnpm`, or `yarn`. It keeps the standard chitin safety baseline, then adds rules for package publishing, forced dependency churn, and Node-specific credential files.

## Included policies

- Blocks destructive shell actions, protected-branch pushes, remote code execution, and direct writes to operator credential paths.
- Protects `.npmrc` from agent writes so registry tokens and publish settings stay operator-managed.
- Blocks `npm publish`, `pnpm publish`, and `yarn npm publish` from agent sessions.
- Blocks forceful dependency remediation commands such as `npm audit fix --force` and broad `--latest` upgrades.

## Apply this pack

1. Copy [`chitin.yaml`](./chitin.yaml) into the repository root you want to govern.
2. Adjust `branches`, `bounds`, and any package-manager regexes to match your release workflow.
3. Review [`sample-violations.md`](./sample-violations.md) with the team before switching from `guide` to `enforce` in stricter environments.

## Good fit

- Express, Fastify, Nest, or Node CLI repositories
- Repos where package publishing is a separate human-controlled release step
- Teams that want dependency updates to stay explicit and reviewable
