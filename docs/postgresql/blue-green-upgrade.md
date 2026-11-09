---
title: Blue/green Major Upgrades
parent: PostgreSQL
nav_order: 5
---

## Blue/green major upgrades

The `blueGreen` strategy creates a target-major CNPG environment, imports the
managed schema, and copies data using logical replication. Applications continue
using blue until a separate switchover gate authorizes an interruption.

This implementation reaches **Target Available** and then waits in
`PostUpgradeBackup`. Target-major recoverability, replication teardown,
`Completed`, cancellation teardown, and retained-environment cleanup belong to
subsequent workflow stages. Keep the upgrade configuration and retained blue
environment in place at this boundary.

### Prepare a candidate

Use the existing [major-upgrade prerequisites](major-version-upgrade.md#upgrade-procedure),
including a healthy source, configured backup protection, and a supported
single-major upgrade. Clients must consume the operator's cluster or database
access ConfigMaps; an application pinned to a blue Service will not follow the
handoff.

Before granting `allow`, ensure `spec.images.postgresql` includes pinned images
for both the source and target majors. Retain existing
`spec.images.imagePullSecrets` when registry access requires them.

Update the tracked `PostgresCluster` manifest. For example:

```yaml
spec:
  postgresVersion: "18"
  # Retain spec.images, including its PostgreSQL 18 mapping and registry credentials.
  postgresMajorUpgradeConfig:
    allow: true
    strategy: blueGreen
    blueGreen:
      switchover: false
      switchoverTimeout: 60s
      cancel: false
      cleanup: false
```

The source-major backup and preflight must pass before publisher preparation.
Preflight rejects unmanaged databases, unsupported schema or access policies,
and insufficient replication capacity. Follow the reported condition instead
of modifying the underlying CNPG resources to bypass a check.

For PostgreSQL 17 and later, publisher preparation enables CNPG HA replication
slots, logical-slot synchronization, `hot_standby_feedback`, and
`sync_replication_slots` together. These settings remain enabled until
replication cleanup. Candidate readiness verifies the running settings and
failover slots on every intended source instance.

Once source preparation starts, keep cluster configuration, credentials,
managed database definitions, schema, and access policies unchanged. Ordinary
blue application DML can continue while green copies and catches up. Green's
candidate write guards prevent ordinary application writes.

Wait for the latest upgrade entry to reach `ReadyForSwitchover` and for its
candidate readiness conditions to pass. The green identity and candidate
endpoints are recorded under that entry's `blueGreen` status. They are not the
application endpoint publication.

### Authorize the handoff

Set `postgresMajorUpgradeConfig.blueGreen.switchover: true` in the tracked
manifest. The operator consumes this gate only after Candidate Ready. A gate
can be preauthorized, and changing a consumed gate to `false` does not undo it.

`switchoverTimeout` accepts `30s` through `3600s` and defaults to `60s`. The
operator first prepares an attempt-scoped control identity. It latches the
interruption deadline immediately before declaring the access fence. Changing
the configured timeout does not extend an active window.

During `SwitchingOver`, the operator:

1. Records the exact source and candidate access baselines and an immutable
   operator-only credential Secret. This identity is never published in
   application access ConfigMaps.
2. Fences ordinary access on both environments through CNPG role and HBA
   declarations, then verifies fresh connections on every healthy instance.
3. Drains source sessions within a bounded grace period and terminates remaining
   ordinary sessions. It also removes sessions that predate the green fence.
   Prepared transactions prevent progression.
4. Records the final blue WAL position, verifies subscriber and source-slot
   parity, and rechecks frozen schema, access policies, configuration,
   credentials, and replication-slot health.
5. Synchronizes and reads back each managed sequence's `last_value` and
   `is_called`. Candidate guards are removed only behind the verified green
   access fence.
6. Waits for every frozen database endpoint preparation acknowledgement while
   all application ConfigMaps still publish blue.
7. Verifies that the final green WAL is flushed on the primary and received,
   flushed, and replayed by every green replica, rechecking primary and Pod
   identities before committing. It then updates the canonical cluster access
   ConfigMap to green and records `provisionerRef` and a monotonic commit receipt
   in one parent status write.
8. Waits for the database ConfigMap publication barrier, terminates remaining
   blue sessions, restores green's captured access policy, and verifies an
   ordinary green read/write transaction.

The canonical cluster ConfigMap update is **Endpoint Committed**, the external
commit boundary. The operator never switches application projections back to
blue after this write. Target Available is later: it proves the child
publication barrier and effective green access. A deterministic
`PostgresEndpointsPublished` Event is created once per attempt after that proof.

### Observe progress

Inspect the latest `status.postgresMajorUpgradeStatus` entry:

| Status field | Evidence |
| --- | --- |
| `blueGreen.gates.switchover` | Consumed authorization and generation |
| `blueGreen.switchover.startedAt`, `deadlineAt` | Latched interruption window |
| `fenceVerifiedAt`, `drainCompletedAt` | Effective fencing and session drain |
| `finalBlueLSN`, `sequenceSynchronization`, `finalParityVerifiedAt` | Final replication and sequence proof |
| `blueGreen.endpointPreparation` | Frozen child preparation barrier |
| `blueGreen.commitReceipt` | Durable canonical commit identity and sequence |
| `blueGreen.endpointPublication` | Verified child publication barrier |
| `blueGreen.switchover.targetAvailableAt` | Verified ordinary green access |
| `blueGreen.canonicalRecovery` | Explicit proof for a recreated canonical ConfigMap |

Fields without a `blueGreen` prefix in the table are under
`blueGreen.switchover`. Phase `PostUpgradeBackup` at this implementation boundary
means green is available and the target-major recoverability stage is pending;
it does not mean the full upgrade is `Completed`. The parent
`PostgresCluster.status.phase` returns to operational `Ready` after live green
health, managed roles, and metrics converge; this permits ordinary database
reconciliation while the upgrade entry remains in `PostUpgradeBackup`.

### Failure and restart behavior

Before Endpoint Committed, a failed or expired interruption window restores the
blue endpoint record, invalidates unresolved older canonical writes, clears
preparation intent, restores blue's captured access policy, and verifies source
access. The window becomes terminal only
after this restoration is proved. Green stays fenced. An explicit timer then
runs ordinary source reconciliation; the parent returns to `Ready` only after
blue passes current component health checks.

Each source-restoration pass has a fresh 30-second operation timeout. If it
expires, the operator releases the worker and retries restoration. The window
remains nonterminal until source access is verified.

After correcting the cause, use a newer
[`platform.splunk.com/major-upgrade-retry-at` timestamp](major-version-upgrade.md#retrying-after-terminal-failure)
to authorize another interruption window within the same attempt. The operator
preserves immutable identity and baseline evidence; it does not reuse an old
window's fencing or parity receipts.

After Endpoint Committed, failures and timeouts continue toward green. The
operator rejects newly requested cancellation after a durable commit and
repairs missing or stale endpoint projections from the existing
commit, completes the publication barrier, and opens green. Recovery runs before
ordinary class and component reconciliation. Missing retired blue replicas do
not require rebuilding source HA: the operator retains the exact durable fence
and verifies and drains reachable healthy source instances. An actual TLS,
authentication, identity, or fence contradiction still prevents progression. An unreadable or
ambiguous canonical record never grants permission to restore blue. A receipt
can recover an earlier commit; it cannot authorize the first green publication.

Blue remains fenced and retained after Target Available. `PostgresDatabase`
creation, deletion, and spec changes remain frozen until `Completed` or
`Cancelled`, as does the broader cluster configuration. Status and metadata
updates remain available throughout the attempt. Never manually
repoint applications or remove blue's fence to recover a committed attempt.

Deleting a parent with `clusterDeletionPolicy: Retain` is blocked while an
unfinished blue/green attempt owns replication or access artifacts. The operator
keeps its finalizer, environment ownership, and recovery credentials. Verified
replication teardown and source restoration are required before retention can
finish; automatic cancellation and teardown belong to the subsequent lifecycle
stages. Do not remove the finalizer to bypass this guard.

The protocol targets zero data loss for the supported, frozen managed database
inventory. The timeout bounds the operator's interruption work; client
reconnection, DNS caching, and external pooling behavior can extend the
application-visible interruption.

For the complete design and subsequent lifecycle stages, see
[ADR-0007](adr/0007-minimum-downtime-major-upgrades.md) and its
[deployment design](adr/appendices/0007-bluegreen-deployment-design.md).
