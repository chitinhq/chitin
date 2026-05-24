---
spec_id: 100
title: Factory Webhook Operator Runbook
status: Draft
owner: chitinhq
created: 2026-05-23
depends_on:
  - 098
---

# Demo Spec — Factory Webhook Operator Runbook

## Why

Spec 098's checklist explicitly deferred `docs/operator/factory-webhook.md` to a follow-up. This is that follow-up — and the live-demo test case proving the orchestrator can dispatch a real spec, push a branch to chitinhq/chitin, and open a real PR.

## What

A single markdown document at `docs/operator/factory-webhook.md` covering:

- What `chitin-orchestrator factory-listen` does
- How to generate + install the HMAC secret
- How to configure GitHub webhook on a repo
- Recommended tunnel setup (Cloudflare named tunnel example)
- How to use `chitin-orchestrator simulate-webhook` for local testing
- Where to find the JSONL log (`~/.cache/chitin/factory-listen.jsonl`)
- Troubleshooting: 401 (HMAC), 502 (listener down), non-main branch filter, no-tasks.md skip
