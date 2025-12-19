---
title: Custom Resources
parent: Operate & Manage
nav_order: 1
---


# Custom Resource Guide

The Splunk Operator provides a collection of
[custom resources](https://kubernetes.io/docs/concepts/extend-kubernetes/api-extension/custom-resources/)
you can use to manage Splunk Enterprise deployments in your Kubernetes cluster.

- [Custom Resource Guide](#custom-resource-guide)
  - [Metadata Parameters](#metadata-parameters)
  - [Common Spec Parameters for All Resources](#common-spec-parameters-for-all-resources)
  - [Common Spec Parameters for Splunk Enterprise Resources](#common-spec-parameters-for-splunk-enterprise-resources)
  - [LicenseManager Resource Spec Parameters](#licensemanager-resource-spec-parameters)
  - [Standalone Resource Spec Parameters](#standalone-resource-spec-parameters)
  - [SearchHeadCluster Resource Spec Parameters](#searchheadcluster-resource-spec-parameters)
  - [Queue Resource Spec Parameters](#queue-resource-spec-parameters)
  - [ClusterManager Resource Spec Parameters](#clustermanager-resource-spec-parameters)
  - [IndexerCluster Resource Spec Parameters](#indexercluster-resource-spec-parameters)
  - [IngestorCluster Resource Spec Parameters](#ingestorcluster-resource-spec-parameters)
  - [ObjectStorage Resource Spec Parameters](#objectstorage-resource-spec-parameters)
  - [MonitoringConsole Resource Spec Parameters](#monitoringconsole-resource-spec-parameters)
  - [Examples of Guaranteed and Burstable QoS](#examples-of-guaranteed-and-burstable-qos)
    - [A Guaranteed QoS Class example:](#a-guaranteed-qos-class-example)
    - [A Burstable QoS Class example:](#a-burstable-qos-class-example)
    - [A BestEffort QoS Class example:](#a-besteffort-qos-class-example)
    - [Pod Resources Management](#pod-resources-management)

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
apiVersion: enterprise.splunk.com/v4
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
apiVersion: enterprise.splunk.com/v4
kind: Standalone
metadata:
  annotations:
    service.beta.kubernetes.io/azure-load-balancer-internal: "true"
  name: example
spec:
  imagePullPolicy: Always
  livenessInitialDelaySeconds: 400
  readinessInitialDelaySeconds: 390
  serviceTemplate:
    spec:
      type: LoadBalancer
  topologySpreadConstraints:
  - maxSkew: 1
    topologyKey: zone
    whenUnsatisfiable: DoNotSchedule
    labelSelector:
      matchLabels:
        foo: bar
  extraEnv:
  - name: ADDITIONAL_ENV_VAR_1
    value: "test_value_1"
  - name: ADDITIONAL_ENV_VAR_2
    value: "test_value_2"
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
| image                 | string     | Container image to use for pod instances (overrides `RELATED_IMAGE_SPLUNK_ENTERPRISE` environment variable |
| imagePullPolicy       | string     | Sets pull policy for all images (either "Always" or the default: "IfNotPresent")                           |
| livenessInitialDelaySeconds       | number     | Sets the initialDelaySeconds for Liveness probe (default: 300)                           |
| readinessInitialDelaySeconds       | number     | Sets the initialDelaySeconds for Readiness probe (default: 10)                           |
| extraEnv       | [EnvVar](https://v1-17.docs.kubernetes.io/docs/reference/generated/kubernetes-api/v1.17/#envvar-v1-core)     | Sets the extra environment variables to be passed to the Splunk instance containers. WARNING: Setting environment variables used by Splunk or Ansible will affect Splunk installation and operation                           |
| schedulerName         | string     | Name of [Scheduler](https://kubernetes.io/docs/concepts/scheduling/kube-scheduler/) to use for pod placement (defaults to "default-scheduler") |
| affinity              | [Affinity](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.17/#affinity-v1-core) | [Kubernetes Affinity](https://kubernetes.io/docs/concepts/configuration/assign-pod-node/#affinity-and-anti-affinity) rules that control how pods are assigned to particular nodes |
| resources             | [ResourceRequirements](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.17/#resourcerequirements-v1-core) | The settings for allocating [compute resource requirements](https://kubernetes.io/docs/concepts/configuration/manage-compute-resources-container/) to use for each pod instance. The default settings should be considered for demo/test purposes.  Please see [Hardware Resource Requirements](https://github.com/splunk/splunk-operator/blob/develop/docs/GettingStarted.md#hardware-resources-requirements) for production values.|
| serviceTemplate       | [Service](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.17/#service-v1-core) | Template used to create Kubernetes [Services](https://kubernetes.io/docs/concepts/services-networking/service/) |
| topologySpreadConstraint       | [TopologySpreadConstraint](https://kubernetes.io/docs/concepts/scheduling-eviction/topology-spread-constraints/) | Template used to create Kubernetes [TopologySpreadConstraint](https://kubernetes.io/docs/concepts/scheduling-eviction/topology-spread-constraints/) |

## Common Spec Parameters for Splunk Enterprise Resources

```yaml
apiVersion: enterprise.splunk.com/v4
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
  licenseManagerRef:
    name: example
  clusterManagerRef:
    name: example
  serviceAccount: custom-serviceaccount
```

The following additional configuration parameters may be used for all Splunk
Enterprise resources, including: `Standalone`, `LicenseManager`,
`SearchHeadCluster`, `ClusterManager`, `IndexerCluster` and `IngestorCluster`:

| Key                | Type    | Description                                                                   |
| ------------------ | ------- | ----------------------------------------------------------------------------- |
| etcVolumeStorageConfig | StorageClassSpec  | Storage class spec for Splunk etc volume as described in [StorageClass](StorageClass.md) |
| varVolumeStorageConfig | StorageClassSpec  | Storage class spec for Splunk var volume as described in [StorageClass](StorageClass.md) |
| volumes            | [Volume](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.17/#volume-v1-core) | List of one or more [Kubernetes volumes](https://kubernetes.io/docs/concepts/storage/volumes/). These will be mounted in all container pods as as `/mnt/<name>` |
| defaults           | string  | Inline map of [default.yml](https://github.com/splunk/splunk-ansible/blob/develop/docs/advanced/default.yml.spec.md) overrides used to initialize the environment |
| defaultsUrl        | string  | Full path or URL for one or more [default.yml](https://github.com/splunk/splunk-ansible/blob/develop/docs/advanced/default.yml.spec.md) files, separated by commas |
| licenseUrl         | string  | Full path or URL for a Splunk Enterprise license file                         |
| licenseManagerRef   | [ObjectReference](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.17/#objectreference-v1-core) | Reference to a Splunk Operator managed `LicenseManager` instance (via `name` and optionally `namespace`) to use for licensing |
| clusterManagerRef  | [ObjectReference](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.17/#objectreference-v1-core) | Reference to a Splunk Operator managed `ClusterManager` instance (via `name` and optionally `namespace`) to use for indexing |
| monitoringConsoleRef  | string     | Logical name assigned to the Monitoring Console pod. You can set the name before or after the MC pod creation.|
| serviceAccount | [ServiceAccount](https://kubernetes.io/docs/tasks/configure-pod-container/configure-service-account/) | Represents the service account used by the pods deployed by the CRD |
| extraEnv | Extra environment variables | Extra environment variables to be passed to the Splunk instance containers |
| readinessInitialDelaySeconds | readinessProbe [initialDelaySeconds](https://kubernetes.io/docs/tasks/configure-pod-container/configure-liveness-readiness-startup-probes/#define-readiness-probes) | Defines `initialDelaySeconds` for Readiness probe |
| livenessInitialDelaySeconds | livenessProbe [initialDelaySeconds](https://kubernetes.io/docs/tasks/configure-pod-container/configure-liveness-readiness-startup-probes/#define-a-liveness-command) | Defines `initialDelaySeconds` for the Liveness probe |
| imagePullSecrets | [imagePullSecrets](https://kubernetes.io/docs/tasks/configure-pod-container/pull-image-private-registry/) | Config to pull images from private registry. Use in conjunction with `image` config from [common spec](#common-spec-parameters-for-all-resources) |

## LicenseManager Resource Spec Parameters

```yaml
apiVersion: enterprise.splunk.com/v4
kind: LicenseManager
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
The `LicenseManager` resource does not provide any additional configuration parameters.


## Standalone Resource Spec Parameters

```yaml
apiVersion: enterprise.splunk.com/v4
kind: Standalone
metadata:
  name: standalone
  labels:
    app: SplunkStandAlone
    type: Splunk
  finalizers:
  - enterprise.splunk.com/delete-pvc
```

In addition to [Common Spec Parameters for All Resources](#common-spec-parameters-for-all-resources)
and [Common Spec Parameters for All Splunk Enterprise Resources](#common-spec-parameters-for-all-splunk-enterprise-resources),
the `Standalone` resource provides the following `Spec` configuration parameters:

| Key        | Type    | Description                                       |
| ---------- | ------- | ------------------------------------------------- |
| replicas   | integer | The number of standalone replicas (miminum of 1, which is the default) |


## SearchHeadCluster Resource Spec Parameters

```yaml
apiVersion: enterprise.splunk.com/v4
kind: SearchHeadCluster
metadata:
  name: example
spec:
  replicas: 5
```

In addition to [Common Spec Parameters for All Resources](#common-spec-parameters-for-all-resources)
and [Common Spec Parameters for All Splunk Enterprise Resources](#common-spec-parameters-for-all-splunk-enterprise-resources),
the `SearchHeadCluster` resource provides the following `Spec` configuration parameters:

| Key      | Type    | Description                                                  |
| -------- | ------- | ------------------------------------------------------------ |
| replicas | integer | The number of search heads cluster members (minimum of 3, which is the default) |

### Search Head Deployer Resource

Since Search Head Deployer doesn't require as many resources as Search Head Peers themselves, then Splunk Operator for Kubernetes 2.7.1 introduced additional field for SearchHeadCluster spec to manage resources for the deployer separately.

If provided, resources are managed separately for Search Head Deployer and Search Head Peers. Otherwise, either default values are used if resources are not defined at all or Search Head Peers resources are applied to Search Head Deployer as well.

Additionally, node affinity specification was introduced for Search Head Deployer to separate it from Search Head Peers specification.

| Key      | Type    | Description                                                  |
| -------- | ------- | ------------------------------------------------------------ |
| deployerNodeAffinity | *corev1.NodeAffinity | Search Head Deployer node affinity |
| deployerResourceSpec | corev1.ResourceRequirements | Search Head Deployer resource specification |

#### Example

```
deployerNodeAffinity:
  preferredDuringSchedulingIgnoredDuringExecution:
    ...
  requiredDuringSchedulingIgnoredDuringExecution:
    ...
deployerResourceSpec:
  claims:
    ...
  limits:
    ...
  requests:
    ...
```

```
apiVersion: enterprise.splunk.com/v4
kind: SearchHeadCluster
metadata:
  name: shc
  finalizers:
    - enterprise.splunk.com/delete-pvc
spec:
  image: splunk/splunk: 9.4.4
  serviceAccount: splunk-service-account
  resources:
    requests:
      memory: "1024Mi"
      cpu: "0.2"
    limits:
      memory: "10Gi"
      cpu: "6"
  deployerResourceSpec:
    requests:
      memory: "512Mi"
      cpu: "0.1"
    limits:
      memory: "8Gi"
      cpu: "4"
```

## Queue Resource Spec Parameters

```yaml
apiVersion: enterprise.splunk.com/v4
kind: Queue
metadata:
  name: queue
spec:
  replicas: 3
  provider: sqs
  sqs:
    name: sqs-test
    region: us-west-2
    endpoint: https://sqs.us-west-2.amazonaws.com
    dlq: sqs-dlq-test
```

Queue inputs can be found in the table below. As of now, only SQS provider of message queue is supported.

| Key        | Type    | Description                                       |
| ---------- | ------- | ------------------------------------------------- |
| provider   | string | [Required] Provider of message queue (Allowed values: sqs) |
| sqs   | SQS | [Required if provider=sqs] SQS message queue inputs  |

SQS message queue inputs can be found in the table below.

| Key        | Type    | Description                                       |
| ---------- | ------- | ------------------------------------------------- |
| name   | string | [Required] Name of the queue |
| region   | string | [Required] Region where the queue is located  |
| endpoint   | string | [Optional, if not provided formed based on region] AWS SQS Service endpoint
| dlq   | string | [Required] Name of the dead letter queue |

Change of any of the queue inputs triggers the restart of Splunk so that appropriate .conf files are correctly refreshed and consumed.

## ClusterManager Resource Spec Parameters
ClusterManager resource does not have a required spec parameter, but to configure SmartStore, you can specify indexes and volume configuration as below -
```yaml
apiVersion: enterprise.splunk.com/v4
kind: ClusterManager
metadata:
  name: example-cm
spec:
  smartstore:
    defaults:
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
apiVersion: enterprise.splunk.com/v4
kind: IndexerCluster
metadata:
  name: example
spec:
  replicas: 3
  clusterManagerRef: 
    name: example-cm
```
Note:  `clusterManagerRef` is required field in case of IndexerCluster resource since it will be used to connect the IndexerCluster to ClusterManager resource.

In addition to [Common Spec Parameters for All Resources](#common-spec-parameters-for-all-resources)
and [Common Spec Parameters for All Splunk Enterprise Resources](#common-spec-parameters-for-all-splunk-enterprise-resources),
the `IndexerCluster` resource provides the following `Spec` configuration parameters:

| Key        | Type    | Description                                           |
| ---------- | ------- | ----------------------------------------------------- |
| replicas   | integer | The number of indexer cluster members (minimum of 3, which is the default) |

## IngestorCluster Resource Spec Parameters

```yaml
apiVersion: enterprise.splunk.com/v4
kind: IngestorCluster
metadata:
  name: ic
spec:
  replicas: 3
  queueRef: 
    name: queue
  objectStorageRef:
    name: os
```
Note:  `queueRef` and `objectStorageRef` are required fields in case of IngestorCluster resource since they will be used to connect the IngestorCluster to Queue and ObjectStorage resources.

In addition to [Common Spec Parameters for All Resources](#common-spec-parameters-for-all-resources)
and [Common Spec Parameters for All Splunk Enterprise Resources](#common-spec-parameters-for-all-splunk-enterprise-resources),
the `IngestorCluster` resource provides the following `Spec` configuration parameters:

| Key        | Type    | Description                                           |
| ---------- | ------- | ----------------------------------------------------- |
| replicas   | integer | The number of ingestor peers (minimum of 3 which is the default) |

## ObjectStorage Resource Spec Parameters

```yaml
apiVersion: enterprise.splunk.com/v4
kind: ObjectStorage
metadata:
  name: os
spec:
  provider: s3
  s3:
    path: s3://ingestion/smartbus-test
    endpoint: https://s3.us-west-2.amazonaws.com
```

ObjectStorage inputs can be found in the table below. As of now, only S3 provider of large message store is supported.

| Key        | Type    | Description                                       |
| ---------- | ------- | ------------------------------------------------- |
| provider   | string | [Required] Provider of large message store (Allowed values: s3) |
| s3   | S3 | [Required if provider=s3] S3 large message store inputs  |

S3 large message store inputs can be found in the table below.

| Key        | Type    | Description                                       |
| ---------- | ------- | ------------------------------------------------- |
| path   | string | [Required] Remote storage location for messages that are larger than the underlying maximum message size  |
| endpoint   | string | [Optional, if not provided formed based on region] S3-compatible service endpoint

Change of any of the large message queue inputs triggers the restart of Splunk so that appropriate .conf files are correctly refreshed and consumed.

## MonitoringConsole Resource Spec Parameters

```yaml
cat <<EOF | kubectl apply -n splunk-operator -f -
apiVersion: enterprise.splunk.com/v3
kind: MonitoringConsole
metadata:
  name: example-mc
  finalizers:
  - enterprise.splunk.com/delete-pvc
EOF
```

Use the Monitoring Console to view detailed topology and performance information about your Splunk Enterprise deployment. See [What can the Monitoring Console do?](https://docs.splunk.com/Documentation/Splunk/latest/DMC/WhatcanDMCdo) in the Splunk Enterprise documentation. 

The Splunk Operator now includes a CRD for the Monitoring Console (MC). This offers a number of advantages available to other CR's, including: customizable resource allocation, app management, and license management. 

* An MC pod is not created automatically in the default namespace when using other Splunk Operator CR's. 
* When upgrading to the latest Splunk Operator, any previously automated MC pods will be deleted. 
* To associate a new MC pod with an existing CR, you must update any CR's and add the `monitoringConsoleRef` parameter. 

The MC pod is referenced by using the `monitoringConsoleRef` parameter. There is no preferred order when running an MC pod; you can start the pod before or after the other CR's in the namespace.  When a pod that references the `monitoringConsoleRef` parameter is created or deleted, the MC pod will automatically update itself and create or remove connections to those pods.


## Examples of Guaranteed and Burstable QoS

You can change the CPU and memory resources, and assign different Quality of Services (QoS) classes to your pods using the [Kubernetes Quality of Service section](README.md#using-kubernetes-quality-of-service-classes). Here are some examples:
  
### A Guaranteed QoS Class example:
Set equal ```requests``` and ```limits``` values for CPU and memory to establish a QoS class of Guaranteed. 

*Note: A pod will not start on a node that cannot meet the CPU and memory ```requests``` values.*

Example: The minimum resource requirements for a Standalone Splunk Enterprise instance in production are 24 vCPU and 12GB RAM. 

```yaml
apiVersion: enterprise.splunk.com/v4
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
Set the ```requests``` value for CPU and memory lower than the ```limits``` value to establish a QoS class of Burstable. 

Example: This Standalone Splunk Enterprise instance should start with minimal indexing and search capacity, but will be allowed to scale up if Kubernetes is able to allocate additional CPU and Memory up to the ```limits``` values.

```yaml
apiVersion: enterprise.splunk.com/v4
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

### A BestEffort QoS Class example:
With no requests or limits values set for CPU and memory, the QoS class is set to BestEffort. The BestEffort QoS is not recommended for use with Splunk Operator.

### Pod Resources Management

__CPU Throttling__

Kubernetes starts throttling CPUs if a pod's demand for CPU exceeds the value set in the ```limits``` parameter. If your nodes have extra CPU resources available, leaving the ```limits``` value unset will allow the pods to utilize more CPUs.

### Troubleshooting

#### CR Status Message
The Splunk Enterprise CRDs with the Splunk Operator have a field `cr.Status.message` which provides a detailed view of the CR's current status.

Here is an example of a Standalone with a message indicating an invalid CR config:

```
bash% kubectl get stdaln
NAME   PHASE   DESIRED   READY   AGE   MESSAGE
ido    Error   0         0       26s   invalid Volume Name for App Source: custom. volume: csh, doesn't exist

bash# kubectl get stdaln -o yaml | grep -i message -A 5 -B 5
      appsStatusMaxConcurrentAppDownloads: 5
      bundlePushStatus: {}
      isDeploymentInProgress: false
      lastAppInfoCheckTime: 0
      version: 0
    message: 'invalid Volume Name for App Source: custom. volume: csh, doesn''t exist'
    phase: Error
    readyReplicas: 0
    replicas: 0
    resourceRevMap: {}
    selector: ""
```
#### Pause Annotations
The Splunk Operator controller reconciles every Splunk Enterprise CR. However, there might be circumstances wherein the influence of the Splunk Operator is not desired and needs to be paused. Every Splunk Enterprise CR has its own pause annotation associated with it, which when configured ensures that the Splunk Operator controller reconcile is paused for it. Below is a table listing the pause annotations:

| Customer Resource Definition | Annotation |
| ----------- | --------- |
| queue.enterprise.splunk.com | "queue.enterprise.splunk.com/paused" |
| clustermaster.enterprise.splunk.com | "clustermaster.enterprise.splunk.com/paused" |
| clustermanager.enterprise.splunk.com | "clustermanager.enterprise.splunk.com/paused" |
| indexercluster.enterprise.splunk.com | "indexercluster.enterprise.splunk.com/paused" |
| ingestorcluster.enterprise.splunk.com | "ingestorcluster.enterprise.splunk.com/paused" |
| objectstorage.enterprise.splunk.com | "objectstorage.enterprise.splunk.com/paused" |
| licensemaster.enterprise.splunk.com | "licensemaster.enterprise.splunk.com/paused" |
| monitoringconsole.enterprise.splunk.com | "monitoringconsole.enterprise.splunk.com/paused" |
| searchheadcluster.enterprise.splunk.com | "searchheadcluster.enterprise.splunk.com/paused" |
| standalone.enterprise.splunk.com | "standalone.enterprise.splunk.com/paused" |

`Note: Removal of the annotation resets the default behavior`

Here is an example of a standalone with the pause annotation set. In this state, the Splunk Operator requeues the reconcillation without performing any reconcile operations unless the annotatation is removed.

```
apiVersion: enterprise.splunk.com/v4
kind: Standalone
metadata:
  name: test-only-debug
  namespace: splunk-operator
  annotations:
    standalone.enterprise.splunk.com/paused: "true"
  finalizers:
  - enterprise.splunk.com/delete-pvc
spec:
  replicas: 1
```

#### admin-managed-pv Annotations
The admin-managed-pv annotation in the splunk-operator's Custom Resource allows the admin to control whether Persistent Volumes (PVs) are dynamically created for the StatefulSet associated with the CR. If set to `true`, no PVs will be created, and the Persistent Volume Claim templates in the StatefulSet manifest will include a selector block to match `app.kubernetes.io/instance` and `app.kubernetes.io/name` labels for pre-created PVs. This means that `/opt/splunk/etc` and `/opt/splunk/var` related PVCs will contain code block like below 

```
apiVersion: v1
kind: PersistentVolumeClaim
...
  selector:
    matchLabels:
      app.kubernetes.io/instance: splunk-cm-cluster-manager
      app.kubernetes.io/name: cluster-manager
```

To match selector definition like this, Persistent Volume must set labels accordingly 

```
apiVersion: v1
kind: PersistentVolume
metadata:
  name: pv-example-etc
  labels:
    app.kubernetes.io/instance: splunk-cm-cluster-manager
    app.kubernetes.io/name: cluster-manager
```

When admin-managed-pv is set to `false`, PVs will be dynamically created as usual, providing dedicated persistent storage for the StatefulSet.

Here is an example of a Standalone with the admin-managed-pv annotation set. After 
```
apiVersion: enterprise.splunk.com/v4
kind: Standalone
metadata:
  name: single
  finalizers:
  - enterprise.splunk.com/delete-pvc
  annotations:
    enterprise.splunk.com/admin-managed-pv: "true"
```
##### PV label values
In order to prepare labels for CR's persistent volumes you need to know values beforehand
Below is a table listing `app.kubernetes.io/name` values mapped to CRDs
| Customer Resource Definition | app.kubernetes.io/name value |
| ----------- | --------- |
| clustermanager.enterprise.splunk.com | cluster-manager |
| clustermaster.enterprise.splunk.com | cluster-master |
| indexercluster.enterprise.splunk.com | indexer-cluster |
| ingestorcluster.enterprise.splunk.com | ingestor-cluster |
| licensemanager.enterprise.splunk.com | license-manager |
| licensemaster.enterprise.splunk.com | license-master |
| monitoringconsole.enterprise.splunk.com | monitoring-console |
| searchheadcluster.enterprise.splunk.com | search-head |
| standalone.enterprise.splunk.com | standalone |

`app.kubernetes.io/instance` value consist of three elements concatenated with hyphens
1. "splunk"
2. provided by admin CR name
3. CRD kind name

For example `clusterManager` CR named "test" will have set `app.kubernetes.io/instance` as `splunk-test-cluster-manager`

#### Container Logs
The Splunk Enterprise CRDs deploy Splunkd in Kubernetes pods running [docker-splunk](https://github.com/splunk/docker-splunk) container images. Adding a couple of environment variables to the CR spec as follows produces `detailed container logs`:

```
apiVersion: enterprise.splunk.com/v4
kind: Standalone
metadata:
  name: test-only
  namespace: splunk-operator
  finalizers:
  - enterprise.splunk.com/delete-pvc
spec:
  replicas: 1
  extraEnv:
  - name: DEBUG
    value: "true"
  - name: ANSIBLE_EXTRA_FLAGS
    value: "-vvvv
```

From the standalone above, here is a snippet from the detailed contianer log:
```
TASK [splunk_common : Ensure license path] *************************************
task path: /opt/ansible/roles/splunk_common/tasks/licenses/add_license.yml:15
ok: [localhost] => {
    "changed": false,
    "invocation": {
        "module_args": {
            "checksum_algorithm": "sha1",
            "follow": false,
            "get_attributes": true,
            "get_checksum": true,
            "get_md5": false,
            "get_mime": true,
            "path": "splunk.lic"
        }
    },
    "stat": {
        "exists": false
    }
}
```

__POD Eviction - OOM__

As oppose to throttling in case of CPU cycles starvation,  Kubernetes will evict a pod from the node if the pod's memory demands exceeds the value set in the ```limits``` parameter.
