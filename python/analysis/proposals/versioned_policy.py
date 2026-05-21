"""Append-only policy versions for approved proposal application."""
from __future__ import annotations

from copy import deepcopy
from dataclasses import dataclass
from datetime import datetime, timezone
from hashlib import sha256
import json
from types import MappingProxyType
from typing import Any, Mapping


class PolicyVersionError(ValueError):
    """Raised when a policy version operation cannot be completed."""


def _freeze_mapping(value: Mapping[str, Any]) -> Mapping[str, Any]:
    return MappingProxyType({key: _freeze_value(item) for key, item in deepcopy(dict(value)).items()})


def _freeze_value(value: Any) -> Any:
    if isinstance(value, Mapping):
        return _freeze_mapping(value)
    if isinstance(value, list | tuple):
        return tuple(_freeze_value(item) for item in value)
    return value


def _plain_value(value: Any) -> Any:
    if isinstance(value, Mapping):
        return {key: _plain_value(item) for key, item in value.items()}
    if isinstance(value, tuple):
        return [_plain_value(item) for item in value]
    return value


def _plain_mapping(value: Mapping[str, Any]) -> dict[str, Any]:
    return {key: _plain_value(item) for key, item in value.items()}


def _hash_payload(payload: Mapping[str, Any]) -> str:
    encoded = json.dumps(payload, sort_keys=True, separators=(",", ":"), default=str).encode("utf-8")
    return sha256(encoded).hexdigest()


@dataclass(frozen=True)
class PolicyVersion:
    version: int
    proposal_id: str
    previous_version_hash: str | None
    applied_diff: Mapping[str, Any]
    content: Mapping[str, Any]
    timestamp: datetime

    @property
    def hash(self) -> str:
        return _hash_payload(
            {
                "version": self.version,
                "proposal_id": self.proposal_id,
                "previous_version_hash": self.previous_version_hash,
                "applied_diff": _plain_mapping(self.applied_diff),
                "content": _plain_mapping(self.content),
                "timestamp": self.timestamp.isoformat(),
            }
        )

    def to_dict(self) -> dict[str, Any]:
        return {
            "version": self.version,
            "proposal_id": self.proposal_id,
            "previous_version_hash": self.previous_version_hash,
            "applied_diff": _plain_mapping(self.applied_diff),
            "content": _plain_mapping(self.content),
            "ts": self.timestamp.isoformat(),
        }


class VersionedPolicyLog:
    """In-memory immutable policy version log.

    Rollback is append-only: it creates a new version with content restored to
    the state before the target proposal was applied.
    """

    def __init__(self, initial_policy: Mapping[str, Any] | None = None) -> None:
        self._versions: list[PolicyVersion] = []
        if initial_policy:
            self._append(
                proposal_id="initial",
                previous_version_hash=None,
                applied_diff={"initial": True},
                content=initial_policy,
            )

    @property
    def versions(self) -> tuple[PolicyVersion, ...]:
        return tuple(self._versions)

    @property
    def latest(self) -> PolicyVersion | None:
        return self._versions[-1] if self._versions else None

    def get_version(self, version: int) -> PolicyVersion:
        for candidate in self._versions:
            if candidate.version == version:
                return candidate
        raise KeyError(f"unknown policy version: {version}")

    def apply(
        self,
        proposal_id: str,
        applied_diff: Mapping[str, Any],
        *,
        content: Mapping[str, Any] | None = None,
    ) -> PolicyVersion:
        previous = self.latest
        next_content = deepcopy(dict(previous.content)) if previous else {}
        next_content.update(dict(applied_diff))
        if content is not None:
            next_content = deepcopy(dict(content))
        return self._append(
            proposal_id=proposal_id,
            previous_version_hash=previous.hash if previous else None,
            applied_diff=applied_diff,
            content=next_content,
        )

    def rollback(self, proposal_id: str, operator_id: str, action_token: str) -> PolicyVersion:
        if not operator_id:
            raise PermissionError("operator id required")
        if not action_token:
            raise PermissionError("operator action token required")

        target_index = next(
            (index for index, version in enumerate(self._versions) if version.proposal_id == proposal_id),
            None,
        )
        if target_index is None:
            raise PolicyVersionError(f"proposal has no applied policy version: {proposal_id}")

        previous_content: Mapping[str, Any] = {}
        if target_index > 0:
            previous_content = self._versions[target_index - 1].content

        return self._append(
            proposal_id=f"rollback:{proposal_id}",
            previous_version_hash=self.latest.hash if self.latest else None,
            applied_diff={
                "rollback_of": proposal_id,
                "operator_id": operator_id,
            },
            content=previous_content,
        )

    def _append(
        self,
        *,
        proposal_id: str,
        previous_version_hash: str | None,
        applied_diff: Mapping[str, Any],
        content: Mapping[str, Any],
    ) -> PolicyVersion:
        version = PolicyVersion(
            version=len(self._versions) + 1,
            proposal_id=proposal_id,
            previous_version_hash=previous_version_hash,
            applied_diff=_freeze_mapping(applied_diff),
            content=_freeze_mapping(content),
            timestamp=datetime.now(tz=timezone.utc),
        )
        self._versions.append(version)
        return version
