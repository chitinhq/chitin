#!/usr/bin/env bash
# Spec 023 R3: install the agent-bus inbound poll into hermes cron.
# Idempotent — running twice produces identical state.
#
# spec: 023-agent-bus-bidirectional-liveness

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

set -euo pipefail

JOBS_FILE="${HOME}/.hermes/cron/jobs.json"
JOB_NAME="agent-bus-inbound-poll"
JOB_ID="agbus-inb-poll"

if [[ ! -f "$JOBS_FILE" ]]; then
    echo "ERROR: $JOBS_FILE does not exist — hermes cron not initialized" >&2
    exit 1
fi

# Spec 023: poll every 60 seconds. Latency budget AC2 allows up to 90s
# (60s cadence + 30s API slack); cadence shorter than budget gives
# headroom for slow Discord API responses.
JOB_JSON=$(cat <<EOF
{
  "id": "${JOB_ID}",
  "name": "${JOB_NAME}",
  "prompt": "Spec 023 R2: poll every Discord-mirrored thread for new inbound messages. See services/agent-bus/discord_mirror.py poll-all.",
  "skills": [],
  "skill": null,
  "model": null,
  "provider": null,
  "base_url": null,
  "script": "agent-bus-inbound-poll.sh",
  "no_agent": true,
  "context_from": null,
  "schedule": {
    "kind": "interval",
    "minutes": 1
  },
  "active": true,
  "deliver": null
}
EOF
)

# Replace any existing job with the same id; otherwise append. Atomic
# write via temp + mv so a partial write doesn't corrupt jobs.json.
python3 - "$JOBS_FILE" "$JOB_ID" "$JOB_JSON" <<'PY'
import json, os, sys, tempfile
jobs_path, job_id, job_json = sys.argv[1], sys.argv[2], sys.argv[3]
state = json.loads(open(jobs_path).read())
new_job = json.loads(job_json)
jobs = state.get("jobs", [])
jobs = [j for j in jobs if j.get("id") != job_id]
jobs.append(new_job)
state["jobs"] = jobs
fd, tmp = tempfile.mkstemp(dir=os.path.dirname(jobs_path), prefix=".jobs.", suffix=".tmp")
try:
    with os.fdopen(fd, "w") as f:
        json.dump(state, f, indent=2)
        f.write("\n")
    os.replace(tmp, jobs_path)
except Exception:
    try: os.unlink(tmp)
    except OSError: pass
    raise
print(f"[install-agent-bus-cron] installed job {job_id} ({len(jobs)} total)")
PY

# Install the wrapper script that the cron entry calls.
WRAPPER="${HOME}/.hermes/scripts/agent-bus-inbound-poll.sh"
mkdir -p "$(dirname "$WRAPPER")"
cat > "$WRAPPER" <<WRAPPER_EOF
#!/usr/bin/env bash
# Cron entry point for the agent-bus inbound poll.
# Spec 023 R2: read every Discord-mirrored thread.
set -eo pipefail
set -a
. "\${HOME}/.hermes/.env"
set +a
exec python3 "${SCRIPT_DIR}/../services/agent-bus/discord_mirror.py" poll-all
WRAPPER_EOF
chmod +x "$WRAPPER"

echo "[install-agent-bus-cron] wrapper installed: $WRAPPER"
echo "[install-agent-bus-cron] verify with: hermes cron list | grep ${JOB_NAME}"
