# Noah local development

Spins up a Kraken vCluster running Noah (plus PostgreSQL and Redis), installs the
operator CRDs, and creates a sample `IndexerCluster`. You run the operator itself
locally against that cluster.

Needs `jq`, `yq`, `kubectl`, `helm`, `openssl` and `kraken` on `PATH`. For Helm,
either use your own install or:

```console
make setup/helm HELM_VERSION=3.18.4 CI_BIN_DIR=$(pwd)/bin
```

## Setup

```console
make noah-local-up
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
in this order. `make help` lists them under **Noah Local Development**.

| target                         |                                                                                                                          |
| ------------------------------ | ------------------------------------------------------------------------------------------------------------------------ |
| `noah-local-cluster`           | create the vCluster, write the `kraken` kubeconfig context, copy Kraken's Artifactory pull secret into `splunk-operator` |
| `install`                      | install all CRDs from `config/crd/bases`                                                                                 |
| `noah-local-deploy`            | `helm upgrade --install` of [`helm/charts/noah`](../../helm/charts/noah), waits for ready                                |
| `noah-local-fixtures`          | generate the auth Secret, apply [`fixtures/indexercluster.yaml`](fixtures/indexercluster.yaml)                           |
| `noah-local-port-forward`      | forward the Noah service to localhost                                                                                    |
| `noah-local-destroy`           | terminate the vCluster                                                                                                   |
| `noah-local-stop-port-forward` | stop the forward                                                                                                         |
| `noah-local-deployment-id`     | print the saved deployment ID                                                                                            |
| `noah-local-lint`              | lint the chart and scripts                                                                                               |

State lives in `.noah-local-dev/`. Re-running `noah-local-cluster` reuses the
saved deployment, or creates a new one if it has gone.

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
`fixtures/indexercluster.yaml`, which is plain YAML. If you override either, edit
the endpoint to match or point `NOAH_LOCAL_FIXTURES` at your own copy.

## Fixture notes

- The Secret key is `pass4SymmKey`, exactly that casing
  (`pkg/splunk/enterprise/noah_indexer.go`). Wrong casing still bootstraps pods
  but fails calls to Noah.
- The `pass4SymmKey` is generated at runtime and never committed. An existing
  Secret is left alone.
- `spec.image` pins a Splunk image from a personal Artifactory path — the only
  non-official image here. It carries splunk-ansible changes needed for Noah;
  drop it once CSPL-5172 lands.
