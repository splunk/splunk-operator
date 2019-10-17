# SplunkEnterprise Parameters

SplunkEnterprise is a [custom resource](https://kubernetes.io/docs/concepts/extend-kubernetes/api-extension/custom-resources/)
that is managed by the Splunk Operator for Kubernetes. It supports the
following configuration parameters:

## Metadata

```yaml
apiVersion: enterprise.splunk.com/v1alpha1
kind: SplunkEnterprise
metadata:
  ...
```

The following configuration parameters can be used within a `metadata` section:

| Key       | Type   | Description                                                                                                                                                                        |
| --------- | ------ | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| name      | string | Your splunk deployments will be distinguished using this name.                                                                                                                     |
| namespace | string | Your splunk deployments will be created in this namespace. You must ensure that this namespace exists before you create Splunk deployments.                                        |


## Spec

```yaml
apiVersion: enterprise.splunk.com/v1alpha1
kind: SplunkEnterprise
metadata:
  name: example
spec:
  ...
```

The following configuration parameters can be used within a `spec` section:

| Key                   | Type    | Description                                                                                                                                                           |
| --------------------- | ------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| enableDFS             | bool    | If this is true, DFS will be installed and enabled on all **searchHeads** and a spark cluster will be created.                                                        |
| sparkImage            | string  | Docker image to use for Spark instances (overrides SPARK_IMAGE environment variables)                                                                                 |
| splunkImage           | string  | Docker image to use for Splunk instances (overrides SPLUNK_IMAGE environment variables)                                                                               |
| splunkVolumes         | [Volumes](https://kubernetes.io/docs/concepts/storage/volumes/) | List of one or more [Kubernetes volumes](https://kubernetes.io/docs/concepts/storage/volumes/). These will be mounted in all Splunk containers as as `/mnt/&lt;name&gt;` |
| defaults              | string  | Inline map of [default.yml](https://github.com/splunk/splunk-ansible/blob/develop/docs/advanced/default.yml.spec.md) overrides used to initialize the environment     |
| defaultsUrl           | string  | Full path or URL for one or more [default.yml](https://github.com/splunk/splunk-ansible/blob/develop/docs/advanced/default.yml.spec.md) files, separated by commas    |
| licenseUrl            | string  | Full path or URL for a Splunk Enterprise license file                                                                                                                 |
| imagePullPolicy       | string  | Sets pull policy for all images (either "Always" or the default: "IfNotPresent")                                                                                      |
| storageClassName      | string  | Name of StorageClass to use for persistent volume claims                                                                                                              |
| schedulerName         | string  | Name of Scheduler to use for pod placement                                                                                                                            |
| affinity              | [Affinity](https://kubernetes.io/docs/concepts/configuration/assign-pod-node/#affinity-and-anti-affinity) | [Kubernetes Affinity](https://kubernetes.io/docs/concepts/configuration/assign-pod-node/#affinity-and-anti-affinity) rules that control how pods are assigned to particular nodes |


## Topology

```yaml
apiVersion: enterprise.splunk.com/v1alpha1
kind: SplunkEnterprise
metadata:
  name: example
spec:
  topology:
    ...
```

The following configuration parameters can be used within a `spec.topology` section:

| Key                   | Type    | Description                                                                                                                                                           |
| --------------------- | ------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| standalones           | integer | The number of standalone instances to deploy.                                                                                                                         |
| searchHeads           | integer | The number of search heads to deploy. If this number is greater than 0, a **deployer** will also be deployed.                                                         |
| indexers              | integer | The number of indexers to deploy. If this number is greater than 0, a **cluster master**  will also be deployed.                                                      |
| sparkWorkers          | integer | The number of spark workers to launch (defaults to 0 if **enableDFS** is false, or 1 if **enableDFS** is true)                                                        |

*Note: If `searchHeads` is greater than 0, then `indexers` must also be
greater than 0 (and vice versa), and a licence must be provided using
`licenseUrl` to create clusters.*


## Resources

```yaml
apiVersion: enterprise.splunk.com/v1alpha1
kind: SplunkEnterprise
metadata:
  name: example
spec:
  resources:
    ...
```

The following configuration parameters can be used within a `spec.resources` section:

| Key                   | Type    | Description                                                                                                                                                           |
| --------------------- | ------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| splunkCpuRequest      | string  | Sets the CPU request (minimum) for Splunk pods (default="0.1")                                                                                                        |
| sparkCpuRequest       | string  | Sets the CPU request (minimum) for Spark pods (default="0.1")                                                                                                         |
| splunkMemoryRequest   | string  | Sets the memory request (minimum) for Splunk pods (default="1Gi")                                                                                                     |
| sparkMemoryRequest    | string  | Sets the memory request (minimum) for Spark pods (default="1Gi")                                                                                                      |
| splunkCpuLimit        | string  | Sets the CPU limit (maximum) for Splunk pods (default="4")                                                                                                            |
| sparkCpuLimit         | string  | Sets the CPU limit (maximum) for Spark pods (default="4")                                                                                                             |
| splunkMemoryLimit     | string  | Sets the memory limit (maximum) for Splunk pods (default="8Gi")                                                                                                       |
| sparkMemoryLimit      | string  | Sets the memory limit (maximum) for Spark pods (default="8Gi")                                                                                                        |
| splunkEtcStorage      | string  | Storage capacity to request for Splunk etc volume claims (default="1Gi")                                                                                              |
| splunkVarStorage      | string  | Storage capacity to request for Splunk var volume claims (default="50Gi")                                                                                             |
| splunkIndexerStorage  | string  | Storage capacity to request for Splunk var volume claims on indexers (default="200Gi")                                                                                |
