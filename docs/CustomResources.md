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
    - [Search Head Deployer Resource](#search-head-deployer-resource)
      - [Example](#example)
  - [ClusterManager Resource Spec Parameters](#clustermanager-resource-spec-parameters)
  - [IndexerCluster Resource Spec Parameters](#indexercluster-resource-spec-parameters)
  - [MonitoringConsole Resource Spec Parameters](#monitoringconsole-resource-spec-parameters)
      - [Scaling Behavior Annotations](#scaling-behavior-annotations)
        - [Scale-Up Ready Wait Timeout](#scale-up-ready-wait-timeout)
        - [Preserve Total CPU](#preserve-total-cpu)
        - [Parallel Pod Updates](#parallel-pod-updates)
        - [Unified Transition Stall Timeout](#unified-transition-stall-timeout)
  - [Examples of Guaranteed and Burstable QoS](#examples-of-guaranteed-and-burstable-qos)
    - [A Guaranteed QoS Class example:](#a-guaranteed-qos-class-example)
    - [A Burstable QoS Class example:](#a-burstable-qos-class-example)
    - [A BestEffort QoS Class example:](#a-besteffort-qos-class-example)
    - [Pod Resources Management](#pod-resources-management)
    - [Troubleshooting](#troubleshooting)
      - [CR Status Message](#cr-status-message)
      - [Pause Annotations](#pause-annotations)
      - [admin-managed-pv Annotations](#admin-managed-pv-annotations)
        - [PV label values](#pv-label-values)
      - [Container Logs](#container-logs)

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
`SearchHeadCluster`, `ClusterManager` and `IndexerCluster`:

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

#### Scaling Behavior Annotations

The Splunk Operator supports annotations that control how StatefulSets scale up when pods are not ready. These annotations can be set on any Splunk Enterprise CR (Standalone, IndexerCluster, SearchHeadCluster, etc.) and will automatically propagate to the underlying StatefulSets.

##### Scale-Up Ready Wait Timeout

**Annotation:** `operator.splunk.com/scale-up-ready-wait-timeout`

By default, when scaling up a StatefulSet, the operator proceeds immediately without waiting for existing pods to become ready. This allows faster scaling operations. The `scale-up-ready-wait-timeout` annotation allows you to configure a specific timeout if you want to wait for existing pods to be ready before proceeding.

**Default Value:** `0` (no wait - scale up immediately)

**Supported Values:**
- Any valid Go duration string like `"5s"`, `"30s"`, `"5m"`, `"10m"`, `"1h"`, `"48h"`, `"168h"` (7 days), etc.
- `"0s"` or `"0"` (default) to immediately proceed with scale-up without waiting
- Negative values like `"-1"` to wait indefinitely for all pods to be ready
- Empty or missing annotation uses default (no wait)
- Invalid format uses default (no wait)

**Recommended CR Annotation:**

When setting this annotation on CRs (Standalone, IndexerCluster, SearchHeadCluster, etc.), use the `sts-only.` prefix to prevent the annotation from propagating to pod templates:

```yaml
metadata:
  annotations:
    sts-only.operator.splunk.com/scale-up-ready-wait-timeout: "5m"
```

The `sts-only.` prefix ensures the annotation is only applied to the StatefulSet and not to the pod template. The unprefixed key (`operator.splunk.com/scale-up-ready-wait-timeout`) is for advanced use cases where you need to annotate the StatefulSet directly.

**Example Usage:**

```yaml
apiVersion: enterprise.splunk.com/v4
kind: IndexerCluster
metadata:
  name: example
  annotations:
    # Recommended: use sts-only. prefix on CRs
    sts-only.operator.splunk.com/scale-up-ready-wait-timeout: "5m"
spec:
  replicas: 5
  clusterManagerRef:
    name: example-cm
```

**Behavior:**
1. When scaling up from N to N+M replicas, the operator checks if existing pods are ready
2. By default (no annotation or `"0"`), the operator proceeds immediately with scale-up without waiting
3. If a positive timeout is configured, the operator waits up to that duration for existing pods to be ready before proceeding
4. Setting a negative value like `"-1"` waits indefinitely for all pods to be ready before scaling up

**Use Cases:**
- **Default behavior (fast scaling):** Omit the annotation for immediate scale-up without waiting
- **Bounded waiting:** Set a specific timeout like `"5m"` or `"30m"` to wait for pods to be ready, but proceed after timeout
- **Maximum stability:** Set to `"-1"` to wait indefinitely, ensuring all pods are ready before adding more
- **Development workflows:** Use short timeouts like `"1m"` to balance speed and stability

**Note:** This annotation affects scale-up operations only. Scale-down operations always proceed to remove pods even if other pods are not ready, as removing pods doesn't add additional load to the cluster.

##### Preserve Total CPU

**Annotation:** `operator.splunk.com/preserve-total-cpu`

The `preserve-total-cpu` annotation enables CPU-aware scaling, which automatically adjusts the number of replicas to maintain the same total CPU allocation when CPU requests per pod change. This is useful for license-based or cost-optimized deployments where total resource allocation should remain constant regardless of individual pod sizing.

**Default Value:** Not set (disabled)

**Supported Values:**
- `"true"` or `"both"`: Enable CPU-preserving scaling for both scale-up and scale-down directions
- `"down"`: Enable CPU-preserving scaling only when replicas decrease (i.e., when CPU per pod increases)
- `"up"`: Enable CPU-preserving scaling only when replicas increase (i.e., when CPU per pod decreases)
- Empty or missing annotation: Feature is disabled

**Recommended CR Annotation:**

When setting this annotation on CRs (Standalone, IndexerCluster, SearchHeadCluster, etc.), use the `sts-only.` prefix to prevent the annotation from propagating to pod templates:

```yaml
metadata:
  annotations:
    sts-only.operator.splunk.com/preserve-total-cpu: "both"
```

**Example Usage:**

```yaml
apiVersion: enterprise.splunk.com/v4
kind: IndexerCluster
metadata:
  name: example
  annotations:
    # Enable CPU-preserving scaling for both directions
    sts-only.operator.splunk.com/preserve-total-cpu: "both"
spec:
  replicas: 4
  resources:
    requests:
      cpu: "2"
  clusterManagerRef:
    name: example-cm
```

**Behavior:**
1. When CPU requests per pod change, the operator calculates the new replica count to preserve total CPU
2. For example, if you have 4 replicas with 2 CPU each (total 8 CPU) and change to 1 CPU per pod, the operator will scale to 8 replicas to maintain 8 total CPU
3. The direction setting allows you to control which scaling operations are allowed
4. During the transition, the operator manages pod recycling to maintain cluster stability

**Use Cases:**
- **License optimization:** Maintain consistent CPU allocation that matches your Splunk license
- **Cost control:** Ensure total resource usage stays within budget when changing pod specs
- **Cluster rebalancing:** Safely transition between different pod sizes while maintaining capacity

##### Parallel Pod Updates

**Annotation:** `operator.splunk.com/parallel-pod-updates`

The `parallel-pod-updates` annotation controls how many pods can be deleted/recycled simultaneously during rolling updates. This can significantly speed up large cluster updates while maintaining cluster stability.

**Default Value:** `1` (sequential updates - one pod at a time)

**Supported Values:**
- A floating-point value `<= 1.0`: Interpreted as a percentage of total replicas (e.g., `"0.25"` means 25% of pods can be updated in parallel)
- A value `> 1.0`: Interpreted as an absolute number of pods (e.g., `"3"` allows up to 3 pods to be updated at once)
- Invalid or missing annotation uses default (sequential updates)
- Values are clamped to the range [1, total replicas]

**Recommended CR Annotation:**

When setting this annotation on CRs, use the `sts-only.` prefix:

```yaml
metadata:
  annotations:
    sts-only.operator.splunk.com/parallel-pod-updates: "3"
```

**Example Usage:**

```yaml
apiVersion: enterprise.splunk.com/v4
kind: IndexerCluster
metadata:
  name: example
  annotations:
    # Allow up to 25% of pods to be updated in parallel
    sts-only.operator.splunk.com/parallel-pod-updates: "0.25"
spec:
  replicas: 12
  clusterManagerRef:
    name: example-cm
```

**Behavior:**
1. During rolling updates, the operator will delete/recycle up to the specified number of pods simultaneously
2. Using percentage values scales with cluster size (e.g., 25% of a 12-pod cluster = 3 pods in parallel)
3. The operator waits for recycled pods to become ready before proceeding to the next batch

**Use Cases:**
- **Large cluster updates:** Speed up updates on clusters with many replicas
- **Maintenance windows:** Complete updates faster during limited maintenance periods
- **Development environments:** Faster iteration with less concern for availability

**Note:** Use with caution in production environments. Updating too many pods simultaneously may impact cluster availability and search performance.

##### Unified Transition Stall Timeout

**Annotation:** `operator.splunk.com/unified-transition-stall-timeout`

The `unified-transition-stall-timeout` annotation allows you to configure the maximum time a CPU-aware transition can run before being considered stalled. If a transition exceeds this timeout, the operator will take recovery action.

**Default Value:** `30m` (30 minutes)

**Supported Values:**
- Any valid Go duration string like `"15m"`, `"30m"`, `"1h"`, `"2h"`, etc.
- Invalid format uses default (30 minutes)

**Recommended CR Annotation:**

When setting this annotation on CRs, use the `sts-only.` prefix:

```yaml
metadata:
  annotations:
    sts-only.operator.splunk.com/unified-transition-stall-timeout: "1h"
```

**Example Usage:**

```yaml
apiVersion: enterprise.splunk.com/v4
kind: IndexerCluster
metadata:
  name: example
  annotations:
    # Allow transitions up to 1 hour before considering them stalled
    sts-only.operator.splunk.com/unified-transition-stall-timeout: "1h"
    sts-only.operator.splunk.com/preserve-total-cpu: "both"
spec:
  replicas: 20
  clusterManagerRef:
    name: example-cm
```

**Behavior:**
1. The operator tracks the start time of CPU-aware transitions
2. If a transition exceeds the configured timeout without progress, it is marked as stalled
3. The operator will attempt recovery actions for stalled transitions

**Use Cases:**
- **Large clusters:** Increase timeout for clusters with many replicas that take longer to transition
- **Slow environments:** Accommodate environments where pods take longer to become ready
- **Debugging:** Set shorter timeouts to quickly detect issues during testing

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
| clustermaster.enterprise.splunk.com | "clustermaster.enterprise.splunk.com/paused" |
| clustermanager.enterprise.splunk.com | "clustermanager.enterprise.splunk.com/paused" |
| indexercluster.enterprise.splunk.com | "indexercluster.enterprise.splunk.com/paused" |
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
