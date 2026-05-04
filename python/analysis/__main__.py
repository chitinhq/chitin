"""`python -m analysis` — guide users to the right submodule."""
import sys

print(
    "Use one of:\n"
    "  python -m analysis.decisions   # repeated-deny pattern detection\n"
    "  python -m analysis.predict     # chain-predict-outcome model (train + predict)\n"
    "  python -m analysis.debt        # debt-ledger draft\n"
    "  python -m analysis.souls       # soul-routing decisions\n"
    "  python -m analysis.skill_mine  # workflow n-gram surface from chain telemetry\n"
    "  python -m analysis.codex_mine  # codex session ingest + quota usage\n"
    "  python -m analysis.fingerprint_outcomes  # fingerprint × PR/review outcomes (P3)\n"
    "  python -m analysis.routing_elo  # ELO leaderboard per (role, task_class) — P4",
    file=sys.stderr,
)
sys.exit(2)
