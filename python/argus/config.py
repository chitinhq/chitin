"""Operator-overridable configuration for Argus.

Loads `~/.argus/config.yaml` if present, with sensible defaults baked
into the code. Config is read-only; never written by Argus itself.
"""
from __future__ import annotations

import os
from dataclasses import dataclass, field
from pathlib import Path

# We deliberately avoid PyYAML — a tiny custom parser keeps Argus dep-free.
# The config file is "key: scalar" lines + nested via two-space indent.
# If operators want richer config, we can adopt PyYAML later without
# breaking back-compat.


CONFIG_PATH = Path(os.environ.get("ARGUS_CONFIG", Path.home() / ".argus" / "config.yaml"))


@dataclass
class Thresholds:
    deny_cluster_window_s: int = 300
    deny_cluster_count: int = 4
    unknown_rate_window_h: int = 24
    unknown_rate_pct: float = 1.0
    agent_failure_min: int = 3
    stuck_flow_idle_s: int = 3600


@dataclass
class KernelSettings:
    tick_interval_s: int = 60
    max_tick_wall_s: int = 300
    critical_push_rate_limit_s: int = 900  # 15 min between Discord pushes
    judge_sample_p_findings: float = 0.2
    judge_sample_p_memory: float = 1.0
    judge_sample_p_report: float = 1.0
    gpu_util_threshold_pct: float = 85.0
    # 256 MiB headroom is enough for KV-cache growth when qwen is
    # already resident in VRAM (typical: ~22GB used, ~500 MiB free).
    # Floor exists to defer when something else is competing for VRAM.
    gpu_vram_floor_mib: int = 256
    daily_llm_cap: int = int(os.environ.get("ARGUS_DAILY_LLM_CAP", "800"))


@dataclass
class DiscordSettings:
    webhook_url: str = ""
    daily_quiet_skip: bool = True


@dataclass
class PolicySettings:
    # Any policy proposal whose rule_id matches this regex AND whose kind
    # is 'relax' is auto-flagged critical and held for explicit operator ack.
    critical_rule_regex: str = r"^(auth|exec|network|gov|sandbox|lockdown)"


@dataclass
class ArgusConfig:
    thresholds: Thresholds = field(default_factory=Thresholds)
    kernel: KernelSettings = field(default_factory=KernelSettings)
    discord: DiscordSettings = field(default_factory=DiscordSettings)
    policy: PolicySettings = field(default_factory=PolicySettings)


def _coerce_value(raw: str) -> object:
    s = raw.strip()
    if s.lower() in ("true", "yes", "on"):
        return True
    if s.lower() in ("false", "no", "off"):
        return False
    if s.lower() in ("null", "~", "none", ""):
        return None
    try:
        return int(s)
    except ValueError:
        pass
    try:
        return float(s)
    except ValueError:
        pass
    if (s.startswith("'") and s.endswith("'")) or (s.startswith('"') and s.endswith('"')):
        return s[1:-1]
    return s


def _parse_minimal_yaml(text: str) -> dict:
    """Parse a strict subset of YAML: top-level keys with two-space nesting."""
    out: dict = {}
    stack: list[tuple[int, dict]] = [(-1, out)]
    for raw_line in text.splitlines():
        # Strip comments after a #, but only if the # is not inside a quoted string.
        line = raw_line.rstrip()
        if not line.strip() or line.lstrip().startswith("#"):
            continue
        indent = len(line) - len(line.lstrip())
        # Pop back to the parent matching this indent
        while stack and indent <= stack[-1][0]:
            stack.pop()
        if not stack:
            stack = [(-1, out)]
        parent_indent, parent = stack[-1]
        # Strip inline comments
        if " #" in line:
            line = line.split(" #", 1)[0]
        body = line.strip()
        if ":" not in body:
            continue
        key, _, value = body.partition(":")
        key = key.strip()
        value = value.strip()
        if value == "":
            child: dict = {}
            parent[key] = child
            stack.append((indent, child))
        else:
            parent[key] = _coerce_value(value)
    return out


def load(path: Path = CONFIG_PATH) -> ArgusConfig:
    """Load config from `path` if it exists, merging onto defaults."""
    cfg = ArgusConfig()
    if not path.exists():
        return cfg

    try:
        raw = path.read_text()
    except OSError:
        return cfg

    data = _parse_minimal_yaml(raw)

    def _apply(section_name: str, target):
        section = data.get(section_name)
        if not isinstance(section, dict):
            return
        for k, v in section.items():
            if hasattr(target, k) and v is not None:
                setattr(target, k, v)

    _apply("thresholds", cfg.thresholds)
    _apply("kernel", cfg.kernel)
    _apply("discord", cfg.discord)
    _apply("policy", cfg.policy)

    # Env overrides for Discord
    env_webhook = os.environ.get("ARGUS_DISCORD_WEBHOOK")
    if env_webhook:
        cfg.discord.webhook_url = env_webhook

    return cfg
