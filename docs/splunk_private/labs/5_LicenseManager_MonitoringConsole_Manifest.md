---
title: 5 - C3 with License Manager and Monitoring Console Deployed with Manifest Files
parent: Guided Labs
nav_order: 5
---

# 5 - C3 with License Manager and Monitoring Console Deployed with Manifest Files

[Clustered (C3)](https://help.splunk.com/en/splunk-enterprise/get-started/splunk-validated-architectures/splunk-platform-indexing-and-search/distributed-clustered-deployment-with-shc---single-site-c3--c13) deployments are distributed, clustered Splunk Enterprise deployments with search head clustering. This lab adds a License Manager and Monitoring Console to the C3 topology so the cluster uses an Enterprise license and can be inspected from the Monitoring Console UI.

In this lab, you will create a temporary Kubernetes environment, install the Splunk Operator with manifest files, create a license ConfigMap, deploy a C3 instance with a License Manager and Monitoring Console, log in to the Monitoring Console using a browser, and tear everything down. The C3 deployment contains one License Manager, one Monitoring Console, one ClusterManager, three search head replicas, and three indexer replicas. The Splunk Operator also creates one deployer pod for the SearchHeadCluster.

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
   INFO	Starting Controller	{"controller": "license-manager-controller", "controllerGroup": "enterprise.splunk.com", "controllerKind": "LicenseManager"}
   INFO	Starting Controller	{"controller": "monitoring-console-controller", "controllerGroup": "enterprise.splunk.com", "controllerKind": "MonitoringConsole"}
   ```

> **Checkpoint:** You now have a running Splunk Operator pod! It is ready to facilitate the deployment of Splunk Enterprise instances.

## 3. Create the License ConfigMap

The License Manager reads the Enterprise license from a Kubernetes ConfigMap. The manifest in this lab expects the license to be mounted as `/mnt/licenses/enterprise.lic`.

1. Follow the [instructions](../BuildAndTest.md#obtaining-a-splunk-enterprise-license) to download the Splunk Enterprise license file, and export the path to your local file.

   Run:

   ```bash
   export SPLUNK_LICENSE_FILE=/absolute/path/to/enterprise.lic
   ```

2. Create the license ConfigMap in the `splunk-operator` namespace.

   Run:

   ```bash
   kubectl create configmap splunk-licenses \
     -n splunk-operator \
     --from-file=enterprise.lic="$SPLUNK_LICENSE_FILE"
   ```

   Expected output:

   ```text
   configmap/splunk-licenses created
   ```

3. Verify the ConfigMap exists and contains the `enterprise.lic` key.

   Run:

   ```bash
   kubectl get configmap splunk-licenses -n splunk-operator -o jsonpath='{.data.enterprise\.lic}' | head -c 20
   ```

   Expected output contains the first characters of the license file:

   ```text
   <?xml version="1.0"
   ```

> **Checkpoint:** The license file is available to the License Manager pod.

## 4. Deploy the C3 Splunk Enterprise Instance with License Manager and Monitoring Console

1. Copy the License Manager, Monitoring Console, and C3 Custom Resource (CR) manifests to a single file.

   Run:

   ```bash
   cat > c3-lm-mc.yaml <<'EOF'
   apiVersion: enterprise.splunk.com/v4
   kind: LicenseManager
   metadata:
     name: c3-lm
     namespace: splunk-operator
     finalizers:
       - enterprise.splunk.com/delete-pvc
   spec:
     volumes:
       - name: licenses
         configMap:
           name: splunk-licenses
     licenseUrl: /mnt/licenses/enterprise.lic
     monitoringConsoleRef:
       name: c3-mc
   ---
   apiVersion: enterprise.splunk.com/v4
   kind: MonitoringConsole
   metadata:
     name: c3-mc
     namespace: splunk-operator
     finalizers:
       - enterprise.splunk.com/delete-pvc
   spec:
     licenseManagerRef:
       name: c3-lm
   ---
   apiVersion: enterprise.splunk.com/v4
   kind: ClusterManager
   metadata:
     name: c3-cm
     namespace: splunk-operator
     finalizers:
       - enterprise.splunk.com/delete-pvc
   spec:
     licenseManagerRef:
       name: c3-lm
     monitoringConsoleRef:
       name: c3-mc
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
     licenseManagerRef:
       name: c3-lm
     monitoringConsoleRef:
       name: c3-mc
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
     licenseManagerRef:
       name: c3-lm
     monitoringConsoleRef:
       name: c3-mc
     replicas: 3
   EOF
   ```

   Explanation of Fields:

   - `apiVersion: enterprise.splunk.com/v4`: Splunk Enterprise CR API version used by all five resources.
   - `kind`: Creates a `LicenseManager`, `MonitoringConsole`, `ClusterManager`, `IndexerCluster`, or `SearchHeadCluster` CR.
   - `metadata.name`: Unique name of each CR.
   - `metadata.namespace: splunk-operator`: Kubernetes namespace in which to create each CR.
   - `metadata.finalizers`: Actions that must complete before Kubernetes deletes each CR.
   - `LicenseManager.spec.volumes`: Mounts Kubernetes volumes in the License Manager pod.
   - `LicenseManager.spec.volumes[].name: licenses`: Names the mounted license volume.
   - `LicenseManager.spec.volumes[].configMap.name: splunk-licenses`: Populates the volume from the ConfigMap created from the Enterprise license file.
   - `LicenseManager.spec.licenseUrl`: Path to the mounted Enterprise license file inside the License Manager pod.
   - `spec.clusterManagerRef.name: c3-cm`: Connects the indexer and search head clusters to the ClusterManager CR.
   - `spec.licenseManagerRef.name: c3-lm`: Connects each applicable CR to the LicenseManager CR.
   - `spec.monitoringConsoleRef.name: c3-mc`: Connects each applicable CR to the MonitoringConsole CR.
   - `spec.replicas: 3`: Creates three indexer peers or three search head peers for the applicable CR.
   - `---`: Separates multiple Kubernetes resources in one YAML file.

2. Create the Splunk Enterprise deployment by applying the YAML.

   Run:

   ```bash
   kubectl apply -f c3-lm-mc.yaml
   ```

   Expected output:

   ```text
   licensemanager.enterprise.splunk.com/c3-lm created
   monitoringconsole.enterprise.splunk.com/c3-mc created
   clustermanager.enterprise.splunk.com/c3-cm created
   indexercluster.enterprise.splunk.com/c3-idxc created
   searchheadcluster.enterprise.splunk.com/c3-shc created
   ```

   After you apply this manifest, the LicenseManager pod starts, the ClusterManager pod comes up, the SearchHeadCluster deployer starts before the search head replicas, and the indexer and search head replicas become ready after they can connect to the ClusterManager, LicenseManager, and MonitoringConsole. It will take a few minutes for all of the resources to become Ready.

3. Verify the LicenseManager reaches the `Ready` phase. It may take several minutes for the resource to become ready.

   Run:

   ```bash
   kubectl get licensemanager -n splunk-operator
   ```

   Expected output:

   ```text
   NAME    PHASE   AGE     MESSAGE
   c3-lm   Ready   XmXXs
   ```

4. Verify the MonitoringConsole reaches the `Ready` phase.

   Run:

   ```bash
   kubectl get monitoringconsole -n splunk-operator
   ```

   Expected output:

   ```text
   NAME    PHASE   DESIRED   READY   AGE     MESSAGE
   c3-mc   Ready                     XmXXs
   ```

5. Verify the ClusterManager reaches the `Ready` phase.

   Run:

   ```bash
   kubectl get clustermanager -n splunk-operator
   ```

   Expected output:

   ```text
   NAME    PHASE   MANAGER   DESIRED   READY   AGE     MESSAGE
   c3-cm   Ready                              XmXXs
   ```

6. Verify the IndexerCluster reaches the `Ready` phase. It may take several minutes for all three indexer pods to become ready.

   Run:

   ```bash
   kubectl get indexercluster -n splunk-operator
   ```

   Expected output:

   ```text
   NAME      PHASE   MASTER   MANAGER   DESIRED   READY   AGE     MESSAGE
   c3-idxc   Ready            Ready     3         3       XmXXs
   ```

7. Verify the SearchHeadCluster reaches the `Ready` phase. It may take several minutes for the deployer and all three search head pods to become ready.

   Run:

   ```bash
   kubectl get searchheadcluster -n splunk-operator
   ```

   Expected output:

   ```text
   NAME     PHASE   DEPLOYER   DESIRED   READY   AGE     MESSAGE
   c3-shc   Ready   Ready      3         3       XmXXs
   ```

> **Checkpoint:** You now have the C3 Custom Resources running with a License Manager and Monitoring Console!

## 5. Verify the Kubernetes Resources

1. Verify the StatefulSets are created.

   Run:

   ```bash
   kubectl get statefulsets -n splunk-operator
   ```

   Expected output:

   ```text
   NAME                                  READY   AGE
   splunk-c3-cm-cluster-manager          1/1     XmXXs
   splunk-c3-idxc-indexer                3/3     XmXXs
   splunk-c3-lm-license-manager          1/1     XmXXs
   splunk-c3-mc-monitoring-console       1/1     XmXXs
   splunk-c3-shc-deployer                1/1     XmXXs
   splunk-c3-shc-search-head             3/3     XmXXs
   ```

2. Verify the Pods are created.

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
   splunk-c3-lm-license-manager-0                        1/1     Running   0          XXm
   splunk-c3-mc-monitoring-console-0                     1/1     Running   0          XXm
   splunk-c3-shc-deployer-0                              1/1     Running   0          XXm
   splunk-c3-shc-search-head-0                           1/1     Running   0          XXm
   splunk-c3-shc-search-head-1                           1/1     Running   0          XXm
   splunk-c3-shc-search-head-2                           1/1     Running   0          XXm
   splunk-operator-controller-manager-xxxx-yyyy          1/1     Running   0          XXm
   ```

   If there are restarts, that is okay, as long as each pod shows `READY: 1/1` and `STATUS: Running`.

3. Verify the services are created.

   Run:

   ```bash
   kubectl get svc -n splunk-operator
   ```

   Expected output contains:

   ```text
   NAME                                      TYPE        CLUSTER-IP     EXTERNAL-IP   PORT(S)                              AGE
   splunk-c3-cm-cluster-manager-service                 ClusterIP   XX.XX.XX.XX    <none>        8000/TCP,8089/TCP                     XXm
   splunk-c3-idxc-indexer-headless                      ClusterIP   None             <none>        8000/TCP,8088/TCP,8089/TCP,9997/TCP   XXm
   splunk-c3-idxc-indexer-service                       ClusterIP   XX.XX.XX.XX     <none>        8000/TCP,8088/TCP,8089/TCP,9997/TCP   XXm
   splunk-c3-lm-license-manager-service                 ClusterIP   XX.XX.XX.XX   <none>        8000/TCP,8089/TCP                     XXm
   splunk-c3-mc-monitoring-console-headless             ClusterIP   None             <none>        8000/TCP,8088/TCP,8089/TCP,9997/TCP   XXm
   splunk-c3-mc-monitoring-console-service              ClusterIP   XX.XX.XX.XX   <none>        8000/TCP,8088/TCP,8089/TCP,9997/TCP   XXm
   splunk-c3-shc-deployer-service                       ClusterIP   XX.XX.XX.XX    <none>        8000/TCP,8089/TCP                     XXm
   splunk-c3-shc-search-head-headless                   ClusterIP   None             <none>        8000/TCP,8089/TCP                     XXm
   splunk-c3-shc-search-head-service                    ClusterIP   XX.XX.XX.XX    <none>        8000/TCP,8089/TCP                     XXm
   splunk-operator-controller-manager-metrics-service   ClusterIP   XX.XX.XX.XX    <none>        8443/TCP                              XXm
   splunk-operator-controller-manager-service           ClusterIP   XX.XX.XX.XX   <none>        8080/TCP,8081/TCP                     XXm
   ```

4. Verify the Persistent Volume Claims are created.

   Run:

   ```bash
   kubectl get pvc -n splunk-operator
   ```

   Expected output contains PVCs for the license manager, monitoring console, cluster manager, indexer peers, deployer, search heads, and the Splunk Operator app download volume.

   Tips:

   - Replace `get` with `describe` for detailed output.
   - Append `-o json` or `-o yaml` for output in JSON or YAML format.
   - Use `kubectl logs <pod_name>` to view logs for any pod. Use the `-f` option to follow logs.

5. Verify an indexer is using the License Manager.

   Run:

   ```bash
   kubectl exec -n splunk-operator splunk-c3-idxc-indexer-0 -- /bin/sh -c \
     'curl -ks -u admin:$(cat /mnt/splunk-secrets/password) https://localhost:8089/services/licenser/localslave?output_mode=json' \
     | jq -r '.entry[0].content | {master_uri, license_keys}'
   ```

   Expected output contains the License Manager service and one or more license keys:

   ```json
   {
     "master_uri": "https://splunk-c3-lm-license-manager-service:8089",
     "license_keys": [
       "..."
     ]
   }
   ```

   This confirms the Splunk instance is using the License Manager created in this lab.

6. Verify the Monitoring Console configuration ConfigMap contains C3 service references.

   Run:

   ```bash
   kubectl get configmap splunk-c3-mc-monitoring-console \
     -n splunk-operator \
     -o yaml | grep -E "SPLUNK_CLUSTER_MASTER_URL|SPLUNK_DEPLOYER_URL|SPLUNK_SEARCH_HEAD_URL|SPLUNK_LICENSE_MASTER_URL"
   ```

   Expected output contains service references similar to:

   ```text
   SPLUNK_CLUSTER_MASTER_URL
   SPLUNK_DEPLOYER_URL
   SPLUNK_LICENSE_MASTER_URL
   SPLUNK_SEARCH_HEAD_URL
   ```

> **Checkpoint:** Kubernetes has created the expected resources for the licensed C3 deployment and Monitoring Console.

## 6. Log In to the Monitoring Console

1. Retrieve the admin password from the created secret.

   Run:

   ```bash
   kubectl get secret splunk-splunk-operator-secret -n splunk-operator -o jsonpath='{.data.password}' | base64 --decode
   ```

2. Use a simple network port forward to open port 8000 for Monitoring Console access.

   Run:

   ```bash
   kubectl port-forward svc/splunk-c3-mc-monitoring-console-service -n splunk-operator 8000:8000
   ```

   Expected output:

   ```text
   Forwarding from 127.0.0.1:8000 -> 8000
   Forwarding from [::1]:8000 -> 8000
   ```

3. Open `http://127.0.0.1:8000/en-US/app/splunk_monitoring_console` in your machine's browser.

4. Login with username: `admin`, and the password output from Step 6.1.

5. Use the Monitoring Console UI to inspect the C3 deployment.

   Suggested checks:

   - Open **Indexing > Indexer Clustering > Indexer Clustering: Status** and verify indexer peers appear.

6. When you are done exploring, close the browser window, and press `Ctrl+C` to stop the port-forwarding.

> **Checkpoint:** You can now connect to the Monitoring Console UI for the licensed C3 deployment.

## 7. Delete the Splunk Enterprise Instance

1. Destroy the Splunk Enterprise instance by deleting the same YAML file.

   Run:

   ```bash
   kubectl delete -f c3-lm-mc.yaml
   ```

   Expected output:

   ```text
   licensemanager.enterprise.splunk.com "c3-lm" deleted
   monitoringconsole.enterprise.splunk.com "c3-mc" deleted
   clustermanager.enterprise.splunk.com "c3-cm" deleted
   indexercluster.enterprise.splunk.com "c3-idxc" deleted
   searchheadcluster.enterprise.splunk.com "c3-shc" deleted
   ```

2. Verify all of the Kubernetes resources are destroyed. It may take several minutes for the finalizers to remove the StatefulSets, Pods, and PVCs.

   1. Verify the Custom Resources are deleted.

      Run:

      ```bash
      kubectl get licensemanager -n splunk-operator
      kubectl get monitoringconsole -n splunk-operator
      kubectl get clustermanager -n splunk-operator
      kubectl get indexercluster -n splunk-operator
      kubectl get searchheadcluster -n splunk-operator
      ```

      Expected output:

      ```text
      No resources found in splunk-operator namespace.
      No resources found in splunk-operator namespace.
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

   3. Verify the Splunk Enterprise Pods are deleted. The Splunk Operator Pod should still exist.

      Run:

      ```bash
      kubectl get pods -n splunk-operator
      ```

      Expected output:

      ```text
      NAME                                                  READY   STATUS    RESTARTS   AGE
      splunk-operator-controller-manager-xxxx-yyyy          1/1     Running   0          XXm
      ```

   4. Verify the Persistent Volume Claims for the Splunk Enterprise Pods are deleted. The Splunk Operator PVC should still exist.

      Run:

      ```bash
      kubectl get pvc -n splunk-operator
      ```

      Expected output:

      ```text
      NAME                            STATUS   VOLUME                            CAPACITY   ACCESS MODES   STORAGECLASS   VOLUMEATTRIBUTESCLASS   AGE
      splunk-operator-app-download    Bound    pvc-vvvv-wwww-xxxx-yyyy-zzzz      10Gi       RWO                           <unset>                 XXm
      ```

3. Delete the license ConfigMap.

   Run:

   ```bash
   kubectl delete configmap splunk-licenses -n splunk-operator
   ```

   Expected output:

   ```text
   configmap "splunk-licenses" deleted
   ```

## 8. Delete the SOK Release

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

## 9. Delete the Kubernetes Environment

1. [Delete](https://kraken.splunkdev.page/kraken-docs/reference/cli/delete/) the Kraken deployment.
