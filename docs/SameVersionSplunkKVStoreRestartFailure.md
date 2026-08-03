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

## Official fixed-build revalidation

The reported product fix has now passed a bounded Kubernetes revalidation with
official Splunk Cloud build `10.5.2605.0`, build `844c593e9c1d`. The artifact
was:

`https://repo.splunkdev.net/artifactory/generic-west-local/splcore/builds/develop/844c593e9c1d83a6e89a3e4b2ed68becf11f6948/231923365/splunkcloud-10.5.2605.0-844c593e9c1d-linux-amd64.tgz`

Docker-Splunk verified the artifact's published SHA-512 during the build. The
resulting runtime was pushed and deployed by immutable digest:

`sha256:2b6d0f3b316eca90f061bfc22be2f6fc59c960fcfaa6791a871c0a5d4ee0b2c2`

The revalidation first replaced one indexer Pod with the same image digest and
the same persistent `/opt/splunk/etc` and `/opt/splunk/var` claims. Before
replacement, the mounted KV Store contained a populated MongoDB/WiredTiger
database. The replacement:

- mounted the same two PVC UIDs;
- reported the exact `10.5.2605.0`/`844c593e9c1d` identity;
- completed Docker-Splunk/Ansible startup and its internal Splunk restart with
  `ok=111`, `failed=0`;
- reached Kubernetes Ready with zero container restarts; and
- produced zero matches for
  `Active KVStore version upgrade precheck FAILED`.

The Operator then completed an automated same-image StatefulSet revision roll
across four persistent indexers in order `3 -> 2 -> 1 -> 0`. Every replacement
reused its persistent data, completed Ansible with `failed=0`, reached Ready
with zero container restarts, retained its Splunk peer GUID, and produced no
prior failure signature. Cluster Manager finished with RF and SF met, all data
searchable, all peers Up, and no fixups.

Continuous workload evidence also passed:

- the primary run submitted 80 numbered events with zero HEC failures and zero
  search-request failures, then found all 80 exactly once; and
- a continuation through the final replacement and stable post-roll period
  submitted 30 events with zero failures and found all 30 exactly once.

This closes the original same-version, existing-volume failure for the tested
official build and topology. It does not by itself qualify every legacy
marker state, supported upgrade path, Search Head role, or older-to-newer
version transition. The exact product-source change in the official artifact
was not inferred from runtime success; source review and broader upgrade
coverage remain separate product gates.

## Separate Docker-Splunk start-retry failure found during final qualification

The official build did not reproduce the original KV Store version-marker
precheck failure. A later same-image Cluster Manager replacement nevertheless
exposed a different startup failure at the Docker-Splunk/Ansible boundary.
This second finding must not be described as a regression of the product fix
above.

On 2 August 2026, Cluster Manager Pod
`splunk-shcfinal-cluster-manager-0` was replaced while preserving both PVCs and
the exact `10.5.2605.0/844c593e9c1d` runtime image. The replacement Pod retained
UID `b48942e5-d016-46ad-a8b0-8f639a53f524` across its container attempts. Its
container exited twice and then succeeded on the third attempt. The two failed
attempts ended in the
Ansible `Start Splunk via CLI` task with exit code 2 and:

```text
ERROR: kvstore port [8191] - port is already bound. Splunk needs to use this port.
```

The same attempt's Splunk log showed that `splunkd` had already started and
launched MongoDB on port 8191. The existing Ansible task retried the complete
`splunk start` command five times whenever that command returned non-zero.
Reissuing `splunk start` while the process from the first invocation was still
initializing converted the live KV Store listener into a false port-conflict
failure. Kubernetes startup-probe budget was not the terminating clock; the
container entrypoint exited after Ansible declared the task failed.

The third container attempt completed and the cluster returned to Ready. RF
and SF were met, all data was searchable, all four peers were Up, no fixups
were active, and continuous HEC and distributed-search requests remained
available. That eventual recovery does not make two avoidable container
crashes an acceptable startup contract.

The bounded Docker-Splunk/Ansible correction is therefore to invoke
`splunk start` once. If that invocation returns non-zero, startup polls
`splunk status` for the already-launched process using the existing bounded
wait policy; it does not issue a second start, kill MongoDB, remove customer
data, fabricate a KV Store marker, or weaken a Kubernetes probe. Source commits
`e0fed1c1a45269ac4f5e4f35c4ad11e4c1ab6300` and
`ae8ecf4af1eb4c143a441e440626a17a5dfeaf6a`, together with Docker-Splunk
pin commit `118cae68a8fdecbac1286582d32eecd996510564`, passed Ansible
syntax/lint execution plus 25 existing SHC unit tests and two new single-start
regression tests. The second source commit also makes exhaustion of the status
poll fatal; it cannot fall through to later startup tasks as a successful
poll.

That exact dependency has now passed a Linux image build and bounded same-PVC
EKS qualification. Docker-Splunk verified the official artifact checksum,
embedded the exact Ansible source, and published runtime OCI index
`sha256:49b12103f8444319dcf823eb829d2dfc020410e44d46273461c1b15e52c724fd`
with Linux AMD64 manifest
`sha256:e790463feefcde666a4ea20e6a602f912feec99786c72c7b2cb7223f80964452`.
Two Cluster Manager replacements reused the same persistent `etc` and `var`
claims. Each issued one initial start, completed Ansible with `failed=0`,
reported no port 8191 conflict, became Ready, and recorded zero container
restarts. The same image then converged across LicenseManager, four indexers,
the Deployer, and three Search Heads. All managed tiers finished Ready with
unchanged PVC identities and zero container restarts; final indexer health had
RF/SF met, all data searchable, all peers Up, and no fixups, while final SHC
health had dynamic captaincy and all three members Up.

The live initial starts returned zero, so the conditional nonzero status-poll
branch was not forced inside a production-style Pod. Its single-start and
fatal-exhaustion behavior remains established by the executable source gate.
The complete immutable inputs, workload evidence, and qualification limits are
recorded in
`shc-reliability-implementation/SHC97DockerSplunkStartupQualification.md`.

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

The official-build result above supersedes the spike as the runtime used for
the current Operator qualification. The spike remains historical diagnostic
work and is not part of the Docker-Splunk or Splunk Operator solution.

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
