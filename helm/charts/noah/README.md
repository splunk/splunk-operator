# Noah development chart

This chart runs a private Noah development stack entirely inside a Kraken
vCluster:

- one Noah server;
- a PostgreSQL 15 StatefulSet with persistent storage;
- an ephemeral Redis instance;
- a single-node MinIO SmartStore service with persistent storage; and
- the Noah migration image as an init container.

It deliberately excludes ingress, a load balancer, Istio, autoscaling, a Pod
Disruption Budget and NetworkPolicies. Noah uses mock authentication and creates
active tenants on first access with `NOAH_AUTO_CREATE_TENANTS=true`, so it does
not require Vault or Noah's tenant-lifecycle SQS queue. Local mode reads optional
per-tenant overrides from the chart-managed `data/config.yaml`; the default file
is an empty map.

Kraken's SmartBus SQS queue and S3 bucket are separate from Noah. Splunk
workloads consume those resources through the `splunk-runtime` ServiceAccount;
Noah neither uses that ServiceAccount nor accesses SmartBus.

All chart workloads select and tolerate Kraken's `workload=splunk` node pool,
matching the placement used by vCluster CoreDNS and Splunk workloads. This is
required for reliable virtual Service and DNS routing in Kraken.

## Install

From the repository root, create and configure the vCluster:

```console
make noah-local-cluster
```

Then install Noah in `splunk-operator`. Cluster setup adds Kraken's
deployment-scoped Artifactory Secret to that namespace and its `default`
ServiceAccount, which all chart workloads use. No separate manual copy, rename
or pod-level reference is required:

```console
make noah-local-deploy
```

The chart pins the Noah server and migration images to the same published
build. Override both tags together when testing a different build:

```console
make noah-local-deploy NOAH_LOCAL_HELM_ARGS='--set image.tag=<version> --set migrationImage.tag=<version>'
```

`NOAH_EXPECTED_NUMBER_OF_PODS` is set from `noah.expectedPods`. This README
describes it as Noah server pods (used to shard Noah's rate limit), but the
spike branch's `skaffold.yaml` bound the same variable to the indexer replica
count. Noah's source is not in this repository, so the two readings cannot be
settled here; confirm with the Noah team before relying on either. Nothing
validates the value.

The chart also sets `NOAH_CACHE_WARM_SCALE_IN_TIMEOUT=5m` so integrated Splunk
peers remain searchable and cache-warm scale-in processing is enabled.

PostgreSQL requests the `gp3-automode` StorageClass exposed by
`tools/noah-local-dev/kraken-request.yaml`. Override storage in a private values
file if required:

```yaml
postgresql:
  persistence:
    storageClass: gp3-automode
    size: 20Gi
```

This chart is intentionally single-replica: each Noah pod runs the migration
init container, so scaling it requires moving migrations into a separately
coordinated deployment step.

PostgreSQL database, user and password values are initialization settings. On
upgrade, the chart reuses the credentials already stored by the release so they
cannot drift from a retained database volume. Changing them requires an explicit
database credential migration.

`postgresql.auth.password` is empty in `values.yaml` so that no database
credential lives in the repository or in Helm release history. On first install
the chart generates a random 32-character password; every later `helm upgrade`
reads it back out of the release Secret. Read it with:

```console
kubectl --context kraken -n splunk-operator get secret noah-database \
  -o jsonpath='{.data.NOAH_DB_PASSWORD}' | base64 -d
```

To pin a known value instead, set it in an untracked values file and pass it
with `--values`. Do not commit real credentials.

MinIO provides the S3-compatible remote store required for an end-to-end Noah
bucket-routing test. The chart creates the `noah-smartstore` bucket and exposes
it at `http://noah-minio:9000`. Its credentials are stored in the `noah-minio`
Secret and preserved across Helm upgrades. The bundled C3 fixture uses a
cluster-specific `s3://noah-smartstore/c3` path; do not share that path
with another active indexer cluster.

`minio.auth.rootPassword` is also empty by default and generated on first
install. Set it in an untracked values file only if a known local credential is
needed. The pinned MinIO images are intended only for this isolated development
environment, and MinIO is not exposed outside the vCluster.

Create the Splunk custom resources in the same namespace using Kraken's
SmartBus-enabled `splunk-runtime` ServiceAccount. A locally run operator can
watch `splunk-operator` without running inside the vCluster.
