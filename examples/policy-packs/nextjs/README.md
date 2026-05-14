# Next.js Policy Pack

This pack is for Next.js applications that combine frontend code, server actions, and deployment wiring. It keeps the common baseline, then adds rules for framework env files, Vercel production actions, and broad framework upgrades.

## Included policies

- Blocks destructive shell actions, protected-branch pushes, remote code execution, and direct writes to operator credential paths.
- Protects all common Next.js env-file variants such as `.env.local` and `.env.production`.
- Blocks `vercel deploy --prod` and `vercel env pull` from agent sessions.
- Blocks broad upgrades to `next@latest` or `next@canary` that often mix framework migration with feature work.

## Apply this pack

1. Copy [`chitin.yaml`](./chitin.yaml) into the Next.js repo.
2. Adjust the deployment command patterns if you use a platform other than Vercel.
3. Review [`sample-violations.md`](./sample-violations.md) during rollout so the team knows which deployment actions stay human-controlled.

## Good fit

- Next.js repos deployed through Vercel
- Full-stack apps that mix frontend code with server-side secrets
- Teams that want agents to prepare changes but not promote them live
