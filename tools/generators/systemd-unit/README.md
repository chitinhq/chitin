# systemd-unit generator

Generates a chitin service/timer pair from `service.tmpl` and `timer.tmpl`.

## Defaults

- **Type=oneshot** — the service runs once per timer tick, then exits.
- **StandardOutput=journal / StandardError=journal** — all output lands in `journalctl --user -u chitin-<name>`.
- **Persistent=false** — missed ticks are dropped; the next tick resumes normally.
- **TimeoutStartSec=60** — override with `--timeout` for long-running passes.
- **OnBootSec=5min** — gives the system time to settle before the first tick.

## Usage

```sh
# Synopsis (POSIX-shell quirk: the line-continuation `\` MUST be the
# last character on its line — anything after it, including a comment,
# breaks the continuation. Inline comments removed; per-flag notes
# follow the synopsis.)
bash tools/generators/systemd-unit/generate.sh \
  --name <unit-name> \
  --description "<text>" \
  ( --ts-script <script> | --exec <full-cmd> ) \
  ( --interval <duration> | --calendar "<spec>" ) \
  [--boot-delay <dur>] \
  [--timeout <sec>] \
  [--persistent] \
  [--after <unit>] \
  [--output-dir <dir>] \
  [--dry-run]
```

Per-flag:
- `--name` — e.g. `pr-event-ingester` (becomes `chitin-pr-event-ingester.{service,timer}`)
- `--description` — human label (appears in `systemctl status`)
- `--ts-script` — shorthand: `pnpm exec tsx apps/runner/src/<script>.ts`
- `--exec` — full ExecStart path (alternative to `--ts-script`)
- `--interval` — e.g. `5min`, `1h`, `24h` (`OnUnitActiveSec`)
- `--calendar` — e.g. `'*-*-* 06:00:00'` (`OnCalendar`)
- `--boot-delay` — `OnBootSec` for interval timers (default: `5min`); ignored on calendar timers (which fire only per `OnCalendar`)
- `--timeout` — `TimeoutStartSec` in seconds (default: `60`)
- `--persistent` — `Persistent=true` (a missed tick fires on next boot)
- `--after` — `After=chitin-<unit>.service` in `[Unit]`
- `--output-dir` — default: `infra/systemd`
- `--dry-run` — print to stdout, don't write files

## Examples

### Per-tick interval (every 5 minutes)

```sh
bash tools/generators/systemd-unit/generate.sh \
  --name pr-event-ingester \
  --description "PR event ingester" \
  --ts-script pr-event-ingester \
  --interval 5min \
  --after worker
```

### Daily at a fixed calendar time

```sh
bash tools/generators/systemd-unit/generate.sh \
  --name nightly-report \
  --description "nightly summary report" \
  --ts-script nightly-report \
  --calendar '*-*-* 02:00:00' \
  --boot-delay 10min \
  --persistent
```

### Custom exec and longer timeout

```sh
bash tools/generators/systemd-unit/generate.sh \
  --name heavy-analysis \
  --description "heavy analysis pass" \
  --exec '%h/workspace/chitin/scripts/heavy-analysis.sh' \
  --interval 6h \
  --timeout 600
```

## Installing

After generating, symlink units and reload:

```sh
bash scripts/install-systemd-units.sh          # symlink + reload
bash scripts/install-systemd-units.sh --enable # symlink + reload + enable timers
```

Verify:

```sh
systemctl --user list-timers
journalctl --user -u chitin-<name> -f
```
