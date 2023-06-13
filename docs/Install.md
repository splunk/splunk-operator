# Splunk Operator Advanced Installation



## Downloading Installation YAML for modifications

If you want to customize the installation of the Splunk Operator, download a copy of the installation YAML locally, and open it in your favorite editor.

```
wget -O splunk-operator-cluster.yaml https://github.com/splunk/splunk-operator/releases/download/2.3.0/splunk-operator-cluster.yaml
```

## Default Installation

Based on the file used Splunk Operator can be installed cluster-wide or namespace scoped. By default operator will be installed in `splunk-operator` namespace. User can change the default installation namespace by editing the manifest file `splunk-operator-namespace.yaml` or `splunk-operator-cluster.yaml`

By installing `splunk-operator-cluster.yaml` Operator will watch all the namespaces of your cluster for splunk enterprise custom resources

```
wget -O splunk-operator-cluster.yaml https://github.com/splunk/splunk-operator/releases/download/2.3.0/splunk-operator-cluster.yaml
kubectl apply -f splunk-operator-cluster.yaml
```

## Install operator to watch multiple namespaces

If Splunk Operator is installed clusterwide and user wants to manage multiple namespaces, they must add the namespaces to the WATCH_NAMESPACE field with each namespace separated by a comma (,).  Edit `deployment` `splunk-operator-controller-manager-<podid>` in `splunk-operator` namespace, set `WATCH_NAMESPACE` field to the namespace that needs to be monitored by Splunk Operator

```yaml
...
        env:
        - name: WATCH_NAMESPACE
          value: "namespace1,namespace2"
        - name: RELATED_IMAGE_SPLUNK_ENTERPRISE
          value: splunk/splunk:9.0.3-a2
        - name: OPERATOR_NAME
          value: splunk-operator
        - name: POD_NAME
          valueFrom:
            fieldRef:
              apiVersion: v1
              fieldPath: metadata.name
...
```

## Install operator to watch single namespace with restrictive permission

In order to install operator with restrictive permission to watch only single namespace use [splunk-operator-namespace.yaml](https://github.com/splunk/splunk-operator/releases/download/2.3.0/splunk-operator-namespace.yaml). This will create Role and Role-Binding to only watch single namespace. By default operator will be installed in `splunk-operator` namespace, user can edit the file to change the namespace

```
wget -O splunk-operator-namespace.yaml https://github.com/splunk/splunk-operator/releases/download/2.3.0/splunk-operator-namespace.yaml
kubectl apply -f splunk-operator-namespace.yaml
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
          value: splunk/splunk:9.0.3-a2
        - name: OPERATOR_NAME
          value: splunk-operator
        - name: POD_NAME
          valueFrom:
            fieldRef:
              apiVersion: v1
              fieldPath: metadata.name
...
```

## Cluster Domain

By default, the Splunk Operator will use a Kubernetes cluster domain of `cluster.local` to calculate the fully qualified domain names (FQDN) for each instance in your deployment. If you have configured a custom domain for your Kubernetes cluster, you can override the operator by adding a `CLUSTER_DOMAIN`
environment variable to the operator's deployment spec:

```yaml
- name: CLUSTER_DOMAIN
  value: "mydomain.com"
```
