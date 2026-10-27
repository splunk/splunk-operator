# KVService

KVService is an Alpha feature and is disabled by default. Install the KVService
CRD and enable its controller with `--feature-gates=KVService=true` or the
corresponding Helm setting.

The controller manages a Deployment and an HTTP Service on port 8066.

## Configuration and health

KVService shares workload settings with the Enterprise CRs through
`CommonWorkloadSpec`: image and pull policy, resources, scheduling, pod
annotations, Service customization, volumes, service account,
environment variables, image pull secrets, certificates, and initial probe
delays. These fields remain directly under `spec` in YAML. PostgreSQL connection
settings and credentials can be provided through `spec.extraEnv`, Secret references, and `spec.volumes`.
Enterprise cluster and license references and Splunk storage settings are not
part of the KVService API.

- Resource defaults per pod are requests of 1 CPU and 4Gi memory, with limits of
  4 CPUs and 16Gi memory. Values in `spec.resources` override these defaults;
  `spec.disableResourceDefaults: true` disables resource defaulting.
- Configure KVService and PostgreSQL through `spec.extraEnv`, using Secret
  references for credentials. Enterprise `default.yml` and Ansible startup
  settings are not part of the KVService API.
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
  the `CertManagement` feature gate is enabled. The serving Secret contains
  `tls.crt` and `tls.key`, optionally with `ca.crt`. Additional Secrets may
  contain just `ca.crt` for PostgreSQL trust; use `role: server` to select the
  serving certificate when configuring multiple Secrets. Without a role,
  certificates mount at `/mnt/tls/<secret-name>/`;
  role-based paths follow the shared certificate convention. Different Secrets
  must resolve to different mount paths; assign each role to at most one distinct
  Secret. Secret content
  changes update a pod-template annotation to trigger a rollout. Missing Secrets
  can be issued through the shared cert-manager integration when its generation
  feature gate is enabled and an issuer is configured. The serving certificate
  also enables HTTPS as described below.
- Direct and projected ConfigMap references in `spec.volumes` trigger
  reconciliation when the referenced ConfigMap changes. Content hashes on the
  pod template trigger rollout when mounted configuration changes. The shared
  ConfigMap restart opt-out annotation remains supported.
- Errors while resolving pod configuration prevent an incomplete Deployment
  from being applied.
- Any nonempty `spec.certs` enables HTTPS for the KVService endpoint;
  `role: server` is optional. The operator selects the first entry with that role,
  or the first certificate entry when no server role is present. The operator
  uses an existing customer-provided Secret or requests a missing certificate
  through cert-manager, mounts the server certificate, configures the TLS
  environment, and uses HTTPS readiness and liveness probes.
  This requires the `CertManagement` feature gate; creating a missing certificate
  also requires `CertManagerCertGeneration` and a pre-existing issuer reference.
  The serving Secret must contain nonempty `tls.crt` and `tls.key`; a CA-only
  Secret cannot serve the HTTPS endpoint. The operator runs `kvstore-server`
  with an explicit HTTP or HTTPS scheme, independently of the single-tenant
  database mode. Changing `spec.certs` updates the listener and both probes.
- Optional `spec.postgres` references an existing `PostgresDatabase` in the
  KVService namespace. When `databaseRef` is omitted, the controller selects
  `kvstore-database`; when `databaseName` is omitted, it selects the logical
  database `kvstore`. The API does not populate either default in the stored
  spec. An explicitly provided `databaseRef` must have a nonempty name.
  Provision the PostgresDatabase and its logical database separately; KVService
  never creates, modifies, or takes ownership of the database resource.
  KVService waits for the database controller to observe the current generation
  and for the selected logical database to publish ready connection references.
  It then reads connection details and tenant admin credentials from
  `PostgresDatabase.status`. KVService does not watch or separately gate on
  PostgresCluster readiness. This path requires the `PostgresController` gate.
- PostgreSQL credential rollout annotations contain only Secret identity, key
  name, UID, and resourceVersion; they contain no credential values or hashes of
  credential values. Any update to a referenced Secret, including metadata or
  unrelated keys, can trigger a rollout. Connection ConfigMap annotations still
  use content hashes of the selected non-secret configuration fields.

## Status

`status.phase`, `status.conditions`, and `status.observedGeneration` describe
the most recently reconciled generation. Conditions include Ready, Progressing,
and Paused. Each condition provides a reason and message explaining its state.
For example:

```sh
kubectl wait --for=condition=Ready kvservice/<name> --timeout=5m
```

Invalid PostgreSQL configuration, such as an empty database reference name, an
undefined logical database, or a disabled
`PostgresController` feature gate, reports `phase: Error` and `Stalled=True`.
These failures are terminal until the configuration is corrected. Missing or
provisioning PostgreSQL dependencies report `Pending` and continue to reconcile.

The `kvservice.enterprise.splunk.com/paused` annotation pauses reconciliation only
when its value is `"true"`. Setting it to `"false"`, setting it to an empty value,
or removing it resumes reconciliation. A KVService created already paused reports
`phase: Pending` and `Paused=True`. Pausing a resource with an existing phase
preserves that phase.
