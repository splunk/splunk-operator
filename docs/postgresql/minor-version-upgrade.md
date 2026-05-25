---
title: Minor Version Upgrades
parent: PostgreSQL
nav_order: 3
---

## PostgreSQL Minor Version Upgrades

This document describes how to perform a PostgreSQL minor version upgrade for a Splunk Operator `PostgresCluster`, what upgrade behaviors are available, and what level of interruption to expect.

Examples here use `15.10 -> 15.12`, but the same process applies to any minor version upgrade within the same PostgreSQL major version.

Backups and major-version upgrades are not covered here.

### How minor upgrades work

Minor version changes are driven by the effective PostgreSQL version for the `PostgresCluster`.

That version can be defined in either field:

- `PostgresClusterClass.spec.config.postgresVersion`
- `PostgresCluster.spec.postgresVersion`

If both are set, the `PostgresCluster` value is the effective override used for that cluster.

In practice:

- use `PostgresClusterClass.spec.config.postgresVersion` to define the default version for new clusters created from that class
- use `PostgresCluster.spec.postgresVersion` to change the version of an existing cluster through its manifest

When that field changes:

- the Postgres Operator updates the managed CNPG `Cluster`
- CNPG performs a rolling update
- `PostgresCluster.status.phase` moves to `Configuring` while the upgrade is in progress
- the cluster returns to `Ready` when CNPG reports a healthy state again

Editing the CNPG `Cluster` directly is not recommended, as the operator will overwrite changes or not recognize them for status reporting.
(See the [CNPG documentation ](https://cloudnative-pg.io/docs/1.28/postgres_upgrades#minor-version-upgrades) for more details on how CNPG handles minor version upgrades.)

### Upgrade options

There are two supported CNPG methods for a minor upgrade:

| Option       | How it works                                                          | Expected impact                                                                    | Recommended use                                                              |
| ------------ | --------------------------------------------------------------------- | ---------------------------------------------------------------------------------- | ---------------------------------------------------------------------------- |
| `restart`    | CNPG restarts the current primary in place after replicas are updated | noticeable write interruption while the primary restarts                           | development, test, or maintenance windows where brief downtime is acceptable |
| `switchover` | CNPG promotes an upgraded replica and moves the primary role          | shorter client-visible interruption than `restart`, especially with pooler routing | production or lower-downtime environments                                    |

For the lowest client-visible interruption, use:

- `primaryUpdateMethod: switchover`
- at least `3` instances
- the RW pooler endpoint for application traffic

### What downtime to expect

Minor upgrades are not interruption-free in all configurations.

Observed results from validation runs:

| Approach                     | Client path       | Primary outcome                                                         | Max observed unavailability while testing |
| ---------------------------- | ----------------- | ----------------------------------------------------------------------- | ----------------------------------------- |
| `restart`                    | direct RW service | primary stayed on `postgresql-cluster-dev-1`                            | about `23s`                               |
| `switchover` + `3` instances | RW pooler         | primary switched `postgresql-cluster-dev-1 -> postgresql-cluster-dev-2` | about `11s`                               |

These numbers are not a hard guarantee. Actual interruption depends on cluster health, node performance, storage behavior, image pull time, and how clients reconnect.

### How to proceed safely

Before starting the upgrade:

- make sure the operator runs with `PostgresController` enabled
- make sure the cluster is healthy and `Ready`
- use a same-major patch version change, for example `15.10 -> 15.12` (not `15.10 -> 16.2`)
- ensure client applications retry transient connection failures
- have a backup and recovery plan in case of unexpected issues

Check that the operator has the required feature gate:

```bash
kubectl get deployment -n splunk-operator splunk-operator-controller-manager \
  -o jsonpath='{.spec.template.spec.containers[0].args}{"\n"}'
```

Expected argument:

```text
--feature-gates=PostgresController=true
```

### Recommended configuration

For lower-downtime upgrades, use a `PostgresClusterClass` similar to:

```yaml
apiVersion: enterprise.splunk.com/v4
kind: PostgresClusterClass
metadata:
  name: postgresql-prod
spec:
  provisioner: postgresql.cnpg.io
  config:
    instances: 3
    storage: 20Gi
    postgresVersion: "15.10"
    connectionPoolerEnabled: true
  cnpg:
    primaryUpdateMethod: switchover
    connectionPooler:
      instances: 2
      mode: transaction
```

If brief write downtime is acceptable, a restart-based configuration can stay simpler:

```yaml
apiVersion: enterprise.splunk.com/v4
kind: PostgresClusterClass
metadata:
  name: postgresql-dev
spec:
  provisioner: postgresql.cnpg.io
  config:
    instances: 1
    storage: 10Gi
    postgresVersion: "15.10"
  cnpg:
    primaryUpdateMethod: restart
```

Important:

- `PostgresClusterClass` is immutable after creation
- if you want to move from `restart` to `switchover`, create a new class and point a new cluster at it
- `switchover` requires replicas, so it is not a good fit for single-instance clusters

### Upgrade procedure

For an existing cluster, trigger the version change by updating `PostgresCluster.spec.postgresVersion` in the version-controlled `PostgresCluster` manifest and applying it with `kubectl apply`.

1. Note the current PostgreSQL version.
2. Select the write endpoint that matches your update method:
   - direct RW service for `restart`
   - RW pooler for `switchover`
3. Update the tracked `PostgresCluster` YAML with the target version.
4. Review and commit the manifest change according to your normal change-management process.
5. Apply the manifest.
6. Watch the cluster status and events.
7. Confirm the cluster returns to `Ready` and the application behaves normally.

Example variables:

```bash
export NS=test
export CLUSTER=postgresql-cluster-dev
```

Update the tracked `PostgresCluster` manifest:

```yaml
apiVersion: enterprise.splunk.com/v4
kind: PostgresCluster
metadata:
  name: postgresql-cluster-dev
spec:
  class: postgresql-dev
  postgresVersion: "15.12"
```

Apply the manifest:

```bash
kubectl apply -n $NS -f path/to/postgrescluster.yaml
```

Use the `PostgresCluster` manifest rather than `kubectl patch`, which bypasses normal manifest review and change history.

Watch progress:

```bash
kubectl get postgrescluster -n $NS $CLUSTER -w
kubectl get cluster -n $NS $CLUSTER -w
kubectl get events -n $NS --sort-by=.lastTimestamp
```

After the cluster returns to `Ready`, validate the application path that matters for your environment. For example, confirm that the application can connect, read existing data, and perform expected writes. Direct `psql` checks may be useful for troubleshooting but are not required.

### What to monitor during the upgrade

At the `PostgresCluster` level, expect:

- `status.phase=Configuring` during the upgrade
- `status.phase=Ready` after the upgrade completes

Common events include:

- `ClusterUpdateStarted`
- `ClusterReady`

You may also see additional namespace events from CNPG and Kubernetes, such as:

- CNPG lifecycle events like `UpgradingInstance` and `Switchover`
- `PostgresDatabase` readiness events
- normal pod lifecycle events such as `Scheduled`, `Pulling`, `Started`, and `Killing`

`ClusterDegraded` is not part of the intended steady-state upgrade sequence, but it may still appear during adjacent reconciliation transitions.

### Rollback to the earlier patch version

Rolling back from a higher patch version to an earlier patch version within the same major version is supported.
The process is the same as the upgrade: revert `spec.postgresVersion` in the tracked `PostgresCluster` manifest, review and commit the change, then apply it. Treat this as a validation or recovery procedure, not as a preferred steady-state operating pattern.
