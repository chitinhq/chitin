# Django Policy Pack

This pack is for Django applications where migrations, fixtures, and env handling need tighter review boundaries. It keeps the baseline governance rules and adds Django-specific controls for destructive management commands, full-data exports, and secret-bearing env files.

## Included policies

- Blocks destructive shell actions, protected-branch pushes, remote code execution, and direct writes to operator credential paths.
- Protects `.env`, `.env.*`, and common Django secret locations from agent writes.
- Blocks `manage.py flush` and migration-to-zero command shapes.
- Blocks `manage.py dumpdata` so agents do not exfiltrate a full logical database export by default.

## Apply this pack

1. Copy [`chitin.yaml`](./chitin.yaml) into the Django repo.
2. Tune the management-command regexes for your project layout and ops conventions.
3. Review [`sample-violations.md`](./sample-violations.md) with the team before enforcing it in shared environments.

## Good fit

- Django monoliths and API services
- Repos where data export and secret handling are tightly controlled
- Teams that want migration and fixture work to stay scoped and reviewable
