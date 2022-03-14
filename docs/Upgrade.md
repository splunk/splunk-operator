# Splunk Operator Upgrade

Please note upgrading Splunk operator to Version `1.1.0` is a new installation rather than upgrade from current operator. Due to this  older Splunk operator need to be cleaned up before installing the latest version.  The script will help customer in doing these steps. This script expect current namespace where operator is installed , and path to `1.1.0` manifest file. script will follow the below mentioned steps

* Backup of all the operator resources within the namespace like
** service-account, deployment, role, role-binding, cluster-role, cluster-role-binding
* Deletes all the old Splunk operator resources and deployment
* Installs the operator 1.1.0 in Splunk-operator namespace.

By default Splunk operator 1.1.0 will be installed to watch cluster-wide

## Steps for upgrade from 1.0.5 to 1.1.0

run upgrade-to-1.1.0.sh script with below mentioned mandatory arguments
`current_namespace` current namespace where operator is installed, if its not found, it will exit with error message
`manifest_file`: path where 1.1.0 Splunk operator manifest file exist

### Example

```upgrade-to-1.1.0.sh --current_namespace=splunk-operator manifest_file=release-v1.1.0/splunk-operator-cluster.yaml```

## Configuring Operator to watch specific namespace

Edit config-map splunk-operator-config in splunk-operator namespace, set WATCH_NAMESPACE field to the namespace operator need to watch

```
apiVersion: v1
data:
  OPERATOR_NAME: '"splunk-operator"'
  RELATED_IMAGE_SPLUNK_ENTERPRISE: splunk/splunk:latest
  WATCH_NAMESPACE: "add namespace here"
kind: ConfigMap
metadata:
  labels:
    name: splunk-operator
  name: splunk-operator-config
  namespace: splunk-operator
```
