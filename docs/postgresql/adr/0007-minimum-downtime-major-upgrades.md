---
title: "ADR-0007: Minimum-downtime major upgrades"
parent: Architecture Decision Records
grand_parent: PostgreSQL
nav_order: 7
---

# ADR-0007: Minimum-downtime major upgrades

- **Status:** Accepted
- **Date:** 2026-08-21
- **Deciders:** Postgres operator team (CPI)
- **Related:** CPI-2073, [ADR-0003](0003-cnpg-integration-and-drift-reconciliation.md), [major-version upgrade operations](../major-version-upgrade.md), [blue/green design appendix](appendices/0007-bluegreen-deployment-design.md)

## Context

The Splunk Operator already ships a PostgreSQL major-upgrade strategy backed by CloudNativePG's declarative in-place `pg_upgrade`. That strategy remains supported as shipped. It provides no downtime SLA and this decision does not redefine its workflow, failure handling, or recovery contract.

Some `PostgresCluster` workloads need a bounded database interruption that an offline in-place upgrade might not provide. "Minimum downtime" is independent of total upgrade time and application recovery time.

The `blueGreen` contract provides RPO 0 for writes acknowledged through supported, declaratively managed application identities before source fencing; privileged out-of-band interference is unsupported. Its switchover target ends only after the target endpoint is published in the canonical cluster access ConfigMap and the target accepts reads and writes. Application detection, reload, reconnection, and retry are outside that target. Preparation, backup, cleanup, and the `Recoverable` milestone are also measured separately.

CloudNativePG documents three major-upgrade methods:

1. logical dump and restore into a replacement cluster, offline;
2. native logical replication into a replacement cluster, online; and
3. physical in-place conversion with `pg_upgrade`, offline.

Blue/green is a topology and API contract, not the name of a replication technology. A source and separately prepared target can be synchronized in several ways, but not all are documented CloudNativePG upgrade workflows.

## Decision drivers

The decision is driven by:

- the scoped RPO 0 contract;
- a configurable operator-controlled `blueGreen` switchover target, defaulting to 60 seconds, that is rollback-enforcing before endpoint commit;
- explicit rather than heuristic strategy selection;
- use of documented CloudNativePG workflows over custom orchestration;
- pre-commit recovery to the source and post-commit fail-forward behavior;
- explicit workload eligibility with no silently omitted objects;
- source high availability throughout preparation and target high availability before Candidate Ready;
- an observable and crash-recoverable production commit point;
- verified target-major backup recovery before old storage is removed; and
- initial qualification of adjacent PostgreSQL major-version upgrades only.

## Options evaluated

### Existing in-place `pg_upgrade`

The Splunk Operator patches the CNPG Cluster to the target PostgreSQL image. CloudNativePG shuts down all database pods and runs `pg_upgrade --link` in a dedicated job with source- and target-version binaries against the primary storage. After conversion, CNPG starts the primary on the target image and rebuilds its replicas. This shipped strategy has the highest fidelity for catalogs, roles, sequences, large objects, and other physical cluster state.

`--link` avoids copying all primary data, so conversion can be relatively quick, but this is an offline data-format conversion rather than a process restart. Reads and writes remain unavailable through conversion and primary startup. The interruption depends on catalog and relation-file complexity, extensions, storage performance, image startup, and primary recovery, so it has no controllable SLA. Replica reconstruction further affects the time to restored high availability.

### Native logical replication into a replacement cluster

CloudNativePG documents an online upgrade workflow that bootstraps the target schema and uses declarative `Publication` and `Subscription` resources for initial table synchronization and continuing change replication. The source remains writable during preparation. At switchover, the operator fences source writes, waits for final parity, synchronizes state omitted by logical replication, and publishes the target.

This is the documented CNPG option capable of a short, bounded write interruption and target-major preparation before cutover. Its costs are a second environment, source WAL retention, a potentially long schema and configuration freeze, endpoint publication, and explicit handling of logical-replication limitations.

### Logical dump and restore

CloudNativePG provides logical import using `pg_dump` and `pg_restore`. A dump is a consistent snapshot but does not include writes committed after that snapshot. RPO 0 therefore requires source writes to remain fenced for the final dump, restore, and endpoint commit interval.

This option is cited because CNPG provides it, but it will not be exposed as a selectable Splunk Operator major-upgrade strategy. It generally offers a longer interruption than the selected online workflow.

### Rejected physical and hybrid approaches

- **Same-major physical replica followed by cutover `pg_upgrade`:** carries physical cluster state but puts conversion and target startup inside the production interruption and prevents validation of a current target-major candidate.
- **Physical snapshot, cloned `pg_upgrade`, and logical delta catch-up:** may avoid a full logical initial copy but requires custom proof and recovery for the snapshot-LSN-slot handoff.
- **PostgreSQL `pg_createsubscriber`:** can convert a same-major physical standby to a logical subscriber on PostgreSQL 17 and later, but cross-major catch-up remains logical and the conversion is not an established CNPG-managed upgrade workflow.

These approaches are not selected. Cross-major physical WAL replay is not supported as a contract by the Postgres upstream, cutover-time conversion defeats the bounded interruption, and custom hybrid handoffs are outside CNPG's documented major-upgrade workflows.

### External CDC, bidirectional replication, and application dual-write

These technologies could address uninterrupted writes or a lossless rollback after target writes begin. They add conflict resolution, ordering, key ownership, and application responsibilities outside CNPG's native major-upgrade workflows.

They are outside the scope of this decision.

## Decision

Retain the shipped in-place strategy unchanged and add an explicitly selected `blueGreen` strategy to the `PostgresCluster` major-upgrade API.

This ADR narrowly supersedes ADR-0003's rule that one `PostgresCluster` owns exactly one CNPG `Cluster`. One-to-one ownership remains the steady-state invariant. During an explicit `blueGreen` attempt, one `PostgresCluster` temporarily owns both the authoritative source and the candidate target CNPG `Cluster`. `status.provisionerRef` remains the backward-compatible pointer to the authoritative environment and supplies ordinary cluster health and endpoints; source and target references and candidate health remain isolated in nested upgrade status. Cleanup restores one-to-one ownership.

This ADR also narrowly supersedes ADR-0003's direct-SQL restriction for attempt-scoped `blueGreen` control operations that CNPG cannot express declaratively: DML guards, catalog and fence verification, session termination, sequence synchronization, and replication inspection and teardown. Those operations use temporary least-privilege roles and attempt-scoped credentials, are reconciled idempotently with auditable ownership, and require verified teardown. ADR-0003's direct-SQL boundary remains unchanged for steady-state reconciliation.

ADR-0003's other CNPG integration, declarative ownership, drift-reconciliation, and phase-projection decisions remain in force.

`blueGreen` is an API contract with these outcomes:

- prepare a separate target environment;
- keep the source authoritative until an explicit commit point;
- provide the scoped RPO 0 contract;
- measure the operator-controlled switchover against a configurable target that defaults to 60 seconds and enforces rollback before endpoint commit;
- publish the target through the canonical cluster access ConfigMap;
- restore source authority after any pre-commit failure; and
- fail forward on the target after commit.

The selected implementation of that contract is CloudNativePG's documented native logical-replication workflow: schema bootstrap followed by declarative publications and subscriptions. The API name does not encode the transport. Replacing the transport in the future requires a new architectural decision and equivalent qualification.

The operator must never automatically select a strategy from runtime heuristics or silently fall back from `blueGreen` to in-place conversion. Upgrade intent belongs only on `PostgresCluster`, and the selected strategy must be explicit and reviewable there. Existing defaulting behavior for the shipped in-place strategy remains unchanged.

Only adjacent major-version upgrades are initially eligible.

## Workload eligibility

The in-place strategy retains its existing compatibility rules.

`blueGreen` is available only when a preflight inventory proves that the workload is compatible with native logical replication and with the operator's reconciliation contract. Preflight must identify every incompatibility and must never silently omit data or objects.

At minimum, preflight covers:

- every managed database and role;
- table replica identity;
- schema and DDL stability;
- sequence inventory;
- large objects;
- materialized views;
- non-empty unlogged tables;
- extensions and background workers;
- credentials, grants, and access policy;
- source logical-slot failover support;
- target storage and source WAL headroom; and
- target-major image and extension compatibility.

An online attempt freezes schema DDL, database and role changes, extensions, credentials, and relevant PostgreSQL configuration while ordinary table DML continues. `ReadyForSwitchover` may remain paused indefinitely: the operator continues replication and invariant checks and fails only when an invariant or capacity limit breaks. It abandons or reseeds an invalid candidate rather than reconciling unknown differences during switchover.

## blueGreen control contract

The existing generic `allow` gate authorizes preparation. A separate pre-authorizable `switchover` gate authorizes the production handoff. Explicit cancellation is permitted before commit. A separate pre-authorizable `cleanup` gate authorizes deletion of the retained source after the target is recoverable.

The switchover timeout is configured on `PostgresCluster`, defaults to 60 seconds, and is latched into the durable attempt record when switchover begins. Spec changes cannot extend or shorten an active fence; they apply to a later attempt. The API validates the duration against defined bounds.

If the timeout expires before Endpoint Committed, the operator restores source authority, removes and verifies the source fence, and reports a retryable switchover failure. It must not commit because the target is almost ready. If it expires after Endpoint Committed but before Target Available, the operator records a target breach and continues fail-forward until green is available.

Cancellation during fencing follows the same pre-commit recovery rule. Cancellation is invalid after commit, when only fail-forward completion and cleanup remain available.

## Preparation and high availability

CloudNativePG's schema import and native subscription initial copy establish the target. Logical decoding slots are synchronized across eligible source replicas so a source failover does not lose or invalidate the stream. The target may bootstrap temporarily with one instance, but it is not ready for switchover until its required replica topology is healthy.

Every strategy requires a fresh, verified pre-upgrade backup. The target requires a new target-major backup and WAL archive baseline after commit because recovery cannot cross a PostgreSQL major-version boundary.

## Switchover and commit

During final switchover the operator:

1. verifies the source write fence and removes write-capable or unclassified sessions;
2. proves final LSN, sequence, fingerprint, and target-health parity while the target remains fenced;
3. publishes the canonical ConfigMap, then atomically updates `status.provisionerRef` and the monotonic commit receipt; and
4. publishes and verifies every database access ConfigMap, terminates remaining source application sessions, keeps the source fenced, and opens and verifies the target.

The canonical cluster access ConfigMap is the externally observable commit record. Database-specific access ConfigMaps and ordinary status are repairable projections. The monotonic status receipt acknowledges that the canonical commit occurred and cannot independently initiate a commit.

Before canonical commit, a failure restores the source as authoritative and does not expose the target to production. After canonical commit, including a crash before the target fence is removed, recovery fails forward by opening and repairing the target. If the canonical ConfigMap is missing after restart, the monotonic receipt proves a prior commit and requires fail-forward. If both ConfigMap and receipt are absent, green was never opened and blue can be restored. The previous environment is not a lossless rollback target; later recovery fails forward on the target or uses backup restoration.

The configured switchover target includes database fencing, final parity, canonical ConfigMap publication, and target availability. Before Endpoint Committed it is a hard rollback deadline; afterward it is an availability target that cannot reverse the commit. Application detection of the ConfigMap update, configuration reload, reconnection, and retry are downstream application responsibilities and are not included.

## Milestones

The operator reports distinct milestones rather than calling the upgrade complete at endpoint publication:

- **Candidate Ready** — initial synchronization is complete, continuous replication is healthy, and the required target replica topology is healthy;
- **Endpoint Committed** — the canonical ConfigMap publishes the target and rollback to the source is no longer permitted;
- **Target Available** — the target fence is removed and verified so the target accepts reads and writes;
- **Recoverable** — a target-major backup and WAL archive baseline are verified; and
- **Completed** — every mandatory post-upgrade action has finished.

`Candidate Ready` is required before switchover begins. The configured switchover target covers the transition from `Candidate Ready` through `Target Available`; `Recoverable` and `Completed` may take substantially longer.

The fenced source is retained until `cleanup` is authorized and the target is both highly available and recoverable. Retention is for diagnostics and controlled cleanup, not lossless rollback.

## Failure and retry

The operator reuses an existing target after transient failures when replication continuity and target integrity remain provable. It requires a new candidate after integrity-breaking failures such as unrecoverable replication conflicts, invalid or missing source history, incompatible drift, target corruption, or ambiguous parity.

Failure classification is deterministic and operator-owned. A user cannot force reuse when target validity cannot be proved. Rebuilding creates a new durable attempt identity.

## Consequences

### Positive

- Workloads that qualify for `blueGreen` gain a bounded, low-interruption major-upgrade contract.
- The implementation remains on a documented CloudNativePG workflow.
- The existing in-place strategy remains available without being constrained by an artificial downtime SLA.
- Strategy selection, eligibility, commit, and cleanup are explicit and observable.
- The scoped RPO 0 contract is protected by a hard pre-commit recovery boundary.
- Application recovery ownership is not confused with database endpoint publication.

### Negative

- `blueGreen` temporarily requires two environments and must bring both to full high availability before Candidate Ready.
- Logical replication requires strict inventory qualification and explicit handling of schema, sequences, and unsupported objects.
- Long or indefinite preparation can increase source load and WAL retention and extends the schema and configuration freeze.
- Replacement endpoints require applications to honor the ConfigMap contract.
- Post-commit rollback to the previous environment is not lossless.
- The control plane must persist and recover more lifecycle states than the in-place strategy.

## Qualification requirements

Before a strategy is supported, release qualification must include a production-representative rehearsal. Per-attempt application validation against a blue/green candidate remains optional and risk-based.

Before `blueGreen` is supported, qualification must also cover:

- adjacent major-version upgrade pairs and supported extension combinations;
- large initial copies under realistic source write load;
- source failover during initial copy and catch-up;
- target failover and replica reconstruction;
- WAL retention and storage exhaustion boundaries;
- schema, credential, role, and configuration drift;
- sequence synchronization and final-LSN parity;
- controller and pod crashes at every fencing and commit boundary;
- expiration of the configured switchover deadline;
- cancellation before and during fencing;
- repair from stale status using the canonical ConfigMap;
- target-major backup and restore;
- candidate reuse and mandatory rebuild classifications; and
- endpoint ConfigMap publication.

Qualification measures database interruption separately from preparation time, degraded-HA time, backup time, cleanup time, and application-owned recovery.

## References

- [CloudNativePG: PostgreSQL upgrades](https://cloudnative-pg.io/docs/current/postgres_upgrades/)
- [CloudNativePG: Logical replication](https://cloudnative-pg.io/docs/current/logical_replication/)
- [CloudNativePG: Importing Postgres databases](https://cloudnative-pg.io/docs/current/database_import/)
- [CloudNativePG: Replica clusters](https://cloudnative-pg.io/docs/current/replica_cluster/)
- [PostgreSQL: Logical replication restrictions](https://www.postgresql.org/docs/current/logical-replication-restrictions.html)
- [PostgreSQL: pg_upgrade](https://www.postgresql.org/docs/current/pgupgrade.html)
- Code: `api/platform/v1alpha1/postgrescluster_types.go`, `pkg/postgresql/cluster/core/use_cases/major_version_upgrade/`
- Existing operations: [Major version upgrades](../major-version-upgrade.md)
- Detailed design: [Blue/green deployment design appendix](appendices/0007-bluegreen-deployment-design.md)
