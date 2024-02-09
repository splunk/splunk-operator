---
layout: home
title: Getting Started
nav_order: 1
permalink: "/"
---

# Getting Started with the Splunk Operator for Kubernetes

The Splunk Operator for Kubernetes enables you to quickly and easily deploy Splunk Enterprise on your choice of private or public cloud provider. The Operator simplifies scaling and management of Splunk Enterprise by automating administrative workflows using Kubernetes best practices.

The Splunk Operator runs as a container, and uses the Kubernetes [operator pattern](https://kubernetes.io/docs/concepts/extend-kubernetes/operator/) and [custom resources](https://kubernetes.io/docs/concepts/extend-kubernetes/api-extension/custom-resources/) objects to create and manage a scalable and sustainable Splunk Enterprise environment.

This guide is intended to help new users get up and running with the
Splunk Operator for Kubernetes.


## Prerequisites for the Splunk Operator

- [CNCF certified distribution](/installation/Prerequisites#platform-recommendations)
- Kubernetes configured to support one or more Kubernetes persistent Storage Classes

## Installing the Splunk Operator

A Kubernetes cluster administrator can install and start the Splunk Operator for specific namespace by running:
```
kubectl apply -f https://github.com/splunk/splunk-operator/releases/download/2.5.0/splunk-operator-namespace.yaml --server-side  --force-conflicts
```

A Kubernetes cluster administrator can install and start the Splunk Operator for cluster-wide by running:
```
kubectl apply -f https://github.com/splunk/splunk-operator/releases/download/2.5.0/splunk-operator-cluster.yaml --server-side  --force-conflicts
```

The [Advanced Installation Instructions](/installation/AdvancedInstallation) page offers guidance for advanced configurations, including the use of private image registries, installation at cluster scope, and installing the Splunk Operator as a user who is not a Kubernetes administrator. Users of Red Hat OpenShift should review the [Red Hat OpenShift](/installation/OpenShift) page.

{: .note }
We recommended that the Splunk Enterprise Docker image is copied to a private registry, or directly onto your Kubernetes workers before creating large Splunk Enterprise deployments. See the [Required Images Documentation](/installation/Images) page, and the [Advanced Installation Instructions](/installation/AdvancedInstallation) page for guidance on working with copies of the Docker images.

After the Splunk Operator starts, you'll see a single pod running within your current namespace:
```
$ kubectl get pods
NAME                               READY   STATUS    RESTARTS   AGE
splunk-operator-75f5d4d85b-8pshn   1/1     Running   0          5s
```

### Installation using Helm charts

Installing the Splunk Operator using Helm allows you to quickly deploy the operator and Splunk Enterprise in a Kubernetes cluster. The operator and custom resources are easily configurable allowing for advanced installations including support for Splunk Validated Architectures. Helm also provides a number of features to manage the operator and custom resource lifecycle. The [Installation using Helm](/installation/Helm) page will walk you through installing and configuring Splunk Enterprise deployments using Helm charts.


## Creating a Splunk Enterprise deployment

The `Standalone` custom resource is used to create a single instance deployment of Splunk Enterprise. For example:

1. Run the command to create a deployment named “s1”:


```yaml
cat <<EOF | kubectl apply -n splunk-operator -f -
apiVersion: enterprise.splunk.com/v4
kind: Standalone
metadata:
  name: s1
  finalizers:
  - enterprise.splunk.com/delete-pvc
EOF
```

**The `enterprise.splunk.com/delete-pvc` finalizer is optional, and tells the Splunk Operator to remove any Kubernetes [Persistent Volumes](https://kubernetes.io/docs/concepts/storage/persistent-volumes/) associated with the instance if you delete the custom resource(CR).**

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

3. Get your passwords for the namespace. The Splunk Enterprise passwords used in the namespace are generated automatically. To learn how to find and read the passwords, see the [Reading global kubernetes secret object](/configuration/Examples#reading-global-kubernetes-secret-object) page.


4. Log into Splunk Enterprise at http://localhost:8000 using the `admin` account with the password.

5. To delete your standalone deployment, run:

```
kubectl delete standalone s1
```

The `Standalone` custom resource is just one of the resources the Splunk Operator provides. You can find more custom resources and the parameters they support on the [Custom Resource Guide](/configuration/CustomResources) page.

For additional deployment examples, including Splunk Enterprise clusters, see the
[Configuring Splunk Enterprise Deployments](/configuration/examples) page.

For additional guidance on making Splunk Enterprise ports accessible outside of Kubernetes, see the [Configuring Ingress](/configuration/Ingress) page.

### Splunk App Installation

Apps and add-ons can be installed using the Splunk Operator by following the instructions given at [Installing Splunk Apps](/configuration/AppFramework). For the installation of premium apps please refer to [Premium Apps Installation Guide](/configuration/PremiumApps).