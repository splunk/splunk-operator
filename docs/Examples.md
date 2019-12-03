# Configuring Splunk Enterprise Deployments

This document includes various examples for configuring Splunk Enterprise
deployments.

* [Creating a ConfigMap for Your License](#creating-a-configmap-for-your-license)
* [Creating a Clustered Deployment](#creating-a-clustered-deployment)
* [Creating a Cluster with Data Fabric Search (DFS)](#creating-a-cluster-with-data-fabric-search-(dfs))
* [Using Default Settings](#using-default-settings)
* [Installing Splunk Apps](#installing-splunk-apps)
* [Using Apps for Splunk Configuration](#using-apps-for-splunk-configuration)


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


## Using Default Settings

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


## Installing Splunk Apps

*Note that this requires using the Splunk Enterprise container version 8.0.1 or later*

The Splunk Operator can be used to automatically install apps for you by
including the `apps_location` parameter in your default settings. The value
may either be a comma-separated list of apps or a YAML list, with each app
referenced using a filesystem path or URL.

When using filesystem paths, the apps should be mounted using the
`splunkVolumes` parameter. This may be used to reference either Kubernetes
ConfigMaps or multi-read Volumes.

For example, let's say you want to store two of your apps (`app1.tgz` and
`app2.tgz`) in a ConfigMap named `splunk-apps`:

```
kubectl create configmap splunk-apps --from-file=app1.tgz --from-file=app2.tgz
```

You can have the Splunk Operator install these automatically using something
like the following:

```yaml
apiVersion: "enterprise.splunk.com/v1alpha1"
kind: "SplunkEnterprise"
metadata:
  name: "example"
spec:
  splunkVolumes:
    - name: apps
      configMap:
        name: splunk-apps
  defaults: |-
    splunk:
      apps_location:
        - "/mnt/apps/app1.tgz"
        - "/mnt/apps/app2.tgz"
```

If you are using a search head cluster, the deployer will be used to push
these apps out to your search heads.

Instead of using a YAML list, you could also have used a comma-separated list:

```yaml
  defaults: |-
    splunk:
      apps_location: "/mnt/apps/app1.tgz,/mnt/apps/app2.tgz"
```

You can also install apps hosted remotely using URLs:

```yaml
    splunk:
      apps_location:
        - "/mnt/apps/app1.tgz"
        - "/mnt/apps/app2.tgz"
        - "https://example.com/splunk-apps/app3.tgz"
```


## Using Apps for Splunk Configuration

Splunk Enterprise apps are often used to package custom configuration files.
An app in its simplest form needs only to provide a `default/app.conf` file.
To create a new app, first create a directory containing a `default`
subdirectory. For example, let's create a simple app in a directory named
`myapp`:

```
mkdir -p myapp/default && cat <<EOF > myapp/default/app.conf
[install]
is_configured = 0

[ui]
is_visible = 1
label = My Splunk App

[launcher]
author = Me
description = My Splunk App for Custom Configuration
version = 1.0
EOF
```

Next, we'll add a few event type knowledge objects (from
[docs](https://docs.splunk.com/Documentation/Splunk/latest/Knowledge/Configureeventtypes)):

```
cat <<EOF > myapp/default/eventtypes.conf
[web]
search = html OR http OR https OR css OR htm OR html OR shtml OR xls OR cgi

[fatal]
search = FATAL
EOF
```

Splunk apps are typically packaged into gzip'd tarballs:

```
tar cvzf myapp.tgz myapp
```

You now have your custom knowledge objects configuration packaged into an app
that can be automatically deployed to your Splunk Enterprise clusters by
following instructions from the [previous example](#installing-splunk-apps).
