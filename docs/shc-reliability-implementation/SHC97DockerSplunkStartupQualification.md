# SHC-97 Docker-Splunk Single-Start Qualification

## Scope

SHC-97 addresses a Docker-Splunk/Ansible startup failure found during a
same-image, same-PVC Cluster Manager replacement. A nonzero result from the
first `splunk start` did not prove that the process had failed: splunkd and
MongoDB were already running and KV Store was still initializing. Retrying the
complete start command then treated the live port 8191 listener as a conflict
and caused the container to exit.

This record qualifies the bounded orchestration correction that issues one
start command and, only when that command returns nonzero, polls the existing
process with `splunk status` under a fatal bounded wait. It is separate from
the earlier Splunk KV Store version-marker defect and does not contain a
Splunkd change.

## Immutable inputs

- Kubernetes context:
  `arn:aws:eks:us-west-2:667741767953:cluster/vivek-spl-301372`;
- Kubernetes server: EKS `v1.31.14-eks-8f14419`;
- namespace: `shc-final-qualification`;
- topology: one LicenseManager, one ClusterManager, four indexers with RF3/SF2,
  one Deployer, and three Search Heads;
- Splunk build: `10.5.2605.0/844c593e9c1d`;
- Splunk-Ansible source:
  `ae8ecf4af1eb4c143a441e440626a17a5dfeaf6a`;
- Docker-Splunk source:
  `118cae68a8fdecbac1286582d32eecd996510564`;
- Operator functional source used by the live campaign:
  `14d885390`;
- deployed Operator OCI digest:
  `sha256:a9f2125097fa823d5182e8729683e5099116a889fdae8e892f0bd3110a8cdf3d`;
- runtime tag:
  `shc-final-docker-118cae6-ansible-ae8ecf4-splunkcloud-10.5.2605.0-844c593e9c1d`;
- runtime OCI index:
  `sha256:49b12103f8444319dcf823eb829d2dfc020410e44d46273461c1b15e52c724fd`;
- Linux AMD64 runtime manifest:
  `sha256:e790463feefcde666a4ea20e6a602f912feec99786c72c7b2cb7223f80964452`;
- runtime image configuration:
  `sha256:79f50a227b97875919c2bfcc8ef7b66057e517bf3859e2c314c8c12c36538be0`;
  and
- feature gates: `SplunkPodLifecycle=true`,
  `SearchHeadClusterLifecycle=true`, and `IndexerClusterLifecycle=true`.

Docker-Splunk built the image on the Linux workstation through the repository
Make target. The build verified the published SHA-512 for the official Splunk
artifact. Image inspection showed the exact Splunk-Ansible commit in
`/opt/ansible/version.txt` and the fatal single-start status-poll task in the
runtime filesystem before the image was pushed by immutable digest.

## Source and packaging gates

The exact Splunk-Ansible source passed `make shc-check`, including syntax and
lint execution, 25 existing Search Head tests, and two new single-start
regression tests. The tests require that the initial start task have no Ansible
retry loop, that a nonzero result invoke `splunk status`, and that status use
the existing bounded retry and delay policy. An executable exhaustion check
returned nonzero after the configured status retries, proving that the wait
cannot fall through as success.

The exact Docker-Splunk source passed `make test_ansible_ref` with four tests.
The Makefile and its regression test both pin the qualified Splunk-Ansible
commit, so a build cannot silently return to the earlier repeated-start task.

## Live Cluster Manager qualification

The LicenseManager was first moved to the target digest because the Operator
correctly prevented a dependent tier from progressing while its declared
image disagreed with the dependency's desired image. Its replacement retained
both PVCs, completed Ansible with `ok=95`, `changed=4`, and `failed=0`, became
Ready, and recorded zero container restarts.

The Cluster Manager then completed two target-image replacements:

| Replacement | Pod UID | Initial starts | Status waits | Port 8191 conflict | Ansible result | Initial start time |
|---|---|---:|---:|---:|---|---:|
| Image convergence | `5ace6777-dc8b-482f-8f6b-586b27e84434` | 1 | 0 | 0 | `ok=113`, `changed=7`, `failed=0` | 13.98 s |
| Controlled same-image deletion | `5f17f483-3540-4a41-88d2-06df3e924b7b` | 1 | 0 | 0 | `ok=113`, `changed=7`, `failed=0` | 14.03 s |

Both replacements reused Cluster Manager PVC
`pvc-etc-splunk-shcfinal-cluster-manager-0` with UID
`5b3ba784-4068-4f4d-bbfe-1279834a2e52` and PVC
`pvc-var-splunk-shcfinal-cluster-manager-0` with UID
`2ddeeba6-fede-4737-88a8-2ae2602491e3`. The second replacement finished on the
same StatefulSet revision and immutable image as the first, so it was a
persistent same-image restart rather than an image upgrade.

The live starts returned zero and therefore did not enter the conditional
status-poll branch. They establish exact-image packaging, same-PVC startup,
clean first-attempt behavior, and absence of the earlier repeated-start crash.
The nonzero and exhausted-poll paths remain established by executable source
tests rather than by a deliberately failed production-style Pod.

## Full tier convergence

The same immutable runtime was then rolled through the referenced tiers in
dependency order. This prevented the successful Cluster Manager check from
being isolated from the rest of the real topology.

### Indexer Cluster

The Operator replaced indexers in order `3 -> 2 -> 1 -> 0`. It withdrew one
target from readiness, waited for decommissioning and primary reassignment,
replaced only that Pod, and required the new peer to be Ready, `Up`, and
searchable before selecting the next ordinal.

| Ordinal | Final Pod UID | Starts | Status waits | Port 8191 conflict | Ansible result | Initial start time |
|---:|---|---:|---:|---:|---|---:|
| 3 | `1cfab1bb-fe81-42b0-a833-66fd3b01c90f` | 1 | 0 | 0 | `ok=111`, `changed=10`, `failed=0` | 14.71 s |
| 2 | `d1933a65-46df-48a8-8670-940835c474fe` | 1 | 0 | 0 | `ok=111`, `changed=10`, `failed=0` | 14.87 s |
| 1 | `f6f66791-990c-4534-a47a-2b52902bb9ac` | 1 | 0 | 0 | `ok=111`, `changed=10`, `failed=0` | 14.56 s |
| 0 | `72b25334-7fa9-412b-9bb5-55e55051840a` | 1 | 0 | 0 | `ok=111`, `changed=10`, `failed=0` | 14.91 s |

All eight indexer PVC UIDs remained unchanged and every replacement recorded
zero container restarts. Final Cluster Manager status reported replication and
search factors met, all data searchable, all peers Up, compatible versions,
no fixup work, and readiness for a searchable rolling restart.

### Search Head Cluster

The Search Head declaration bound `SameVersionRestart` to the exact source and
target image references. The Operator replaced the Deployer and then Search
Head members in order `2 -> 1 -> 0`. Each target left the serving EndpointSlice
before termination, entered manual detention, and returned to `Up` and serving
before the next member was selected.

| Role | Final Pod UID | Starts | Status waits | Port 8191 conflict | Ansible result | Initial start time |
|---|---|---:|---:|---:|---|---:|
| Deployer | `a4ed1853-54c3-4bc3-98b6-7ac29b94bcbd` | 1 | 0 | 0 | `ok=111`, `changed=7`, `failed=0` | 14.10 s |
| Search Head 2 | `bf1b42e4-7870-45e3-ae44-363a47186c16` | 1 | 0 | 0 | `ok=132`, `changed=8`, `failed=0` | 14.90 s |
| Search Head 1 | `491d5135-38dd-4306-adb5-9bea9bfff84f` | 1 | 0 | 0 | `ok=132`, `changed=8`, `failed=0` | 13.70 s |
| Search Head 0 | `49bda3e4-a417-4735-abdd-f581cbec6ebf` | 1 | 0 | 0 | `ok=132`, `changed=7`, `failed=0` | 14.20 s |

Ordinal 1 was captain when its turn began. Captaincy moved from ordinal 1 to
ordinal 0 before its partition was lowered. Ordinal 0 was then captain when
its turn began, and captaincy moved from ordinal 0 to ordinal 1 before
partition zero was released. The Service retained at least two serving Search
Head endpoints and later returned to three. All eight Search Head and Deployer
PVC UIDs remained unchanged, and every container restart count remained zero.

The rendered termination grace period was 1,200 seconds for every managed
Splunk Pod. Kubernetes recorded 43, 43, and 44 seconds respectively between
the old ordinal `2`, `1`, and `0` container `Killing` event and scheduling of
the replacement. None approached grace expiry or required a container restart.

Final Splunk status reported dynamic captaincy, `service_ready_flag=1`, and all
three members `Up`. The SearchHeadCluster reported `Ready`, three of three
ready members, partition three, and equal current and update revisions. No
image-upgrade workflow was used.

## Continuous availability evidence

The first workload window covered LicenseManager convergence, both Cluster
Manager replacements, and the start of the indexer roll. It submitted 120
numbered HEC events with zero HEC failures and zero search-request failures,
then converged to exact `count=120`, `min=1`, `max=120`, and `distinct=120`.
Search Head endpoints remained three, indexer endpoints remained at least
three, and total container restarts remained zero. Evidence file
`build/_test/shc-final/shc-final-cluster-manager-single-start-118cae6.log`
has SHA-256
`990a36a757a3959c26bc057695f684c21eea2b52ebc59fa65b77b10ebd117aac`.

The second workload window covered the complete Indexer Cluster and Search
Head Cluster rolls plus final steady-state recovery. It submitted 240 numbered
HEC events with zero HEC failures and issued 240 distributed-search samples
with zero request failures. The final result was exactly `count=240`,
`min=1`, `max=240`, and `distinct=240`. At least three indexer endpoints and
two Search Head endpoints remained available during their respective
withdrawals, and total container restarts remained zero. Evidence file
`build/_test/shc-final/shc-final-tier-roll-118cae6.log` has SHA-256
`8817efd5ea9cdc0c1f8d6db3b0b0ff66ba72120da62a0bf945762e8852a87191`.

The successful-search result count was not monotonic during indexer
replacement. The first window recorded two regressions with maximum pending
28, and the tier window recorded four regressions with maximum pending 13;
every missing result later became searchable and both runs reached exact final
completeness. The regressions occurred during the Indexer Cluster roll, not
the Search Head member roll. This is not recorded as zero-disruption search
completeness. It is the already-open SHC-85/OPS-011 distributed-search gap:
the Splunk search response did not expose a partial-result signal even while a
successful request temporarily returned fewer acknowledged events.

## Final state and bounded conclusion

LicenseManager, ClusterManager, IndexerCluster, and SearchHeadCluster all
finished `Ready=True` on the exact target digest. Every managed Splunk Pod was
Ready with zero container restarts. The Operator log contained zero scoped
Reconciler errors during the campaign.

This closes SHC-97 for the bounded source, packaging, same-PVC EKS startup, and
full-topology startup contract. It does not prove every old-to-new
version path, legacy KV Store state, storage provider, Kubernetes provider, or
nonzero live-start timing. It does not close immediate distributed-search
completeness during indexer replacement. No marker fabrication, process kill,
customer-data deletion, probe weakening, or Splunkd modification is part of
the accepted result.
