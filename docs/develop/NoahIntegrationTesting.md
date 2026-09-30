---
title: Noah Integration Tests on Kraken
parent: Develop & Contribute
nav_order: 5
---

# Noah integration tests on Kraken

Use this workflow to deploy a Noah-backed C3 to a disposable Kraken vCluster
and run the repository's non-mutating Ginkgo readiness test. Run these commands
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

The initial Ginkgo suite intentionally contains only the framework and
readiness scenario. Distributed data-path, Indexer lifecycle, Search Head
lifecycle, and failure/recovery Ginkgo scenarios belong in separate changes so
each behavior can be run and diagnosed independently. The bundled fixture
currently uses ephemeral Splunk etc/var storage; tests that assert PVC or
storage recovery must use a persistent-storage fixture rather than this
readiness fixture.

There is no separate script-based smoke test. Distributed data-path, Noah API,
bucket-map and warm-bootstrap scenarios will be added as focused Ginkgo specs
in subsequent changes.

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

## Clean up

```bash
make noah-local-down
```

This stops the port-forward and deletes the saved Kraken deployment.
