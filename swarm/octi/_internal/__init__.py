"""Internal helpers for ``swarm.octi``.

Nothing here is importable from outside the ``swarm.octi`` package; the
CI grep in ``swarm/tests/test_octi_import_boundary.py`` does not check
this directly, but the operator-facing contract is: only ``Controller``,
``Verifier``, ``run_verify``, and their dataclasses are public.
"""
