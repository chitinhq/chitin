"""Tests that all v1 templates self-register on import."""
from analysis.templates import REGISTRY
import analysis.templates.all  # noqa: F401


def test_all_v1_templates_registered():
    expected = {
        "no-destructive-rm",
        "bounds:max_files_changed",
        "no-curl-pipe-bash",
        "no-force-push",
    }
    assert expected.issubset(REGISTRY.keys())
