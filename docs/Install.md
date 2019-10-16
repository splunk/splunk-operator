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


## Private Registries

*Note: The `splunk/splunk:8.0` image is rather large, so we strongly
recommend copying this to a private registry or directly onto your
Kubernetes workers as per the [Air Gap Documentation](AirGap.md), and
following these instructions before creating any large Splunk deployments.*

If you retagged the Splunk Operator container image as part of pushing
it to a private registry, you will need to edit the image parameter in the 
`splunk-operator` Deployment to reference the appropriate image name.

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
apiVersion: rbac.authorization.k8s.io/v1beta1
kind: ClusterRole
metadata:
  labels:
    rbac.authorization.k8s.io/aggregate-to-admin: "true"
  name: splunk:splunk-enterprise-operator
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
