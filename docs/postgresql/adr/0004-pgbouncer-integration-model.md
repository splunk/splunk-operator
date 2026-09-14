---
title: "ADR-0004: PgBouncer connection-pooler integration model"
parent: Architecture Decision Records
grand_parent: PostgreSQL
nav_order: 4
---

# ADR-0004: PgBouncer connection-pooler integration model

- **Status:** Accepted
- **Date:** 2026-07-20
- **Deciders:** Postgres operator team (CPI)
- **Related:** [ADR-0003](0003-cnpg-integration-and-drift-reconciliation.md), [ADR-0005](0005-postgresclusterclass-abstraction.md)

## Context

PostgreSQL handles each client connection with a backend process, so a workload
that opens many short-lived connections (typical of pooled application servers
and serverless callers) can exhaust `max_connections` and pay a high per-connect
cost. A connection pooler in front of PostgreSQL absorbs connection storms and
enables near-zero-downtime primary cutovers by holding client connections while
the backend changes. CNPG ships a first-class `Pooler` resource backed by
PgBouncer. The design questions:

- Who decides whether pooling is on — the platform (class) or the service team
  (cluster)?
- Which pool mode is the default, given the compatibility trade-offs?
- Do we expose read-write and read-only pooling separately, and how does that
  interact with replica availability?

## Decision

**Pooling is a class-defined capability with per-cluster enablement.** The
`PostgresClusterClass` carries two related pieces:

- `spec.config.connectionPooler` (`ConnectionPoolerEnableConfig`) — the
  overridable enable/shape switch: `enabled`, `readWrite`, `readOnly`. This can
  be overridden on `PostgresCluster.spec.connectionPooler` per cluster.
- `spec.cnpg.connectionPooler` (`ConnectionPoolerConfig`) — the **platform
  policy** for PgBouncer itself: `instances`, `mode`, and raw PgBouncer
  `config`. This lives under `spec.cnpg` and therefore **cannot** be overridden
  per cluster (see [ADR-0005](0005-postgresclusterclass-abstraction.md)). A CEL rule on
  the class requires `spec.cnpg.connectionPooler` to be present whenever
  `config.connectionPooler.enabled` is true.

**Two pooler endpoints, RW and RO, reconciled as separate CNPG `Pooler` CRs.**
When enabled, the operator creates a read-write pooler; the read-only pooler is
additionally gated on **effective instances ≥ 2** (there must be a replica to
route reads to). The admission webhook rejects `readOnly: true` with
`instances < 2`. Endpoints are published on the cluster's connection-info
ConfigMap for consumers to read.

**Default pool mode is `transaction`.** The `ConnectionPoolerMode` enum is
`session | transaction | statement`; `transaction` is the CRD default because it
gives the best pooling ratio for typical stateless application workloads while
remaining broadly compatible. `session` (most compatible, needed for
session-level features like prepared statements without protocol-level support)
and `statement` (most aggressive, limited compatibility) are available as class
policy.

**The enable-config fields intentionally carry no CRD defaults.** `enabled`,
`readWrite`, and `readOnly` are `*bool` with no `+kubebuilder:default`, because a
CRD default would be materialized onto the stored object by the apiserver and
overwrite the `nil` ("inherit from class") sentinel that per-field override
merging relies on. Defaulting for omitted fields is owned by the Go layer
instead (`isPoolerEnabled`: nil→false; `poolerReadWriteWanted`/
`poolerReadOnlyWanted`: nil→true).

**The pooler is a gate on cluster readiness.** In the reconcile pipeline the
pooler component can hold the cluster at `Pending`/`Provisioning` until RW+RO
instances are ready, and only then does the cluster reach `Ready`
(`PoolerReady` condition; see [ADR-0002](0002-actuate-converge-reconcile-pattern.md)).

## Alternatives considered

- **Per-cluster PgBouncer tuning (mode, instances, raw config).** Rejected as
  default: pool mode and sizing have correctness and capacity implications the
  platform team should own uniformly. Enablement and RW/RO shape are delegated
  to the service team; PgBouncer behavior stays platform policy under
  `spec.cnpg`.
- **Session mode as default** (maximum compatibility). Rejected: it largely
  defeats the point of pooling for the common stateless-app workload, since a
  connection is pinned for the whole client session. `transaction` is the
  default; `session` remains available where needed.
- **Single combined endpoint** (route reads and writes through one pooler).
  Rejected: separating RW and RO lets read traffic scale onto replicas and lets
  the RO pooler be gated on replica availability, which a combined endpoint
  can't express.
- **Sidecar / self-managed PgBouncer deployment.** Rejected: CNPG's `Pooler` CR
  is first-class, integrates with CNPG TLS and service discovery, and is owned
  and drift-reconciled like the rest of the stack (see
  [ADR-0003](0003-cnpg-integration-and-drift-reconciliation.md)).

## Consequences

- **Positive:** connection-storm protection and smoother primary cutovers;
  platform owns PgBouncer behavior while service teams control enablement; RO
  pooling scales reads onto replicas and is safely gated on replica count;
  drift-reconciled like other CNPG objects.
- **Negative / costs:**
  - The `nil`-sentinel merge model is subtle: contributors must remember *not*
    to add CRD defaults to `ConnectionPoolerEnableConfig`, or per-field override
    inheritance breaks. This is documented in the type's Go comment.
  - `transaction` mode silently breaks session-level features (some prepared
    statement patterns, `SET`/`LISTEN`); consumers relying on those must request
    a `session`-mode class.
  - RO pooler availability is coupled to the effective instance count, so
    scaling a cluster down to a single instance disables RO pooling — consumers
    must mirror the `ConnectionPoolerStatus.ReadOnlyEnabled` gate before
    advertising RO endpoints.

## References

- Code: `api/platform/v1alpha1/postgresclusterclass_types.go`
  (`ConnectionPoolerEnableConfig`, `ConnectionPoolerConfig`,
  `ConnectionPoolerMode`), `pkg/postgresql/cluster/core/pooler_model.go`,
  `api/platform/v1alpha1/postgrescluster_types.go` (`ConnectionPoolerStatus`)
- Design docs: "Integration & Onboarding Guide for Splunk Operator's PostgreSQL
  Support", "PostgreSQL Components State Machine"
- Jira: CPI-2066
