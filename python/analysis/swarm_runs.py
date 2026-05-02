import os
import json
import re
from dataclasses import dataclass
from typing import List, Optional, Dict, Any
from datetime import datetime

@dataclass
class SwarmRun:
    entry_id: str
    tier: str
    driver: str
    dispatched_at: datetime
    exit_code: int
    duration_ms: int
    commits_added: int
    pr_url: Optional[str]
    cost_usd: Optional[float]
    model: Optional[str]
    bucket_b: bool

def extract_pr_url(stdout: str) -> Optional[str]:
    match = re.search(r"https://github.com/[^\s]+/pull/\d+", stdout)
    return match.group(0) if match else None

def extract_cost_usd(stdout: str) -> Optional[float]:
    match = re.search(r"cost: \$([0-9.]+)", stdout)
    return float(match.group(1)) if match else None

def extract_model(model_usage: Any) -> Optional[str]:
    if isinstance(model_usage, dict):
        return model_usage.get("model")
    if isinstance(model_usage, str):
        return model_usage
    return None

def is_bucket_b(diff: str, worktree_settings: str) -> bool:
    return diff == worktree_settings

def load_swarm_runs(state_dir: str, tmp_dir: str, window) -> List[SwarmRun]:
    # Load marker files
    markers = {}
    for fname in os.listdir(state_dir):
        if not fname.endswith(".json"): continue
        with open(os.path.join(state_dir, fname)) as f:
            marker = json.load(f)
            workflow_id = marker.get("workflow_id")
            if workflow_id:
                markers[workflow_id] = marker

    # Load envelope files
    runs = []
    for fname in os.listdir(tmp_dir):
        if not fname.startswith("result-swarm-") or not fname.endswith(".json"): continue
        with open(os.path.join(tmp_dir, fname)) as f:
            envelope = json.load(f)
            workflow_id = envelope.get("workflow_id")
            marker = markers.get(workflow_id)
            if not marker:
                continue
            dispatched_at = datetime.fromisoformat(marker["dispatched_at"]) if "dispatched_at" in marker else None
            entry_id = marker.get("entry_id", "")
            tier = marker.get("tier", "")
            driver = marker.get("driver", "")
            exit_code = envelope.get("exit_code", -1)
            duration_ms = envelope.get("duration_ms", 0)
            commits_added = envelope.get("commits_added", 0)
            stdout = envelope.get("stdout", "")
            pr_url = extract_pr_url(stdout)
            cost_usd = extract_cost_usd(stdout)
            model = extract_model(envelope.get("modelUsage"))
            diff = envelope.get("diff", "")
            worktree_settings = envelope.get("writeWorktreeClaudeSettings", "")
            bucket_b = is_bucket_b(diff, worktree_settings)
            run = SwarmRun(
                entry_id=entry_id,
                tier=tier,
                driver=driver,
                dispatched_at=dispatched_at,
                exit_code=exit_code,
                duration_ms=duration_ms,
                commits_added=commits_added,
                pr_url=pr_url,
                cost_usd=cost_usd,
                model=model,
                bucket_b=bucket_b
            )
            # Window filter
            if window is None or window.contains(dispatched_at):
                runs.append(run)
    return runs

def cost_by_driver(runs: List[SwarmRun]) -> Dict[str, float]:
    costs = {}
    for run in runs:
        if run.driver and run.cost_usd is not None:
            costs.setdefault(run.driver, 0.0)
            costs[run.driver] += run.cost_usd
    return costs

def outcomes_by_driver(runs: List[SwarmRun]) -> Dict[str, Dict[str, int]]:
    outcomes = {}
    for run in runs:
        if run.driver:
            d = outcomes.setdefault(run.driver, {"success": 0, "failure": 0})
            if run.exit_code == 0:
                d["success"] += 1
            else:
                d["failure"] += 1
    return outcomes

def bucket_b_rate(runs: List[SwarmRun]) -> float:
    if not runs:
        return 0.0
    bucket_b_count = sum(1 for run in runs if run.bucket_b)
    return bucket_b_count / len(runs)
