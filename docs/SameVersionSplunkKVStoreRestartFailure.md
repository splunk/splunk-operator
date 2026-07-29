# Same-Version Splunk Restart Fails the KV Store Upgrade Precheck

## Summary

A Splunk Search Head Pod cannot start after an ordinary Kubernetes Pod
replacement, even though the replacement uses the **same Splunk version and the
same container image digest** as the Pod it replaces.

Splunk reports that the restart is an upgrade, runs the KV Store upgrade
precheck, and exits because it cannot find a KV Store 7.0 or 8.0 version marker.
The container then enters a restart loop.

This is not currently understood to be an Operator, readiness-probe, or timeout
failure. Splunk exits with code 2 during `splunk start`, before it can become
ready.

## Environment and exact identity

Observed on 27 July 2026 in a three-member Search Head Cluster on Kubernetes.

| Item | Before and after Pod replacement |
| --- | --- |
| Splunk version | `10.6.0.0` |
| Splunk build | `1c3c2df1c656` |
| Image tag | `shc-reliability-55d3a58-splunk-10.6.0.0-1c3c2df1c656` |
| Image digest | `sha256:487469ee65975177aa5502085a4facc4a10a069d6875f469c09e965a81d14654` |
| Persistent storage | The existing `/opt/splunk/etc` and `/opt/splunk/var` PVCs were reused |

The StatefulSet Pod-template change only added a qualification revision
environment value. It did not change the Splunk image. The surviving members
and the replacement Pod all reported the same image digest.

Inside both surviving members, and during the replacement container's startup:

```text
VERSION=10.6.0.0
BUILD=1c3c2df1c656
PRODUCT=splunk
PLATFORM=Linux-x86_64
```

## Reproduction

1. Start a three-member Search Head Cluster on persistent volumes.
2. Confirm that all members are running on Splunk `10.6.0.0`, build
   `1c3c2df1c656`.
3. Make a harmless Pod-template change without changing the Splunk image.
4. Detain and drain one member, then replace only that Pod.
5. Allow the replacement Pod to mount the same `etc` and `var` PVCs.
6. Docker-Splunk invokes `splunk start` through its normal Ansible startup.
7. Splunk treats the start as a migration and the KV Store precheck fails.

The same startup failure was also observed on the deployer Pod after a
same-image replacement, so the symptom is not limited to one Search Head
ordinal.

## Exact failure

The Ansible `Start Splunk via CLI` task retries five times. Each attempt fails,
and the container exits with code 2:

```text
This appears to be an upgrade of Splunk.

Migrating to:
VERSION=10.6.0.0
BUILD=1c3c2df1c656

Version file="/opt/splunk/var/run/splunk/kvstore_upgrade/versionFile70"
does not exists or not accessible!
Version file="/opt/splunk/var/run/splunk/kvstore_upgrade/versionFile80"
does not exists or not accessible!

isKVstoreDisabled=0
isKVstoreDatabaseFolderExist=0
isKVstoreDiagnosticsFolderExist=0
isKVstoreVersionFileFolderExist=1
isKVstoreVersionFileFolderEmpty=0
isKVstoreVersionFileMatched=0
isKVstoreVersionFromBsonMatched=0

Active KVStore version upgrade precheck FAILED!
Some upgrade prechecks failed!
ERROR while running splunk-preinstall.
```

At the time of failure, the observed marker directory contained
`versionFile42`, but not `versionFile70` or `versionFile80`. No KV Store database
directory was present under the configured KV Store path.

The recovery message asks for the previous Splunk version to be reinstalled.
That guidance does not fit this case because the Pod replacement did not change
the Splunk version or build.

## Source-confirmed cause

The observed state is explained by the current Splunk launcher sequence:

1. The migration path in `src/launcher/migrate.cpp` creates
   `versionFile42` when no KV Store version marker exists.
2. A first start can be interrupted after that marker is persisted but before
   MongoDB creates its database and current-version marker.
3. On the next start, `splunk-preinstall` sees a non-empty marker directory.
4. The current preinstall check accepts `versionFile70` or `versionFile80`, but
   not `versionFile42`, so it stops Splunk before runtime initialization can
   select the supported MongoDB version.

This is a persisted partial-initialization state produced by Splunk itself. A
Kubernetes Pod replacement exposes it because the replacement correctly
reattaches the same persistent volume.

Read-only inspection of the preserved test volumes confirmed the same state on
the deployer and all three Search Heads:

| Instance | Marker directory | KV Store database |
| --- | --- | --- |
| Deployer | `versionFile42` only | Missing |
| Search Head 0 | `versionFile42` only | Missing |
| Search Head 1 | `versionFile42` only | Missing |
| Search Head 2 | `versionFile42` only | Missing |

## Narrow Splunkd recovery spike

A Splunkd spike is being validated to address both production and recovery of
this state:

- migration no longer creates an unsupported `versionFile42` for a fresh KV
  Store;
- preinstall removes `versionFile42` only when KV Store is enabled, its
  database path is known, no MongoDB database directory exists, and
  `versionFile42` is the only entry in the marker directory;
- a legacy marker is preserved and startup continues to fail closed if MongoDB
  data exists, another directory entry exists, the database path cannot be
  resolved, or the state is otherwise ambiguous; and
- when a supported `versionFile70` or `versionFile80` exists, migration may
  remove duplicate obsolete markers without changing the supported selection.

The recovery does not fabricate `versionFile70` or `versionFile80`. After the
verified stale marker is removed, normal KV Store initialization remains
responsible for selecting and recording the supported MongoDB version.

The spike is isolated on `codex/kvstore-same-version-restart`. It is not yet a
product-approved or merged fix. Qualification must use the package built from
the exact reviewed commit and must demonstrate both recovery and fail-closed
behavior.

## Kubernetes impact

The member is correctly removed from serving endpoints before replacement, but
the replacement never starts Splunk and cannot rejoin the cluster. The Search
Head Cluster remains available through the other two members, but it is left
with reduced redundancy and the rolling operation cannot safely continue.

Increasing readiness, liveness, or startup-probe thresholds cannot resolve this
failure because the Splunk process exits on its own.

## Product review questions

1. Is any supported customer state expected to contain only `versionFile42`
   while the configured MongoDB database directory is absent?
2. Are the proposed recovery predicates sufficient to prove that removing the
   marker cannot disconnect it from real MongoDB data?
3. Should preinstall distinguish same-build restart from cross-version upgrade
   in its diagnostics even when both use the migration path?
4. What additional upgrade-path coverage is required before this guarded
   recovery can be productized?
5. Which diagnostics should be retained so Support can distinguish an
   automatically repaired interrupted initialization from a preserved,
   ambiguous legacy state?

## Expected behavior

A new container using the same Splunk build must be able to mount its existing
PVCs, start idempotently, and rejoin the Search Head Cluster. If the persisted
KV Store state is invalid, Splunk should report the specific state that is
invalid and provide a recovery path that is valid for a same-version restart.

The Operator and Docker-Splunk must not fabricate version markers or bypass
Splunk preinstall checks. Their responsibility is to preserve storage, provide
sufficient startup time, report the Splunk exit reason, and stop a rollout when
the replacement cannot become healthy. The marker lifecycle and guarded
recovery belong in Splunkd and require KV Store product review.
