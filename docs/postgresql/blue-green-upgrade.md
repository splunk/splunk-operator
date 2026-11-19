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
| `blueGreen.cleanup.environmentRef`, `retirementGeneration` | Exact retiring environment and independent retirement revision |
| `blueGreen.cleanup.retirementAcknowledgements` | Child/resource inventory and aggregated retirement proofs |
| `blueGreen.cleanup.retiredResources` | Exact parent-owned resource identities retained through deletion retries |

Each `PostgresDatabase.status.environmentRetirement` proof binds its parent UID,
attempt, retirement revision, environment UID, child generation, and provider
Database identities. A proof from another generation or a recreated object
cannot satisfy retirement. These optional status fields do not authorize
deletion; cleanup still requires its separate permission and verified runtime
checks. Application credentials and authoritative Database declarations are
outside the retirement inventory.

Abbreviated fields share the prefix of the first field in their row. The fence,
drain, final LSN, sequence, and parity fields are under `blueGreen.switchover`.
Phase `PostUpgradeBackup` means green is available and target-major recoverability
is pending. `Recoverable` means backup and WAL evidence are verified while
teardown proceeds. Neither is full upgrade completion. The parent
`PostgresCluster.status.phase` returns to operational `Ready` after live green
health, managed roles, and metrics converge; this permits ordinary database
reconciliation while the upgrade entry remains in `PostUpgradeBackup`.

Before requesting target backups or enabling their schedules, the operator waits
for the Barman sidecar rollout and effective archive settings on every green
instance. A healthy Cluster status or a configured plugin alone does not satisfy
this check. Both native and conventional sidecars are supported.

The Barman sidecar is prepared on green while blue still serves traffic, with
`isWALArchiver: false` and no green backup schedules. After blue's archive producer
has stopped, the operator activates green's existing sidecars. Older candidates
without sidecars first roll out the inactive plugin before activation. This avoids
enabling an archiver that the primary Pod cannot call during shutdown.

Barman WAL archiving is currently required for blue/green upgrades. A snapshot-only
configuration is blocked during preflight with `TargetWALArchiveNotConfigured`;
the completion verifier requires both a target backup and an archived-WAL baseline.
CSI snapshots can be enabled alongside Barman. In that configuration the recovery
anchor uses Barman, and both schedules are enabled after the readiness check.
Other backup plugins are not exposed by the current backup-provider configuration.

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

After recoverability is verified, Subscription retirement does not require a
previously failed CNPG apply to succeed. The operator records Retain intent,
deletes the exact owned declaration, and waits for CNPG's finalizer acknowledgement
before removing the SQL subscription. Publication apply barriers and unrelated
finalizers remain in force.

After a source primary becomes a standby, it can retain local failover slots
that PostgreSQL's slot synchronization worker cannot adopt. The prepared
standby retirement helper removes only this attempt's inactive, persistent,
unsynchronized `pgoutput` slot in its recorded database, after the source
publisher role can no longer log in. Active slots remain blockers; synchronized
slots are left to PostgreSQL. The next observation must confirm absence.

An attempt prepared by an older operator without a required restricted
retirement helper can restore green access but fails closed during source teardown.
In particular, an older attempt with a local standby slot reports that the
standby slot retirement helper was not prepared and requires operator-assisted
recovery. Updating the operator does not rewrite SQL helpers in a committed attempt.
Green stays authoritative; do not open blue or bypass helper ownership checks to
finish it. Escalate for an explicitly authorized recovery of the recorded source
control overlay.

Blue remains fenced and retained after Target Available. `PostgresDatabase`
creation, deletion, and spec changes remain frozen until `Completed` or
`Cancelled`, as does the broader cluster configuration. Status and metadata
updates remain available throughout the attempt. Never manually
repoint applications or remove blue's fence to recover a committed attempt.

Deleting a parent with `clusterDeletionPolicy: Retain` waits for verified SQL
teardown and safe access to the surviving environments. The operator preserves
its finalizer and recovery material while that proof is unavailable. See
[Delete a cluster during an upgrade](#delete-a-cluster-during-an-upgrade).

After endpoint commit, Retain finalization verifies blue's fence, drains its
remaining application sessions, and restores and verifies green access before
recording SQL teardown. Interrupted handoff checkpoints survive retries. A
completed replication checkpoint permits finalization after temporary control
credentials have been retired. A surviving stamped canonical record can repair
a lost commit receipt; finalization cannot create a new endpoint commit.

The protocol targets zero data loss for the supported, frozen managed database
inventory. The timeout bounds the operator's interruption work; client
reconnection, DNS caching, and external pooling behavior can extend the
application-visible interruption.

### Retry after a failure

Green uses its own server certificate under the frozen blue CA. With CNPG's
default certificate mode, the candidate TLS plan adds green's generated RW and
RO pooler hostnames before CNPG issues that certificate. Clients retain
`sslmode=verify-full` when switching to a pooler. For an unfinished pre-cutover
attempt, an older candidate whose certificate configuration differs from this
contract is rejected by the existing candidate-spec validation; cancel it and
start a fresh attempt rather than bypassing TLS hostname verification. After
Endpoint Committed, keep green authoritative and recover forward. A corrected
certificate in a Secret alone does not prove that running poolers present it;
verify connections through the published pooler endpoints as well.

During green preparation, CNPG plugin-hook failures retry automatically for up
to five minutes from the green Cluster's creation time. The attempt reports
`Upgrading` with reason `CandidatePluginPending`; blue remains authoritative
and no switchover starts. This allows temporary plugin setup failures, such as
a newly created Barman Role not yet being visible, to recover without ending
the attempt. Reconciliation and operator restarts do not reset this deadline.
If the plugin remains failed after that window, the attempt reports `Failed`
with reason `CandidatePluginTimedOut`. Check the green CNPG Cluster and plugin
logs, correct the dependency, and explicitly retry or cancel. Unknown plugins
and unrecoverable configuration errors still fail immediately. This grace
period applies only to candidate preparation, not post-commit recovery.

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
retained-environment cleanup. You cannot force the
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
upgrade cannot start. After cleanup completes, starting another attempt for the
same version pair requires setting `allow: false`, letting the operator observe
it, and then setting `allow: true`.

### Clean up the retained environment

After `Completed` or `Cancelled`, set
`postgresMajorUpgradeConfig.blueGreen.cleanup: true`. A failed attempt must
first be retried or cancelled. Cleanup retires blue after completion and green
after cancellation, preserving the authoritative environment and recovery backups.

Before deleting the retired Cluster, cleanup transfers shared CA Secrets still
used by the authoritative Cluster to that Cluster. It preserves Backup records
and detaches Cluster-owned data, WAL and tablespace VolumeSnapshots so garbage
collection cannot remove the recovery snapshots.

Cleanup records its permission before making changes. Once consumed, it
continues even if `cleanup` or `allow` becomes `false`. Its independent status
changes from `Cleaning` to `Cleaned`; the upgrade remains `Completed` or
`Cancelled`. Errors report `CleanupFailed` and retry without reopening the
retired environment.

Children remove their retired CNPG Database declarations and acknowledge the
exact environment UID and retirement generation. The parent independently
checks those proofs and declaration absence before deleting retired resources.
Child changes invalidate the barrier and require fresh acknowledgements.
`Cleaned` also requires removal of retired storage and workloads; a terminating
resource or storage finalizer keeps cleanup pending. Do not remove finalizers
to advance it.

### Delete a cluster during an upgrade

Deleting the `PostgresCluster` follows `clusterDeletionPolicy` without requiring
`cleanup: true`, a current class/image configuration, or a completed upgrade.
The operator discovers managed environments and temporary resources through
exact references and ownership, including resources whose creation preceded
an interrupted status write. `status.finalization` preserves controller-observed
inventory, policy and SQL checkpoints across restarts and owner detachment.
Only identities verified before detachment can authorize later replay. Metadata
annotations never authorize finalization or supply SQL-completion proof.

With `Delete`, it attempts SQL teardown within a bounded timeout, then removes
all managed environments, their operational storage and workloads, Poolers,
and temporary resources. Unreachable SQL produces a `FinalizationSQLSkipped`
warning and does not prevent this deletion. Recovery Backup metadata is
preserved for its existing retention policy. A remaining storage-protection or
unrelated finalizer keeps the parent finalizer in place until removal completes.

With `Retain`, actual subscription and source-slot teardown must be proved
before environments are orphaned. Before Endpoint Committed, blue access is
restored and verified. After commit, green stays authoritative and blue stays
fenced. Valid completion receipts allow finalization to proceed after temporary
credentials have already been retired. Application credentials, TLS material,
and renewing Certificates remain with retained environments. Managed Poolers
and backup schedules are removed, and retained environments stop referencing
the managed WAL archiver. Copied CA deletion under `Delete` follows the latched
class retention policy; if the class is already unavailable, the CA is preserved.

Retain rechecks blue's live fence before each ownership change and finalizer
release, including after `status.finalization.sqlDone` is saved. A new replication
declaration invalidates SQL proof; its removal must be acknowledged before a
fresh SQL check. Historical declaration references alone do not invalidate a
subsequently saved proof. A writer for an already cleaned attempt latches
`status.finalization.sqlBlocked`: automatic cleanup cannot reconstruct that
attempt's SQL proof. Removing the declaration does not clear this receipt;
resolve the SQL and proof with the owning team while preserving the finalizer.

Draft versions that stored inventory in annotations cannot supply trusted
provenance for already detached resources. Those records are not imported into
status; unresolved detached identities require owner-assisted recovery.

`FinalizationBlocked` means proof is missing; ownership and recovery material
are preserved. Older cancelled candidates can still contain SQL subscriptions
after their credentials were removed. Older fenced attempts may also lack the
restricted subscription-removal helper. Use authorized administrative access
to inspect the exact candidate's subscriptions and the source's attempt slots,
including synchronization slots. Resolve the missing helper or teardown proof
with the owning team. The operator never reopens the fence or recreates a retired
credential automatically.

Manual removal of the parent finalizer is a break-glass action: first secure
credentials and TLS renewal, verify subscription/slot removal and the correct
source access state, and account for every retained environment and storage
resource. The operator does not perform that bypass.
