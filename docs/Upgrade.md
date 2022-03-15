# Splunk Operator Upgrade

Upgrading the Splunk operator to Version 1.1.0 is a new installation rather than an upgrade from the current operator. The older Splunk operator must be cleaned up before installing the new version. This script helps you to do the cleanup. The script expects the current namespace where the operator is installed and the path to the 1.1.0 manifest file. The script performs the following steps

* Backup of all the operator resources within the namespace like
** service-account, deployment, role, role-binding, cluster-role, cluster-role-binding
* Deletes all the old Splunk operator resources and deployment
* Installs the operator 1.1.0 in Splunk-operator namespace.

By default Splunk operator 1.1.0 will be installed to watch cluster-wide

## Steps for upgrade from 1.0.5 to 1.1.0

Run upgrade-to-1.1.0.sh script with the following mandatory arguments
`current_namespace` current namespace where operator is installed
`manifest_file`: path to 1.1.0 Splunk operator manifest file

### Example

```upgrade-to-1.1.0.sh --current_namespace=splunk-operator manifest_file=release-v1.1.0/splunk-operator-install.yaml```

## Configuring Operator to watch specific namespace

Edit config-map splunk-operator-config in splunk-operator namespace, set WATCH_NAMESPACE field namespace to be monitored by the Splunk operator

```
apiVersion: v1
data:
  OPERATOR_NAME: "splunk-operator"
  RELATED_IMAGE_SPLUNK_ENTERPRISE: splunk/splunk:latest
  WATCH_NAMESPACE: "add namespace here"
kind: ConfigMap
metadata:
  labels:
    name: splunk-operator
  name: splunk-operator-config
  namespace: splunk-operator
```
