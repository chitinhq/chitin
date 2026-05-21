"""Predict whether a future action will be denied, given chain history.

Trains on `gov-decisions-*.jsonl` rows: features = (action_type,
agent, hour-of-day); label = `not allowed` (deny / interrupt). Uses
a stdlib-only logistic regression — no numpy / sklearn dep. Intended
for the kernel's advisor.when=predicted_failure_above_threshold gate.

Why no numpy: the analysis layer already ships across machines (CI,
operator boxes) and adding a heavy dep for a sub-second classifier
on hundreds of rows is a bad trade. The math here is one numpy line
expanded to ~30 stdlib lines; readability stays.

Invariants (see SPEC.md):
    I3  No network. Pure function of input rows.
    I5  Bad input never aborts a run — handled in loaders, predict is
        called only with well-formed Decision rows.
    I8  1-year lookback window in `_cli_train` uses timedelta(days=365),
        not now.replace(year=year-1) (leap-day crash). Half-open window.

Boundaries:
    - Empty training set → degenerate model; `predict()` returns base_rate
      (0.0). `n_samples == 0` is the caller-visible signal.
    - Unknown action_type / agent at predict time → "<unk>" column. Never
      raises KeyError.
    - n_samples < 50 OR |base_rate - 0.5| < 0.05 → `insufficient_signal: true`
      at the CLI layer. Callers gate decisions on that flag, not on raw probability.
    - Hour default at predict time uses UTC (training timestamps are UTC);
      a local-time default on non-UTC hosts would bucket actions into a
      different time-of-day feature than training saw.
    - L2 regularization is applied to every weight including bias. Acceptable
      for the data scale; if bias-shrinkage matters at higher n, exempt the
      last column explicitly.

Public API:
    extract_features(decisions) -> X, y, vocab
    train(X, y, vocab, ...) -> Model         # vocab is a positional
    predict(model, action_type, agent, hour) -> float

CLI:
    python -m analysis.predict train --decisions-dir=$HOME/.chitin
    python -m analysis.predict predict \\
        --action-type=shell.exec --agent=claude-code --hour=14
"""
from __future__ import annotations

import argparse
import json
import math
import sys
from dataclasses import dataclass, field
from datetime import datetime, timedelta, timezone
from pathlib import Path
from typing import Iterable

from chitin_telemetry.loaders import Window, load_gov_decisions
from chitin_telemetry.models import Decision


# ──────────────────────────────────────────────────────────────────
# Feature engineering
# ──────────────────────────────────────────────────────────────────

@dataclass(frozen=True)
class Vocab:
    """Categorical -> column index. Built from training data; reused
    at prediction time so unseen categories map to the dedicated
    "<unk>" column instead of crashing."""
    action_types: dict[str, int]
    agents: dict[str, int]

    @property
    def n_features(self) -> int:
        # action_type one-hot + agent one-hot + hour-bucket (4) + bias
        return len(self.action_types) + len(self.agents) + 4 + 1


def _build_vocab(decisions: Iterable[Decision]) -> Vocab:
    at_set: set[str] = {"<unk>"}
    ag_set: set[str] = {"<unk>"}
    for d in decisions:
        if d.action_type:
            at_set.add(d.action_type)
        if d.agent:
            ag_set.add(d.agent)
    action_types = {a: i for i, a in enumerate(sorted(at_set))}
    agents = {a: i for i, a in enumerate(sorted(ag_set))}
    return Vocab(action_types=action_types, agents=agents)


def _hour_bucket(hour: int) -> int:
    # Crude diurnal bucket: morning/afternoon/evening/night. The
    # signal is whether some action_types skew unsafe at certain
    # hours (e.g., 3am git pushes from a flaky agent). Tighter
    # buckets without more data just memorize noise.
    if 6 <= hour < 12:
        return 0  # morning
    if 12 <= hour < 18:
        return 1  # afternoon
    if 18 <= hour < 22:
        return 2  # evening
    return 3      # night


def _featurize_row(
    vocab: Vocab,
    action_type: str | None,
    agent: str | None,
    hour: int,
) -> list[float]:
    n = vocab.n_features
    x = [0.0] * n

    at = action_type or "<unk>"
    at_idx = vocab.action_types.get(at, vocab.action_types["<unk>"])
    x[at_idx] = 1.0

    ag = agent or "<unk>"
    ag_offset = len(vocab.action_types)
    ag_idx = vocab.agents.get(ag, vocab.agents["<unk>"])
    x[ag_offset + ag_idx] = 1.0

    hour_offset = ag_offset + len(vocab.agents)
    x[hour_offset + _hour_bucket(hour)] = 1.0

    # Bias term — last column.
    x[-1] = 1.0
    return x


def extract_features(
    decisions: list[Decision],
) -> tuple[list[list[float]], list[int], Vocab]:
    """Returns (X, y, vocab). y[i] = 1 if decision was DENIED (the
    failure label). Empty inputs return empty arrays + a vocab
    containing only "<unk>" so prediction still runs."""
    vocab = _build_vocab(decisions)
    X: list[list[float]] = []
    y: list[int] = []
    for d in decisions:
        X.append(_featurize_row(vocab, d.action_type, d.agent, d.ts.hour))
        y.append(0 if d.allowed else 1)
    return X, y, vocab


# ──────────────────────────────────────────────────────────────────
# Logistic regression (stdlib)
# ──────────────────────────────────────────────────────────────────

@dataclass
class Model:
    """Trained logistic regression weights + the vocab they require."""
    weights: list[float]
    vocab: Vocab
    n_samples: int
    iterations: int
    final_loss: float
    # Class balance — useful for the "insufficient signal" hint.
    base_rate: float = field(default=0.0)


def _sigmoid(z: float) -> float:
    if z >= 0:
        ez = math.exp(-z)
        return 1.0 / (1.0 + ez)
    ez = math.exp(z)
    return ez / (1.0 + ez)


def _dot(w: list[float], x: list[float]) -> float:
    return sum(wi * xi for wi, xi in zip(w, x))


def train(
    X: list[list[float]],
    y: list[int],
    vocab: Vocab,
    *,
    learning_rate: float = 0.05,
    iterations: int = 200,
    l2: float = 0.01,
) -> Model:
    """Stdlib batch-gradient-descent logistic regression with L2.

    Empty input → returns a degenerate model whose predict() returns
    the base rate (0.0). Caller gets a usable predictor; the
    insufficient_signal flag is set via base_rate proximity to 0.5.
    """
    n_features = vocab.n_features
    weights = [0.0] * n_features

    if not X:
        return Model(weights=weights, vocab=vocab, n_samples=0, iterations=0, final_loss=0.0)

    n = len(X)
    base_rate = sum(y) / n

    final_loss = 0.0
    for it in range(iterations):
        grad = [0.0] * n_features
        loss = 0.0
        for xi, yi in zip(X, y):
            z = _dot(weights, xi)
            p = _sigmoid(z)
            err = p - yi
            for j, x_val in enumerate(xi):
                grad[j] += err * x_val
            # binary cross-entropy
            eps = 1e-12
            loss += -(yi * math.log(p + eps) + (1 - yi) * math.log(1 - p + eps))

        for j in range(n_features):
            grad[j] = grad[j] / n + l2 * weights[j]
            weights[j] -= learning_rate * grad[j]

        final_loss = loss / n

    return Model(
        weights=weights,
        vocab=vocab,
        n_samples=n,
        iterations=iterations,
        final_loss=final_loss,
        base_rate=base_rate,
    )


def predict(model: Model, action_type: str | None, agent: str | None, hour: int) -> float:
    """Returns P(deny) ∈ [0, 1] for the given (action_type, agent, hour)."""
    if model.n_samples == 0:
        # No training data — fall back to base rate (which is 0.0
        # when no rows seen). Caller treats predict==0 as "no signal".
        return model.base_rate
    x = _featurize_row(model.vocab, action_type, agent, hour)
    return _sigmoid(_dot(model.weights, x))


# ──────────────────────────────────────────────────────────────────
# Persistence
# ──────────────────────────────────────────────────────────────────

def to_dict(model: Model) -> dict:
    return {
        "weights": model.weights,
        "vocab": {
            "action_types": model.vocab.action_types,
            "agents": model.vocab.agents,
        },
        "n_samples": model.n_samples,
        "iterations": model.iterations,
        "final_loss": model.final_loss,
        "base_rate": model.base_rate,
    }


def from_dict(d: dict) -> Model:
    vocab = Vocab(
        action_types=dict(d["vocab"]["action_types"]),
        agents=dict(d["vocab"]["agents"]),
    )
    return Model(
        weights=list(d["weights"]),
        vocab=vocab,
        n_samples=int(d["n_samples"]),
        iterations=int(d["iterations"]),
        final_loss=float(d["final_loss"]),
        base_rate=float(d.get("base_rate", 0.0)),
    )


# ──────────────────────────────────────────────────────────────────
# CLI
# ──────────────────────────────────────────────────────────────────

def _cli_train(args: argparse.Namespace) -> int:
    decisions_dir = Path(args.decisions_dir).expanduser()
    now = datetime.now(timezone.utc)
    # 1-year lookback. timedelta(days=365) handles Feb 29 cleanly;
    # now.replace(year=now.year - 1) crashes on leap day because
    # the prior year has no Feb 29.
    window = Window(
        since=now - timedelta(days=365),
        until=now,
    )
    result = load_gov_decisions(decisions_dir, window)
    if not result.decisions:
        sys.stderr.write(f"no decisions found in {decisions_dir}\n")
        return 2
    X, y, vocab = extract_features(result.decisions)
    model = train(X, y, vocab, iterations=args.iterations, learning_rate=args.lr, l2=args.l2)
    out_path = Path(args.out).expanduser()
    out_path.parent.mkdir(parents=True, exist_ok=True)
    out_path.write_text(json.dumps(to_dict(model), indent=2))
    summary = {
        "trained_on": result.files_read,
        "samples": model.n_samples,
        "deny_rate": model.base_rate,
        "final_loss": model.final_loss,
        "out": str(out_path),
    }
    print(json.dumps(summary, indent=2))
    return 0


def _cli_predict(args: argparse.Namespace) -> int:
    model_path = Path(args.model).expanduser()
    if not model_path.exists():
        sys.stderr.write(f"model not found: {model_path}\n")
        return 2
    model = from_dict(json.loads(model_path.read_text()))
    p_deny = predict(model, args.action_type, args.agent, args.hour)
    insufficient = (
        model.n_samples < 50
        or abs(model.base_rate - 0.5) < 0.05
    )
    out = {
        "action_type": args.action_type,
        "agent": args.agent,
        "hour": args.hour,
        "predicted_deny_probability": p_deny,
        "model_n_samples": model.n_samples,
        "insufficient_signal": insufficient,
        "base_rate": model.base_rate,
    }
    print(json.dumps(out, indent=2))
    return 0


def main(argv: list[str] | None = None) -> int:
    p = argparse.ArgumentParser(prog="analysis predict", description=__doc__.split("\n")[0])
    sub = p.add_subparsers(dest="cmd", required=True)

    pt = sub.add_parser("train", help="train + persist a model from gov-decisions JSONL")
    # gov-decisions-*.jsonl files live at the top of ~/.chitin (alongside
    # events-*.jsonl), not in a sub-directory. The default below matches
    # the kernel's actual layout.
    pt.add_argument("--decisions-dir", default="~/.chitin")
    pt.add_argument("--out", default="~/.chitin/predict-model.json")
    pt.add_argument("--iterations", type=int, default=200)
    pt.add_argument("--lr", type=float, default=0.05)
    pt.add_argument("--l2", type=float, default=0.01)
    pt.set_defaults(func=_cli_train)

    pp = sub.add_parser("predict", help="emit P(deny) for a single action shape")
    pp.add_argument("--model", default="~/.chitin/predict-model.json")
    pp.add_argument("--action-type", required=True)
    pp.add_argument("--agent", default="")
    # Use UTC for the default hour: training timestamps come from the
    # kernel in UTC, so a local-time default (datetime.now().hour) on
    # a non-UTC host would bucket the same action into a different
    # time-of-day feature than the model was trained on.
    pp.add_argument("--hour", type=int, default=datetime.now(timezone.utc).hour)
    pp.set_defaults(func=_cli_predict)

    args = p.parse_args(argv)
    return args.func(args)


if __name__ == "__main__":
    raise SystemExit(main())
