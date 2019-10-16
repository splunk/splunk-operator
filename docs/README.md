# Getting Started with the Splunk Operator for Kubernetes

This guide is intended to help new users get up and running with the
Splunk Operator. It is divided into the following sections:

* [Known Issues for the Splunk Operator](#known-issues-for-the-splunk-operator)
* [Prerequisites for the Splunk Operator](#prerequisites-for-the-splunk-operator)
* [Installing the Splunk Operator](#installing-the-splunk-operator)
* [Creating SplunkEnterprise Deployments](#creating-splunkenterprise-deployments)

*Please note that the Splunk Operator is undergoing active development
and considered to be an "alpha" quality release. We expect significant
modifications will be made prior to its general availability, it is not
covered by support, and we strongly discourage using it in production.*


## Known Issues for the Splunk Operator

We are working to resolve the following in future releases:

* Using the Splunk Operator to scale search heads, indexers and spark workers
is not currently supported. These can be resized by modifying the StatefulSet
objects directly, but is not recommended. Scaling up may work fine, but
scaling down requires additional coordination that has not yet been implemented.
* Managed Splunk Enterprise upgrades is not currently supported. The
`image` parameter of StatefulSets can be manually updated to perform an
upgrade, but we intend future release to provide more reliable, controlled
paths for automating upgrades.


## Prerequisites for the Splunk Operator

We have tested basic functionality of the Splunk Operator with the following:

* [Amazon Elastic Kubernetes Service](https://aws.amazon.com/eks/) (EKS)
* [Google Kubernetes Engine](https://cloud.google.com/kubernetes-engine/) (GKE)
* [Red Hat OpenShift](https://www.openshift.com/) (4.1)
* [Docker Enterprise Edition](https://docs.docker.com/ee/) (3.1)
* [Open Source Kubernetes](https://kubernetes.io/) (1.15.1)

While we are only able to test and support a small subset of configurations,
the Splunk Operator should work with any CNCF certified distribution of
Kubernetes, version 1.12 or later. Setting up, configuring and managing
Kubernetes clusters is outside the scope of this guide and Splunk’s coverage
of support. For evaluation, we recommend using EKS or GKE.

The Splunk operator requires three docker images to be present or available
to your Kubernetes cluster:

* `splunk/splunk-operator`: The Splunk Operator image (built by this repository)
* `splunk/splunk:8.0`: The Splunk Enterprise image (8.0 or later)
* `splunk/spark`: The default Spark image (used when DFS is enabled)

All of these images are publicly available on [Docker Hub](https://hub.docker.com/).
If your cluster does not have access to pull from Docker Hub, please see the
[Air Gap Documentation](AirGap.md).

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
Please see [Setting Up a Storage Class for Splunk](StorageClass.md) for more
information.


## Installing the Splunk Operator

Most users can install and start the Splunk Operator by just running
```
kubectl apply -f https://tiny.cc/splunk-operator-install
```

Users of Red Hat OpenShift should read the additional
[Red Hat OpenShift](OpenShift.md) documentation.

Please see the [Advanced Installation Instructions](Install.md) for
special considerations, including the use of private image registries and
installing as a regular user (who is not a Kubernetes cluster administrator).

*Note: The `splunk/splunk:8.0` image is rather large, so we strongly
recommend copying this to a private registry or directly onto your
Kubernetes workers as per the [Air Gap Documentation](AirGap.md),
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
kubectl delete splunkenterprises --all
kubectl delete -f https://tiny.cc/splunk-operator-install
```


## Creating SplunkEnterprise Deployments

The `SplunkEnterprise` resource is used to create a single deployment of Splunk
Enterprise. For example,  run the following command to create a single instance 
deployment named “s1”:

```yaml
cat <<EOF | kubectl apply -f -
apiVersion: enterprise.splunk.com/v1alpha1
kind: SplunkEnterprise
metadata:
  name: s1
  finalizers:
  - enterprise.splunk.com/delete-pvc
EOF
```

Within a few seconds (or minutes if you are pulling the public Splunk images
for the first time), you should see a new pod running in your cluster:

```
$ kubectl get pods
NAME                                    READY   STATUS    RESTARTS   AGE
splunk-operator-75f5d4d85b-8pshn        1/1     Running   0          11m
splunk-s1-standalone-5d54bcc4bf-bpzx7   1/1     Running   0          16s
```

By default, an admin user password will be automatically generated for your 
deployment. You can get the password by running:

```
kubectl get secret splunk-s1-secrets -o jsonpath='{.data.password}' | base64 --decode
```

*Note: if your shell prints a `%` at the end, leave that out when you
copy/paste the output.*

To log into your instance, you can forward port 8000 from your local
machine by running:
```
kubectl port-forward splunk-s1-standalone-5d54bcc4bf-bpzx7 8000
```

You should then be able to log into Splunk at http://localhost:8000 using the
`admin` account with the password that you obtained from `splunk-s1-secrets`.

To delete your deployment, just run:
```
kubectl delete splunkenterprise/s1
```

The `SplunkEnterprise` resource supports many additional configuration parameters.
Please see [SplunkEnterprise Parameters](SplunkEnterprise.md) for more information.
For more deployment examples, please see [SplunkEnterprise Examples](Examples.md).

Please see [Configuring Ingress](Ingress.md) for guidance on making your Splunk
clusters accessible outside of Kubernetes.
