---
title: Blue/green Major Upgrades
parent: PostgreSQL
nav_order: 5
---

## Blue/green major upgrades

The `blueGreen` strategy creates a target-major CNPG environment, imports the
managed schema, and copies data using logical replication. Applications continue
using blue until a separate switchover gate authorizes an interruption.

After **Target Available**, the workflow verifies a target-major recovery
baseline, records `Recoverable`, removes temporary replication and credentials,
and reaches `Completed`. Blue remains fenced and retained. Explicit retained
environment cleanup and its deletion lifecycle are separate stages; keep the
upgrade configuration in place until cleanup is available and completes.

Before Endpoint Committed, an attempt can also be
[cancelled](#cancel-an-attempt).

### Prepare a candidate

Use the existing [major-upgrade prerequisites](major-version-upgrade.md#upgrade-procedure),
including a healthy source, configured backup protection, and a supported
single-major upgrade. Clients must consume the operator's cluster or database
access ConfigMaps; an application pinned to a blue Service will not follow the
handoff.

The class must configure Barman object storage for the target WAL archive.
Volume snapshots can coexist with it, but snapshot-only protection cannot prove
the required WAL baseline and keeps new attempts waiting in preflight. Configure
this protection before requesting a blue/green upgrade.

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
retained-environment cleanup, including after a cancellation. Candidate readiness verifies the running settings and
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

At commit, backup and WAL production start transferring to green. The operator
removes owned blue backup schedules, drains active backups, and restarts
surviving blue processes with archiving disabled behind their access fence.
Only after confirming source shutdown does it enable green protection, using
the class policy and green's own archive identity. This transfer retries safely
after a restart without running both environments as producers. Backup waits
do not prevent endpoint repair or green access recovery.

### Recoverability and completion

Green creates a workflow-owned target-major backup through the effective
class-configured backend. A completed backup alone is insufficient: the operator
also verifies the target's PostgreSQL major and system identity and fresh WAL
archive progress covering the backup's ending WAL. The source-major recovery
anchor remains under normal retention.

Only after this evidence is durably `Recoverable` does the operator remove the
attempt's subscriptions, publications, logical slots, and replication source
configuration. Teardown waits for pending CNPG apply operations and is safe to
repeat. Temporary credentials on green are removed; CNPG disables the source
control and publisher passwords without reopening ordinary source access. The
inert source control declarations and SQL helpers remain with retained blue
until environment cleanup, with their password Secrets removed.

`Completed` requires current green health and ordinary read/write access, every
endpoint projection on green, verified recoverability, and successful teardown.
Application restart, acknowledgment, or observed reconnection is never required.
Backup and teardown time are separate from the interruption window.

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
| `blueGreen.gates.cancellation` | Latched cancellation request and each completed cancellation step |
| `blueGreen.rebuildRequired` | Why the attempt can no longer be reused by a retry |
| `blueGreen.conditions` | Whether cancellation, retry, and cleanup are currently available |
| `blueGreen.completion.startedAt`, `sourceStoppedAt` | Backup/archive handoff and verified blue producer shutdown |
| `blueGreen.recoverability.targetBackupName` | Workflow-owned target recovery anchor |
| `blueGreen.recoverability.backupVerifiedAt`, `walArchiveBaselineVerifiedAt` | Verified backup and WAL baseline |
| `blueGreen.completion.replicationRemovedAt` | Verified replication teardown |
| `blueGreen.completion.sourceCredentialsRetiredAt`, `credentialsRemovedAt` | Source password retirement and attempt Secret removal |

Abbreviated fields share the prefix of the first field in their row. The fence,
drain, final LSN, sequence, and parity fields are under `blueGreen.switchover`.
Phase `PostUpgradeBackup` means green is available and target-major recoverability
is pending. `Recoverable` means backup and WAL evidence are verified while
teardown proceeds. Neither is full upgrade completion. The parent
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

After correcting the cause, [retry the attempt](#retry-after-a-failure) or
[cancel it](#cancel-an-attempt).

Before opening that new window, the operator rechecks SQL catch-up and candidate
endpoint readiness, including the green Pooler Deployments. An unavailable
Pooler keeps the retry waiting while blue remains writable; readiness is checked
again on subsequent reconciliations.

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

A failed or delayed target backup or WAL check blocks teardown while green
continues serving. Correct the backend first. If an immutable backup request
has failed, set a newer `platform.splunk.com/major-upgrade-retry-at` timestamp
after the commit time to request a fresh backup within the same attempt. The
operator preserves earlier requests and anchors. Once recoverability is verified,
its anchor is fixed; later retry annotations do not replace it.

An attempt prepared by an older operator without the restricted replication
retirement helper can restore green access but fails closed during source teardown.
Green stays authoritative; do not open blue or bypass helper ownership checks to
finish it. Escalate for an explicitly authorized recovery of the recorded source
control overlay.

Blue remains fenced and retained after Target Available. `PostgresDatabase`
creation, deletion, and spec changes remain frozen until `Completed` or
`Cancelled`, as does the broader cluster configuration. Status and metadata
updates remain available throughout the attempt. Never manually
repoint applications or remove blue's fence to recover a committed attempt.

Deleting a parent with `clusterDeletionPolicy: Retain` is blocked while an
unfinished blue/green attempt owns replication or access artifacts. The operator
keeps its finalizer, environment ownership, and required recovery material. The
normal cleanup and finalization lifecycle remains separate from production
completion. Do not remove the finalizer to bypass this guard.

The protocol targets zero data loss for the supported, frozen managed database
inventory. The timeout bounds the operator's interruption work; client
reconnection, DNS caching, and external pooling behavior can extend the
application-visible interruption.

### Retry after a failure

Transient failures retry automatically. A terminal failure stops the attempt
until you set a newer
[`platform.splunk.com/major-upgrade-retry-at` timestamp](major-version-upgrade.md#retrying-after-terminal-failure).
One timestamp authorizes at most one retry: if the retried attempt fails again,
set a newer timestamp.

A retry resumes from the earliest step whose durable evidence is still valid.
Completed source preparation is not repeated, and the operator never reuses an
old window's fencing or parity evidence.

After a failed or expired interruption window, the operator reuses green only
after it catches up with blue and passes every Candidate Ready check again.
Until then the attempt reports `Upgrading`, and no new interruption starts.

Some failures mean the operator can no longer prove that green, or the
attempt's frozen baseline, is valid:

- a changed schema, access policy, credential, CA, or configuration, or a
  changed database, role, object, or sequence inventory;
- broken replication history: an invalidated slot, a subscription apply error
  such as a replication conflict, or a changed publication, subscription, or
  replication credential; and
- a green that no longer matches the attempt: an imported schema that does
  not match blue, a tampered write guard, or a changed or replaced environment.

The attempt then records `blueGreen.rebuildRequired`, reports
`CandidateRebuildRequired` and `RetryAvailable=False`, and ignores retry
timestamps. Cancel it instead. A new attempt can start only after
retained-environment cleanup, a later workflow stage. You cannot force the
operator to reuse an attempt it cannot prove valid.

### Cancel an attempt

Set `postgresMajorUpgradeConfig.blueGreen.cancel: true` to abandon an attempt
before Endpoint Committed. Cancellation takes priority over every unconsumed
gate. It is accepted from any phase before the commit, including a terminal
failure, and continues even if you later set `allow` or `cancel` back to
`false`.

During `Cancelling`, the operator:

1. Restores and verifies blue's endpoint records and application access, if a
   switchover window or endpoint preparation had started.
2. Retains and removes CNPG Subscription declarations, waiting for CNPG to
   acknowledge its last apply. It then disables green's subscriptions, waits
   for their workers to stop, and detaches and drops the actual SQL subscriptions,
   publications, source replication slots, and green's replication source.
   `gates.cancellation.greenReplicationRemovedAt` records verified subscriber
   SQL removal before temporary credentials are retired.
3. Drops the attempt's temporary replication and control roles on blue and
   deletes their Secrets. Green keeps its copy of the control role until
   cleanup deletes green; its password existed only in the deleted Secret.
4. Fences green: it disables every managed login and verifies that ordinary
   database logins are refused. The reject rule blocks ordinary remote database
   access; CNPG's preceding fixed certificate rules preserve provider access,
   including physical replication. Local administrative access through the Unix
   socket remains available.

The attempt then reports `Cancelled`. Blue remains authoritative, ordinary
reconciliation resumes, and the frozen configuration and database definitions
are released. Green is retained, fenced, for explicit cleanup; it is never
reopened. Blue keeps the publisher settings described in
[Prepare a candidate](#prepare-a-candidate) until that cleanup. A blocked step keeps the attempt in `Cancelling` and retries; the
condition message names the step.

The admission webhook rejects `cancel: true` after Endpoint Committed. If a
cancel races the commit, the operator ignores it and continues toward green.

While a retained environment remains, `CleanupRequired=True` and a new major
upgrade cannot start. Retained-environment cleanup is a later workflow stage
and is not yet available. After it completes, starting another attempt for the
same version pair requires setting `allow: false`, letting the operator observe
it, and then setting `allow: true`.

For the complete design and subsequent lifecycle stages, see
[ADR-0007](adr/0007-minimum-downtime-major-upgrades.md) and its
[deployment design](adr/appendices/0007-bluegreen-deployment-design.md).
