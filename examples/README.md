# Examples

This directory collects copyable, operator-facing examples that build on the kernel without expanding chitin's scope.

## Available examples

### Policy packs

Ready-to-use `chitin.yaml` starting points for common stacks:

- [`policy-packs/node/`](./policy-packs/node/) - Node.js services and CLIs
- [`policy-packs/python/`](./policy-packs/python/) - Python apps, jobs, and APIs
- [`policy-packs/go/`](./policy-packs/go/) - Go services and tooling
- [`policy-packs/rails/`](./policy-packs/rails/) - Ruby on Rails applications
- [`policy-packs/k8s/`](./policy-packs/k8s/) - Kubernetes and Helm-driven delivery
- [`policy-packs/nextjs/`](./policy-packs/nextjs/) - Next.js applications
- [`policy-packs/django/`](./policy-packs/django/) - Django applications
- [`policy-packs/monorepo/`](./policy-packs/monorepo/) - Nx, Turborepo, and multi-package repos

Each pack includes:

- `chitin.yaml` - curated baseline policy for the stack
- `README.md` - purpose, included rules, and how to apply the pack
- `sample-violations.md` - realistic violation examples and expected fixes

### Router plugins

Small, language-specific examples for extending chitin's router surfaces:

- [`router-plugins/python-blast-radius-v2/`](./router-plugins/python-blast-radius-v2/)
- [`router-plugins/python-nx-test-before-commit/`](./router-plugins/python-nx-test-before-commit/)
- [`router-plugins/typescript-allowlist/`](./router-plugins/typescript-allowlist/)
