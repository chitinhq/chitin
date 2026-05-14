# Rails Policy Pack

This pack targets Rails applications where database lifecycle commands, credentials, and gem updates need tighter guardrails. It keeps the common baseline and adds Rails-specific checks for destructive database tasks, credentials handling, and broad Bundler churn.

## Included policies

- Blocks destructive shell actions, protected-branch pushes, remote code execution, and direct writes to operator credential paths.
- Protects `config/master.key` from agent writes.
- Blocks `bin/rails db:drop` and `bin/rails db:reset` class commands.
- Blocks broad `bundle update` sweeps so gem upgrades stay targeted and reviewable.
- Blocks `bin/rails credentials:edit` so encrypted secrets remain operator-managed.

## Apply this pack

1. Copy [`chitin.yaml`](./chitin.yaml) into the Rails repo.
2. Adjust branch names, bounds, and any Rails command regexes to match your app conventions.
3. Share [`sample-violations.md`](./sample-violations.md) with the team before enabling it in production workflows.

## Good fit

- Standard Rails monoliths
- API-only Rails apps with migrations and encrypted credentials
- Teams that separate gem upgrades and release steps from day-to-day coding work
