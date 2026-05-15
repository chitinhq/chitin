# Chitin Agentguard

Local VS Code extension for watching the live chitin v3 decision chain.

Features:

- Status bar summary for lockdown state and the last blocked action
- Tree view of recent decision events with timestamps
- Click-to-open detail panel backed by `chitin-kernel explain`
- Socket-first ingestion with JSONL tail fallback for local development
