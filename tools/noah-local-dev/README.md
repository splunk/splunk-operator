# Noah local development

Spins up a Kraken vCluster running Noah, PostgreSQL, Redis and MinIO, installs
the operator CRDs, and creates a SmartStore-backed C3 deployment. You run the
operator itself locally against that cluster.

Needs `jq`, `yq`, `kubectl`, `helm`, `openssl` and `kraken` on `PATH`. For Helm,
either use your own install or:

```console
make setup/helm HELM_VERSION=3.18.4 CI_BIN_DIR=$(pwd)/bin
```

## Setup

```console
make noah-local-up NOAH_LOCAL_LICENSE_FILE=/absolute/path/to/enterprise.lic
printf '%s\n' '127.0.0.1 noah.splunk-operator.svc' | sudo tee -a /etc/hosts
```

Then run the operator, supplying your own accepted terms:

```console
SPLUNK_GENERAL_TERMS="--accept-sgt-current-at-splunk-com" WATCH_NAMESPACE=splunk-operator go run ./cmd/main.go
```

Tear down when you are done

```console
make noah-local-down
```

## Targets

`make noah-local-up` chains the first five of these. Each also runs on its own,
in this order. `make help` lists them under **Noah Local Development**. They are
defined in [`noah.mk`](noah.mk), which the root `Makefile` includes.

| target                         |                                                                                                                          |
| ------------------------------ | ------------------------------------------------------------------------------------------------------------------------ |
| `noah-local-cluster`           | create the vCluster, write the `kraken` kubeconfig context, copy Kraken's Artifactory pull secret into `splunk-operator` |
| `install`                      | install all CRDs from `config/crd/bases`                                                                                 |
| `noah-local-deploy`            | `helm upgrade --install` of [`helm/charts/noah`](../../helm/charts/noah), waits for ready                                |
| `noah-local-fixtures`          | create prerequisite Secrets, apply [`fixtures/c3.yaml`](fixtures/c3.yaml)                                                |
| `noah-local-smoke`             | index one marker per indexer, roll the buckets, and search every marker through the SHC                                  |
| `noah-local-port-forward`      | forward the Noah service to localhost                                                                                    |
| `noah-local-destroy`           | terminate the vCluster                                                                                                   |
| `noah-local-stop-port-forward` | stop the forward                                                                                                         |
| `noah-local-deployment-id`     | print the saved deployment ID                                                                                            |
| `noah-local-lint`              | lint the chart and scripts                                                                                               |

State lives in `.noah-local-dev/`. Re-running `noah-local-cluster` reuses the
saved deployment, or creates a new one if it has gone.

## SmartStore

The Noah chart installs a private MinIO service and creates the
`noah-smartstore` bucket. The C3 IndexerCluster stores the `main` index at
`s3://noah-smartstore/c3/main` through `http://noah-minio:9000`. MinIO is only
reachable inside the vCluster.

The `noah-minio` Secret contains the S3 access key and a randomly generated
secret key. The chart preserves both across upgrades so retained SmartStore
data remains accessible. As with the PostgreSQL password, set
`minio.auth.rootPassword` in an untracked values file if a known development
credential is required.

Once the operator has reconciled the C3 deployment, verify indexing, SmartStore
bucket rolls, and distributed search with:

```console
make noah-local-smoke
```

The local operator cannot reach the SearchHeadCluster management endpoints
without additional ingress, so the smoke test does not wait for CR phase
`Ready`. Run it once the C3 pods are running.

## Why the /etc/hosts entry

The `NoahCluster` endpoint is an in-cluster address,
`http://noah.splunk-operator.svc:8443`. Indexer pods resolve it through cluster
DNS; mapping the same name to `127.0.0.1` locally means your operator resolves it
through the port-forward. One endpoint value works from both sides.

It needs `sudo`, so it is not automated — `noah-local-port-forward` prints the
command if the entry is missing. A `401` from
`curl http://noah.splunk-operator.svc:8443/` means you reached Noah.

## Before you run the operator

- Scale any in-cluster operator to 0 first. `go run` takes no leader-election
  lease, so both would reconcile the same resources.
- Use a branch build. A released operator image ignores `NoahCluster` silently.

## Overrides

`NOAH_LOCAL_*` variables, listed at the top of the section in the
[`Makefile`](../../Makefile) — namespace, port, release name, chart, fixtures,
Helm args, deploy timeout.

`NOAH_LOCAL_NAMESPACE` and `NOAH_LOCAL_PORT` do not reach
`fixtures/c3.yaml`, which is plain YAML. If you override either, edit
the endpoint to match or point `NOAH_LOCAL_FIXTURES` at your own copy. Changing
`NOAH_LOCAL_RELEASE` also changes the MinIO Service and Secret names, which are
`noah-minio` in the bundled fixture.

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
- The fixture does not override `spec.image`; all Splunk roles use the image
  configured for the operator under test.
