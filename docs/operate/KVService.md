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
MonitoringConsole, and their v3 counterparts. IngestorCluster and SearchHeadCluster
deployers do not receive these settings.

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
defaultKVStoreType = external
kvserviceConnectionString = http://splunk-kvs2-kvservice-service.default:8066

[scs]
tenant = tenant1
mvl-enabled = true
```

SOK does not set `kvserviceScope` or `[scs] environment`. Changing the tenant or
address rolls affected workloads. Removing or disabling the enable label removes
the connection string and tenant and restores these local defaults:

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

Without `spec.certs`, the listener and probes use HTTP. A nonempty `spec.certs`
enables HTTPS and requires `CertManagement`. The serving Secret must contain
nonempty `tls.crt` and `tls.key`; its certificate must cover the configured endpoint
hostname. Use `role: server` to select the serving certificate when providing
multiple Secrets. A CA-only Secret cannot serve the endpoint.

Provide an existing Secret or use cert-manager to issue a missing certificate.
Issuance requires `CertManagerCertGeneration` and an existing issuer reference.
See [certificate configuration](Certificates.md) for fields, mount paths and
rotation. Certificate changes trigger a rollout. With `spec.certs`, the Operator
manages `TLS_CERTIFICATE`, `TLS_PRIVATE_KEY`, `TLS_HOST` and `TLS_PORT`; setting
these variables without `spec.certs` does not select HTTPS.

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
