# KVService

KVService is an Alpha feature and is disabled by default. Install the KVService
CRD and enable its controller with `--feature-gates=KVService=true` or the
corresponding Helm setting.

The controller manages a Deployment and an HTTP Service on port 8066.

## Configuration and health

KVService shares workload settings with the Enterprise CRs through
`CommonWorkloadSpec`: image and pull policy, resources, scheduling, pod
annotations, Service customization, volumes, inline defaults, service account,
environment variables, image pull secrets, certificates, and initial probe
delays. These fields remain directly under `spec` in YAML. PostgreSQL connection
settings and credentials can be provided through `spec.extraEnv`, Secret references, and `spec.volumes`.
Enterprise cluster and license references and Splunk storage settings are not
part of the KVService API.

- Resource defaults per pod are requests of 1 CPU and 4Gi memory, with limits of
  4 CPUs and 16Gi memory. Values in `spec.resources` override these defaults;
  `spec.disableResourceDefaults: true` disables resource defaulting.
- Inline `spec.defaults` is stored in an owned ConfigMap before the Deployment
  mounts it.
- `spec.podAnnotations` overrides inherited pod annotations.
- Liveness and readiness checks use `/service/liveness` and `/service/readiness`
  on port 8066. Both default to a 30-second initial delay, a 1-second timeout,
  a 5-second period, a success threshold of 1, and a failure threshold of 3.
  Use `spec.livenessProbe` and `spec.readinessProbe` to override delay, timeout,
  period, and failure threshold. Unspecified or zero timing fields retain their
  defaults. The separate `spec.livenessInitialDelaySeconds` and
  `spec.readinessInitialDelaySeconds` fields apply when the corresponding probe
  does not specify an initial delay. HTTP endpoints and the success threshold
  remain fixed; KVService does not configure a startup probe.
  The CRD supplies timeout, period, and failure-threshold defaults for omitted
  probe objects. The 30-second initial-delay fallback is applied by the controller
  so the separate initial-delay fields continue to take effect.
- `spec.certs` uses the shared certificate mounting and rotation workflow when
  the `CertManagement` feature gate is enabled. Existing Secrets may contain
  `tls.crt` and `tls.key`, optionally with `ca.crt`, or just `ca.crt` for a
  PostgreSQL CA. Without a role, certificates mount at `/mnt/tls/<secret-name>/`;
  role-based paths follow the shared certificate convention. Secret content
  changes update a pod-template annotation to trigger a rollout. Missing Secrets
  can be issued through the shared cert-manager integration when its generation
  feature gate is enabled and an issuer is configured. Mounting a certificate
  does not by itself configure the KVService process to use TLS.
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
