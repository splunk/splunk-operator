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

## Kubernetes impact

The member is correctly removed from serving endpoints before replacement, but
the replacement never starts Splunk and cannot rejoin the cluster. The Search
Head Cluster remains available through the other two members, but it is left
with reduced redundancy and the rolling operation cannot safely continue.

Increasing readiness, liveness, or startup-probe thresholds cannot resolve this
failure because the Splunk process exits on its own.

## Questions for the KV Store team

1. Is `versionFile42` expected after a fresh start of this 10.6 development
   build? If not, which component should create the 7.0 or 8.0 marker?
2. Why does a same-version, same-build restart enter the KV Store upgrade
   precheck?
3. When no KV Store database directory exists, should the active-version
   upgrade precheck fail, or should it treat the instance as not yet
   initialized?
4. Should the precheck recognize that the persisted
   `/opt/splunk/etc/splunk.version` already matches the running binary and avoid
   treating this as a cross-version migration?
5. What is the supported, non-destructive recovery for this state? We do not
   want to fabricate marker files, bypass the precheck, or delete customer PVCs.

## Expected behavior

A new container using the same Splunk build must be able to mount its existing
PVCs, start idempotently, and rejoin the Search Head Cluster. If the persisted
KV Store state is invalid, Splunk should report the specific state that is
invalid and provide a recovery path that is valid for a same-version restart.

No splunkd change or precheck bypass is proposed in the current Operator work.
The immediate request is for the KV Store team to confirm the intended marker
lifecycle, explain why this state is produced, and identify the supported fix.
