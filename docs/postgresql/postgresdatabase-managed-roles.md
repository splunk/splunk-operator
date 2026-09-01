---
title: PostgresDatabase Managed Roles
parent: PostgreSQL
nav_order: 7
---

# PostgresDatabase Managed Roles

A `PostgresDatabase` declares one or more application databases for an existing
`PostgresCluster`. For each `spec.databases[]` entry, the operator manages two
PostgreSQL login roles:

- `<database>_admin` (admin/database owner)
- `<database>_rw` (read-write application role)

These are the defaults. Set `adminRoleName` and/or `rwRoleName` on an entry in
`spec.databases[]` when a consumer requires tenant-derived role names. Each
configured name must be a valid PostgreSQL identifier, must not use a
PostgreSQL or CloudNativePG reserved name, and the two names must differ.

The database controller manages the application resources around those roles:
role Secrets, database connection ConfigMaps, CNPG `Database` resources, and
privilege grants. The cluster controller reconciles the roles into CNPG
`spec.managed.roles` for the referenced `PostgresCluster`.

## Naming options

Both overrides are optional and independent. Omitting either field preserves
the existing derived name for that role:

| Configuration | Admin role | Read-write role |
| --- | --- | --- |
| No overrides | `appdb_admin` | `appdb_rw` |
| `adminRoleName: tenant_app_owner` | `tenant_app_owner` | `appdb_rw` |
| `rwRoleName: tenant_app_rw` | `appdb_admin` | `tenant_app_rw` |
| Both overrides | `tenant_app_owner` | `tenant_app_rw` |

For example, the default configuration remains:

```yaml
databases:
  - name: appdb
```

An application that only needs a tenant-derived read-write role can override
just that role:

```yaml
databases:
  - name: appdb
    rwRoleName: tenant_app_rw
```

An application that needs tenant-derived names for both roles can configure
both fields:

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
      adminRoleName: tenant_app_owner
      rwRoleName: tenant_app_rw
      deletionPolicy: Delete
      extensions:
        - pg_trgm
```

When this resource is ready, the application can use the generated credentials
for `tenant_app_owner` and `tenant_app_rw` and the connection metadata ConfigMap
published by the operator. If the overrides are omitted, the credentials use
`appdb_admin` and `appdb_rw`.

The admin role is the CNPG database owner and the role used for default
privilege grants. The read-write role receives the application grants for
existing and future tables and sequences. External admin and read-write Secret
usernames must match their configured role names.

Role-name overrides are immutable once the database has started provisioning.
Choose the names before creating the database; changing them later requires a
new database entry and an intentional migration of credentials and privileges.

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
    - name: tenant_app_owner
      exists: true
      secretRef:
        name: <admin-secret>
    - name: tenant_app_rw
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
    - tenant_app_owner
    - tenant_app_rw
    roleOwners:
      tenant_app_owner:
        name: <postgresdatabase-name>
        uid: <postgresdatabase-uid>
```

## Managed Secret drift

For operator-generated role Secrets, the operator creates credential data only
for new databases. After a database has been provisioned, the operator does not
regenerate or rewrite Secret data because the live PostgreSQL role and
applications already depend on the existing password.

If a previously provisioned generated Secret is deleted,
`PostgresDatabase.status.conditions` reports `SecretsReady=False` with reason
`ManagedSecretMissing`. Restore the Secret with the original credential data and
the expected name from `status.databases[].adminUserSecretRef` or
`status.databases[].rwUserSecretRef`.

If the expected generated Secret exists but is controlled by another Kubernetes
controller, `SecretsReady=False` reports reason
`ManagedSecretOwnershipConflict`. Remove the conflicting controller owner or
restore operator ownership so the `PostgresDatabase` can reconcile the Secret
metadata without changing its data.

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
