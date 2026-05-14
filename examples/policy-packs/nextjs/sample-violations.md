# Next.js Sample Violations

These examples focus on framework and deployment actions that are high leverage in a Next.js repo.

## 1. Pulling platform-managed env vars

Violation:

```bash
vercel env pull .env.local
```

Expected fix:

```bash
vercel env ls
```

Reason: listing env metadata is safer than writing live secrets into the repo workspace.

## 2. Production deployment from the coding session

Violation:

```bash
vercel deploy --prod
```

Expected fix:

```bash
vercel deploy
```

Reason: preview deployments are acceptable for review; production promotion is not.

## 3. Broad framework jump

Violation:

```bash
pnpm up next@latest
```

Expected fix:

```bash
pnpm up next@14.2.7
```

Reason: explicit framework versioning makes migration risk visible in code review.
