# Splunk Operator Advanced Installation

Users of Red Hat OpenShift should read the additional
[Red Hat OpenShift](OpenShift.md) documentation.


## Downloading Installation YAML for modifications

If you need to make any custom modifications for installation of the
Splunk Operator (as described below), please download a local copy of
the installation YAML and open it in your favorite editor.

```
wget -O splunk-operator.yaml https://github.com/splunk/splunk-operator/releases/download/0.2.0/splunk-operator-install.yaml
```


## Installation By a Regular User

Kubernetes only allows administrators to install new `CustomResourceDefinition`
objects. All other objects included in the `splunk-operator.yaml` file can be
installed by regular users within their own namespaces. If you are not an
administrator, you can have someone else create these objects for you by running

```
kubectl apply -f https://github.com/splunk/splunk-operator/releases/download/0.2.0/splunk-operator-crds.yaml
```

You should then be able download and use the following YAML to install the
operator within your own namespace:

```
wget -O splunk-operator.yaml https://github.com/splunk/splunk-operator/releases/download/0.2.0/splunk-operator-noadmin.yaml
kubectl config set-context --current --namespace=<NAMESPACE>
kubectl apply -f splunk-operator.yaml
```


## Admin Installation for All Namespaces

If you wish to have a single instance of the operator managing Splunk
objects for all the namespaces of your cluster, you can use the alternative
cluster scope installation YAML:

```
wget -O splunk-operator.yaml https://github.com/splunk/splunk-operator/releases/download/0.2.0/splunk-operator-cluster.yaml
```

When running at cluster scope, you will need to bind the
`splunk:operator:namespace-manager` ClusterRole to the `splunk-operator`
ServiceAccount in all namespaces you would like it to manage. For example,
to create a new namespace called `splunk` that is managed by Splunk Operator:

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

*Note: The `splunk/splunk:8.1.0` image is rather large, so we strongly
recommend copying this to a private registry or directly onto your
Kubernetes workers as per the [Required Images Documentation](Images.md), and
following these instructions before creating any large Splunk deployments.*

If you retagged the Splunk Operator container image as part of pushing
it to a private registry, you will need to edit the image parameter in the 
`splunk-operator` deployment to reference the appropriate image name.

```yaml
# Replace this with the built image name
image: splunk/splunk-operator
```

If you are using a private registry for the `splunk/splunk:8.1.0` and
`splunk/spark` (used by DFS) images, you should modify the
`RELATED_IMAGE_SPLUNK_ENTERPRISE` and `RELATED_IMAGE_SPLUNK_SPARK`
environment variables in `splunk-operator.yaml` to point
to the appropriate locations.

```yaml
- name: RELATED_IMAGE_SPLUNK_ENTERPRISE
  value: "splunk/splunk:8.1.0"
- name: RELATED_IMAGE_SPLUNK_SPARK
  value: "splunk/spark"
```


## Cluster Domain

By default, the Splunk Operator will use a Kubernetes cluster domain of
`cluster.local` to calculate fully qualified domain names (FQDN) for each
instance in your deployments. If you have configured a custom domain for
your Kubernetes cluster, you can override this by adding a `CLUSTER_DOMAIN`
environment variable to the operator's deployment spec:

```yaml
- name: CLUSTER_DOMAIN
  value: "mydomain.com"
```

## External TLS for Splunkd and HEC

By default Splunk will enable SSL/TLS for splunkd (8089) and hec (8088). It can be desirable
to offload encryption between pods to a the network layer using CNI and externally using ingress 
or istio. The following environment variables can be added to the operator deployment to disable 
default SSL/TLS

```yaml
- name: SPLUNKD_SSL_ENABLE
  value: "false"
- name: SPLUNK_HEC_SSL
  value: "false"
```

## Installing Splunk Operator

You can install and start the operator by running

```
kubectl apply -f splunk-operator.yaml
```

After starting the operator, you should see a single pod running
within your namespace:

```
kubectl get pods
NAME                               READY   STATUS    RESTARTS   AGE
splunk-operator-75f5d4d85b-8pshn   1/1     Running   0          5s
```

To remove all Splunk deployments and completely uninstall the
Splunk Operator, run:

```
kubectl delete standalones --all
kubectl delete licensemasters --all
kubectl delete searchheadclusters --all
kubectl delete clustermasters --all
kubectl delete indexerclusters --all
kubectl delete spark --all
kubectl delete -f splunk-operator.yaml
```
