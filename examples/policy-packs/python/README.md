# Python Policy Pack

This pack targets Python applications, services, and jobs that use `pip`, virtual environments, Poetry, or Twine. It keeps the common baseline, then adds rules for Python package publishing, unsafe global installs, and Python credential files.

## Included policies

- Blocks destructive shell actions, protected-branch pushes, remote code execution, and direct writes to operator credential paths.
- Protects `.pypirc` from agent writes so package index credentials stay operator-managed.
- Blocks `twine upload` and `poetry publish` from agent sessions.
- Blocks `pip install --break-system-packages` and broad upgrade patterns that often mutate shared interpreters unexpectedly.

## Apply this pack

1. Copy [`chitin.yaml`](./chitin.yaml) into the repo root you want to govern.
2. Tune `branches`, `bounds`, and dependency command regexes for your package manager of record.
3. Use [`sample-violations.md`](./sample-violations.md) during rollout so the team understands which Python workflows stay manual.

## Good fit

- FastAPI, Flask, or Celery repositories
- Data and automation repos that still need strong secret and dependency guardrails
- Teams that separate artifact build from package publication
