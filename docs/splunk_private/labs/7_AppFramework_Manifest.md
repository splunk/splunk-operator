---
title: 7 - App Framework Deployed with Manifest Files
parent: Guided Labs
nav_order: 7
---

# 7 - Standalone and Clustered Deployment with Apps Deployed with Manifest Files

The Splunk Operator App Framework installs and updates Splunk apps from remote object storage. You define app source locations in a Custom Resource manifest, and the operator downloads the app packages and installs them on standalone pods or pushes them across clustered deployments through Splunk Enterprise management components.

In this lab, you will create a temporary Kubernetes environment, install the Splunk Operator, connect to Kraken App Framework storage, create a Standalone custom resource with an app, then create a C3 deployment that installs apps on indexer peers through the ClusterManager and on search head peers through the SearchHeadCluster.

## Before You Start

You need:

- Kraken CLI - [Download and install](https://kraken.splunkdev.page/kraken-docs/get-started/quick-start/#download-the-cli)
- kubectl - [Install](https://kubernetes.io/docs/tasks/tools/)
- jq - [Install](https://jqlang.org/download/)
- AWS CLI - [Install](https://docs.aws.amazon.com/cli/latest/userguide/getting-started-install.html)

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

4. Follow the steps in the [Kraken docs](https://kraken.splunkdev.page/kraken-docs/features/app-framework/#vcluster-only-and-manual-sok-installs) to read the expected identity and role and annotate the Splunk Operator ServiceAccount.

5. Verify the Splunk Operator Controller Manager pod is up and running. It might take up to 1 minute for the pod to be ready. You can run the command multiple times if necessary.

   Run:

   ```bash
   kubectl get pods -n splunk-operator
   ```

   Expected output:

   ```text
   NAME                                           READY   STATUS    RESTARTS   AGE
   splunk-operator-controller-manager-xxxx-yyyy   1/1     Running   0          XXs
   ```

6. Check the Splunk Operator logs are healthy.

   Run:

   ```bash
   kubectl logs deployment/splunk-operator-controller-manager -n splunk-operator | grep -F "Starting Controller"
   ```

   Expected output contains logs verifying the controllers are starting for each Custom Resource:

   ```text
   INFO	Starting Controller	{"controller": "indexer-cluster-controller", "controllerGroup": "enterprise.splunk.com", "controllerKind": "IndexerCluster"}
   ```

7. Verify the Splunk Operator ServiceAccount has the correct IRSA annotation.

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

## 3. Deploy a Standalone Splunk Enterprise Pod with Apps with Manifest Files

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

3. Copy the Standalone Custom Resource (CR) manifest to a file.

   Run:

   ```bash
   cat > s1-apps.yaml <<EOF
   apiVersion: enterprise.splunk.com/v4
   kind: Standalone
   metadata:
     name: s1-apps
     namespace: splunk-operator
     finalizers:
       - enterprise.splunk.com/delete-pvc
   spec:
     replicas: 1
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

   - `apiVersion: enterprise.splunk.com/v4`: Splunk Enterprise CR API version.
   - `kind: Standalone`: Splunk Enterprise CR type.
   - `metadata.name: s1-apps`: Name of the Standalone CR.
   - `metadata.namespace: splunk-operator`: Kubernetes namespace in which to create the CR.
   - `metadata.finalizers`: Actions that must complete before Kubernetes deletes the CR.
   - `spec.replicas: 1`: Number of Standalone pods to create.
   - `spec.appRepo`: App Framework configuration for locating and installing apps from remote storage.
   - `appsRepoPollIntervalSeconds: 60`: Number of seconds between checks for new or changed apps in remote storage.
   - `defaults`: Default settings applied to each entry in `appSources` unless that entry overrides them.
   - `defaults.volumeName: volume_app_repo`: Remote volume that each app source uses by default; it must match a name in `volumes`.
   - `defaults.scope: local`: Installs apps locally on the Standalone pod.
   - `appSources`: List of remote locations containing app packages such as `.tgz` or `.spl` files.
   - `appSources[].name: apps`: Unique logical name for this app source within `appRepo`.
   - `appSources[].location: s1-apps/`: Directory containing the apps, relative to the remote volume's `path`.
   - `volumes`: List of remote storage volumes available to the App Framework.
   - `volumes[].name: volume_app_repo`: Unique name used to reference this remote volume from `volumeName`.
   - `volumes[].storageType: s3`: Type of remote object storage.
   - `volumes[].provider: aws`: Provider of the remote object storage.
   - `volumes[].path: ${WORKSPACE}/`: S3 bucket and optional prefix that form the base path for app sources.
   - `volumes[].endpoint`: URL of the S3 service endpoint.
   - `volumes[].region: $AWS_REGION`: AWS region containing the S3 bucket.

4. Create a Splunk Standalone instance by applying the YAML.

   Run:

   ```bash
   kubectl apply -f s1-apps.yaml
   ```

   Expected output:

   ```text
   standalone.enterprise.splunk.com/s1-apps created
   ```

5. Verify all of the Kubernetes resources are deployed. It may take several minutes for the resource to become ready.

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
      NAME                        READY   AGE
      splunk-s1-apps-standalone   1/1     XmXXs
      ```

   3. Verify the Standalone Pod is created.

      Run:

      ```bash
      kubectl get pods -n splunk-operator
      ```

      Expected output:

      ```text
      NAME                                                  READY   STATUS    RESTARTS   AGE
      splunk-operator-controller-manager-xxxx-yyyy          1/1     Running   0          XXm
      splunk-s1-apps-standalone-0                           1/1     Running   0          XmXXs
      ```

   Tips:

   - Replace `get` with `describe` for detailed output.
   - Append `-o json` or `-o yaml` for output in JSON or YAML format.
   - Use `kubectl logs <pod_name>` to view logs for any pod. Use the `-f` option to follow logs.

6. Verify the splunkd logs are running.

   Run:

   ```bash
   kubectl exec -it splunk-s1-apps-standalone-0 -n splunk-operator -- tail -n 100 /opt/splunk/var/log/splunk/splunkd.log
   ```

7. Verify the app is installed.

   Run:

   ```bash
   kubectl exec splunk-s1-apps-standalone-0 -n splunk-operator -- ls /opt/splunk/etc/apps | grep -Fx TA-LDAP
   ```

   Expected output:

   ```text
   TA-LDAP
   ```

> **Checkpoint:** You now have a running Splunk Standalone instance with an app installed!

## 4. Deploy a Clustered Splunk Enterprise Deployment with Apps with Manifest Files

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

2. Copy the C3 Custom Resource (CR) manifests to a single file.

   Run:

   ```bash
   cat > c3-apps.yaml <<EOF
   apiVersion: enterprise.splunk.com/v4
   kind: ClusterManager
   metadata:
     name: c3-apps-cm
     namespace: splunk-operator
     finalizers:
       - enterprise.splunk.com/delete-pvc
   spec:
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
   ---
   apiVersion: enterprise.splunk.com/v4
   kind: IndexerCluster
   metadata:
     name: c3-apps-idxc
     namespace: splunk-operator
     finalizers:
       - enterprise.splunk.com/delete-pvc
   spec:
     clusterManagerRef:
       name: c3-apps-cm
     replicas: 3
   ---
   apiVersion: enterprise.splunk.com/v4
   kind: SearchHeadCluster
   metadata:
     name: c3-apps-shc
     namespace: splunk-operator
     finalizers:
       - enterprise.splunk.com/delete-pvc
   spec:
     clusterManagerRef:
       name: c3-apps-cm
     replicas: 3
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

   - `apiVersion: enterprise.splunk.com/v4`: Splunk Enterprise CR API version used by all three resources.
   - `kind`: Creates a `ClusterManager`, `IndexerCluster`, or `SearchHeadCluster` CR.
   - `metadata.name`: Unique name of each CR: `c3-apps-cm`, `c3-apps-idxc`, or `c3-apps-shc`.
   - `metadata.namespace: splunk-operator`: Kubernetes namespace in which to create each CR.
   - `metadata.finalizers`: Actions that must complete before Kubernetes deletes each CR.
   - `spec.clusterManagerRef.name: c3-apps-cm`: Connects the indexer and search head clusters to the ClusterManager CR.
   - `spec.replicas: 3`: Creates three indexer peers or three search head peers for the applicable CR.
   - `spec.appRepo`: App Framework configuration on the ClusterManager and SearchHeadCluster CRs.
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
   - `---`: Separates multiple Kubernetes resources in one YAML file.

3. Create the C3 Splunk Enterprise deployment by applying the YAML.

   Run:

   ```bash
   kubectl apply -f c3-apps.yaml
   ```

   Expected output:

   ```text
   clustermanager.enterprise.splunk.com/c3-apps-cm created
   indexercluster.enterprise.splunk.com/c3-apps-idxc created
   searchheadcluster.enterprise.splunk.com/c3-apps-shc created
   ```

   After you apply this manifest, the cluster manager pod comes up first, the search head cluster deployer starts before the search head replicas, and the indexer and search head replicas become ready after they can connect to the cluster manager.

4. Verify the ClusterManager reaches the `Ready` phase. It may take several minutes for the resource to become ready.

   Run:

   ```bash
   kubectl get clustermanager -n splunk-operator
   ```

   Expected output:

   ```text
   NAME         PHASE   MANAGER   DESIRED   READY   AGE     MESSAGE
   c3-apps-cm   Ready                               XmXXs
   ```

5. Verify the IndexerCluster reaches the `Ready` phase. It may take several minutes for all three indexer pods to become ready.

   Run:

   ```bash
   kubectl get indexercluster -n splunk-operator
   ```

   Expected output:

   ```text
   NAME           PHASE   MASTER   MANAGER   DESIRED   READY   AGE     MESSAGE
   c3-apps-idxc   Ready            Ready     3         3       XmXXs
   ```

6. Verify the SearchHeadCluster reaches the `Ready` phase. It may take several minutes for the deployer and all three search head pods to become ready.

   Run:

   ```bash
   kubectl get searchheadcluster -n splunk-operator
   ```

   Expected output:

   ```text
   NAME          PHASE   DEPLOYER   DESIRED   READY   AGE     MESSAGE
   c3-apps-shc   Ready   Ready      3         3       XmXXs
   ```

7. Verify the C3 StatefulSets are created.

   Run:

   ```bash
   kubectl get statefulsets -n splunk-operator
   ```

   Expected output contains the C3 StatefulSets:

   ```text
   NAME                                  READY   AGE
   splunk-c3-apps-cm-cluster-manager     1/1     XmXXs
   splunk-c3-apps-idxc-indexer           3/3     XmXXs
   splunk-c3-apps-shc-deployer           1/1     XmXXs
   splunk-c3-apps-shc-search-head        3/3     XmXXs
   splunk-s1-apps-standalone             1/1     XmXXs
   ```

8. Verify the app is installed on an indexer peer.

   Run:

   ```bash
   kubectl exec splunk-c3-apps-idxc-indexer-0 -n splunk-operator -- ls /opt/splunk/etc/peer-apps | grep -Fx Splunk_TA_paloalto
   ```

   Expected output:

   ```text
   Splunk_TA_paloalto
   ```

9. Verify the app is installed on a search head peer.

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

1. Destroy the Splunk Standalone instance by deleting the YAML.

   Run:

   ```bash
   kubectl delete -f s1-apps.yaml
   ```

   Expected output:

   ```text
   standalone.enterprise.splunk.com "s1-apps" deleted
   ```

2. Destroy the C3 Splunk Enterprise deployment by deleting the YAML.

   Run:

   ```bash
   kubectl delete -f c3-apps.yaml
   ```

   Expected output:

   ```text
   clustermanager.enterprise.splunk.com "c3-apps-cm" deleted
   indexercluster.enterprise.splunk.com "c3-apps-idxc" deleted
   searchheadcluster.enterprise.splunk.com "c3-apps-shc" deleted
   ```

3. Verify all of the Kubernetes resources are destroyed.

   1. Verify the Custom Resource is deleted.

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

   2. Verify the StatefulSet is deleted.

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
      NAME                                     STATUS   VOLUME                            CAPACITY   ACCESS MODES   STORAGECLASS   VOLUMEATTRIBUTESCLASS   AGE
      splunk-operator-app-download             Bound    pvc-vvvv-wwww-xxxx-yyyy-zzzz      10Gi       RWO                           <unset>                 24m
      ```

## 6. Delete the SOK Release

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

## 7. Delete the Kubernetes Environment

1. [Delete](https://kraken.splunkdev.page/kraken-docs/reference/cli/delete/) the Kraken deployment.
