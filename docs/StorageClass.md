# Setting Up a Storage Class for Splunk

## Amazon Elastic Kubernetes Service (EKS)

Users of EKS are able to create Storage Classes that use
[Elastic Block Store](https://aws.amazon.com/ebs/) (EBS) to back their
Persistent Volumes. EKS automatically creates a default Storage Class
named “gp2” for all new clusters. 

Please see the Kubernetes
[AWS EBS documentation](https://kubernetes.io/docs/concepts/storage/storage-classes/#aws-ebs)
for additional Storage Class configuration options that are available,
such as enabling encryption, using provisioned IOPS, etc.


## Google Kubernetes Engine (GKE)

Users of GKE are able to create Storage Classes that use
[Persistent Disks](https://cloud.google.com/persistent-disk/) (PD)
to back their Persistent Volumes. GKE automatically creates a default
Storage Class named “standard” for all new clusters. 

Please see the Kubernetes
[GKE PD documentation](https://kubernetes.io/docs/concepts/storage/storage-classes/#gce-pd)
for additional Storage Class configuration options that are available.


## Performance Considerations

Splunk’s performance is heavily dependent on the performance of your
underlying storage infrastructure. Different Storage Class providers have a
wide range of performance characteristics, and how you configure and architect
your storage infrastructure will have a significant impact on the performance
of Splunk. While we are unable to provide any guidance on performance at this
point in time, we aim to do so in the future as this project matures.


## Local Persistent Volumes

Users of Kubernetes 1.14 or later may consider using
[Local Persistent Volumes](https://kubernetes.io/blog/2019/04/04/kubernetes-1.14-local-persistent-volumes-ga/)
with their Splunk cluster. The use of local, direct-attached storage can offer
performance on par with running Splunk on bare metal, at the cost of
sacrificing high availability provided by other storage options. Splunk’s
built-in clustering technologies mitigate this cost by replicating your
data across multiple instances.


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
performance that is on-par with bare metal infrastructure. However, at this
time we cannot make any specific recommendations, or verify any claims or
comparisons regarding performance.
