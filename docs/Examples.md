# Configuring Splunk Enterprise Deployments

This document includes various examples for configuring Splunk Enterprise
deployments.

* [Creating a Clustered Deployment](#creating-a-clustered-deployment)
* [Creating a Cluster with Data Fabric Search (DFS)](#creating-a-cluster-with-data-fabric-search-(dfs))
* [Using Default Settings](#using-default-settings)
* [Installing Splunk Apps](#installing-splunk-apps)
* [Using Apps for Splunk Configuration](#using-apps-for-splunk-configuration)
* [Creating a LicenseMaster Using a ConfigMap](#creating-a-licensemaster-using-a-configmap)

Please refer to the [Custom Resource Guide](CustomResources.md) for more
information about the custom resources that you can use with the Splunk
Operator.


## Creating a Clustered Deployment

The two basic building blocks of Splunk Enterprise are search heads and
indexers. A `Standalone` resource can be used to create a single instance
that can perform either, or both, of these roles.

```yaml
apiVersion: enterprise.splunk.com/v1alpha2
kind: Standalone
metadata:
  name: single
  finalizers:
  - enterprise.splunk.com/delete-pvc
```


### Indexer Clusters

When growing, customers will typically want to first expand by upgrading
to an [indexer cluster](https://docs.splunk.com/Documentation/Splunk/latest/Indexer/Aboutindexesandindexers).
The Splunk Operator makes creation of an indexer cluster as easy as creating an `Indexer` resource:

```yaml
cat <<EOF | kubectl apply -f -
apiVersion: enterprise.splunk.com/v1alpha2
kind: Indexer
metadata:
  name: example
  finalizers:
  - enterprise.splunk.com/delete-pvc
EOF
```

This will automatically configure a cluster master with a single indexer
peer.

```
$ kubectl get pods
NAME                               READY   STATUS    RESTARTS   AGE
splunk-example-cluster-master-0    0/1     Running   0          29s
splunk-example-indexer-0           0/1     Running   0          29s
splunk-operator-7c5599546c-wt4xl   1/1     Running   0          14h
```

If you want more indexers, just patch it to include a `replicas` parameter:

```yaml
cat <<EOF | kubectl apply -f -
apiVersion: enterprise.splunk.com/v1alpha2
kind: Indexer
metadata:
  name: example
  finalizers:
  - enterprise.splunk.com/delete-pvc
spec:
  replicas: 3
EOF
```

```
$ kubectl get pods
NAME                               READY   STATUS    RESTARTS   AGE
splunk-example-cluster-master-0    1/1     Running   0          14m
splunk-example-indexer-0           1/1     Running   0          14m
splunk-example-indexer-1           1/1     Running   0          70s
splunk-example-indexer-2           1/1     Running   0          70s
splunk-operator-7c5599546c-wt4xl   1/1     Running   0          14h
```

You can now easily scale your indexer cluster by just patching `replicas`.

```
$ kubectl patch indexer example --type=json -p '[{"op": "replace", "path": "/spec/replicas", "value": 5}]'
indexer.enterprise.splunk.com/example patched
```

For efficiency, note that you can use the following short names with `kubectl`:

* `indexer`: `idx`
* `searchhead`: `search` or `sh`
* `licensemaster`: `lm`

To create a standalone search head that uses your indexer cluster, all you
have to do is add an `indexerRef` parameter:

```yaml
cat <<EOF | kubectl apply -f -
apiVersion: enterprise.splunk.com/v1alpha2
kind: Standalone
metadata:
  name: single
  finalizers:
  - enterprise.splunk.com/delete-pvc
spec:
  indexerRef:
    name: example
EOF
```

### Search Head Clusters

To scale search performance and provide high availability, customers will
often want to deploy a [search head cluster](https://docs.splunk.com/Documentation/Splunk/latest/DistSearch/AboutSHC).
Similar to a `Standalone` search head, you can create a search head cluster
that uses your indexer cluster by just adding a new `SearchHead` resource
with an `indexerRef` parameter:

```yaml
cat <<EOF | kubectl apply -f -
apiVersion: enterprise.splunk.com/v1alpha2
kind: SearchHead
metadata:
  name: example
  finalizers:
  - enterprise.splunk.com/delete-pvc
spec:
  indexerRef:
    name: example
EOF
```

This will automatically create a deployer with 3 search heads clustered
together (search head clusters require a minimum of 3 members):

```
$ kubectl get pods
NAME                               READY   STATUS    RESTARTS   AGE
splunk-example-cluster-master-0    1/1     Running   0          53m
splunk-example-deployer-0          0/1     Running   0          29s
splunk-example-indexer-0           1/1     Running   0          53m
splunk-example-indexer-1           1/1     Running   0          40m
splunk-example-indexer-2           1/1     Running   0          40m
splunk-example-indexer-3           1/1     Running   0          37m
splunk-example-indexer-4           1/1     Running   0          37m
splunk-example-search-head-0       0/1     Running   0          29s
splunk-example-search-head-1       0/1     Running   0          29s
splunk-example-search-head-2       0/1     Running   0          29s
splunk-operator-7c5599546c-pmbc2   1/1     Running   0          12m
splunk-single-standalone-0         1/1     Running   0          11m
```

Similar to indexer clusters, you can easily scale search head clusters
by just patching the `replicas` parameter.


### Cluster Services

Note that the creation of `SearchHead` and `Indexer` resources also creates
corresponding Kubernetes services:

```
$ kubectl get svc
NAME                                    TYPE        CLUSTER-IP       EXTERNAL-IP   PORT(S)                                          AGE
splunk-example-cluster-master-service   ClusterIP   10.100.98.17     <none>        8000/TCP,8089/TCP                                55m
splunk-example-deployer-service         ClusterIP   10.100.43.240    <none>        8000/TCP,8089/TCP                                118s
splunk-example-indexer-headless         ClusterIP   None             <none>        8000/TCP,8088/TCP,8089/TCP,9997/TCP              55m
splunk-example-indexer-service          ClusterIP   10.100.192.73    <none>        8000/TCP,8088/TCP,8089/TCP,9997/TCP              55m
splunk-example-search-head-headless     ClusterIP   None             <none>        8000/TCP,8089/TCP,9000/TCP,17000/TCP,19000/TCP   118s
splunk-example-search-head-service      ClusterIP   10.100.37.53     <none>        8000/TCP,8089/TCP,9000/TCP,17000/TCP,19000/TCP   118s
splunk-operator-metrics                 ClusterIP   10.100.181.146   <none>        8383/TCP,8686/TCP                                11d
```

To login to your new Splunk Enterprise cluster, you can forward port 8000
to one of the search head pods, or use a load balancing service that is
automatically created for your deployment:

```
kubectl port-forward service/splunk-example-search-head-service 8000
```

Similar to other examples, the default admin password can be obtained
from the secrets it generated for your deployment:

```
kubectl get secret splunk-example-search-head-secrets -o jsonpath='{.data.password}' | base64 --decode
```

Please see [Configuring Ingress](Ingress.md) for guidance on making your
Splunk clusters accessible outside of Kubernetes.


### Creating a Cluster with Data Fabric Search (DFS)

Data Fabric Search (DFS) can easily be enabled on any `Standalone` or
`SearchHead` insteance. To use DFS, you must first create a Spark cluster
using the `Spark` resource:

```yaml
cat <<EOF | kubectl apply -f -
apiVersion: enterprise.splunk.com/v1alpha2
kind: Spark
metadata:
  name: example
spec:
  replicas: 3
EOF
```

Within seconds, this will provision a Spark master and 3 workers to use
with DFS. Similar to indexer clusters and search head clusters, you can
easily scale search head clusters by just patching the `replicas` parameter.

Once you have a Spark cluster created, you can enable DFS by just adding the
`sparkRef` parameter to any `Standalone` or `SearchHead` instances. For
example, to create an additional single instance search head with DFS enabled:

```yaml
cat <<EOF | kubectl apply -f -
apiVersion: enterprise.splunk.com/v1alpha2
kind: Standalone
metadata:
  name: dfsexample
  finalizers:
  - enterprise.splunk.com/delete-pvc
spec:
  sparkRef:
    name: example
EOF
```


### Cleaning Up

As these examples demonstrate, the Splunk Operator for Kubernetes makes it
easy to create and manage clustered deployments of Splunk Enterprise. Given
the reduced complexity, the comparable resource requirements from
leveraging containers, and the ability to easily start small and scale as
necessary, we recommend that you use clustered `Indexer` and `SearchHead`
resources in favor of `Standalone`, unless you have a specific reason not to.

To remove the resources created from this example, run

```
kubectl delete standalone dfsexample
kubectl delete standalone single
kubectl delete spark example
kubectl delete sh example
kubectl delete idx example
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

You can use the `volumes` and `defaultsUrl` parameters in the
configuration spec to have the Splunk Operator initialize
your deployment using these settings.

```yaml
apiVersion: "enterprise.splunk.com/v1alpha2"
kind: "Standalone"
metadata:
  name: "example"
spec:
  volumes:
    - name: defaults
      configMap:
        name: splunk-defaults
  defaultsUrl: /mnt/defaults/default.yml
```

`volumes` will mount the ConfigMap in all of your pods under the
`/mnt/licenses` directory.

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
apiVersion: "enterprise.splunk.com/v1alpha2"
kind: "Standalone"
metadata:
  name: "example"
spec:
  volumes:
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
`volumes` parameter. This may be used to reference either Kubernetes
ConfigMaps, Secrets or multi-read Volumes.

For example, let's say you want to store two of your apps (`app1.tgz` and
`app2.tgz`) in a ConfigMap named `splunk-apps`:

```
kubectl create configmap splunk-apps --from-file=app1.tgz --from-file=app2.tgz
```

You can have the Splunk Operator install these automatically using something
like the following:

```yaml
apiVersion: "enterprise.splunk.com/v1alpha2"
kind: "Standalone"
metadata:
  name: "example"
spec:
  volumes:
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


## Creating a LicenseMaster Using a ConfigMap

We recommend that you create a `LicenseMaster` instance to share a license
with all the components in your Splunk Enterprise deployment.

First, you can create a ConfigMap named `splunk-licenses` that includes
a license file named `enterprise.lic` by running:

```
kubectl create configmap splunk-licenses --from-file=enterprise.lic
```

You can create a `LicenseMaster` that references this license by
using the `volumes` and `licenseUrl` configuration parameters:
 
```yaml
apiVersion: enterprise.splunk.com/v1alpha2
kind: LicenseMaster
metadata:
  name: example
spec:
  volumes:
    - name: licenses
      configMap:
        name: splunk-licenses
  licenseUrl: /mnt/licenses/enterprise.lic
```

`volumes` will mount the ConfigMap in your `LicenseMaster` pod under the
`/mnt/licenses` directory, and `licenseUrl` will configure Splunk to use
the `enterprise.lic` file within it.

Note that `licenseUrl` may specify a local path or URL such as
"https://myco.com/enterprise.lic", and the `volumes` parameter can
be used to mount any type of [Kubernetes Volumes](https://kubernetes.io/docs/concepts/storage/volumes/).

Finally, configure all of your other Splunk Enterprise components to use
the `LicenseMaster` by adding `licenseMasterRef` to their spec:

```yaml
apiVersion: enterprise.splunk.com/v1alpha2
kind: Indexer
metadata:
  name: example
spec:
  licenseMasterRef:
    name: example
```
