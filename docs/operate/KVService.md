# KVService

KVService is an Alpha feature and is disabled by default. Install the KVService
CRD and enable its controller with `--feature-gates=KVService=true` or the
corresponding Helm setting.

The controller manages a Deployment and an HTTP Service on port 8066.

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
and Paused. Each condition provides a reason and message explaining its state.
For example:

```sh
kubectl wait --for=condition=Ready kvservice/<name> --timeout=5m
```

The `kvservice.enterprise.splunk.com/paused` annotation pauses reconciliation only
when its value is `"true"`. Setting it to `"false"`, setting it to an empty value,
or removing it resumes reconciliation. A KVService created already paused reports
`phase: Pending` and `Paused=True`. Pausing a resource with an existing phase
preserves that phase.
