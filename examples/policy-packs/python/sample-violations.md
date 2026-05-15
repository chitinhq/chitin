# Python Sample Violations

These examples illustrate the Python-specific command shapes the pack is meant to stop.

## 1. Unsafe system interpreter mutation

Violation:

```bash
pip install requests --break-system-packages
```

Expected fix:

```bash
python -m venv .venv && . .venv/bin/activate && pip install requests
```

Reason: Python agent work should happen inside a project virtual environment, not by mutating the shared interpreter.

## 2. Broad package upgrade sweep

Violation:

```bash
pip install -U -r requirements.txt
```

Expected fix:

```bash
pip install -r requirements.txt
```

Reason: explicit version bumps should happen in the dependency file first, then be installed and tested.

## 3. Publishing from the workstation session

Violation:

```bash
twine upload dist/*
```

Expected fix:

```bash
python -m build
```

Reason: building artifacts is fine; publishing them is a separate, human-controlled release action.
