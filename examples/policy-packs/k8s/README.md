# Kubernetes Policy Pack

This pack is for repos where agents may touch Kubernetes manifests, Helm charts, or deployment scripts. It keeps the common baseline, then adds strong controls around production contexts, namespace deletion, and secret manifest writes.

## Included policies

- Blocks destructive shell actions, protected-branch pushes, remote code execution, and direct writes to operator credential paths.
- Blocks namespace deletion and production-targeted `kubectl` or `helm` mutations from agent sessions.
- Blocks `kubectl config use-context` to production contexts.
- Blocks direct writes to secret-manifest directories so secret material is never committed or rewritten by the agent.

## Apply this pack

1. Copy [`chitin.yaml`](./chitin.yaml) into the governed infra repo.
2. Replace `prod` and `production` regexes with your actual context and namespace names.
3. Review [`sample-violations.md`](./sample-violations.md) with operators and SREs before enforcing it.

## Good fit

- Platform or delivery repos with Kubernetes YAML and Helm
- Application repos that bundle deployment manifests
- Teams that want agents to prepare changes but never apply them to production
