# KVService

KVService is an Alpha feature and is disabled by default. Install the KVService
CRD and enable its controller with `--feature-gates=KVService=true` or the
corresponding Helm setting.

The controller manages a Deployment and an HTTP Service on port 8066.

## Enterprise client configuration

This applies to v4 Standalone, IndexerCluster, SearchHeadCluster, ClusterManager,
LicenseManager, and MonitoringConsole, and their v3 counterparts. IngestorCluster
and SearchHeadCluster deployers do not receive KVService client settings.

Enterprise workloads discover KVService through labels and an address annotation
on their own Namespace. See [KV Service tenant labels](KVServiceTenantLabels.md)
for the namespace metadata requirements:

```yaml
apiVersion: v1
kind: Namespace
metadata:
  name: workload
  labels:
    platform.splunk.com/kvservice: enabled
    platform.splunk.com/tenant: customer
  annotations:
    platform.splunk.com/kvservice-addr: "https://splunk-shared-kvservice-service.backend:8066"
```

Only the exact value `enabled` activates KVService client configuration. The
`platform.splunk.com/kvservice-addr` annotation supplies the address. The operator
accepts an HTTP or HTTPS endpoint and adds `http://` when no scheme is supplied.
Only the short DNS form
`splunk-<kvservice-name>-kvservice-service.<hosting-namespace>:8066` is accepted.
Full DNS names, IP addresses, aliases, missing/custom ports, credentials, query
strings, fragments, and non-root paths are rejected. Client configuration, tenant
discovery and source-Secret routing share this validation.
The resulting URL is written to
`[kvstore] kvserviceConnectionString`. The `[scs] tenant` value comes from
`platform.splunk.com/tenant`, falling back to the workload Namespace name when
the label is absent or empty.

An enabled namespace with a missing or empty address annotation fails
reconciliation. Namespace read errors also fail reconciliation instead of silently
falling back to local KV Store. Consumers do not require a KVService CR or Service
to exist, inspect PostgreSQL configuration, or wait for backend readiness. Splunk
retries connections while the backend starts. The KVService controller also uses
the namespace metadata to discover and provision tenants in its required
`spec.postgres.clusterRef` cluster, as described below. The annotation identifies
the managed KVService by its short Service DNS; it does not select arbitrary
external backends.

SOK delivers client settings through generated defaults. For an enabled Namespace,
Ansible renders the following into `$SPLUNK_HOME/etc/system/local/server.conf`:

```ini
[kvstore]
defaultKVStoreType = external
kvserviceConnectionString = https://splunk-shared-kvservice-service.backend:8066

[scs]
tenant = customer
mvl-enabled = true
```

SOK does not generate `kvserviceScope` or `[scs] environment` for these clients.
Use Splunk/Ansible images that support this connection-string configuration.
KVService client environment variables are not injected or rejected in `spec.extraEnv`.

Changing the address or tenant triggers a rollout of affected workloads. Removing or
disabling the enable label replaces the generated configuration with
the following local defaults, with no connection string or tenant:

```ini
[kvstore]
defaultKVStoreType = local
postgresMigrateOnStartup = false
```

Provision the backend, selected tenant, authentication, and TLS trust separately.
For TLS, certificates must cover the configured endpoint hostname.

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
  with an explicit HTTP or HTTPS scheme, independently of tenant database
  provisioning. Changing `spec.certs` updates the listener and both probes.
  When `spec.certs` is configured, `spec.extraEnv` cannot override
  `TLS_CERTIFICATE`, `TLS_PRIVATE_KEY`, `TLS_HOST`, or `TLS_PORT`. Without
  `spec.certs`, these variables remain allowed; they do not select HTTPS.
- `spec.postgres.clusterRef.name` is required and names the PostgreSQL cluster
  used for tenant provisioning. Enable the
  `PostgresController` feature gate. KVService waits on tenant database readiness
  and published connection references, leaving cluster/class validation and
  readiness to the PostgreSQL controller.
- PostgreSQL credential rollout annotations contain only Secret identity, key
  name, UID, and resourceVersion; they contain no credential values or hashes of
  credential values. Any update to a referenced Secret, including metadata or
  unrelated keys, can trigger a rollout. Connection ConfigMap annotations still
  use content hashes of the selected non-secret configuration fields.

Terminal validation failures expose a `Stalled=True` condition and the standard
Warning `Stalled` event. After configuration or a watched dependency is repaired,
the controller clears that condition and emits Normal `StalledResolved`; recovery
can resume provisioning before the workload is Ready.

## Namespace-driven tenant provisioning

KVService requires `spec.postgres.clusterRef.name`, identifying a PostgresCluster
in its hosting namespace. Enable both `KVService` and `PostgresController` and
watch the hosting namespace and every tenant namespace; an empty `WATCH_NAMESPACE`
runs cluster-wide.

```yaml
apiVersion: enterprise.splunk.com/v4
kind: KVService
metadata:
  name: kvs2
  namespace: default
spec:
  postgres:
    clusterRef:
      name: postgresql-cluster-dev
---
apiVersion: v1
kind: Namespace
metadata:
  name: tenant1
  labels:
    platform.splunk.com/kvservice: enabled
    platform.splunk.com/tenant: tenant1
  annotations:
    platform.splunk.com/kvservice-addr: "splunk-kvs2-kvservice-service.default:8066"
```

The address annotation selects the backend Service and port. Bare addresses use
HTTP for Enterprise clients; specify `https://` for an HTTPS listener. The tenant
label overrides the namespace name. Each selected namespace represents one tenant,
and tenant names must match `^[a-z][a-z0-9]{0,29}$`. Use a valid tenant label for
namespace names containing dashes. Tenant names must be unique for a backend.
Invalid selected tenant configuration stalls reconciliation and suspends serving;
transient Kubernetes read failures retain the previous valid authorization state.

The controller requests a `<tenant>-database` PostgresDatabase in the KVService
namespace for every discovered tenant, without waiting for one tenant before
requesting another. The PostgreSQL controller creates the logical database and
publishes its readiness, connection information and credentials. KVService waits
for the tenant databases and their connection information; it does not separately
wait for PostgresCluster or PostgresClusterClass readiness.

Each tenant namespace must contain `splunk-<namespace>-secret` with a non-empty
`pass4SymmKey`. The KVService image must support the mounted multi-tenant runtime
configuration. Namespace membership, source Secrets, tenant databases and their
connection ConfigMaps and credential Secrets are watched automatically.

### Authentication-key rotation

KVService's generated `tenant-verification-keys` Secret retains the current and
previous pass4SymmKey for each tenant so clients can rotate gradually. At most two
distinct keys are allowed per tenant. Before another rotation, confirm all clients have
adopted the current key, then remove the previous entry from that tenant's JSON
array in this Secret. Retirement is explicit; the controller does not detect
client rollout completion or expire keys. Different tenants may use the same
pass4SymmKey; rotation and retirement are tracked independently for each tenant.

An unavailable tenant Secret retains that tenant's last published keys while other
tenants can rotate. Removing a tenant revokes its keys even when another tenant's
Secret is unavailable. A key value used by another tenant does not block publication.

### Runtime refresh and pod rollouts

Tenant membership and verification-key changes deliberately trigger rolling
updates when dependency reconciliation completes. The mounted files also support
runtime rereading by a compatible KVService image; this does not make the
Operator-managed deployment restart-free. Adding a tenant also adds its database
password Secret mount to the pod specification.

Any update to the generated keyring Secret, including metadata-only changes,
can trigger a rollout. An unchanged reconciliation does not request one. File
propagation and rollouts are asynchronous, and pending dependencies can delay a
Deployment update; neither mechanism guarantees an immediate revocation cutoff.

### Retention and replacement

Generated tenant databases use `Retain` and survive tenant removal or KVService
deletion. A recreated KVService can reuse its retained databases; a differently
named replacement can reuse compatible managed databases after the previous
KVService is gone. Unrelated or incompatible databases are never overwritten.
Reuse is authorized by the `enterprise.splunk.com/kvservice-owner` annotation on
the PostgresDatabase; application labels alone do not authorize reuse. A database
with a Kubernetes controller owner is rejected instead of having its owner
reference removed automatically.

Only one KVService is allowed per hosting namespace. If multiple CRs exist,
reconciliation reports a terminal conflict and leaves serving resources unchanged
until only one CR remains, including completion of any pending deletions.
Creating or deleting a peer requeues other KVServices in that namespace;
status-only changes do not. Updates and deletion remain available to resolve
conflicts. The keyring ownership check also prevents taking over a previous
KVService's Secret before garbage collection finishes.
If no tenant namespaces select the backend, KVService reports Pending, suspends
its workload, and removes generated membership and authentication keys while
retaining the databases. Re-enabling a tenant resumes provisioning.

Enterprise clients use the same Namespace metadata. SOK writes the external KV
Store selection, `kvserviceConnectionString`, tenant, and MVL configuration through
its defaults workflow into `$SPLUNK_HOME/etc/system/local/server.conf`. See
[namespace configuration](KVServiceTenantLabels.md) for the complete client contract.

## Status

`status.phase`, `status.conditions`, and `status.observedGeneration` describe
the most recently reconciled generation. Conditions include Ready, Progressing,
and Paused. Each condition provides a reason and message explaining its state.
For example:

```sh
kubectl wait --for=condition=Ready kvservice/<name> --timeout=5m
```

Invalid tenant configuration or a disabled
`PostgresController` feature gate reports `phase: Error` and `Stalled=True`.
These failures are terminal until the configuration is corrected. Missing or
provisioning PostgreSQL dependencies report `Pending` and continue to reconcile.

The `kvservice.enterprise.splunk.com/paused` annotation pauses reconciliation only
when its value is `"true"`. Setting it to `"false"`, setting it to an empty value,
or removing it resumes reconciliation. A KVService created already paused reports
`phase: Pending` and `Paused=True`. Pausing a resource with an existing phase
preserves that phase.
