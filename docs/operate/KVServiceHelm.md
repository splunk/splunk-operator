---
title: KVService through Helm
parent: Operate
nav_order: 12
---

# Deploy shared PostgreSQL and KVService with SOK Helm

The Operator chart keeps `sharedPostgres`, `sharedKVService`, `cloudnative-pg`
and `plugin-barman-cloud` disabled by default. Enable the services explicitly
when needed; an unconfigured installation retains the Operator-only behavior.
Both CRs use the Operator namespace, including `namespaceOverride`. The chart
connects KVService to the generated PostgreSQL cluster name; it creates no tenant
databases or KVService Deployment directly. Existing controllers provision the
system database, serving resources and discovered tenant databases.

## Before installing or upgrading

Install matching SOK CRDs and a compatible cert-manager first. Enable
`cloudnative-pg.enabled` and `plugin-barman-cloud.enabled` only when compatible
controllers do not already exist.
Do not install a second CNPG or cert-manager controller over an existing one.
Provide storage capacity, eligible nodes, a supported KVService release image,
and a deliberate backup policy when opting in. Existing releases remain
Operator-only unless their values enable these services. No Splunk installation,
tenant or namespace labels are required to start the shared services.

When enabling KVService, supply its qualified release image.
`sharedKVService.image` is deliberately required only for that opt-in; the chart does not guess an
unpublished image or deploy a `latest` fallback. Chart defaults do not accept
license terms or configure a log sink on the customer's behalf.

```yaml
cloudnative-pg:
  enabled: true
plugin-barman-cloud:
  enabled: true
sharedPostgres:
  enabled: true
  storageClass: gp3
  storage: 100Gi
  backup:
    enabled: true
    destinationPath: s3://customer-backups/postgres/
    credentials:
      existingSecret: postgres-backup-credentials
sharedKVService:
  enabled: true
  image: REGISTRY/KVSERVICE:RELEASE
  replicas: 2
  readiness:
    timeoutSeconds: 900
```

For disposable testing only, replace the backup configuration with
`sharedPostgres.backup.acknowledgeNoBackups: true`. Disabling backups is never
acknowledged automatically. PostgreSQL defaults still request three instances
and node separation; see [PostgreSQL backend configuration](../deploy/Helm.md#postgresql-backend-controllers).

```bash
helm upgrade --install sok ./helm-chart/splunk-operator \
  --namespace splunk-operator --create-namespace \
  --values services-values.yaml --wait --timeout 20m
```

Use a Helm timeout long enough for controller installation plus the configured
readiness budget. With Helm 4.0.1, use `--wait=legacy` in place of `--wait` so a
failed hook Job promptly fails Helm; its default watcher waits until Helm's own
timeout instead. `--no-hooks` bypasses the readiness check and is not a supported
way to establish shared-service readiness. If enabling validation on an existing
release, first enable its controllers with shared provisioning disabled and wait
for the Operator to be Ready, then enable the shared services; follow the
[PostgreSQL backend configuration](../deploy/Helm.md#postgresql-backend-controllers).

## Readiness and failure handling

Helm submits both CRs. KVService reconciliation gates its Deployment on the
`kvservicesystem` database and credentials; tenant failures are independent.
A post-install/post-upgrade Job waits in this order:

1. PostgreSQL `Ready=True` for the current observed generation.
2. KVService `Ready=True` for the current observed generation, representing its
   system-database-dependent serving Deployment. Tenant readiness is not required.

The init container completes before the KVService check starts. The whole Job
has one `activeDeadlineSeconds` budget, no retries, and read-only access to only
the selected PostgreSQL and KVService CRs. It reads no credentials. The Job's
container image contains the kubectl CLI used to run `kubectl wait`.
For a private registry or air-gapped environment, configure
`sharedKVService.readiness.image` and `imagePullSecrets`. Keep the kubectl version
within the cluster's supported client/server skew; the default image is pinned
by digest. Override `sharedKVService.readiness.securityContext` when the
namespace requires a specific UID/GID range, such as OpenShift SCCs.

On failure Helm reports a failed operation. The controllers continue recovery
unless Helm rollback/cleanup flags remove their resources. Successful Jobs are
removed; failed Jobs remain up to 24 hours for inspection. For release `sok`:

```bash
kubectl -n splunk-operator get postgresclusters.platform.splunk.com,kvservices
kubectl -n splunk-operator logs job/sok-kvs-ready -c wait-postgres
kubectl -n splunk-operator logs job/sok-kvs-ready -c wait-kvservice
```

After correction rerun `helm upgrade`; the old readiness Job is replaced.

## Independent opt-ins and existing PostgreSQL

For PostgreSQL without KVService, enable `sharedPostgres` and the backend
controllers needed by the cluster, and configure backups as above. Leave
`sharedKVService.enabled=false`.

Enable each required component explicitly: enabling KVService does not install
CNPG, Barman or a PostgreSQL cluster automatically. To reuse existing backend
controllers, leave their dependency switches disabled.

Operator only (the default; no values overrides are required):

```yaml
sharedPostgres:
  enabled: false
sharedKVService:
  enabled: false
cloudnative-pg:
  enabled: false
plugin-barman-cloud:
  enabled: false
```

KVService with an existing PostgreSQL cluster in the Operator namespace:

```yaml
sharedPostgres:
  enabled: false
cloudnative-pg:
  enabled: false
plugin-barman-cloud:
  enabled: false
sharedKVService:
  enabled: true
  image: REGISTRY/KVSERVICE:RELEASE
  postgres:
    clusterRef:
      name: customer-postgres
```

The chart enables `KVService` and `PostgresController` gates for managed workloads
only when those gates are unset. Explicit disables in `splunkOperator.featureGates`,
`extraEnvs.FEATURE_GATES` or an applicable `AllAlpha=false` reject the conflicting
shared workload. Explicit per-gate CLI values retain the Operator's precedence
over environment settings; the chart never adds an enable flag to override a disable.
Extra gates remain opt-in. An existing KVService is not implicitly adopted.
Configure tenant namespace labels/annotations separately, following
[namespace-driven tenant provisioning](KVService.md#namespace-driven-tenant-provisioning).
The chart neither relabels tenant namespaces nor changes their authentication keys.

`sharedKVService.certs` passes the current CR certificate API through unchanged;
use an existing certificate Secret or supported cert-manager issuance. Listener
behavior follows the installed controller/API. Independent `spec.scheme`
selection is a separate MR and is not introduced here.

Changing `sharedKVService.enabled` from true to false removes its CR and
controller-owned serving resources, interrupting service for its tenants.
PostgreSQL and retained system/tenant databases remain; do not disable KVService
while clients still use it. The shared PostgreSQL chart's existing immutable
identity, retention and disable/rollback guards continue to apply. Helm does
not wait for garbage collection when disabling KVService.
