"""`python -m analysis` — guide users to the right submodule."""
import sys

print(
    "Use one of:\n"
    "  python -m analysis.decisions   # repeated-deny pattern detection\n"
    "  python -m analysis.predict     # chain-predict-outcome model (train + predict)\n"
    "  python -m analysis.debt        # debt-ledger draft\n"
    "  python -m analysis.souls       # soul-routing decisions",
    file=sys.stderr,
)
sys.exit(2)
