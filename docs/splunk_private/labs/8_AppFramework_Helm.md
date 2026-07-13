---
title: 8 - App Framework Deployed with Helm
parent: Guided Labs
nav_order: 8
---

# 8 - Standalone and Clustered Deployment with Apps Deployed with Helm

The Splunk Operator App Framework installs and updates Splunk apps from remote object storage. You define app source locations in Helm values, and the Splunk Enterprise Helm chart renders Custom Resources that tell the operator where to download app packages and how to install them.

In this lab, you will create a temporary Kubernetes environment, install the Splunk Operator with Helm, connect to Kraken App Framework storage, create a Standalone custom resource with an app, then create a C3 deployment that installs apps on indexer peers through the ClusterManager and on search head peers through the SearchHeadCluster.

## Before You Start

You need:

- Kraken CLI - [Download and install](https://kraken.splunkdev.page/kraken-docs/get-started/quick-start/#download-the-cli)
- kubectl - [Install](https://kubernetes.io/docs/tasks/tools/)
- jq - [Install](https://jqlang.org/download/)
- AWS CLI - [Install](https://docs.aws.amazon.com/cli/latest/userguide/getting-started-install.html)
- Helm - [Install](https://helm.sh/docs/intro/install/)

## 1. Create an Empty vCluster Kubernetes Environment

1. Create a [vCluster-only mode with app framework](https://kraken.splunkdev.page/kraken-docs/features/app-framework/) deployment.
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

4. Follow the [Kraken docs](https://kraken.splunkdev.page/kraken-docs/features/app-framework/#vcluster-only-and-manual-sok-installs) to create a `/tmp/sok-operator-irsa-values.yaml` file to patch the Splunk Operator ServiceAccount with the expected identity and role.

5. Install the Splunk Operator Helm release.

   **Note:** Customers are redirected to the README to find the value for the `SPLUNK_GENERAL_TERMS` environment variable. This is so they see the link to the terms they are accepting. For developer use, the value is included here.

   Run:

   ```bash
   helm upgrade --install splunk-operator splunk/splunk-operator \
     --namespace splunk-operator \
     --create-namespace \
     --version "$SOK_RELEASE_VERSION" \
     -f /tmp/sok-operator-irsa-values.yaml \
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

7. Verify the Splunk Operator Controller Manager pod is up and running. It might take up to 1 minute for the pod to be ready. You can run the command multiple times if necessary.

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

9. Verify the Splunk Operator ServiceAccount has the correct IRSA annotation.

   Run:

   ```bash
   kubectl get serviceaccount splunk-operator-controller-manager \
     -n splunk-operator \
     -o jsonpath='{.metadata.annotations.eks\.amazonaws\.com/role-arn}{"\n"}'
   kubectl exec -n splunk-operator \
     deployment/splunk-operator-controller-manager -- \
     printenv AWS_ROLE_ARN
   ```

   Expected output:

   ```text
   arn:aws:iam::610437687531:role/kraken-splunk-runtime
   arn:aws:iam::610437687531:role/kraken-splunk-runtime
   ```

> **Checkpoint:** You now have a running Splunk Operator pod! It is ready to facilitate the deployment of a Splunk Enterprise instance with apps.

## 3. Deploy a Standalone Splunk Enterprise Pod with Apps with Helm

1. Save the kraken connection information.

   Run:

   ```bash
   kraken connection "$DEPLOYMENT_ID" > /tmp/kraken-af-connection.json
   ```

2. Follow the [instructions](https://kraken.splunkdev.page/kraken-docs/features/app-framework/#use-temporary-app-framework-credentials) to connect to the Kraken s3 buckets. Verify you can list the contents of each.

   Run:

   ```bash
   aws s3 ls "s3://$FIXTURE/" --region "$AWS_REGION"
   aws s3 ls "s3://$WORKSPACE/" --region "$AWS_REGION"
   ```

   Expected output contains a list of the contents:

   ```text
                             PRE appframework/
                             ...
                             PRE test_licenses/
   2026-05-27 16:28:24       1453 enterprise.lic
   2026-05-27 16:28:25   13137005 microsoft-azure-add-on-for-splunk.tgz
   2026-05-27 16:28:25    5084109 palo-alto-networks-add-on-for-splunk.tgz
   2026-05-27 16:28:25     257913 splunk-common-information-model-cim.tgz
   2026-05-27 16:28:26    2763353 splunk-es-content-update.tgz
   ```

2. Copy an app to your Kraken workspace s3 bucket.

   Run:

   ```bash
   aws s3 cp s3://$FIXTURE/appframework/v1apps/add-on-for-ldap.tgz s3://$WORKSPACE/s1-apps/add-on-for-ldap.tgz
   ```

   Expected output:

   ```text
   copy: s3://splk-test-data-bucket-copy-kraken/appframework/v1apps/add-on-for-ldap.tgz to s3://kraken-af-<region>-<workspace folder>/s1-apps/add-on-for-ldap.tgz
   ```

3. Copy the Standalone Helm values to a file.

   Run:

   ```bash
   cat > s1-apps-values.yaml <<EOF
   splunk-operator:
     enabled: false

   standalone:
     enabled: true
     name: s1-apps
     replicaCount: 1
     appRepo:
       appsRepoPollIntervalSeconds: 60
       defaults:
         volumeName: volume_app_repo
         scope: local
       appSources:
         - name: apps
           location: s1-apps/
       volumes:
         - name: volume_app_repo
           storageType: s3
           provider: aws
           path: ${WORKSPACE}/
           endpoint: https://s3-us-west-2.amazonaws.com
           region: $AWS_REGION
   EOF
   ```

   Explanation of Fields:

   - `splunk-operator.enabled: false`: Does not install another Splunk Operator because the lab already deployed one.
   - `standalone.enabled: true`: Creates a Standalone CR through the Splunk Enterprise chart.
   - `standalone.name: s1-apps`: Sets the name of the Standalone CR.
   - `standalone.replicaCount: 1`: Creates one Standalone pod.
   - `standalone.appRepo`: App Framework configuration for locating and installing apps from remote storage.
   - `appsRepoPollIntervalSeconds: 60`: Number of seconds between checks for new or changed apps in remote storage.
   - `defaults.volumeName: volume_app_repo`: Remote volume that each app source uses by default; it must match a name in `volumes`.
   - `defaults.scope: local`: Installs apps locally on the Standalone pod.
   - `appSources[].name: apps`: Unique logical name for this app source within `appRepo`.
   - `appSources[].location: s1-apps/`: App directory relative to the remote volume's `path`.
   - `volumes[].name: volume_app_repo`: Unique name used to reference the remote volume from `volumeName`.
   - `volumes[].storageType: s3`: Uses S3-compatible remote object storage.
   - `volumes[].provider: aws`: Uses AWS as the remote storage provider.
   - `volumes[].path: ${WORKSPACE}/`: S3 bucket and optional prefix that form the base path for app sources.
   - `volumes[].endpoint`: URL of the S3 service endpoint.
   - `volumes[].region: $AWS_REGION`: AWS region containing the S3 bucket.

4. Install the Standalone Splunk Enterprise Helm release.

   Run:

   ```bash
   helm upgrade --install s1-apps splunk/splunk-enterprise \
     --namespace splunk-operator \
     --version "$SOK_RELEASE_VERSION" \
     -f s1-apps-values.yaml
   ```

   Expected output:

   ```text
   Release "s1-apps" does not exist. Installing it now.
   NAME: s1-apps
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
   NAME              NAMESPACE        REVISION   UPDATED       STATUS     CHART                       APP VERSION
   s1-apps           splunk-operator  1          <timestamp>   deployed   splunk-enterprise-X.Y.Z     X.Y.Z
   splunk-operator   splunk-operator  1          <timestamp>   deployed   splunk-operator-X.Y.Z       X.Y.Z
   ```

6. Verify all of the Kubernetes resources are deployed. It may take several minutes for the resource to become ready.

   1. Verify the Standalone Custom Resource is created.

      Run:

      ```bash
      kubectl get standalone -n splunk-operator
      ```

      Expected output:

      ```text
      NAME      PHASE   DESIRED   READY   AGE     MESSAGE
      s1-apps   Ready   1         1       XmXXs
      ```

   2. Verify the Standalone StatefulSet is created.

      Run:

      ```bash
      kubectl get statefulsets -n splunk-operator
      ```

      Expected output:

      ```text
      NAME                         READY   AGE
      splunk-s1-apps-standalone    1/1     XmXXs
      ```

   3. Verify the Standalone Pod is created.

      Run:

      ```bash
      kubectl get pods -n splunk-operator
      ```

      Expected output:

      ```text
      NAME                                           READY   STATUS    RESTARTS   AGE
      splunk-operator-controller-manager-xxxx-yyyy   1/1     Running   0          XXm
      splunk-s1-apps-standalone-0                    1/1     Running   0          XmXXs
      ```

   Tips:

   - Replace `get` with `describe` for detailed output.
   - Append `-o json` or `-o yaml` for output in JSON or YAML format.
   - Use `kubectl logs <pod_name>` to view logs for any pod. Use the `-f` option to follow logs.
   - Use `helm get values s1-apps -n splunk-operator` to see the values Helm stored for the release.

7. Verify the splunkd logs are running.

   Run:

   ```bash
   kubectl exec -it splunk-s1-apps-standalone-0 -n splunk-operator -- tail -n 100 /opt/splunk/var/log/splunk/splunkd.log
   ```

8. Verify the app is installed.

   Run:

   ```bash
   kubectl exec splunk-s1-apps-standalone-0 -n splunk-operator -- ls /opt/splunk/etc/apps | grep -Fx TA-LDAP
   ```

   Expected output:

   ```text
   TA-LDAP
   ```

> **Checkpoint:** You now have a running Splunk Standalone instance with an app installed!

## 4. Deploy a Clustered Splunk Enterprise Deployment with Apps with Helm

1. Copy apps for the indexer cluster and search head cluster to your Kraken workspace s3 bucket.

   Run:

   ```bash
   aws s3 cp \
     "s3://$FIXTURE/appframework/v1apps/palo-alto-networks-add-on-for-splunk.tgz" \
     "s3://$WORKSPACE/idxc-apps/palo-alto-networks-add-on-for-splunk.tgz" \
     --region "$AWS_REGION"

   aws s3 cp \
     "s3://$FIXTURE/appframework/v1apps/splunk-common-information-model-cim.tgz" \
     "s3://$WORKSPACE/shc-apps/splunk-common-information-model-cim.tgz" \
     --region "$AWS_REGION"
   ```

   Expected output:

   ```text
   copy: s3://splk-test-data-bucket-copy-kraken/appframework/v1apps/palo-alto-networks-add-on-for-splunk.tgz to s3://kraken-af-<region>-<workspace folder>/idxc-apps/palo-alto-networks-add-on-for-splunk.tgz
   copy: s3://splk-test-data-bucket-copy-kraken/appframework/v1apps/splunk-common-information-model-cim.tgz to s3://kraken-af-<region>-<workspace folder>/shc-apps/splunk-common-information-model-cim.tgz
   ```

2. Copy the C3 Helm values to a file.

   Run:

   ```bash
   cat > c3-apps-values.yaml <<EOF
   splunk-operator:
     enabled: false

   clusterManager:
     enabled: true
     name: c3-apps-cm
     appRepo:
       appsRepoPollIntervalSeconds: 60
       defaults:
         volumeName: volume_app_repo
         scope: cluster
       appSources:
         - name: indexerApps
           location: idxc-apps/
       volumes:
         - name: volume_app_repo
           storageType: s3
           provider: aws
           path: ${WORKSPACE}/
           endpoint: https://s3-us-west-2.amazonaws.com
           region: $AWS_REGION

   indexerCluster:
     enabled: true
     name: c3-apps-idxc
     replicaCount: 3

   searchHeadCluster:
     enabled: true
     name: c3-apps-shc
     replicaCount: 3
     appRepo:
       appsRepoPollIntervalSeconds: 60
       defaults:
         volumeName: volume_app_repo
         scope: cluster
       appSources:
         - name: searchHeadApps
           location: shc-apps/
       volumes:
         - name: volume_app_repo
           storageType: s3
           provider: aws
           path: ${WORKSPACE}/
           endpoint: https://s3-us-west-2.amazonaws.com
           region: $AWS_REGION
   EOF
   ```

   Explanation of Fields:

   - `splunk-operator.enabled: false`: Does not install another Splunk Operator because the lab already deployed one.
   - `<component>.enabled: true`: Creates the ClusterManager, IndexerCluster, or SearchHeadCluster CR for that component.
   - `<component>.name`: Sets each component's CR name: `c3-apps-cm`, `c3-apps-idxc`, or `c3-apps-shc`.
   - `indexerCluster.replicaCount: 3`: Sets the number of indexer peers.
   - `searchHeadCluster.replicaCount: 3`: Sets the number of search head peers.
   - `<component>.appRepo`: App Framework configuration on the ClusterManager and SearchHeadCluster CRs.
   - `appsRepoPollIntervalSeconds: 60`: Number of seconds between checks for new or changed apps in remote storage.
   - `defaults.volumeName: volume_app_repo`: Remote volume that each app source uses by default; it must match a name in `volumes`.
   - `defaults.scope: cluster`: Distributes apps from the management component to the cluster's peers.
   - `appSources[].name`: Unique logical name for an app source within its `appRepo`.
   - `appSources[].location`: App directory relative to the remote volume's `path`: `idxc-apps/` or `shc-apps/`.
   - `volumes[].name: volume_app_repo`: Unique name used to reference the remote volume from `volumeName`.
   - `volumes[].storageType: s3`: Uses S3-compatible remote object storage.
   - `volumes[].provider: aws`: Uses AWS as the remote storage provider.
   - `volumes[].path: ${WORKSPACE}/`: S3 bucket and optional prefix that form the base path for app sources.
   - `volumes[].endpoint`: URL of the S3 service endpoint.
   - `volumes[].region: $AWS_REGION`: AWS region containing the S3 bucket.

3. Install the C3 Splunk Enterprise Helm release.

   Run:

   ```bash
   helm upgrade --install c3-apps splunk/splunk-enterprise \
     --namespace splunk-operator \
     --version "$SOK_RELEASE_VERSION" \
     -f c3-apps-values.yaml
   ```

   Expected output:

   ```text
   Release "c3-apps" does not exist. Installing it now.
   NAME: c3-apps
   LAST DEPLOYED: <timestamp>
   NAMESPACE: splunk-operator
   STATUS: deployed
   REVISION: 1
   TEST SUITE: None
   ```

   After you install this Helm release, the cluster manager pod comes up first, the search head cluster deployer starts before the search head replicas, and the indexer and search head replicas become ready after they can connect to the cluster manager.

4. Verify the Helm releases are deployed.

   Run:

   ```bash
   helm list -n splunk-operator
   ```

   Expected output:

   ```text
   NAME              NAMESPACE        REVISION   UPDATED       STATUS     CHART                       APP VERSION
   c3-apps           splunk-operator  1          <timestamp>   deployed   splunk-enterprise-X.Y.Z     X.Y.Z
   s1-apps           splunk-operator  1          <timestamp>   deployed   splunk-enterprise-X.Y.Z     X.Y.Z
   splunk-operator   splunk-operator  1          <timestamp>   deployed   splunk-operator-X.Y.Z       X.Y.Z
   ```

5. Verify the ClusterManager reaches the `Ready` phase. It may take several minutes for the resource to become ready.

   Run:

   ```bash
   kubectl get clustermanager -n splunk-operator
   ```

   Expected output:

   ```text
   NAME         PHASE   MANAGER   DESIRED   READY   AGE     MESSAGE
   c3-apps-cm   Ready                               XmXXs
   ```

6. Verify the IndexerCluster reaches the `Ready` phase. It may take several minutes for all three indexer pods to become ready.

   Run:

   ```bash
   kubectl get indexercluster -n splunk-operator
   ```

   Expected output:

   ```text
   NAME           PHASE   MASTER   MANAGER   DESIRED   READY   AGE     MESSAGE
   c3-apps-idxc   Ready            Ready     3         3       XmXXs
   ```

7. Verify the SearchHeadCluster reaches the `Ready` phase. It may take several minutes for the deployer and all three search head pods to become ready.

   Run:

   ```bash
   kubectl get searchheadcluster -n splunk-operator
   ```

   Expected output:

   ```text
   NAME          PHASE   DEPLOYER   DESIRED   READY   AGE     MESSAGE
   c3-apps-shc   Ready   Ready      3         3       XmXXs
   ```

8. Verify the C3 StatefulSets are created.

   Run:

   ```bash
   kubectl get statefulsets -n splunk-operator
   ```

   Expected output contains the C3 StatefulSets and the Standalone StatefulSet:

   ```text
   NAME                                  READY   AGE
   splunk-c3-apps-cm-cluster-manager     1/1     XmXXs
   splunk-c3-apps-idxc-indexer           3/3     XmXXs
   splunk-c3-apps-shc-deployer           1/1     XmXXs
   splunk-c3-apps-shc-search-head        3/3     XmXXs
   splunk-s1-apps-standalone             1/1     XmXXs
   ```

9. Verify the app is installed on an indexer peer.

   Run:

   ```bash
   kubectl exec splunk-c3-apps-idxc-indexer-0 -n splunk-operator -- ls /opt/splunk/etc/peer-apps | grep -Fx Splunk_TA_paloalto
   ```

   Expected output:

   ```text
   Splunk_TA_paloalto
   ```

10. Verify the app is installed on a search head peer.

    Run:

    ```bash
    kubectl exec splunk-c3-apps-shc-search-head-0 -n splunk-operator -- ls /opt/splunk/etc/apps | grep -Fx Splunk_SA_CIM
    ```

    Expected output:

    ```text
    Splunk_SA_CIM
    ```

> **Checkpoint:** You now have a running C3 deployment with apps installed on both the indexer peers and search head peers!

## 5. Delete the Splunk Enterprise Instances

1. Destroy the Standalone Splunk Enterprise instance by uninstalling the Standalone Helm release.

   Run:

   ```bash
   helm uninstall s1-apps -n splunk-operator
   ```

   Expected output:

   ```text
   release "s1-apps" uninstalled
   ```

2. Destroy the C3 Splunk Enterprise deployment by uninstalling the C3 Helm release.

   Run:

   ```bash
   helm uninstall c3-apps -n splunk-operator
   ```

   Expected output:

   ```text
   release "c3-apps" uninstalled
   ```

3. Verify the Splunk Enterprise Helm releases are removed. The Splunk Operator Helm release should still exist.

   Run:

   ```bash
   helm list -n splunk-operator
   ```

   Expected output:

   ```text
   NAME              NAMESPACE        REVISION   UPDATED       STATUS     CHART                     APP VERSION
   splunk-operator   splunk-operator  1          <timestamp>   deployed   splunk-operator-X.Y.Z     X.Y.Z
   ```

4. Verify all of the Kubernetes resources are destroyed. It may take several minutes for the StatefulSets and Pods to be removed.

   1. Verify the Custom Resources are deleted.

      Run:

      ```bash
      kubectl get standalone -n splunk-operator
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

5. Delete any remaining Splunk Enterprise PVCs.

   StatefulSet PVCs can outlive a Helm release. The temporary vCluster will be deleted at the end of the lab, but these commands keep the namespace clean before you uninstall the operator.

   Run:

   ```bash
   kubectl delete pvc -n splunk-operator -l app.kubernetes.io/instance=splunk-s1-apps-standalone --ignore-not-found
   kubectl delete pvc -n splunk-operator -l app.kubernetes.io/instance=splunk-c3-apps-cm-cluster-manager --ignore-not-found
   kubectl delete pvc -n splunk-operator -l app.kubernetes.io/instance=splunk-c3-apps-idxc-indexer --ignore-not-found
   kubectl delete pvc -n splunk-operator -l app.kubernetes.io/instance=splunk-c3-apps-shc-deployer --ignore-not-found
   kubectl delete pvc -n splunk-operator -l app.kubernetes.io/instance=splunk-c3-apps-shc-search-head --ignore-not-found
   ```

   Expected output if PVCs existed:

   ```text
   persistentvolumeclaim "pvc-etc-splunk-s1-apps-standalone-0" deleted
   persistentvolumeclaim "pvc-var-splunk-s1-apps-standalone-0" deleted
   persistentvolumeclaim "pvc-etc-splunk-c3-apps-cm-cluster-manager-0" deleted
   persistentvolumeclaim "pvc-var-splunk-c3-apps-cm-cluster-manager-0" deleted
   persistentvolumeclaim "pvc-etc-splunk-c3-apps-idxc-indexer-0" deleted
   persistentvolumeclaim "pvc-etc-splunk-c3-apps-idxc-indexer-1" deleted
   persistentvolumeclaim "pvc-etc-splunk-c3-apps-idxc-indexer-2" deleted
   persistentvolumeclaim "pvc-var-splunk-c3-apps-idxc-indexer-0" deleted
   persistentvolumeclaim "pvc-var-splunk-c3-apps-idxc-indexer-1" deleted
   persistentvolumeclaim "pvc-var-splunk-c3-apps-idxc-indexer-2" deleted
   persistentvolumeclaim "pvc-etc-splunk-c3-apps-shc-deployer-0" deleted
   persistentvolumeclaim "pvc-var-splunk-c3-apps-shc-deployer-0" deleted
   persistentvolumeclaim "pvc-etc-splunk-c3-apps-shc-search-head-0" deleted
   persistentvolumeclaim "pvc-etc-splunk-c3-apps-shc-search-head-1" deleted
   persistentvolumeclaim "pvc-etc-splunk-c3-apps-shc-search-head-2" deleted
   persistentvolumeclaim "pvc-var-splunk-c3-apps-shc-search-head-0" deleted
   persistentvolumeclaim "pvc-var-splunk-c3-apps-shc-search-head-1" deleted
   persistentvolumeclaim "pvc-var-splunk-c3-apps-shc-search-head-2" deleted
   ```

6. Verify the Persistent Volume Claims for the Splunk Enterprise Pods are deleted. The Splunk Operator PVC should still exist.

   Run:

   ```bash
   kubectl get pvc -n splunk-operator
   ```

   Expected output:

   ```text
   NAME                           STATUS   VOLUME                            CAPACITY   ACCESS MODES   STORAGECLASS   VOLUMEATTRIBUTESCLASS   AGE
   splunk-operator-app-download   Bound    pvc-vvvv-wwww-xxxx-yyyy-zzzz      10Gi       RWO                           <unset>                 XXm
   ```

## 6. Delete the SOK Release

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

## 7. Delete the Kubernetes Environment

1. [Delete](https://kraken.splunkdev.page/kraken-docs/reference/cli/delete/) the Kraken deployment.
