# Frequently Asked Questions

* [When will Splunk Enterprise be supported in containers?](#when-will-splunk-enterprise-be-supported-in-containers?)
* [When will the Splunk Operator for Kubernetes be generally available?](#when-will-the-splunk-operator-for-kubernetes-be-generally-available?)
* [Why use a Kubernetes operator versus something like Helm charts?](#why-use-a-kubernetes-operator-versus-something-like-helm-charts?)

## When will Splunk Enterprise be supported in containers?

Support from Splunk is currently only available for single-instance
deployments that use our
[official Splunk Enterprise container images](https://hub.docker.com/r/splunk/splunk/).
While our images can also be used as a foundation for building other
Splunk Enterprise architectures (including clusters), support for these
architectures is currently only available from the
[open source community](https://github.com/splunk/docker-splunk).

For other architectures, we recommend that you try out the
[Splunk Operator for Kubernetes](https://splunk.github.io/splunk-operator/).
This open source project is currently a “beta,” and we welcome any
[feedback](https://github.com/splunk/splunk-operator/issues)
on your requirements.

## When will the Splunk Operator for Kubernetes be generally available?

At this time, we are unable to provide any guidance on when, or even if,
the Splunk Operator for Kubernetes will be made generally available for
production use.

## Why use a Kubernetes operator versus something like Helm charts?

The use of operators has evolved as a
[standard pattern](https://kubernetes.io/docs/concepts/extend-kubernetes/operator/)
for managing applications in Kubernetes. While simple, stateless
microservices may be well served by the basic resources that the platform
provides, extensive work is required to deploy and manage more distributed
and stateful applications like Splunk Enterprise.

[Custom Resources](https://kubernetes.io/docs/concepts/extend-kubernetes/api-extension/custom-resources/)
enable application developers to extend the vocabulary of Kubernetes.
Operators use the [control loop](https://kubernetes.io/docs/concepts/#kubernetes-control-plane)
principle to bring life to Custom Resources, turning them into native
extensions that act like all the other basic resources Kubernetes
provides out of the box. A single Custom Resource can be used to represent
higher level concepts, simplifying the complexity of managing many more
basic, underlying components.

Operators are a lot like installers for the cloud (similar to RPM for Red Hat
Linux or MSI for Microsoft Windows). They enable developers to package all the
components of their application in a way that makes it easy to deploy, even
across thousands of servers.

Operators also enable developers to fully automate the operations of their
application. Applications often require specific, custom sequences of actions
to be performed. By "owning" all the underlying components and having a
greater "understanding" of how they are meant to work together, operators
have the potential to offload a great amount of work from their human
counterparts.

Helm and similar tools may make it easier to deploy applications, but after
that you are basically on your own. We prefer operators because they have
the potential to enable far better day 2+ experiences for our customers.
