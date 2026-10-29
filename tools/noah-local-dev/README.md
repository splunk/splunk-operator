# Noah local development

Spins up a Kraken vCluster running Noah, PostgreSQL and Redis, installs
cert-manager, the operator and its CRDs, and creates a SmartStore-backed C3
deployment. Kraken provides the S3 bucket, SQS queue, dead-letter queue and
workload-identity ServiceAccount used by the Indexers. The recommended workflow
runs the operator inside the vCluster; a local `go run` workflow is also
available for operator development. The LicenseManager, Indexers and Search
Heads use test-sized `gp3-automode` persistent volumes.

Both workflows install cert-manager into the vCluster, which issues the operator's
webhook serving certificate. The operator needs that certificate when the
`ValidationWebhook` or `CrossVersionFieldProtection` feature gate is enabled;
`CrossVersionFieldProtection` is on by default.

Prerequisites:

- `curl`, `jq`, `yq`, `envsubst`, `kubectl`, `helm`, `openssl` and `kraken` on
  `PATH`. For Helm, either use your own install or:

  ```console
  make setup/helm HELM_VERSION=3.18.4 CI_BIN_DIR=$(pwd)/bin
  ```

- A valid Splunk Enterprise license file obtained through your approved
  development process and stored outside the repository.
- A Noah-capable Splunk Enterprise image containing the required provisioning
  changes. Use an immutable image produced by the Noah Docker image build
  pipeline.
- A staged operator image for the checked-out commit. The default image name is
  produced by the branch pipeline's `build-stage-image` job.

## Setup

```console
make noah-local-c3-up \
  NOAH_LOCAL_LICENSE_FILE=/absolute/path/to/enterprise.lic \
  NOAH_LOCAL_SPLUNK_IMAGE='<immutable Noah-capable Splunk image>' \
  SPLUNK_GENERAL_TERMS='<your accepted terms>'
```

This creates the vCluster, installs the CRDs, Noah and the operator, then applies
the C3 fixture. `NOAH_LOCAL_OPERATOR_IMAGE` defaults to the staged image for the
currently checked-out commit:

```console
docker-test.repo.splunkdev.net/sok/splunk-operator:$(git rev-parse HEAD)
```

The branch pipeline must have completed successfully for that image to exist.
Override `NOAH_LOCAL_OPERATOR_IMAGE` if it was published elsewhere. The setup
target runs the Ginkgo readiness and Kraken AWS runtime scenarios after the C3
is deployed; rerun them independently with `make noah-local-ready` and `make
noah-local-aws-runtime`. Run the mutating distributed data-path scenario
separately with `make noah-local-data-path`.

Automation can separate deployment from validation without changing the local
workflow:

```console
make noah-local-c3-deploy \
  NOAH_LOCAL_LICENSE_FILE=/absolute/path/to/enterprise.lic \
  NOAH_LOCAL_SPLUNK_IMAGE='<immutable Noah-capable Splunk image>' \
  SPLUNK_GENERAL_TERMS='<your accepted terms>'
make noah-local-test \
  NOAH_LOCAL_GINKGO_ARGS='--no-color --output-dir=ci-output --junit-report=noah.xml'
```

`noah-local-test` initializes the attached Ginkgo suite once and runs the
readiness, AWS-runtime and data-path specs selected by
`NOAH_LOCAL_TEST_LABEL_FILTER` (default: `tier:noah-e2e && cloud:kraken`). The
existing scenario-specific targets remain available for focused development.

The fixture uses `gp3-automode` PVCs for each Splunk workload's etc and var
volumes. Ensure that StorageClass is available in the vCluster before deploying.
Changing an existing ephemeral C3 to this fixture does not replace its
StatefulSet storage; use a fresh development deployment for the PVC setup.

### Run the operator locally

For an operator development loop, prepare the cluster and port-forward:

```console
make noah-local-up NOAH_LOCAL_LICENSE_FILE=/absolute/path/to/enterprise.lic
printf '%s\n' '127.0.0.1 noah.splunk-operator.svc' | sudo tee -a /etc/hosts
```

As well as the Noah port-forward, this issues the operator's webhook serving
certificate and copies it to `/tmp/k8s-webhook-server/serving-certs`, which is
where the operator reads it from. Re-run `make noah-local-webhook-certs` if
cert-manager later rotates the certificate, or if that directory is cleared.

Then run the operator, supplying your own accepted terms:

```console
export RELATED_IMAGE_SPLUNK_ENTERPRISE='<immutable Noah-capable Splunk image>'
SPLUNK_GENERAL_TERMS='<your accepted terms>' \
  WATCH_NAMESPACE=splunk-operator \
  go run ./cmd/main.go
```

Do not run local and in-cluster operators at the same time.

The attached readiness target requires the in-cluster workflow because it
validates the operator Deployment as part of the system.

Tear down when you are done

```console
make noah-local-down
```

## Targets

`make noah-local-c3-up` runs the complete in-cluster workflow. `make
noah-local-up` prepares the equivalent local-operator workflow and starts the
required Noah port-forward. Each constituent target also runs on its own. `make
help` lists the targets under **Noah Local Development**. They are defined in
[`noah.mk`](noah.mk), which the root `Makefile` includes.

| target                         |                                                                                                                          |
| ------------------------------ | ------------------------------------------------------------------------------------------------------------------------ |
| `noah-local-c3-up`             | create a complete C3 deployment with an in-cluster operator                                                              |
| `noah-local-c3-deploy`         | create that C3 deployment without running attached Ginkgo scenarios                                                      |
| `noah-local-up`                | prepare a C3 deployment and port-forward for an operator running locally                                                 |
| `noah-local-cluster`           | create the vCluster, configure access, and save only its non-secret S3/SQS/workload-identity coordinates              |
| `install`                      | install all CRDs from `config/crd/bases`                                                                                 |
| `noah-local-deploy`            | `helm upgrade --install` of [`helm/charts/noah`](../../helm/charts/noah), waits for ready                                |
| `noah-local-cert-manager`      | install cert-manager, which issues the operator's webhook serving certificate                                            |
| `noah-local-webhook-certs`     | issue that certificate and copy it out for a locally run operator                                                        |
| `noah-local-operator-deploy`   | install or upgrade a staged operator image in the vCluster                                                               |
| `noah-local-fixtures`          | create prerequisite Secrets and render the C3 fixture with the active Kraken runtime coordinates                        |
| `noah-local-port-forward`      | forward the Noah service to localhost                                                                                    |
| `noah-local-ready`             | run the non-mutating Noah Ginkgo readiness scenario against the deployed C3                                              |
| `noah-local-aws-runtime`       | verify Indexer workload identity and effective S3/SQS SmartStore configuration                                           |
| `noah-local-data-path`         | ingest on every Indexer, verify each exact rolled bucket in Kraken S3, then verify distributed search through the SHC    |
| `noah-local-test`              | run all selected attached Kraken scenarios once, with optional Ginkgo/JUnit arguments                                    |
| `noah-local-runtime-context`   | verify the saved external S3/SQS runtime metadata belongs to the active Kraken deployment                                 |
| `noah-local-test-context`      | verify that the active kubeconfig context matches `NOAH_LOCAL_CONTEXT` before attached tests run                          |
| `noah-local-destroy`           | terminate the vCluster                                                                                                   |
| `noah-local-stop-port-forward` | stop the forward                                                                                                         |
| `noah-local-deployment-id`     | print the saved deployment ID                                                                                            |
| `noah-local-lint`              | lint the chart and deployment helpers                                                                                    |

State lives in `.noah-local-dev/`. Re-running `noah-local-cluster` reuses the
saved deployment, or creates a new one if it has gone. The runtime-state file
contains only the allowlisted deployment identity and resource coordinates.
Kraken's full connection response and temporary AWS credentials are never
written to disk. For a new deployment, the deployment ID is written atomically
before the bounded readiness wait begins, so `make noah-local-down` can still
clean up after a timeout or later setup failure.

## SmartStore

The Kraken request provisions an S3 bucket, SQS queue, dead-letter queue and a
`splunk-runtime` ServiceAccount linked to AWS workload identity. Cluster setup
captures only their non-secret coordinates. Fixture rendering assigns the
ServiceAccount to the IndexerCluster and configures SmartStore directly; the
LicenseManager and SearchHeadCluster remain on the namespace's default
ServiceAccount. Static AWS access-key variables are not used. All three Splunk
roles use `gp3-automode` PVCs (`10Gi` for `etc`, `20Gi` for `var`) so the
baseline exercises persistent StatefulSet storage semantics without claiming
lifecycle-recovery coverage.

The chart retains a development-only MinIO fallback for use outside this
workflow, but `make noah-local-deploy` disables it because the Kraken resources
are authoritative for these integration tests. The mandatory Helm override is
applied after caller-provided Helm arguments, so this workflow cannot be
switched back to MinIO accidentally. Every attached Kraken Ginkgo scenario also
checks the saved S3/SQS values and fails if chart-managed MinIO workloads,
services, Secrets, Jobs or Pods exist. MinIO is unsupported for the Kraken and
EKS Noah integration-test paths; an inert retained PVC is not selected or used.

When upgrading a pre-existing development release from the MinIO workflow,
Kubernetes retains the old StatefulSet PVC by default even after MinIO is
disabled. This protects existing data from implicit deletion. The disposable
Kraken cluster teardown removes it; otherwise, delete that PVC manually only
after confirming its data is no longer needed.

Only Noah API authentication is mocked; SmartStore uses the real Kraken AWS
resources. `noah-local-ready` and `noah-local-aws-runtime` run non-mutating
Ginkgo scenarios.
`noah-local-data-path` adds uniquely marked data to `main`, rolls hot buckets
and keeps each `oneshot` source in place while rolling and polling through the
SHC. It captures the exact bucket containing each searchable marker, verifies
that bucket through Splunk's SmartStore `rfs` listing, and then searches all
markers through the SHC. The source marker must come from every Indexer, while
any currently ready Indexer may serve the result after replication,
rebalancing, or bucket-primary movement. The check proves an S3 receipt exists
for the exact rolled bucket containing each run marker; it does not prove
cold-cache rehydration or SQS consumption. Noah API, bucket-map and
warm-bootstrap checks are out of scope and require separate Ginkgo scenarios;
there is no script-based smoke-test path.

The attached suite also provides read-only `scenario:membership` checks and a
`scenario:scaling` replica round trip. Scaling verifies peer and PVC cleanup
without restarting existing Pods; run it on a disposable stack with no concurrent
configuration changes. Neither scenario needs a Noah port-forward.

## Why the /etc/hosts entry

The `NoahCluster` endpoint is an in-cluster address,
`http://noah.splunk-operator.svc:8443`. Indexer pods resolve it through cluster
DNS; mapping the same name to `127.0.0.1` locally means your operator resolves it
through the port-forward. One endpoint value works from both sides.

It needs `sudo`, so it is not automated — `noah-local-port-forward` prints the
command if the entry is missing. A `401` from
`curl http://noah.splunk-operator.svc:8443/` means you reached Noah.

## Before you run the operator

- Run either the local operator or `noah-local-operator-deploy`, not both.
  `go run` takes no leader-election lease, so both would reconcile the same
  resources.
- Use a branch build. A released operator image ignores `NoahCluster` silently.

## Overrides

`NOAH_LOCAL_*` variables, listed at the top of the section in the
[`Makefile`](../../Makefile) — context, port, release name, chart, fixtures,
Helm args, deploy timeout, and the in-cluster operator release, images, chart,
Helm args and timeout.

Kraken calls use the production control plane by default, as selected by the
production CLI. New deployments wait
`NOAH_LOCAL_KRAKEN_WAIT_TIMEOUT` (default `20m`). CI can also set
`NOAH_LOCAL_KRAKEN_TTL_SECONDS`, `NOAH_LOCAL_KRAKEN_PURPOSE`, and the optional
`NOAH_LOCAL_KRAKEN_WORKFLOW_TAG`, `NOAH_LOCAL_KRAKEN_PIPELINE_TAG` and
`NOAH_LOCAL_KRAKEN_SHA_TAG` values so deployments are identifiable and
eventually expire if active cleanup cannot run. Those metadata fields remain
unset for normal local use.

Keep `NOAH_LOCAL_NAMESPACE=splunk-operator`: Kraken currently provisions the
runtime workload-identity ServiceAccount in that namespace. `NOAH_LOCAL_PORT`
does not reach `fixtures/c3.yaml`, which is plain YAML. If you override the
port, edit the endpoint to match or point `NOAH_LOCAL_FIXTURES` at your own
copy.

## Fixture notes

- The C3 LicenseManager expects a `splunk-license` Secret containing an
  `enterprise.lic` key. Pass the file when applying the fixture:

  ```console
  make noah-local-fixtures \
    NOAH_LOCAL_LICENSE_FILE=/absolute/path/to/enterprise.lic
  ```

  The target reuses an existing `splunk-license` Secret when the variable is
  omitted. License contents are never stored in the repository.

- The Secret key is `pass4SymmKey`, exactly that casing
  (`pkg/splunk/enterprise/noah_indexer.go`). Wrong casing still bootstraps pods
  but fails calls to Noah.
- The `pass4SymmKey` is generated at runtime and never committed. An existing
  Secret is left alone.
- Before the C3 resources are applied, `create-splunk-secret` puts a generated
  `splunk_secret` in the operator's namespace-scoped Secret
  (`splunk-<namespace>-secret`) so every Splunk pod starts with the same
  `splunk.secret`. Otherwise each pod generates its own, and a Search Head
  that later joins a different captain re-encrypts `server.conf` at runtime;
  until splunkd restarts, its searches fail to parse
  `[noahService]/pass4SymmKey` and return no results. The value is only added
  while no Splunk pods exist, because changing it afterwards rotates
  `splunk.secret` under existing encrypted values.
- The fixture does not override `spec.image`; all Splunk roles use the image
  configured for the operator under test.
