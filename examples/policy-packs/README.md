# Policy Packs

These packs give operators a non-blank starting point for `chitin.yaml`. Each one layers the same governance baseline over stack-specific risky commands, secret locations, and dependency-management workflows.

## How to use a pack

1. Pick the closest stack directory below.
2. Copy its `chitin.yaml` to the repo root or operator config directory you govern.
3. Edit branch names, bounds, and path patterns to match your environment.
4. Review `sample-violations.md` with the team so the blocked shapes are unsurprising.

## Packs

- [`node/`](./node/) - package-manager heavy Node.js repos
- [`python/`](./python/) - virtualenv, pip, Poetry, and Twine workflows
- [`go/`](./go/) - module-safe Go service repos
- [`rails/`](./rails/) - Rails apps with database and credentials guardrails
- [`k8s/`](./k8s/) - Kubernetes and Helm delivery boundaries
- [`nextjs/`](./nextjs/) - Next.js frontend and full-stack deployments
- [`django/`](./django/) - Django apps with migration, dump, and env protection
- [`monorepo/`](./monorepo/) - workspace-wide bounds and root-manifest safeguards

## Design notes

All packs follow the same shape for discoverability:

- Metadata: `id`, `name`, `description`, `mode`
- Global safety rails: `bounds`, `escalation`, `invariantModes`
- Rules: common baseline first, then stack-specific rules
- Docs: one README and one violation guide per pack
