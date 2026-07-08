---
title: 1 - Standalone Deployed with Manifest Files
parent: Guided Labs
nav_order: 1
---

# 1 - Standalone Deployed with Manifest Files

[Standalone (S1)](https://help.splunk.com/en/splunk-enterprise/get-started/splunk-validated-architectures/splunk-platform-indexing-and-search/single-server-deployment-s1) deployments are used for smaller, non-business critical use-cases that are often departmental in nature. They are great for starting to understand how SOK and Splunk Enterprise work together.

In this lab, you will create a temporary Kubernetes environment, install the Splunk Operator, create a Standalone custom resource, verify the resulting StatefulSet, Pod, and PVCs, scale the Standalone deployment from one pod to two pods, then log in to Splunk Web.

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

4. Verify the Splunk Operator Controller Manager pod is up and running. It might take up to 1 minute for the pod to be ready. You can run the command multiple times if necessary.

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

## 3. Deploy a Standalone Splunk Enterprise Pod with Manifest Files

1. Copy the Standalone Custom Resource (CR) manifest to a file.

   Run:

   ```bash
   cat > s1.yaml <<'EOF'
   apiVersion: enterprise.splunk.com/v4       # CR Version
   kind: Standalone                           # CR Type
   metadata:
     name: standalone                         # CR Name
     namespace: splunk-operator               # Kubernetes Namespace to deploy the CR
     finalizers:                              # Conditions before it fully deleting resources
       - enterprise.splunk.com/delete-pvc
   EOF
   ```

2. Create a Splunk Standalone instance by applying the YAML.

   Run:

   ```bash
   kubectl apply -f s1.yaml
   ```

   Expected output:

   ```text
   standalone.enterprise.splunk.com/standalone created
   ```

3. Verify all of the Kubernetes resources are deployed. It may take up to 2 minutes for the resources to become available.

   1. Verify the Standalone Custom Resource is created.

      Run:

      ```bash
      kubectl get standalone -n splunk-operator
      ```

      Expected output:

      ```text
      NAME         PHASE   DESIRED   READY   AGE     MESSAGE
      standalone   Ready   1         1       XmXXs
      ```

   2. Verify the Standalone StatefulSet is created.

      Run:

      ```bash
      kubectl get statefulsets -n splunk-operator
      ```

      Expected output:

      ```text
      NAME                           READY   AGE
      splunk-standalone-standalone   1/1     XmXXs
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
      splunk-standalone-standalone-0                        1/1     Running   0          XmXXs
      ```

   4. Verify the Persistent Volume Claims are created.

      Run:

      ```bash
      kubectl get pvc -n splunk-operator
      ```

      Expected output:

      ```text
      NAME                                     STATUS   VOLUME                            CAPACITY   ACCESS MODES   STORAGECLASS   VOLUMEATTRIBUTESCLASS   AGE
      pvc-etc-splunk-standalone-standalone-0   Bound    pvc-vvvv-wwww-xxxx-yyyy-zzzz      10Gi       RWO                           <unset>                 6m5s
      pvc-var-splunk-standalone-standalone-0   Bound    pvc-vvvv-wwww-xxxx-yyyy-zzzz      100Gi      RWO                           <unset>                 6m5s
      splunk-operator-app-download             Bound    pvc-vvvv-wwww-xxxx-yyyy-zzzz      10Gi       RWO                           <unset>                 24m
      ```

   Tips:

   - Replace `get` with `describe` for detailed output.
   - Append `-o json` or `-o yaml` for output in JSON or YAML format.
   - Use `kubectl logs <pod_name>` to view logs for any pod. Use the `-f` option to follow logs.

4. Verify the splunkd logs are running.

   Run:

   ```bash
   kubectl exec -it splunk-standalone-standalone-0 -n splunk-operator -- tail -n 100 /opt/splunk/var/log/splunk/splunkd.log
   ```

> **Checkpoint:** You now have a running Splunk Standalone instance!

## 4. Scale the Standalone Splunk Enterprise Pods

1. Update the Standalone Custom Resource (CR) manifest to request two replicas.

   Run:

   ```bash
   cat > standalone.yaml <<'EOF'
   apiVersion: enterprise.splunk.com/v4       # CR Version
   kind: Standalone                           # CR Type
   metadata:
     name: standalone                         # CR Name
     namespace: splunk-operator               # Kubernetes Namespace to deploy the CR
     finalizers:                              # Conditions before it fully deleting resources
       - enterprise.splunk.com/delete-pvc
   spec:
     replicas: 2                              # Number of Standalone pods
   EOF
   ```

2. Apply the updated YAML.

   Run:

   ```bash
   kubectl apply -f standalone.yaml
   ```

   Expected output:

   ```text
   standalone.enterprise.splunk.com/standalone configured
   ```

3. Verify all of the Kubernetes resources are scaled. It may take up to 2 minutes for the second pod to become available.

   1. Verify the Standalone Custom Resource is scaled.

      Run:

      ```bash
      kubectl get standalone -n splunk-operator
      ```

      Expected output:

      ```text
      NAME         PHASE   DESIRED   READY   AGE     MESSAGE
      standalone   Ready   2         2       XmXXs
      ```

   2. Verify the Standalone StatefulSet is scaled.

      Run:

      ```bash
      kubectl get statefulsets -n splunk-operator
      ```

      Expected output:

      ```text
      NAME                           READY   AGE
      splunk-standalone-standalone   2/2     XmXXs
      ```

   3. Verify both Standalone Pods are created.

      Run:

      ```bash
      kubectl get pods -n splunk-operator
      ```

      Expected output:

      ```text
      NAME                                                  READY   STATUS    RESTARTS   AGE
      splunk-operator-controller-manager-xxxx-yyyy          1/1     Running   0          XXm
      splunk-standalone-standalone-0                        1/1     Running   0          XXm
      splunk-standalone-standalone-1                        1/1     Running   0          XmXXs
      ```

   4. Verify the Persistent Volume Claims are created for both Standalone Pods.

      Run:

      ```bash
      kubectl get pvc -n splunk-operator
      ```

      Expected output:

      ```text
      NAME                                     STATUS   VOLUME                            CAPACITY   ACCESS MODES   STORAGECLASS   VOLUMEATTRIBUTESCLASS   AGE
      pvc-etc-splunk-standalone-standalone-0   Bound    pvc-vvvv-wwww-xxxx-yyyy-zzzz      10Gi       RWO                           <unset>                 XXm
      pvc-etc-splunk-standalone-standalone-1   Bound    pvc-vvvv-wwww-xxxx-yyyy-zzzz      10Gi       RWO                           <unset>                 XmXXs
      pvc-var-splunk-standalone-standalone-0   Bound    pvc-vvvv-wwww-xxxx-yyyy-zzzz      100Gi      RWO                           <unset>                 XXm
      pvc-var-splunk-standalone-standalone-1   Bound    pvc-vvvv-wwww-xxxx-yyyy-zzzz      100Gi      RWO                           <unset>                 XmXXs
      splunk-operator-app-download             Bound    pvc-vvvv-wwww-xxxx-yyyy-zzzz      10Gi       RWO                           <unset>                 XXm
      ```

> **Checkpoint:** You now have two running Splunk Standalone pods!

## 5. Login to Splunk

1. Retrieve the admin password from the created secret.

   Run:

   ```bash
   kubectl get secret splunk-splunk-operator-secret -n splunk-operator -o jsonpath='{.data.password}' | base64 --decode
   ```

2. Use a simple network port forward to open port 8000 for Splunk Web access.

   Run:

   ```bash
   kubectl port-forward svc/splunk-standalone-standalone-service -n splunk-operator 8000
   ```

   Expected output:

   ```text
   Forwarding from 127.0.0.1:8000 -> 8000
   Forwarding from [::1]:8000 -> 8000
   ```

3. Open `127.0.0.1:8000` in your machine's browser. Login with username: `admin`, and the password output from Step 5.1.
   1. When you are done exploring, close the browser window, and press `Ctrl+C` to stop the port-forwarding.

> **Checkpoint:** You can now connect to the running Splunk Standalone instance and run SPL queries!

## 6. Delete the Splunk Enterprise Instance

1. Destroy the Splunk Standalone instance by deleting the YAML.

   Run:

   ```bash
   kubectl delete -f s1.yaml
   ```

   Expected output:

   ```text
   standalone.enterprise.splunk.com "standalone" deleted
   ```

2. Verify all of the Kubernetes resources are destroyed.

   1. Verify the Custom Resource is deleted.

      Run:

      ```bash
      kubectl get standalone -n splunk-operator
      ```

      Expected output:

      ```text
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

   3. Verify the Standalone Pod is deleted. The Splunk Operator Pod should still exist.

      Run:

      ```bash
      kubectl get pods -n splunk-operator
      ```

      Expected output:

      ```text
      NAME                                                  READY   STATUS    RESTARTS   AGE
      splunk-operator-controller-manager-xxxx-yyyy          1/1     Running   0          XXm
      ```

   4. Verify the Persistent Volume Claims for the Standalone Pods are deleted. The Splunk Operator PVC should still exist.

      Run:

      ```bash
      kubectl get pvc -n splunk-operator
      ```

      Expected output:

      ```text
      NAME                                     STATUS   VOLUME                            CAPACITY   ACCESS MODES   STORAGECLASS   VOLUMEATTRIBUTESCLASS   AGE
      splunk-operator-app-download             Bound    pvc-vvvv-wwww-xxxx-yyyy-zzzz      10Gi       RWO                           <unset>                 24m
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
