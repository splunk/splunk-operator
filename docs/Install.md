# Splunk Operator Advanced Installation



## Downloading Installation YAML for modifications

If you want to customize the installation of the Splunk Operator, download a copy of the installation YAML locally, and open it in your favorite editor.

```
wget -O splunk-operator-cluster.yaml https://github.com/splunk/splunk-operator/releases/download/3.0.0/splunk-operator-cluster.yaml
```

## Default Installation

Based on the file used Splunk Operator can be installed cluster-wide or namespace scoped. By default operator will be installed in `splunk-operator` namespace. User can change the default installation namespace by editing the manifest file `splunk-operator-namespace.yaml` or `splunk-operator-cluster.yaml`

By installing `splunk-operator-cluster.yaml` Operator will watch all the namespaces of your cluster for splunk enterprise custom resources

```
wget -O splunk-operator-cluster.yaml https://github.com/splunk/splunk-operator/releases/download/3.0.0/splunk-operator-cluster.yaml
kubectl apply -f splunk-operator-cluster.yaml --server-side
```

## Install Operator to Accept the Splunk General Terms

Starting with Operator version 3.0.0, which includes support for Splunk Enterprise version 10.x, an additional Docker-Splunk specific parameter is required to start containers. **This is a breaking change, and user action is required.**

Starting in 10.x image versions of Splunk Enterprise, license acceptance requires an additional `SPLUNK_GENERAL_TERMS=--accept-sgt-current-at-splunk-com` argument. This indicates that users have read and accepted the current/latest version of the Splunk General Terms, available at https://www.splunk.com/en_us/legal/splunk-general-terms.html as may be updated from time to time. Unless you have jointly executed with Splunk a negotiated version of these General Terms that explicitly supersedes this agreement, by accessing or using Splunk software, you are agreeing to the Splunk General Terms posted at the time of your access and use and acknowledging its applicability to the Splunk software. Please read and make sure you agree to the Splunk General Terms before you access or use this software. Only after doing so should you include the `--accept-sgt-current-at-splunk-com` flag to indicate your acceptance of the current/latest Splunk General Terms and launch this software. All examples below have been updated with this change.

If you use the below examples and the ‘--accept-sgt-current-at-splunk-com’ flag, you are indicating that you have read and accepted the current/latest version of the Splunk General Terms, as may be updated from time to time, and acknowledging its applicability to this software - as noted above.

By default, the SPLUNK_GENERAL_TERMS environment variable will be set to an empty string. You must either manually update it to have the required additional value `--accept-sgt-current-at-splunk-com` in the splunk-operator-controller-manager deployment, or you can pass the `SPLUNK_GENERAL_TERMS` parameter with the required additional value to the `make deploy` command.

```yaml
...
        env:
        - name: WATCH_NAMESPACE
          value: "namespace1,namespace2"
        - name: RELATED_IMAGE_SPLUNK_ENTERPRISE
          value: splunk/splunk:9.4.0
        - name: OPERATOR_NAME
          value: splunk-operator
        - name: SPLUNK_GENERAL_TERMS
          value: "--accept-sgt-current-at-splunk-com"
        - name: POD_NAME
          valueFrom:
            fieldRef:
              apiVersion: v1
              fieldPath: metadata.name
...
```

## Install operator to watch multiple namespaces

If Splunk Operator is installed clusterwide and user wants to manage multiple namespaces, they must add the namespaces to the WATCH_NAMESPACE field with each namespace separated by a comma (,).  Edit `deployment` `splunk-operator-controller-manager-<podid>` in `splunk-operator` namespace, set `WATCH_NAMESPACE` field to the namespace that needs to be monitored by Splunk Operator

```yaml
...
        env:
        - name: WATCH_NAMESPACE
          value: "namespace1,namespace2"
        - name: RELATED_IMAGE_SPLUNK_ENTERPRISE
          value: splunk/splunk:9.4.0
        - name: OPERATOR_NAME
          value: splunk-operator
        - name: SPLUNK_GENERAL_TERMS
          value: ""
        - name: POD_NAME
          valueFrom:
            fieldRef:
              apiVersion: v1
              fieldPath: metadata.name
...
```

## Install operator to watch single namespace with restrictive permission

In order to install operator with restrictive permission to watch only single namespace use [splunk-operator-namespace.yaml](https://github.com/splunk/splunk-operator/releases/download/3.0.0/splunk-operator-namespace.yaml). This will create Role and Role-Binding to only watch single namespace. By default operator will be installed in `splunk-operator` namespace, user can edit the file to change the namespace.

```
wget -O splunk-operator-namespace.yaml https://github.com/splunk/splunk-operator/releases/download/3.0.0/splunk-operator-namespace.yaml
kubectl apply -f splunk-operator-namespace.yaml --server-side
```


## Private Registries

If you plan to retag the container images as part of pushing it to a private registry, edit the `manager` container image parameter in the  `splunk-operator-controller-manager` deployment to reference the appropriate image name.

```yaml
# Replace this with the built image name
image: splunk/splunk-operator
```

If you are using a private registry for the Docker images, edit `deployment` `splunk-operator-controller-manager-xxxx` in `splunk-operator` namespace, set `RELATED_IMAGE_SPLUNK_ENTERPRISE` field splunk docker image path

```yaml
...
        env:
        - name: WATCH_NAMESPACE
          value: "namespace1,namespace2"
        - name: RELATED_IMAGE_SPLUNK_ENTERPRISE
          value: splunk/splunk:9.4.0
        - name: OPERATOR_NAME
          value: splunk-operator
        - name: SPLUNK_GENERAL_TERMS
          value: ""
        - name: POD_NAME
          valueFrom:
            fieldRef:
              apiVersion: v1
              fieldPath: metadata.name
...
```
## Distroless Image Support

As part of enhancing security and reducing the attack surface of the Splunk Operator container, a **distroless image** is now supported. The distroless image contains only the essential components required to run the Splunk Operator, without a shell or package manager, resulting in a smaller and more secure image.

### How to Use the Distroless Image

1. **Image Tag**:
   - The distroless image can be identified by the `-distroless` suffix in its tag.
   - Example: `splunk/splunk-operator:3.0.0-distroless`

2. **Modifying the Deployment**:
   - To use the distroless image, update the `manager` container image in the `splunk-operator-controller-manager` deployment as follows:

   ```yaml
   # Replace this with the distroless image name
   image: splunk/splunk-operator:3.0.0-distroless
   ```

3. **Private Registry**:
   - If using a private registry, ensure that the distroless image is retagged and pushed appropriately, and update the deployment image reference.

### Debugging with Distroless Images

Since distroless images do not contain a shell, debugging may require additional steps. One approach is to use a **sidecar container** that includes a shell and necessary utilities to inspect mapped volumes and files.

---

#### **Steps to Debug a Distroless Image Using a Sidecar**

1. **Modify the Splunk Operator Deployment**:
   - Add a sidecar container to the `splunk-operator-controller-manager` deployment. The sidecar container will have a shell and basic debugging tools.

   Example deployment snippet with a sidecar:

   ```yaml
   apiVersion: apps/v1
   kind: Deployment
   metadata:
     name: splunk-operator-controller-manager
     namespace: splunk-operator
   spec:
     replicas: 1
     selector:
       matchLabels:
         control-plane: controller-manager
     template:
       metadata:
         labels:
           control-plane: controller-manager
       spec:
         containers:
           - name: manager
             image: splunk/splunk-operator:3.0.0-distroless
             env:
               - name: WATCH_NAMESPACE
                 value: ""
               - name: RELATED_IMAGE_SPLUNK_ENTERPRISE
                 value: splunk/splunk:9.4.0
               - name: SPLUNK_GENERAL_TERMS
                 value: ""
           - name: sok-debug
             image: ubuntu:20.04  # Use any lightweight image with a shell
             command: ["/bin/bash", "-c", "tail -f /dev/null"]
             volumeMounts:
               - name: app-staging
                 mountPath: /opt/splunk/appframework/
         volumes:
           - name: app-staging
             persistentVolumeClaim:
              claimName: splunk-operator-app-download
   ```

2. **Access the Sidecar Container**:
   - Once the sidecar is running, you can `exec` into it and inspect the shared volumes or files.
   
   Example command to access the sidecar container:
   ```bash
   kubectl exec -it <splunk-operator-pod-name> -c sok-debug -- /bin/bash
   ```

3. **Inspect Shared Files**:
   - Navigate to the mounted volume (`/opt/splunk/appframework/` in the example above) to inspect files shared with the distroless container.
   
   Example commands:
   ```bash
   cd /opt/splunk/appframework/
   ls -l
   ```

4. **Check Logs**:
   - You can also check logs directly by accessing the appropriate log files (if mapped) or using `kubectl logs`.
   
   Example command:
   ```bash
   kubectl logs <splunk-operator-pod-name> -c manager
   ```

5. **Cleanup**:
   - After debugging, remove the sidecar container by editing the deployment and deleting the sidecar configuration, or simply redeploy the Splunk Operator without the sidecar.

---

## Cluster Domain

By default, the Splunk Operator will use a Kubernetes cluster domain of `cluster.local` to calculate the fully qualified domain names (FQDN) for each instance in your deployment. If you have configured a custom domain for your Kubernetes cluster, you can override the operator by adding a `CLUSTER_DOMAIN`
environment variable to the operator's deployment spec:

```yaml
- name: CLUSTER_DOMAIN
  value: "mydomain.com"
```
