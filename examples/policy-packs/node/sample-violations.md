# Node Sample Violations

These examples show the command shapes this pack is intended to catch and the safer follow-up the operator should expect.

## 1. Package publish from an agent session

Violation:

```bash
pnpm publish --access public
```

Expected fix:

```bash
pnpm pack
```

Reason: packaging artifacts for review is allowed; publishing to the registry stays a human-owned release step.

## 2. Forced dependency remediation

Violation:

```bash
npm audit fix --force
```

Expected fix:

```bash
npm audit
```

Reason: the pack blocks automatic breaking upgrades and expects a reviewed dependency update instead.

## 3. Broad latest-version sweep

Violation:

```bash
pnpm up --latest
```

Expected fix:

```bash
pnpm up lodash
```

Reason: targeted upgrades are easier to review, test, and revert than repo-wide churn.
