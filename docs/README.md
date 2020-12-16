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

While we are only able to test and support a small subset of configurations,
the Splunk Operator should work with any CNCF certified distribution of
Kubernetes, version 1.12 or later. Setting up, configuring and managing
Kubernetes clusters is outside the scope of this guide and Splunk’s coverage
of support. Please submit bugs to https://github.com/splunk/splunk-operator/issues.

*Kubernetes releases 1.16.0 and 1.16.1 contain a
[critical bug(https://github.com/kubernetes/kubernetes/pull/83789) that can
crash your API server when using custom resource definitions. Please do not
attempt to run the Splunk Operator using these releases. This bug is fixed in
Kuberenetes 1.16.2.*

The Splunk Operator requires three docker images to be present or available
to your Kubernetes cluster:

* `splunk/splunk-operator`: The Splunk Operator image (built by this repository)
* `splunk/splunk:8.1.0`: The [Splunk Enterprise image](https://github.com/splunk/docker-splunk) (8.1.0 or later)
* `splunk/spark`: The [Splunk Spark image](https://github.com/splunk/docker-spark) (used when DFS is enabled)

All of these images are publicly available on [Docker Hub](https://hub.docker.com/).
If your cluster does not have access to pull from Docker Hub, please see the
[Required Images Documentation](Images.md).

The Splunk Operator uses
[Persistent Volume Claims](https://kubernetes.io/docs/concepts/storage/persistent-volumes/#persistentvolumeclaims)
to store all of your configuration (etc) and event (var) data. If an
underlying server fails, Kubernetes will automatically try to recover by
restarting Splunk pods on another server that is able to reuse the same
data volumes. This minimizes the maintenance burden on your operations team
by reducing the impact of common hardware failures to be similar to a server
restart. The use of Persistent Volume Claims requires that your cluster is
configured to support one or more persistent
[Storage Classes](https://kubernetes.io/docs/concepts/storage/storage-classes/).
Please see [Setting Up a Persistent Storage for Splunk](StorageClass.md) for more
information.


## Installing the Splunk Operator

Most users can install and start the Splunk Operator by just running
```
kubectl apply -f https://github.com/splunk/splunk-operator/releases/download/0.2.1/splunk-operator-install.yaml
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
kubectl delete -f https://github.com/splunk/splunk-operator/releases/download/0.2.1/splunk-operator-install.yaml
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
