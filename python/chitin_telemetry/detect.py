"""Pattern detection: group decisions by (rule_id, action_type, agent), rank."""
from __future__ import annotations

from collections import defaultdict
from typing import Iterable

from chitin_telemetry.models import Decision, Pattern


def detect_patterns(decisions: Iterable[Decision]) -> list[Pattern]:
    """Group denies by (rule_id, action_type, agent_id), rank by count desc.

    Invariants:
      - Allows are skipped (v1 ranks denies only).
      - Tie-breaker: count desc → alphabetic on rule_id → action_type → agent_id.
        Two runs over identical input produce identical output (I1).
      - Null/missing fields bucket as '<none>' / '<unknown>'.
    """
    buckets: dict[tuple[str, str, str], list[Decision]] = defaultdict(list)
    for d in decisions:
        if d.allowed:
            continue
        key = (
            d.rule_id or "<none>",
            d.action_type or "<none>",
            d.agent or "<unknown>",
        )
        buckets[key].append(d)

    keys_sorted = sorted(
        buckets.keys(),
        key=lambda k: (-len(buckets[k]), k[0], k[1], k[2]),
    )

    patterns: list[Pattern] = []
    for key in keys_sorted:
        bucket = buckets[key]
        bucket_sorted = sorted(bucket, key=lambda d: (d.ts, d.envelope_id or ""))
        sample_ids = tuple(
            d.envelope_id for d in bucket_sorted[:3] if d.envelope_id
        )
        patterns.append(Pattern(
            rule_id=key[0],
            action_type=key[1],
            agent_id=key[2],
            count=len(bucket),
            first_seen=bucket_sorted[0].ts,
            last_seen=bucket_sorted[-1].ts,
            decision_class="deny",
            sample_envelope_ids=sample_ids,
            decisions=tuple(bucket_sorted),
        ))
    return patterns
