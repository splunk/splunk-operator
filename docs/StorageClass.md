# Setting Up a Persistent Storage for Splunk

The Splunk Operator for Kubernetes uses
[Storage Classes](https://kubernetes.io/docs/concepts/storage/storage-classes/)
to create and manage two Persistent Volumes for all of the Splunk pods in your
deployments. For each pod, the following two volumes will be mounted:

| Path            | Purpose                                                        |
| --------------- | -------------------------------------------------------------- |
| `/opt/splunk/etc` | This is used to store all configuration and any installed apps |
| `/opt/splunk/var` | This is used to store all indexed events, logs, and other data |

By default, 10GiB volumes will be created for `/opt/splunk/etc` and 100GiB
volumes will be created for `/opt/splunk/var`. 

You can customize the `storage capacity` and `storage class names` for the `/opt/splunk/etc`
and `/opt/splunk/var` volumes by using the `storageCapacity` and `storageClassName` fields
under the `etcVolumeStorageConfig` and `varVolumeStorageConfig` spec as follows:

```yaml
apiVersion: enterprise.splunk.com/v1
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
The following `kubectl` command can be used to see which Storage Classes are available in
your Kubernetes cluster:

```
$ kubectl get storageclass
NAME            PROVISIONER             AGE
gp2 (default)   kubernetes.io/aws-ebs   176d
```

If no `storageClassName` is provided, the default Storage Class for your
Kubernetes cluster will be used.


## Ephemeral Storage

For testing and demonstration purposes, you may bypass the use of persistent
storage by using the `ephemeralStorage` field under the `etcVolumeStorageConfig`
and `varVolumeStorageConfig` spec as follows:

```yaml
apiVersion: enterprise.splunk.com/v1
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

Splunk Enterprise’s performance is heavily dependent on the performance of your
underlying storage infrastructure. Different Storage Class providers have a
wide range of performance characteristics, and how you configure and architect
your storage infrastructure will have a significant impact on the performance
of Splunk Enterprise. While we cannot provide any guidance on performance at this
point in time, we aim to do so in the future as this project matures.


## Amazon Elastic Kubernetes Service (EKS)

Users of EKS can create Storage Classes that use
[Elastic Block Store](https://aws.amazon.com/ebs/) (EBS) as their
Persistent Volumes. EKS automatically creates a default Storage Class
named “gp2” for all new clusters. 

Please see the Kubernetes
[AWS EBS documentation](https://kubernetes.io/docs/concepts/storage/storage-classes/#aws-ebs)
for additional Storage Class configuration options that are available,
such as enabling encryption, using provisioned IOPS, etc.


## Google Kubernetes Engine (GKE)

Users of GKE can create Storage Classes that use
[Persistent Disks](https://cloud.google.com/persistent-disk/) (PD)
as their Persistent Volumes. GKE automatically creates a default
Storage Class named “standard” for all new clusters. 

Please see the Kubernetes
[GKE PD documentation](https://kubernetes.io/docs/concepts/storage/storage-classes/#gce-pd)
for additional Storage Class configuration options that are available.


## Local Persistent Volumes

Users of Kubernetes 1.14 or later may use
[Local Persistent Volumes](https://kubernetes.io/blog/2019/04/04/kubernetes-1.14-local-persistent-volumes-ga/)
with their Splunk cluster. The use of local, direct-attached storage can offer
performance on par with running Splunk on bare metal, at the cost of
sacrificing high availability provided by other storage options. Splunk’s
built-in clustering technologies mitigate this cost by replicating your
data across multiple instances.

If you are interested in using Local Persistent Volumes, we recommend
considering these open source projects:

* [TopoLVM](https://blog.kintone.io/entry/topolvm) (uses LVM to dynamically provision and manage volumes)
* [Local Path Provisioner](https://github.com/rancher/local-path-provisioner) (shares a single, mounted volume on each node)


## Additional Providers

The [introduction](https://kubernetes.io/blog/2018/01/introducing-container-storage-interface/)
of Kubernetes’s [Container Storage Interface](https://kubernetes.io/blog/2019/01/15/container-storage-interface-ga/) (CSI)
has made it easy for new vendors to offer innovative solutions for managing
the persistence of containerized applications. Users of Kubernetes 1.13 or
later are encouraged to take a look at
[this list of CSI Drivers](https://kubernetes-csi.github.io/docs/drivers.html).

We have tested basic functionality of the Splunk Operator with the following:

* [Portworx](https://portworx.com/)
* [StorageOS](https://storageos.com/)
* [Robin.io](https://robin.io/)
* [Rook Ceph](https://www.rook.io/) (open source)

Some of these offer management and portability advantages and claim
performance that is on par with bare metal infrastructure. However, at this
time we cannot make any specific recommendations, or verify any claims or
comparisons regarding performance.
