# Go Policy Pack

This pack is aimed at Go services and tooling repos that use modules, `go test`, and the standard toolchain. It keeps the baseline governance rules, then adds controls around broad module upgrades, persistent Go environment mutation, and sweeping code generation.

## Included policies

- Blocks destructive shell actions, protected-branch pushes, remote code execution, and direct writes to operator credential paths.
- Blocks `go env -w` so machine-wide Go settings are not mutated by an agent session.
- Blocks broad `go get -u ./...` upgrades that can rewrite the dependency graph in one move.
- Blocks `go generate ./...` sweeps so generated output changes stay scoped and reviewable.

## Apply this pack

1. Copy [`chitin.yaml`](./chitin.yaml) into the governed repo.
2. Tune `branches`, `bounds`, and module-upgrade rules for your release process.
3. Review [`sample-violations.md`](./sample-violations.md) with engineers who routinely touch `go.mod`, `go.sum`, or generators.

## Good fit

- Go APIs and daemons
- CLI tools with committed generated code
- Teams that want module churn and global toolchain changes to stay explicit
