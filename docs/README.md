# Getting Started with the Splunk Operator for Kubernetes

Splunk Operator for Kubernetes enables you to quickly and easily deploy Splunk Enterprise on your choice of private or public cloud provider. The Operator simplifies scaling and management of Splunk Enterprise by automating administrative workflows using Kubernetes best practices. 

The Splunk Operator runs as a container, and uses the Kubernetes [operator pattern](https://kubernetes.io/docs/concepts/extend-kubernetes/operator/) and [custom resources](https://kubernetes.io/docs/concepts/extend-kubernetes/api-extension/custom-resources/) objects to create and manage a scalable and sustainable Splunk Enterprise environment.

This guide is intended to help new users get up and running with the
Splunk Operator for Kubernetes. It is divided into the following sections:

* [Known Issues for the Splunk Operator](#known-issues-for-the-splunk-operator)
* [Prerequisites for the Splunk Operator](#prerequisites-for-the-splunk-operator)
* [Installing the Splunk Operator](#installing-the-splunk-operator)
* [Creating Splunk Enterprise Deployments](#creating-splunk-enterprise-deployments)
* [Securing Splunk Deployments in Kubernetes](Security.md)

## Support Resources

COMMUNITY SUPPORTED: Splunk Operator for Kubernetes (SOK) is an open source product developed by Splunkers with contributions from the community of partners and customers. This unique product will be enhanced, maintained and supported by the community, led by Splunkers with deep subject matter expertise. The primary reason why Splunk is taking this approach is to push product development closer to those that use and depend upon it. This direct connection will help us all be more successful and move at a rapid pace.

If you're interested in contributing to the SOK open source project, review the [Contributing to the Project](CONTRIBUTING.md) page.

**Community Support & Discussions on
[Slack](https://splunk-usergroups.slack.com)** channel #kubernetes

**File Issues or Enhancements in
[GitHub](https://github.com/splunk/splunk-operator/issues)** splunk/splunk-operator


## Known Issues for the Splunk Operator

Review the [Change Log](ChangeLog.md) page for a history of changes in each release.

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
| [Google Kubernetes Engine (GKE)](https://cloud.google.com/kubernetes-engine)| No | Actively used in the development of the Splunk Operator.|
| [Kind](https://kind.sigs.k8s.io/) |  Yes| Actively used in the development of the Splunk Operator. Suitable for development/test environments only|
| [Microsoft Azure Kubernetes Service (AKS)](https://azure.microsoft.com/en-us/services/kubernetes-service/)| No| No additional info available |
| [Red Hat OpenShift](https://www.openshift.com/)| No| No additional info available|
| [HPE Ezmeral](https://www.hpe.com/us/en/solutions/container-platform.html)|No | No additional info available|


*Kubernetes releases 1.16.0 and 1.16.1 contain a
[critical bug(https://github.com/kubernetes/kubernetes/pull/83789) that can
crash your API server when using custom resource definitions. Do not
attempt to run the Splunk Operator using these releases. This bug is fixed in
Kuberenetes 1.16.2.*

### Splunk Enterprise Compatibility
Each Splunk Operator release has specific Splunk Enterprise compatibility requirements. Before installing or upgrading the Splunk Operator, review the [Change Log](https://github.com/splunk/splunk-operator/blob/develop/docs/ChangeLog.md) to verify version compatibility with Splunk Enterprise releases.

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

Splunk Operator does not support vCPU licensing.

For information on utilizing Kubernetes Quality of Service classes to enforce minimum CPU and memory allocations in production environments, see [Kubernetes Quality of Service classes](CustomResources.md#Kubernetes-Quality-of-Service-classes)


### Storage guidelines
The Splunk Operator uses Kubernetes [Persistent Volume Claims](https://kubernetes.io/docs/concepts/storage/persistent-volumes/#persistentvolumeclaims) to store all of your Splunk Enterprise configuration ("$SPLUNK_HOME/etc" path) and event ("$SPLUNK_HOME/var" path) data. If one of the underlying machines fail, Kubernetes will automatically try to recover by restarting the Splunk Enterprise pods on another machine that is able to reuse the same data volumes. This minimizes the maintenance burden on your operations team by reducing the impact of common hardware failures to the equivalent of a service restart. 
The use of Persistent Volume Claims requires that your cluster is configured to support one or more Kubernetes persistent [Storage Classes](https://kubernetes.io/docs/concepts/storage/storage-classes/). See the [Setting Up a Persistent Storage for Splunk](StorageClass.md) page for more
information.

The Kubernetes infrastructure must have access to storage that meets or exceeds the recommendations provided in the the Splunk Enterprise [Reference Hardware documentation](https://docs.splunk.com/Documentation/Splunk/latest/Capacity/Referencehardware#What_storage_type_should_I_use_for_a_role.3F). 

### Use of Splunk SmartStore
As a splunk deployment's data volume increases, demand for storage typically outpaces demand for compute resources. [Splunk's SmartStore Feature](https://docs.splunk.com/Documentation/Splunk/latest/Indexer/AboutSmartStore) allows you to manage your indexer storage and compute resources in a ___cost-effective___ manner by scaling those resources separately. SmartStore utilizes a fast, SSD-based cache on each Splunk indexer node to keep recent data locally available for search. When data rolls to WARM lifecycle stage, it is uploaded to an S3 API-compliant object store for persistence. Look into the [Configuring SmartStore](https://docs.splunk.com/Documentation/Splunk/latest/Indexer/AboutSmartStore) document for configuring and using SmartStore.


## Installing the Splunk Operator

A Kubernetes cluster administrator can install and start the Splunk Operator by running:
```
kubectl apply -f https://github.com/splunk/splunk-operator/releases/download/1.0.0-RC/splunk-operator-install.yaml
```

The [Advanced Installation Instructions](Install.md) page offers guidance for advanced configurations, including the use of private image registries, installation at cluster scope, and installing the Splunk Operator as a user who is not a Kubernetes administrator. Users of Red Hat OpenShift should review the [Red Hat OpenShift](OpenShift.md) page.

*Note: We recommended that the Splunk Enterprise Docker image is copied to a private registry, or directly onto your Kubernetes workers before creating large Splunk Enterprise deployments. See the [Required Images Documentation](Images.md) page, and the [Advanced Installation Instructions](Install.md) page for guidance on working with copies of the Docker images.*

After the Splunk Operator starts, you'll see a single pod running within your current namespace:
```
$ kubectl get pods
NAME                               READY   STATUS    RESTARTS   AGE
splunk-operator-75f5d4d85b-8pshn   1/1     Running   0          5s
```


## Creating a Splunk Enterprise deployment

The `Standalone` custom resource is used to create a single instance deployment of Splunk Enterprise. For example:

1. Run the command to create a deployment named “s1”:


```yaml
cat <<EOF | kubectl apply -f -
apiVersion: enterprise.splunk.com/v1
kind: Standalone
metadata:
  name: s1
  finalizers:
  - enterprise.splunk.com/delete-pvc
EOF
```

**The `enterprise.splunk.com/delete-pvc` finalizer is optional, and tells the Splunk Operator to remove any Kubernetes [Persistent Volumes](https://kubernetes.io/docs/concepts/storage/persistent-volumes/) associated with the instance if you delete the pod.**

Within a few minutes, you'll see new pods running in your namespace:

```
$ kubectl get pods
NAME                                   READY   STATUS    RESTARTS   AGE
splunk-operator-7c5599546c-wt4xl        1/1    Running   0          11h
splunk-default-monitoring-console-0     1/1    Running   0          30s
splunk-s1-standalone-0                  1/1    Running   0          45s
```

*Note: if your shell prints a `%` at the end, leave that out when you copy the output.*

2. You can use a simple network port forward to open port 8000 for Splunk Web access:

```
kubectl port-forward splunk-s1-standalone-0 8000
```

3. Get your passwords for the namespace. The Splunk Enterprise passwords used in the namespace are generated automatically. To learn how to find and read the passwords, see the [Reading global kubernetes secret object](Examples.md#reading-global-kubernetes-secret-object) page.


4. Log into Splunk Enterprise at http://localhost:8000 using the `admin` account with the password.


The `Standalone` custom resource is just one of the resources the Splunk Operator provides. You can find more custom resources and the parameters they support on the [Custom Resource Guide](CustomResources.md) page.

For additional deployment examples, including Splunk Enterprise clusters, see the 
[Configuring Splunk Enterprise Deployments](Examples.md) page.

For additional guidance on making Splunk Enterprise ports accessible outside of Kubernetes, see the [Configuring Ingress](Ingress.md) page.

To delete your standalone deployment, run:

```
kubectl delete standalone s1
```
