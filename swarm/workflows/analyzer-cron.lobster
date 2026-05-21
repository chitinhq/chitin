name: analyzer-cron
description: |
  Daily analyzer pass over recent chitin sessions.

  Canonical source: swarm/workflows/analyzer-cron.lobster
  Runtime path: ~/.openclaw/workflows/analyzer-cron.lobster

  Intended scheduler: OpenClaw cron.
  Manual run:
    lobster run --file ~/.openclaw/workflows/analyzer-cron.lobster --args-json '{"window":"24h"}'

args:
  window:
    description: Recent decision window to analyze.
    default: 24h
  skip_llm:
    description: Skip frontier refinement even when ANTHROPIC_API_KEY is set.
    default: "false"

steps:
  - id: analyze
    description: Run the Python analyzer and write ~/.chitin/analyzer.db
    run: |
      if [ "${skip_llm}" = "true" ]; then
        PYTHONPATH=python python3 -m analysis.analyzer --window "${window}" --skip-llm
      else
        PYTHONPATH=python python3 -m analysis.analyzer --window "${window}"
      fi
