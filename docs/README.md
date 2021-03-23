# Getting Started with the Splunk Operator for Kubernetes

The Splunk Operator for Kubernetes (SOK) makes it easy for Splunk
Administrators to deploy and operate Enterprise deployments in a Kubernetes
infrastructure. Packaged as a container, it uses the
[operator pattern](https://kubernetes.io/docs/concepts/extend-kubernetes/operator/)
to manage Splunk-specific [custom resources](https://kubernetes.io/docs/concepts/extend-kubernetes/api-extension/custom-resources/),
following best practices to manage all the underlying Kubernetes objects for you. 

This guide is intended to help new users get up and running with the
Splunk Operator for Kubernetes. It is divided into the following sections:

* [Known Issues for the Splunk Operator](#known-issues-for-the-splunk-operator)
* [Prerequisites for the Splunk Operator](#prerequisites-for-the-splunk-operator)
* [Installing the Splunk Operator](#installing-the-splunk-operator)
* [Creating Splunk Enterprise Deployments](#creating-splunk-enterprise-deployments)

COMMUNITY SUPPORTED: Splunk Operator for Kubernetes is an open source product
developed by Splunkers with contributions from the community of partners and
customers. This unique product will be enhanced, maintained and supported by
the community, led by Splunkers with deep subject matter expertise. The primary
reason why Splunk is taking this approach is to push product development closer
to those that use and depend upon it. This direct connection will help us all
be more successful and move at a rapid pace.

**For help, please first read our [Frequently Asked Questions](FAQ.md)**

**Community Support & Discussions on
[Slack](https://splunk-usergroups.slack.com)** channel #kubernetes

**File Issues or Enhancements in
[GitHub](https://github.com/splunk/splunk-operator/issues)** splunk/splunk-operator


## Known Issues for the Splunk Operator

*Please note that the Splunk Operator is undergoing active development
and considered to be a "beta" quality release. We expect significant
modifications will be made prior to its general availability, it is not
covered by support, and we strongly discourage using it in production.*

Please see the [Change Log](ChangeLog.md) for a history of changes made in
previous releases.

## Prerequisites for the Splunk Operator

### Supported Kubernetes Versions

- Kubernetes 1.12 and later

The Splunk Operator is supported with [CNCF certified](https://www.cncf.io/certification/software-conformance/) distributions of Kubernetes, version 1.12* and later.

Implementing, configuring, and administering Kubernetes clusters is outside the scope of this guide, and Splunk’s coverage of support.

You can submit defects to https://github.com/splunk/splunk-operator/issues.

### Kubernetes Platform Suggestions:

In order to create Splunk deployments through Splunk operator, access to a functioning kubernetes environment is required. Some options include:



| Platform        | Splunk Smartstore Support | Additional Info|
| ---------- | ------- | ------- |
| [Amazon Elastic Kubernetes Service (EKS)](https://aws.amazon.com/eks)| Yes | Actively used in the development of the Splunk Operator.|
| [Google Kubernetes Engine (GKE)](https://cloud.google.com/kubernetes-engine)| | Actively used in the development of the Splunk Operator.|
| [Kind](https://kind.sigs.k8s.io/) |  Yes| Actively used in the development of the Splunk Operator. Suitable for development/test environments only |
| [Microsoft Azure Kubernetes Service (AKS)](https://azure.microsoft.com/en-us/services/kubernetes-service/)| | |
| [Red Hat OpenShift](https://www.openshift.com/)| | |
| [HPE Ezmeral](https://www.hpe.com/us/en/solutions/container-platform.html)| | |


*Kubernetes releases 1.16.0 and 1.16.1 contain a
[critical bug(https://github.com/kubernetes/kubernetes/pull/83789) that can
crash your API server when using custom resource definitions. Do not
attempt to run the Splunk Operator using these releases. This bug is fixed in
Kuberenetes 1.16.2.*

### Splunk Enterprise Compatibility
Each Splunk Operator release has specific Splunk Enterprise compatibility requirements. Before installing or upgrading the Splunk Operator, review the [Change Log](#ChangeLog.md) to verify version compatibility with Splunk Enterprise releases.

### Splunk Apps Installation

Splunk Apps can be installed using Splunk operator by following instructions given at [Installing Splunk Apps](https://github.com/splunk/splunk-operator/blob/develop/docs/Examples.md#installing-splunk-apps). Splunk Premium apps are not supported with the Splunk Operator.


### Docker requirements
The Splunk Operator requires these docker images to be present or available to your Kubernetes cluster:

* `splunk/splunk-operator`: The Splunk Operator image (built by this repository)
* `splunk/splunk:8.1.0`: The [Splunk Enterprise image](https://github.com/splunk/docker-splunk) (8.1.0 or later)

All of the Splunk Enterprise images are publicly available on [Docker Hub](https://hub.docker.com/). If your cluster does not have access to pull from Docker Hub, see the [Required Images Documentation](Images.md) page.

### Summary of performance requirements
The resources guidelines for running production Splunk Enterprise instances in pods using the Splunk Operator are the same as running Splunk Enterprise natively on a supported operating system. Refer to the Splunk Enterprise [Reference Hardware documentation](https://docs.splunk.com/Documentation/Splunk/latest/Capacity/Referencehardware) for additional detail.

A summary of the minimum reference hardware requirements:
| Standalone        | Search Head Cluster | Indexer Cluster |    
| ---------- | ------- | ------- |
| 12 CPU Cores or 24 vCPU, 2Ghz or greater per core, 12GB RAM. | Each pod requires: 16 CPU Cores or 32 vCPU, 2Ghz or greater per core, 12GB RAM.| Each pod requires: 12 CPU cores, or 24 vCPU at 2GHz or greater per core, 12GB RAM.|  

For information on utilizing Kubernetes Quality of Service classes to enforce minimum CPU and memory allocations in production environments, see [Kubernetes Quality of Service classes](CustomResources.md#Kubernetes-Quality-of-Service-classes)


### Storage guidelines
The Splunk Operator uses Kubernetes [Persistent Volume Claims](https://kubernetes.io/docs/concepts/storage/persistent-volumes/#persistentvolumeclaims) to store all of your Splunk Enterprise configuration ("$SPLUNK_HOME/etc" path) and event ("$SPLUNK_HOME/var" path) data. If one of the underlying machines fail, Kubernetes will automatically try to recover by restarting the Splunk Enterprise pods on another machine that is able to reuse the same data volumes. This minimizes the maintenance burden on your operations team by reducing the impact of common hardware failures to the equivalent of a service restart. 
The use of Persistent Volume Claims requires that your cluster is configured to support one or more Kubernetes persistent [Storage Classes](https://kubernetes.io/docs/concepts/storage/storage-classes/). See the [Setting Up a Persistent Storage for Splunk](StorageClass.md) page for more
information.

The Kubernetes infrastructure must have access to storage that meets or exceeds the recommendations provided in the the Splunk Enterprise [Reference Hardware documentation](https://docs.splunk.com/Documentation/Splunk/latest/Capacity/Referencehardware#What_storage_type_should_I_use_for_a_role.3F). 

### Use of Splunk SmartStore
As a splunk deployment's data volume increases, demand for storage typically outpaces demand for compute resources. [Splunk's SmartStore Feature](https://docs.splunk.com/Documentation/Splunk/latest/Indexer/AboutSmartStore) allows you to manage your indexer storage and compute resources in a ___cost-effective___ manner by scaling those resources separately. SmartStore utilizes a fast, SSD-based cache on each Splunk indexer node to keep recent data locally available for search. When data rolls to WARM lifecycle stage, it is uploaded to an S3 API-compliant object store for persistence. Look into the [Configuring SmartStore](https://docs.splunk.com/Documentation/Splunk/latest/Indexer/AboutSmartStore) document for configuring and useing SmartStore.


## Installing the Splunk Operator

Most users can install and start the Splunk Operator by just running
```
kubectl apply -f https://github.com/splunk/splunk-operator/releases/download/0.2.2/splunk-operator-install.yaml
```

Users of Red Hat OpenShift should read the additional
[Red Hat OpenShift](OpenShift.md) documentation.

Please see the [Advanced Installation Instructions](Install.md) for
special considerations, including the use of private image registries,
installation at cluster scope, and installing as a regular user (who is
not a Kubernetes cluster administrator).

*Note: The `splunk/splunk:8.1.0` image (or later) is rather large, so we strongly
recommend copying this to a private registry or directly onto your
Kubernetes workers as per the [Required Images Documentation](Images.md),
and following the [Advanced Installation Instructions](Install.md),
before creating any large Splunk deployments.*

After starting the Splunk Operator, you should see a single pod running
within your current namespace:
```
$ kubectl get pods
NAME                               READY   STATUS    RESTARTS   AGE
splunk-operator-75f5d4d85b-8pshn   1/1     Running   0          5s
```

To remove all Splunk deployments and completely remove the
Splunk Operator, run:
```
kubectl delete standalones --all
kubectl delete licensemasters --all
kubectl delete searchheadclusters --all
kubectl delete clustermasters --all
kubectl delete indexerclusters --all
kubectl delete spark --all
kubectl delete -f https://github.com/splunk/splunk-operator/releases/download/0.2.2/splunk-operator-install.yaml
```


## Creating Splunk Enterprise Deployments

The `Standalone` resource is used to create a single instance deployment
of Splunk Enterprise. For example,  run the following command to create a
deployment named “s1”:

```yaml
cat <<EOF | kubectl apply -f -
apiVersion: enterprise.splunk.com/v1beta1
kind: Standalone
metadata:
  name: s1
  finalizers:
  - enterprise.splunk.com/delete-pvc
EOF
```

The `enterprise.splunk.com/delete-pvc` finalizer is optional, and may be
used to tell the Splunk Operator that you would like it to remove all the
[Persistent Volumes](https://kubernetes.io/docs/concepts/storage/persistent-volumes/)
associated with the instance when you delete it.

Within a few seconds (or minutes if you are pulling the public Splunk images
for the first time), you should see a new pod running in your cluster:

```
$ kubectl get pods
NAME                                   READY   STATUS    RESTARTS   AGE
splunk-operator-7c5599546c-wt4xl        1/1    Running   0          11h
splunk-default-monitoring-console-0     1/1    Running   0          30s
splunk-s1-standalone-0                  1/1    Running   0          45s
```

The passwords for the instance are generated automatically. To review the passwords, please refer to the [Reading global kubernetes secret object](Examples.md#reading-global-kubernetes-secret-object) instructions.

*Note: if your shell prints a `%` at the end, leave that out when you
copy the output.*

To log into your instance, you can forward port 8000 from your local
machine by running:

```
kubectl port-forward splunk-s1-standalone-0 8000
```

You should then be able to log into Splunk Enterprise at http://localhost:8000 using
the `admin` account with the password that you obtained from `splunk-s1-standalone-secrets`.

To delete your deployment, just run:

```
kubectl delete standalone s1
```

`Standalone` is just one custom resource that the Splunk Operator provides.
You can find more information about other custom resources and the parameters
they support in the [Custom Resource Guide](CustomResources.md).

For additional deployment examples, including clustering, please see
[Configuring Splunk Enterprise Deployments](Examples.md).

Please see [Configuring Ingress](Ingress.md) for guidance on making your
Splunk clusters accessible outside of Kubernetes.
