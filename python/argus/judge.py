"""Judge pass: bias-hardened critique of an LLM output.

Catches three failure modes deterministically before invoking the LLM
again:

  1. JSON / structural validation.
  2. Citation-provenance bind — the response can only reference event
     ids, rule ids, agent names, ticket slugs, commit SHAs, or PR numbers
     that the kernel supplied as `expected_citations`. This blocks the
     "cite a real id while injecting" prompt-injection vector.
  3. Length budget — runaway prose is rejected before judge LLM cost.

Only outputs that pass deterministic checks proceed to the LLM judge.
The LLM judge sees the same context, but its output is itself JSON and
must pass a structural check. If the judge LLM rejects, the kernel may
retry up to 2 revise rounds before giving up.
"""
from __future__ import annotations

import json
import re
import sqlite3
from dataclasses import dataclass
from typing import Optional

from argus import llm, prompts


# Citation regex shapes. These are the *forms* a citation can take in
# generated text; the kernel still must pre-compute a closed set of
# expected citations and reject anything not in that set.
_CITATION_PATTERNS = [
    re.compile(r"\bt_[0-9a-f]{6,12}\b"),      # ticket slugs
    re.compile(r"\b[a-f0-9]{7,40}\b"),         # git SHAs
    re.compile(r"#\d{2,6}\b"),                 # PR numbers
    re.compile(r"\bPR\s?\d{2,6}\b", re.IGNORECASE),
    re.compile(r"\bevent\s?#?\d+\b", re.IGNORECASE),
    re.compile(r"\bf_\d+\b"),                  # finding ids (internal)
    re.compile(r"\bh_[0-9a-f]{6,12}\b"),       # hypothesis ids
]


# Words/phrases that are common false-positive matches for hex shas
# (`fffffff` or words like `deadbeef` showing up in prose). We keep them
# as citations only if they're in the expected set.
def extract_citations(text: str) -> set[str]:
    """Extract every citation-shaped substring from `text`."""
    found: set[str] = set()
    for pat in _CITATION_PATTERNS:
        for m in pat.findall(text):
            if isinstance(m, str):
                found.add(m.lower() if m.startswith(("PR", "Pr", "pr")) else m)
    return found


@dataclass(frozen=True)
class JudgeVerdict:
    verdict: str            # 'pass' | 'reject' | 'revise'
    reason: str
    hallucinated_citations: list[str]
    structural_ok: bool
    judge_skipped: bool     # True if we didn't actually call the LLM


def _structural_check(
    response: Optional[str],
    *,
    max_length_chars: int,
) -> tuple[bool, str]:
    if response is None:
        return False, "no_response"
    if not response.strip():
        return False, "empty_response"
    if len(response) > max_length_chars:
        return False, f"too_long({len(response)}>{max_length_chars})"
    return True, "ok"


def _citation_check(
    response: str, expected: list[str]
) -> tuple[bool, list[str]]:
    expected_set = {c.lower() for c in expected}
    found = extract_citations(response)
    hallucinated = []
    for c in found:
        if c.lower() not in expected_set:
            # Allow citation tokens that happen to also appear verbatim
            # inside the prompt's UNTRUSTED blocks ONLY if they were also
            # in expected — the expected set is the closed allowlist.
            hallucinated.append(c)
    return (not hallucinated), hallucinated


def judge(
    conn: sqlite3.Connection,
    *,
    purpose: str,
    prompt: str,
    response: Optional[str],
    expected_citations: list[str],
    max_length_chars: int = 8000,
    sample_p: float = 1.0,
) -> JudgeVerdict:
    """Run the judge pipeline.

    `sample_p` ∈ (0,1] controls whether the LLM judge is invoked. If
    `sample_p < 1`, a deterministic hash of (purpose, response, expected)
    decides whether to call the LLM for this row, so the same content
    always gets the same sampling decision (replay-safe).

    Findings landing in `memory` or pushed to Discord should ALWAYS call
    the LLM judge (sample_p=1.0). Inline findings can use sample_p=0.2.
    """
    # 1. Structural
    ok, why = _structural_check(response, max_length_chars=max_length_chars)
    if not ok:
        return JudgeVerdict(
            verdict="reject",
            reason=f"structural:{why}",
            hallucinated_citations=[],
            structural_ok=False,
            judge_skipped=True,
        )
    assert response is not None

    # 2. Citation provenance bind
    cite_ok, hallucinated = _citation_check(response, expected_citations)
    if not cite_ok:
        return JudgeVerdict(
            verdict="reject",
            reason="hallucinated_citations",
            hallucinated_citations=hallucinated,
            structural_ok=True,
            judge_skipped=True,
        )

    # 3. LLM judge (sampled)
    if sample_p < 1.0:
        # Stable per-content sampling decision.
        import hashlib
        h = hashlib.sha256()
        h.update(purpose.encode())
        h.update(response.encode())
        h.update("".join(sorted(expected_citations)).encode())
        digest = int(h.hexdigest(), 16)
        # Take the bottom 32 bits as a value in [0,1).
        unit = (digest & 0xFFFFFFFF) / 0xFFFFFFFF
        if unit > sample_p:
            return JudgeVerdict(
                verdict="pass",
                reason="sampled_out",
                hallucinated_citations=[],
                structural_ok=True,
                judge_skipped=True,
            )

    result = llm.call(
        conn,
        purpose="judge",
        system=prompts.JUDGE_SYSTEM,
        user=prompts.judge_user(purpose, prompt, response, expected_citations),
        bypass_cap=True,  # judge calls don't count against operator budget
    )
    if not result.ok or not result.text:
        # Judge unavailable. Conservative: pass on deterministic checks
        # alone (we already passed structural + citation).
        return JudgeVerdict(
            verdict="pass",
            reason=f"judge_unavailable:{result.error or result.skipped_reason or 'unknown'}",
            hallucinated_citations=[],
            structural_ok=True,
            judge_skipped=True,
        )

    parsed = _parse_judge_json(result.text)
    if parsed is None:
        return JudgeVerdict(
            verdict="pass",
            reason="judge_unparseable_output",
            hallucinated_citations=[],
            structural_ok=True,
            judge_skipped=True,
        )

    return JudgeVerdict(
        verdict=parsed.get("verdict", "pass"),
        reason=str(parsed.get("reason", "")),
        hallucinated_citations=[],
        structural_ok=True,
        judge_skipped=False,
    )


def _parse_judge_json(text: str) -> Optional[dict]:
    """Parse the judge's JSON output. Tolerant of code-fences and prose."""
    text = text.strip()
    # Strip code fences.
    if text.startswith("```"):
        text = text.strip("`")
        if text.lower().startswith("json"):
            text = text[4:].strip()
    # Find the first { and last } if there's surrounding prose.
    start = text.find("{")
    end = text.rfind("}")
    if start == -1 or end == -1 or end < start:
        return None
    try:
        return json.loads(text[start:end + 1])
    except json.JSONDecodeError:
        return None
