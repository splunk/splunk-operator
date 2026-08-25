---
title: PostgresDatabase Managed Roles
parent: PostgreSQL
nav_order: 7
---

# PostgresDatabase Managed Roles

A `PostgresDatabase` declares one or more application databases for an existing
`PostgresCluster`. For each `spec.databases[]` entry, the operator manages two
PostgreSQL login roles:

- `<database>_admin`
- `<database>_rw`

The database controller manages the application resources around those roles:
role Secrets, database connection ConfigMaps, CNPG `Database` resources, and
privilege grants. The cluster controller reconciles the roles into CNPG
`spec.managed.roles` for the referenced `PostgresCluster`.

## Example

```yaml
apiVersion: platform.splunk.com/v1alpha1
kind: PostgresDatabase
metadata:
  name: myapp-db
  namespace: myapp
spec:
  clusterRef:
    name: shared-postgres
  databases:
    - name: appdb
      deletionPolicy: Delete
      extensions:
        - pg_trgm
```

When this resource is ready, the application can use the generated credentials
for `appdb_admin` and `appdb_rw` and the connection metadata ConfigMap published
by the operator.

## Reconciliation status

Role intent is visible on the `PostgresDatabase`:

```bash
kubectl get postgresdatabase <name> -n <namespace> -o yaml
```

Look for:

```yaml
status:
  databases:
  - name: appdb
    roles:
    - name: appdb_admin
      exists: true
      secretRef:
        name: <admin-secret>
    - name: appdb_rw
      exists: true
      secretRef:
        name: <rw-secret>
```

Cluster-side role reconciliation is visible on the referenced `PostgresCluster`:

```bash
kubectl get postgrescluster <cluster> -n <namespace> -o yaml
```

Look for:

```yaml
status:
  managedRolesStatus:
    reconciled:
    - appdb_admin
    - appdb_rw
    roleOwners:
      appdb_admin:
        name: <postgresdatabase-name>
        uid: <postgresdatabase-uid>
```

## Role ownership conflicts

A PostgreSQL role name can have only one owning `PostgresDatabase`. If multiple
`PostgresDatabase` resources claim the same role name, the cluster records the
conflict in `PostgresCluster.status.managedRolesStatus.conflicts`. The affected
`PostgresDatabase` resources report `RolesReady=False` with reason
`RoleConflict`.

To resolve the conflict, rename or remove the duplicate database entry, then wait
for both the `PostgresCluster` and affected `PostgresDatabase` resources to
reconcile.

## Deletion behavior

`deletionPolicy` controls what happens when the `PostgresDatabase` is deleted:

- `Delete` removes the CNPG `Database`, generated Secrets, ConfigMaps, and the
  managed PostgreSQL login roles.
- `Retain` orphans the application resources and leaves the PostgreSQL database
  and roles in place.

For `Delete`, the `PostgresDatabase` remains finalizing until the referenced
`PostgresCluster` stops reporting ownership of the deleted roles.
