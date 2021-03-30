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

While we are only able to test and support a small subset of configurations, the Splunk Operator should work with any CNCF certified distribution of Kubernetes, version 1.12 or later. Setting up, configuring and managing Kubernetes clusters is outside the scope of this guide and Splunk’s coverage of support. You can submit bugs to https://github.com/splunk/splunk-operator/issues.

*Kubernetes releases 1.16.0 and 1.16.1 contain a
[critical bug(https://github.com/kubernetes/kubernetes/pull/83789) that can
crash your API server when using custom resource definitions. Do not
attempt to run the Splunk Operator using these releases. This bug is fixed in
Kuberenetes 1.16.2.*

The Splunk Operator requires these docker images to be present or available to your Kubernetes cluster:

* `splunk/splunk-operator`: The Splunk Operator image (built by this repository)
* `splunk/splunk:8.1.0`: The [Splunk Enterprise image](https://github.com/splunk/docker-splunk) (8.1.0 or later)

All of the Splunk Enterprise images are publicly available on [Docker Hub](https://hub.docker.com/). If your cluster does not have access to pull from Docker Hub, see the [Required Images Documentation](Images.md) page.

The Splunk Operator uses Kubernetes [Persistent Volume Claims](https://kubernetes.io/docs/concepts/storage/persistent-volumes/#persistentvolumeclaims) to store all of your Splunk Enterprise configuration ("$SPLUNK_HOME/etc" path) and event ("$SPLUNK_HOME/var" path) data. If one of the underlying machines fail, Kubernetes will automatically try to recover by restarting the Splunk Enterprise pods on another machine that is able to reuse the same data volumes. This minimizes the maintenance burden on your operations team by reducing the impact of common hardware failures to the equivalent of a service restart. 
The use of Persistent Volume Claims requires that your cluster is configured to support one or more Kubernetes persistent [Storage Classes](https://kubernetes.io/docs/concepts/storage/storage-classes/). See the [Setting Up a Persistent Storage for Splunk](StorageClass.md) page for more
information.


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

5. To delete your standalone deployment, run:

```
kubectl delete standalone s1
``` 


The `Standalone` custom resource is just one of the resources the Splunk Operator provides. You can find more custom resources and the parameters they support on the [Custom Resource Guide](CustomResources.md) page.

For additional deployment examples, including Splunk Enterprise clusters, see the 
[Configuring Splunk Enterprise Deployments](Examples.md) page.

For additional guidance on making Splunk Enterprise ports accessible outside of Kubernetes, see the [Configuring Ingress](Ingress.md) page.


