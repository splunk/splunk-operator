---
title: 6 - C3 with License Manager and Monitoring Console Deployed with Helm
parent: Guided Labs
nav_order: 6
---

# 6 - C3 with License Manager and Monitoring Console Deployed with Helm

[Clustered (C3)](https://help.splunk.com/en/splunk-enterprise/get-started/splunk-validated-architectures/splunk-platform-indexing-and-search/distributed-clustered-deployment-with-shc---single-site-c3--c13) deployments are distributed, clustered Splunk Enterprise deployments with search head clustering. This lab uses Helm to add a License Manager and Monitoring Console to the C3 topology so the cluster uses an Enterprise license and can be inspected from the Monitoring Console UI.

In this lab, you will create a temporary Kubernetes environment, install the Splunk Operator with Helm, create a license ConfigMap, deploy a C3 instance with a License Manager and Monitoring Console using a Helm values file, log in to the Monitoring Console using a browser, and tear everything down. The C3 deployment contains one License Manager, one Monitoring Console, one ClusterManager, three search head replicas, and three indexer replicas. The Splunk Operator also creates one deployer pod for the SearchHeadCluster.

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

5. Verify the Helm release is deployed.

   Run:

   ```bash
   helm list -n splunk-operator
   ```

   Expected output:

   ```text
   NAME              NAMESPACE        REVISION   UPDATED       STATUS     CHART                     APP VERSION
   splunk-operator   splunk-operator  1          <timestamp>   deployed   splunk-operator-X.Y.Z     X.Y.Z
   ```

6. Verify the Splunk Operator Controller Manager pod is up and running. It might take several minutes for the pod to be ready. You can run the command multiple times if necessary.

   Run:

   ```bash
   kubectl get pods -n splunk-operator
   ```

   Expected output:

   ```text
   NAME                                           READY   STATUS    RESTARTS   AGE
   splunk-operator-controller-manager-xxxx-yyyy   1/1     Running   0          XXs
   ```

7. Check the Splunk Operator logs are healthy.

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

The License Manager reads the Enterprise license from a Kubernetes ConfigMap. The Helm values in this lab mount the ConfigMap as `/mnt/licenses` and set `licenseManager.licenseUrl` to `/mnt/licenses/enterprise.lic`.

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

1. Copy the C3 Helm values to a file.

   Run:

   ```bash
   cat > c3-lm-mc-values.yaml <<'EOF'
   splunk-operator:
     enabled: false

   licenseManager:
     enabled: true
     name: c3-lm
     volumes:
       - name: licenses
         configMap:
           name: splunk-licenses
     licenseUrl: /mnt/licenses/enterprise.lic

   monitoringConsole:
     enabled: true
     name: c3-mc

   clusterManager:
     enabled: true
     name: c3-cm

   indexerCluster:
     enabled: true
     name: c3-idxc
     replicaCount: 3

   searchHeadCluster:
     enabled: true
     name: c3-shc
     replicaCount: 3
   EOF
   ```

   Explanation of Fields:

   - `splunk-operator.enabled: false`: Does not install another Splunk Operator because the lab already deployed one.
   - `<component>.enabled: true`: Creates the LicenseManager, MonitoringConsole, ClusterManager, IndexerCluster, or SearchHeadCluster CR for that component.
   - `<component>.name`: Sets each component's CR name: `c3-lm`, `c3-mc`, `c3-cm`, `c3-idxc`, or `c3-shc`.
   - `licenseManager.volumes`: Mounts Kubernetes volumes in the License Manager pod.
   - `licenseManager.volumes[].name: licenses`: Names the mounted license volume.
   - `licenseManager.volumes[].configMap.name: splunk-licenses`: Populates the volume from the ConfigMap created from the Enterprise license file.
   - `licenseManager.licenseUrl`: Path to the mounted Enterprise license file inside the License Manager pod.
   - `indexerCluster.replicaCount: 3`: Sets the number of indexer peers.
   - `searchHeadCluster.replicaCount: 3`: Sets the number of search head peers.

   When `licenseManager.enabled` and `monitoringConsole.enabled` are true, the `splunk/splunk-enterprise` chart automatically adds `licenseManagerRef` and `monitoringConsoleRef` to the generated C3 Custom Resources.

2. Install the Splunk Enterprise Helm release.

   Run:

   ```bash
   helm upgrade --install splunk-enterprise splunk/splunk-enterprise \
     --namespace splunk-operator \
     --version "$SOK_RELEASE_VERSION" \
     -f c3-lm-mc-values.yaml
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

   After you install this Helm release, the LicenseManager pod starts, the ClusterManager pod comes up, the SearchHeadCluster deployer starts before the search head replicas, and the indexer and search head replicas become ready after they can connect to the ClusterManager, LicenseManager, and MonitoringConsole.

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

4. Verify the Helm values are stored on the release.

   Run:

   ```bash
   helm get values splunk-enterprise -n splunk-operator
   ```

   Expected output contains:

   ```yaml
   licenseManager:
     enabled: true
     name: c3-lm
   monitoringConsole:
     enabled: true
     name: c3-mc
   ```

5. Verify the LicenseManager reaches the `Ready` phase. It may take several minutes for the resource to become ready.

   Run:

   ```bash
   kubectl get licensemanager -n splunk-operator
   ```

   Expected output:

   ```text
   NAME    PHASE   AGE     MESSAGE
   c3-lm   Ready   XmXXs
   ```

6. Verify the MonitoringConsole reaches the `Ready` phase.

   Run:

   ```bash
   kubectl get monitoringconsole -n splunk-operator
   ```

   Expected output:

   ```text
   NAME    PHASE   DESIRED   READY   AGE     MESSAGE
   c3-mc   Ready                     XmXXs
   ```

7. Verify the ClusterManager reaches the `Ready` phase.

   Run:

   ```bash
   kubectl get clustermanager -n splunk-operator
   ```

   Expected output:

   ```text
   NAME    PHASE   MANAGER   DESIRED   READY   AGE     MESSAGE
   c3-cm   Ready                              XmXXs
   ```

8. Verify the IndexerCluster reaches the `Ready` phase. It may take several minutes for all three indexer pods to become ready.

   Run:

   ```bash
   kubectl get indexercluster -n splunk-operator
   ```

   Expected output:

   ```text
   NAME      PHASE   MASTER   MANAGER   DESIRED   READY   AGE     MESSAGE
   c3-idxc   Ready            Ready     3         3       XmXXs
   ```

9. Verify the SearchHeadCluster reaches the `Ready` phase. It may take several minutes for the deployer and all three search head pods to become ready.

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
   splunk-c3-idxc-indexer-service                       ClusterIP   XX.XX.XX.XX    <none>        8000/TCP,8088/TCP,8089/TCP,9997/TCP   XXm
   splunk-c3-lm-license-manager-service                 ClusterIP   XX.XX.XX.XX   <none>        8000/TCP,8089/TCP                     XXm
   splunk-c3-mc-monitoring-console-headless             ClusterIP   None             <none>        8000/TCP,8088/TCP,8089/TCP,9997/TCP   XXm
   splunk-c3-mc-monitoring-console-service              ClusterIP   XX.XX.XX.XX   <none>        8000/TCP,8088/TCP,8089/TCP,9997/TCP   XXm
   splunk-c3-shc-deployer-service                       ClusterIP   XX.XX.XX.XX    <none>        8000/TCP,8089/TCP                     XXm
   splunk-c3-shc-search-head-headless                   ClusterIP   None             <none>        8000/TCP,8089/TCP                     XXm
   splunk-c3-shc-search-head-service                    ClusterIP   XX.XX.XX.XX    <none>        8000/TCP,8089/TCP                     XXm
   splunk-operator-controller-manager-metrics-service   ClusterIP   XX.XX.XX.XX   <none>        8443/TCP                              XXm
   splunk-operator-controller-manager-service           ClusterIP   XX.XX.XX.XX    <none>        80/TCP                                XXm
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
   - Use `helm get manifest splunk-enterprise -n splunk-operator` to see the Custom Resources rendered by Helm.

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

> **Checkpoint:** Kubernetes has created the expected resources for the Helm-managed licensed C3 deployment and Monitoring Console.

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

   - Open **Indexing > Indexer Clustering > Indexer Clustering: Status** and verify indexer peers appear

6. When you are done exploring, close the browser window, and press `Ctrl+C` to stop the port-forwarding.

> **Checkpoint:** You can now connect to the Monitoring Console UI for the Helm-managed licensed C3 deployment.

## 7. Delete the Splunk Enterprise Instance

1. Destroy the Splunk Enterprise instance by uninstalling the Splunk Enterprise Helm release.

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
      NAME                                           READY   STATUS    RESTARTS   AGE
      splunk-operator-controller-manager-xxxx-yyyy   1/1     Running   0          XXm
      ```

4. Delete any remaining Splunk Enterprise PVCs.

   StatefulSet PVCs can outlive a Helm release. The temporary vCluster will be deleted at the end of the lab, but these commands keep the namespace clean before you uninstall the operator.

   Run:

   ```bash
   kubectl delete pvc -n splunk-operator -l app.kubernetes.io/instance=splunk-c3-cm-cluster-manager --ignore-not-found
   kubectl delete pvc -n splunk-operator -l app.kubernetes.io/instance=splunk-c3-idxc-indexer --ignore-not-found
   kubectl delete pvc -n splunk-operator -l app.kubernetes.io/instance=splunk-c3-lm-license-manager --ignore-not-found
   kubectl delete pvc -n splunk-operator -l app.kubernetes.io/instance=splunk-c3-mc-monitoring-console --ignore-not-found
   kubectl delete pvc -n splunk-operator -l app.kubernetes.io/instance=splunk-c3-shc-deployer --ignore-not-found
   kubectl delete pvc -n splunk-operator -l app.kubernetes.io/instance=splunk-c3-shc-search-head --ignore-not-found
   ```

   Expected output if PVCs existed:

   ```text
   persistentvolumeclaim "pvc-etc-splunk-c3-cm-cluster-manager-0" deleted
   persistentvolumeclaim "pvc-var-splunk-c3-cm-cluster-manager-0" deleted
   persistentvolumeclaim "pvc-etc-splunk-c3-idxc-indexer-0" deleted
   persistentvolumeclaim "pvc-etc-splunk-c3-idxc-indexer-1" deleted
   persistentvolumeclaim "pvc-etc-splunk-c3-idxc-indexer-2" deleted
   persistentvolumeclaim "pvc-var-splunk-c3-idxc-indexer-0" deleted
   persistentvolumeclaim "pvc-var-splunk-c3-idxc-indexer-1" deleted
   persistentvolumeclaim "pvc-var-splunk-c3-idxc-indexer-2" deleted
   persistentvolumeclaim "pvc-etc-splunk-c3-lm-license-manager-0" deleted
   persistentvolumeclaim "pvc-var-splunk-c3-lm-license-manager-0" deleted
   persistentvolumeclaim "pvc-etc-splunk-c3-mc-monitoring-console-0" deleted
   persistentvolumeclaim "pvc-var-splunk-c3-mc-monitoring-console-0" deleted
   persistentvolumeclaim "pvc-etc-splunk-c3-shc-deployer-0" deleted
   persistentvolumeclaim "pvc-var-splunk-c3-shc-deployer-0" deleted
   persistentvolumeclaim "pvc-etc-splunk-c3-shc-search-head-0" deleted
   persistentvolumeclaim "pvc-etc-splunk-c3-shc-search-head-1" deleted
   persistentvolumeclaim "pvc-etc-splunk-c3-shc-search-head-2" deleted
   persistentvolumeclaim "pvc-var-splunk-c3-shc-search-head-0" deleted
   persistentvolumeclaim "pvc-var-splunk-c3-shc-search-head-1" deleted
   persistentvolumeclaim "pvc-var-splunk-c3-shc-search-head-2" deleted
   ```

5. Verify the Persistent Volume Claims for the Splunk Enterprise Pods are deleted. The Splunk Operator PVC should still exist.

   Run:

   ```bash
   kubectl get pvc -n splunk-operator
   ```

   Expected output:

   ```text
   NAME                           STATUS   VOLUME                            CAPACITY   ACCESS MODES   STORAGECLASS   VOLUMEATTRIBUTESCLASS   AGE
   splunk-operator-app-download   Bound    pvc-vvvv-wwww-xxxx-yyyy-zzzz      10Gi       RWO                           <unset>                 XXm
   ```

6. Delete the license ConfigMap.

   Run:

   ```bash
   kubectl delete configmap splunk-licenses -n splunk-operator
   ```

   Expected output:

   ```text
   configmap "splunk-licenses" deleted
   ```

## 8. Delete the SOK Release

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

## 9. Delete the Kubernetes Environment

1. [Delete](https://kraken.splunkdev.page/kraken-docs/reference/cli/delete/) the Kraken deployment.
