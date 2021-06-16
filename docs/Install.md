# Splunk Operator Advanced Installation



## Downloading Installation YAML for modifications

If you want to customize the installation of the Splunk Operator, download a copy of the installation YAML locally, and open it in your favorite editor.

```
wget -O splunk-operator.yaml https://github.com/splunk/splunk-operator/releases/download/1.0.1/splunk-operator-install.yaml
```


## Installation using a non-admin user

Kubernetes only allows administrators to install new `CustomResourceDefinition` objects. All other objects included in the `splunk-operator.yaml` file can be installed by regular users within their own namespaces. 

If you are not an administrator, you can have an administrator create the required objects for you by running:

```
kubectl apply -f https://github.com/splunk/splunk-operator/releases/download/1.0.1/splunk-operator-crds.yaml
```

Afterwards, you can download and use the yaml file to install the operator within your own namespace:

```
wget -O splunk-operator.yaml https://github.com/splunk/splunk-operator/releases/download/1.0.1/splunk-operator-noadmin.yaml
kubectl config set-context --current --namespace=<NAMESPACE>
kubectl apply -f splunk-operator.yaml
```


## Admin Installation for All Namespaces

_**Note:** The Admin Installation for all Namespaces is not functioning as intended with this release. We're tracking the issue [here](https://github.com/splunk/splunk-operator/issues/206). Check the status of the issue before attempting to use these instructions._

--------------------


If you want to configure a single instance of the operator to manage all the namespaces of your cluster, use the alternative cluster scope installation yaml file:

```
wget -O splunk-operator.yaml https://github.com/splunk/splunk-operator/releases/download/1.0.1/splunk-operator-cluster.yaml
```

When running at cluster scope, you will need to bind the `splunk:operator:namespace-manager` ClusterRole to the `splunk-operator` ServiceAccount in all namespaces you want it to manage. 

For example, to create a new namespace called `splunk` that is managed by Splunk Operator:

```yaml
cat <<EOF | kubectl apply -f -
---
apiVersion: v1
kind: Namespace
metadata:
  name: splunk
---
kind: RoleBinding
apiVersion: rbac.authorization.k8s.io/v1
metadata:
  name: splunk:operator:namespace-manager
  namespace: splunk
subjects:
- kind: ServiceAccount
  name: splunk-operator
  namespace: splunk-operator
roleRef:
  kind: ClusterRole
  name: splunk:operator:namespace-manager
  apiGroup: rbac.authorization.k8s.io
EOF
```


## Private Registries

If you plan to retag the container images as part of pushing it to a private registry, edit the image parameter in the  `splunk-operator` deployment to reference the appropriate image name.

```yaml
# Replace this with the built image name
image: splunk/splunk-operator
```

If you are using a private registry for the Docker images, edit the `RELATED_IMAGE_SPLUNK_ENTERPRISE` environment variables in `splunk-operator.yaml`.

```yaml
- name: RELATED_IMAGE_SPLUNK_ENTERPRISE
  value: "splunk/splunk:8.1.0" (or later)
```


## Cluster Domain

By default, the Splunk Operator will use a Kubernetes cluster domain of `cluster.local` to calculate the fully qualified domain names (FQDN) for each instance in your deployment. If you have configured a custom domain for your Kubernetes cluster, you can override the operator by adding a `CLUSTER_DOMAIN`
environment variable to the operator's deployment spec:

```yaml
- name: CLUSTER_DOMAIN
  value: "mydomain.com"
```
