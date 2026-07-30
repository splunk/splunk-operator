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
