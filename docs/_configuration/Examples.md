---
title: Example Deployments
nav_order: 28.1
#nav_exclude: true
---

# Example Splunk Enterprise Deployments
{: .no_toc }
This document includes various examples for configuring Splunk Enterprise deployments with the Splunk Operator.
For more information about the custom resources that you can use with the Splunk Operator, refer to the [Custom Resource Reference](/configuration/CustomResources).

#### Table of contents
{: .no_toc }
- TOC
{:toc}

## Creating a Standalone Deployment

The two basic building blocks of Splunk Enterprise infrastructure are search heads and indexers. A `Standalone` resource can be used to create a single instance that can perform either, or both of these roles.

```yaml
apiVersion: enterprise.splunk.com/v4
kind: Standalone
metadata:
  name: single
  finalizers:
  - enterprise.splunk.com/delete-pvc
```

The passwords for the instance are generated automatically. To review the passwords, please refer to the [Reading global kubernetes secret object](#reading-global-kubernetes-secret-object) instructions.

## Creating a Clustered Deployment

### Indexer Clusters

When customers outgrow the capabilites of single instance for indexing and search, they will scale the infrastructure up to an [indexer cluster](https://docs.splunk.com/Documentation/Splunk/latest/Indexer/Aboutindexesandindexers).
The Splunk Operator makes creation of a cluster easy by utilizing a `ClusterManager` resource for Cluster Manager, and using the `IndexerCluster` resource for the cluster peers:

#### Cluster Manager
{: .no_toc }
```yaml
cat <<EOF | kubectl apply -f -
apiVersion: enterprise.splunk.com/v4
kind: ClusterManager
metadata:
  name: cm
  namespace: splunk-operator
  finalizers:
  - enterprise.splunk.com/delete-pvc
spec:
  monitoringConsoleRef:
    name: example-mc
EOF
```
The Splunk Operator is responsible for configuring and maintaing the connection between the cluster manager and the index cluster peers, but it does not manage Splunk Apps. The cluster manager manages the Splunk Apps and Add-ons distributed to all peers in the indexer cluster. See [Installing Splunk Apps](#installing-splunk-apps) for more information.

The Splunk Operator also controls the upgrade cycle, and implements the recommended order of cluster manager, search heads, and indexers, by defining and updating the docker image used by each IndexerCluster part.

This example includes the `monitoringConsoleRef` parameter used to define a monitoring console pod. The monitoring console pod does not need to be running; the name can be predefined and the pod started later. To start the monitoring console pod, see [Monitoring Console](#monitoring-console), or use the example below:

```yaml
cat <<EOF | kubectl apply -f -
apiVersion: enterprise.splunk.com/v4
kind: MonitoringConsole
metadata:
  name: example-mc
  namespace: splunk-operator
  finalizers:
  - enterprise.splunk.com/delete-pvc
EOF
```

The process is similar to build a multisite cluster, through defining a different zone affinity and site in each child IndexerCluster resource. See [Multisite cluster examples](/configuration/MultisiteExamples)

The passwords for the instance are generated automatically. To review the passwords, refer to the [Reading global kubernetes secret object](#reading-global-kubernetes-secret-object) instructions.

#### Indexer cluster peers
{: .no_toc }
```yaml
cat <<EOF | kubectl apply -f -
apiVersion: enterprise.splunk.com/v4
kind: IndexerCluster
metadata:
  name: example
  namespace: splunk-operator
  finalizers:
  - enterprise.splunk.com/delete-pvc
spec:
  clusterManagerRef:
    name: cm
  monitoringConsoleRef:
    name: example-mc
EOF
```
This will automatically configure a cluster, with a predetermined number of index cluster peers generated automatically based upon the replication_factor (RF) set. This example includes the `monitoringConsoleRef` parameter used to define a monitoring console pod. 

{: .note }
If you try to specify the number of `replicas` on an IndexerCluster CR less than the RF (as set on ClusterManager,) the Splunk Operator will always scale the number of peers to either the `replication_factor` for single site indexer clusters, or to the `origin` count in `site_replication_factor` for multi-site indexer clusters.

After starting the cluster manager, indexer cluster peers, and monitoring console pods using the examples above, use the `kubectl get pods` command to verify your environment:
```
$ kubectl get pods
NAME                                       READY   STATUS    RESTARTS    AGE
splunk-cm-cluster-manager-0                  1/1     Running   0          29s
splunk-example-indexer-0                    1/1     Running   0          29s
splunk-example-indexer-1                    1/1     Running   0          29s
splunk-example-indexer-2                    1/1     Running   0          29s
splunk-example-mc-monitoring-console-0      1/1     Running   0          40s
splunk-operator-7c5599546c-wt4xl            1/1     Running   0          14h
```

#### Scaling cluster peers using replicas
{: .no_toc }
If you want to add more indexers as cluster peers, update your `IndexerCluster` CR and define the `replicas` parameter:

```yaml
cat <<EOF | kubectl apply -f -
apiVersion: enterprise.splunk.com/v4
kind: IndexerCluster
metadata:
  name: example
  namespace: splunk-operator
  finalizers:
  - enterprise.splunk.com/delete-pvc
spec:
  spec:
  clusterManagerRef:
    name: cm
  replicas: 3
  monitoringConsoleRef:
    name: example-mc
EOF
```

```
$ kubectl get pods
NAME                                         READY    STATUS    RESTARTS   AGE
splunk-cm-cluster-manager-0                    1/1     Running   0          14m
splunk-example-indexer-0                      1/1     Running   0          14m
splunk-example-indexer-1                      1/1     Running   0          70s
splunk-example-indexer-2                      1/1     Running   0          70s
splunk-example-mc-monitoring-console-0        1/1     Running   0          80s
splunk-operator-7c5599546c-wt4xl              1/1     Running   0          14h
```

You can now easily scale your indexer cluster by patching the `replicas` count. For example:

```
$ kubectl patch indexercluster example --type=json -p '[{"op": "replace", "path": "/spec/replicas", "value": 5}]'
indexercluster.enterprise.splunk.com/example patched
```

{: .note }
For efficiency, you can use the following short names with `kubectl`:
* `clustermanager`: `cmanager-idxc`
* `indexercluster`: `idc` or `idxc`
* `searchheadcluster`: `shc`
* `LicenseManager`: `lm`
* `monitoringconsole`: `mc`


All CR's that support a `replicas` field can be scaled using the `kubectl scale` command. For example:

```
$ kubectl scale idc example --replicas=5
indexercluster.enterprise.splunk.com/example scaled
```
#### Scaling cluster peers using pod autoscaling
{: .no_toc }
You can also create [Horizontal Pod Autoscalers](https://kubernetes.io/docs/tasks/run-application/horizontal-pod-autoscale/) to manage dynamic scaling for you. For example:

```yaml
cat <<EOF | kubectl apply -f -
apiVersion: autoscaling/v1
kind: HorizontalPodAutoscaler
metadata:
  name: idc-example
  namespace: splunk-operator
spec:
  scaleTargetRef:
    apiVersion: enterprise.splunk.com/v3
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

#### Create a search head for your index cluster
{: .no_toc }
To create a standalone search head that is preconfigured to search your indexer cluster, add the `clusterManagerRef` parameter:

```yaml
cat <<EOF | kubectl apply -f -
apiVersion: enterprise.splunk.com/v4
kind: Standalone
metadata:
  name: single
  namespace: splunk-operator
  finalizers:
  - enterprise.splunk.com/delete-pvc
spec:
  clusterManagerRef:
    name: cm
  monitoringConsoleRef:
    name: example-mc
EOF
```

{: .note }
The `clusterManagerRef` field points to the cluster manager for the indexer cluster. This example includes the `monitoringConsoleRef` parameter used to define a monitoring console pod. 

```
$ kubectl get pods
NAME                                         READY    STATUS    RESTARTS   AGE
splunk-cm-cluster-manager-0                    1/1     Running   0          14m
splunk-example-indexer-0                      1/1     Running   0          14m
splunk-example-indexer-1                      1/1     Running   0          70s
splunk-example-indexer-2                      1/1     Running   0          70s
splunk-example-mc-monitoring-console-0        1/1     Running   0          80s
splunk-single-standalone-0                    1/1     Running   0          90s
splunk-operator-7c5599546c-wt4xl              1/1     Running   0          14h
```

#### Another Cluster Manager example
{: .no_toc }
Having a separate CR for cluster manager allows you to define parameters differently than the indexers, such as storage capacity and the storage class used by persistent volumes.

```yaml
cat <<EOF | kubectl apply -f -
apiVersion: enterprise.splunk.com/v4
kind: ClusterManager
metadata:
  name: cm
  namespace: splunk-operator
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
apiVersion: enterprise.splunk.com/v3
kind: IndexerCluster
metadata:
  name: idxc-part1
  finalizers:
  - enterprise.splunk.com/delete-pvc
spec:
  # No cluster-manager created, uses the referenced one
  clusterManagerRef:
    name: cm
  replicas: 3
  storageClassName: local
  varStorage: "128Gi"
  monitoringConsoleRef:
    name: example-mc
EOF
```

### Monitoring Console

The Monitoring Console provides detailed topology and performance information about your Splunk Enterprise deployment. The monitoring console (MC) pod is referenced by using the `monitoringConsoleRef` parameter. When a pod that references the `monitoringConsoleRef` parameter is created or deleted, the MC pod will automatically update itself and create or remove connections to those pods. 


```yaml
cat <<EOF | kubectl apply -f -
apiVersion: enterprise.splunk.com/v4
kind: MonitoringConsole
metadata:
  name: example-mc
  namespace: splunk-operator
  finalizers:
  - enterprise.splunk.com/delete-pvc
EOF
```

There is no preferred order when running an MC pod; you can start the pod before or after the other CR's in the namespace.  To associate a new MC pod with an existing CR that does not define the `monitoringConsoleRef`, you can patch those CR's and add it.  For example: ```kubectl patch cm-idxc cm --type=json -p '[{"op":"add", "path":"/spec/monitoringConsoleRef/name", "value":example-mc}]'``` for a cluster manager and ```kubectl patch shc test --type=json -p '[{"op":"add", "path":"/spec/monitoringConsoleRef/name", "value":example-mc}]'``` for a search head cluster.


### Search Head Clusters

A search head cluster is used to distribute users and search load across multiple instances, and provides high availabilty for search jobs. See [About search head clustering](https://docs.splunk.com/Documentation/Splunk/latest/DistSearch/AboutSHC) in the Splunk Enterprise documentation.

You can create a search head cluster that is configured to communicate with your indexer cluster by using a `SearchHeadCluster` resource
and adding the `clusterManagerRef` parameter. 

```yaml
cat <<EOF | kubectl apply -f -
apiVersion: enterprise.splunk.com/v4
kind: SearchHeadCluster
metadata:
  name: example
  namespace: splunk-operator
  finalizers:
  - enterprise.splunk.com/delete-pvc
spec:
  clusterManagerRef:
    name: cm
  monitoringConsoleRef:
    name: example-mc
EOF
```

This will automatically create a deployer with 3 search heads clustered together. Search head clusters require a minimum of 3 members. This example includes the `monitoringConsoleRef` parameter and name used to define a monitoring console (MC) pod.  To start the monitoring console pod, see [Monitoring Console](#monitoring-console), or use the example below:

```yaml
cat <<EOF | kubectl apply -f -
apiVersion: enterprise.splunk.com/v4
kind: MonitoringConsole
metadata:
  name: example-mc
  namespace: splunk-operator
  finalizers:
  - enterprise.splunk.com/delete-pvc
EOF
```

```
$ kubectl get pods
NAME                                        READY   STATUS    RESTARTS   AGE
splunk-cm-cluster-manager-0                   1/1     Running   0          53m
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
splunk-example-mc-monitoring-console-0       1/1     Running   0          80s
```

Similar to indexer clusters, you can scale a search head cluster by patching the `replicas` parameter.

The passwords for the instance are generated automatically. To review the passwords, refer to the [Reading global kubernetes secret object](#reading-global-kubernetes-secret-object) instructions

### Cluster Services

The creation of `SearchHeadCluster`, `ClusterManager`, `MonitoringConsole`, and `IndexerCluster` resources also creates corresponding Kubernetes services:

```
$ kubectl get svc
NAME                                                        TYPE        CLUSTER-IP       EXTERNAL-IP   PORT(S)                                          AGE
splunk-cm-cluster-manager-service                            ClusterIP   10.100.98.17     <none>        8000/TCP,8089/TCP                                55m
splunk-cm-indexer-service                                   ClusterIP   10.100.119.27    <none>        8000/TCP,8089/TCP                                55m
splunk-example-mc-monitoring-console-service                ClusterIP   10.100.7.28      <none>        8000/TCP,8088/TCP,8089/TCP,9997/TCP              54m
splunk-example-deployer-service                             ClusterIP   10.100.43.240    <none>        8000/TCP,8089/TCP                                118s
splunk-example-indexer-headless                             ClusterIP   None             <none>        8000/TCP,8088/TCP,8089/TCP,9997/TCP              55m
splunk-example-indexer-service                              ClusterIP   10.100.192.73    <none>        8000/TCP,8088/TCP,8089/TCP,9997/TCP              55m
splunk-example-search-head-headless                         ClusterIP   None             <none>        8000/TCP,8089/TCP                                118s
splunk-example-search-head-service                          ClusterIP   10.100.37.53     <none>        8000/TCP,8089/TCP                                118s
splunk-operator-metrics                                     ClusterIP   10.100.181.146   <none>        8383/TCP,8686/TCP                                11d
```

To login to your new Splunk Enterprise cluster, you can forward port 8000 to one of the search head pods, or use a load balancing service that is automatically created for your deployment:

```
kubectl port-forward service/splunk-example-search-head-service 8000
```

Similar to other examples, the default administrator password can be obtained from the global kubernetes secrets object as described here:

```
kubectl get secret splunk-`<namespace`>-secret -o jsonpath='{.data.password}' | base64 --decode
```

See [Configuring Ingress](/configuration/Ingress) for guidance on making your Splunk Enterprise clusters accessible from outside of Kubernetes.

### Cleaning Up
{: .no_toc }
As these examples demonstrate, the Splunk Operator makes it easy to create and manage clustered deployments of Splunk Enterprise. Given the reduced complexity, the comparable resource requirements from leveraging containers, and the ability to easily start small and scale as necessary, we recommend that you use the `IndexerCluster` and `SearchHeadCluster` resources when creating deployments using the Splunk Operator. 

To remove the resources created from this example, run:

```
kubectl delete -n splunk-opertaor standalone single
kubectl delete -n splunk-opertaor shc example
kubectl delete -n splunk-opertaor idc example
kubectl delete -n splunk-opertaor mc example-mc
kubectl delete -n splunk-opertaor clustermanager cm
```

## SmartStore Index Management

Indexes can be managed through the Splunk Operator. Every index configured through the Splunk Operator must be SmartStore enabled. See [SmartStore Resource Guide](/configuration/SmartStore).

## Using Default Settings

The Splunk Enterprise container supports many
[default configuration settings](https://github.com/splunk/splunk-ansible/blob/develop/docs/advanced/default.yml.spec.md)
which are used to set up and configure new deployments. The Splunk Operator provides several ways to configure these.

Suppose we create a ConfigMap named `splunk-defaults` that includes a
`default.yml` in our kubernetes cluster:

```
kubectl create configmap splunk-defaults --from-file=default.yml
```

You can use the `volumes` and `defaultsUrl` parameters in the
configuration spec to have the Splunk Operator initialize
your deployment using these settings.

```yaml
apiVersion: enterprise.splunk.com/v4
kind: Standalone
metadata:
  name: example
  namespace: splunk-operator
  finalizers:
  - enterprise.splunk.com/delete-pvc
spec:
  volumes:
    - name: defaults
      configMap:
        name: splunk-defaults
  defaultsUrl: /mnt/defaults/default.yml
```

In the above example, `volumes` will mount the `splunk-defaults` ConfigMap with `default.yml` file under the `/mnt/defaults` directory on all pods of
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
has been disabled. Please review [`PasswordManagement.md`](/configuration/PasswordManagement)
and [`Managing global kubernetes secret object`](#managing-global-kubernetes-secret-object)
for more details.

## Installing Splunk Apps

{: .note }
This requires using the Splunk Enterprise container version 9.0.0 or later*

With the Splunk Operator 2.0 release, new App Framework is available to centrally store and deploy apps. See [AppFramework](/configuration/AppFramework) for information and examples.

## Creating a LicenseManager Using a ConfigMap

We recommend that you create a `LicenseManager` instance to share a license
with all the components in your Splunk Enterprise deployment.

First, you can create a ConfigMap named `splunk-licenses` that includes
a license file named `enterprise.lic` by running:

```
kubectl create configmap splunk-licenses --from-file=enterprise.lic
```
Your ConfigMap can contain multiple licenses ```--from-file=enterprise1.lic,enterprise2.lic```

You can create a `LicenseManager` that references this license by
using the `volumes` and `licenseUrl` configuration parameters:

```yaml
apiVersion: enterprise.splunk.com/v4
kind: LicenseManager
metadata:
  name: example
  namespace: splunk-operator
  finalizers:
  - enterprise.splunk.com/delete-pvc
spec:
  volumes:
    - name: licenses
      configMap:
        name: splunk-licenses
  licenseUrl: /mnt/licenses/enterprise.lic
```

`volumes` will mount the ConfigMap in your `LicenseManager` pod under the
`/mnt/licenses` directory, and `licenseUrl` will configure Splunk to use
the `enterprise.lic` file within it.

`licenseUrl` can reference more than one license.
```
licenseUrl: "/mnt/licenses/enterprise1.lic,/mnt/licenses/enterprise2.lic"
```

{: .note }
`licenseUrl` may specify a local path or URL such as "https://myco.com/enterprise.lic", and the `volumes` parameter can be used to mount any type of [Kubernetes Volumes](https://kubernetes.io/docs/concepts/storage/volumes/).

## Configuring Standalone to use License Manager

Once a LicenseManager is created, you can configure your `Standalone` to use
the `LicenseManager` by adding `licenseManagerRef` to its spec as follows:

```yaml
apiVersion: enterprise.splunk.com/v4
kind: Standalone
metadata:
  name: example
  namespace: splunk-operator
  finalizers:
  - enterprise.splunk.com/delete-pvc
spec:
  licenseManagerRef:
    name: example
```

## Configuring Indexer Clusters to use License Manager

While configuring [`Indexer Clusters`](#indexer-clusters) to use the `LicenseManager`, you need to add `licenseManagerRef` only to the `ClusterManager` spec as follows:

```yaml
cat <<EOF | kubectl apply -f -
apiVersion: enterprise.splunk.com/v4
kind: ClusterManager
metadata:
  name: example-cm
  namespace: splunk-operator
  finalizers:
  - enterprise.splunk.com/delete-pvc
spec:
  licenseManagerRef:
    name: example
---
apiVersion: enterprise.splunk.com/v3
kind: IndexerCluster
metadata:
  name: example-idc
  namespace: splunk-operator
  finalizers:
  - enterprise.splunk.com/delete-pvc
spec:
  clusterManagerRef:
    name: example-cm
EOF
```

In order to forward `LicenseManager` logs to the above `Indexer Cluster`, you need to add `clusterManagerRef` to the `LicenseManager` spec as follows:

```yaml
apiVersion: enterprise.splunk.com/v4
kind: LicenseManager
metadata:
  name: example
  namespace: splunk-operator
  finalizers:
  - enterprise.splunk.com/delete-pvc
spec:
  volumes:
    - name: licenses
      configMap:
        name: splunk-licenses
  licenseUrl: /mnt/licenses/enterprise.lic
  clusterManagerRef:
    name: example-cm
```

## Using an External License Manager

{: .note }
This requires using the Splunk Enterprise container version 8.1.0 or later*

The Splunk Operator for Kubernetes allows you to use an external License
Manager(LM) with the custom resources it manages. To do this, you will
share the same `pass4Symmkey` between the global secret object setup by the
operator & the external LM, and configure the `splunk.license_master_url`.
The operator requires that the external LM have a configured
pass4SymmKey for authentication.

### Configuring pass4Symmkey:

There are two ways to configure `pass4Symmkey` with an External LM:

#### Approach 1
{: .no_toc }
- Setup the desired plain-text [`pass4Symmkey`](/configuration/PasswordManagement#pass4Symmkey) in the global secret object(Note: The `pass4Symmkey` would be stored in a base64 encoded format). For details see [updating global kubernetes secret object](#updating-global-kubernetes-secret-object).
- Setup the same plain-text `pass4SymmKey` in the `[general]` section of your LM's `server.conf` file.

#### Approach 2
{: .no_toc }
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
- Setup the above decrypted plain-text [`pass4Symmkey`](/configuration/PasswordManagement#pass4Symmkey) in the global secret object(Note: The `pass4Symmkey` would be stored in a base64 encoded format). For details see [updating global kubernetes secret object](#updating-global-kubernetes-secret-object)

### Configuring license_master_url:

Assuming that the hostname for your LM is `license-manager.splunk.mydomain.com`,
you should create a `default.yml` file with the following contents:

```yaml
splunk:
  license_master_url: license-manager.splunk.mydomain.com
```

Next, save this file as a secret. In this example we are calling it `splunk-license-manager`:

```
kubectl create secret generic splunk-license-manager --from-file=default.yml
```

You can then use the `defaultsUrl` parameter and a reference to the secret object created above to configure any Splunk
Enterprise custom resource to use your External LM:

```yaml
apiVersion: enterprise.splunk.com/v4
kind: Standalone
metadata:
  name: example
  namespace: splunk-operator
  finalizers:
  - enterprise.splunk.com/delete-pvc
spec:
  volumes:
    - name: license-manager
      secret:
        secretName: splunk-license-manager
  defaultsUrl: /mnt/license-manager/default.yml
```

## Using an External Indexer Cluster

{: .note }
This requires using the Splunk Enterprise container version 8.1.0 or later*

The Splunk Operator for Kubernetes allows you to use an external cluster of
indexers with its `Standalone`, `SearchHeadCluster` and `LicenseManager`
resources. To do this, you will share the same `IDXC pass4Symmkey` between the global secret object setup by the
operator & the external indexer cluster, and configure the `splunk.cluster_master_url`.


### Configuring IDXC pass4Symmkey:

There are two ways to configure `IDXC pass4Symmkey` with an External Indexer Cluster:
#### Approach 1
{: .no_toc }
- Setup the desired plain-text [`IDXC pass4Symmkey`](/configuration/PasswordManagement#idxc-pass4Symmkey) in the global secret object(Note: The `IDXC pass4Symmkey` would be stored in a base64 encoded format). For details see [updating global kubernetes secret object](#updating-global-kubernetes-secret-object).
- Setup the same plain-text `IDXC pass4SymmKey` in the `[clustering]` section of your cluster manager's and indexers' `server.conf` file.

#### Approach 2
{: .no_toc }
- Retrieve the plain-text `IDXC pass4SymmKey` in the `[clustering]` section of your cluster manager's `server.conf` file.
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
- Setup the above decrypted plain-text [`IDXC pass4Symmkey`](/configuration/PasswordManagement#idxc-pass4Symmkey) in the global secret object(Note: The `IDXC pass4Symmkey` would be stored in a base64 encoded format). For details see [updating global kubernetes secret object](#updating-global-kubernetes-secret-object)

### Configuring cluster_master_url:

Assuming the hostname for your cluster manager is `cluster-manager.splunk.mydomain.com`,
you should create a `default.yml` file with the following contents:

```yaml
splunk:
  cluster_master_url: cluster-manager.splunk.mydomain.com
```

Next, save this file as a secret. In the example here, it is called `splunk-cluster-manager`:

```
kubectl create secret generic splunk-cluster-manager --from-file=default.yml
```

You can then use the `defaultsUrl` parameter and a reference to the secret created above to configure any Splunk
Enterprise custom resource to use your external indexer cluster:

```yaml
apiVersion: enterprise.splunk.com/v4
kind: SearchHeadCluster
metadata:
  name: example
  namespace: splunk-operator
  finalizers:
  - enterprise.splunk.com/delete-pvc
spec:
  volumes:
    - name: cluster-manager
      secret:
        secretName: splunk-cluster-manager
  defaultsUrl: /mnt/cluster-manager/default.yml
```
## Managing global kubernetes secret object

### Creating global kubernetes secret object

Use the kubectl command to create the global kubernetes secret object:

1. Verify the namespace. You can retrieve the namespace in the current context using `kubectl config view --minify --output 'jsonpath={..namespace}'`. Make a note of the output. If the command doesn't display an output it indicates that we are in the `default` namespace.  
{: .note }
If you already have a desired namespace, you can set current context to the same using the following command: `kubectl config set-context --current --namespace=<desired_namespace>`

2. Gather the password values for the secret tokens you want to configure. To see all available secret tokens defined for the global kubernetes secret object, review [password management](/configuration/PasswordManagement#splunk-secret-tokens-in-the-global-secret-object)

3. Create a kubernetes secret object referencing the namespace. Example: splunk-`<desired_namespace`>-secret.
In the example below, we are creating the global kubernetes secret object, defining the default administrator and pass4Symmkey tokens, and passing in the values.  
`kubectl create secret generic splunk-<desired_namespace>-secret --from-literal='password=<admin_password_value>' --from-literal='pass4SymmKey=<pass4Symmkey_value>'`

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
  namespace: splunk-operator
  ownerReferences:
  - apiVersion: enterprise.splunk.com/v3
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
{% raw  %}
`kubectl get secret splunk-<desired_namespace>-secret -o go-template='{{range $k,$v := .data}}{{printf "%s: " $k}}{{if not $v}}{{$v}}{{else}}{{$v | base64decode}}{{end}}{{"\n"}}{{end}}'`
{% endraw %}
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
2. Obtain the key name for the secret token you are populating. The list of tokens is available in [password management](/configuration/PasswordManagement#splunk-secret-tokens-in-the-global-secret-object). 
3. Update the global kubernetes secret object using the key and the encoded value: `kubectl patch secret splunk-<desired_namespace>-secret -p='{"data":{"<key_name_for_secret_token>": "<encoded_value>"}}' -v=1`

### Deleting global kubernetes secret object

Use the kubectl command to delete the global kubernetes secret object:

`kubectl delete secret splunk-<desired_namespace>-secret`
