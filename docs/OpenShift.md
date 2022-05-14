# Red Hat OpenShift Configuration

The Splunk Operator will always start Splunk Enterprise containers using
a specific, unprivileged `splunk` user and group. Running as a specific user
and group is required for the Splunk Enterprise containers to write to
Kubernetes PersistentVolumes. This follows best security practices, which
helps prevent any malicious actor from escalating access outside of the
container and compromising the host. For more information, please see the
Splunk Enterprise container's
[Documentation on Security](https://github.com/splunk/docker-splunk/blob/develop/docs/SECURITY.md).

Similarly the Splunk Operator pod is attached to the Service Account
`splunk-operator-controller-manager` and runs as user `1001`.

Users of Red Hat OpenShift may find that the default Security Context
Constraint is too restrictive. You can fix this by granting the `default`
and `splunk-operator-controller-manager` Service Accounts the `nonroot`
Security Context Constraint by running the following commands within your namespace:

```
oc adm policy add-scc-to-user nonroot -z default
oc adm policy add-scc-to-user nonroot -z splunk-operator-controller-manager
```