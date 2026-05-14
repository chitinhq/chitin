# Kubernetes Sample Violations

These examples show the production-facing actions this pack is meant to stop.

## 1. Namespace deletion

Violation:

```bash
kubectl delete namespace payments
```

Expected fix:

```bash
kubectl get namespace payments
```

Reason: inspection is fine; destructive cluster mutation is not.

## 2. Applying to production

Violation:

```bash
kubectl --context production apply -f k8s/deployment.yaml
```

Expected fix:

```bash
kubectl diff -f k8s/deployment.yaml
```

Reason: agents can prepare and diff changes, but live production apply stays a human action.

## 3. Switching the active context to prod

Violation:

```bash
kubectl config use-context prod-us-east-1
```

Expected fix:

```bash
kubectl config current-context
```

Reason: production context switches are a strong indicator the session is moving beyond review and into live operations.
