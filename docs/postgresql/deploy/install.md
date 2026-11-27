---
title: Prepare the controllers
parent: Deploy PostgreSQL
nav_order: 1
---

# Prepare the PostgreSQL controllers

The Splunk Operator provides the `PostgresClusterClass`, `PostgresCluster`, and `PostgresDatabase` APIs. CloudNativePG (CNPG) manages the PostgreSQL workloads. You do not need to deploy Splunk Enterprise to enable shared PostgreSQL.

## Choose an installation path

Make sure that `kubectl` points to the intended cluster before making changes:

```bash
kubectl config current-context
kubectl cluster-info
```

Use the official Splunk Operator Helm chart with its bundled CNPG controller by default. If you already run CNPG, use the [existing CNPG path](#use-an-existing-cnpg-installation).

If the Splunk Operator already runs on your cluster, upgrade its Helm release to enable PostgreSQL. Do not install a second Splunk Operator. If both controllers are already ready and PostgreSQL is enabled, [check the installation](#check-the-installation).

If you need to install or update the controllers, first [choose where to pull the artifacts](#choose-an-artifact-source). You need `kubectl`, Helm, and permission to install custom resource definitions (CRDs). A CRD registers a resource type with Kubernetes. Make sure that the nodes can reach the image registry.

## Choose an artifact source

Use Splunk's official Helm repository unless your deployment needs a mirror. Both paths use the same [Helm installation step](#apply-the-helm-configuration).

### Use Splunk's published artifacts

Install `splunk/splunk-operator` from the official Helm repository. The published chart includes the CNPG and Barman dependencies. You do not need to install the bundled dependencies as separate releases. The chart pulls the PostgreSOK controller images from Splunk's Docker Hub namespace, `registry-1.docker.io/splunk`. It also supplies pinned PostgreSQL and PgBouncer references for the Helm-owned shared cluster.

Use the [artifact catalog](public-artifacts.md#published-release) for exact versions. Follow the [release verification steps](public-artifacts.md#select-and-verify-a-release) when you need to verify the selected artifacts. You do not need to download the backend packages separately for this path. Continue to [prepare the Helm values](#install-or-enable-postgresql-through-helm).

### Use a mirror

[Verify the selected release](public-artifacts.md#select-and-verify-a-release) before you copy artifacts. Copy the Splunk Operator chart package and the images your deployment uses. Preserve the chart package bytes, image tags, and image digests. See how to [pull an exact image](public-artifacts.md#pull-a-selected-image). If you need a backend chart outside the packaged chart, [download its package separately](public-artifacts.md#download-the-selected-chart-packages).

If the chart mirror requires authentication, run `helm registry login` on the machine that runs Helm. For a verified local `.tgz`, replace `splunk/splunk-operator` with the file path. Omit `--version` for a local file. If you install from a mirror, use the mirrored package's OCI reference and version.

A packaged Splunk Operator chart already contains its dependencies. An image override does not change where Helm gets chart packages. If you build from an unpackaged chart and need to fetch dependencies from a mirror, configure their repository URLs at build time.

For an authenticated image mirror, create registry pull Secrets before installation. A pull Secret gives Kubernetes credentials to pull an image. Use the following configuration fields:

| Images | Fields for the mirror | Fields for pull Secrets |
| --- | --- | --- |
| Splunk Operator | `splunkOperator.image.repository` takes the full image reference | `splunkOperator.imagePullSecrets` |
| Bundled CNPG controller and bootstrap image | `cloudnative-pg.image.repository` | `cloudnative-pg.imagePullSecrets` for the controller |
| Bundled Barman controller and sidecar | `plugin-barman-cloud.image` and `plugin-barman-cloud.sidecarImage` each take a `registry` and a `repository` | `plugin-barman-cloud.imagePullSecrets` for the controller |
| PostgreSQL and PgBouncer workloads | `sharedPostgres.images` for a shared cluster or `PostgresCluster.spec.images` for a separate cluster | The `imagePullSecrets` list under that same `images` field |

Store each controller pull Secret in the namespace of its Deployment. Store each workload pull Secret in the PostgreSQL cluster namespace. Make sure that the workload can pull any private CNPG bootstrap or Barman sidecar images it uses. A controller pull Secret does not grant access in another namespace. If your cluster cannot reach a registry for another chart image, such as cert-manager, configure that image separately.

Merge the relevant overrides into your Helm values file. This example changes the backend repositories without replacing the chart's pinned tags or digests:

```yaml
cloudnative-pg:
  image:
    repository: mirror.example.com/postgresql/cnpg
  imagePullSecrets:
    - name: controller-registry-reader
plugin-barman-cloud:
  image:
    registry: mirror.example.com
    repository: postgresql/barman
  sidecarImage:
    registry: mirror.example.com
    repository: postgresql/barman-sidecar
  imagePullSecrets:
    - name: controller-registry-reader
```

For a Helm-owned shared cluster, replace the sample tag and digest below with the complete values from the selected release. Configure every PostgreSQL major that you plan to use. If you enable pooling, include its PgBouncer reference.

```yaml
sharedPostgres:
  images:
    postgresql:
      "17": mirror.example.com/postgresql/postgresql:17-RELEASE@sha256:REPLACE_WITH_DIGEST
    pgbouncer: mirror.example.com/postgresql/pgbouncer:RELEASE@sha256:REPLACE_WITH_DIGEST
    imagePullSecrets:
      - name: postgres-registry-reader
```

If you reuse an existing CNPG or Barman installation, configure its mirror and credentials through the release that owns it. Do not download a separate CNPG chart for the Splunk Operator release if you reuse CNPG. Keep these overrides and continue to [prepare the Helm values](#install-or-enable-postgresql-through-helm).

## Install or enable PostgreSQL through Helm

Follow the [Splunk Operator Helm guide](../../deploy/Helm.md) to install matching SOK CRDs and select the chart version. Review the [Splunk General Terms instructions](../../README.md#splunk-general-terms-acceptance) before installing or updating the operator.

### Default path: install CNPG with the chart

Select an official chart release that includes the PostgreSQL backend dependencies. Add the `splunk` repository and install CRDs for that release as described in the Helm guide.

Set `splunkOperator.featureGates.PostgresController: true` to enable the PostgreSQL controllers in the Splunk Operator. Set `cloudnative-pg.enabled: true` to install the bundled CNPG controller. `ValidationWebhook` is a separate gate for admission checks. Keep it enabled for the procedure below.

Save this example as `postgres-operator.yaml` for a new installation without cert-manager. If cert-manager already runs on your cluster, set `cert-manager.enabled: false`. Read the [webhook certificate prerequisites](../../deploy/Helm.md#prerequisite-cert-manager) before you install.

If you will [configure object storage backups](provision.md#configure-backups-before-enabling-the-cluster) and Barman is not installed, set `plugin-barman-cloud.enabled: true`. If a compatible plugin already exists, keep the subchart off.

For a new release, leave both shared services off until the controllers and admission webhook are ready. This includes the CA bundle and the webhook serving endpoint. The Helm hook that creates the shared PostgreSQL class needs working admission.

```yaml
splunkOperator:
  featureGates:
    PostgresController: true
    ValidationWebhook: true
cert-manager:
  enabled: true
  crds:
    enabled: true
cloudnative-pg:
  enabled: true
plugin-barman-cloud:
  enabled: false
sharedPostgres:
  enabled: false
sharedKVService:
  enabled: false
```

If you do not already run CNPG, continue to [apply the Helm configuration](#apply-the-helm-configuration). If you do, make the change below before you install.

### Use an existing CNPG installation

Use CloudNativePG 1.30.1 as the minimum for this deployment path. This is the upstream version in the [published backend release manifest](../releases/postgresok-2026-11-27/release-manifest.json). A newer CNPG version is not necessarily compatible. Check the versions listed for your Splunk Operator release.

Before you install, make sure that CNPG meets the version requirement. Make sure that it watches the namespace for shared PostgreSQL. If the version is below the minimum, upgrade CNPG through its own installation method. Do not install a second CNPG controller to work around an incompatible version.

Start with the [same values file](#default-path-install-cnpg-with-the-chart). Keep `splunkOperator.featureGates.PostgresController: true` and the webhook configuration. Disable the bundled CNPG controller before you run Helm:

```yaml
cloudnative-pg:
  enabled: false
```

The existing CNPG controller continues to manage PostgreSQL workloads. The Splunk Operator chart does not adopt or upgrade that controller. Continue to [apply the Helm configuration](#apply-the-helm-configuration).

## Apply the Helm configuration

Before you run Helm, finish the values file. If you selected a mirror, merge its [image overrides and pull Secrets](#use-a-mirror) into the file.

Use the selected chart version and your own terms value from the instructions above. The following command installs a new release. If a release already exists, use its name, namespace, and values instead. If you mirrored the package, use the chart argument from [the mirror instructions](#use-a-mirror):

```bash
: "${CHART_VERSION:?Set the approved Splunk Operator chart version}"
: "${SPLUNK_GENERAL_TERMS:?Supply your own terms value after reading the terms instructions}"
export RELEASE=splunk-operator
export RELEASE_NAMESPACE=splunk-operator
helm upgrade --install "$RELEASE" splunk/splunk-operator \
  --version "$CHART_VERSION" --namespace "$RELEASE_NAMESPACE" --create-namespace \
  --values postgres-operator.yaml \
  --set-string splunkOperator.splunkGeneralTerms="$SPLUNK_GENERAL_TERMS" \
  --wait --timeout 10m
```

When you upgrade an existing release, keep its enabled shared services enabled. Do not roll back to a revision that predates their creation. After the Helm operation, [check the installation](#check-the-installation) before provisioning.

## Check the installation

Make sure that the required CRDs exist:

```bash
kubectl get crd \
  postgresclusterclasses.platform.splunk.com \
  postgresclusters.platform.splunk.com \
  postgresdatabases.platform.splunk.com \
  clusters.postgresql.cnpg.io
```

List the Deployments. Find the Splunk Operator and CNPG Deployments in the output. Set the four variables below to their names and namespaces. Bundled CNPG can share the operator namespace. A separate CNPG installation can use a different namespace.

```bash
kubectl get deployments --all-namespaces
```

```bash
: "${OPERATOR_NAMESPACE:?Set the Splunk Operator namespace}"
: "${OPERATOR_DEPLOYMENT:?Set the Splunk Operator deployment name}"
: "${CNPG_NAMESPACE:?Set the CloudNativePG namespace}"
: "${CNPG_DEPLOYMENT:?Set the CloudNativePG deployment name}"
kubectl rollout status -n "$OPERATOR_NAMESPACE" "deployment/$OPERATOR_DEPLOYMENT" --timeout=5m
kubectl rollout status -n "$CNPG_NAMESPACE" "deployment/$CNPG_DEPLOYMENT" --timeout=5m
kubectl get deployment -n "$OPERATOR_NAMESPACE" "$OPERATOR_DEPLOYMENT" \
  -o jsonpath='{.spec.template.spec.containers[?(@.name=="manager")].args}{"\n"}'
```

For the Helm path, look for `PostgresController=true` in the feature gate arguments. The CRDs can exist while the gate is disabled. If the release uses admission validation, make sure that its webhook has a CA bundle and a ready serving endpoint. Use the [webhook guide](../../deploy/Helm.md#validation-webhooks) to identify the configuration for your release.

When both controllers are ready, [size the shared cluster](cluster-policy.md#size-the-cluster-before-provisioning). Keep the release name, namespace, chart source, and values file.

## If installation fails

If `kubectl` reports `no matches for kind "PostgresCluster"`, make sure that the context and CRDs are correct. If resources exist but do not reconcile, inspect the operator deployment and its feature gates. For rollout failures, inspect the pods and events in the affected controller namespace:

```bash
kubectl get pods -n "$OPERATOR_NAMESPACE"
kubectl get events -n "$OPERATOR_NAMESPACE" --sort-by='.lastTimestamp'
```

If you see `ImagePullBackOff`, make sure that the nodes can pull the image. If you see `FailedMount` on `webhook-server-cert`, inspect the certificate. Make sure that its Secret exists. Do not print the Secret contents. If you see `FailedScheduling`, read the event for the resource or placement constraint.
