# KVService

KVService is an Alpha feature. Install the KVService CRD and enable the controller
with `--feature-gates=KVService=true` (or the corresponding Helm feature-gate
setting). It is disabled by default.

The controller manages a Deployment and an HTTP Service on port 8066. Helm
namespace and cluster roles, and the OLM operator permissions, include access to
the KVService resource, its status and finalizers, and Deployments.

## Configuration and health

- Inline `spec.defaults` is stored in an owned ConfigMap before the Deployment
  mounts it.
- `spec.podAnnotations` overrides inherited pod annotations.
- Liveness and readiness checks use `/service/liveness` and `/service/readiness`.
  Their configured delays, timeout, period, and failure threshold are honored.
  The HTTP endpoints remain fixed; command overrides are not used for these
  probes. KVService does not currently configure a startup probe.
- Direct and projected ConfigMap references in `spec.volumes` trigger
  reconciliation when the referenced ConfigMap changes. Content hashes on the
  pod template trigger rollout when mounted configuration changes. The shared
  ConfigMap restart opt-out annotation remains supported.
- Errors while resolving pod configuration prevent an incomplete Deployment
  from being applied.

## Status

`status.phase`, `status.conditions`, and `status.observedGeneration` describe
the most recently reconciled generation. Conditions include Ready, Progressing,
and Paused. For example:

```sh
kubectl wait --for=condition=Ready kvservice/<name> --timeout=5m
```

The `kvservice.enterprise.splunk.com/paused` annotation pauses reconciliation;
removing it resumes reconciliation.
