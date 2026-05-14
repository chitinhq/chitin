# Go Sample Violations

These examples show the kinds of Go toolchain mutations this pack is designed to stop.

## 1. Persistent Go environment changes

Violation:

```bash
go env -w GOPRIVATE=github.com/acme/*
```

Expected fix:

```bash
export GOPRIVATE=github.com/acme/*
```

Reason: agent sessions should not rewrite machine-level Go configuration.

## 2. Repo-wide dependency upgrade

Violation:

```bash
go get -u ./...
```

Expected fix:

```bash
go get github.com/stretchr/testify@v1.10.0
```

Reason: targeted module bumps are reviewable; repo-wide upgrades are noisy and hard to verify.

## 3. Global code generation sweep

Violation:

```bash
go generate ./...
```

Expected fix:

```bash
go generate ./internal/router
```

Reason: scoped generation keeps the resulting diff tied to the package being changed.
