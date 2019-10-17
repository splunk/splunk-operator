# Configuring Splunk Enterprise Deployments

## Creating a ConfigMap for Your License

Many of the examples in this document require that you have a valid Splunk
Enterprise license.

You can create a ConfigMap named `splunk-licenses` that includes a license
file named `enterprise.lic` by running:

```
kubectl create configmap splunk-licenses --from-file=enterprise.lic
```

You can make this license available to your deployments by using the
`splunkVolumes` and `licenseUrl` parameters in your `SplunkEnterprise` spec:

```yaml
spec:
  splunkVolumes:
  - name: licenses
    configMap:
      name: splunk-licenses
  licenseUrl: /mnt/licenses/enterprise.lic
```

`splunkVolumes` will mount the ConfigMap in all of your Splunk Enterprise 
containers under the `/mnt/licenses` directory, and `licenseUrl` will
configure your deployment to use the `enterprise.lic` file within it.

Note that `licenseUrl` may specify a local path or URL such as
"https://myco.com/enterprise.lic", and the `splunkVolumes` parameter can
be used to mount any type of [Kubernetes Volumes](https://kubernetes.io/docs/concepts/storage/volumes/).


## Creating a Clustered Deployment

You can create a new cluster with 3 indexers and 3 search heads by running:

```yaml
cat <<EOF | kubectl apply -f -
apiVersion: enterprise.splunk.com/v1alpha1
kind: SplunkEnterprise
metadata:
  name: cluster
  finalizers:
  - enterprise.splunk.com/delete-pvc
spec:
  splunkVolumes:
  - name: licenses
    configMap:
      name: splunk-licenses
  licenseUrl: /mnt/licenses/enterprise.lic
  resources:
    splunkVarStorage: 10Gi
    splunkIndexerStorage: 50Gi
  topology:
    indexers: 3
    searchHeads: 3
EOF
```

*Note that this example also demonstrates overriding the default storage
resource allocations.*

Within a few minutes, you should have a fully configured cluster up and
ready to use:

```
$ kubectl get pods
NAME                               READY   STATUS    RESTARTS   AGE
splunk-cluster-cluster-master-0    1/1     Running   0          51s
splunk-cluster-deployer-0          1/1     Running   0          51s
splunk-cluster-indexer-0           1/1     Running   0          51s
splunk-cluster-indexer-1           1/1     Running   0          51s
splunk-cluster-indexer-2           1/1     Running   0          51s
splunk-cluster-license-master-0    1/1     Running   0          51s
splunk-cluster-search-head-0       1/1     Running   0          50s
splunk-cluster-search-head-1       1/1     Running   0          50s
splunk-cluster-search-head-2       1/1     Running   0          50s
splunk-operator-67596d99f4-vwm7r   1/1     Running   0          81m
```

To login you can forward port 8000 to one of the search heads, or use a load
balancing service that is automatically created for your deployment:

```
kubectl port-forward service/splunk-cluster-search-head-service 8000
```

Similar to other examples, the default admin password can be obtained
from the secrets it generated for your deployment:

```
kubectl get secret splunk-cluster-secrets -o jsonpath='{.data.password}' | base64 --decode
```

To delete your cluster, run

```
kubectl delete splunkenterprise/cluster
```


## Creating a Cluster with Data Fabric Search (DFS)

Building on the previous example, adding support for Data Fabric Search to your
cluster is as easy as adding an `enableDFS` parameter:

```yaml
cat <<EOF | kubectl apply -f -
apiVersion: enterprise.splunk.com/v1alpha1
kind: SplunkEnterprise
metadata:
  name: dfscluster
  finalizers:
  - enterprise.splunk.com/delete-pvc
spec:
  enableDFS: true
  splunkVolumes:
  - name: licenses
    configMap:
      name: splunk-licenses
  licenseUrl: /mnt/licenses/dfs.lic
  topology:
    indexers: 3
    searchHeads: 3
    sparkWorkers: 3
EOF
```

Within a few minutes, you should have a fully configured cluster up and
ready to use:

```
$ kubectl get pods
NAME                                              READY   STATUS    RESTARTS   AGE
splunk-dfscluster-cluster-master-0                1/1     Running   0          31s
splunk-dfscluster-deployer-0                      1/1     Running   0          31s
splunk-dfscluster-indexer-0                       1/1     Running   0          30s
splunk-dfscluster-indexer-1                       1/1     Running   0          30s
splunk-dfscluster-indexer-2                       1/1     Running   0          30s
splunk-dfscluster-license-master-0                1/1     Running   0          31s
splunk-dfscluster-search-head-0                   1/1     Running   0          29s
splunk-dfscluster-search-head-1                   1/1     Running   0          29s
splunk-dfscluster-search-head-2                   1/1     Running   0          29s
splunk-dfscluster-spark-master-856bcb8dcb-4szms   1/1     Running   0          31s
splunk-dfscluster-spark-worker-0                  1/1     Running   0          31s
splunk-dfscluster-spark-worker-1                  1/1     Running   0          31s
splunk-dfscluster-spark-worker-2                  1/1     Running   0          31s
splunk-operator-7bcdd5bb54-v8vtb                  1/1     Running   0          16d
```

To login, you can forward port 8000 to one of the search heads, or use a load balancing service that is automatically created for your deployment:

```
kubectl port-forward service/splunk-dfscluster-search-head-service 8000
```

Similar to the previous example, the default admin password can be obtained from the secrets it generated for your deployment:

```
kubectl get secret splunk-dfscluster-secrets -o jsonpath='{.data.password}' | base64 --decode
```

To delete your cluster, run

```
kubectl delete splunkenterprise/dfscluster
```


## Default Settings

The Splunk Enterprise container supports many
[default configuration settings](https://github.com/splunk/splunk-ansible/blob/develop/docs/advanced/default.yml.spec.md)
which are used to set up and configure new deployments. The Splunk Operator
provides several ways to configure these.

Suppose we create a ConfigMap named `splunk-defaults` that includes a
`default.yml` in our kubernetes cluster:

```
kubectl create configmap splunk-defaults --from-file=default.yml
```

Similar to [license files](#creating-a-configmap-for-your-license), you
can use the `splunkVolumes` and `defaultsUrl` parameters in the
`SplunkEnterprise` spec to have the Splunk Operator initialize
your deployment using these settings.

```yaml
apiVersion: "enterprise.splunk.com/v1alpha1"
kind: "SplunkEnterprise"
metadata:
  name: "example"
spec:
  splunkVolumes:
    - name: defaults
      configMap:
        name: splunk-defaults
  defaultsUrl: /mnt/defaults/default.yml
```

`defaultsUrl` may specify one or more local paths or URLs, each separated
by a comma. For example, you can use a `generic.yml` with common
settings and an `apps.yml` that provides additional parameters for app
installation.

```yaml
  defaultsUrl: "http://myco.com/splunk/generic.yml,/mnt/defaults/apps.yml"
```

Suppose you want to just override the admin password for your deployment
(instead of using the automatically generated one), you can also specify
inline overrides using the `defaults` parameter:

```yaml
apiVersion: "enterprise.splunk.com/v1alpha1"
kind: "SplunkEnterprise"
metadata:
  name: "example"
spec:
  splunkVolumes:
    - name: defaults
      configMap:
        name: splunk-defaults
  defaultsUrl: /mnt/defaults/default.yml
  defaults: |-
    splunk:
      password: helloworld456
```

*Setting passwords in your CRDs may be OK for testing, but it is discouraged.*

Inline `defaults` are always processed last, after any `defaultsUrl` files.
