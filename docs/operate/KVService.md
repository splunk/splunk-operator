# KVService

KVService is an Alpha feature, disabled by default. The controller manages a
Deployment and Service on port 8066. Before creating a KVService:

- Install its CRD and enable `KVService=true,PostgresController=true` on the Operator.
- Provide a PostgresCluster in the KVService namespace and compatible KVService,
  Splunk and Ansible images.
- Watch both the hosting and tenant namespaces; an empty `WATCH_NAMESPACE` watches
  all namespaces. Each tenant namespace needs `splunk-<namespace>-secret` with a
  non-empty `pass4SymmKey`.

## Enterprise client configuration

Namespace labels and an address annotation select KVService for v4 Standalone,
IndexerCluster, SearchHeadCluster, ClusterManager, LicenseManager and
MonitoringConsole, and their v3 counterparts. SearchHeadCluster members and their
deployer share the namespace-selected settings. IngestorCluster does not receive
these settings. Noah SearchHeadClusters do not deploy a deployer.

Use the Namespace example under [tenant provisioning](#namespace-driven-tenant-provisioning).
Only `platform.splunk.com/kvservice: enabled` activates the configuration. The
`platform.splunk.com/tenant` label supplies the tenant, falling back to the namespace
name when absent or empty. The address must use the short Service DNS form
`splunk-<kvservice-name>-kvservice-service.<hosting-namespace>:8066`.
Specify `https://` for an HTTPS listener; a bare address uses HTTP. See the
[complete namespace contract](KVServiceTenantLabels.md) for address restrictions.

An enabled namespace with missing or invalid address metadata fails reconciliation
rather than falling back to local KV Store. Client workloads do not wait for backend
readiness; Splunk retries connections while KVService starts.

SOK passes generated defaults to Ansible, which writes the following to
`$SPLUNK_HOME/etc/system/local/server.conf`:

```ini
[kvstore]
kvserviceConnectionString = http://splunk-kvs2-kvservice-service.default:8066

[scs]
tenant = tenant1
mvl-enabled = true
```

Use Splunk/Ansible images that support this connection-string configuration.
SOK retains `SPLUNK_KVSTORE_DEFAULT_TYPE=local` on non-ingestor workloads for
compatibility with older 10.6 Ansible images. Updated Ansible honors that legacy
input on 10.6.x regardless of the enablement flag and skips its writes on managed
10.9 workloads. Splunk 10.9 images contain the updated Ansible policy, so this
compatibility input does not reset a migrated external backend.
SOK also sets `SPLUNK_KVSERVICE_ENABLED=true` for these clients from the same
namespace decision that supplies the connection string. Disabled clients receive
`SPLUNK_KVSERVICE_ENABLED=false`. This variable is operator-owned and reserved;
admission rejects it in `spec.extraEnv`. Generated pod configuration also replaces
stored overrides so its value matches the namespace configuration.

For enabled clients, SOK omits `defaultKVStoreType` and
`postgresMigrateOnStartup` from generated defaults. Use images whose Ansible
configuration honors the explicit enablement flag: on Splunk 10.6 it keeps local
KV Store with migration disabled; on Splunk 10.9 enabled clients receive startup
migration while their saved backend type is preserved. Successful migration
persists the external backend, which SOK leaves unchanged on subsequent
reconciliations. Fresh-install backend initialization and startup sequencing are
separate from this flag contract.

Older operators do not supply this flag. An unset flag does not mean disabled;
the image retains its legacy behavior for deployments without the explicit flag.

Changing the address or tenant triggers a rollout of affected workloads. Removing or
disabling the enable label replaces the generated configuration with
the following local defaults, with no connection string or tenant, and sets the
enablement flag to `false`:

```ini
[kvstore]
defaultKVStoreType = local
postgresMigrateOnStartup = false
```

## Configuration and health

Workload settings such as image, resources, scheduling, Service customization,
volumes, service account, image pull Secrets and `extraEnv` remain directly under
`spec`. The Operator derives database connection settings and credentials from
`spec.postgres.clusterRef` and generated database resources; use `extraEnv` only
for settings it does not manage.

| Setting | Default | Override |
|---|---|---|
| CPU / memory requests | 1 CPU / 4Gi | `spec.resources.requests` |
| CPU / memory limits | 4 CPUs / 16Gi | `spec.resources.limits` |
| Probe initial delay | 30 seconds | `spec.livenessProbe` / `spec.readinessProbe` |
| Probe timeout / period | 1 / 5 seconds | Corresponding probe fields |
| Probe failure threshold | 3 | Corresponding probe field |

`spec.disableResourceDefaults: true` disables resource defaulting. Probe timing
fields omitted or set to zero retain their defaults. The separate
`livenessInitialDelaySeconds` and `readinessInitialDelaySeconds` apply when the
corresponding probe does not specify a delay. Probes use `/service/liveness` and
`/service/readiness` on port 8066, with success threshold 1; no startup probe is
configured.

### TLS certificates

`spec.scheme` is a required `http` or `https` field. Kubernetes defaults an
omitted value to `http`; an explicit empty or unsupported value is rejected.
The listener, Service
port name and health probes use this selection on port 8066. Certificate entries
do not implicitly enable HTTPS. In HTTP mode, `spec.certs` is ignored: the Operator
does not issue, read or mount these certificates, or react to their rotation.
It logs a warning on every reconcile when certificate entries are present.

For HTTPS, enable `CertManagement`, set `spec.scheme: https` and provide
`spec.certs`. The serving Secret must contain nonempty `tls.crt` and `tls.key`,
and its certificate must cover the configured endpoint hostname. Use `role: server`
to select the serving certificate when providing multiple Secrets. A CA-only
Secret cannot serve the endpoint.

```yaml
spec:
  scheme: https
  certs:
    - role: server
      secretRef:
        name: kvservice-tls
  postgres:
    clusterRef:
      name: postgresql-cluster-dev
```

Provide an existing Secret or use cert-manager to issue a missing certificate.
Issuance requires `CertManagerCertGeneration` and an existing issuer reference.
The default generated DNS SAN is the short Service name
`splunk-<name>-kvservice-service.<namespace>`, matching the client endpoint.
Customer-provided certificates and explicit `dnsNames` must cover this name;
the `.svc.cluster.local` suffix is not required.
See [certificate configuration](Certificates.md) for fields, mount paths and
rotation. In HTTPS mode, certificate changes trigger a rollout, and the Operator
manages `TLS_CERTIFICATE`, `TLS_PRIVATE_KEY`, `TLS_HOST` and `TLS_PORT`.
Setting these variables in HTTP mode does not enable HTTPS.

The controller checks the serving PEM certificate/key pair, current validity,
TLS server usage and the short Service DNS name before changing the Service or
Deployment. A certificate restricted to unrecognized extended key usages is
rejected. An invalid certificate leaves the existing serving resources in place;
correcting the Secret triggers reconciliation again. Secret reference names must
be valid Kubernetes Secret names in HTTPS mode. Certificate preflight runs during
reconciliation; customers supplying certificates must monitor expiry and renew them.

### Client protocol and certificate trust

The listener scheme and namespace `platform.splunk.com/kvservice-addr` annotation
are configured independently. A bare address means HTTP. For an HTTPS listener,
use an explicit annotation, for example:

```yaml
metadata:
  annotations:
    platform.splunk.com/kvservice-addr: "https://splunk-kvs2-kvservice-service.default:8066"
```

`ClientConfigurationReady=False` with reason `ListenerProtocolMismatch` lists
up to five namespaces whose client protocol disagrees with the listener, the total
count, and the expected address. A Warning event is emitted when this diagnostic
changes; correction emits a recovery event. Database provisioning, key publication and other tenants continue.
The condition is Unknown if discovery could not run. It checks namespace protocol
configuration only, not client rollout completion, connectivity or certificate trust.

A server certificate does not install its CA on Splunk clients. For a private CA
or self-signed certificate, mount the issuing CA into each Splunk client using the
existing Secret-volume facilities and configure its outbound TLS trust settings.
Verify both CA trust and hostname verification on the KVService client path; do not
use disabled verification as the production configuration. With a TLS-terminating
gateway, clients trust the gateway certificate; the backend listener's protocol is
configured separately. For private-CA rotation, distribute trust for both old and
new CAs to clients before changing the server issuer. Retire the old CA after the
server and client rollouts complete.

Validate from a client pod with verification enabled, then run a KV Store smoke
or distributed-lookup test through Splunk to exercise Splunk's own TLS configuration:

```bash
kubectl -n <client-namespace> exec <splunk-pod> -- \
  curl --fail --cacert /path/to/kvservice-ca.crt \
  https://splunk-kvs2-kvservice-service.default:8066/service/readiness
```

HTTPS Kubernetes probes skip certificate verification; pod readiness alone does
not establish that clients trust the endpoint. See the
[Kubernetes probe documentation](https://kubernetes.io/docs/tasks/configure-pod-container/configure-liveness-readiness-startup-probes/).

### Changing HTTP and HTTPS

Treat `spec.scheme` changes as planned maintenance. The Deployment uses a rolling
update, so old and new pods can temporarily serve different protocols behind the
same Service. Splunk client configuration also rolls independently. This flow does
not promise uninterrupted HTTP-to-HTTPS or HTTPS-to-HTTP cutover.

Prepare the serving certificate and client CA trust first. During the maintenance
window, update `spec.scheme` and all selecting namespaces' `kvservice-addr`
annotations to the matching protocol. Wait for the KVService rollout, the
`ClientConfigurationReady=True` condition and all Splunk client rollouts; then
validate verified TLS, KV Store operations and distributed lookups before resuming
traffic. For rollback, restore both the previous listener scheme and client URLs.
A zero-downtime cutover needs a stable gateway or a separate parallel-backend design.

## Namespace-driven tenant provisioning

`spec.postgres.clusterRef.name` is mandatory and identifies a PostgresCluster in
the KVService's hosting namespace. For example:

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

The annotation selects the backend, including across namespaces. Each selected
namespace represents one tenant. Tenant names must be unique for that backend and
match `^[a-z][a-z0-9]{0,29}$`; use a valid tenant label when a namespace name contains
dashes. `kvservicesystem` is reserved and cannot be a tenant. Invalid or duplicate
tenant names exclude only the affected tenants.

The controller provisions databases in its hosting namespace:

| Resource | SQL database | Purpose |
|---|---|---|
| `kvservice-service-database` | `kvservicesystem` | Service startup and state |
| `<tenant>-database` | Tenant name | Tenant schema and data |

The PostgreSQL controller creates the databases, roles and connection resources.
KVService startup waits only for the service database and its usable connection
credentials. It does not separately wait for cluster/class readiness or for every
tenant. All discovered tenant databases are requested without serial readiness
waits, and the listener can run with zero tenants.

A tenant enters runtime membership when its database, connection ConfigMap, owner
password Secret and namespace authentication key are usable. KVService initializes
its schema independently and retries failures; an API request may also complete
initialization. Service credentials are never used for tenant operations. Use a
KVService image supporting independent startup and tenant-local credential retries.

### Authentication-key rotation

The generated `tenant-verification-keys` Secret holds at most two distinct
pass4SymmKeys per tenant: current and previous. Both are accepted during a client
rollout. Before another rotation, confirm all clients use the current key, then
remove the previous entry from that tenant's JSON array in this Secret. Retirement
is explicit; the controller neither detects client rollout completion nor expires
keys. Different tenants may use the same key.

If a source Secret is unavailable, its last published keys are retained while other
tenants can rotate; the affected tenant is excluded from runtime membership until
its prerequisites recover. Removing a tenant revokes its keys. Transient namespace
discovery read failures preserve the previous valid authorization state.

### Runtime refresh and pod rollouts

The Operator watches Namespace metadata, authentication Secrets, database resources
and their connection ConfigMaps and credential Secrets. Membership and keyring
changes deliberately trigger rolling updates when dependencies are reconciled.
Adding a tenant also adds its password mount. During propagation, a compatible
runtime retries missing tenant credentials without terminating healthy tenants.

Mounted files also support runtime rereading, but the managed Deployment is not
restart-free. Updates to referenced credential or keyring Secrets, even metadata-only
updates, can trigger rollouts; unchanged reconciliation does not. File propagation
and pending dependencies mean revocation has no guaranteed immediate cutoff.
User-provided ConfigMap volumes also trigger reconciliation and rollout on content
changes, subject to the shared restart opt-out annotation.

### Retention and replacement

Generated service and tenant databases use `Retain` and survive tenant removal or
KVService deletion. A recreated KVService can reuse them; a differently named
replacement can reuse compatible managed databases after the previous KVService
is gone. The `enterprise.splunk.com/kvservice-owner` annotation authorizes reuse;
application labels alone do not. Unrelated, incompatible or controller-owned
databases are not adopted or overwritten.

Only one KVService is allowed per hosting namespace. Conflicts leave serving
resources unchanged until only one CR remains, including completion of pending
deletions. A previous KVService's keyring must also be released before replacement.
Removing all tenants publishes empty membership and keys while retaining the
databases and keeping the service running on `kvservicesystem`.

## Status and troubleshooting

Inspect the service, tenant prerequisites and events:

```sh
kubectl -n default get kvservice kvs2 -o yaml
kubectl -n default describe kvservice kvs2
kubectl -n default get postgresdatabases.platform.splunk.com
kubectl -n default wait --for=condition=Ready kvservice/kvs2 --timeout=5m
```

| Signal | Meaning / action |
|---|---|
| `phase: Pending` | Service database or its connection credentials are not ready; inspect its PostgresDatabase. |
| `Ready=True` | The serving workload is ready; this does not require every tenant to be ready. |
| `TenantsReady=Unknown` | Tenant evaluation could not run; tenant entries retain last-known observations. Inspect the service condition and retry after the underlying failure. |
| `TenantsReady=True`, reason `NoTenants` | No tenant namespaces select this service; zero-tenant startup is supported. |
| `status.tenants` / `TenantsReady=False` | Inspect the affected namespace's reason/message and database or authentication prerequisites. This status does not acknowledge runtime schema completion. |
| `phase: Error`, `Stalled=True` | Correct a terminal configuration problem, such as a disabled PostgresController gate or duplicate hosting-namespace KVServices. |

Watched corrections resume reconciliation; pending tenant dependencies also retry.
Tenant transitions emit `TenantReady` or Warning `TenantNotReady` events. Terminal
errors emit Warning `Stalled`; recovery clears the condition and emits
`StalledResolved`. Check `status.observedGeneration` when evaluating current status.
Operator logs identify the tenant, namespace, Secret name and underlying error for
failed authentication reads other than a Secret not yet existing.

Set `kvservice.enterprise.splunk.com/paused: "true"` to pause reconciliation.
Removing it or using another value resumes reconciliation. An initially paused
resource reports `Pending` and `Paused=True`; an existing phase is preserved.

For implementation details, see [KVService reconciliation](../develop/Architecture.md#kvservice-reconciliation).

## Horizontal autoscaling

Set `spec.autoscaling` to have the operator create and manage a Kubernetes
`autoscaling/v2` HorizontalPodAutoscaler (HPA). When `spec.autoscaling` is
omitted, the operator does not create an HPA and `spec.replicas` remains the
manually managed replica count.

```yaml
apiVersion: enterprise.splunk.com/v4
kind: KVService
metadata:
  name: kvservice-sample
spec:
  postgres:
    clusterRef:
      name: postgresql-cluster-dev
  replicas: 2
  autoscaling:
    minReplicas: 2
    maxReplicas: 10
    targetAverageUtilizationCPU: 60
    targetAverageUtilizationMemory: 60
  resources:
    requests:
      cpu: "4"
      memory: 4Gi
    limits:
      cpu: "8"
      memory: 16Gi
```

The utilization targets are percentages of the container resource requests,
not percentages of node capacity. Define both CPU and memory requests when
using autoscaling. The operator supplies the resource defaults described above
when they are omitted. If `spec.disableResourceDefaults` is enabled, provide both
requests explicitly. Select production requests from measured KVService usage.

The default autoscaling configuration uses the following replica bounds and
scaling rate limits:

| Setting | Default |
| --- | --- |
| Minimum replicas | 2 |
| Maximum replicas | 10 |
| Average CPU utilization | 60% |
| Average memory utilization | 60% |
| Scale-up stabilization window | 60 seconds |
| Scale-down stabilization window | 600 seconds |

The scale-up and scale-down stabilization windows are fixed by the operator at
60 and 600 seconds, respectively. They are not configurable through
`spec.autoscaling`.

Both `minReplicas` and `maxReplicas` must be between 2 and 10, inclusive, with
`minReplicas` less than or equal to `maxReplicas`.

Scale-up is limited to 2 pods or 25% per minute, whichever permits the larger
change. Scale-down is limited to 1 pod or 25% per minute, with the longer
stabilization window protecting against rapid oscillation.

The HPA targets the KVService custom resource rather than its generated
Deployment. This is important because the HPA writes the replica count through
the KVService `scale` subresource and the KVService controller remains the sole
owner of the Deployment.

The reconciliation flow is:

1. Metrics Server reports CPU and memory usage for KVService pods.
2. The HPA calculates a desired replica count from both metrics and uses the
   larger recommendation.
3. The HPA updates `KVService.spec.replicas` through the `scale` subresource.
4. The KVService controller updates the generated Deployment.
5. The Deployment creates or removes pods.
6. The KVService controller reports desired and ready replicas in status.

Inspect the resources with:

```bash
kubectl get kvs,deploy,hpa,pods
kubectl describe hpa splunk-kvservice-sample-kvservice-hpa
```

The cluster must expose the `metrics.k8s.io` API, commonly provided by
Kubernetes Metrics Server. If metrics are unavailable, the HPA reports an
`AbleToScale` or `ScalingActive` condition explaining why it cannot calculate a
replica count.

Removing `spec.autoscaling` deletes the operator-owned HPA and returns replica
management to `spec.replicas`. That field retains the HPA's last selected value,
so set it explicitly when disabling autoscaling if a different fixed count is
required. During active reconciliation, HPA removal does not wait for the service
database or workload dependencies to become ready.

`spec.replicas` defaults to 2 when omitted, including when autoscaling is disabled.
