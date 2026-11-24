---
title: Helm Chart
parent: Deploy & Configure
nav_order: 2
---


# Splunk Operator Helm Installation

## Splunk Operator Helm chart Repository

Add the Splunk Operator and Enterprise charts to your Helm repository.

```
helm repo add splunk https://splunk.github.io/splunk-operator/
helm repo update
```

The ```splunk``` chart repository contains the ```splunk/splunk-operator``` chart to deploy the Splunk Operator and the ```splunk/splunk-enterprise``` chart to deploy Splunk Enterprise custom resources.

Users need to deploy the latest CRDs manually. This is a [limitation](https://helm.sh/docs/chart_best_practices/custom_resource_definitions/) from helm. The ```splunk/splunk-operator``` chart no longer contains the CRDs to install on the first deployment due to the size of the CRDs. Helm has a chart size [limit of 1MB](https://helm.sh/docs/topics/advanced/#sql-storage-backend) due to internal limits in Kubernetes' underlying etcd key-value store, and the Splunk Operator for Kubernetes CRDs are too big to fit into the helm chart to deploy the operator. To install the CRDs for the first time, or to update the CRDs to the latest versions, follow one of the following steps.

```
git clone https://github.com/splunk/splunk-operator.git .
git checkout release/3.2.0
make install
```

OR

```
kubectl apply -f https://github.com/splunk/splunk-operator/releases/download/3.2.0/splunk-operator-crds.yaml --server-side
```

Helm provides a long list of commands to manage your deployment, we'll be going over a few useful ones in the sections to come. You can learn more about supported commands [here](https://helm.sh/docs/helm/helm/).

For opt-in shared PostgreSQL and KVService provisioning, required values and
readiness checks, see [KVService through Helm](../operate/KVServiceHelm.md).

## Splunk Operator deployments

Installing the ```splunk/splunk-operator``` chart deploys the Splunk Operator with cluster-wide access. View the configurable values for the chart using the following command:

```
helm show values splunk/splunk-operator
```

### Prerequisite: cert-manager

By default the chart requires [cert-manager](https://cert-manager.io/). It creates a cert-manager `Issuer` and `Certificate` for the operator's webhook serving certificate, and cert-manager injects that certificate's CA into the chart's `ValidatingWebhookConfiguration`s. Install cert-manager before the chart. If the cluster does not already run cert-manager, you can install it as a subchart with `--set cert-manager.enabled=true`; don't do this when cert-manager is already installed, because a second copy conflicts.

To supply your own serving certificate instead of using cert-manager, create the certificate Secret, set `splunkOperator.webhook.useCertManager=false` and pass the CA that signed it with `--set-file splunkOperator.webhook.caBundle=ca.crt`. The chart then creates no `Issuer` or `Certificate` and writes the CA into its `ValidatingWebhookConfiguration`s; see [Option B: Use Your Own Certificates](../reference/ValidationWebhook.md#option-b-use-your-own-certificates), including how to rotate the CA.

### Validation webhooks

The chart creates a `<fullname>-webhook-service` Service that forwards port 443 to the operator's webhook server on port 9443. It renders up to two `ValidatingWebhookConfiguration`s pointing at that Service, both with `failurePolicy: Fail`:

| `ValidatingWebhookConfiguration` | Rendered when | What it validates |
|---|---|---|
| `<fullname>-cross-version-field-protection[-<namespace>]` | The `CrossVersionFieldProtection` gate is on (the default) | Writes through an older served API version that would silently drop fields only the stored version can represent. It denies them. |
| `<fullname>-validating-webhook-configuration[-<namespace>]` | The `ValidationWebhook` gate is on (off by default) | Writes to the operator's custom resources, checked against each resource's validation rules. |

Set gates under `splunkOperator.featureGates`. The chart also honours a `FEATURE_GATES` or the deprecated `ENABLE_VALIDATION_WEBHOOK` entry in `extraEnvs`, with `splunkOperator.featureGates` taking precedence, in the same order the operator applies them. `AllAlpha` and `AllBeta` count as well: for example, `AllBeta: false` turns `CrossVersionFieldProtection` off, and removes its webhook configuration, unless that gate is also set by name. Either env var must use `value`, not `valueFrom`, because the chart reads it to decide what to render.

`<fullname>` is `splunk-operator` unless you set `splunkOperator.nameOverride`. The resources, API versions and operations each webhook covers are listed in its rules; see `helm template` output or `kubectl get validatingwebhookconfiguration <name> -o yaml`.

For namespace-mode installs (`splunkOperator.clusterWideAccess=false`), the chart appends the Helm release namespace to each webhook configuration name. Each configuration selects that namespace and points to the webhook Service for that install, so multiple namespace-mode releases can coexist and be managed independently. For cluster-wide installs, names have no namespace suffix. Cluster-wide installs with `splunkOperator.watchNamespaces` set only match the listed namespaces; otherwise they match every namespace. Kubernetes doesn't apply a `namespaceSelector` to cluster-scoped resources, so writes to any cluster-scoped resource in a webhook's rules are always sent to it, whatever the operator watches.

In namespace mode, install into the operator namespace with `--namespace`.
`namespaceOverride` must be empty or match that release namespace; a different
value is rejected because the watch and admission scopes use the release namespace.

#### Turning off cross-version field protection

Set the gate to `false`:

```
helm upgrade --set splunkOperator.featureGates.CrossVersionFieldProtection=false <RELEASE_NAME> splunk/splunk-operator -n <RELEASE_NAMESPACE>
```

On Helm this also removes the `<fullname>-cross-version-field-protection` `ValidatingWebhookConfiguration`, so writes through older API versions keep working without the check. No manual cleanup is needed. To set more than one gate, set each one under `splunkOperator.featureGates`, for example in a ```new_values.yaml``` file:

```
splunkOperator:
  featureGates:
    CrossVersionFieldProtection: false
    ValidationWebhook: true
```

Then pass the file to Helm:

```
helm upgrade -f new_values.yaml <RELEASE_NAME> splunk/splunk-operator -n <RELEASE_NAMESPACE>
```

### PostgreSQL backend controllers

CNPG and Barman dependencies are **disabled by default**. Enable them with the
alpha PostgreSQL controller gate:

```yaml
splunkOperator:
  featureGates:
    PostgresController: true
cloudnative-pg: {enabled: true}
plugin-barman-cloud: {enabled: true}
```

Pinned subcharts supply their tested image tags/digests; SOK overrides only public
repositories/registries, including CNPG's bootstrap and Barman's sidecar. See
[public artifacts](../postgresql/public-artifacts.md) for versions and verification.
Install SOK CRDs and cert-manager first when enabling the bundle; Barman requires
cert-manager even when SOK uses another webhook certificate mechanism.

PostgreSQL RBAC follows the gate in both access modes. Bundling creates no databases or
backup policy by itself. Set `sharedPostgres.enabled=true` to provision a
release-owned PostgreSQL backend. Configure backups, or explicitly set
`sharedPostgres.backup.acknowledgeNoBackups=true` only for disposable data.
Dedicated workloads remain covered by the [PostgreSQL quickstart](../postgresql/quickstart.md).
PG17/PG18/PgBouncer runtime compatibility remains subject to installation validation.

For existing compatible controllers, leave their dependencies disabled and enable
only the PostgreSQL gate. Enterprise-only installs need no backend overrides.

Avoid overlapping CNPG controllers; Helm cannot adopt another release's resources.
Restart SOK if Barman is installed later, so its ObjectStore watch registers.

For image mirrors, retain the pinned tags/digests and copy identical image bytes:

```yaml
cloudnative-pg:
  image:
    repository: mirror.example.com/postgresql/cnpg
  imagePullSecrets: [{name: controller-registry-reader}]
plugin-barman-cloud:
  image:
    registry: mirror.example.com
    repository: postgresql/barman
  sidecarImage:
    registry: mirror.example.com
    repository: postgresql/barman-sidecar
  imagePullSecrets: [{name: controller-registry-reader}]
```

Override both Barman images. Controller pull Secrets belong in the release
namespace; workload credentials use `PostgresCluster.spec.images.imagePullSecrets`
in the workload namespace. SOK's own pull Secrets are separate. Packages contain
both dependencies; image overrides do not redirect chart downloads. Private chart
mirrors require build-time dependency configuration and `helm registry login`.
Nest these values under `splunk-operator:` when using the Enterprise chart.
`make helm-dependencies` verifies archive hashes against the release manifest.
It requires jq and `sha256sum` (or `shasum`) and uses temporary repository
configuration/cache. Helm tests check public image references; the live suite also
needs yq v4.

Helm updates templated backend CRDs on upgrade and retains them on disable/uninstall
(`helm.sh/resource-policy: keep`). Removing a controller stops its management of
existing databases. Coordinate ownership before reinstalling; do not delete CRDs
to resolve conflicts. Dependency switches provide no data-retention/recovery policy.

### Configuring Splunk Operator deployments

**Note: The `SPLUNK_GENERAL_TERMS` environment variable is set to an empty string by default. This will need to be manually updated to the required value. See [SplunkGeneralTermsMigration.md](../reference/SplunkGeneralTermsMigration.md) for instructions on how to set the variable using helm commands with both the splunk/splunk-operator and splunk/splunk-enterprise charts.**

There are a couple ways you can configure your operator deployment

1. Using a ```new_values.yaml``` file to override default values (Recommended)
```
helm install -f new_values.yaml <RELEASE_NAME> splunk/splunk-operator -n <RELEASE_NAMESPACE>
```

2. Using the Helm CLI directly to set new values
```
helm install --set <KEY>=<VALUE> <RELEASE_NAME> splunk/splunk-operator -n <RELEASE_NAMESPACE>
```

If the release already exists, we can use ```helm upgrade``` to configure and upgrade the deployment using a file or the CLI directly as above.

```
helm upgrade -f new_values.yaml <RELEASE_NAME> splunk/splunk-operator -n <RELEASE_NAMESPACE>
```

Read more about configuring values [here](https://helm.sh/docs/intro/using_helm/).

In the following example, we will install and upgrade the Splunk Operator.

Specify the release name and namespace to install the Operator:

```
helm install splunk-operator-test splunk/splunk-operator -n splunk-operator
```
```
NAME: splunk-operator-test
LAST DEPLOYED: Tue Aug 23 12:47:57 2022
NAMESPACE: splunk-operator
STATUS: deployed
REVISION: 1
TEST SUITE: None
```
```
NAME                                                  READY   STATUS    RESTARTS   AGE
splunk-operator-controller-manager-545cccf79f-9xpll   2/2     Running   0          2m14s
```
The ```helm list``` command can be used to retrieve all deployed releases.

By default, the Splunk Operator has cluster-wide access. Let's upgrade the ```splunk-operator-test``` release by revoking cluster-wide access:
```
helm upgrade --set splunkOperator.clusterWideAccess=false splunk-operator-test splunk/splunk-operator -n splunk-operator
```
```
NAME: splunk-operator-test
LAST DEPLOYED: Tue Aug 23 12:53:08 2022
NAMESPACE: splunk-operator
STATUS: deployed
REVISION: 2
TEST SUITE: None
```
Finally, let's terminate the Splunk Operator by uninstalling the ```splunk-operator-test``` release:
```
helm uninstall splunk-operator-test -n splunk-operator
```
```
release "splunk-operator-test" uninstalled
```

## Splunk Enterprise deployments

The Splunk Enterprise chart allows you to install and configure Splunk Enterprise custom resources. The ```splunk/splunk-enterprise``` chart has the ```splunk/splunk-operator``` chart as a dependency by default.

If the operator is already installed then you will need to disable the dependency:
```
helm install --set splunk-operator.enabled=false <RELEASE_NAME> splunk/splunk-enterprise -n <RELEASE_NAMESPACE>
```
Installing ```splunk/splunk-enterprise``` will deploy Splunk Enterprise custom resources according to your configuration, the following ```new_values.yaml``` file specifies override configurations to deploy a Cluster Manager, an Indexer Cluster and a Search Head Cluster.

```
clusterManager:
  enabled: true
  name: cm-test

indexerCluster:
  enabled: true
  name: idxc-test

searchHeadCluster:
  enabled: true
  name: shc-test
```
The configurations above will override values in ```splunk/splunk-enterprise``` values file.  To see all configurable values contained in the ```values.yaml``` file:
```
helm show values splunk/splunk-enterprise
```

To install a Splunk Enterprise deployment according to our configurations above:
```
helm install -f new_values.yaml splunk-enterprise-test splunk/splunk-enterprise -n splunk-operator
```
```
NAME: splunk-enterprise-test
LAST DEPLOYED: Tue Aug 23 12:11:48 2022
NAMESPACE: splunk-operator
STATUS: deployed
REVISION: 1
TEST SUITE: None
```
```
splunk-cm-test-cluster-manager-0                      1/1     Running   0               11m
splunk-idxc-test-indexer-0                            1/1     Running   0               5m49s
splunk-idxc-test-indexer-1                            1/1     Running   0               5m49s
splunk-idxc-test-indexer-2                            1/1     Running   0               5m49s
splunk-operator-controller-manager-54979b7d88-9c54b   2/2     Running   0               11m
splunk-shc-test-deployer-0                            1/1     Running   0               11m
splunk-shc-test-search-head-0                         1/1     Running   0               11m
splunk-shc-test-search-head-1                         1/1     Running   0               11m
splunk-shc-test-search-head-2                         1/1     Running   0               11m
```
We can clean-up the deployed resources quickly by uninstalling the release:
```
helm uninstall splunk-enterprise-test -n splunk-operator
```
```
release "splunk-enterprise-test" uninstalled
```
```helm uninstall``` terminates all resources deployed by Helm including Persistent Volume Claims created for Splunk Enterprise resources.

Note: Helm by default does not cleanup Custom Resource Definitions and Persistent Volume Claims. Splunk Admin needs to manually clean them.

### Noah deployments

> **Experimental:** Noah support (`noahCluster.enabled`) is experimental and off by default. It is not supported for production use and may change or be removed without notice. With it off, the chart renders exactly what it rendered before.

The Splunk Enterprise chart can create a Noah topology, in which an Indexer Cluster and a Search Head Cluster coordinate through a `NoahCluster` instead of a Cluster Manager. Noah is only available with the `sva.c3` preset: the chart fails at render time if `noahCluster.enabled` is set without `sva.c3.enabled`. The other presets do not support it: `sva.s1` is a standalone deployment, and `sva.m4` requires a Cluster Manager.

With `noahCluster.enabled`, `sva.c3` selects the Noah topology. The chart renders a `NoahCluster`, and the `IndexerCluster` and `SearchHeadCluster` resources from the `indexerClusters` and `searchHeadClusters` lists, with a `noahClusterRef` and no `ClusterManager`:

```
sva:
  c3:
    enabled: true
    indexerClusters:
      - name: idxc-test
    searchHeadClusters:
      - name: shc-test

noahCluster:
  enabled: true
  endpoint: http://noah.splunk-operator.svc:8443
  tenant: tenant-1
  authSecretRef:
    name: noah-auth
```

`sva.c3` without `noahCluster.enabled` is the classic C3 with a Cluster Manager, unchanged. If a list is empty, one cluster is rendered, named after `indexerCluster.name` or `searchHeadCluster.name`. The ingestor stays a separate `ingestorCluster` block, and the operator decides whether the search head cluster gets a deployer. With Noah, the license manager and monitoring console, if you enable them, do not get a reference to a Cluster Manager.

- `endpoint`, `tenant` and `authSecretRef.name` are required when `noahCluster.enabled` is set and have no defaults. The chart does not create the authentication Secret: it must already exist in the release namespace and hold the Noah `pass4SymmKey` under the `pass4SymmKey` key.
- The `NoahCluster` is rendered in the release namespace (or `noahCluster.namespaceOverride`). It must be in the same namespace as the clusters that reference it, because `noahClusterRef` has no namespace field. The chart fails at render time if `noahCluster.namespaceOverride` differs from `indexerCluster.namespaceOverride` or `searchHeadCluster.namespaceOverride`.
- The Noah topology has no Cluster Manager. The chart fails at render time if Noah is combined with `clusterManager.enabled` or `existingClusterManager`.
- With Noah, the chart adds the `restmap` settings the indexers need (the `admin:certificates`, `streams:search` and `streams:rdin` stanzas, which authenticate instance-to-instance calls with the Noah service key) to the `IndexerCluster` `defaults`. They are merged with `indexerCluster.defaults`, and your values win. Without them, the search head's call to an indexer is rejected with a 401 and the indexers are left out of searches. Other Splunk settings that Noah needs, such as the remote store and index settings, are not set by the chart: put them in `indexerCluster.defaults`.
- `noahClusterRef` cannot be added to, removed from or changed on an existing `IndexerCluster` or `SearchHeadCluster`. Set it when you first install the release. To change it, uninstall the release and install it again. This also means a release installed as a classic C3 cannot be switched to the Noah topology by enabling `noahCluster` later.
- `cacheWarmScaleOutEnabled` and `cacheWarmScaleOutTimeoutSeconds` are optional. When they are not set, the operator defaults apply.
- With `noahCluster` disabled, the chart renders exactly what it rendered before.

For the `NoahCluster` and `noahClusterRef` fields, see [Custom Resources](../operate/CustomResources.md).

### Troubleshooting Splunk Enterprise Deployments

#### CRDs are not installed
If you attempt to install a Splunk Enterprise deployment, and there is an error that says:
```
Error: INSTALLATION FAILED: unable to build kubernetes objects from release manifest: resource mapping not found for name: "release-name" namespace: "release-namespace" from "": no matches for kind "Standalone" in version "enterprise.splunk.com/v4"
ensure CRDs are installed first
```

Verify that the CRDs have been installed with the instructions at the [top of this documentation](#splunk-operator-helm-chart-repository).

## Splunk Validated Architecture deployments

The Splunk Enterprise chart has support for three Splunk Validated Architectures:

- [Single Server Deployment (S1)](https://www.splunk.com/pdfs/technical-briefs/splunk-validated-architectures.pdf#page=9)
- [Distributed Clustered Deployment + SHC - Single Site (C3)](https://www.splunk.com/pdfs/technical-briefs/splunk-validated-architectures.pdf#page=14)
- [Distributed Clustered Deployment + SHC - Multi-Site (M4)](https://www.splunk.com/pdfs/technical-briefs/splunk-validated-architectures.pdf#page=20)

Install a Standalone deployment using the following command:
```
helm install --set s1.enabled=true <RELEASE_NAME> splunk/splunk-enterprise -n <RELEASE_NAMESPACE>
```
Visit the Splunk Operator github repository to learn more about the configurable values of [splunk/splunk-operator](https://github.com/splunk/splunk-operator/blob/develop/helm-chart/splunk-operator/values.yaml) and [splunk/splunk-enterprise](https://github.com/splunk/splunk-operator/blob/develop/helm-chart/splunk-enterprise/values.yaml).
