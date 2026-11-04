---
title: Database connections for Splunk sidecars
parent: PostgreSQL
nav_order: 2.2
---

# Database connections for Splunk sidecars

A Splunk-managed sidecar runs alongside `splunkd` inside the `splunk` container.
It is not a separate Kubernetes container.

The operator mounts existing PostgreSQL connection metadata, passwords, and a CA certificate into the container.
A CA certificate identifies the authority that signs the server certificate.
The sidecar must read these files and configure its database client.
Adding a database mount does not change which Splunk features are supported on Kubernetes.

## Prepare the environment

Before creating resources, enable `PostgresController=true` on the Splunk Operator deployment.
Install CloudNativePG and the PostgreSQL custom resource definitions.
The [quickstart setup paths](quickstart.md#setup-and-prerequisites) give the supported versions and the checks.
Use an approved `PostgresClusterClass` with `spec.provisioner: postgresql.cnpg.io` and CNPG default certificates.
The [integration guide](integration-patterns.md) describes class creation and ownership.

Use Enterprise `v4` `Standalone` or `SearchHeadCluster` resources for the Splunk workload.
The workload, `PostgresDatabase`, and `PostgresCluster` must use the same namespace.
The examples use the namespace `splunk` and an existing class named `postgresql-dev`.
Replace those names with your approved namespace and class.
Follow the [terms instructions](../README.md#splunk-general-terms-acceptance) before starting Splunk workloads.

The mount requires CNPG default certificates.
The operator gives the CNPG cluster the same name as the PostgresCluster.
It reads `ca.crt` from the Secret named `<PostgresCluster name>-ca`.
Do not use custom certificates unless the mount implementation supports their CA reference.
[CPI-2235](https://splunk.atlassian.net/browse/CPI-2235) tracks changes to CA discovery.

## Create the database

This example uses the bundled PostgreSQL client in Splunk Enterprise 10.2.0.
That client negotiated TLS 1.2 in the local tests.
TLS encrypts the connection, and the client and server must support a common version.
Get platform approval before allowing TLS 1.2 with the configuration below.
If your client supports the platform's TLS-1.3-only default, omit `postgresqlConfig`.

Set `TEST_PG17_IMAGE` to an approved CNPG-compatible PostgreSQL 17 image reference.
The reference must include a version tag that starts with `17` and a complete `@sha256:` digest.
The operator does not select a default image or inherit images from the class.
The [`postgresql-dev` sample class](../../config/samples/platform_v1alpha1_postgresclusterclass_dev.yaml) enables PgBouncer, which reuses database connections.
The quickstart creates a class with the same name that disables PgBouncer.
Make sure that you know which of the two classes your cluster has.
If PgBouncer is enabled, set `TEST_PGBOUNCER_IMAGE` to its approved image reference, also with a tag and SHA-256 digest.
If the registry requires credentials, add `spec.images.imagePullSecrets` with Secret names from the cluster namespace.
The [quickstart image requirements](quickstart.md#3-create-a-postgresql-cluster) describe the image publication and registry requirements.

Save this example as `sidecar-database.yaml`:

```yaml
apiVersion: platform.splunk.com/v1alpha1
kind: PostgresCluster
metadata:
  name: sidecar-postgres
  namespace: splunk
spec:
  class: postgresql-dev
  postgresVersion: "17"
  images:
    postgresql:
      "17": ${TEST_PG17_IMAGE}
    pgbouncer: ${TEST_PGBOUNCER_IMAGE}
    # Optional registry credentials must exist in this namespace.
    # imagePullSecrets:
    #   - name: registry-reader
  postgresqlConfig:
    ssl_min_protocol_version: TLSv1.2
    ssl_max_protocol_version: TLSv1.3
---
apiVersion: platform.splunk.com/v1alpha1
kind: PostgresDatabase
metadata:
  name: sidecar-database
  namespace: splunk
spec:
  clusterRef:
    name: sidecar-postgres
  databases:
    - name: sidecardb
      deletionPolicy: Retain
```

Render the image variables before applying the YAML:

```bash
: "${TEST_PG17_IMAGE:?Set the approved PostgreSQL 17 tag@sha256 reference}"
: "${TEST_PGBOUNCER_IMAGE:?Set the approved PgBouncer tag@sha256 reference}"
envsubst '${TEST_PG17_IMAGE} ${TEST_PGBOUNCER_IMAGE}' \
  < sidecar-database.yaml > sidecar-database.rendered.yaml
```

Make sure that the rendered file contains the approved image references.
Apply the rendered resources through your authorized deployment process.
Wait until the cluster and database report `Ready`:

```bash
kubectl wait -n splunk --for=jsonpath='{.status.phase}'=Ready \
  postgrescluster/sidecar-postgres postgresdatabase/sidecar-database --timeout=10m
```

The operator publishes references to the connection ConfigMap and credential Secrets in `PostgresDatabase.status.databases[]`.
A ConfigMap contains non-secret connection metadata.
A Secret contains credentials or certificate data.

The generated `sidecardb_rw` role can read and change application data.
The generated `sidecardb_admin` role owns the database and can change its schema.
A schema defines the database's tables and related objects.
See [managed roles](postgresdatabase-managed-roles.md) for role names and permissions.

## Mount the connection into Splunk

Add `postgresDatabaseMounts` to the Splunk workload specification.
The `database` field selects the declared database name, not the Kubernetes resource name.
The entry's `name` identifies the mount request and does not determine file names.

```yaml
apiVersion: enterprise.splunk.com/v4
kind: Standalone
metadata:
  name: sidecar-search
  namespace: splunk
spec:
  image: splunk/splunk:10.2.0
  replicas: 1
  postgresDatabaseMounts:
    - name: sidecar-connection
      postgresDatabaseRef:
        name: sidecar-database
      database: sidecardb
```

For a search head cluster, use `kind: SearchHeadCluster` and `replicas: 3` with the same mount entry.
The operator mounts the connection into each search head member, not the deployer.
The operator exposes the existing ConfigMap and Secrets as one directory, called a projection.
It does not mount this projection into other containers or init containers.
It does not copy the ConfigMap or Secrets or change their ownership.

The operator mounts the read-write password by default.
If the consumer must create or change schema objects, add `includeAdmin: true` to its mount entry.
This exposes the database-owner password to processes in the Splunk container.
Use the read-write role for ordinary data operations.
If you omit `includeAdmin`, it defaults to false.
A change to `postgresDatabaseMounts` updates the pod template, so the operator restarts the Splunk pods.

The operator mounts the projection read-only at `/opt/splunk/var/run/supervisor/postgresok`.
For each mounted database, the projection contains these files:

| Path | Content |
|---|---|
| `<database>/CLUSTER_RW_ENDPOINT`, `<database>/DEFAULT_CLUSTER_PORT` | Read-write host and port |
| `<database>/DATABASE_NAME`, `<database>/RW_USER_NAME`, `<database>/ADMIN_USER_NAME` | Database and role names |
| `<database>-rw` | Read-write password |
| `<database>-admin` | Owner password, only with `includeAdmin: true` |
| `<database>-ca.crt` | CA certificate for `verify-full` |

The operator does not mount the other ConfigMap keys, such as the read-only or pooler endpoints.

## Check the connection

This shell function runs SQL from standard input inside a Splunk pod.
It uses only the mounted files and the bundled client.
The bundled `psql` needs `LD_LIBRARY_PATH=/opt/splunk/lib`.
The first argument is the role, `rw` or `admin`.
Use `admin` only when `includeAdmin` is true.
The second argument is the database name.
It is optional, and the default is `sidecardb`.

```bash
# Search head cluster members are named splunk-<name>-search-head-<n>.
POD=splunk-sidecar-search-standalone-0
probe() {
  kubectl exec -i -n splunk "$POD" -c splunk -- sh -c '
    R=/opt/splunk/var/run/supervisor/postgresok D=/opt/splunk/var/run/supervisor/postgresok/$2
    if [ "$1" = admin ]; then U=ADMIN_USER_NAME; else U=RW_USER_NAME; fi
    LD_LIBRARY_PATH=/opt/splunk/lib PGSSLMODE=verify-full PGSSLROOTCERT="$R/$2-ca.crt" \
    PGHOST="$(cat "$D/CLUSTER_RW_ENDPOINT")" PGPORT="$(cat "$D/DEFAULT_CLUSTER_PORT")" \
    PGDATABASE="$(cat "$D/DATABASE_NAME")" PGUSER="$(cat "$D/$U")" PGPASSWORD="$(cat "$R/$2-$1")" \
    /opt/splunk/bin/psql -X -w -At -v ON_ERROR_STOP=1' sh "$1" "${2:-sidecardb}"
}
echo 'SELECT current_database(), current_user;
SELECT ssl, version FROM pg_stat_ssl WHERE pid = pg_backend_pid();' | probe rw
```

The expected output is `sidecardb|sidecardb_rw`, then `t|TLSv1.2` or `t|TLSv1.3`.
A certificate or hostname error is a failed check.
Do not disable verification. Fix the mount or the endpoint.

## Sidecar configuration

The operator keeps the legacy embedded PostgreSQL sidecar disabled through `SPLUNK_NODE_SIDECAR_POSTGRES_DISABLED=true`.
Keep that database process disabled when the consumer uses managed PostgreSQL.
Enable the sidecar through its supported Splunk configuration.
The database mount does not start the sidecar or configure its client.
