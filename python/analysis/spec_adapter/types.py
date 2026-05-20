"""Python dataclasses matching the UnifiedSpec schema (spec 061 R1).

Uses dataclasses (not pydantic) to keep deps minimal per bucket-2 convention.
"""
from __future__ import annotations

from dataclasses import dataclass, field
from typing import List, Optional


@dataclass(frozen=True)
class Requirement:
    """A single requirement (R1, R2, ...)."""
    id: str
    text: str


@dataclass(frozen=True)
class AcceptanceCriterion:
    """A single acceptance criterion (AC1, AC2, ...)."""
    id: str
    text: str


@dataclass(frozen=True)
class Slice:
    """An implementation slice."""
    id: str
    scope: str
    requirement_ids: List[str] = field(default_factory=list)


@dataclass(frozen=True)
class Question:
    """An open question with optional proposed resolution."""
    id: str
    text: str
    proposed: Optional[str] = None


@dataclass(frozen=True)
class UnifiedSpec:
    """The normalized spec model — the only shape L2–L7 consume.

    Every framework adapter's ``parse()`` produces one of these.
    Fields match the JSON Schema at libs/contracts/src/unified-spec.schema.json.
    """
    spec_id: str
    title: str
    status: str                    # draft | ratified | superseded
    source_framework: str           # spec-kit | openspec | superpowers | house
    source_path: str
    requirements: List[Requirement] = field(default_factory=list)
    acceptance: List[AcceptanceCriterion] = field(default_factory=list)
    boundaries: List[str] = field(default_factory=list)
    slices: List[Slice] = field(default_factory=list)
    open_questions: List[Question] = field(default_factory=list)

    def to_dict(self) -> dict:
        """Serialize to a JSON-compatible dict matching the schema."""
        return {
            "spec_id": self.spec_id,
            "title": self.title,
            "status": self.status,
            "source_framework": self.source_framework,
            "source_path": self.source_path,
            "requirements": [{"id": r.id, "text": r.text} for r in self.requirements],
            "acceptance": [{"id": a.id, "text": a.text} for a in self.acceptance],
            "boundaries": list(self.boundaries),
            "slices": [
                {
                    "id": s.id,
                    "scope": s.scope,
                    "requirement_ids": list(s.requirement_ids),
                }
                for s in self.slices
            ],
            "open_questions": [
                {
                    "id": q.id,
                    "text": q.text,
                    "proposed": q.proposed,
                }
                for q in self.open_questions
            ],
        }