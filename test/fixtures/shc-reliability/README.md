# SHC reliability qualification fixtures

The baseline topology uses a `LicenseManager` named `shc82`. The
`ClusterManager`, `IndexerCluster`, and `SearchHeadCluster` each set
`spec.licenseManagerRef.name: shc82`. For the `SearchHeadCluster`, that single
reference configures both the deployer and every Search Head member.

The LicenseManager mounts the `shc82-license` Secret at `/mnt/licenses` and
loads `/mnt/licenses/enterprise.lic`. The license file is deliberately not
stored in Git.

Create or update the Secret before applying the baseline manifest:

```bash
make shc82-license-secret \
  SHC82_NAMESPACE=<qualification-namespace> \
  SHC82_LICENSE_FILE=/absolute/path/to/enterprise.lic
```

The supplied license must support operation as a remote license manager. The
built-in Enterprise trial license does not satisfy this qualification
requirement.

`shc82_appframework_monitor.sh` sends numbered events through the Indexer
Cluster HTTP Event Collector and searches for those events through the Search
Head Cluster Service while collecting Kubernetes, SHC, IDXC, captain, and App
Framework state. It does not print the HEC token or administrator password.
Search Head observations report container readiness, Kubernetes Pod readiness,
the Operator-owned SHC serving gate, and Service EndpointSlice readiness as
separate signals so an internal Splunk restart cannot be mistaken for a
healthy Kubernetes traffic endpoint. Each sample also records the exact
EndpointSlice target Pods, each serving-gate reason, and the local and
captain-reported member/restart states exposed by the SearchHeadCluster.
Failed searches retain a bounded, whitespace-normalized response detail
without logging credentials.
The monitor exits successfully only when every submitted sequence is present
exactly once at the final completeness gate.

Run it from the repository root:

```bash
SHC82_NAMESPACE=<qualification-namespace> \
SHC82_SAMPLES=180 \
test/fixtures/shc-reliability/shc82_appframework_monitor.sh
```

The evidence log is written below `build/_test/shc82` unless
`SHC82_EVIDENCE_FILE` specifies another location.

## SHC-83 initial-formation readiness

`shc83-startup-readiness-cluster.yaml` creates an isolated LicenseManager and
three-member Search Head Cluster using the current qualification runtime. The
license file is deliberately not stored in Git.

Create the namespace and license Secret before applying the fixture:

```bash
kubectl create namespace shc83-startup-readiness
make shc83-license-secret \
  SHC83_LICENSE_FILE=/absolute/path/to/enterprise.lic
```

Start `shc83_startup_readiness_monitor.sh` before applying the fixture. The
monitor fails immediately if any Search Head becomes a ready Service endpoint,
or its Operator-owned serving gate becomes true, before the durable v4 initial
formation stage is complete. It records the formation stage, restart decision,
stabilization timestamp, Pod UIDs, container and Pod readiness, serving-gate
reasons, EndpointSlice targets, container restart counts, local container-state
files, and aggregate SHC formation status.

```bash
SHC83_NAMESPACE=shc83-startup-readiness \
test/fixtures/shc-reliability/shc83_startup_readiness_monitor.sh &
kubectl apply -f \
  test/fixtures/shc-reliability/shc83-startup-readiness-cluster.yaml
wait
```

The default success gate requires twelve consecutive five-second samples with
all desired members ready, serving, present in the client Service
EndpointSlice, reflected by `status.lastStableReplicas`, and reported in the
`Complete` initial-formation stage after the required restart was initiated.

After startup qualifies, use `shc83_established_recovery_monitor.sh` to verify
that deletion of an established non-captain member removes only that member
from client traffic. The monitor fails if either unaffected peer leaves the
EndpointSlice or fewer than two established endpoints remain. It succeeds only
after a replacement Pod with a new UID rejoins the captain view, becomes
serving and ready, returns to the EndpointSlice, and remains stable.

```bash
SHC83_TARGET_POD=splunk-shc83-shc-search-head-1 \
SHC83_EVIDENCE_FILE=build/_test/shc83/established-recovery.tsv \
test/fixtures/shc-reliability/shc83_established_recovery_monitor.sh &
kubectl -n shc83-startup-readiness delete pod \
  splunk-shc83-shc-search-head-1 --wait=false
wait
```
