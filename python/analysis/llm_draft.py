"""Layer 2 — LLM-drafted rules via local ollama. Opt-in, fail-safe.

Any failure (network, parse, timeout) falls back to the heuristic draft, marked
kind='heuristic-fallback'. The LLM is never load-bearing for the demo.
"""
from __future__ import annotations

import json
import urllib.request
from typing import Iterable, Optional

from analysis.models import Pattern, PredictedImpact, RuleDraft

OLLAMA_URL = "http://localhost:11434/api/generate"
OLLAMA_MODEL = "qwen3-coder"
TIMEOUT_SECONDS = 30.0


def _build_prompt(pattern: Pattern, heuristic: RuleDraft) -> str:
    samples = "\n".join(
        f"  - {d.action_target!r} (envelope {d.envelope_id})"
        for d in pattern.decisions[:5]
    )
    return (
        f"You are drafting a chitin governance rule. Return ONLY YAML, no prose.\n"
        f"\n"
        f"Observed pattern: rule_id={pattern.rule_id}, "
        f"action_type={pattern.action_type}, agent={pattern.agent_id}, "
        f"count={pattern.count}.\n"
        f"\n"
        f"Sample denied actions:\n{samples}\n"
        f"\n"
        f"Heuristic draft (improve on this if you can):\n"
        f"{heuristic.rule_yaml}\n"
        f"\n"
        f"Reply with a single YAML block proposing a refinement. Be conservative."
    )


def _call_ollama(prompt: str) -> str:
    req = urllib.request.Request(
        OLLAMA_URL,
        data=json.dumps({
            "model": OLLAMA_MODEL,
            "prompt": prompt,
            "stream": False,
        }).encode("utf-8"),
        headers={"Content-Type": "application/json"},
    )
    with urllib.request.urlopen(req, timeout=TIMEOUT_SECONDS) as resp:
        body = json.loads(resp.read().decode("utf-8"))
    return body.get("response", "")


def enrich_with_llm(
    pairs: Iterable[tuple[Pattern, Optional[RuleDraft]]],
) -> list[RuleDraft]:
    """For each (pattern, heuristic_draft) pair, attempt LLM enrichment.

    Pairs with None heuristic are skipped. Pairs with a heuristic always
    produce a draft — either kind='llm' on success or kind='heuristic-fallback'
    on any failure.
    """
    out: list[RuleDraft] = []
    for pattern, heuristic in pairs:
        if heuristic is None:
            continue
        try:
            prompt = _build_prompt(pattern, heuristic)
            response = _call_ollama(prompt)
            if not response.strip():
                raise RuntimeError("empty LLM response")
            inherited = PredictedImpact(
                samples_evaluated=heuristic.predicted_impact.samples_evaluated,
                would_allow=heuristic.predicted_impact.would_allow,
                would_still_deny=heuristic.predicted_impact.would_still_deny,
                method=f"{heuristic.predicted_impact.method} (inherited; LLM yaml not re-evaluated)",
            )
            out.append(RuleDraft(
                kind="llm",
                template=heuristic.template,
                confidence="medium",
                rule_yaml=response.strip() + "\n",
                predicted_impact=inherited,
                notes=f"LLM-enriched (ollama {OLLAMA_MODEL}). Heuristic was: {heuristic.template}",
            ))
        except Exception as e:
            out.append(RuleDraft(
                kind="heuristic-fallback",
                template=heuristic.template,
                confidence=heuristic.confidence,
                rule_yaml=heuristic.rule_yaml,
                predicted_impact=heuristic.predicted_impact,
                notes=f"LLM enrichment failed ({e}); fell back to heuristic.",
            ))
    return out
