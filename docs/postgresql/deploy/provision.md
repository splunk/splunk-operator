---
title: Enable shared PostgreSQL
parent: Deploy PostgreSQL
nav_order: 3
---

# Enable shared PostgreSQL

Before you start, [check the controllers](install.md#check-the-installation). Then [choose the shared cluster policy](cluster-policy-reference.md#choose-shared-cluster-policy). Helm creates the class and cluster. Do not create them directly. For a new deployment, leave `sharedPostgres.databases` empty.

## Configure backups before enabling the cluster

If you need to recover data, configure Barman object storage before you first enable shared PostgreSQL. The shared Helm values do not configure volume snapshot backups. Retaining the cluster on deletion is not a backup.

Set a limit on how much recent data you can lose. Set a limit on how long the service can be down. Keep backups long enough to cover failures that take time to detect. More frequent full backups can reduce how much log data PostgreSQL must replay. Measure data loss and downtime in a restore drill. A backup schedule or archive interval alone does not guarantee either result.

If Barman is not installed, enable it during [controller installation](install.md#install-or-enable-postgresql-through-helm). Make sure that the plugin is ready and that Kubernetes recognizes its `ObjectStore` resource:

```bash
kubectl get crd objectstores.barmancloud.cnpg.io
```

Prepare an S3-compatible bucket and a prefix for this cluster. Grant permission to list the bucket. Grant read, write, and delete access to objects in the backup location. Make sure that the PostgreSQL pods can reach the object-storage endpoint.

Create a Secret named `postgres-backup-credentials` through your usual secret management process. Use the shared cluster namespace. This is the Helm release namespace unless you use `namespaceOverride`. Use the keys `accessKeyId` and `secretAccessKey`, or set the key names below to match your Secret. Do not put the credentials themselves in Helm values.

Merge this example into your release values. Replace the destination with your bucket and prefix. Set the schedule and retention to meet your recovery requirements:

```yaml
sharedPostgres:
  backup:
    enabled: true
    destinationPath: s3://your-backup-bucket/postgresql/shared/
    schedule: "0 2 * * *"
    retentionPolicy: "30d"
    credentials:
      existingSecret: postgres-backup-credentials
      accessKeyIdKey: accessKeyId
      secretAccessKeyKey: secretAccessKey
```

Use a five-field cron schedule. The example schedules a daily base backup at 02:00. If your S3-compatible service needs a custom endpoint, set `sharedPostgres.backup.endpointURL`. Choose the destination and Secret references before you enable the cluster. You cannot change them on the existing class or add a destination to a class that has none.

If the data is disposable and does not need recovery, use this alternative instead:

```yaml
sharedPostgres:
  backup:
    enabled: false
    acknowledgeNoBackups: true
```

Helm refuses to enable a shared cluster without backups unless you explicitly acknowledge that choice.

### Prepare for a restore drill

If you configured backups, plan the drill for a separate PostgreSQL cluster managed by the existing operator. Do not install a second operator or change the source cluster or its Helm release for the drill.

Use the same PostgreSQL major version for the restore target. Reserve enough storage and node capacity for it. Make sure that it can reach the source archive. Provide credentials that can read the archive. Once the source cluster is ready, record its archive location and source server name. Then [check the first backup and test recovery](verify.md#check-backups-and-test-recovery).

## Enable the shared Helm cluster

Use the Helm release that owns the controllers. For a new release, first complete [controller installation](install.md#install-or-enable-postgresql-through-helm) with shared PostgreSQL off. Wait for admission to be ready before you enable it.

Set `sharedPostgres.enabled: true` in the release configuration. Set `sharedPostgres.storageClass`, `sharedPostgres.storage`, and `sharedPostgres.instances` to fit your cluster. Check `sharedPostgres.connectionPooler` and `sharedPostgres.monitoring` against your deployment choices.

Keep the [backup settings you chose above](#configure-backups-before-enabling-the-cluster) in the same values file.

For the new release from [installation](install.md#apply-the-helm-configuration), update `postgres-operator.yaml`. Repeat the Helm command with the same chart source, version, release name, and namespace. If you used a different procedure to install the release, follow its upgrade procedure. Keep the other release values unchanged.

Use the release label to find the name that Helm generated for the cluster. Set `RELEASE` to the Helm release name. Set `NAMESPACE` to the cluster namespace. Use the release namespace unless you set `namespaceOverride`.

```bash
: "${RELEASE:?Set the Helm release name}"
: "${NAMESPACE:?Set the namespace of the shared cluster}"
CLUSTER="$(kubectl get postgresclusters -n "$NAMESPACE" \
  -l "app.kubernetes.io/instance=$RELEASE" -o json \
  | jq -er '.items | if length == 1 then .[0].metadata.name else error("expected one shared cluster for the release") end')" || exit 1
export CLUSTER NAMESPACE
kubectl wait -n "$NAMESPACE" --for=jsonpath='{.status.phase}'=Ready \
  "postgrescluster/$CLUSTER" --timeout=10m
```

Use `jq` for the lookup above. Do not guess the generated name.
Do not disable shared provisioning or change the namespace or release identity after provisioning.
Do not point rollback at a revision from before the shared cluster existed.
When the cluster is ready, [check the deployment](verify.md).
