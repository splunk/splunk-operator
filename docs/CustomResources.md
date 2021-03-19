# Custom Resource Guide

The Splunk Operator provides a collection of
[custom resources](https://kubernetes.io/docs/concepts/extend-kubernetes/api-extension/custom-resources/)
you can use to manage Splunk Enterprise deployments in your Kubernetes cluster.

- [Custom Resource Guide](#custom-resource-guide)
  - [Metadata Parameters](#metadata-parameters)
  - [Common Spec Parameters for All Resources](#common-spec-parameters-for-all-resources)
  - [Common Spec Parameters for Splunk Enterprise Resources](#common-spec-parameters-for-splunk-enterprise-resources)
  - [Spark Resource Spec Parameters](#spark-resource-spec-parameters)
  - [LicenseMaster Resource Spec Parameters](#licensemaster-resource-spec-parameters)
  - [Standalone Resource Spec Parameters](#standalone-resource-spec-parameters)
  - [SearchHeadCluster Resource Spec Parameters](#searchheadcluster-resource-spec-parameters)
  - [ClusterMaster Resource Spec Parameters](#clustermaster-resource-spec-parameters)
  - [IndexerCluster Resource Spec Parameters](#indexercluster-resource-spec-parameters)
  - [Kubernetes Quality of Service classes](#kubernetes-quality-of-service-classes)

For examples on how to use these custom resources, please see
[Configuring Splunk Enterprise Deployments](Examples.md).


## Metadata Parameters

All resources in Kubernetes include a `metadata` section. You can use this
to define a name for a specific instance of the resource, and which namespace
you would like the resource to reside within:

| Key       | Type   | Description                                                                                                 |
| --------- | ------ | ----------------------------------------------------------------------------------------------------------- |
| name      | string | Each instance of your resource is distinguished using this name.                                            |
| namespace | string | Your instance will be created within this namespace. You must ensure that this namespace exists beforehand. |

If you do not provide a `namespace`, you current context will be used.

```yaml
apiVersion: enterprise.splunk.com/v1beta1
kind: Standalone
metadata:
  name: s1
  namespace: splunk
  finalizers:
  - enterprise.splunk.com/delete-pvc
```

The `enterprise.splunk.com/delete-pvc` finalizer is optional, and may be
used to tell the Splunk Operator that you would like it to remove all the
[Persistent Volumes](https://kubernetes.io/docs/concepts/storage/persistent-volumes/)
associated with the instance when you delete it.


## Common Spec Parameters for All Resources

```yaml
apiVersion: enterprise.splunk.com/v1beta1
kind: Standalone
metadata:
  name: example
spec:
  imagePullPolicy: Always
  resources:
    requests:
      memory: "512Mi"
      cpu: "0.1"
    limits:
      memory: "8Gi"
      cpu: "4"
```

The `spec` section is used to define the desired state for a resource. All
custom resources provided by the Splunk Operator include the following
configuration parameters:

| Key                   | Type       | Description                                                                                                |
| --------------------- | ---------- | ---------------------------------------------------------------------------------------------------------- |
| image                 | string     | Container image to use for pod instances (overrides `RELATED_IMAGE_SPLUNK_ENTERPRISE` or `RELATED_IMAGE_SPLUNK_SPARK` environment variables) |
| imagePullPolicy       | string     | Sets pull policy for all images (either "Always" or the default: "IfNotPresent")                           |
| schedulerName         | string     | Name of [Scheduler](https://kubernetes.io/docs/concepts/scheduling/kube-scheduler/) to use for pod placement (defaults to "default-scheduler") |
| affinity              | [Affinity](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.17/#affinity-v1-core) | [Kubernetes Affinity](https://kubernetes.io/docs/concepts/configuration/assign-pod-node/#affinity-and-anti-affinity) rules that control how pods are assigned to particular nodes |
| resources             | [ResourceRequirements](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.17/#resourcerequirements-v1-core) | CPU and memory [compute resource requirements](https://kubernetes.io/docs/concepts/configuration/manage-compute-resources-container/) to use for each pod instance (defaults shown in example above). These resources should be set to values that meet the SLOs of the Splunk Deployment. The guildelines for CPU/Mem resources are given in the section [Setting CPU/Mem resources](#cpu-mem-spec-for-splunk-enterprise-resources)  |
| serviceTemplate       | [Service](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.17/#service-v1-core) | Template used to create Kubernetes [Services](https://kubernetes.io/docs/concepts/services-networking/service/) |

## Common Spec Parameters for Splunk Enterprise Resources

```yaml
apiVersion: enterprise.splunk.com/v1beta1
kind: Standalone
metadata:
  name: example
spec:
  etcVolumeStorageConfig:
    storageClassName: gp2
    storageCapacity: 15Gi
  varVolumeStorageConfig:
    storageClassName: customStorageClass
    storageCapacity: 25Gi
  volumes:
    - name: licenses
      configMap:
        name: splunk-licenses
  licenseMasterRef:
    name: example
  clusterMasterRef:
    name: example
  serviceAccount: custom-serviceaccount
```

The following additional configuration parameters may be used for all Splunk
Enterprise resources, including: `Standalone`, `LicenseMaster`,
`SearchHeadCluster`, `ClusterMaster` and `IndexerCluster`:

| Key                | Type    | Description                                                                   |
| ------------------ | ------- | ----------------------------------------------------------------------------- |
| etcVolumeStorageConfig | StorageClassSpec  | Storage class spec for Splunk etc volume as described in [StorageClass](StorageClass.md) |
| varVolumeStorageConfig | StorageClassSpec  | Storage class spec for Splunk var volume as described in [StorageClass](StorageClass.md) |
| volumes            | [[]Volume](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.17/#volume-v1-core) | List of one or more [Kubernetes volumes](https://kubernetes.io/docs/concepts/storage/volumes/). These will be mounted in all container pods as as `/mnt/<name>` |
| defaults           | string  | Inline map of [default.yml](https://github.com/splunk/splunk-ansible/blob/develop/docs/advanced/default.yml.spec.md) overrides used to initialize the environment |
| defaultsUrl        | string  | Full path or URL for one or more [default.yml](https://github.com/splunk/splunk-ansible/blob/develop/docs/advanced/default.yml.spec.md) files, separated by commas |
| licenseUrl         | string  | Full path or URL for a Splunk Enterprise license file                         |
| licenseMasterRef   | [ObjectReference](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.17/#objectreference-v1-core) | Reference to a Splunk Operator managed `LicenseMaster` instance (via `name` and optionally `namespace`) to use for licensing |
| clusterMasterRef  | [ObjectReference](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.17/#objectreference-v1-core) | Reference to a Splunk Operator managed `ClusterMaster` instance (via `name` and optionally `namespace`) to use for indexing |
| serviceAccount | [ServiceAccount](https://kubernetes.io/docs/tasks/configure-pod-container/configure-service-account/) | Represents the service account used by the pods deployed by the CRD |

## Spark Resource Spec Parameters

```yaml
apiVersion: enterprise.splunk.com/v1beta1
kind: Spark
metadata:
  name: example
spec:
  replicas: 3
```

In addition to [Common Spec Parameters for All Resources](#common-spec-parameters-for-all-resources),
the `Spark` resource provides the following `Spec` configuration parameters:

| Key      | Type    | Description                                      |
| -------- | ------- | ------------------------------------------------ |
| replicas | integer | The number of spark workers pods (defaults to 1) |


## LicenseMaster Resource Spec Parameters

```yaml
apiVersion: enterprise.splunk.com/v1beta1
kind: LicenseMaster
metadata:
  name: example
spec:
  volumes:
    - name: licenses
      configMap:
        name: splunk-licenses
  licenseUrl: /mnt/licenses/enterprise.lic
```

Please see [Common Spec Parameters for All Resources](#common-spec-parameters-for-all-resources)
and [Common Spec Parameters for All Splunk Enterprise Resources](#common-spec-parameters-for-all-splunk-enterprise-resources).
The `LicenseMaster` resource does not provide any additional configuration parameters.


## Standalone Resource Spec Parameters

```yaml
apiVersion: enterprise.splunk.com/v1beta1
kind: Standalone
metadata:
  name: example
spec:
  sparkImage: splunk/spark:edge
  sparkRef:
    name: example
```

In addition to [Common Spec Parameters for All Resources](#common-spec-parameters-for-all-resources)
and [Common Spec Parameters for All Splunk Enterprise Resources](#common-spec-parameters-for-all-splunk-enterprise-resources),
the `Standalone` resource provides the following `Spec` configuration parameters:

| Key        | Type    | Description                                       |
| ---------- | ------- | ------------------------------------------------- |
| replicas   | integer | The number of standalone replicas (defaults to 1) |
| sparkImage | string  | Container image Data Fabric Search (DFS) will use for JDK and Spark libraries (overrides `RELATED_IMAGE_SPLUNK_SPARK` environment variables) |
| sparkRef   | [ObjectReference](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.17/#objectreference-v1-core) | Reference to a Splunk Operator managed `Spark` instance (via `name` and optionally `namespace`). When defined, Data Fabric Search (DFS) will be enabled and configured to use it. |


## SearchHeadCluster Resource Spec Parameters

```yaml
apiVersion: enterprise.splunk.com/v1beta1
kind: SearchHeadCluster
metadata:
  name: example
spec:
  replicas: 5
  sparkImage: splunk/spark:edge
  sparkRef:
    name: example
```

In addition to [Common Spec Parameters for All Resources](#common-spec-parameters-for-all-resources)
and [Common Spec Parameters for All Splunk Enterprise Resources](#common-spec-parameters-for-all-splunk-enterprise-resources),
the `SearchHeadCluster` resource provides the following `Spec` configuration parameters:

| Key        | Type    | Description                                                                     |
| ---------- | ------- | ------------------------------------------------------------------------------- |
| replicas   | integer | The number of search heads cluster members (minimum of 3, which is the default) |
| sparkImage | string  | Container image Data Fabric Search (DFS) will use for JDK and Spark libraries (overrides `RELATED_IMAGE_SPLUNK_SPARK` environment variables) |
| sparkRef   | [ObjectReference](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.17/#objectreference-v1-core) | Reference to a Splunk Operator managed `Spark` instance (via `name` and optionally `namespace`). When defined, Data Fabric Search (DFS) will be enabled and configured to use it. |

## ClusterMaster Resource Spec Parameters
ClusterMaster resource does not have a required spec parameter, but to configure SmartStore, you can specify indexes and volume configuration as below -
```yaml
apiVersion: enterprise.splunk.com/v1beta1
kind: ClusterMaster
metadata:
  name: example-cm
spec:
  smartstore:
    defaults:
        remotePath: $_index_name
        volumeName: msos_s2s3_vol
    indexes:
      - name: salesdata1
        remotePath: $_index_name
        volumeName: msos_s2s3_vol
      - name: salesdata2
        remotePath: $_index_name
        volumeName: msos_s2s3_vol
      - name: salesdata3
        remotePath: $_index_name
        volumeName: msos_s2s3_vol
    volumes:
      - name: msos_s2s3_vol
        path: <remote path>
        endpoint: <remote endpoint>
        secretRef: s3-secret
```

## IndexerCluster Resource Spec Parameters

```yaml
apiVersion: enterprise.splunk.com/v1beta1
kind: IndexerCluster
metadata:
  name: example
spec:
  replicas: 3
  clusterMasterRef: 
    name: example-cm
```
Note:  `clusterMasterRef` is required field in case of IndexerCluster resource since it will be used to connect the IndexerCluster to ClusterMaster resource.

In addition to [Common Spec Parameters for All Resources](#common-spec-parameters-for-all-resources)
and [Common Spec Parameters for All Splunk Enterprise Resources](#common-spec-parameters-for-all-splunk-enterprise-resources),
the `IndexerCluster` resource provides the following `Spec` configuration parameters:

| Key        | Type    | Description                                           |
| ---------- | ------- | ----------------------------------------------------- |
| replicas   | integer | The number of indexer cluster members (defaults to 1) |


## Kubernetes Quality of Service classes:

| QoS        | Summary| Description    
| ---------- | ------- | ------- |
| Guaranteed | CPU/Mem ```requests``` = CPU/Mem ```limits```    | When the CPU and memory  ```requests``` and ```limits``` values are equal, the pod is given a QoS class of Guaranteed. This level of service is recommended for Splunk Enterprise production environments. |
| Burstable | CPU/Mem ```requests``` < CPU/Mem ```limits```  | When the CPU and memory  ```requests``` value is set lower than the ```limits``` the pod is given a QoS class of Burstable. This level of service is useful in a user acceptance testing (UAT) environment, where the pods run with minimum resources, and Kubernetes allocates additional resources depending on usage. |
| BestEffort | No CPU/Mem ```requests``` or ```limits``` are set | When the ```requests``` or ```limits``` values are not set, the pod is given a QoS class of BestEffort. This level of service is sufficient for testing, or a small development task. |  
  
### A Guaranteed QoS Class example:

Let's look into what should be the setting for CPU/Mem resources based on the [Reference Hardware](https://docs.splunk.com/Documentation/Splunk/8.1.2/Capacity/Referencehardware) for a standalone splunk pod. The minimumn resources requirement is "12 physical CPU cores, or 24 vCPU at 2Ghz or greater speed per core. 12GB RAM".
Assuming vCPUs, in order to make sure Kubernetes schedule the Splunk POD in a node that has above reference resources bandwith, set the CPU and Memory values for ```requests``` to 24 and 12Gi respectively. Make sure the same values are used for ```limits``` parameter also. So the yaml file will look like:

```yaml
apiVersion: enterprise.splunk.com/v1
kind: Standalone
metadata:
  name: example
spec:
  imagePullPolicy: Always
  resources:
    requests:
      memory: "12Gi"
      cpu: "24"
    limits:
      memory: "12Gi"
      cpu: "24"  
```

### A Burstable QoS Class example:
Using the similar hardware reference from the last example, you can have a Burstable QoS service by letting the Kubernetes schedule the Splunk POD in a node that can serve the CPU/Mem providided in the ```requests``` section. However, set the CPU and Memory values for ```limits``` to 24 and 12Gi respectively. 

```yaml
apiVersion: enterprise.splunk.com/v1
kind: Standalone
metadata:
  name: example
spec:
  imagePullPolicy: Always
  resources:
    requests:
      memory: "2Gi"
      cpu: "4"
    limits:
      memory: "12Gi"
      cpu: "24"  
```

In the above example, Splunk POD will be able to start serving the searches and ingestion that can be served by the 4 CPU cores and 2Gi memory. Subsequently, as the load grows, kubernetes will allocate additional CPU cores upto the ```limits``` of 24 CPU cores, as well as additional memory upto 12Gi.


### A BestEffort QoS Class example:

```yaml
apiVersion: enterprise.splunk.com/v1
kind: Standalone
metadata:
  name: example
spec:
  imagePullPolicy: Always

```

### Pod resource management 

__CPU Throttling__

In the QoS examples, Kubernetes will begin throttling CPUs if the pod's demand for CPU  exceeds the value set in the ```limits``` parameter. If your nodes have extra CPU resources available, leaving the ```limits``` value unset will allow the pods to utilize more CPUs.

__POD Eviction - OOM__

In the QoS examples, Kubernetes will evict a pod from the node if the pod's memory demands exceeds the value set in the ```limits``` parameter. To avoid pod eviction due to memory usage, set the ```requests``` and ```limits``` values for memory to the same value.

