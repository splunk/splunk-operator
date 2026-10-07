---
title: Noah Integration Tests on Kraken
parent: Develop & Contribute
nav_order: 5
---

# Noah integration tests on Kraken

Use this workflow to deploy a Noah-backed C3 to a disposable Kraken vCluster
and run focused Ginkgo scenarios against it. Run these commands
from the repository root on a vWorkstation.

## Prerequisites

Install `git`, `go`, `kraken`, `kubectl`, `helm`, `curl`, `jq`, `yq` and
`openssl`. You also need:

- An immutable Noah-capable Splunk Enterprise image.
- A valid Splunk Enterprise license stored outside the repository.
- A staged operator image. The default is the current commit at
  `docker-test.repo.splunkdev.net/sok/splunk-operator`.
- A user-supplied `SPLUNK_GENERAL_TERMS` value after following the
  [terms guidance](../README.md#splunk-general-terms-acceptance).

Both Helm 3 and Helm 4 are supported. The workflow selects Helm 4's compatible
wait strategy automatically.

## Deploy C3

```bash
export NOAH_LOCAL_LICENSE_FILE=/absolute/path/to/enterprise.lic
export NOAH_LOCAL_SPLUNK_IMAGE='<immutable Noah-capable Splunk image>'
export SPLUNK_GENERAL_TERMS='<your supplied terms value>'

# Set this only when the default commit-addressed image is unavailable:
# export NOAH_LOCAL_OPERATOR_IMAGE='<immutable staged operator image>'

make noah-local-c3-up
```

The command creates or reuses the saved Kraken deployment, installs Noah,
cert-manager and the operator, applies the C3 fixture, and runs the non-mutating
Ginkgo readiness scenario. That scenario requires generation-current CR status,
ready Pods and running Splunk processes. State is kept under
`.noah-local-dev/`, so rerunning the command continues an interrupted setup.

The bundled fixture expects context `kraken`, namespace `splunk-operator`, C3
name `c3` and port `8443`. Supply a matching fixture if you override them.

## Run the readiness test

```bash
make noah-local-ready
```

The readiness target runs the `scenario:readiness` spec in `test/noah`. It checks
that the operator and Noah Deployments are rolled out, the LicenseManager,
IndexerCluster and SearchHeadCluster report generation-current readiness, every
expected member is represented in CR status, each Pod is ready, and `splunkd`
is running. A generation-current `Stalled=True` condition fails immediately.

For framework-only validation that does not create a cluster, run:

```bash
make test-testenv
make test-noah-framework
```

Override `NOAH_LOCAL_READY_TIMEOUT` (default `30m`) only when required. The
target fails before running if the active kubeconfig context is not
`NOAH_LOCAL_CONTEXT` (default `kraken`).

The attached readiness test requires the in-cluster operator workflow because
it validates the operator Deployment as part of the system. The local `go run`
workflow remains useful for reconciliation development, but it is not a valid
substitute for this end-to-end readiness gate.

## Run membership and scaling checks

After C3 is ready:

```bash
make setup/ginkgo noah-local-test-context

# Read-only: observe each current indexer in Noah.
ginkgo -v --trace \
  --label-filter='tier:noah-e2e && scenario:membership' ./test/noah

# Mutating: add one indexer, then restore the original replica count.
ginkgo -v --trace \
  --label-filter='tier:noah-e2e && scenario:scaling' ./test/noah
```

Ensure Go's binary directory is on `PATH`; see [development setup](DevelopmentSetup.md).

These scenarios attach to the running stack without deploying another one.
Healthy runs finish as soon as their checks pass; timeouts are backstops.

| Scenario | Checks | Changes |
| --- | --- | --- |
| `membership` | Each expected indexer is `up` in Noah, with a current incarnation and the expected HTTPS management address | None |
| `scaling` | Add one indexer, then remove it; verify current membership, completed lifecycles, unchanged existing Pod UIDs, and removal of the extra Pod and its PVCs | Temporarily increases replicas by one, then restores the original count |

The fixture starts with two indexers, so this runs `2 → 3 → 2`. A three-indexer
deployment runs `3 → 4 → 3`. Cleanup restores the replica count on failure, but
does not force-delete resources or clear lifecycle status. It refuses to
overwrite another user's scaling or a recreated CR, and refuses to consume
pre-existing PVCs for the extra ordinal. Run on a disposable stack
with no concurrent configuration changes.

Scale-in completes when the removed peer is absent or `down` in Noah, not when
it disappears from a bucket map. These scenarios do not verify bucket-map
repairs, remote persistence/recovery, or service availability during disruption.
Rollout, SHC lifecycle, and fault-injection scenarios remain separate follow-ups.

Noah requests use HMAC v3 with the referenced Secret and travel through the
Kubernetes Service proxy; no local Noah port-forward or DNS entry is needed.
The test user needs `get` on the `NoahCluster` and its auth Secret, `get` on the
Noah `services/proxy` subresource, and permission to execute commands in Splunk
Pods. Scaling also requires `get` on StatefulSets, Pods, PersistentVolumeClaims,
and the IndexerCluster, and permission to patch the IndexerCluster.

The suite's shared Kubernetes client is backed by a cluster-wide cache, so the
test user also needs `list` and `watch` in all namespaces on Pods, Events,
Deployments, IndexerClusters, SearchHeadClusters, and LicenseManagers, plus
StatefulSets for scaling. The Noah inputs are read without the cache, so no
cluster-wide Secret access is required.

The local chart uses mock authentication, so a successful request there does not
prove credential enforcement.

Both scenarios use `tier:noah-e2e` and `cloud:kraken` for CI selection. The
commands above use the fixture defaults; for overrides, set `NOAH_TEST_NAMESPACE`,
`NOAH_TEST_OPERATOR_NAME`, `NOAH_TEST_NOAH_DEPLOYMENT`, `NOAH_TEST_C3_NAME`, and
optionally `NOAH_TEST_NOAH_SERVICE` (defaults to the Noah Deployment name).
Confirm the active kubeconfig context before invoking Ginkgo directly.

## Clean up

```bash
make noah-local-down
```

This stops the port-forward and deletes the saved Kraken deployment.
