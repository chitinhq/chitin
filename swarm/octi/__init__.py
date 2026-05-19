"""Octi — deterministic workflow governance for swarm operations.

Slice 2 surface:

- ``Controller`` — outer loop that drives a Mini session: stall detection,
  nudge on stale ``status.json``, independent verifier execution on
  ``state=done`` claims.
- ``Verifier`` / ``run_verify`` — runs the ``verify`` command from
  ``status.json`` in a subprocess with a bounded timeout.

Octi imports only ``MiniSession`` from ``swarm.mini``. The grep regression
test in ``swarm/tests/test_octi_import_boundary.py`` enforces this.
"""

from .controller import (
    Controller,
    ControllerConfig,
    TickOutcome,
)
from .verifier import (
    VerifyResult,
    VerifyVerdict,
    run_verify,
)

__all__ = [
    "Controller",
    "ControllerConfig",
    "TickOutcome",
    "VerifyResult",
    "VerifyVerdict",
    "run_verify",
]
