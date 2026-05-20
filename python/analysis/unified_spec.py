"""Unified spec model — Python binding (spec 061).

Mirrors the canonical JSON Schema at libs/contracts/schemas/unified-spec.schema.json.
Validated against that schema in CI.
"""
from __future__ import annotations

from dataclasses import dataclass, field
from enum import Enum
from typing import Optional


class SpecStatus(str, Enum):
    DRAFT = "draft"
    RATIFIED = "ratified"
    SUPERSEDED = "superseded"


class SourceFramework(str, Enum):
    SPEC_KIT = "spec-kit"
    OPENSPEC = "openspec"
    SUPERPOWERS = "superpowers"
    HOUSE = "house"


@dataclass(frozen=True)
class Requirement:
    """A single requirement (R1, R2, …) within a spec."""

    id: str
    text: str


@dataclass(frozen=True)
class AcceptanceCriterion:
    """A single acceptance criterion (AC1, AC2, …)."""

    id: str
    text: str


@dataclass(frozen=True)
class Slice:
    """A delivery slice within the spec."""

    id: str
    scope: str
    requirement_ids: tuple[str, ...] = ()

    def __post_init__(self) -> None:
        # Allow passing a list; normalise to tuple for immutability.
        if isinstance(self.requirement_ids, list):
            object.__setattr__(self, "requirement_ids", tuple(self.requirement_ids))


@dataclass(frozen=True)
class Question:
    """An open / unresolved question within a spec."""

    id: str
    text: str
    proposed: Optional[str] = None


@dataclass(frozen=True)
class UnifiedSpec:
    """Normalized spec shape produced by all framework adapters (spec 061).

    Fields map 1:1 to the JSON Schema at
    libs/contracts/schemas/unified-spec.schema.json.
    """

    spec_id: str
    title: str
    status: SpecStatus
    source_framework: SourceFramework
    source_path: str
    requirements: tuple[Requirement, ...] = ()
    acceptance: tuple[AcceptanceCriterion, ...] = ()
    boundaries: tuple[str, ...] = ()
    slices: tuple[Slice, ...] = ()
    open_questions: tuple[Question, ...] = ()

    def __post_init__(self) -> None:
        # Normalise mutable list inputs to immutable tuples.
        for attr in (
            "requirements",
            "acceptance",
            "boundaries",
            "slices",
            "open_questions",
        ):
            val = getattr(self, attr)
            if isinstance(val, list):
                object.__setattr__(self, attr, tuple(val))

    def to_dict(self) -> dict:
        """Serialize to a JSON-compatible dict matching the schema."""
        return {
            "spec_id": self.spec_id,
            "title": self.title,
            "status": self.status.value,
            "source_framework": self.source_framework.value,
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
                {"id": q.id, "text": q.text, "proposed": q.proposed}
                if q.proposed is not None
                else {"id": q.id, "text": q.text}
                for q in self.open_questions
            ],
        }