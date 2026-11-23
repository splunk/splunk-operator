---
title: Shared PostgreSQL with Helm
parent: PostgreSQL
nav_order: 0.3
---

# Shared PostgreSQL with Helm

Enable `sharedPostgres.enabled` to provision a shared PostgreSQL backend for
KVService and other database consumers. Each enabled Helm release owns one
cluster.

Applications explicitly reference that cluster; dedicated `PostgresCluster`
resources remain supported.

The release owns a cluster-scoped `PostgresClusterClass`, a namespaced
`PostgresCluster`, and, when configured, an application `PostgresDatabase` and
backup credential Secret. Names incorporate the full release name and namespace
in a stable hash. Keep the release name and namespace when reinstalling. Query
the names from the resources; do not reconstruct the hash.

## Infrastructure and sizing

Defaults are three PostgreSQL instances separated by a **required**
anti-affinity rule on `kubernetes.io/hostname`. Provide at least three eligible
worker nodes, 100Gi of persistent storage per instance, and 2 CPU/8Gi memory
requests per instance (limits 4 CPU/16Gi). Both read-write and read-only
PgBouncer poolers are enabled, each with three pods. Include capacity for six
PgBouncer pods and the controllers. These are starting values, not a workload
sizing guarantee. For a single-instance PostgreSQL deployment, set
`sharedPostgres.connectionPooler.readOnly: false`.

Set `sharedPostgres.storage`, `storageClass`, `resources`, and `affinity` to
match the workload and infrastructure. An empty `storageClass` uses the default
class. Operational settings such as instance count, resources, storage size,
PostgreSQL configuration, exporter switches, pooler enablement and backup
schedules are rendered on `PostgresCluster` and can be updated subject to API
constraints. Storage class, node placement, pooler mode/replica policy, and
backup destination, retention and Secret references belong to the **immutable**
class policy. Select them at installation. The chart rejects later policy
changes before applying resources; use a replacement cluster for those changes.
Credential bytes in an existing referenced Secret can rotate independently. To
enable backups later, configure their destination and Secret policy initially,
even while disabling their schedule with the explicit acknowledgement.

Storage must support the database's durability requirements. Volume expansion
requires a supporting provisioner; changing `storageClass` does not migrate
existing PVCs. Preferred anti-affinity or fewer instances reduces HA guarantees.
Database affinity applies to database pods; PgBouncer uses provider placement.

## Installation

Use the operator/chart build under test and follow the repository's existing
[CRD installation
procedure](../deploy/Helm.md#splunk-operator-helm-chart-repository) first:
install the SOK CRDs matching this operator build with `make install` from
source or the matching release manifest. Install a compatible cert-manager,
including its CRDs and a ready controller/webhook, as described in [the Helm
prerequisites](../deploy/Helm.md#prerequisite-cert-manager). These are cluster
prerequisites; the SOK chart does not install SOK CRDs.

After those prerequisites, one Helm installation creates the shared PostgreSQL
class, cluster, application databases and configured backup Secret. No manual
creation of those workload resources is needed. Enabled CNPG/Barman subcharts
provide their backend definitions and controllers. Leave the cert-manager
subchart disabled when using the prerequisite installation. Installing CRDs and
backend controllers requires cluster administrator permissions.

From source, run `make helm-dependencies` before installing, or use `make
helm-package` for packaged charts.

A missing class is created before cluster admission by a pre-install/pre-upgrade
hook and retained independently of later release manifests. Existing classes are
checked for ownership and policy but never recreated or updated. With validation
already active, enable PostgresController and wait for operator readiness before
enabling shared provisioning.

Create a protected `shared-values.yaml`:

```yaml
splunkOperator:
  featureGates:
    PostgresController: true
cloudnative-pg: {enabled: true}
plugin-barman-cloud: {enabled: true}
cert-manager: {enabled: false}
sharedPostgres:
  enabled: true
  storageClass: encrypted-ssd
  backup:
    enabled: true
    destinationPath: s3://customer-backups/postgres/
    endpointURL: https://s3.example.com
  databases:
    - name: application
      extensions: [pg_stat_statements]
```

After accepting the terms in [the installation guide](../README.md), supply your
accepted `SPLUNK_GENERAL_TERMS`. Set `BACKUP_ACCESS_KEY_FILE` and
`BACKUP_SECRET_KEY_FILE` to protected files containing your storage credentials.

```bash
: "${SPLUNK_GENERAL_TERMS:?Supply your accepted terms value}"
: "${BACKUP_ACCESS_KEY_FILE:?Supply a protected credential file}"
: "${BACKUP_SECRET_KEY_FILE:?Supply a protected credential file}"
helm install shared ./helm-chart/splunk-operator \
  --namespace postgres-system --create-namespace --values shared-values.yaml \
  --set-string splunkOperator.splunkGeneralTerms="$SPLUNK_GENERAL_TERMS" \
  --set-file sharedPostgres.backup.credentials.accessKeyId="$BACKUP_ACCESS_KEY_FILE" \
  --set-file sharedPostgres.backup.credentials.secretAccessKey="$BACKUP_SECRET_KEY_FILE" \
  --wait --timeout 15m
```

Protect values, rendered Secrets and Helm release storage. Use an existing
credential Secret through `sharedPostgres.backup.credentials.existingSecret` if
managed externally. Omitting backups requires `backup.acknowledgeNoBackups:
true`. External Secrets live in the database namespace; configure
`accessKeyIdKey` and `secretAccessKeyKey` as needed. Supply one credential
source. The generated Secret is retained and reused on reinstall when credential
values are omitted.

Backups use the existing API's S3-compatible Barman destination and five-field
cron schedule. Native Azure/GCS credential APIs are not exposed by this chart.
Every destination advertised for a deployment must pass backup **and restore**
acceptance against that destination. An S3-compatible fixture does not qualify
every vendor or endpoint. Disabling backups provides no recovery protection.

For existing compatible CNPG/Barman/cert-manager installations, leave their
subcharts disabled. The PostgreSQL feature gate remains enabled. See [backend
setup](../deploy/Helm.md#postgresql-backend-controllers). Do not install two
controllers managing the same backend resources.

The optional ValidationWebhook gate may be enabled after the operator's webhook
Service and serving certificate are ready. A fresh install with that gate and
workload resources in the same Helm transaction can encounter an unavailable
webhook. CRD validation and reconciliation validation remain active without it.

## Images and upgrades

PG17 is the default, preserving compatibility with future migrations from
non-Kubernetes Splunk deployments. PG18 is an explicit option for installations
that do not need that compatibility: set `sharedPostgres.postgresVersion: "18"`
on initial installation. Migration tooling and testing are outside this feature.

The [artifact catalog](public-artifacts.md), `postgresok-ac78e560-43371564`,
supplies pinned public `tag@sha256` references for PG17, PG18 and PgBouncer.
Backend charts use the same release. Use `sharedPostgres.images` for private
mirror references and `imagePullSecrets` for namespace-local pull credentials;
preserve the tested tag and digest. Backend mirror keys are documented in the
backend setup guide.

Keep the selected major on every upgrade. Use the saved values file or reuse the
previous values, as in this example:

```bash
helm upgrade shared ./helm-chart/splunk-operator \
  --namespace postgres-system --reuse-values --wait --timeout 15m
```

The chart rejects a changed major against an existing shared cluster using a
live lookup, and sets `postgresMajorUpgradeConfig.allow: false`. Offline
rendering cannot perform that lookup. Do not bypass controller/admission
protections. An intentional major upgrade uses the existing documented
PostgreSQL upgrade workflow; reconcile the chart values with its completed major
before the next Helm upgrade. Chart default changes must never select a new
major implicitly.

The bundled CNPG and Barman charts render their CRDs as Helm templates. When
CRD creation is enabled, Helm installs them and updates them on upgrade. These
CRDs carry `helm.sh/resource-policy: keep`, so they remain when a backend is
disabled or the release is uninstalled. Review backend CRD changes before
upgrading. SOK CRDs remain separately installed prerequisites.

Disabling `sharedPostgres.enabled` after provisioning is enabled is rejected to
preserve Helm rollback. Uninstall the release to retain its data instead. A
release-state Secret in the Helm release namespace records the backend name and
namespace. Upgrades reject changes to this identity, including changing
`namespaceOverride` while provisioning stays enabled. State without identity
metadata requires verifying the existing backend before continuing. The Secret
is removed on uninstall. Keep rollback targets at or after the revision that
enabled provisioning: Helm rollback reuses stored manifests and cannot run this
upgrade guard.

## Database consumers

Find the shared cluster and wait for readiness:

```bash
kubectl get postgrescluster -n postgres-system \
  -l app.kubernetes.io/instance=shared
kubectl wait -n postgres-system --for=condition=Ready \
  postgrescluster/<shared-cluster-name> --timeout=15m
```

Additional application database definitions go in `sharedPostgres.databases`
(maximum ten entries per resource). They default to `deletionPolicy: Retain`.
Other `PostgresDatabase` resources explicitly set `spec.clusterRef.name` to the
shared cluster in the same namespace. Dedicated clusters use their own
reference.

Optionally enable `sharedKVService.enabled` to deploy KVService in the same Helm
release; see [KVService through Helm](../operate/KVServiceHelm.md). KVService stays
disabled by default. For a separately managed KVService, configure its
`spec.postgres.clusterRef.name` with the shared `PostgresCluster` name in the same
namespace. Read the generated name from the release's resources; see
[KVService configuration](../operate/KVService.md#namespace-driven-tenant-provisioning).

Wait for the application `PostgresDatabase` Ready condition. Its
`status.databases[]` references the connection ConfigMap and credential Secrets.
Use `CLUSTER_RW_ENDPOINT` for direct SQL or `CLUSTER_POOLER_RW_ENDPOINT` for the
pooler. Mount the CA Secret identified by the cluster connection ConfigMap's
`SERVER_CA_SECRET_REF`, and use `sslmode=verify-full`. Verify an authenticated
write/read through both endpoints. Never print passwords or put them in shell
arguments. A Ready condition alone does not establish SQL connectivity.

## Restore, failover and retained resources

For object-store recovery, follow [backup and
restore](backup-object-storage.md). Keep the source archive and its credentials.
Create a replacement cluster with the same image major, backup destination and
credential references, and `spec.bootstrapFrom.objectStorage.serverName` equal
to the source backend server name. The Helm equivalent for a **new release** is
`sharedPostgres.bootstrapFrom.objectStorage.serverName`. Do not change the
immutable bootstrap source on an existing cluster. Restore-only releases still
need the object-store destination and credentials when using object-storage or
snapshot `walArchive` recovery, even with scheduled backups disabled.

After restoration completes, create replacement application database resources
referencing the replacement cluster, wait for their published credentials, and
update consumers to their new connection metadata. `PostgresDatabase.clusterRef`
is immutable; retaining and recreating the resource or using a new resource name
is required. Verify the restored rows through the consumer endpoint before
retiring the original database.

For HA acceptance, write a marker, remove the current primary **on a disposable
test cluster**, wait for a different healthy primary and all instances, then
verify the marker and new writes through direct and pooled endpoints. Failover
availability and durability depend on infrastructure and replication settings;
three replicas and placement are not a zero-data-loss guarantee.

Helm uninstall retains the class, shared cluster, application database and
generated backup Secret. Their controller-owned storage and database credentials
remain attached to the retained resources. Reinstall with the same release name,
namespace, values and major to resume management. Verify row contents, PVC
identities and credential bytes through uninstall/reinstall and same-major
upgrades. Deleting the namespace destroys namespaced retained data.

Uninstalling bundled CNPG/Barman removes their controllers. Retained database
pods may keep serving, but failover, repair, certificate maintenance and backups
are not managed until those controllers return. Plan the maintenance window
accordingly.

Destructive cleanup is separate from uninstall. While SOK and the backend
controllers still run, explicitly set application databases' deletion policies
to Delete and delete those resources after removing their consumers. Then set
the shared cluster's `spec.clusterDeletionPolicy` to Delete and delete the
cluster. Verify backend cluster/PVC cleanup before deleting its class and the
retained backup Secret. External backup objects and external credential Secrets
require their own owner-approved cleanup. Never remove finalizers as a cleanup
shortcut.

## Metrics and acceptance evidence

Both existing exporters are enabled by default. Scrape the PostgreSQL pod
annotations on port 9187 and the PgBouncer exporter on port 9127, subject to
your network policy and scraper configuration. Verify actual scrape targets and
query results in the customer's monitoring system; exporter configuration alone
does not establish ingestion. No monitoring stack, dashboards or rules are
added. See [custom metrics](custom-metrics.md) for coverage and privileges.

The isolated `kuttl/kuttl-test-helm-shared-postgresql.yaml` suite exercises the
Helm lifecycle on an explicitly authorized disposable cluster. Run the suite
with both PG17 and PG18 against each backup endpoint you need to qualify. Its
context, values, image, three-node storage capacity, credentials and accepted
terms are prerequisites. Static tests and compiled scripts do not qualify these
live acceptance criteria. The scenario first starts the operator with validation
webhooks enabled, proves that admission rejects a missing class, then enables
shared provisioning through Helm upgrade. Disabling provisioning must fail
without creating a release revision. Consecutive rollbacks preserve unchanged
workloads, and reinstall also exercises reuse of the retained class.

From the repository root, after explicitly authorizing a disposable context:

```bash
export HELM_POSTGRES_CONTEXT=<authorized-context>
export HELM_POSTGRES_NAMESPACE=<authorized-disposable-namespace>
export HELM_POSTGRES_VALUES=<absolute-path-to-protected-backup-values>
export HELM_REPO_PATH="$PWD/helm-chart"
export KUTTL_SPLUNK_OPERATOR_IMAGE=<tested-operator-image>
export HELM_POSTGRES_MAJOR=17 # repeat on a fresh disposable cluster with 18
export HELM_POSTGRES_UPGRADE_IMAGE=<distinct-tested-same-major-tag@sha256-reference>
make helm-package
make helm-shared-postgresql-test
```

The protected values file must enable backups and supply its destination and
credentials (or an existing Secret). Before running this suite, install the SOK
CRDs matching the tested build and a compatible, ready cert-manager as separate
prerequisites. The scenario leaves cert-manager outside the SOK release. The
upgrade image must match the selected major and have a different digest from the
initial image. The dedicated target validates the context before KUTTL starts
and gives KUTTL a private, flattened kubeconfig and the explicit authorized
namespace. It checks that the prerequisite SOK and cert-manager CRDs are
established before creating that namespace. The harness pins all commands to
that context and namespace, verifies that all three database pods run the
upgraded image, and requires fresh backend CRDs. It preserves fixtures after
failure; inspect them before deliberate cleanup. Successful runs explicitly
delete only the test's database resources before uninstalling the release.
Backend CRDs remain, so a subsequent fresh-install run needs a new cluster.

Calling KUTTL directly or using the generic `helm-kuttl-test` target bypasses
the required preflight. Use the dedicated target for this suite.
