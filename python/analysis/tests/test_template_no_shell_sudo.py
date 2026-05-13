"""Tests for the no-shell-sudo research template."""
from datetime import datetime, timezone

from analysis.models import Decision, Pattern
from analysis.templates.no_shell_sudo import draft


def _pattern_with_targets(*targets):
    decisions = tuple(
        Decision(
            ts=datetime(2026, 4, 25, 8, i, tzinfo=timezone.utc),
            allowed=False,
            rule_id="no-shell-sudo",
            action_type="shell.exec",
            action_target=t,
            agent="copilot-cli",
            envelope_id=f"e{i}",
        )
        for i, t in enumerate(targets)
    )
    return Pattern(
        rule_id="no-shell-sudo",
        action_type="shell.exec",
        agent_id="copilot-cli",
        count=len(targets),
        first_seen=decisions[0].ts,
        last_seen=decisions[-1].ts,
        decision_class="deny",
        sample_envelope_ids=tuple(d.envelope_id for d in decisions[:3]),
        decisions=decisions,
    )


def test_package_installs_emit_research_prompt_not_allow_rule():
    p = _pattern_with_targets(
        "sudo apt-get install jq",
        "sudo yum install sqlite",
        "sudo dnf install ripgrep",
        "sudo apk add bash",
    )

    d = draft(p)

    assert d is not None
    assert d.kind == "research-prompt"
    assert d.template == "no_shell_sudo"
    assert d.rule_yaml == ""
    assert "auto-allow" in d.notes
    assert d.predicted_impact.samples_evaluated == 4
    assert d.predicted_impact.would_allow == 0
    assert d.predicted_impact.would_still_deny == 4


def test_non_package_sudo_returns_none():
    p = _pattern_with_targets("sudo systemctl restart docker")

    assert draft(p) is None
