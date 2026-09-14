---
title: PostgresDatabase options
parent: PostgreSQL
nav_order: 7.5
---

# PostgresDatabase options

Each `PostgresDatabase.spec.databases[]` entry can configure PostgreSQL-native
database properties. The operator maps these fields to the CNPG `Database`
resource and continues to derive its owner from `adminRoleName`, or
`<name>_admin` when that field is omitted. Direct owner configuration is not
supported.

```yaml
apiVersion: platform.splunk.com/v1alpha1
kind: PostgresDatabase
metadata:
  name: application-databases
spec:
  clusterRef:
    name: application-cluster
  databases:
    - name: reporting
      adminRoleName: reporting_owner
      template: template0
      encoding: UTF8
      localeProvider: icu
      icuLocale: en-US
      connectionLimit: 20
      allowConnections: true
      deletionPolicy: Delete
```

## Creation-only options

The following fields are fixed when a database entry is admitted and cannot be
added, removed, or changed later:

| Field | Purpose | PostgreSQL availability |
|---|---|---|
| `template` | Source template database | Supported versions |
| `encoding` | Character-set encoding | Supported versions |
| `locale` | Default collation and character classification | Supported versions |
| `localeProvider` | Locale implementation | PostgreSQL 16+ |
| `localeCollate` | `LC_COLLATE` | Supported versions |
| `localeCType` | `LC_CTYPE` | Supported versions |
| `icuLocale` | ICU locale; requires `localeProvider: icu` | PostgreSQL 15+ |
| `icuRules` | Additional ICU collation rules; requires `localeProvider: icu` | PostgreSQL 16+ |
| `builtinLocale` | Builtin locale; requires `localeProvider: builtin` | PostgreSQL 17+ |
| `collationVersion` | Collation version recorded at creation | Supported versions |

To use different creation options, add a new database entry or replace the
existing database through an explicitly planned data-migration workflow. The
API rejects an in-place edit before reconciliation starts.

## Mutable options

| Field | Behavior |
|---|---|
| `isTemplate` | Allows or prevents using the database as a template. |
| `allowConnections` | Allows or prevents new connections. |
| `connectionLimit` | Sets the concurrent connection limit. `-1` means unlimited; values below `-1` are rejected. |
| `tablespace` | Selects a PostgreSQL tablespace. The tablespace must already be usable by the target cluster. |

An omitted field preserves the previous operator behavior and lets CNPG or
PostgreSQL apply its default. Explicit `false`, `0`, and `-1` values are
preserved.

When a new database requests `allowConnections: false`, the operator first
creates it in a connectable state, waits for CNPG, and grants the managed
application privileges. It then applies `allowConnections: false` and keeps the
`PostgresDatabase` in `Provisioning` until CNPG reports that exact update as
applied. This temporary connection window is required for initial bootstrap;
the final declared state remains closed.

When another database is added later, privilege bootstrap targets only that new
database. Existing databases with `allowConnections: false` remain closed and
are not contacted again.

PostgreSQL-version-specific values and unavailable tablespaces are validated by
CNPG/PostgreSQL. If the provider rejects them, `DatabasesReady` remains false,
the per-database status message reports the provider error, and normal
reconciliation retries continue. Mutable options can be corrected in place.
Creation-only options cannot be changed or removed after admission; recovery
requires making the target compatible or replacing the database through a
planned data-migration workflow.

`allowConnections: false` cannot be combined with managed `extensions`. CNPG
must connect to the target database to reconcile extensions, which PostgreSQL
rejects after connections are disabled. An update that removes the final
managed extension and closes the database is staged: the operator keeps the
database connectable until CNPG confirms extension removal, then applies the
requested closed state.
