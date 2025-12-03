---
title: OpenShift
parent: Platform Guides
nav_order: 1
---


# Red Hat OpenShift Configuration

The Splunk Operator will always start Splunk Enterprise containers using
a specific, unprivileged `splunk(41812)` user and group to allow write access
to Kubernetes PersistentVolumes. This follows best security practices, helping
prevent malicious actors from escalating access beyond the container and
compromising the host. For more information, please see the Splunk Enterprise
container's [Documentation on Security](https://github.com/splunk/docker-splunk/blob/develop/docs/SECURITY.md).

The Splunk Enterprise pods are attached to the `default` serviceaccount or the configured
[serviceaccount](CustomResources.md#common-spec-parameters-for-splunk-enterprise-resources) if
any. The Splunk Operator pod is attached to the Service Account `splunk-operator-controller-manager`
and runs as user `1001`.

Users of Red Hat OpenShift may find that the default Security Context
Constraint is too restrictive. You can fix this by granting the appropriate
Service Accounts the `nonroot` Security Context Constraint by running the
following commands within your namespace. If you are using OpenShift 4.14
or later, you must use the `nonroot-v2` Security Context Constraint instead.

For the Splunk Operator pod:
```
oc adm policy add-scc-to-user nonroot -z splunk-operator-controller-manager
```

For the Splunk Enterprise CR pods(replace `default` with the configured [serviceaccount](CustomResources.md#common-spec-parameters-for-splunk-enterprise-resources) if any):
```
oc adm policy add-scc-to-user nonroot -z default
```
