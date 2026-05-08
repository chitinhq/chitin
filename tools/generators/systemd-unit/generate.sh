#!/usr/bin/env bash
# Generate a chitin systemd service/timer pair from templates.
#
# Default behavior: one-shot Type=oneshot, StandardOutput=journal,
# per-tick interval timing via OnUnitActiveSec.
#
# Usage:
#   generate.sh --name <unit> --description <desc> --exec <cmd> \
#               (--interval <dur> | --calendar <spec>) [options]
#
# Options:
#   --name <name>          Unit name, e.g. pr-event-ingester (required)
#   --description <text>   Human-readable description (required)
#   --exec <path>          ExecStart command (required)
#   --interval <duration>  OnUnitActiveSec, e.g. 5min, 1h, 24h (mutually exclusive with --calendar)
#   --calendar <spec>      OnCalendar spec, e.g. '*-*-* 06:00:00' (mutually exclusive with --interval)
#   --boot-delay <dur>     OnBootSec (default: 5min)
#   --timeout <sec>        TimeoutStartSec in seconds (default: 60)
#   --persistent           Set Persistent=true on timer (default: false)
#   --after <unit>         Add After=chitin-<unit>.service in [Unit] (optional)
#   --output-dir <dir>     Output directory (default: infra/systemd)
#   --dry-run              Print to stdout instead of writing files

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../../.." && pwd)"

NAME=""
DESCRIPTION=""
EXEC_START=""
INTERVAL=""
CALENDAR=""
BOOT_DELAY="5min"
TIMEOUT="60"
PERSISTENT="false"
AFTER_UNIT=""
OUTPUT_DIR="$REPO_ROOT/infra/systemd"
DRY_RUN=false

usage() {
  sed -n '/^# Usage:/,/^[^#]/p' "$0" | sed 's/^# \?//'
  exit 0
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --name)        NAME="$2";                                                                  shift 2 ;;
    --description) DESCRIPTION="$2";                                                          shift 2 ;;
    --exec)        EXEC_START="$2";                                                            shift 2 ;;
    --interval)    INTERVAL="$2";                                                              shift 2 ;;
    --calendar)    CALENDAR="$2";                                                              shift 2 ;;
    --boot-delay)  BOOT_DELAY="$2";                                                            shift 2 ;;
    --timeout)     TIMEOUT="$2";                                                               shift 2 ;;
    --persistent)  PERSISTENT="true";                                                          shift   ;;
    --after)       AFTER_UNIT="$2";                                                            shift 2 ;;
    --output-dir)  OUTPUT_DIR="$2";                                                            shift 2 ;;
    --dry-run)     DRY_RUN=true;                                                               shift   ;;
    --help|-h)     usage ;;
    *) echo "error: unknown option: $1" >&2; exit 1 ;;
  esac
done

[[ -z "$NAME"        ]] && { echo "error: --name is required"                    >&2; exit 1; }
[[ -z "$DESCRIPTION" ]] && { echo "error: --description is required"             >&2; exit 1; }
[[ -z "$EXEC_START"  ]] && { echo "error: --exec is required"                    >&2; exit 1; }
[[ -z "$INTERVAL" && -z "$CALENDAR" ]] && { echo "error: --interval or --calendar is required" >&2; exit 1; }
[[ -n "$INTERVAL" && -n "$CALENDAR" ]] && { echo "error: --interval and --calendar are mutually exclusive" >&2; exit 1; }

if [[ -n "$INTERVAL" ]]; then
  SCHEDULE_LINE="OnUnitActiveSec=$INTERVAL"
  # Boot trigger is sensible for interval timers — ensures a tick
  # fires shortly after boot rather than waiting a full interval.
  BOOT_LINE="OnBootSec=$BOOT_DELAY"
else
  SCHEDULE_LINE="OnCalendar=$CALENDAR"
  # For calendar timers, an OnBootSec line would ALSO fire on every
  # boot in addition to the calendar schedule — that's almost never
  # what an operator wants when they specifically asked for a
  # calendar-based unit. Omit by default; operator can add manually
  # if they need both.
  BOOT_LINE="# (no OnBootSec — calendar timer fires per OnCalendar only)"
fi

AFTER_SECTION=""
if [[ -n "$AFTER_UNIT" ]]; then
  AFTER_SECTION="After=chitin-${AFTER_UNIT}.service"
fi

# Python substitution handles multi-line AFTER_SECTION and paths with special chars.
substitute() {
  local tmpl="$1"
  GEN_NAME="$NAME" \
  GEN_DESCRIPTION="$DESCRIPTION" \
  GEN_EXEC_START="$EXEC_START" \
  GEN_TIMEOUT="$TIMEOUT" \
  GEN_AFTER_SECTION="$AFTER_SECTION" \
  GEN_SCHEDULE_LINE="$SCHEDULE_LINE" \
  GEN_BOOT_LINE="$BOOT_LINE" \
  GEN_PERSISTENT="$PERSISTENT" \
  python3 - "$tmpl" <<'PYEOF'
import sys, os

tmpl = open(sys.argv[1]).read()

replacements = {
    '@@NAME@@':          os.environ['GEN_NAME'],
    '@@DESCRIPTION@@':   os.environ['GEN_DESCRIPTION'],
    '@@EXEC_START@@':    os.environ['GEN_EXEC_START'],
    '@@TIMEOUT@@':       os.environ['GEN_TIMEOUT'],
    '@@SCHEDULE_LINE@@': os.environ['GEN_SCHEDULE_LINE'],
    '@@BOOT_LINE@@':     os.environ['GEN_BOOT_LINE'],
    '@@PERSISTENT@@':    os.environ['GEN_PERSISTENT'],
}
for k, v in replacements.items():
    tmpl = tmpl.replace(k, v)

after = os.environ.get('GEN_AFTER_SECTION', '')
if after:
    tmpl = tmpl.replace('@@AFTER_SECTION@@\n', after + '\n')
else:
    tmpl = tmpl.replace('@@AFTER_SECTION@@\n', '')

sys.stdout.write(tmpl)
PYEOF
}

SERVICE_TMPL="$SCRIPT_DIR/service.tmpl"
TIMER_TMPL="$SCRIPT_DIR/timer.tmpl"
SERVICE_OUT="$OUTPUT_DIR/chitin-$NAME.service"
TIMER_OUT="$OUTPUT_DIR/chitin-$NAME.timer"

if [[ "$DRY_RUN" == "true" ]]; then
  echo "=== $SERVICE_OUT ==="
  substitute "$SERVICE_TMPL"
  echo ""
  echo "=== $TIMER_OUT ==="
  substitute "$TIMER_TMPL"
else
  mkdir -p "$OUTPUT_DIR"
  substitute "$SERVICE_TMPL" > "$SERVICE_OUT"
  substitute "$TIMER_TMPL"   > "$TIMER_OUT"
  echo "Generated:"
  echo "  $SERVICE_OUT"
  echo "  $TIMER_OUT"
  echo ""
  echo "Install with: bash scripts/install-systemd-units.sh"
fi
