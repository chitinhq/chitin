"""Template for `no-curl-pipe-bash`.

Heuristic: extract URL host from the curl command. If the host is on a
known-trusted list, propose host-allowlist exemption.
"""
from __future__ import annotations

import re
from typing import Optional

from chitin_telemetry.templates import register
from chitin_telemetry.models import Pattern, PredictedImpact, RuleDraft

# MUST stay in sync with the regex in `draft()` below. predicted_impact uses
# this set, so any host added here must also appear in the emitted regex.
TRUSTED_HOSTS = frozenset({
    "get.deno.land",
    "sh.rustup.rs",
    "install.python-poetry.org",
})

URL_RE = re.compile(r"https?://([^/\s|]+)")


def extract_host(target: str) -> Optional[str]:
    if not target:
        return None
    m = URL_RE.search(target)
    return m.group(1) if m else None


def draft(pattern: Pattern) -> Optional[RuleDraft]:
    if not pattern.decisions:
        return None

    hosts = [extract_host(d.action_target or "") for d in pattern.decisions]
    trusted = sum(1 for h in hosts if h and h in TRUSTED_HOSTS)
    if trusted == 0:
        return None

    rule_yaml = (
        "# Insert ABOVE the existing no-curl-pipe-bash rule in chitin.yaml.\n"
        "- id: trusted-curl-hosts\n"
        "  action: shell.exec\n"
        "  effect: allow\n"
        "  target_regex: 'curl[^|]*https?://(get\\.deno\\.land|sh\\.rustup\\.rs|install\\.python-poetry\\.org)/'\n"
        "  reason: 'Official installer from trusted host (analysis-suggested)'\n"
    )
    impact = PredictedImpact(
        samples_evaluated=pattern.count,
        would_allow=trusted,
        would_still_deny=pattern.count - trusted,
        method="host-extracted-from-url-allowlist",
    )
    return RuleDraft(
        kind="heuristic",
        template="no_curl_pipe_bash",
        confidence="medium",
        rule_yaml=rule_yaml,
        predicted_impact=impact,
        notes="Trusted-host list is conservative; extend with reviewer approval.",
    )


register("no-curl-pipe-bash", draft)
