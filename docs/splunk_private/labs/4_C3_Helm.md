---
title: 4 - Clustered Deployment Deployed with Helm
parent: Guided Labs
nav_order: 4
---

# 4 - Clustered Deployment Deployed with Helm

[Clustered (C3)](https://help.splunk.com/en/splunk-enterprise/get-started/splunk-validated-architectures/splunk-platform-indexing-and-search/distributed-clustered-deployment-with-shc---single-site-c3--c13) deployments are distributed, clustered Splunk Enterprise deployments with search head clustering. They are useful for learning how the Splunk Operator manages multiple related Custom Resources as one Splunk topology.

In this lab, you will create a temporary Kubernetes environment, install the Splunk Operator with Helm, create a C3 Splunk Enterprise deployment with a Helm values file, verify the resulting StatefulSets, Pods, and PVCs, then log in to Splunk Web. The C3 deployment contains one ClusterManager, three search head replicas, and three indexer replicas. The Splunk Operator also creates one deployer pod for the SearchHeadCluster.

## Before You Start

### Prerequisite Knowledge

This lab expects baseline knowledge of Kubernetes and SOK before you start. If
you need that context, review [What Is SOK?](../WhatIsSOK.md), especially the
baseline Kubernetes knowledge and SOK overview sections.

You should be comfortable with:

- Kubernetes objects and workflows, including Pods, Services, StatefulSets,
  PersistentVolumeClaims, Secrets, namespaces, and basic `kubectl` inspection
  commands.
- The SOK operator model, including Splunk Custom Resources, reconciliation,
  and how SOK turns declared Splunk Enterprise topology into Kubernetes
  resources.

### Prerequisite Tools

- Kraken CLI - [Download and install](https://kraken.splunkdev.page/kraken-docs/get-started/quick-start/#download-the-cli)
- kubectl - [Install](https://kubernetes.io/docs/tasks/tools/)
- jq - [Install](https://jqlang.org/download/)
- Helm - [Install](https://helm.sh/docs/intro/install/)

## 1. Create an Empty vCluster Kubernetes Environment

1. Create a [vCluster-only mode](https://kraken.splunkdev.page/kraken-docs/workflows/vcluster-only/) deployment.
2. Export the deployment ID for the cluster. The deployment ID is in the `id` field of the JSON output from the `kraken create` command.

   Run:

   ```bash
   export DEPLOYMENT_ID=<deployment ID>
   ```

3. Wait until the kraken environment is ready.

   Run:

   ```bash
   kraken status "$DEPLOYMENT_ID" | jq -r '.state'
   ```

   Expected output:

   ```text
   ready
   ```

4. Follow the workflow to [access the kraken connection](https://kraken.splunkdev.page/kraken-docs/access/vcluster/) in your terminal.

> **Checkpoint:** You now have a Kubernetes environment ready to use!

## 2. Deploy the Latest SOK Release with Helm

[Helm](https://helm.sh/) uses charts and values files to install Kubernetes resources in a repeatable way. The `splunk/splunk-operator` chart installs the Splunk Operator. The `splunk/splunk-enterprise` chart installs Splunk Enterprise custom resources. It is the recommended installation path for production systems.

1. Add the Splunk Helm chart repository.

   Run:

   ```bash
   helm repo add splunk https://splunk.github.io/splunk-operator/
   helm repo update
   ```

2. Find the latest [release version](https://github.com/splunk/splunk-operator/releases) on GitHub. Set the SOK_RELEASE_VERSION environment variable.

   Run:

   ```bash
   export SOK_RELEASE_VERSION=<release version>
   ```

3. Install the Splunk Operator CRDs.

   Helm does not install or upgrade SOK CRDs because the CRDs are too large for the chart. Install the CRDs from the matching SOK release before installing the Helm chart.

   Run:

   ```bash
   kubectl apply -f https://github.com/splunk/splunk-operator/releases/download/$SOK_RELEASE_VERSION/splunk-operator-crds.yaml --server-side
   ```

4. Install the Splunk Operator Helm release.

   **Note:** Customers are redirected to the README to find the value for the `SPLUNK_GENERAL_TERMS` environment variable. This is so they see the link to the terms they are accepting. For developer use, the value is included here.

   Run:

   ```bash
   helm upgrade --install splunk-operator splunk/splunk-operator \
     --namespace splunk-operator \
     --create-namespace \
     --version "$SOK_RELEASE_VERSION" \
     --set splunkOperator.splunkGeneralTerms="--accept-sgt-current-at-splunk-com"
   ```

   Expected output:

   ```text
   Release "splunk-operator" does not exist. Installing it now.
   NAME: splunk-operator
   LAST DEPLOYED: <timestamp>
   NAMESPACE: splunk-operator
   STATUS: deployed
   REVISION: 1
   TEST SUITE: None
   ```

6. Verify the Helm release is deployed.

   Run:

   ```bash
   helm list -n splunk-operator
   ```

   Expected output:

   ```text
   NAME              NAMESPACE        REVISION   UPDATED       STATUS     CHART                     APP VERSION
   splunk-operator   splunk-operator  1          <timestamp>   deployed   splunk-operator-X.Y.Z     X.Y.Z
   ```

7. Verify the Splunk Operator Controller Manager pod is up and running. It might take several minutes for the pod to be ready. You can run the command multiple times if necessary.

   Run:

   ```bash
   kubectl get pods -n splunk-operator
   ```

   Expected output:

   ```text
   NAME                                           READY   STATUS    RESTARTS   AGE
   splunk-operator-controller-manager-xxxx-yyyy   1/1     Running   0          XXs
   ```

8. Check the Splunk Operator logs are healthy.

   Run:

   ```bash
   kubectl logs deployment/splunk-operator-controller-manager -n splunk-operator | grep -F "Starting Controller"
   ```

   Expected output contains logs verifying the controllers are starting for each Custom Resource:

   ```text
   INFO	Starting Controller	{"controller": "indexer-cluster-controller", "controllerGroup": "enterprise.splunk.com", "controllerKind": "IndexerCluster"}
   ```

> **Checkpoint:** You now have a running Splunk Operator pod! It is ready to facilitate the deployment of a Splunk Enterprise instance.

## 3. Deploy the C3 Splunk Enterprise Instance with Helm

1. Copy the C3 Helm values to a file.

   Run:

   ```bash
   cat > c3-values.yaml <<'EOF'
   splunk-operator:
     enabled: false

   indexerCluster:
     replicaCount: 3

   searchHeadCluster:
     replicaCount: 3

   sva:
     c3:
       enabled: true
       indexerClusters:
         - name: c3-idxc
       searchHeadClusters:
         - name: c3-shc
   EOF
   ```

2. Install the Splunk Enterprise Helm release.

   Run:

   ```bash
   helm upgrade --install splunk-enterprise splunk/splunk-enterprise \
     --namespace splunk-operator \
     --version "$SOK_RELEASE_VERSION" \
     -f c3-values.yaml
   ```

   Expected output:

   ```text
   Release "splunk-enterprise" does not exist. Installing it now.
   NAME: splunk-enterprise
   LAST DEPLOYED: <timestamp>
   NAMESPACE: splunk-operator
   STATUS: deployed
   REVISION: 1
   TEST SUITE: None
   ```

   After you install this Helm release, the ClusterManager pod comes up first, the SearchHeadCluster deployer starts before the search head replicas, and the indexer and search head replicas become ready after they can connect to the ClusterManager.

3. Verify the Helm release is deployed.

   Run:

   ```bash
   helm list -n splunk-operator
   ```

   Expected output:

   ```text
   NAME                NAMESPACE        REVISION   UPDATED       STATUS     CHART                       APP VERSION
   splunk-enterprise   splunk-operator  1          <timestamp>   deployed   splunk-enterprise-X.Y.Z     X.Y.Z
   splunk-operator     splunk-operator  1          <timestamp>   deployed   splunk-operator-X.Y.Z       X.Y.Z
   ```

4. Verify the ClusterManager reaches the `Ready` phase. It may take several minutes for the resource to become ready.

   Run:

   ```bash
   kubectl get clustermanager -n splunk-operator
   ```

   Expected output:

   ```text
   NAME    PHASE   MANAGER   DESIRED   READY   AGE     MESSAGE
   cm      Ready                               XmXXs
   ```

5. Verify the IndexerCluster reaches the `Ready` phase. It may take several minutes for all three indexer pods to become ready.

   Run:

   ```bash
   kubectl get indexercluster -n splunk-operator
   ```

   Expected output:

   ```text
   NAME      PHASE   MASTER   MANAGER   DESIRED   READY   AGE     MESSAGE
   c3-idxc   Ready            Ready     3         3       XmXXs
   ```

6. Verify the SearchHeadCluster reaches the `Ready` phase. It may take several minutes for the deployer and all three search head pods to become ready.

   Run:

   ```bash
   kubectl get searchheadcluster -n splunk-operator
   ```

   Expected output:

   ```text
   NAME     PHASE   DEPLOYER   DESIRED   READY   AGE     MESSAGE
   c3-shc   Ready   Ready      3         3       XmXXs
   ```

> **Checkpoint:** You now have the C3 Custom Resources running for one cluster manager, three indexers, one deployer, and three search heads!

## 4. Verify the C3 Kubernetes Resources

1. Verify the C3 StatefulSets are created.

   Run:

   ```bash
   kubectl get statefulsets -n splunk-operator
   ```

   Expected output:

   ```text
   NAME                            READY   AGE
   splunk-c3-idxc-indexer          3/3     XmXXs
   splunk-c3-shc-deployer          1/1     XmXXs
   splunk-c3-shc-search-head       3/3     XmXXs
   splunk-cm-cluster-manager       1/1     XmXXs
   ```

2. Verify the C3 Pods are created.

   Run:

   ```bash
   kubectl get pods -n splunk-operator
   ```

   Expected output:

   ```text
   NAME                                                  READY   STATUS    RESTARTS   AGE
   splunk-c3-idxc-indexer-0                              1/1     Running   0          XXm
   splunk-c3-idxc-indexer-1                              1/1     Running   0          XXm
   splunk-c3-idxc-indexer-2                              1/1     Running   0          XXm
   splunk-c3-shc-deployer-0                              1/1     Running   0          XXm
   splunk-c3-shc-search-head-0                           1/1     Running   0          XXm
   splunk-c3-shc-search-head-1                           1/1     Running   0          XXm
   splunk-c3-shc-search-head-2                           1/1     Running   0          XXm
   splunk-cm-cluster-manager-0                           1/1     Running   0          XXm
   splunk-operator-controller-manager-xxxx-yyyy          1/1     Running   0          XXm
   ```

   If there are restarts, that is okay, as long as each pod shows `READY: 1/1` and `STATUS: Running`.

3. Verify the C3 Persistent Volume Claims are created.

   Run:

   ```bash
   kubectl get pvc -n splunk-operator
   ```

   Expected output contains PVCs for the cluster manager, indexer peers, deployer, search heads, and the Splunk Operator app download volume.

   Tips:

   - Replace `get` with `describe` for detailed output.
   - Append `-o json` or `-o yaml` for output in JSON or YAML format.
   - Use `kubectl logs <pod_name>` to view logs for any pod. Use the `-f` option to follow logs.
   - Use `helm get values splunk-enterprise -n splunk-operator` to see the values Helm stored for the release.

4. Verify the splunkd logs are running on one of the search heads.

   Run:

   ```bash
   kubectl exec -it splunk-c3-shc-search-head-0 -n splunk-operator -- tail -n 100 /opt/splunk/var/log/splunk/splunkd.log
   ```

> **Checkpoint:** You now have a running C3 deployment with one cluster manager, three indexers, one deployer, and three search heads!

## 5. Login to Splunk

1. Retrieve the admin password from the created secret.

   Run:

   ```bash
   kubectl get secret splunk-splunk-operator-secret -n splunk-operator -o jsonpath='{.data.password}' | base64 --decode
   ```

2. Use a simple network port forward to open port 8000 for Splunk Web access.

   Run:

   ```bash
   kubectl port-forward svc/splunk-c3-shc-search-head-service -n splunk-operator 8000
   ```

   Expected output:

   ```text
   Forwarding from 127.0.0.1:8000 -> 8000
   Forwarding from [::1]:8000 -> 8000
   ```

3. Open `127.0.0.1:8000` in your machine's browser. Login with username: `admin`, and the password output from Step 5.1.
   1. When you are done exploring, close the browser window, and press `Ctrl+C` to stop the port-forwarding.

> **Checkpoint:** You can now connect to the running C3 deployment and run SPL queries from a search head!

## 6. Delete the Splunk Enterprise Instance

1. Destroy the C3 Splunk Enterprise instance by uninstalling the Splunk Enterprise Helm release.

   Run:

   ```bash
   helm uninstall splunk-enterprise -n splunk-operator
   ```

   Expected output:

   ```text
   release "splunk-enterprise" uninstalled
   ```

2. Verify the Splunk Enterprise Helm release is removed. The Splunk Operator Helm release should still exist.

   Run:

   ```bash
   helm list -n splunk-operator
   ```

   Expected output:

   ```text
   NAME              NAMESPACE        REVISION   UPDATED       STATUS     CHART                     APP VERSION
   splunk-operator   splunk-operator  1          <timestamp>   deployed   splunk-operator-X.Y.Z     X.Y.Z
   ```

3. Verify all of the Kubernetes resources are destroyed. It may take several minutes for the StatefulSets and Pods to be removed.

   1. Verify the Custom Resources are deleted.

      Run:

      ```bash
      kubectl get clustermanager -n splunk-operator
      kubectl get indexercluster -n splunk-operator
      kubectl get searchheadcluster -n splunk-operator
      ```

      Expected output:

      ```text
      No resources found in splunk-operator namespace.
      No resources found in splunk-operator namespace.
      No resources found in splunk-operator namespace.
      ```

   2. Verify the StatefulSets are deleted.

      Run:

      ```bash
      kubectl get statefulsets -n splunk-operator
      ```

      Expected output:

      ```text
      No resources found in splunk-operator namespace.
      ```

   3. Verify the C3 Pods are deleted. The Splunk Operator Pod should still exist.

      Run:

      ```bash
      kubectl get pods -n splunk-operator
      ```

      Expected output:

      ```text
      NAME                                           READY   STATUS    RESTARTS   AGE
      splunk-operator-controller-manager-xxxx-yyyy   1/1     Running   0          XXm
      ```

4. Delete any remaining C3 PVCs.

   StatefulSet PVCs can outlive a Helm release. The temporary vCluster will be deleted at the end of the lab, but these commands keep the namespace clean before you uninstall the operator.

   Run:

   ```bash
   kubectl delete pvc -n splunk-operator -l app.kubernetes.io/instance=splunk-cm-cluster-manager --ignore-not-found
   kubectl delete pvc -n splunk-operator -l app.kubernetes.io/instance=splunk-c3-idxc-indexer --ignore-not-found
   kubectl delete pvc -n splunk-operator -l app.kubernetes.io/instance=splunk-c3-shc-deployer --ignore-not-found
   kubectl delete pvc -n splunk-operator -l app.kubernetes.io/instance=splunk-c3-shc-search-head --ignore-not-found
   ```

   Expected output if PVCs existed:

   ```text
   persistentvolumeclaim "pvc-etc-splunk-cm-cluster-manager-0" deleted
   persistentvolumeclaim "pvc-var-splunk-cm-cluster-manager-0" deleted
   persistentvolumeclaim "pvc-etc-splunk-c3-idxc-indexer-0" deleted
   persistentvolumeclaim "pvc-etc-splunk-c3-idxc-indexer-1" deleted
   persistentvolumeclaim "pvc-etc-splunk-c3-idxc-indexer-2" deleted
   persistentvolumeclaim "pvc-var-splunk-c3-idxc-indexer-0" deleted
   persistentvolumeclaim "pvc-var-splunk-c3-idxc-indexer-1" deleted
   persistentvolumeclaim "pvc-var-splunk-c3-idxc-indexer-2" deleted
   persistentvolumeclaim "pvc-etc-splunk-c3-shc-deployer-0" deleted
   persistentvolumeclaim "pvc-var-splunk-c3-shc-deployer-0" deleted
   persistentvolumeclaim "pvc-etc-splunk-c3-shc-search-head-0" deleted
   persistentvolumeclaim "pvc-etc-splunk-c3-shc-search-head-1" deleted
   persistentvolumeclaim "pvc-etc-splunk-c3-shc-search-head-2" deleted
   persistentvolumeclaim "pvc-var-splunk-c3-shc-search-head-0" deleted
   persistentvolumeclaim "pvc-var-splunk-c3-shc-search-head-1" deleted
   persistentvolumeclaim "pvc-var-splunk-c3-shc-search-head-2" deleted
   ```

5. Verify the Persistent Volume Claims for the C3 Pods are deleted. The Splunk Operator PVC should still exist.

   Run:

   ```bash
   kubectl get pvc -n splunk-operator
   ```

   Expected output:

   ```text
   NAME                           STATUS   VOLUME                            CAPACITY   ACCESS MODES   STORAGECLASS   VOLUMEATTRIBUTESCLASS   AGE
   splunk-operator-app-download   Bound    pvc-vvvv-wwww-xxxx-yyyy-zzzz      10Gi       RWO                           <unset>                 XXm
   ```

## 7. Delete the SOK Release

1. Destroy the Splunk Operator instance by uninstalling the Splunk Operator Helm release.

   Run:

   ```bash
   helm uninstall splunk-operator -n splunk-operator
   ```

   Expected output:

   ```text
   release "splunk-operator" uninstalled
   ```

2. Delete the Splunk Operator CRDs.

   Run:

   ```bash
   kubectl delete -f https://github.com/splunk/splunk-operator/releases/download/$SOK_RELEASE_VERSION/splunk-operator-crds.yaml --ignore-not-found
   ```

3. Verify all Helm releases are removed.

   Run:

   ```bash
   helm list -n splunk-operator
   ```

   Expected output:

   ```text
   NAME   NAMESPACE   REVISION   UPDATED   STATUS   CHART   APP VERSION
   ```

4. Verify the Splunk Operator Pod is deleted.

   Run:

   ```bash
   kubectl get pods -n splunk-operator
   ```

   Expected output:

   ```text
   No resources found in splunk-operator namespace.
   ```

5. Verify the Splunk Operator PVC is deleted.

   Run:

   ```bash
   kubectl get pvc -n splunk-operator
   ```

   Expected output:

   ```text
   No resources found in splunk-operator namespace.
   ```

## 8. Delete the Kubernetes Environment

1. [Delete](https://kraken.splunkdev.page/kraken-docs/reference/cli/delete/) the Kraken deployment.
