# SmartStore Resource Guide

*NOTE: The below method is recommended as a temporary way of installing SmartStore configuration & indexes. In the upcoming releases, an enhanced App Installation method will be introduced which is expected to help install SmartStore indexes & configuration using Apps. The new approach, once released, will become the preferred method moving ahead*

The Splunk Operator includes a method for configuring a SmartStore remote storage volume with index support using a [Custom Resource](https://splunk.github.io/splunk-operator/CustomResources.html). The SmartStore integration is not implemented as a StorageClass. This feature and its settings rely on support integrated into Splunk Enterprise. See [SmartStore](https://docs.splunk.com/Documentation/Splunk/latest/Indexer/AboutSmartStore) for information on the feature and implementation considerations.

 * SmartStore configuration is supported on these Custom Resources: Standalone and ClusterMaster.
 * SmartStore support in the Splunk Operator is limited to Amazon S3 & S3-API-compliant object stores only if you are using the CRD configuration for S3 as described below."
 * Use of GCS with SmartStore is supported by using configuration via Splunk App.
 * Specification allows definition of SmartStore-enabled indexes only.
 * Already existing indexes data should be migrated from local storage to the remote store as a pre-requisite before configuring those indexes in the Custom Resource of the Splunk Operator. For more details, please see [Migrate existing data on an indexer cluster to SmartStore](https://docs.splunk.com/Documentation/Splunk/latest/Indexer/MigratetoSmartStore#Migrate_existing_data_on_an_indexer_cluster_to_SmartStore).
 

SmartStore configuration involves indexes, volumes, and the volume credentials. Indexes and volume configurations are configured through the Custom Resource specification. However, the volume credentials are configured securely in a Kubernetes secret object, and that secret object is referred by the Custome Resource with SmartStore volume spec, through `SecretRef`

## Storing Smartstore Secrets
Here is an example command to encode and load your remote storage volume secret key and access key in the kubernetes secret object: `kubectl create secret generic <secret_store_obj> --from-literal='s3_access_key=<access_key>' --from-literal='s3_secret_key=<secret_key>'`
  

## Creating a SmartStore-enabled Standalone instance
1. Create a Secret object with Secret & Access credentials, as explained in [Storing SmartStore Secrets](#storing-smartstore-secrets)
2. Confirm your S3-based storage volume path and URL.
3. Confirm the name of the Splunk indexes being used with the SmartStore volume. 
4. Create/Update the Standalone Customer Resource specification with volume and index configuration (see Example below)
5. Apply the Customer Resource specification: kubectl -f apply Standalone.yaml


Example. Standalone.yaml:

```yaml
apiVersion: enterprise.splunk.com/v1
kind: Standalone
metadata:
  name: <name>
  finalizers:
  - enterprise.splunk.com/delete-pvc
spec:
  replicas: 1
  smartstore:
    volumes:
      - name: <remote_volume_name>
        path: <remote_volume_path>
        endpoint: https://s3-<region>.amazonaws.com
        secretRef: <secret_store_obj>
    indexes:
      - name: <index_name_1>
        remotePath: $_index_name
        volumeName: <remote_volume_name>
      - name: <index_name_2>
        remotePath: $_index_name
        volumeName: <remote_volume_name>
      - name: <index_name_3>
        remotePath: $_index_name
        volumeName: <remote_volume_name>
```

The SmartStore parameters will be placed into the required .conf files in an app. The app is named as `splunk-operator`. In the case of a standalone deployment, the app is located at `/opt/splunk/etc/apps/`

Note: Custom apps with higher precedence can potentially overwrite the index and volume configuration in the splunk-operator app. Hence, care should be taken to avoid conflicting SmartStore configuration in custom apps. See  [Configuration file precedence order](https://docs.splunk.com/Documentation/Splunk/latest/Admin/Wheretofindtheconfigurationfiles#How_Splunk_determines_precedence_order)
 
 
 
## Creating a SmartStore-enabled Indexer Cluster
1. Create a Secret object with Secret & Access credentials, as explained in [Storing SmartStore Secrets](#storing-smartstore-secrets)
2. Confirm your S3-based storage volume path and URL.
3. Confirm the name of the Splunk indexes being used with the SmartStore volume. 
4. Create/Update the Cluster Master Customer Resource specification with volume and index configuration (see Example below)
5. Apply the Customer Resource specification: kubectl -f apply Clustermaster.yaml
6. Follow the rest of the steps to Create an Indexer Cluster. See [Examples](Examples.md)


Example. Clustermaster.yaml:

```yaml
apiVersion: enterprise.splunk.com/v1
kind: ClusterMaster
metadata:
  name: <name>
  finalizers:
  - enterprise.splunk.com/delete-pvc
spec:
  smartstore:
    volumes:
      - name: <remote_volume_name>
        path: <remote_volume_path>
        endpoint: https://s3-<region>.amazonaws.com
        secretRef: <secret_store_obj>
    indexes:
      - name: <index_name_1>
        remotePath: $_index_name
        volumeName: <remote_volume_name>
      - name: <index_name_2>
        remotePath: $_index_name
        volumeName: <remote_volume_name>
      - name: <index_name_3>
        remotePath: $_index_name
        volumeName: <remote_volume_name>
```


The SmartStore parameters will be placed into the required .conf files in an app. The app is named as `splunk-operator`. In case of a Indexer cluster deployment, the app is located on Cluster master at `/opt/splunk/etc/master-apps/`. 
Once the SmartStore configuration is populated to Cluster Master's `splunk-operator` app, Operator issues a bundle push command to Cluster Master, so that the SmartStore configuration is distributed to all the peers in that indexer cluster

Note: Custom apps with higher precedence can potentially overwrite the index and volume configuration in the splunk-operator app. Hence, care should be taken to avoid conflicting SmartStore configuration in custom apps. See  [Configuration file precedence order](https://docs.splunk.com/Documentation/Splunk/latest/Admin/Wheretofindtheconfigurationfiles#How_Splunk_determines_precedence_order)


## SmartStore Resource Spec Parameters
There are additional SmartStore settings available for tuning and storage management. The settings are equivalent to the SmartStore settings defined in indexes.conf and server.conf for Splunk Enterprise.  The SmartStore resource applies to the `Standalone` and `ClusterMaster` Custom Resources, and adds the following `Spec` configuration parameters:


```
smartstore:
  description:
    Splunk Smartstore configuration. Refer to indexes.conf.spec and
    server.conf.spec on docs.splunk.com
  properties:
    cacheManager:
      description: Defines Cache manager settings
      properties:
        evictionPadding:
          description: Additional size beyond 'minFreeSize' before eviction kicks in
          type: integer
        evictionPolicy:
          description: Eviction policy to use
          type: string
        hotlistBloomFilterRecencyHours:
          description:
            Time period relative to the bucket's age, during which the bloom
            filter file is protected from cache eviction
          type: integer
        hotlistRecencySecs:
          description:
            Time period relative to the bucket's age, during which the bucket is
            protected from cache eviction
          type: integer
        maxCacheSize:
          description: Max cache size per partition
          type: integer
        maxConcurrentDownloads:
          description:
            Maximum number of buckets that can be downloaded from remote storage
            in parallel
          type: integer
        maxConcurrentUploads:
          description: 
            Maximum number of buckets that can be uploaded to remote storage in
            parallel
          type: integer
      type: object
    defaults:
      description: Default configuration for indexes
      properties:
        maxGlobalDataSizeMB:
          description: 
            MaxGlobalDataSizeMB defines the maximum amount of space for warm and
            cold buckets of an index
          type: integer
        maxGlobalRawDataSizeMB:
          description: 
            MaxGlobalDataSizeMB defines the maximum amount of cumulative space
            for warm and cold buckets of an index
          type: integer
        volumeName:
          description: Remote Volume name
          type: string
      type: object
    indexes:
      description: List of Splunk indexes
      items:
        description: IndexSpec defines Splunk index name and storage path
        properties:
          hotlistBloomFilterRecencyHours:
            description: 
              Time period relative to the bucket's age, during which the bloom
              filter file is protected from cache eviction
            type: integer
          hotlistRecencySecs:
            description: 
              Time period relative to the bucket's age, during which the bucket
              is protected from cache eviction
            type: integer
          maxGlobalDataSizeMB:
            description: 
              MaxGlobalDataSizeMB defines the maximum amount of space for warm
              and cold buckets of an index
            type: integer
          maxGlobalRawDataSizeMB:
            description: 
              MaxGlobalDataSizeMB defines the maximum amount of cumulative space
              for warm and cold buckets of an index
            type: integer
          name:
            description: Splunk index name
            type: string
          remotePath:
            description: Index location relative to the remote volume path
            type: string
          volumeName:
            description: Remote Volume name
            type: string
        type: object
      type: array
    volumes:
      description: List of remote storage volumes
      items:
        description: VolumeSpec defines remote volume name and remote volume URI
        properties:
          endpoint:
            description: Remote volume URI
            type: string
          name:
            description: Remote volume name
            type: string
          path:
            description: Remote volume path
            type: string
          secretRef:
            description: Secret object name
            type: string
        type: object
      type: array
  type: object

```


## Following is the table that maps the Custom Resource SmartStore spec to the Splunk docs. 

See [indexes.conf](https://docs.splunk.com/Documentation/Splunk/latest/Admin/Indexesconf), and [server.conf](https://docs.splunk.com/Documentation/Splunk/latest/Admin/Serverconf) for more information about these configuration details.

| Custom Resource Spec | Splunk Config| Splunk Stanza |
| :--- | :--- | :--- |
| volumeName + remotePath | remotePath | [\<index name\>], [default] in indexes.conf |
| maxGlobalDataSizeMB | maxGlobalDataSizeMB  | [\<index name\>], [default] in indexes.conf |
| maxGlobalRawDataSizeMB | maxGlobalRawDataSizeMB  | [\<index name\>], [default] in indexes.conf |
| hotlistRecencySecs |hotlist_recency_secs |[\<index name\>], [cachemanager] |
| hotlistBloomFilterRecencyHours |hotlist_bloom_filter_recency_hours  | [\<index name\>], [cachemanager] |
| endpoint  |remote.s3.endpoint  | [volume:\<name\>] |
| path | path  | [volume:\<name\>] |
| maxConcurrentUploads | max_concurrent_uploads |[cachemanager] |
| maxConcurrentDownloads | max_concurrent_downloads  |[cachemanager] |
| maxCacheSize | max_cache_size  | [cachemanager] |
| evictionPolicy |eviction_policy  |[cachemanager] |
| evictionPadding | eviction_padding  |[cachemanager] |

## Additional configuration

There are SmartStore/Index config settings that are not covered by the Custom Resource SmartStore spec.
If there is a need to configure additional settings, this can be achieved by configuring the same via Apps:
1. Create an App with the additional configuration
For example, in order to set the remote S3 encryption scheme as `sse-s3`, create an app with the config in indexes.conf file under default/local sub-directory as follows:
```
[volume:\<remote_volume_name\>]]
path = <remote_volume_path>
remote.s3.encryption = sse-s3
```
2. Apply the CR with the necessary & supported Smartstore and Index related configs
3. Install the App created using the [currently supported methods](https://splunk.github.io/splunk-operator/Examples.html#installing-splunk-apps) (*Note: This can be combined with the previous step*)