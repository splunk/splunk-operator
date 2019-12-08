# Splunk Operator Advanced Installation

Users of Red Hat OpenShift should read the additional
[Red Hat OpenShift](OpenShift.md) documentation.


## Downloading Installation YAML for modifications

If you need to make any custom modifications for installation of the
Splunk Operator (as described below), please download a local copy of
the installation YAML and open it in your favorite editor.

```
wget -O splunk-operator.yaml https://tiny.cc/splunk-operator-install
```


## Installation for Multiple Namespaces

If you wish to have a single instance of the operator managing Splunk
objects for all the namespaces of your cluster, you can use the alternative
cluster scope installation YAML:

```
wget -O splunk-operator.yaml https://tiny.cc/splunk-operator-cluster-install
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

*Note: The `splunk/splunk:8.0` image is rather large, so we strongly
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

If you are using a private registry for the `splunk/splunk:8.0` and
`splunk/spark` (used by DFS) images, you should modify the `SPLUNK_IMAGE`
and `SPARK_IMAGE` environment variables in `splunk-operator.yaml` to point
to the appropriate locations.

```yaml
- name: SPLUNK_IMAGE
  value: "splunk/splunk:8.0"
- name: SPARK_IMAGE
  value: "splunk/spark"
```


## For Kubernetes Administrators

If you are an administrator of your Kubernetes cluster, you can install and
start the operator by running

```
kubectl apply -f splunk-operator.yaml
```


## For Other Kubernetes Users (Not Administrators)

Please note that Kubernetes only allows administrators to install new
`CustomResourceDefinition` and `ClusterRole` objects. All other objects
included in the `splunk-operator.yaml` file can be installed by regular users
within their own namespaces. If you are not an administrator, you can have
someone else create these objects for you by running

```yaml
cat <<EOF | kubectl apply -f -
---
apiVersion: apiextensions.k8s.io/v1beta1
kind: CustomResourceDefinition
metadata:
  labels:
    k8s-app: splunk-kubecontroller
  name: splunkenterprises.enterprise.splunk.com
spec:
  group: enterprise.splunk.com
  names:
    kind: SplunkEnterprise
    listKind: SplunkEnterpriseList
    plural: splunkenterprises
    shortNames:
    - enterprise
    singular: splunkenterprise
  scope: Namespaced
  version: v1alpha1
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: splunk:operator:namespace-manager
rules:
- apiGroups:
  - ""
  resources:
  - services
  - endpoints
  - persistentvolumeclaims
  - configmaps
  - secrets
  - events
  verbs:
  - create
  - delete
  - deletecollection
  - get
  - list
  - patch
  - update
  - watch
- apiGroups:
  - apps
  resources:
  - deployments
  - daemonsets
  - replicasets
  - statefulsets
  verbs:
  - create
  - delete
  - deletecollection
  - get
  - list
  - patch
  - update
  - watch
- apiGroups:
  - enterprise.splunk.com
  resources:
  - '*'
  verbs:
  - '*'
---
apiVersion: rbac.authorization.k8s.io/v1beta1
kind: ClusterRole
metadata:
  labels:
    rbac.authorization.k8s.io/aggregate-to-admin: "true"
  name: splunk:operator:resource-manager
rules:
  - apiGroups:
      - enterprise.splunk.com
    resources:
      - '*'
    verbs:
      - '*'
EOF
```

After removing the above content from the beginning of your 
`splunk-operator.yaml` file, you should be able to install the Splunk 
Operator within your own namespace by running

```
kubectl config set-context --current --namespace=<NAMESPACE>
kubectl apply -f splunk-operator.yaml
```


## After Installation

After starting the Splunk Operator, you should see a single pod running
within your namespace:

```
kubectl get pods
NAME                               READY   STATUS    RESTARTS   AGE
splunk-operator-75f5d4d85b-8pshn   1/1     Running   0          5s
```

To remove all Splunk deployments and completely uninstall the
Splunk Operator, run:

```
kubectl delete splunkenterprises --all
kubectl delete -f splunk-operator.yaml
```
