"""`python -m analysis` — guide users to the right submodule."""
import sys

print("Use: python -m analysis.decisions", file=sys.stderr)
sys.exit(2)
