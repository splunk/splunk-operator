# Red Hat OpenShift Configuration

The Splunk Operator will always start Splunk Enterprise containers using
a specific, unprivileged `splunk` user and group. Running as a specific user
and group is required for the Splunk Enterprise containers to write to
Kubernetes PersistentVolumes. This follows best security practices, which
helps prevent any malicious actor from escalating access outside of the
container and compromising the host. For more information, please see the
Splunk Enterprise container's
[Documentation on Security](https://github.com/splunk/docker-splunk/blob/develop/docs/SECURITY.md).

Users of Red Hat OpenShift may find that the default Security Context
Constraint is too restrictive. You can fix this by granting the default
Service Account the `nonroot` Security Context Constraint by running the
following command within your namespace:

```
oc adm policy add-scc-to-user nonroot -z default
```
