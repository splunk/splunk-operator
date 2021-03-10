# Configuring Splunk Enterprise Deployments

This document includes various examples for configuring Splunk Enterprise
deployments.


  - [Creating a Clustered Deployment](#creating-a-clustered-deployment)
    - [Indexer Clusters](#indexer-clusters)
      - [Cluster Master](#cluster-master)
      - [Indexer part](#indexer-part)
    - [Search Head Clusters](#search-head-clusters)
    - [Cluster Services](#cluster-services)
    - [Creating a Cluster with Data Fabric Search (DFS)](#creating-a-cluster-with-data-fabric-search-dfs)
    - [Cleaning Up](#cleaning-up)
  - [Smartstore Index Management](#smartstore-index-management)
  - [Using Default Settings](#using-default-settings)
  - [Installing Splunk Apps](#installing-splunk-apps)
  - [Using Apps for Splunk Configuration](#using-apps-for-splunk-configuration)
  - [Creating a LicenseMaster Using a ConfigMap](#creating-a-licensemaster-using-a-configmap)
  - [Using an External License Master](#using-an-external-license-master)
  - [Using an External Indexer Cluster](#using-an-external-indexer-cluster)
  - [Managing global kubernetes secret object](#managing-global-kubernetes-secret-object)
    - [Creating global kubernetes secret object](#creating-global-kubernetes-secret-object)
    - [Reading global kubernetes secret object](#reading-global-kubernetes-secret-object)
    - [Updating global kubernetes secret object](#updating-global-kubernetes-secret-object)
    - [Deleting global kubernetes secret object](#deleting-global-kubernetes-secret-object)

Please refer to the [Custom Resource Guide](CustomResources.md) for more
information about the custom resources that you can use with the Splunk
Operator.


## Creating a Clustered Deployment

The two basic building blocks of Splunk Enterprise are search heads and
indexers. A `Standalone` resource can be used to create a single instance
that can perform either, or both, of these roles.

```yaml
apiVersion: enterprise.splunk.com/v1
kind: Standalone
metadata:
  name: single
  finalizers:
  - enterprise.splunk.com/delete-pvc
```

The passwords for the instance are generated automatically. To review the passwords, please refer to the [Reading global kubernetes secret object](#reading-global-kubernetes-secret-object) instructions.

### Indexer Clusters

When growing, customers will typically want to first expand by upgrading
to an [indexer cluster](https://docs.splunk.com/Documentation/Splunk/latest/Indexer/Aboutindexesandindexers).
The Splunk Operator makes creation of an indexer cluster as easy as creating a `ClusterMaster` resource for Cluster Master and an `IndexerCluster` resource for indexers part respectively:

#### Cluster Master
```yaml
cat <<EOF | kubectl apply -f -
apiVersion: enterprise.splunk.com/v1
kind: ClusterMaster
metadata:
  name: cm
  finalizers:
  - enterprise.splunk.com/delete-pvc
EOF
```
#### Indexer part
```yaml
cat <<EOF | kubectl apply -f -
apiVersion: enterprise.splunk.com/v1
kind: IndexerCluster
metadata:
  name: example
  finalizers:
  - enterprise.splunk.com/delete-pvc
spec:
  clusterMasterRef:
    name: cm
EOF
```

This will automatically configure a cluster with RF(replication_factor) number of indexer peers.

NOTE: Whenever we try to specify `replicas` on IndexerCluster CR less than RF(as set on ClusterMaster),
the operator will always scale the number of peers to either `replication_factor`(in case of single site indexer cluster)
or to `origin` count in `site_replication_factor`(in case of multi-site indexer cluster).

```
$ kubectl get pods
NAME                                       READY   STATUS    RESTARTS    AGE
splunk-cm-cluster-master-0                  1/1     Running   0          29s
splunk-default-monitoring-console-0         1/1     Running   0          15s
splunk-example-indexer-0                    1/1     Running   0          29s
splunk-example-indexer-1                    1/1     Running   0          29s
splunk-example-indexer-2                    1/1     Running   0          29s
splunk-operator-7c5599546c-wt4xl            1/1     Running   0          14h
```
Notes: 
- The monitoring console pod is automatically created and pre-configured per namespace
- The name of the monitoring console pod is of the format splunk-`namespace`-monitoring-console-0

If you want more indexers, just update it to include a `replicas` parameter:

```yaml
cat <<EOF | kubectl apply -f -
apiVersion: enterprise.splunk.com/v1
kind: IndexerCluster
metadata:
  name: example
  finalizers:
  - enterprise.splunk.com/delete-pvc
spec:
  spec:
  clusterMasterRef:
    name: cm
  replicas: 3
EOF
```

```
$ kubectl get pods
NAME                                         READY    STATUS    RESTARTS   AGE
splunk-cm-cluster-master-0                    1/1     Running   0          14m
splunk-default-monitoring-console-0           1/1     Running   0          13m
splunk-example-indexer-0                      1/1     Running   0          14m
splunk-example-indexer-1                      1/1     Running   0          70s
splunk-example-indexer-2                      1/1     Running   0          70s
splunk-operator-7c5599546c-wt4xl              1/1     Running   0          14h
```

You can now easily scale your indexer cluster by just patching `replicas`.

```
$ kubectl patch indexercluster example --type=json -p '[{"op": "replace", "path": "/spec/replicas", "value": 5}]'
indexercluster.enterprise.splunk.com/example patched
```

For efficiency, note that you can use the following short names with `kubectl`:

* `clustermaster`: `cm-idxc`
* `indexercluster`: `idc` or `idxc`
* `searchheadcluster`: `shc`
* `licensemaster`: `lm`

Even better, all the custom resources with a `replicas` field also support
using the `kubectl scale` command:

```
$ kubectl scale idc example --replicas=5
indexercluster.enterprise.splunk.com/example scaled
```

You can also create [Horizontal Pod Autoscalers](https://kubernetes.io/docs/tasks/run-application/horizontal-pod-autoscale/)
to manage scaling for you. For example:

```yaml
cat <<EOF | kubectl apply -f -
apiVersion: autoscaling/v1
kind: HorizontalPodAutoscaler
metadata:
  name: idc-example
spec:
  scaleTargetRef:
    apiVersion: enterprise.splunk.com/v1
    kind: IndexerCluster
    name: example
  minReplicas: 5
  maxReplicas: 10
  targetCPUUtilizationPercentage: 50
EOF
```

```
$ kubectl get hpa
NAME          REFERENCE                TARGETS   MINPODS   MAXPODS   REPLICAS   AGE
idc-example   IndexerCluster/example   16%/50%   5         10        5          15m
```

To create a standalone search head that uses your indexer cluster, all you
have to do is add an `clusterMasterRef` parameter:

```yaml
cat <<EOF | kubectl apply -f -
apiVersion: enterprise.splunk.com/v1
kind: Standalone
metadata:
  name: single
  finalizers:
  - enterprise.splunk.com/delete-pvc
spec:
  clusterMasterRef:
    name: cm
EOF
```

The important parameter to note here is the `clusterMasterRef` field which points to the cluster master of the indexer cluster.
Having a separate CR for cluster master gives us the control to define a size or StorageClass for the PersistentVolumes of the cluster master
different from the indexers:

```yaml
cat <<EOF | kubectl apply -f -
apiVersion: enterprise.splunk.com/v1
kind: ClusterMaster
metadata:
  name: cm
  finalizers:
  - enterprise.splunk.com/delete-pvc
spec:
  etcVolumeStorageConfig:
    storageClassName: gp2
    storageCapacity: 15Gi
  varVolumeStorageConfig:
    storageClassName: customStorageClass
    storageCapacity: 25Gi
---
apiVersion: enterprise.splunk.com/v1
kind: IndexerCluster
metadata:
  name: idxc-part1
  finalizers:
  - enterprise.splunk.com/delete-pvc
spec:
  # No cluster-master created, uses the referenced one
  clusterMasterRef:
    name: cm
  replicas: 3
  storageClassName: local
  varStorage: "128Gi"
EOF
```

In the above environment, cluster master controls the [applications loaded](#installing-splunk-apps) to all
the parts of the indexer cluster, and the indexer services that it creates select the indexers
deployed by all the IndexerCluster parts, while the indexer services created by indexer cluster only select the indexers that it manages.

This can also allow to better control
the upgrade cycle to respect the recommended order: cluster master, then search heads,
then indexers, by defining and updating the docker image used by each IndexerCluster part.

This solution can also be used to build a [multisite cluster](MultisiteExamples.md)
by defining a different zone affinity and site in each child IndexerCluster resource.

The passwords for the instance are generated automatically. To review the passwords, please refer to the [Reading global kubernetes secret object](#reading-global-kubernetes-secret-object) instructions.

### Search Head Clusters

To scale search performance and provide high availability, customers will
often want to deploy a [search head cluster](https://docs.splunk.com/Documentation/Splunk/latest/DistSearch/AboutSHC).
Similar to a `Standalone` search head, you can create a search head cluster
that uses your indexer cluster by just adding a new `SearchHeadCluster` resource
with an `clusterMasterRef` parameter pointing to the cluster master we created in the above steps:

```yaml
cat <<EOF | kubectl apply -f -
apiVersion: enterprise.splunk.com/v1
kind: SearchHeadCluster
metadata:
  name: example
  finalizers:
  - enterprise.splunk.com/delete-pvc
spec:
  clusterMasterRef:
    name: cm
EOF
```

This will automatically create a deployer with 3 search heads clustered
together (search head clusters require a minimum of 3 members):

```
$ kubectl get pods
NAME                                        READY   STATUS    RESTARTS   AGE
splunk-cm-cluster-master-0                   1/1     Running   0          53m
splunk-default-monitoring-console-0          0/1     Running   0          52m
splunk-example-deployer-0                    0/1     Running   0          29s
splunk-example-indexer-0                     1/1     Running   0          53m
splunk-example-indexer-1                     1/1     Running   0          40m
splunk-example-indexer-2                     1/1     Running   0          40m
splunk-example-indexer-3                     1/1     Running   0          37m
splunk-example-indexer-4                     1/1     Running   0          37m
splunk-example-search-head-0                 0/1     Running   0          29s
splunk-example-search-head-1                 0/1     Running   0          29s
splunk-example-search-head-2                 0/1     Running   0          29s
splunk-operator-7c5599546c-pmbc2             1/1     Running   0          12m
splunk-single-standalone-0                   1/1     Running   0          11m
```

Similar to indexer clusters, you can easily scale search head clusters
by just patching the `replicas` parameter.

The passwords for the instance are generated automatically. To review the passwords, please refer to the [Reading global kubernetes secret object](#reading-global-kubernetes-secret-object) instructions

### Cluster Services

Note that the creation of `SearchHeadCluster`, `ClusterMaster` and `IndexerCluster`
resources also creates corresponding Kubernetes services:

```
$ kubectl get svc
NAME                                                        TYPE        CLUSTER-IP       EXTERNAL-IP   PORT(S)                                          AGE
splunk-cm-cluster-master-service                            ClusterIP   10.100.98.17     <none>        8000/TCP,8089/TCP                                55m
splunk-cm-indexer-service                                   ClusterIP   10.100.119.27    <none>        8000/TCP,8089/TCP                                55m
service/splunk-default-monitoring-console-headless          ClusterIP   None             <none>        8000/TCP,8088/TCP,8089/TCP,9997/TCP              54m
service/splunk-default-monitoring-console-service           ClusterIP   10.100.7.28      <none>        8000/TCP,8088/TCP,8089/TCP,9997/TCP              54m
splunk-example-deployer-service                             ClusterIP   10.100.43.240    <none>        8000/TCP,8089/TCP                                118s
splunk-example-indexer-headless                             ClusterIP   None             <none>        8000/TCP,8088/TCP,8089/TCP,9997/TCP              55m
splunk-example-indexer-service                              ClusterIP   10.100.192.73    <none>        8000/TCP,8088/TCP,8089/TCP,9997/TCP              55m
splunk-example-search-head-headless                         ClusterIP   None             <none>        8000/TCP,8089/TCP,9000/TCP,17000/TCP,19000/TCP   118s
splunk-example-search-head-service                          ClusterIP   10.100.37.53     <none>        8000/TCP,8089/TCP,9000/TCP,17000/TCP,19000/TCP   118s
splunk-operator-metrics                                     ClusterIP   10.100.181.146   <none>        8383/TCP,8686/TCP                                11d
```

To login to your new Splunk Enterprise cluster, you can forward port 8000
to one of the search head pods, or use a load balancing service that is
automatically created for your deployment:

```
kubectl port-forward service/splunk-example-search-head-service 8000
```

Similar to other examples, the default administrator password can be obtained
from the global kubernetes secrets object as described here:

```
kubectl get secret splunk-`<namespace`>-secret -o jsonpath='{.data.password}' | base64 --decode
```

Please see [Configuring Ingress](Ingress.md) for guidance on making your
Splunk clusters accessible outside of Kubernetes.

### Creating a Cluster with Data Fabric Search (DFS)

Data Fabric Search (DFS) can easily be enabled on any `Standalone` or
`SearchHeadCluster` insteance. To use DFS, you must first create a Spark
cluster using the `Spark` resource:

```yaml
cat <<EOF | kubectl apply -f -
apiVersion: enterprise.splunk.com/v1
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
`sparkRef` parameter to any `Standalone` or `SearchHeadCluster` instances. For
example, to create an additional single instance search head with DFS enabled:

```yaml
cat <<EOF | kubectl apply -f -
apiVersion: enterprise.splunk.com/v1
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

The passwords for the instance are generated automatically. To review the passwords, please refer to the [Reading global kubernetes secret object](#reading-global-kubernetes-secret-object) instructions

### Cleaning Up

As these examples demonstrate, the Splunk Operator for Kubernetes makes it
easy to create and manage clustered deployments of Splunk Enterprise. Given
the reduced complexity, the comparable resource requirements from
leveraging containers, and the ability to easily start small and scale as
necessary, we recommend that you use the `IndexerCluster` and `SearchHeadCluster`
resources in favor of `Standalone`, unless you have a specific reason not to.

To remove the resources created from this example, run

```
kubectl delete standalone dfsexample
kubectl delete standalone single
kubectl delete spark example
kubectl delete shc example
kubectl delete idc example
kubectl delete clustermaster cm
```

## SmartStore Index Management

Indexes can be managed through the Splunk Operator. Every index configured through the Splunk Operator must be SmartStore enabled. For further details, see [SmartStore Resource Guide](SmartStore.md).

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
apiVersion: enterprise.splunk.com/v1
kind: Standalone
metadata:
  name: example
  finalizers:
  - enterprise.splunk.com/delete-pvc
spec:
  volumes:
    - name: defaults
      configMap:
        name: splunk-defaults
  defaultsUrl: /mnt/defaults/default.yml
```

In the above example, `volumes` will mount the `splunk-defaults` ConfigMap
with `default.yml` file under the `/mnt/defaults` directory on all pods of
the Custom Resource `Standalone`.

`defaultsUrl` represents the full path to the `default.yml` configuration
file on the pods. In addition, `defaultsUrl` may specify one or more local
paths or URLs, each separated by a comma. For example, you can use a `generic.yml`
with common settings and an `apps.yml` that provides additional parameters for
app installation.

```yaml
  defaultsUrl: "http://myco.com/splunk/generic.yml,/mnt/defaults/apps.yml"
```

Inline `defaults` are always processed last, after any `defaultsUrl` files.

Any password management related configuration via `defaults` and `defaultsUrl`
has been disabled. Please review [`PasswordManagement.md`](PasswordManagement.md)
and [`Managing global kubernetes secret object`](#managing-global-kubernetes-secret-object)
for more details.

## Installing Splunk Apps

*Note that this requires using the Splunk Enterprise container version 8.1.0 or later*

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
apiVersion: enterprise.splunk.com/v1
kind: Standalone
metadata:
  name: example
  finalizers:
  - enterprise.splunk.com/delete-pvc
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

Also these application configuration parameters can be placed in a `defaults.yml`
file and use the `defaultsUrlApps` parameter.  The `defaultsUrlApps` parameter
is specific for applicastion installation and will install the apps in the
correct instances as per the deployment.

Unlike `defaultsUrl` which is applied at every instance created by the CR, the
`defaultsUrlApps` will be applied on instances that will **not** get the application
installed via a bundle push.  Search head and indexer cluster members will not have
the `defaultsUrlApps` parameter applied.  This means:

 - For Standalone & License Master, these applications will be installed as normal.
 - For SearchHeadClusters, these applications will only be installed on the SHC Deployer
and pushed to the members via SH Bundle push.
 - For IndexerClusters, these applications will only be installed on the ClusterMaster
and pushed to the indexers in the cluster via CM Bundle push.

For application installation the preferred method will be through the `defaultsUrlApps`
while other ansible defaults can be still be installed via `defaultsUrl`.  For backwards
compatibility applications could be installed via `defaultsUrl` though this is not
recommended.  Both options can be used in conjunction:

```yaml
    defaultsUrl : "http://myco.com/splunk/generic.yml"
    defaultsUrlApps: "/mnt/defaults/apps.yml"
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
apiVersion: enterprise.splunk.com/v1
kind: LicenseMaster
metadata:
  name: example
  finalizers:
  - enterprise.splunk.com/delete-pvc
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
apiVersion: enterprise.splunk.com/v1
kind: IndexerCluster
metadata:
  name: example
  finalizers:
  - enterprise.splunk.com/delete-pvc
spec:
  licenseMasterRef:
    name: example
```


## Using an External License Master

*Note that this requires using the Splunk Enterprise container version 8.1.0 or later*

The Splunk Operator for Kubernetes allows you to use an external License
Master(LM) with the custom resources it manages. To do this, you will
share the same `pass4Symmkey` between the global secret object setup by the
operator & the external LM, and configure the `splunk.license_master_url`.
The operator requires that the external LM have a configured
pass4SymmKey for authentication.

### Configuring pass4Symmkey:

There are two ways to configure `pass4Symmkey` with an External LM:

#### Approach 1
- Setup the desired plain-text [`pass4Symmkey`](PasswordManagement.md#pass4Symmkey) in the global secret object(Note: The `pass4Symmkey` would be stored in a base64 encoded format). For details see [updating global kubernetes secret object](#updating-global-kubernetes-secret-object).
- Setup the same plain-text `pass4SymmKey` in the `[general]` section of your LM's `server.conf` file.

#### Approach 2
- Retrieve the plain-text `pass4SymmKey` in the `[general]` section of your LM's `server.conf` file.
  ```
  cat $SPLUNK_HOME/etc/system/local/server.conf
  ...
  [general]
  pass4SymmKey = $7$Sw0A+wvJdTztMcA2Ge7u435XmpTzPqyaq49kUZqn0yfAgwFpwrArM2JjWJ3mUyf/FyHAnCZkE/U=
  ...
  ```

  You can decrypt the `pass4SymmKey` by running the following command with
  `--value` set to the value from your `server.conf` file:

  ```
  $SPLUNK_HOME/bin/splunk show-decrypted --value '$7$Sw0A+wvJdTztMcA2Ge7u435XmpTzPqyaq49kUZqn0yfAgwFpwrArM2JjWJ3mUyf/FyHAnCZkE/U='
  ```
- Setup the above decrypted plain-text [`pass4Symmkey`](PasswordManagement.md#pass4Symmkey) in the global secret object(Note: The `pass4Symmkey` would be stored in a base64 encoded format). For details see [updating global kubernetes secret object](#updating-global-kubernetes-secret-object)

### Configuring license_master_url:

Assuming that the hostname for your LM is `license-master.splunk.mydomain.com`,
you should create a `default.yml` file with the following contents:

```yaml
splunk:
  license_master_url: license-master.splunk.mydomain.com
```

Next, save this file as a secret. In this example we are calling it `splunk-license-master`:

```
kubectl create secret generic splunk-license-master --from-file=default.yml
```

You can then use the `defaultsUrl` parameter and a reference to the secret object created above to configure any Splunk
Enterprise custom resource to use your External LM:

```yaml
apiVersion: enterprise.splunk.com/v1
kind: Standalone
metadata:
  name: example
  finalizers:
  - enterprise.splunk.com/delete-pvc
spec:
  volumes:
    - name: license-master
      secret:
        secretName: splunk-license-master
  defaultsUrl: /mnt/license-master/default.yml
```

## Using an External Indexer Cluster

*Note that this requires using the Splunk Enterprise container version 8.1.0 or later*

The Splunk Operator for Kubernetes allows you to use an external cluster of
indexers with its `Standalone`, `SearchHeadCluster` and `LicenseMaster`
resources. To do this, you will share the same `IDXC pass4Symmkey` between the global secret object setup by the
operator & the external indexer cluster, and configure the `splunk.cluster_master_url`.


### Configuring IDXC pass4Symmkey:

There are two ways to configure `IDXC pass4Symmkey` with an External Indexer Cluster:
#### Approach 1
- Setup the desired plain-text [`IDXC pass4Symmkey`](PasswordManagement.md#idxc-pass4Symmkey) in the global secret object(Note: The `IDXC pass4Symmkey` would be stored in a base64 encoded format). For details see [updating global kubernetes secret object](#updating-global-kubernetes-secret-object).
- Setup the same plain-text `IDXC pass4SymmKey` in the `[clustering]` section of your cluster master's and indexers' `server.conf` file.

#### Approach 2
- Retrieve the plain-text `IDXC pass4SymmKey` in the `[clustering]` section of your cluster master's `server.conf` file.
  ```
  cat $SPLUNK_HOME/etc/system/local/server.conf
  ...
  [clustering]
  pass4SymmKey = $7$Sw0A+wvJdTztMcA2Ge7u435XmpTzPqyaq49kUZqn0yfAgwFpwrArM2JjWJ3mUyf/FyHAnCZkE/U=
  ...
  ```

  You can decrypt the `IDXC pass4SymmKey` by running the following command with
  `--value` set to the value from your `server.conf` file:

  ```
  $SPLUNK_HOME/bin/splunk show-decrypted --value '$7$Sw0A+wvJdTztMcA2Ge7u435XmpTzPqyaq49kUZqn0yfAgwFpwrArM2JjWJ3mUyf/FyHAnCZkE/U='
  ```
- Setup the above decrypted plain-text [`IDXC pass4Symmkey`](PasswordManagement.md#idxc-pass4Symmkey) in the global secret object(Note: The `IDXC pass4Symmkey` would be stored in a base64 encoded format). For details see [updating global kubernetes secret object](#updating-global-kubernetes-secret-object)

### Configuring cluster_master_url:

Assuming the hostname for your cluster master is `cluster-master.splunk.mydomain.com`,
you should create a `default.yml` file with the following contents:

```yaml
splunk:
  cluster_master_url: cluster-master.splunk.mydomain.com
```

Next, save this file as a secret. In the example here, it is called `splunk-cluster-master`:

```
kubectl create secret generic splunk-cluster-master --from-file=default.yml
```

You can then use the `defaultsUrl` parameter and a reference to the secret created above to configure any Splunk
Enterprise custom resource to use your external indexer cluster:

```yaml
apiVersion: enterprise.splunk.com/v1
kind: SearchHeadCluster
metadata:
  name: example
  finalizers:
  - enterprise.splunk.com/delete-pvc
spec:
  volumes:
    - name: cluster-master
      secret:
        secretName: splunk-cluster-master
  defaultsUrl: /mnt/cluster-master/default.yml
```
## Managing global kubernetes secret object

### Creating global kubernetes secret object

Use the kubectl command to create the global kubernetes secret object:

1. Verify the namespace. You can retrieve the namespace in the current context using `kubectl config view --minify --output 'jsonpath={..namespace}'`. Make a note of the output. If the command doesn't display an output it indicates that we are in the `default` namespace.  **NOTE**: If you already have a desired namespace, you can set current context to the same using the following command: `kubectl config set-context --current --namespace=<desired_namespace>`

2. Gather the password values for the secret tokens you want to configure. To see all available secret tokens defined for the global kubernetes secret object, review [password management](PasswordManagement.md#splunk-secret-tokens-in-the-global-secret-object)

3. Create a kubernetes secret object referencing the namespace. Example: splunk-`<desired_namespace`>-secret.
In the example below, we are creating the global kubernetes secret object, defining the default administrator and pass4Symmkey tokens, and passing in the values.  
`kubectl create secret generic splunk-<desired_namespace>-secret --from-literal='password=<admin_password_value>' --from-literal='pass4Symmkey=<pass4Symmkey_value>'`

### Reading global kubernetes secret object

Once created, all secret tokens in the secret object are base64 encoded. To read the global kubernetes secret object you can use the following command:

`kubectl get secret splunk-<desired_namespace>-secret -o yaml`

A sample global kubernetes secret object with base64 encoded values looks like:

```
kubectl get secret splunk-default-secret -o yaml
apiVersion: v1
data:
  hec_token: RUJFQTE4OTMtMDI4My03RkMzLThEQTAtQ0I1RTFGQzgzMzc1
  idxc_secret: VUY5dWpHU1I4ZmpoZlJKaWNNT2VMSUNY
  pass4SymmKey: dkFjelZSUzJjZzFWOHZPaVRGZk9hSnYy
  password: OHFqcnV5WFhHRFJXU1hveDdZMzY5MGRs
  shc_secret: ZEdHWG5Ob2dzTDhWNHlocDFiYWpiclo1
kind: Secret
metadata:
  creationTimestamp: "2020-10-07T19:42:07Z"
  name: splunk-default-secret
  namespace: default
  ownerReferences:
  - apiVersion: enterprise.splunk.com/v1beta1
    controller: false
    kind: SearchHeadCluster
    name: example-shc
    uid: f7264daf-4a3e-4b44-adb7-af52f45b45fe
  resourceVersion: "11433590"
  selfLink: /api/v1/namespaces/default/secrets/splunk-default-secret
  uid: d6c9a59c-1acf-4482-9990-cdb0eed56e87
type: Opaque
```

The kubectl command line tool can be used to decode the splunk secret tokens with the following command:

`kubectl get secret splunk-<desired_namespace>-secret -o go-template=' {{range $k,$v := .data}}{{printf "%s: " $k}}{{if not $v}}{{$v}}{{else}}{{$v | base64decode}}{{end}}{{"\n"}}{{end}}'`

A sample global kubernetes secret object with tokens decoded looks like:

```
hec_token: EBEA1893-0283-7FC3-8DA0-CB5E1FC83375
idxc_secret: UF9ujGSR8fjhfRJicMOeLICX
pass4SymmKey: vAczVRS2cg1V8vOiTFfOaJv2
password: 8qjruyXXGDRWSXox7Y3690dl
shc_secret: dGGXnNogsL8V4yhp1bajbrZ5
```

### Updating global kubernetes secret object

Use the kubectl command to update the global kubernetes secret object:

1. Base64 encode the plain-text value of the secret token using the following command: `echo -n <plain_text_value> | base64`
2. Obtain the key name for the secret token you are populating. The list of tokens is available in [password management](PasswordManagement.md#splunk-secret-tokens-in-the-global-secret-object). 
3. Update the global kubernetes secret object using the key and the encoded value: `kubectl patch secret splunk-<desired_namespace>-secret -p='{"data":{"<key_name_for_secret_token>": "<encoded_value>"}}' -v=1`

### Deleting global kubernetes secret object

Use the kubectl command to delete the global kubernetes secret object:

`kubectl delete secret splunk-<desired_namespace>-secret`
