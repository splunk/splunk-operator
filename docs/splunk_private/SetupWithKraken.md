---
title: Set Up SOK with Kraken
parent: Internal Onboarding
nav_order: 2
---

# Set Up SOK with Kraken

## Table of Contents

- [Bootstrap a Disposable Kubernetes Environment](#bootstrap-a-disposable-kubernetes-environment)
  - [Create an Empty vCluster Kubernetes Environment](#create-an-empty-vcluster-kubernetes-environment)
- [Deploy SOK](#deploy-sok)
  - [Deploy a Release Version](#deploy-a-release-version)
  - [Deploy a Pre-Release Version](#deploy-a-pre-release-version)
  - [Verify the Splunk Operator Deployment](#verify-the-splunk-operator-deployment)
- [Tear Down SOK](#tear-down-sok)
  - [Tear Down a Release Version](#tear-down-a-release-version)
  - [Tear Down a Pre-Release Version](#tear-down-a-pre-release-version)
- [Tear Down the Kubernetes Environment](#tear-down-the-kubernetes-environment)

---

This page describes the local setup path for SOK development with Kraken. Follow the sections in order to create a disposable Kubernetes environment, deploy SOK, verify the operator, and clean everything up when you are done.

## Bootstrap a Disposable Kubernetes Environment

Kraken is the internal tool created to support the Kubernetes First push by providing fast, repeatable environments for development, testing, and CI.

### Create an Empty vCluster Kubernetes Environment

1. [Download](https://kraken.splunkdev.page/kraken-docs/cli-preview/) and [install](https://kraken.splunkdev.page/kraken-docs/cli-preview/#1-install-the-cli) the Kraken CLI.
2. Create a kraken deployment
   1. For a simple deployment, create a [vCluster-only mode](https://kraken.splunkdev.page/kraken-docs/cli-commands/create-vcluster-only/) deployment.
   2. For a deployment requiring app framework, create a vCluster-only mode with [app framework infrastructure](https://kraken.splunkdev.page/kraken-docs/cli-commands/create-app-framework/) deployment. Follow the entire page for credentials to the app framework s3 bucket.
3. Export the deployment ID for the cluster. The deployment ID is in the `id` field of the JSON output from the `kraken create` command.
   ```bash
   export DEPLOYMENT_ID=<deployment ID>
   ```
4. Follow the workflow to [access the kraken connection](https://kraken.splunkdev.page/kraken-docs/cli-commands/connection-accessing-vcluster/) in your terminal.

## Deploy SOK

### Deploy a Release Version

#### Option 1: Helm (Official Deployment Path)

1. Add the Splunk Helm repository.
   ```bash
   helm repo add splunk https://splunk.github.io/splunk-operator/
   helm repo update
   ```
2. Install the CRDs. Find the [release versions](https://github.com/splunk/splunk-operator/releases) on GitHub.
   ```bash
   kubectl apply -f https://github.com/splunk/splunk-operator/releases/download/<release version>/splunk-operator-crds.yaml --server-side --force-conflicts
   ```
3. Deploy the Splunk Operator.

   **Note:** Customers are redirected to the README to find the value for the `SPLUNK_GENERAL_TERMS` environment variable. This is so they see the link to the terms they are accepting. For developer use, the value is included here.
   ```bash
   helm install splunk-operator -n splunk-operator \
      splunk/splunk-operator \
      --version <release version> \
      --set splunkOperator.splunkGeneralTerms="--accept-sgt-current-at-splunk-com" \
      --create-namespace
   ```

#### Option 2: Manifest Files

1. Deploy the Splunk Operator. Find the [release versions](https://github.com/splunk/splunk-operator/releases) on GitHub.
   ```bash
   kubectl apply -f https://github.com/splunk/splunk-operator/releases/download/<release version>/splunk-operator-cluster.yaml --server-side --force-conflicts
   ```
2. Set `SPLUNK_GENERAL_TERMS`.

   **Note:** Customers are redirected to the README to find the value for the `SPLUNK_GENERAL_TERMS` environment variable. This is so they see the link to the terms they are accepting. For developer use, the value is included here.
   ```bash
   kubectl set env deployment/splunk-operator-controller-manager \
     -n splunk-operator \
     SPLUNK_GENERAL_TERMS="--accept-sgt-current-at-splunk-com"
   ```

### Deploy a Pre-Release Version

#### Option 1: Helm (Official Deployment Path)

1. Publish the SOK image to Artifactory. This can be done in the following ways:
    1. Open an MR in this [repo](https://cd.splunkdev.com/sok/splunk-operator) (mark it as a draft if the changes are not ready for review). The `build-stage-image` job will publish a multi-platform image to Artifactory. Search for `pushing manifest for docker-test.repo.splunkdev.net/sok/splunk-operator:` in the log output of that job to find the full Artifactory image.
    2. [Manually push](https://cloud-automation.splunkdev.page/ci-cd/artifactory/ephemeral-credentials-examples/user-guide/docker/#local-cli) a locally built image to your `docker-test.repo.splunkdev.net/user-<username>` user repository.
2. Navigate to the cloned `splunk-operator` repository on your local machine. Check out the branch that matches the built image.
3. Install the CRDs with the `make install` Makefile target.
    ```bash
    make install
    ```
4. Deploy the Splunk Operator with the pre-release image. Replace `<artifactory image>` with the full operator image from step 1, and replace `<splunk enterprise image>` with the Splunk Enterprise image under test. Find Splunk Enterprise images on [DockerHub](https://hub.docker.com/repository/docker/splunk/splunk/general), [Artifactory](https://repo.splunkdev.net/ui/repos/tree/General/docker/eng-effectiveness/docker-splunk/dev), or [Artifactory Test](https://repo.splunkdev.net/ui/repos/tree/General/docker-test/eng-effectiveness/docker-splunk/dev).

   **Note:** Customers are redirected to the README to find the value for the `SPLUNK_GENERAL_TERMS` environment variable. This is so they see the link to the terms they are accepting. For developer use, the value is included here.
   ```bash
   helm install splunk-operator -n splunk-operator \
      ./helm-chart/splunk-operator \
      --set splunkOperator.image.repository="<artifactory image>" \
      --set image.repository="<splunk enterprise image>" \
      --set splunkOperator.splunkGeneralTerms="--accept-sgt-current-at-splunk-com" \
      --create-namespace
   ```

#### Option 2: Manifest Files

1. Publish the SOK image to Artifactory. This can be done in the following ways:
    1. Open an MR in this [repo](https://cd.splunkdev.com/sok/splunk-operator) (mark it as a draft if the changes are not ready for review). The `build-stage-image` job will publish a multi-platform image to Artifactory. Search for `docker-test.repo.splunkdev.net/sok/splunk-operator:` in the log output of that job to find the full Artifactory image.
    2. [Manually push](https://cloud-automation.splunkdev.page/ci-cd/artifactory/ephemeral-credentials-examples/user-guide/docker/#local-cli) a locally built image to your `docker-test.repo.splunkdev.net/user-<username>` user repository.
2. Navigate to the cloned `splunk-operator` repository on your local machine. Check out the branch that matches the built image.
3. Use the `make deploy` Makefile target to install CRDs, deploy the operator pod, and create a debug sidecar. Replace `<artifactory image>` with the full operator image from step 1, and replace `<splunk enterprise image>` with the Splunk Enterprise image under test. Find Splunk Enterprise images on [DockerHub](https://hub.docker.com/repository/docker/splunk/splunk/general), [Artifactory](https://repo.splunkdev.net/ui/repos/tree/General/docker/eng-effectiveness/docker-splunk/dev), or [Artifactory Test](https://repo.splunkdev.net/ui/repos/tree/General/docker-test/eng-effectiveness/docker-splunk/dev).

   **Note:** Customers are redirected to the README to find the value for the `SPLUNK_GENERAL_TERMS` environment variable. This is so they see the link to the terms they are accepting. For developer use, the value is included here.
   ```bash
   make deploy IMG=<artifactory image> \
     SPLUNK_ENTERPRISE_IMAGE="<splunk enterprise image>" \
     SPLUNK_GENERAL_TERMS="--accept-sgt-current-at-splunk-com" \
     WATCH_NAMESPACE="" \
     ENVIRONMENT=debug
   ```

### Verify the Splunk Operator Deployment

```bash
kubectl get pods -n splunk-operator
```

Wait for the operator pod to be in the `Running` state. If there are errors, or if the pod takes more than one minute to come up, see the [troubleshooting documentation](./Troubleshooting.md).

## Tear Down SOK

### Tear Down a Release Version

#### Option 1: Helm (Official Deployment Path)

1. Uninstall the Splunk Operator.
   ```bash
   helm uninstall splunk-operator -n splunk-operator
   ```
2. Uninstall the CRDs. Find the [release versions](https://github.com/splunk/splunk-operator/releases) on GitHub.
   ```bash
   kubectl delete -f https://github.com/splunk/splunk-operator/releases/download/<release version>/splunk-operator-crds.yaml
   ```

#### Option 2: Manifest Files

1. Uninstall the Splunk Operator. Find the [release versions](https://github.com/splunk/splunk-operator/releases) on GitHub.
   ```bash
   kubectl delete -f https://github.com/splunk/splunk-operator/releases/download/<release version>/splunk-operator-cluster.yaml
   ```

### Tear Down a Pre-Release Version

#### Option 1: Helm (Official Deployment Path)

1. Uninstall the Splunk Operator.
   ```bash
   helm uninstall splunk-operator -n splunk-operator
   ```
2. Uninstall the CRDs with the `make uninstall` Makefile target.

   ```bash
   make uninstall
   ```

#### Option 2: Manifest Files

1. Navigate to the cloned `splunk-operator` repository.
2. Use the `make undeploy` Makefile target to uninstall CRDs, remove the operator pod, and remove the debug sidecar.

   ```bash
   make undeploy ENVIRONMENT=debug
   ```

## Tear Down the Kubernetes Environment

1. [Delete](https://kraken.splunkdev.page/kraken-docs/cli-commands/delete/) the Kraken deployment when it is no longer needed.
