---
title: Custom Resources Reference
nav_order: 31
#review_complete: true
---

# Custom Resource Reference
{: .no_toc }
The Splunk Operator provides a collection of Custom Resources you can use to manage Splunk Enterprise deployments in your Kubernetes cluster. A Kubernetes Custom Resource is an extention of the Kubernetes API that represents a resource that is not necessarily avalaible in a default Kubernetes installation. Additional documentation about how Custom Resources are used within a Kubernetes cluster can be found on the [Kubernetes docs page - Custom Resources](https://kubernetes.io/docs/concepts/extend-kubernetes/api-extension/custom-resources/)

This page is intended to be a reference for all Custom Resources included with a Splunk Operator deployment. For examples on how to use these custom resources, please see [Example Deployments](/configuration/Examples).


#### Table of contents
{: .no_toc }
- TOC
{:toc}


## Metadata Parameters

All resources in Kubernetes include a `metadata` section. You can use this to define a name for a specific instance of the resource, and which namespace you would like the resource to reside within

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
  annotations:
    service.beta.kubernetes.io/azure-load-balancer-internal: "true"
```

{: .note }
The `enterprise.splunk.com/delete-pvc` finalizer is optional, and may be used to tell the Splunk Operator that you would like it to remove all the Persistent Volumes associated with the instance when you delete it. \
Additional documentation about Persistent Volumes can be found on the [Kubernetes docs page - Persistent Volumes](https://kubernetes.io/docs/concepts/storage/persistent-volumes/)


## Common Spec Parameters for All Resources

The `spec` section is used to define the desired state for a resource. All custom resources provided by the Splunk Operator include the following configuration parameters.

| Key                           | Type       | Description                                                                                                 |
| ----------------------------- | ---------- | ----------------------------------------------------------------------------------------------------------- |
| image                         | string     | Container image to use for pod instances - overrides `RELATED_IMAGE_SPLUNK_ENTERPRISE` environment variable |
| imagePullPolicy               | string     | Sets pull policy for all images, this can be either `Always`, or the default `IfNotPresent`                 |
| livenessInitialDelaySeconds   | number     | Sets the initialDelaySeconds for Liveness probe (default: 300)                                              |
| readinessInitialDelaySeconds  | number     | Sets the initialDelaySeconds for Readiness probe (default: 10)                                              |
| extraEnv                      | [EnvVar](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.24/#envvar-v1-core) | Sets the extra environment variables to be passed to the Splunk instance containers.<br/><br/>  **Warning:** Setting environment variables used by Splunk or Ansible will affect Splunk installation and operation |
| schedulerName                 | string     | Name of [Scheduler](https://kubernetes.io/docs/concepts/scheduling/kube-scheduler/) to use for pod placement (default: `default-scheduler`) |
| affinity                      | [Affinity](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.24/#affinity-v1-core) | [Kubernetes Affinity](https://kubernetes.io/docs/concepts/configuration/assign-pod-node/#affinity-and-anti-affinity) rules that control how pods are assigned to particular nodes |
| resources                     | [ResourceRequirements](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.24/#resourcerequirements-v1-core) | The settings for allocating [compute resource requirements](https://kubernetes.io/docs/concepts/configuration/manage-compute-resources-container/) to use for each pod instance. The default settings should be considered for demo/test purposes.<br/><br/>  Please see [Hardware Resource Requirements](/installation/Prerequisites#hardware-resources-requirements) for production values. |
| serviceTemplate               | [Service](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.24/#service-v1-core) | Template used to create Kubernetes [Services](https://kubernetes.io/docs/concepts/services-networking/service/) |
| topologySpreadConstraint      | [TopologySpreadConstraint](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.24/#topologyspreadconstraint-v1-core) | Template used to create Kubernetes [TopologySpreadConstraint](https://kubernetes.io/docs/concepts/scheduling-eviction/topology-spread-constraints/) |

```yaml
apiVersion: enterprise.splunk.com/v4
kind: Standalone
metadata:
  name: s1
  namespace: splunk
spec:
  image: splunk/splunk-operator:latest
  imagePullPolicy: Always
  livenessInitialDelaySeconds: 400
  readinessInitialDelaySeconds: 390
  extraEnv:
  - name: ADDITIONAL_ENV_VAR_1
    value: "test_value_1"
  - name: ADDITIONAL_ENV_VAR_2
    value: "test_value_2"
  schedulerName: "default-scheduler"
  affinity:
    nodeAffinity:
      requiredDuringSchedulingIgnoredDuringExecution:
        nodeSelectorTerms:
        - matchExpressions:
          - key: topology.kubernetes.io/zone
            operator: In
            values:
            - antarctica-east1
            - antarctica-west1
      preferredDuringSchedulingIgnoredDuringExecution:
      - weight: 1
        preference:
          matchExpressions:
          - key: another-node-label-key
            operator: In
            values:
            - another-node-label-value
  resources:
    requests:
      memory: "512Mi"
      cpu: "0.1"
    limits:
      memory: "8Gi"
      cpu: "4"
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
```


## Common Spec Parameters for Splunk Enterprise Resources

The following additional configuration parameters may be used for all Splunk Enterprise resources, including: 
  - `Standalone`
  - `LicenseManager`
  - `SearchHeadCluster`
  - `ClusterManager`
  - `IndexerCluster`

| Key                    | Type              | Description                                                                   |
| ---------------------- | ----------------- | ----------------------------------------------------------------------------- |
| etcVolumeStorageConfig | StorageClassSpec  | Storage class spec for Splunk etc volume as described in [StorageClass](/configuration/StorageClass) |
| varVolumeStorageConfig | StorageClassSpec  | Storage class spec for Splunk var volume as described in [StorageClass](/configuration/StorageClass) |
| volumes                | [Volume](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.24/#volume-v1-core)     | List of one or more [Kubernetes volumes](https://kubernetes.io/docs/concepts/storage/volumes/). These will be mounted in all container pods as as `/mnt/<name>` |
| defaults     | string  | Inline map of [Splunk Ansible default.yml](https://github.com/splunk/splunk-ansible/blob/develop/docs/advanced/default.yml.spec.md) overrides used to initialize the environment |
| defaultsUrl  | string  | Full path or URL for one or more [Splunk Ansible default.yml](https://github.com/splunk/splunk-ansible/blob/develop/docs/advanced/default.yml.spec.md) files, separated by commas |
| licenseUrl             | string            | Full path or URL for a Splunk Enterprise license file                         |
| licenseManagerRef   | [ObjectReference](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.24/#objectreference-v1-core) | Reference to a Splunk Operator managed `LicenseManager` instance (via `name` and optionally `namespace`) to use for licensing |
| clusterManagerRef  | [ObjectReference](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.24/#objectreference-v1-core) | Reference to a Splunk Operator managed `ClusterManager` instance (via `name` and optionally `namespace`) to use for indexing |
| monitoringConsoleRef  | string     | Logical name assigned to the Monitoring Console pod. You can set the name before or after the MC pod creation. |
| serviceAccount | [ServiceAccount](https://kubernetes.io/docs/tasks/configure-pod-container/configure-service-account/) | Represents the service account used by the pods deployed by the CRD |
| imagePullSecrets | [imagePullSecrets](https://kubernetes.io/docs/tasks/configure-pod-container/pull-image-private-registry/) | Config to pull images from private registry. Use in conjunction with `image` config from [common spec](#common-spec-parameters-for-all-resources) |

```yaml
apiVersion: enterprise.splunk.com/v4
kind: Standalone
metadata:
  name: s1
  namespace: splunk
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
  # defaults: {}
  # defaultsUrl: ""
  # licenseUrl: ""
  licenseManagerRef:
    name: example
  clusterManagerRef:
    name: example
  # monitoringConsoleRef: ""
  serviceAccount: custom-serviceaccount
  # imagePullSecrets: ""
```


## SmartStore Resource Spec Parameters

The `smartstore` section contains the following SmartStore configuration parameters and may be used for the `Standalone` and `ClusterManager` Splunk Enterprise resources. 

| Key        | Type    | Description                                       |
| ---------- | ------- | ------------------------------------------------- |
| cacheManager | object | Defines Cache manager settings |
| cacheManager.evictionPadding | integer | Additional size beyond 'minFreeSize' before eviction kicks in |
| cacheManager.evictionPolicy | string | Eviction policy to use |
| cacheManager.hotlistBloomFilterRecencyHours | integer | Time period relative to the bucket's age, during which the bloom filter file is protected from cache eviction |
| cacheManager.hotlistRecencySecs | integer | Time period relative to the bucket's age, during which the bucket is protected from cache eviction |
| cacheManager.maxCacheSize | integer | Max cache size per partition |
| cacheManager.maxConcurrentDownloads | integer | Maximum number of buckets that can be downloaded from remote storage in parallel |
| cacheManager.maxConcurrentUploads | integer | Maximum number of buckets that can be uploaded to remote storage in parallel |
| defaults | object | Default configuration for indexes |
| defaults.maxGlobalDataSizeMB | integer | MaxGlobalDataSizeMB defines the maximum amount of space for warm and cold buckets of an index |
| defaults.maxGlobalRawDataSizeMB | integer | MaxGlobalDataSizeMB defines the maximum amount of cumulative space for warm and cold buckets of an index |
| defaults.volumeName | string | Remote Volume name |
| indexes[]  | array | List of Splunk indexes defined as a IndexSpec object |
| IndexSpec | object | IndexSpec defines Splunk index name and storage |
| IndexSpec.hotlistBloomFilterRecencyHours | integer | Time period relative to the bucket's age, during which the bloom filter file is protected from cache eviction |
| IndexSpec.hotlistRecencySecs | integer | Time period relative to the bucket's age, during which the bucket is protected from cache eviction |
| IndexSpec.maxGlobalDataSizeMB | integer | MaxGlobalDataSizeMB defines the maximum amount of space for warm and cold buckets of an index |
| IndexSpec.maxGlobalRawDataSizeMB | integer | MaxGlobalDataSizeMB defines the maximum amount of cumulative space for warm and cold buckets of an index |
| IndexSpec.name | string | Splunk index name |
| IndexSpec.remotePath | string | Index location relative to the remote volume path |
| IndexSpec.volumeName | string | Remote Volume name |
| volumes[] | array | List of remote storage volumes defined as a VolumeSpec object |
| VolumeSpec | object | VolumeSpec defines remote volume config |
| VolumeSpec.endpoint | string | Remote volume URI |
| VolumeSpec.name | string | Remote volume name |
| VolumeSpec.path | string | Remote volume path |
| VolumeSpec.provider | string | App Package Remote Store provider. Supported values: aws, minio, azure. |
| VolumeSpec.region | string | Region of the remote storage volume where apps reside. Used for aws, if provided. Not used for minio and azure. |
| VolumeSpec.secretRef | string | Secret object name |
| VolumeSpec.storageType | string | Remote Storage type. Supported values: s3, blob. s3 works with aws or minio providers, whereas blob works with azure provider. |

```yaml
apiVersion: enterprise.splunk.com/v4
kind: Standalone
metadata:
  name: s1
  namespace: splunk
spec:
  smartstore:
    defaults:
        maxGlobalDataSizeMB: 50000
        maxGlobalRawDataSizeMB: 100000
        volumeName: msos_s2s3_vol
    indexes:
      - name: salesdata1
        remotePath: $_index_name
        volumeName: msos_s2s3_vol
        hotlistBloomFilterRecencyHours: 360
        hotlistRecencySecs: 86400
        maxGlobalDataSizeMB: 50000
        maxGlobalRawDataSizeMB: 100000
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
        provider: aws
        region: ap-southeast-2
        storageType: s3
```


## App Framework Resource Spec Parameters

The `appRepo` section contains the App Framework configuration parameters listed below and may be used the following Splunk Enterprise resources: 
  - `Standalone`
  - `LicenseManager`
  - `MonitoringConsole`
  - `SearchHeadCluster`
  - `ClusterManager`


| Key        | Type    | Description                                       |
| ---------- | ------- | ------------------------------------------------- |
| appSources[] | array | List of App sources on remote storage |
| AppSourceSpec | object | AppSourceSpec defines list of App package (*.spl, *.tgz) locations on remote volumes |
| AppSourceSpec.location | string | Location relative to the volume path |
| AppSourceSpec.name | string | Logical name for the set of apps placed in this location. Logical name must be unique to the appRepo |
| AppSourceSpec.scope | string | Scope of the App deployment: cluster, local. Scope determines whether the App(s) is/are installed locally or cluster-wide |
| AppSourceSpec.volumeName | string | Remote Storage Volume name |
| appsRepoPollIntervalSeconds | string | Remote Storage Volume name |
| defaults | object | Defines the default configuration settings for App sources |
| defaults.scope | string | Scope of the App deployment: cluster, local. Scope determines whether the App(s) is/are installed locally or cluster-wide |
| defaults.volumeName | string | Remote Storage Volume name |
| volumes[] | array | List of remote storage volumes |
| VolumeSpec | object | VolumeSpec defines remote volume config |
| VolumeSpec.endpoint | string | Remote volume URI |
| VolumeSpec.name | string | Remote volume name |
| VolumeSpec.path | string | Remote volume path |
| VolumeSpec.provider | string | App Package Remote Store provider. Currently supported proiders are aws, minio and azure |
| VolumeSpec.region | string | Region of the remote storage volume where apps reside. Not required for azure. |
| VolumeSpec.secretRef | string | Secret object name |
| VolumeSpec.storageType | string | Remote Storage type. Possible values are s3 (works with aws and minio) or blob (works with azure) |


```yaml
apiVersion: enterprise.splunk.com/v4
kind: Standalone
metadata:
  name: s1
  namespace: splunk
spec:
  appRepo:
    appsRepoPollIntervalSeconds: 900
    defaults:
      volumeName: volume_app_repo_au
      scope: cluster
    appSources:
      - name: searchApps
        location: searchAppsLoc/
      - name: machineLearningApps
        location: machineLearningAppsLoc/
      - name: adminApps
        location: adminAppsLoc/
        scope: local
    volumes:
      - name: volume_app_repo_au
        storageType: s3
        provider: aws
        path: bucket-app-framework/shcLoc-au/
        endpoint: https://s3-ap-southeast-2.amazonaws.com
        region: ap-southeast-2
        secretRef: s3-secret
```


## LicenseManager Resource Spec Parameters

Please see [Common Spec Parameters for All Resources](#common-spec-parameters-for-all-resources) and [Common Spec Parameters for All Splunk Enterprise Resources](#common-spec-parameters-for-all-splunk-enterprise-resources). The `LicenseManager` resource does not provide any additional configuration parameters.

```yaml
apiVersion: enterprise.splunk.com/v4
kind: LicenseManager
metadata:
  name: s1
  namespace: splunk
spec:
  volumes:
    - name: licenses
      configMap:
        name: splunk-licenses
  licenseUrl: /mnt/licenses/enterprise.lic
```

## MonitoringConsole Resource Spec Parameters

Use the Monitoring Console to view detailed topology and performance information about your Splunk Enterprise deployment. See [What can the Monitoring Console do?](https://docs.splunk.com/Documentation/Splunk/latest/DMC/WhatcanDMCdo) in the Splunk Enterprise documentation. 

The Splunk Operator now includes a CRD for the Monitoring Console (MC). This offers a number of advantages available to other CR's, including: customizable resource allocation, app management, and license management. 

* An MC pod is not created automatically in the default namespace when using other Splunk Operator CR's. 
* When upgrading to the latest Splunk Operator, any previously automated MC pods will be deleted. 
* To associate a new MC pod with an existing CR, you must update any CR's and add the `monitoringConsoleRef` parameter. 

The MC pod is referenced by using the `monitoringConsoleRef` parameter. There is no preferred order when running an MC pod; you can start the pod before or after the other CR's in the namespace.  When a pod that references the `monitoringConsoleRef` parameter is created or deleted, the MC pod will automatically update itself and create or remove connections to those pods.

```yaml
apiVersion: enterprise.splunk.com/v3
kind: MonitoringConsole
metadata:
  name: example-mc
  finalizers:
  - enterprise.splunk.com/delete-pvc

```


## Standalone Resource Spec Parameters

In addition to [Common Spec Parameters for All Resources](#common-spec-parameters-for-all-resources), [Common Spec Parameters for All Splunk Enterprise Resources](#common-spec-parameters-for-all-splunk-enterprise-resources), and [SmartStore Resource Spec Parameters](#smartstore-resource-spec-parameters). The `Standalone` resource provides the following `Spec` configuration parameters.

| Key        | Type    | Description                                       |
| ---------- | ------- | ------------------------------------------------- |
| replicas   | integer | The number of standalone replicas (defaults to 1) |

```yaml
apiVersion: enterprise.splunk.com/v4
kind: Standalone
metadata:
  name: s1
  namespace: splunk
  labels:
    app: SplunkStandAlone
    type: Splunk
  finalizers:
  - enterprise.splunk.com/delete-pvc
spec:
  replicas: 1
```

## ClusterManager Resource Spec Parameters

Please see [SmartStore Resource Spec Parameters](#smartstore-resource-spec-parameters) The `ClusterManager` resource does not provide any additional configuration parameters.

```yaml
apiVersion: enterprise.splunk.com/v4
kind: ClusterManager
metadata:
  name: example-cm
  namespace: splunk
spec:
  smartstore:
    defaults:
        maxGlobalDataSizeMB: 50000
        maxGlobalRawDataSizeMB: 100000
        volumeName: msos_s2s3_vol
    indexes:
      - name: salesdata1
        remotePath: $_index_name
        volumeName: msos_s2s3_vol
        hotlistBloomFilterRecencyHours: 360
        hotlistRecencySecs: 86400
        maxGlobalDataSizeMB: 50000
        maxGlobalRawDataSizeMB: 100000
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
        provider: aws
        region: ap-southeast-2
        storageType: s3
```


## IndexerCluster Resource Spec Parameters

In addition to [Common Spec Parameters for All Resources](#common-spec-parameters-for-all-resources) and [Common Spec Parameters for All Splunk Enterprise Resources](#common-spec-parameters-for-all-splunk-enterprise-resources), the `IndexerCluster` resource provides the following `Spec` configuration parameters.

| Key        | Type    | Description                                           |
| ---------- | ------- | ----------------------------------------------------- |
| replicas   | integer | The number of indexer cluster members (defaults to 1) |

```yaml
apiVersion: enterprise.splunk.com/v4
kind: IndexerCluster
metadata:
  name: idxc1
  namespace: splunk
spec:
  replicas: 3
  clusterManagerRef: 
    name: example-cm
    namespace: splunk
```

{: .note }
`clusterManagerRef` is required field in case of IndexerCluster resource since it will be used to connect the IndexerCluster to ClusterManager resource.


## SearchHeadCluster Resource Spec Parameters

In addition to [Common Spec Parameters for All Resources](#common-spec-parameters-for-all-resources) and [Common Spec Parameters for All Splunk Enterprise Resources](#common-spec-parameters-for-all-splunk-enterprise-resources). The `SearchHeadCluster` resource provides the following `Spec` configuration parameters.

| Key      | Type    | Description                                                  |
| -------- | ------- | ------------------------------------------------------------ |
| replicas | integer | The number of search heads cluster members (minimum of 3, which is the default) |

```yaml
apiVersion: enterprise.splunk.com/v4
kind: SearchHeadCluster
metadata:
  name: shc1
  namespace: splunk
spec:
  replicas: 5
```

