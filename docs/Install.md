# Splunk Operator Advanced Installation



## Downloading Installation YAML for modifications

If you want to customize the installation of the Splunk Operator, download a copy of the installation YAML locally, and open it in your favorite editor.

```
wget -O splunk-operator.yaml https://github.com/splunk/splunk-operator/releases/download/1.0.5/splunk-operator-cluster.yaml
```

## Default Installation

By default operator will be installed in `splunk-operator` and will watch all the namespaces of your cluster for splunk enterprise custom resources:

```
wget -O splunk-operator.yaml https://github.com/splunk/splunk-operator/releases/download/1.0.5/splunk-operator-cluster.yaml
kubectl apply -f splunk-operator-cluster.yaml
```

## Install operator to watch single namespace

By default operator will be installed in `splunk-operator` namespace and will watch all the namespaces of your cluster for splunk enterprise custom resources:
if user wants to watch only one namespace then eeit `config-map` `splunk-operator-config` in `splunk-operaor` namespace, set `WATCH_NAMESPACE` field to the namespace operator should watch

```
apiVersion: v1
data:
  OPERATOR_NAME: '"splunk-operator"'
  RELATED_IMAGE_SPLUNK_ENTERPRISE: splunk/splunk:latest
  WATCH_NAMESPACE: "namespace1"
kind: ConfigMap
metadata:
  labels:
    name: splunk-operator
  name: splunk-operator-config
  namespace: splunk-operator
```

## Install operator to watch multiple namespace

if user want to manager multiple namespaces, then they can set `WATCH_NAMESPACE` field to all those namespaces , names should be comma (,) sepearated. example

```
apiVersion: v1
data:
  OPERATOR_NAME: '"splunk-operator"'
  RELATED_IMAGE_SPLUNK_ENTERPRISE: splunk/splunk:latest
  WATCH_NAMESPACE: "namespace1,namespace2"
kind: ConfigMap
metadata:
  labels:
    name: splunk-operator
  name: splunk-operator-config
  namespace: splunk-operator
```

## Private Registries

If you plan to retag the container images as part of pushing it to a private registry, edit the `manager` container image parameter in the  `splunk-operator-controller-manager` deployment to reference the appropriate image name.

```yaml
# Replace this with the built image name
image: splunk/splunk-operator
```

If you are using a private registry for the Docker images, edit `config-map` `splunk-operator-config` in `splunk-operaor` namespace, set `RELATED_IMAGE_SPLUNK_ENTERPRISE` field splunk docker image path

```
apiVersion: v1
data:
  OPERATOR_NAME: '"splunk-operator"'
  RELATED_IMAGE_SPLUNK_ENTERPRISE: splunk/splunk:latest
  WATCH_NAMESPACE: ""
kind: ConfigMap
metadata:
  labels:
    name: splunk-operator
  name: splunk-operator-config
  namespace: splunk-operator
```

## Cluster Domain

By default, the Splunk Operator will use a Kubernetes cluster domain of `cluster.local` to calculate the fully qualified domain names (FQDN) for each instance in your deployment. If you have configured a custom domain for your Kubernetes cluster, you can override the operator by adding a `CLUSTER_DOMAIN`
environment variable to the operator's deployment spec:

```yaml
- name: CLUSTER_DOMAIN
  value: "mydomain.com"
```
