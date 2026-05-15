# Monorepo Sample Violations

These examples show the broad workspace operations this pack is designed to narrow or block.

## 1. Recursive latest-version sweep

Violation:

```bash
pnpm -r up --latest
```

Expected fix:

```bash
pnpm --filter @acme/web up react@18.3.1
```

Reason: upgrade only the package you intend to change.

## 2. Recursive publish flow

Violation:

```bash
pnpm -r publish
```

Expected fix:

```bash
pnpm --filter @acme/web pack
```

Reason: packaging one workspace is safer than publishing many packages from the coding session.

## 3. Run-many against the whole graph

Violation:

```bash
pnpm exec nx run-many --target=build --all
```

Expected fix:

```bash
pnpm exec nx run web:build
```

Reason: targeted task execution keeps runtime, logs, and change intent bounded.
