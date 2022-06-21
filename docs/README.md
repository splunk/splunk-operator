# Getting Started with the Splunk Operator for Kubernetes

The Splunk Operator for Kubernetes enables you to quickly and easily deploy Splunk Enterprise on your choice of private or public cloud provider. The Operator simplifies scaling and management of Splunk Enterprise by automating administrative workflows using Kubernetes best practices. 

The Splunk Operator runs as a container, and uses the Kubernetes [operator pattern](https://kubernetes.io/docs/concepts/extend-kubernetes/operator/) and [custom resources](https://kubernetes.io/docs/concepts/extend-kubernetes/api-extension/custom-resources/) objects to create and manage a scalable and sustainable Splunk Enterprise environment.

This guide is intended to help new users get up and running with the
Splunk Operator for Kubernetes. It is divided into the following sections:

* [Known Issues for the Splunk Operator](#known-issues-for-the-splunk-operator)
* [Prerequisites for the Splunk Operator](#prerequisites-for-the-splunk-operator)
* [Installing the Splunk Operator](#installing-the-splunk-operator)
* [Creating Splunk Enterprise Deployments](#creating-a-splunk-enterprise-deployment)
* [Securing Splunk Deployments in Kubernetes](Security.md)
* [Contacting Support](#contacting-support)

## Support Resources

SPLUNK SUPPORTED: The Splunk Operator for Kubernetes is a supported method for deploying distributed Splunk Enterprise environments using containers.

COMMUNITY DEVELOPED: Splunk Operator for Kubernetes is an open source product developed by Splunkers with contributions from the community of partners and customers. This unique product will be enhanced, maintained and supported by the community, led by Splunkers with deep subject matter expertise. The primary reason why Splunk is taking this approach is to push product development closer to those that use and depend upon it. This direct connection will help us all be more successful and move at a rapid pace.

If you're interested in contributing to the SOK open source project, review the [Contributing to the Project](CONTRIBUTING.md) page.

**Community Support & Discussions on
[Slack](https://splunk-usergroups.slack.com)** channel #splunk-operator-for-kubernetes

**File Issues or Enhancements in
[GitHub](https://github.com/splunk/splunk-operator/issues)** splunk/splunk-operator


## Known Issues for the Splunk Operator

Review the [Change Log](ChangeLog.md) page for a history of changes in each release.

## Prerequisites for the Splunk Operator

### Supported Kubernetes Versions

- Kubernetes, version 1.16.2+ and later (x86 64-bit only).

The Splunk Operator should work with any [CNCF certified distribution](https://www.cncf.io/certification/software-conformance/) of Kubernetes. We do not have platform recommendations, but this is a table of platforms that our developers, customers, and partners have used successfully with the Splunk Operator.

<table>
<tr><td> Splunk Development & Testing Platforms </td><td> Amazon Elastic Kubernetes Service (EKS), Google Kubernetes Engine (GKE) </td></tr>
<tr><td> Customer Reported Platforms </td><td> Microsoft Azure Kubernetes Service (AKS), Red Hat OpenShift </td></tr>
<tr><td> Partner Tested Platforms</td><td> HPE Ezmeral</td></tr>
<tr><td> Other Platforms </td><td>CNCF certified distribution</td></tr>
</table>

### Splunk Enterprise Compatibility
Each Splunk Operator release has specific Splunk Enterprise compatibility requirements. Before installing or upgrading the Splunk Operator, review the [Change Log](ChangeLog.md) to verify version compatibility with Splunk Enterprise releases.

### Splunk Apps Installation

Apps and add-ons can be installed using the Splunk Operator by following the instructions given at [Installing Splunk Apps](Examples.md#installing-splunk-apps).  Premium apps such as Enterprise Security and IT Service Intelligence are currently not supported.


### Docker requirements
The Splunk Operator requires these docker images to be present or available to your Kubernetes cluster:

* `splunk/splunk-operator`: The Splunk Operator image built by this repository or the [official release](https://hub.docker.com/r/splunk/splunk-operator) (2.0.0 or later)
* `splunk/splunk:<version>`: The [Splunk Enterprise image](https://github.com/splunk/docker-splunk) (8.2.6 or later)

All of the Splunk Enterprise images are publicly available on [Docker Hub](https://hub.docker.com/). If your cluster does not have access to pull from Docker Hub, see the [Required Images Documentation](Images.md) page.

Review the [Change Log](ChangeLog.md) page for a history of changes and Splunk Enterprise compatibility for each release.

### Hardware Resources Requirements
The resource guidelines for running production Splunk Enterprise instances in pods through the Splunk Operator are the same as running Splunk Enterprise natively on a supported operating system and file system. Refer to the Splunk Enterprise [Reference Hardware documentation](https://docs.splunk.com/Documentation/Splunk/latest/Capacity/Referencehardware) for additional details.  We would also recommend following the same guidance on [Splunk Enterprise for disabling Transparent Huge Pages (THP)](https://docs.splunk.com/Documentation/Splunk/latest/ReleaseNotes/SplunkandTHP) for the nodes in your Kubernetes cluster.  Please be aware that this may impact performance of other non-Splunk workloads.

#### Minimum Reference Hardware
Based on Splunk Enterprise [Reference Hardware documentation](https://docs.splunk.com/Documentation/Splunk/latest/Capacity/Referencehardware), a summary of the minimum reference hardware requirements is given below.

| Standalone        | Search Head / Search Head Cluster | Indexer Cluster |
| ---------- | ------- | ------- |
| _Each Standalone Pod: 12 Physical CPU Cores or 24 vCPU at 2Ghz or greater per core, 12GB RAM._| _Each Search Head Pod: 16 Physical CPU Cores or 32 vCPU at 2Ghz or greater per core, 12GB RAM._| _Each Indexer Pod: 12 Physical CPU cores, or 24 vCPU at 2GHz or greater per core, 12GB RAM._ |


#### _Using Kubernetes Quality of Service Classes_

In addition to the guidelines provided in the reference hardware, [Kubernetes Quality of Service Classes](https://kubernetes.io/docs/tasks/configure-pod-container/quality-service-pod/)  can be used to configure CPU/Mem resources allocations that map to your _service level objectives_. For further information on utilizing Kubernetes Quality of Service (QoS) classes, see the table below:


| QoS        | Summary| Description |
| ---------- | ------- | ------- |
| _Guaranteed_ | _CPU/Mem ```requests``` = CPU/Mem ```limits```_    | _When the CPU and memory  ```requests``` and ```limits``` values are equal, the pod is given a QoS class of Guaranteed. This level of service is recommended for Splunk Enterprise ___production environments___._ |
| _Burstable_ | _CPU/Mem ```requests``` < CPU/Mem ```limits```_  | _When the CPU and memory  ```requests``` value is set lower than the ```limits``` the pod is given a QoS class of Burstable. This level of service is useful in a user acceptance testing ___(UAT) environment___, where the pods run with minimum resources, and Kubernetes allocates additional resources depending on usage._|
| _BestEffort_ | _No CPU/Mem ```requests``` or ```limits``` are set_ | _When the ```requests``` or ```limits``` values are not set, the pod is given a QoS class of BestEffort. This level of service is sufficient for ___testing, or a small development task___._ |

Examples on how to implement these QoS are given at [Examples of Guaranteed and Burstable QoS](CustomResources.md#examples-of-guaranteed-and-burstable-qos) section.


### Storage guidelines
The Splunk Operator uses Kubernetes [Persistent Volume Claims](https://kubernetes.io/docs/concepts/storage/persistent-volumes/#persistentvolumeclaims) to store all of your Splunk Enterprise configuration ("$SPLUNK_HOME/etc" path) and event ("$SPLUNK_HOME/var" path) data. If one of the underlying machines fail, Kubernetes will automatically try to recover by restarting the Splunk Enterprise pods on another machine that is able to reuse the same data volumes. This minimizes the maintenance burden on your operations team by reducing the impact of common hardware failures to the equivalent of a service restart. 
The use of Persistent Volume Claims requires that your cluster is configured to support one or more Kubernetes persistent [Storage Classes](https://kubernetes.io/docs/concepts/storage/storage-classes/). See the [Setting Up a Persistent Storage for Splunk](StorageClass.md) page for more
information.

### What Storage Type To Use?

The Kubernetes infrastructure must have access to storage that meets or exceeds the recommendations provided in the Splunk Enterprise storage type recommendations at [Reference Hardware documentation - what storage type to use for a given role?](https://docs.splunk.com/Documentation/Splunk/latest/Capacity/Referencehardware#What_storage_type_should_I_use_for_a_role.3F) In summary, Indexers with SmartStore need NVMe or SSD storage to provide the necessary IOPs for a successful Splunk Enterprise environment.


### Splunk SmartStore Required
For production environments, we are requiring the use of Splunk SmartStore. As a Splunk Enterprise deployment's data volume increases, demand for storage typically outpaces demand for compute resources. [Splunk's SmartStore Feature](https://docs.splunk.com/Documentation/Splunk/latest/Indexer/AboutSmartStore) allows you to manage your indexer storage and compute resources in a ___cost-effective___ manner by scaling those resources separately. SmartStore utilizes a fast storage cache on each indexer node to keep recent data locally available for search and keep other data in a remote object store. Look into the [SmartStore Resource Guide](SmartStore.md) document for configuring and using SmartStore through operator.
 
## Installing the Splunk Operator

A Kubernetes cluster administrator can install and start the Splunk Operator for specific namespace by running:
```
kubectl apply -f https://github.com/splunk/splunk-operator/releases/download/2.0.0/splunk-operator-namespace.yaml
```

A Kubernetes cluster administrator can install and start the Splunk Operator for cluster-wide by running:
```
kubectl apply -f https://github.com/splunk/splunk-operator/releases/download/2.0.0/splunk-operator-cluster.yaml
```

The [Advanced Installation Instructions](Install.md) page offers guidance for advanced configurations, including the use of private image registries, installation at cluster scope, and installing the Splunk Operator as a user who is not a Kubernetes administrator. Users of Red Hat OpenShift should review the [Red Hat OpenShift](OpenShift.md) page.

The [Advanced Installation Instructions](Install.md) page offers guidance for advanced configurations, including the use of private image registries, installation at cluster scope, and installing the Splunk Operator as a user who is not a Kubernetes administrator. Users of Red Hat OpenShift should review the [Red Hat OpenShift](OpenShift.md) page.

*Note: We recommended that the Splunk Enterprise Docker image is copied to a private registry, or directly onto your Kubernetes workers before creating large Splunk Enterprise deployments. See the [Required Images Documentation](Images.md) page, and the [Advanced Installation Instructions](Install.md) page for guidance on working with copies of the Docker images.*

After the Splunk Operator starts, you'll see a single pod running within your current namespace:
```
$ kubectl get pods
NAME                               READY   STATUS    RESTARTS   AGE
splunk-operator-75f5d4d85b-8pshn   1/1     Running   0          5s
```
## Upgrading the Splunk Operator

For information on upgrading the Splunk Operator, see the [How to upgrade Splunk Operator and Splunk Enterprise Deployments](SplunkOperatorUpgrade.md) page.

## Creating a Splunk Enterprise deployment

The `Standalone` custom resource is used to create a single instance deployment of Splunk Enterprise. For example:

1. Run the command to create a deployment named “s1”:


```yaml
cat <<EOF | kubectl apply -n splunk-operator -f -
apiVersion: enterprise.splunk.com/v3
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
splunk-s1-standalone-0                  1/1    Running   0          45s
```

*Note: if your shell prints a `%` at the end, leave that out when you copy the output.*

2. You can use a simple network port forward to open port 8000 for Splunk Web access:

```
kubectl port-forward splunk-s1-standalone-0 8000
```

3. Get your passwords for the namespace. The Splunk Enterprise passwords used in the namespace are generated automatically. To learn how to find and read the passwords, see the [Reading global kubernetes secret object](Examples.md#reading-global-kubernetes-secret-object) page.


4. Log into Splunk Enterprise at http://localhost:8000 using the `admin` account with the password.

5. To delete your standalone deployment, run:

```
kubectl delete standalone s1
``` 

The `Standalone` custom resource is just one of the resources the Splunk Operator provides. You can find more custom resources and the parameters they support on the [Custom Resource Guide](CustomResources.md) page.

For additional deployment examples, including Splunk Enterprise clusters, see the 
[Configuring Splunk Enterprise Deployments](Examples.md) page.

For additional guidance on making Splunk Enterprise ports accessible outside of Kubernetes, see the [Configuring Ingress](Ingress.md) page.

## Contacting Support
If you are a Splunk Enterprise customer with a valid support entitlement contract and have a Splunk-related question, you can open a support case on the https://www.splunk.com/ support portal.
