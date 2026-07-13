---
title: 3 - Clustered Deployment Deployed with Manifest Files
parent: Guided Labs
nav_order: 3
---

# 3 - Clustered Deployment Deployed with Manifest Files

[Clustered (C3)](https://help.splunk.com/en/splunk-enterprise/get-started/splunk-validated-architectures/splunk-platform-indexing-and-search/distributed-clustered-deployment-with-shc---single-site-c3--c13) deployments are distributed, clustered Splunk Enterprise deployments with search head clustering. They are useful for learning how the Splunk Operator manages multiple related Custom Resources as one Splunk topology.

In this lab, you will create a temporary Kubernetes environment, install the Splunk Operator, create a C3 deployment with manifest files, verify the resulting StatefulSets, Pods, and PVCs, then log in to Splunk Web. The C3 deployment contains one ClusterManager, three search head replicas, and three indexer replicas. The Splunk Operator also creates one deployer pod for the SearchHeadCluster.

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

## 2. Deploy the Latest SOK Release with Manifest Files

[Manifest files](https://kubernetes.io/docs/concepts/overview/working-with-objects/) are YAML files. Tools such as kubectl convert the information from a manifest into JSON or another supported serialization format when making the API request over HTTP. They are good for local development and POC work.

1. Find the latest [release version](https://github.com/splunk/splunk-operator/releases) on GitHub. Set the SOK_RELEASE_VERSION environment variable.

   Run:

   ```bash
   export SOK_RELEASE_VERSION=<release version>
   ```

2. Deploy the release manifest file.

   Run:

   ```bash
   kubectl apply -f https://github.com/splunk/splunk-operator/releases/download/$SOK_RELEASE_VERSION/splunk-operator-cluster.yaml --server-side
   ```

3. Set `SPLUNK_GENERAL_TERMS`.

   **Note:** Customers are redirected to the README to find the value for the `SPLUNK_GENERAL_TERMS` environment variable. This is so they see the link to the terms they are accepting. For developer use, the value is included here.

   Run:

   ```bash
   kubectl set env deployment/splunk-operator-controller-manager \
     -n splunk-operator \
     SPLUNK_GENERAL_TERMS="--accept-sgt-current-at-splunk-com"
   ```

   Expected output:

   ```text
   deployment.apps/splunk-operator-controller-manager env updated
   ```

4. Verify the Splunk Operator Controller Manager pod is up and running. It might take several minutes for the pod to be ready. You can run the command multiple times if necessary.

   Run:

   ```bash
   kubectl get pods -n splunk-operator
   ```

   Expected output:

   ```text
   NAME                                           READY   STATUS    RESTARTS   AGE
   splunk-operator-controller-manager-xxxx-yyyy   1/1     Running   0          XXs
   ```

5. Check the Splunk Operator logs are healthy.

   Run:

   ```bash
   kubectl logs deployment/splunk-operator-controller-manager -n splunk-operator | grep -F "Starting Controller"
   ```

   Expected output contains logs verifying the controllers are starting for each Custom Resource:

   ```text
   INFO	Starting Controller	{"controller": "indexer-cluster-controller", "controllerGroup": "enterprise.splunk.com", "controllerKind": "IndexerCluster"}
   ```

> **Checkpoint:** You now have a running Splunk Operator pod! It is ready to facilitate the deployment of a Splunk Enterprise instance.

## 3. Deploy the C3 Splunk Enterprise Instance with a Manifest File

1. Copy the C3 Custom Resource (CR) manifests to a single file.

   Run:

   ```bash
   cat > c3.yaml <<'EOF'
   apiVersion: enterprise.splunk.com/v4
   kind: ClusterManager
   metadata:
     name: c3-cm
     namespace: splunk-operator
     finalizers:
       - enterprise.splunk.com/delete-pvc
   ---
   apiVersion: enterprise.splunk.com/v4
   kind: IndexerCluster
   metadata:
     name: c3-idxc
     namespace: splunk-operator
     finalizers:
       - enterprise.splunk.com/delete-pvc
   spec:
     clusterManagerRef:
       name: c3-cm
     replicas: 3
   ---
   apiVersion: enterprise.splunk.com/v4
   kind: SearchHeadCluster
   metadata:
     name: c3-shc
     namespace: splunk-operator
     finalizers:
       - enterprise.splunk.com/delete-pvc
   spec:
     clusterManagerRef:
       name: c3-cm
     replicas: 3
   EOF
   ```

   Explanation of Fields:

   - `apiVersion: enterprise.splunk.com/v4`: Splunk Enterprise CR API version used by all three resources.
   - `kind`: Creates a `ClusterManager`, `IndexerCluster`, or `SearchHeadCluster` CR.
   - `metadata.name`: Unique name of each CR: `c3-cm`, `c3-idxc`, or `c3-shc`.
   - `metadata.namespace: splunk-operator`: Kubernetes namespace in which to create each CR.
   - `metadata.finalizers`: Actions that must complete before Kubernetes deletes each CR.
   - `spec.clusterManagerRef.name: c3-cm`: Connects the indexer and search head clusters to the ClusterManager CR.
   - `spec.replicas: 3`: Creates three indexer peers or three search head peers for the applicable CR.
   - `---`: Separates multiple Kubernetes resources in one YAML file.

2. Create the C3 Splunk Enterprise instance by applying the YAML.

   Run:

   ```bash
   kubectl apply -f c3.yaml
   ```

   Expected output:

   ```text
   clustermanager.enterprise.splunk.com/c3-cm created
   indexercluster.enterprise.splunk.com/c3-idxc created
   searchheadcluster.enterprise.splunk.com/c3-shc created
   ```

   After you apply this manifest, the ClusterManager pod comes up first, the SearchHeadCluster deployer starts before the search head replicas, and the indexer and search head replicas become ready after they can connect to the ClusterManager.

3. Verify the ClusterManager reaches the `Ready` phase. It may take several minutes for the resource to become ready.

   Run:

   ```bash
   kubectl get clustermanager -n splunk-operator
   ```

   Expected output:

   ```text
   NAME   PHASE   MANAGER   DESIRED   READY   AGE     MESSAGE
   c3-cm  Ready                              XmXXs
   ```

4. Verify the IndexerCluster reaches the `Ready` phase. It may take several minutes for all three indexer pods to become ready.

   Run:

   ```bash
   kubectl get indexercluster -n splunk-operator
   ```

   Expected output:

   ```text
   NAME      PHASE   MASTER   MANAGER   DESIRED   READY   AGE     MESSAGE
   c3-idxc   Ready            Ready     3         3       XmXXs
   ```

5. Verify the SearchHeadCluster reaches the `Ready` phase. It may take several minutes for the deployer and all three search head pods to become ready.

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
   splunk-c3-cm-cluster-manager    1/1     XmXXs
   splunk-c3-idxc-indexer          3/3     XmXXs
   splunk-c3-shc-deployer          1/1     XmXXs
   splunk-c3-shc-search-head       3/3     XmXXs
   ```

2. Verify the C3 Pods are created.

   Run:

   ```bash
   kubectl get pods -n splunk-operator
   ```

   Expected output:

   ```text
   NAME                                                  READY   STATUS    RESTARTS   AGE
   splunk-c3-cm-cluster-manager-0                        1/1     Running   0          XXm
   splunk-c3-idxc-indexer-0                              1/1     Running   0          XXm
   splunk-c3-idxc-indexer-1                              1/1     Running   0          XXm
   splunk-c3-idxc-indexer-2                              1/1     Running   0          XXm
   splunk-c3-shc-deployer-0                              1/1     Running   0          XXm
   splunk-c3-shc-search-head-0                           1/1     Running   0          XXm
   splunk-c3-shc-search-head-1                           1/1     Running   0          XXm
   splunk-c3-shc-search-head-2                           1/1     Running   0          XXm
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

1. Destroy the C3 Splunk Enterprise instance by deleting the same YAML file.

   Run:

   ```bash
   kubectl delete -f c3.yaml
   ```

   Expected output:

   ```text
   clustermanager.enterprise.splunk.com "c3-cm" deleted
   indexercluster.enterprise.splunk.com "c3-idxc" deleted
   searchheadcluster.enterprise.splunk.com "c3-shc" deleted
   ```

2. Verify all of the Kubernetes resources are destroyed. It may take several minutes for the finalizers to remove the StatefulSets, Pods, and PVCs.

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
      NAME                                                  READY   STATUS    RESTARTS   AGE
      splunk-operator-controller-manager-xxxx-yyyy          1/1     Running   0          XXm
      ```

   4. Verify the Persistent Volume Claims for the C3 Pods are deleted. The Splunk Operator PVC should still exist.

      Run:

      ```bash
      kubectl get pvc -n splunk-operator
      ```

      Expected output:

      ```text
      NAME                            STATUS   VOLUME                            CAPACITY   ACCESS MODES   STORAGECLASS   VOLUMEATTRIBUTESCLASS   AGE
      splunk-operator-app-download    Bound    pvc-vvvv-wwww-xxxx-yyyy-zzzz      10Gi       RWO                           <unset>                 XXm
      ```

## 7. Delete the SOK Release

1. Destroy the Splunk Operator instance by deleting the YAML.

   Run:

   ```bash
   kubectl delete -f https://github.com/splunk/splunk-operator/releases/download/$SOK_RELEASE_VERSION/splunk-operator-cluster.yaml
   ```

2. Verify all of the Kubernetes resources are destroyed.

   1. Verify the Splunk Operator Pod is deleted.

      Run:

      ```bash
      kubectl get pods -n splunk-operator
      ```

      Expected output:

      ```text
      No resources found in splunk-operator namespace.
      ```

   2. Verify the Persistent Volume Claims for the Splunk Operator Pod are deleted.

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
