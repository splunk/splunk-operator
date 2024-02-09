---
title: Persistent Storage
nav_order: 23
#nav_exclude: true
---


# Setting Up a Persistent Storage for Splunk

The Splunk Operator for Kubernetes uses Kubernetes [Storage Classes](https://kubernetes.io/docs/concepts/storage/storage-classes/) to create and manage two Persistent Volumes for all of the Splunk Enterprise pods in your deployment. Two volumes will be mounted for each pod:

| Path            | Purpose                                                    | Size   |
| --------------- | ---------------------------------------------------------- | ------ |
| `/opt/splunk/etc` | Used for apps, user objects, and custom configurations | 10GiB |
| `/opt/splunk/var` | Used to store all indexed events, logs, and other data | 100GiB|

By default, a 10GiB volume will be created for `/opt/splunk/etc`, and a 100GiB volume will be created for `/opt/splunk/var`. 

You can customize the storage capacity and storage class name used by the `/opt/splunk/etc ` and `/opt/splunk/var` volumes by modifying the `storageCapacity` and `storageClassName` fields under the `etcVolumeStorageConfig` and `varVolumeStorageConfig` spec. If no `storageClassName` is provided, the default Storage Class for your Kubernetes cluster will be used.

For example:

```yaml
apiVersion: enterprise.splunk.com/v4
kind: Standalone
metadata:
  name: example
  finalizers:
  - enterprise.splunk.com/delete-pvc
spec:
  etcVolumeStorageConfig:
    storageClassName: gp2
    storageCapacity: 15Gi
  varVolumeStorageConfig:
    storageClassName: customStorageClass
    storageCapacity: 25Gi
```
To see which Storage Classes are available in your Kubernetes cluster, use the `storageclass` command:

```
$ kubectl get storageclass
NAME            PROVISIONER             AGE
gp2 (default)   kubernetes.io/aws-ebs   176d
```

If no storageClassName is provided, the default Storage Class for your Kubernetes cluster will be used.

The following `kubectl` command can be use to verify space allocated to the etc and var directories on your POD. 
Replace the [POD_NAME] with your Splunk pod name.

```
$ kubectl exec [POD_NAME] -- df -h
In this example, you can verify that Splunk pod has got requested amount of storage -- etcVolumeStorageConfig is set to 15GB and varVolumeStorageConfig size is set to 25GB

Filesystem      Size  Used Avail Use% Mounted on
....
/dev/nvme2n1     25G  530M   24G   3% /opt/splunk/var
/dev/nvme1n1     15G  270M   15G   2% /opt/splunk/etc
....
```

## Ephemeral Storage

For testing and demonstration of Splunk Enterprise instances, you have the option of using ephemeral storage instead of persistent storage. Use the `ephemeralStorage` field under the `etcVolumeStorageConfig`and `varVolumeStorageConfig` spec to mount local, ephemeral volumes for `/opt/splunk/etc` and`/opt/splunk/var` using the Kubernetes [emptyDir](https://kubernetes.io/docs/concepts/storage/volumes/#emptydir) feature.

For example:

```yaml
apiVersion: enterprise.splunk.com/v4
kind: Standalone
metadata:
  name: example
  finalizers:
  - enterprise.splunk.com/delete-pvc
spec:
  etcVolumeStorageConfig:
    ephemeralStorage: true
  varVolumeStorageConfig:
    ephemeralStorage: true
```

This will mount local, ephemeral volumes for `/opt/splunk/etc` and
`/opt/splunk/var` using the Kubernetes
[emptyDir](https://kubernetes.io/docs/concepts/storage/volumes/#emptydir) feature.

*Please note that the contents of these directories will automatically be deleted
forever whenever the Pod is removed from a node (for any reason). We strongly
discourage and do not support using this for any production environments.*

## Performance Considerations

The performance of Splunk Enterprise is highly dependent on the performance of your underlying storage infrastructure. Different Storage Class providers offer a wide range of performance characteristics, and how you configure and architect your storage infrastructure will have a significant impact on the performance of Splunk Enterprise running in Kubernetes.


## Amazon Elastic Kubernetes Service (EKS)

Users of EKS can create Storage Classes that use Amazon [Elastic Block Store](https://aws.amazon.com/ebs/) (EBS) as their Persistent Volumes. EKS automatically creates a default Storage Class named `gp2` for all new clusters. 

For additional Storage Class configuration options, such as enabling encryption, and using provisioned IOPS, see the Kubernetes [AWS EBS documentation](https://kubernetes.io/docs/concepts/storage/storage-classes/#aws-ebs).


## Google Kubernetes Engine (GKE)

Users of GKE can create Storage Classes that use Google [Persistent Disks](https://cloud.google.com/persistent-disk/) (PD) as their Persistent Volumes. GKE automatically creates a default Storage Class named `standard` for all new clusters. 

For additional Storage Class configuration options, see the Kubernetes [GKE PD documentation](https://kubernetes.io/docs/concepts/storage/storage-classes/#gce-pd).


## Local Persistent Volumes

Users of Kubernetes 1.14 or later can also use Kubernetes [Local Persistent Volumes](https://kubernetes.io/blog/2019/04/04/kubernetes-1.14-local-persistent-volumes-ga/). The use of local, direct attached storage can offer performance similar to running Splunk Enterprise on bare metal, at the cost of sacrificing the high availability provided by other storage options. The built-in clustering technologies provided by Splunk Enterprise can mitigate the cost by replicating your data across multiple instances.

If you are interested in using Local Persistent Volumes, consider one of these open source projects:

* [TopoLVM](https://blog.kintone.io/entry/topolvm) (uses LVM to dynamically provision and manage volumes)
* [Local Path Provisioner](https://github.com/rancher/local-path-provisioner) (shares a single, mounted volume on each node)


## Additional Storage Providers

The [introduction](https://kubernetes.io/blog/2018/01/introducing-container-storage-interface/) of Kubernetes’s [Container Storage Interface](https://kubernetes.io/blog/2019/01/15/container-storage-interface-ga/) (CSI) has made it easy for new vendors to offer innovative solutions for managing the persistence of containerized applications. Users of Kubernetes 1.13 or later are encouraged to review the Kubernetes [list of CSI Drivers](https://kubernetes-csi.github.io/docs/drivers.html).

We have tested basic functionality of the Splunk Operator with the storage options:

* [Portworx](https://portworx.com/)
* [StorageOS](https://storageos.com/)
* [Robin.io](https://robin.io/)
* [Rook Ceph](https://www.rook.io/) (open source)

But we cannot make any specific recommendations, or verify any claims or comparisons regarding performance.


## Workload Management(WLM) Support
Workload management is a rule-based framework that lets you allocate compute and memory resources to search, indexing, and other workloads in Splunk Enterprise. Currently, Splunk Operator deployed workloads do not support this feature.