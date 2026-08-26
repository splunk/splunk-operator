---
title: Custom Resources
parent: Operate & Manage
nav_order: 1
permalink: /docs/CustomResources.html
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
  - [Status Conditions](#status-conditions)
  - [Troubleshooting](#troubleshooting)

For examples on how to use these custom resources, please see
[Configuring Splunk Enterprise Deployments](../reference/Examples.md).


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
  disableResourceDefaults: false
  imagePullPolicy: Always
  livenessInitialDelaySeconds: 400
  readinessInitialDelaySeconds: 390
  podAnnotations:
    traffic.sidecar.istio.io/excludeOutboundPorts: "8089,8191,9997,15020"
    traffic.sidecar.istio.io/includeInboundPorts: "8000,8088,15021"
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
| disableResourceDefaults | boolean | Prevents the operator from filling in default CPU and memory requests and limits. Defaults to `false`. Set to `true` to preserve `resources` exactly as provided, including an empty value. |
| livenessInitialDelaySeconds       | number     | Sets the initialDelaySeconds for Liveness probe (default: 300)                           |
| readinessInitialDelaySeconds       | number     | Sets the initialDelaySeconds for Readiness probe (default: 10)                           |
| extraEnv       | [EnvVar](https://v1-17.docs.kubernetes.io/docs/reference/generated/kubernetes-api/v1.17/#envvar-v1-core)     | Sets the extra environment variables to be passed to the Splunk instance containers. WARNING: Setting environment variables used by Splunk or Ansible will affect Splunk installation and operation                           |
| schedulerName         | string     | Name of [Scheduler](https://kubernetes.io/docs/concepts/scheduling/kube-scheduler/) to use for pod placement (defaults to "default-scheduler") |
| podAnnotations        | map[string]string | Sets annotations on Splunk instance pods. These annotations can override operator-provided pod annotations, including the Istio sidecar traffic annotations. |
| affinity              | [Affinity](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.17/#affinity-v1-core) | [Kubernetes Affinity](https://kubernetes.io/docs/concepts/configuration/assign-pod-node/#affinity-and-anti-affinity) rules that control how pods are assigned to particular nodes |
| resources             | [ResourceRequirements](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.17/#resourcerequirements-v1-core) | The settings for allocating [compute resource requirements](https://kubernetes.io/docs/concepts/configuration/manage-compute-resources-container/) to use for each pod instance. Missing CPU and memory keys receive operator defaults unless `disableResourceDefaults` is `true`. The default settings should be considered for demo/test purposes. Please see [Hardware Resource Requirements](https://github.com/splunk/splunk-operator/blob/develop/docs/GettingStarted.md#hardware-resources-requirements) for production values.|
| serviceTemplate       | [Service](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.17/#service-v1-core) | Template used to create Kubernetes [Services](https://kubernetes.io/docs/concepts/services-networking/service/) |
| topologySpreadConstraint       | [TopologySpreadConstraint](https://kubernetes.io/docs/concepts/scheduling-eviction/topology-spread-constraints/) | Template used to create Kubernetes [TopologySpreadConstraint](https://kubernetes.io/docs/concepts/scheduling-eviction/topology-spread-constraints/) |

### Postgres Node Sidecar

Starting with Splunk Enterprise 10.6, SOK disables the Postgres node sidecar by default on all managed pods by injecting `SPLUNK_NODE_SIDECAR_POSTGRES_DISABLED=true`. No SOK-supported use case requires the sidecar on Kubernetes (DMX, SPL2, and Data Orchestrator are unsupported on CMP-K), and on 10.4+ it would otherwise silently start a database accumulating data in an unsupported configuration.

To re-enable the sidecar, set the variable to a different value in `spec.extraEnv`:

```yaml
spec:
  extraEnv:
    - name: SPLUNK_NODE_SIDECAR_POSTGRES_DISABLED
      value: "false"
```

> **Note:** This override may be removed in a future SOK release. Enabling the Postgres sidecar in SOK deployments is not officially supported.

### KV Store Default Type

SOK configures Splunk Enterprise pods to use local KV Store by default by injecting `SPLUNK_KVSTORE_DEFAULT_TYPE=local` into managed pods, except for `IngestorCluster`. Splunk Ansible consumes this variable and writes `[kvstore] defaultKVStoreType` to `server.conf`. This setting requires Splunk Enterprise 10.6.0 or later.

The only supported value is `local`. If the variable is set in `spec.extraEnv`, it must use the same value:

```yaml
spec:
  extraEnv:
    - name: SPLUNK_KVSTORE_DEFAULT_TYPE
      value: "local"
```

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
| etcVolumeStorageConfig | StorageClassSpec  | Storage class spec for Splunk etc volume as described in [StorageClass](../deploy/StorageClass.md) |
| varVolumeStorageConfig | StorageClassSpec  | Storage class spec for Splunk var volume as described in [StorageClass](../deploy/StorageClass.md) |
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
| certs | []CertSpec | List of TLS certificates to mount into Splunk pods. Each entry references a Kubernetes Secret containing `tls.crt`, `tls.key`, and optionally `ca.crt`. An optional `role` field (`server` or `input`) controls the mount path: `server` mounts at `/mnt/tls/splunk-server-tls-cert/`, `input` at `/mnt/tls/splunk-input-tls-cert/`, and no role mounts at `/mnt/tls/<secretName>/`. When a referenced Secret's content changes, the operator automatically triggers a rolling restart. **Note:** cert secret rotation detection is supported for v4 CR types only (`Standalone`, `LicenseManager`, `SearchHeadCluster`, `ClusterManager`, `IndexerCluster`, `MonitoringConsole`, `IngestorCluster`). The deprecated v3 types (`ClusterMaster`, `LicenseMaster`) carry the `certs` field but do not watch for Secret changes. |

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
| secretKeyRef | object | [Optional] Per-key selectors for AWS credentials. Contains `awsAccessKey` and `awsSecretKey`, each a `SecretKeySelector` with `name` (Secret name) and `key` (key within the Secret). When not set, IRSA / workload identity is assumed. |

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
    path: ingestion/smartbus-test
    endpoint: https://s3.us-west-2.amazonaws.com
```

ObjectStorage inputs can be found in the table below. As of now, only S3 provider of object storage is supported.

| Key        | Type    | Description                                       |
| ---------- | ------- | ------------------------------------------------- |
| provider   | string | [Required] Provider of object storage (Allowed values: s3) |
| s3   | S3 | [Required if provider=s3] S3 object storage inputs  |

S3 object storage inputs can be found in the table below.

| Key        | Type    | Description                                       |
| ---------- | ------- | ------------------------------------------------- |
| path   | string | [Required] Remote storage location for messages that are larger than the underlying maximum message size  |
| endpoint   | string | [Optional, if not provided formed based on region] S3-compatible service endpoint |
| encryptionScheme | string | [Optional] Encryption scheme used by remote storage. Allowed values: `sse-s3`, `sse-c`, `none` |
| kmsEndpoint | string | [Optional] KMS endpoint for generating data keys. Required when `encryptionScheme` is `sse-c`; auto-derived from region if not provided |
| kmsKeyId | string | [Optional] ID of the primary KMS key (UUID, alias, or ARN). Required when `encryptionScheme` is `sse-c` |

Change of any of the object storage inputs triggers the restart of Splunk so that appropriate .conf files are correctly refreshed and consumed.

## MonitoringConsole Resource Spec Parameters

```yaml
cat <<EOF | kubectl apply -n splunk-operator -f -
apiVersion: enterprise.splunk.com/v4
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

You can change the CPU and memory resources, and assign different Quality of Services (QoS) classes to your pods. Here are some examples:
  
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
Set `disableResourceDefaults` to `true` and omit requests and limits to prevent the operator from adding resource defaults:

```yaml
apiVersion: enterprise.splunk.com/v4
kind: Standalone
metadata:
  name: example
spec:
  disableResourceDefaults: true
  resources: {}
```

With no requests or limits set for any container, Kubernetes assigns the pod the BestEffort QoS class. BestEffort QoS is not recommended for Splunk Enterprise production workloads. A namespace `LimitRange` may add resources during pod admission and result in a different QoS class.

### Pod Resources Management

__CPU Throttling__

Kubernetes starts throttling CPUs if a pod's demand for CPU exceeds the value set in the ```limits``` parameter. If your nodes have extra CPU resources available, leaving the ```limits``` value unset will allow the pods to utilize more CPUs.

## Status Conditions

All Splunk Enterprise Custom Resources include Kubernetes-standard [status conditions](https://kubernetes.io/docs/concepts/workloads/pods/pod-lifecycle/#pod-conditions) that provide detailed information about the resource state. These conditions follow Kubernetes conventions and can be used for monitoring, alerting, and automation.

### Condition Types

| Condition Type | Description |
|----------------|-------------|
| `Ready` | Indicates whether the resource is fully operational and all replicas are ready |
| `Progressing` | Indicates whether the resource is being updated, scaled, or initialized |
| `Paused` | Indicates whether reconciliation is paused via the pause annotation |
| `Stalled` | Indicates a non-recoverable failure that requires user intervention before reconciliation can resume |

### Condition Fields

Each condition includes the following fields:

| Field | Description |
|-------|-------------|
| `type` | The condition type (Ready, Progressing, Paused, or Stalled) |
| `status` | Either "True", "False", or "Unknown" |
| `reason` | A machine-readable reason code for the condition's state |
| `message` | A human-readable description of the condition |
| `lastTransitionTime` | The last time the condition status changed |
| `observedGeneration` | The generation of the CR spec that was observed |

### Example Status with Conditions

```yaml
status:
  phase: Ready
  conditions:
    - type: Ready
      status: "True"
      reason: ReconcileComplete
      message: Resource is ready
      lastTransitionTime: "2026-05-04T10:00:00Z"
      observedGeneration: 3
    - type: Progressing
      status: "False"
      reason: Stable
      message: Resource is stable
      lastTransitionTime: "2026-05-04T09:55:00Z"
      observedGeneration: 3
    - type: Paused
      status: "False"
      reason: NotPaused
      message: Reconciliation is not paused
      lastTransitionTime: "2026-05-04T08:00:00Z"
      observedGeneration: 3
    - type: Stalled
      status: "False"
      reason: NotStalled
      message: ""
      lastTransitionTime: "2026-05-04T08:00:00Z"
      observedGeneration: 3
```

When a terminal failure is detected, `Stalled` flips to `True`:

```yaml
status:
  phase: Error
  conditions:
    - type: Ready
      status: "False"
      reason: ReconcileFailed
      message: Pod stuck in terminal state — manual fix required
      lastTransitionTime: "2026-05-04T11:00:00Z"
      observedGeneration: 4
    - type: Progressing
      status: "False"
      reason: ReconcileFailed
      message: Pod stuck in terminal state — manual fix required
      lastTransitionTime: "2026-05-04T11:00:00Z"
      observedGeneration: 4
    - type: Paused
      status: "False"
      reason: NotPaused
      message: Reconciliation is not paused
      lastTransitionTime: "2026-05-04T08:00:00Z"
      observedGeneration: 4
    - type: Stalled
      status: "True"
      reason: PodTerminalFailure
      message: Pod stuck in terminal state — manual fix required
      lastTransitionTime: "2026-05-04T11:00:00Z"
      observedGeneration: 4
```

### Checking Conditions

You can view conditions using kubectl:

```bash
kubectl get standalone example -o jsonpath='{.status.conditions}' | jq .
```

Or describe the resource:

```bash
kubectl describe standalone example
```

### Condition Behavior

- **`lastTransitionTime`** only updates when the condition's `status` field changes (e.g., from "False" to "True"), not on every reconcile
- **`observedGeneration`** reflects which spec generation the controller has processed
- When an error occurs, the `Ready` condition's `message` field contains the specific error description
- **`Stalled=True`** signals a non-recoverable failure: the operator has stopped requeueing the CR and will not retry until the user resolves the root cause. `Stalled` is always `False` when `phase` is not `Error` — `Ready=True` and `Stalled=True` can never coexist
- Use `Stalled=True` in monitoring or alerting rules to page on failures that need human intervention, as opposed to transient errors that self-heal
- A **Warning** event with reason `Stalled` is emitted on **every** reconcile where `Stalled=True` (not only on the initial flip); a **Normal** event with reason `StalledResolved` is emitted once when the condition clears from `True` to `False`. Both are visible via `kubectl describe`

## Troubleshooting

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
#### Terminal Failures

Some failure states are non-recoverable without external intervention. When the operator detects one, it stops reconciling the CR immediately — the CR is **not requeued** — and sets `status.phase` to `Error` with `Stalled=True` in the status conditions. The CR remains in this state until the root cause is resolved and the operator detects the change.

**What triggers a terminal failure**

| Cause | `Stalled` condition message | Affected CRs |
|-------|----------------------------|---------------|
| A container is stuck in a non-recoverable waiting state: `ErrImagePull`, `ImagePullBackOff`, `InvalidImageName`, `ErrInvalidImage`, `CreateContainerConfigError`, `CreateContainerError`, or `RunContainerError` | `Pod stuck in terminal state — manual fix required` | All |
| The TLS Secret referenced by `spec.certs[]` is missing a required key (`tls.crt` or `tls.key`) | `cert secret <namespace>/<name> is missing required key "<key>"` | All |
| The CR spec fails validation during reconciliation (e.g. missing required field, invalid value) | `<CR type> spec validation failed` | All |
| The Queue or ObjectStorage CR referenced by an IndexerCluster or IngestorCluster cannot be found | `Referenced Queue or ObjectStorage CR not found` | IndexerCluster, IngestorCluster |
| `queueRef` or `objectStorageRef` is removed after having been applied | `queueRef and objectStorageRef cannot be removed once applied` | IndexerCluster, IngestorCluster |
| `clusterManagerRef` is empty at the point where it is required at runtime | `empty Cluster Manager reference` | IndexerCluster |

**Detecting a terminal failure**

When a terminal failure occurs, `status.phase` is `Error` and the `Stalled` condition flips to `True`. A Kubernetes **Warning** event with reason `Stalled` is also emitted and is visible in `kubectl describe`. Check the conditions directly:

```bash
kubectl get standalone example -o jsonpath='{.status.conditions}' | jq .
```

Or filter for the `Stalled` condition specifically:

```bash
kubectl get standalone example -o jsonpath='{.status.conditions[?(@.type=="Stalled")]}' | jq .
```

The `Stalled` condition `message` field describes the failure. For pod-level failures, check the pod status for more detail:

```bash
kubectl describe pod <pod-name> -n <namespace>
```

**Recovery**

Once the root cause is resolved and the operator successfully reconciles the CR, the `Stalled` condition is cleared and a Kubernetes **Normal** event with reason `StalledResolved` is emitted.

For a pod stuck in a terminal container state:
1. Inspect the failing pod with `kubectl describe pod <pod-name> -n <namespace>` to read the `Waiting.Reason` and `Waiting.Message`.
2. Fix the root cause (correct the image tag, provide the missing `imagePullSecret`, create the missing Secret or ConfigMap).
3. Delete the stuck pods — the StatefulSet controller recreates them and the operator resumes reconciliation.

```bash
kubectl delete pod <stuck-pod-name> -n <namespace>
```

For a malformed TLS Secret:
1. Update or recreate the Secret to include both `tls.crt` and `tls.key`.
2. The operator detects the fix and resumes automatically on the next reconcile cycle.

For a missing Queue or ObjectStorage CR (IndexerCluster, IngestorCluster):
1. Create the missing CR in the same namespace as the IndexerCluster or IngestorCluster.
2. The operator resumes automatically on the next reconcile cycle.

For a spec validation failure:
1. Check the `Stalled` condition `message` and operator logs to identify the invalid field.
2. Correct the spec with `kubectl edit` or `kubectl patch`.
3. The operator processes the spec change and resumes reconciliation automatically.

For immutable refs cleared (`queueRef`/`objectStorageRef` removed after being applied):
1. Restore the previous `queueRef` and `objectStorageRef` values in the CR spec.
2. Apply the corrected spec — the operator resumes automatically.

For an empty ClusterManager reference (IndexerCluster):
1. Ensure `spec.clusterManagerRef.name` is set on the IndexerCluster.
2. Apply the corrected spec — the operator resumes automatically.

#### Pause Annotations
The Splunk Operator controller reconciles every Splunk Enterprise CR. However, there might be circumstances wherein the influence of the Splunk Operator is not desired and needs to be paused. Every Splunk Enterprise CR has its own pause annotation associated with it, which when configured ensures that the Splunk Operator controller reconcile is paused for it. Below is a table listing the pause annotations:

**What pausing affects:**
When a CR is paused, the Splunk Operator returns before normal reconcile work after updating the `Paused` condition — no spec changes are applied, no scaling operations are performed, no app installs or upgrades are triggered, and no rolling restarts are initiated. The CR is requeued periodically, but only the pause/status check runs until the annotation is removed.

**What pausing does not affect:**
Pausing only suspends the Splunk Operator's reconcile loop for that CR. Standard Kubernetes components continue operating normally: the kubelet still enforces liveness, readiness, and startup probes and will restart containers that fail them; the StatefulSet controller still manages pod replacement if pods are deleted; and any other controllers (e.g. cert-manager, ingress) continue their own reconciliation. Pausing does not stop or freeze the running Splunk pods themselves.

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

Here is an example of a standalone with the pause annotation set. In this state, the Splunk Operator requeues the reconciliation without performing any reconcile operations unless the annotation is removed.

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
