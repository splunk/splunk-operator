# App Framework Resource Guide

The Splunk Operator provides support for Splunk app and add-on deployment using the App Framework. The App Framework specification supports configuration management using the Splunk Enterprise cluster and standalone [custom resources](https://splunk.github.io/splunk-operator/CustomResources.html) (CR).

## Prerequisites

Utilizing the App Framework requires one of the following remote storage providers:
   * An Amazon S3 or S3-API-compliant remote object storage location
   * Azure blob storage
   * GCP Cloud Storage

### Prerequisites common to both remote storage providers
* The App framework requires read-only access to the path used to host the apps. DO NOT give any other access to the operator to maintain the integrity of data in S3 bucket , Azure blob container or GCP bucket.
* Splunk apps and add-ons in a .tgz or .spl archive format.
* Connections to the remote object storage endpoint need to be secured using a minimum version of TLS 1.2.
* A persistent storage volume and path for the Operator Pod. See [Add a persistent storage volume to the Operator pod](#add-a-persistent-storage-volume-to-the-operator-pod).

### Prerequisites for S3 based remote object storage
* Create role and role-binding for splunk-operator service account, to provide read-only access for S3 credentials.
* The remote object storage credentials provided as a kubernetes secret, or in an IAM role.
* If you are using [interface VPC endpoints](https://docs.aws.amazon.com/vpc/latest/privatelink/create-interface-endpoint.html) with DNS enabled to access AWS S3, please update the corresponding volume endpoint URL with one of the `DNS names` from the endpoint. Please ensure that the endpoint has access to the S3 buckets using the credentials configured. Similarly other endpoint URLs with access to the S3 buckets can also be used.

### Prerequisites for Azure Blob remote object storage
* The remote object storage credentials provided as a kubernetes secret.
* OR, Use "Managed Indentity" role assigment to the Azure blob container. See [Setup Azure bob access with Managed Indentity](#setup-azure-bob-access-with-managed-indentity)

### Prerequisites for GCP bucket based remote object storage
To use GCP storage in the App Framework, follow these setup requirements:

### Role & Role Binding for Access: 
Create a role and role-binding for the splunk-operator service account. This allows read-only access to the GCP bucket to retrieve Splunk apps. Access should be limited to read-only for the security of data within the GCP bucket.

### Credentials via Kubernetes Secret or Workload Identity: 
Configure credentials through either a Kubernetes secret (e.g., storing a GCP service account key in key.json) or use Workload Identity for secure access:

* **Kubernetes Secret**: Create a Kubernetes secret using the service account JSON key file for GCP access.
* **Workload Identity**: Use Workload Identity to associate the Kubernetes service account used by the Splunk Operator with a GCP service account that has the Storage Object Viewer IAM role for the required bucket.

## Example for creating the secret

```shell
kubectl create secret generic gcs-secret --from-file=key.json=path/to/your-service-account-key.json
```

Splunk apps and add-ons deployed or installed outside of the App Framework are not managed, and are unsupported.

Note: For the App Framework to detect that an app or add-on had changed, the updated app must use the same archive file name as the previously deployed one.

## Examples of App Framework usage
Following section shows examples of using App Framework for both remote data storages. First, the examples for S3 based remote object storage are given and then same examples are covered for Azure blob. The examples in both the cases have lot of commonalities and the places they differ are mainly in the values for `storageType`, `provider` and `endpoint`. There are also some differences in the authoriziation setup for using IAM /Managed Identity in both remote data storages.

### Examples of App Framework usage

#### How to use the App Framework on a Standalone CR

In this example, you'll deploy a Standalone CR with a remote storage volume, the location of the app archive, and set the installation location for the Splunk Enterprise Pod instance by using `scope`.

1. Confirm your remote storage volume path and URL.

2. Configure credentials to connect to remote store by:
   * s3 based remote storage:
       * Configuring an IAM role for the Operator and Splunk instance pods using a service account or annotations.
       * Or, create a Kubernetes Secret Object with the static storage credentials.
           * Example: `kubectl create secret generic s3-secret --from-literal=s3_access_key=AKIAIOSFODNN7EXAMPLE --from-literal=s3_secret_key=wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLE_S3_SECRET_KEY`
   * azure blob:
       * Configuring an IAM through  "Managed Indentity" role assigment to give read access for your bucket (azure blob container). For more details see [Setup Azure bob access with Managed Indentity](#setup-azure-bob-access-with-managed-indentity)
       * Or, create a Kubernetes Secret Object with the static storage credentials.
           * Example: `kubectl create secret generic azureblob-secret --from-literal=azure_sa_name=mystorageaccount --from-literal=azure_sa_secret_key=wJalrXUtnFEMI/K7MDENG/EXAMPLE_AZURE_SHARED_ACCESS_KEY`
   * GCP bucket:
       * Configure credentials through either a Kubernetes secret (e.g., storing a GCP service account key in key.json) or use Workload Identity for secure access:
          * Kubernetes Secret: Create a Kubernetes secret using the service account JSON key file for GCP access.
            * Example: `kubectl create secret generic gcs-secret --from-file=key.json=path/to/your-service-account-key.json`
          * Workload Identity: Use Workload Identity to associate the Kubernetes service account used by the Splunk Operator with a GCP service account that has the Storage Object Viewer IAM role for the required bucket.
3. Create unique folders on the remote storage volume to use as App Source locations.
   * An App Source is a folder on the remote storage volume containing a select subset of Splunk apps and add-ons. In this example, the network and authentication Splunk Apps are split into different folders and named `networkApps` and `authApps`.

4. Copy your Splunk App or Add-on archive files to the App Source.
   * In this example, the Splunk Apps are located at `bucket-app-framework/Standalone-us/networkAppsLoc/` and `bucket-app-framework/Standalone-us/authAppsLoc/`, and are both accessible through the end point `https://s3-us-west-2.amazonaws.com` for s3, https://mystorageaccount.blob.core.windows.net for azure blob and https://storage.googleapis.com for GCP bucket.

5. Update the standalone CR specification and append the volume, App Source configuration, and scope.
   * The scope determines where the apps and add-ons are placed into the Splunk Enterprise instance. For CRs where the Splunk Enterprise instance will run the apps locally, set the `scope: local ` The Standalone, Monitoring Console and License Manager CRs always use a local scope.

Example using s3: Standalone.yaml

```yaml
apiVersion: enterprise.splunk.com/v4
kind: Standalone
metadata:
  name: stdln
  finalizers:
  - enterprise.splunk.com/delete-pvc
spec:
  replicas: 1
  appRepo:
    appsRepoPollIntervalSeconds: 600
    defaults:
      volumeName: volume_app_repo
      scope: local
    appSources:
      - name: networkApps
        location: networkAppsLoc/
      - name: authApps
        location: authAppsLoc/
    volumes:
      - name: volume_app_repo
        storageType: s3
        provider: aws
        path: bucket-app-framework/Standalone-us/
        endpoint: https://s3-us-west-2.amazonaws.com
        region: us-west-2
        secretRef: s3-secret
```

Example using azure blob: Standalone.yaml

```yaml
apiVersion: enterprise.splunk.com/v4
kind: Standalone
metadata:
  name: stdln
  finalizers:
  - enterprise.splunk.com/delete-pvc
spec:
  replicas: 1
  appRepo:
    appsRepoPollIntervalSeconds: 600
    defaults:
      volumeName: volume_app_repo
      scope: local
    appSources:
      - name: networkApps
        location: networkAppsLoc/
      - name: authApps
        location: authAppsLoc/
    volumes:
      - name: volume_app_repo
        storageType: blob
        provider: azure
        path: bucket-app-framework/Standalone-us/
        endpoint: https://mystorageaccount.blob.core.windows.net
        secretRef: azureblob-secret
```

Example using GCP blob: Standalone.yaml

```yaml
apiVersion: enterprise.splunk.com/v4
kind: Standalone
metadata:
  name: stdln
  finalizers:
    - enterprise.splunk.com/delete-pvc
spec:
  replicas: 1
  appRepo:
    appsRepoPollIntervalSeconds: 600
    defaults:
      volumeName: volume_app_repo
      scope: local
    appSources:
      - name: networkApps
        location: networkAppsLoc/
      - name: authApps
        location: authAppsLoc/
    volumes:
      - name: volume_app_repo
        storageType: gcs
        provider: gcp
        path: bucket-app-framework/Standalone-us/
        endpoint: https://storage.googleapis.com
        secretRef: gcs-secret
```

6. Apply the Custom Resource specification: `kubectl apply -f Standalone.yaml`

The App Framework detects the Splunk app or add-on archive files available in the App Source locations, and deploys them to the standalone instance path for local use.

The App Framework maintains a checksum for each app or add-on archive file in the App Source location. The app name and checksum is recorded in the CR, and used to compare the deployed apps to the app archive files in the App Source location. The App Framework will scan for changes to the App Source folders using the polling interval, and deploy any updated apps to the instance. For the App Framework to detect that an app or add-on had changed, the updated app must use the same archive file name as the previously deployed one.

By default, the App Framework polls the remote object storage location for new or changed apps at the `appsRepoPollIntervalSeconds` interval. To disable the interval check, and manage app updates manually, see the [Manual initiation of app management](#manual-initiation-of-app-management).

For more information, see the [Description of App Framework Specification fields](#description-of-app-framework-specification-fields).

#### How to use the App Framework on Indexer Cluster

This example describes the installation of apps on an Indexer Cluster and Cluster Manager. This is achieved by deploying a ClusterManager CR with a remote storage volume, setting the location of the app archives, and the installation scope to support both local and cluster app path distribution.

1. Confirm your remote storage volume path and URL.

2. Configure credentials to connect to remote store by:
   * s3 based remote storage:
       * Configuring an IAM role for the Operator and Splunk instance pods using a service account or annotations.
       * Or, create a Kubernetes Secret Object with the static storage credentials.
           * Example: `kubectl create secret generic s3-secret --from-literal=s3_access_key=AKIAIOSFODNN7EXAMPLE --from-literal=s3_secret_key=wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLE_S3_SECRET_KEY`
   * azure blob:
       * Configuring an IAM through  "Managed Indentity" role assigment to give read access for your bucket (azure blob container). For more details see [Setup Azure bob access with Managed Indentity](#setup-azure-bob-access-with-managed-indentity)
       * Or, create a Kubernetes Secret Object with the static storage credentials.
           * Example: `kubectl create secret generic azureblob-secret --from-literal=azure_sa_name=mystorageaccount --from-literal=azure_sa_secret_key=wJalrXUtnFEMI/K7MDENG/EXAMPLE_AZURE_SHARED_ACCESS_KEY`
   * GCP bucket:
       * Configure credentials through either a Kubernetes secret (e.g., storing a GCP service account key in key.json) or use Workload Identity for secure access:
          * Kubernetes Secret: Create a Kubernetes secret using the service account JSON key file for GCP access.
            * Example: `kubectl create secret generic gcs-secret --from-file=key.json=path/to/your-service-account-key.json`
          * Workload Identity: Use Workload Identity to associate the Kubernetes service account used by the Splunk Operator with a GCP service account that has the Storage Object Viewer IAM role for the required bucket.

3. Create unique folders on the remote storage volume to use as App Source locations.
   * An App Source is a folder on the remote storage volume containing a select subset of Splunk apps and add-ons. In this example, there are Splunk apps installed and run locally on the cluster manager, and select apps that will be distributed to all cluster peers by the cluster manager.
   * The apps are split across three folders named `networkApps`, `clusterBase`, and `adminApps`. The apps placed into  `networkApps` and `clusterBase` are distributed to the cluster peers, but the apps in `adminApps` are for local use on the cluster manager instance only.

4. Copy your Splunk app or add-on archive files to the App Source.
   * In this example, the Splunk apps for the cluster peers are located at `bucket-app-framework/idxcAndCmApps/networkAppsLoc/`,  `bucket-app-framework/idxcAndCmApps/clusterBaseLoc/`, and the apps for the cluster manager are located at`bucket-app-framework/idxcAndCmApps/adminAppsLoc/`. They are all accessible through the end point `https://s3-us-west-2.amazonaws.com` for s3, https://mystorageaccount.blob.core.windows.net for azure blob and https://storage.googleapis.com for GCP bucket.


5. Update the ClusterManager CR specification and append the volume, App Source configuration, and scope.
   * The scope determines where the apps and add-ons are placed into the Splunk Enterprise instance. For CRs where the Splunk Enterprise instance will deploy the apps to cluster peers, set the `scope:  cluster`. The ClusterManager and SearchHeadCluster CRs support both cluster and local scopes.
   * In this example, the cluster manager will install some apps locally, and deploy other apps to the cluster peers. The App Source folder `adminApps` contains Splunk apps that are installed and run on the cluster manager, and will use a local scope. The apps in the App Source folders `networkApps` and `clusterBase` will be deployed from the cluster manager to the peers, and will use a cluster scope.

Example using S3: ClusterManager.yaml

```yaml
apiVersion: enterprise.splunk.com/v4
kind: ClusterManager
metadata:
  name: cm
  finalizers:
  - enterprise.splunk.com/delete-pvc
spec:
  appRepo:
    appsRepoPollIntervalSeconds: 900
    defaults:
      volumeName: volume_app_repo_us
      scope: cluster
    appSources:
      - name: networkApps
        location: networkAppsLoc/
      - name: clusterBase
        location: clusterBaseLoc/
      - name: adminApps
        location: adminAppsLoc/
        scope: local
    volumes:
      - name: volume_app_repo_us
        storageType: s3
        provider: aws
        path: bucket-app-framework/idxcAndCmApps/
        endpoint: https://s3-us-west-2.amazonaws.com
        region: us-west-2
        secretRef: s3-secret
```

Example using Azure Blob: ClusterManager.yaml

```yaml
apiVersion: enterprise.splunk.com/v4
kind: ClusterManager
metadata:
  name: cm
  finalizers:
  - enterprise.splunk.com/delete-pvc
spec:
  appRepo:
    appsRepoPollIntervalSeconds: 900
    defaults:
      volumeName: volume_app_repo_us
      scope: cluster
    appSources:
      - name: networkApps
        location: networkAppsLoc/
      - name: clusterBase
        location: clusterBaseLoc/
      - name: adminApps
        location: adminAppsLoc/
        scope: local
    volumes:
      - name: volume_app_repo_us
        storageType: blob
        provider: azure
        path: bucket-app-framework/idxcAndCmApps/
        endpoint: https://mystorageaccount.blob.core.windows.net
        secretRef: azureblob-secret
```

Example using GCP Bucket: ClusterManager.yaml
```yaml
apiVersion: enterprise.splunk.com/v4
kind: ClusterManager
metadata:
  name: cm
  finalizers:
    - enterprise.splunk.com/delete-pvc
spec:
  appRepo:
    appsRepoPollIntervalSeconds: 900
    defaults:
      volumeName: volume_app_repo_us
      scope: cluster
    appSources:
      - name: networkApps
        location: networkAppsLoc/
      - name: clusterBase
        location: clusterBaseLoc/
      - name: adminApps
        location: adminAppsLoc/
        scope: local
    volumes:
      - name: volume_app_repo_us
        storageType: gcs
        provider: gcp
        path: bucket-app-framework/idxcAndCmApps/
        endpoint: https://storage.googleapis.com
        secretRef: gcs-secret
```

6. Apply the Custom Resource specification: `kubectl apply -f ClusterManager.yaml`

The App Framework detects the Splunk app or add-on archive files available in the App Source locations, and deploys the apps from the `adminApps` folder to the cluster manager instance for local use.

The apps in the `networkApps` and `clusterBase` folders are deployed to the cluster manager for use on the cluster peers. The cluster manager is responsible for deploying those apps to the cluster peers.

Note: The Splunk cluster peer restarts are triggered by the contents of the Splunk apps deployed, and are not initiated by the App Framework.

The App Framework maintains a checksum for each app or add-on archive file in the App Source location. The app name and checksum is recorded in the CR, and used to compare the deployed apps to the app archive files in the App Source location. The App Framework will scan for changes to the App Source folders using the polling interval, and deploy any updated apps to the instance. For the App Framework to detect that an app or add-on had changed, the updated app must use the same archive file name as the previously deployed one.

By default, the App Framework polls the remote object storage location for new or changed apps at the `appsRepoPollIntervalSeconds` interval. To disable the interval check, and manage app updates manually, see the [Manual initiation of app management](#manual-initiation-of-app-management).

For more information, see the [Description of App Framework Specification fields](#description-of-app-framework-specification-fields)

#### How to use the App Framework on Search Head Cluster

This example describes the installation of apps on the Deployer and the Search Head Cluster. This is achieved by deploying a SearchHeadCluster CR with a storage volume, the location of the app archives, and set the installation scope to support both local and cluster app distribution.

1. Confirm your remote storage volume path and URL.

2. Configure credentials to connect to remote store by:
   * s3 based remote storage:
       * Configuring an IAM role for the Operator and Splunk instance pods using a service account or annotations.
       * Or, create a Kubernetes Secret Object with the static storage credentials.
           * Example: `kubectl create secret generic s3-secret --from-literal=s3_access_key=AKIAIOSFODNN7EXAMPLE --from-literal=s3_secret_key=wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLE_S3_SECRET_KEY`
   * azure blob:
       * Configuring an IAM through  "Managed Indentity" role assigment to give read access for your bucket (azure blob container). For more details see [Setup Azure bob access with Managed Indentity](#setup-azure-bob-access-with-managed-indentity)
       * Or, create a Kubernetes Secret Object with the static storage credentials.
           * Example: `kubectl create secret generic azureblob-secret --from-literal=azure_sa_name=mystorageaccount --from-literal=azure_sa_secret_key=wJalrXUtnFEMI/K7MDENG/EXAMPLE_AZURE_SHARED_ACCESS_KEY`
   * GCP bucket:
       * Configure credentials through either a Kubernetes secret (e.g., storing a GCP service account key in key.json) or use Workload Identity for secure access:
          * Kubernetes Secret: Create a Kubernetes secret using the service account JSON key file for GCP access.
            * Example: `kubectl create secret generic gcs-secret --from-file=key.json=path/to/your-service-account-key.json`
          * Workload Identity: Use Workload Identity to associate the Kubernetes service account used by the Splunk Operator with a GCP service account that has the Storage Object Viewer IAM role for the required bucket.


3. Create unique folders on the remote storage volume to use as App Source locations.
   * An App Source is a folder on the remote storage volume containing a select subset of Splunk apps and add-ons. In this example, there are Splunk apps installed and run locally on the Deployer, and select apps that will be distributed to all cluster search heads by the Deployer.
   * The apps are split across three folders named `searchApps`, `machineLearningApps` and `adminApps`. The apps placed into `searchApps` and `machineLearningApps` are distributed to the search heads, but the apps in `adminApps` are for local use on the Deployer instance only.

4. Copy your Splunk app or add-on archive files to the App Source.
   * In this example, the Splunk apps for the search heads are located at `bucket-app-framework/shcLoc-us/searchAppsLoc/`,  `bucket-app-framework/shcLoc-us/machineLearningAppsLoc/`, and the apps for the Deployer are located at `bucket-app-framework/shcLoc-us/adminAppsLoc/`. They are all accessible through the end point `https://s3-us-west-2.amazonaws.com` for s3, https://mystorageaccount.blob.core.windows.net for azure blob and and https://storage.googleapis.com for GCP bucket.

5. Update the SearchHeadCluster CR specification, and append the volume, App Source configuration, and scope.
   * The scope determines where the apps and add-ons are placed into the Splunk Enterprise instance.
      * For CRs where the Splunk Enterprise instance will deploy the apps to search heads, set the `scope:cluster`. The ClusterManager and SearchHeadCluster CRs support both cluster and local scopes.
   * In this example, the Deployer will run some apps locally, and deploy other apps to the clustered search heads. The App Source folder `adminApps` contains Splunk apps that are installed and run on the Deployer, and will use a local scope. The apps in the App Source folders `searchApps` and `machineLearningApps` will be deployed from the Deployer to the search heads, and will use a cluster scope.

Example using S3: SearchHeadCluster.yaml

```yaml
apiVersion: enterprise.splunk.com/v4
kind: SearchHeadCluster
metadata:
  name: shc
  finalizers:
  - enterprise.splunk.com/delete-pvc
spec:
  replicas: 1
  appRepo:
    appsRepoPollIntervalSeconds: 900
    defaults:
      volumeName: volume_app_repo_us
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
      - name: volume_app_repo_us
        storageType: s3
        provider: aws
        path: bucket-app-framework/shcLoc-us/
        endpoint: https://s3-us-west-2.amazonaws.com
        region: us-west-2
        secretRef: s3-secret
```

Example using Azure blob: SearchHeadCluster.yaml

```yaml
apiVersion: enterprise.splunk.com/v4
kind: SearchHeadCluster
metadata:
  name: shc
  finalizers:
  - enterprise.splunk.com/delete-pvc
spec:
  replicas: 1
  appRepo:
    appsRepoPollIntervalSeconds: 900
    defaults:
      volumeName: volume_app_repo_us
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
      - name: volume_app_repo_us
        storageType: blob
        provider: azure
        path: bucket-app-framework/shcLoc-us/
        endpoint: https://mystorageaccount.blob.core.windows.net
        secretRef: azureblob-secret
```

Example using GCP bucket: SearchHeadCluster.yaml

```yaml
apiVersion: enterprise.splunk.com/v4
kind: SearchHeadCluster
metadata:
  name: shc
  finalizers:
    - enterprise.splunk.com/delete-pvc
spec:
  appRepo:
    appsRepoPollIntervalSeconds: 900
    defaults:
      volumeName: volume_app_repo_us
      scope: cluster
    appSources:
      - name: networkApps
        location: networkAppsLoc/
      - name: clusterBase
        location: clusterBaseLoc/
      - name: adminApps
        location: adminAppsLoc/
        scope: local
    volumes:
      - name: volume_app_repo_us
        storageType: gcs
        provider: gcp
        path: bucket-app-framework/idxcAndCmApps/
        endpoint: https://storage.googleapis.com
        secretRef: gcs-secret

```

6. Apply the Custom Resource specification: `kubectl apply -f SearchHeadCluster.yaml`

The App Framework detects the Splunk app or add-on archive files available in the App Source locations, and deploys the apps from the `adminApps`  folder to the Deployer instance for local use.

The apps in the `searchApps` and `machineLearningApps` folders are deployed to the Deployer for use on the clustered search heads. The Deployer is responsible for deploying those apps to the search heads.

Note: The Splunk search head restarts are triggered by the contents of the Splunk apps deployed, and are not initiated by the App Framework.

The App Framework maintains a checksum for each app or add-on archive file in the App Source location. The app name and checksum is recorded in the CR, and used to compare the deployed apps to the app archive files in the App Source location. The App Framework will scan for changes to the App Source folders using the polling interval, and deploy any updated apps to the instance. For the App Framework to detect that an app or add-on had changed, the updated app must use the same archive file name as the previously deployed one.

By default, the App Framework polls the remote object storage location for new or changed apps at the `appsRepoPollIntervalSeconds` interval. To disable the interval check, and manage app updates manually, see the [Manual initiation of app management](#manual-initiation-of-app-management).

For more information, see the [Description of App Framework Specification fields](#description-of-app-framework-specification-fields).

## Description of App Framework Specification fields
The App Framework configuration is supported on the following Custom Resources: Standalone, ClusterManager, SearchHeadCluster, MonitoringConsole and LicenseManager. Configuring the App framework requires:

* Remote Source of Apps: Define the remote storage location, including unique folders, and the path to each folder.
* Destination of Apps: Define which Custom Resources need to be configured.
* Scope of Apps: Define if the apps need to be installed and run locally (such as Standalone, Monitoring Console and License Manager,) or cluster-wide (such as Indexer Cluster, and Search Head Cluster.)

Here is a typical App framework configuration in a Custom Resource definition:

```yaml
              appRepo:
                description: Splunk Enterprise App repository. Specifies remote App
                  location and scope for Splunk App management
                properties:
                  appSources:
                    description: List of App sources on remote storage
                    items:
                      description: AppSourceSpec defines list of App package (*.spl,
                        *.tgz) locations on remote volumes
                      properties:
                        location:
                          description: Location relative to the volume path
                          type: string
                        name:
                          description: Logical name for the set of apps placed in
                            this location. Logical name must be unique to the appRepo
                          type: string
                        scope:
                          description: 'Scope of the App deployment: cluster,  local.
                            Scope determines whether the App(s) is/are installed locally
                            or cluster-wide'
                          type: string
                        volumeName:
                          description: Remote Storage Volume name
                          type: string
                      type: object
                    type: array
                  appsRepoPollIntervalSeconds:
                    description: Interval in seconds to check the Remote Storage for
                      App changes
                    type: integer
                  defaults:
                    description: Defines the default configuration settings for App
                      sources
                    properties:
                      scope:
                        description: 'Scope of the App deployment: cluster, local.
                          Scope determines whether the App(s) is/are installed locally
                          or cluster-wide'
                        type: string
                      volumeName:
                        description: Remote Storage Volume name
                        type: string
                    type: object
                  volumes:
                    description: List of remote storage volumes
                    items:
                      description: VolumeSpec defines remote volume config
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
                        provider:
                          description: App Package Remote Store provider. Currently supported proiders are aws, minio and azure
                          type: string
                        region:
                          description: Region of the remote storage volume where apps
                            reside. Not required for azure.
                          type: string
                        secretRef:
                          description: Secret object name
                          type: string
                        storageType:
                          description: Remote Storage type. Possible values are s3 (works with aws and minio) or blob (works with azure)
                          type: string
                      type: object
                    type: array
                type: object
```

### appRepo

`appRepo` is the start of the App Framework specification, and contains all the configurations required for App Framework to be successfully configured.

### volumes

`volumes` defines the remote storage configurations. The App Framework expects any apps to be installed in various Splunk deployments to be hosted in one or more remote storage volumes.

* `name` uniquely identifies the remote storage volume name within a CR. This is used by the Operator to identify the local volume.
* `storageType` describes the type of remote storage. Currently, `s3`, `blob` are the supported storage type.
* `provider` describes the remote storage provider. Currently, `aws`, `minio` `gcp` and `azure` are the supported providers. Use `s3` with `aws` or `minio`, use `blob` with `azure` or `gcp`
* `endpoint` describes the URI/URL of the remote storage endpoint that hosts the apps.
* `secretRef` refers to the K8s secret object containing the static remote storage access key.  This parameter is not required if using IAM role based credentials.
* `path` describes the path (including the folder) of one or more app sources on the remote store.

### appSources

`appSources` defines the name and scope of the appSource, the remote storage volume, and its location.

NOTE: If an app source name needs to be changed, make sure the name change is persisted across the app framework spec and CR status. The Splunk Operator should automatically update the app path in the CR and on the pod on the next reconciliation.

* `name` uniquely identifies the App source configuration within a CR. This used locally by the Operator to identify the App source.
* `scope` defines the scope of the app to be installed.
  * If the scope is `local`, the apps will be installed and run locally on the pod referred to by the CR.
  * If the scope is `cluster`, the apps will be placed onto the configuration management node (Deployer, Cluster Manager) for deployment across the cluster referred to by the CR.
  * The cluster scope is only supported on CRs that manage cluster-wide app deployment.

    | CRD Type          | Scope support                          | App Framework support |
    | :---------------- | :------------------------------------- | :-------------------- |
    | ClusterManager    | cluster, local                         | Yes                   |
    | SearchHeadCluster | cluster, local                         | Yes                   |
    | Standalone        | local                                  | Yes                   |
    | LicenceManager    | local                                  | Yes                   |
    | MonitoringConsole | local                                  | Yes                   |
    | IndexerCluster    | N/A                                    | No                    |

* `volume` refers to the remote storage volume name configured under the `volumes` stanza (see previous section.)
* `location` helps configure the specific appSource present under the `path` within the `volume`, containing the apps to be installed.

### appsRepoPollIntervalSeconds

If app framework is enabled, the Splunk Operator creates a namespace scoped configMap named **splunk-\<namespace\>-manual-app-update**, which is used to manually trigger the app updates. The App Framework uses the polling interval `appsRepoPollIntervalSeconds` to check for additional apps, or modified apps on the remote object storage.

When `appsRepoPollIntervalSeconds` is set to `0` for a CR, the App Framework will not perform a check until the configMap `status` field is updated manually. See [Manual initiation of app management](#manual_initiation_of_app_management).

## Add a persistent storage volume to the Operator pod

Note:- If the persistent storage volume is not configured for the Operator, by default, the App Framework uses the main memory(RAM) as the staging area for app package downloads. In order to avoid pressure on the main memory, it is strongly advised to use a persistent volume for the operator pod.

1. Create the persistent volume used by the Operator pod to cache apps and add-ons:

```yaml
kind: PersistentVolumeClaim
apiVersion: v1
metadata:
  name: operator-volume-claim
spec:
  accessModes:
    - ReadWriteOnce
  resources:
    requests:
      storage: 8Gi
  storageClassName: gp2
```

2. Associate the persistent volume with the Operator pod by updating the Operator configuration:

```yaml
volumes:
- name: app-staging
  persistentVolumeClaim:
    claimName: operator-volume-claim
```

3. Mount the volume on the path:

```yaml
volumeMounts:
- mountPath: /opt/splunk/appframework/
  name: app-staging
```

A full example of the Operator configuration:

```yaml
kind: PersistentVolumeClaim
apiVersion: v1
metadata:
  name: operator-volume-claim
spec:
  accessModes:
    - ReadWriteOnce
  resources:
    requests:
      storage: 8Gi
  storageClassName: gp2
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: splunk-operator
spec:
  replicas: 1
  selector:
    matchLabels:
      name: splunk-operator
  template:
    metadata:
      labels:
        name: splunk-operator
    spec:
      securityContext:
        fsGroup: 1001
      serviceAccountName: splunk-operator
      containers:
      - name: splunk-operator
        image: "docker.io/splunk/splunk-operator:2.8.1"
        volumeMounts:
        - mountPath: /opt/splunk/appframework/
          name: app-staging
        imagePullPolicy: IfNotPresent
        env:
        - name: WATCH_NAMESPACE
          valueFrom:
            fieldRef:
              fieldPath: metadata.namespace
        - name: POD_NAME
          valueFrom:
            fieldRef:
              fieldPath: metadata.name
        - name: OPERATOR_NAME
          value: "splunk-operator"
        - name: RELATED_IMAGE_SPLUNK_ENTERPRISE
          value: "docker.io/splunk/splunk:9.4.0"

      volumes:
      - name: app-staging
        persistentVolumeClaim:
          claimName: operator-volume-claim
```


## Manual Initiation of App Management

You can control how the App Framework manages app updates by configuring the polling behavior. This allows you to prevent the App Framework from automatically polling the remote storage for app changes and instead manually trigger app updates when desired.

### Disabling Automatic Polling

To disable the App Framework's automatic polling of the remote storage for app changes, set the `appsRepoPollIntervalSeconds` setting to `0`. This configuration stops the App Framework from periodically checking for app updates and updates the `configMap` with a new `status` field. 

**Note:** The App Framework will still perform an initial poll of the remote storage even when polling is disabled upon CR initialization.

```yaml
appsRepoPollIntervalSeconds: 0
```

### Manual Triggering of App Checks

When you are ready to initiate an app check using the App Framework, you need to manually update the `status` field in the `configMap`. The `status` field defaults to `off`.

#### Namespace-Specific ConfigMap

The primary `configMap` used for manual updates is namespace-specific. For example, if you have deployed a Standalone Custom Resource (CR) with the App Framework enabled, the Splunk Operator will create a `configMap` named `splunk-default-manual-app-update` (assuming the `default` namespace).

##### Example Standalone CR Deployment

```bash
kubectl get standalone
NAME   PHASE   DESIRED   READY   AGE
s1     Ready   1         1       13h
```

##### Generated Namespace-Specific ConfigMap

```yaml
apiVersion: v1
data:
  Standalone: |-
    status: off
    refCount: 1
kind: ConfigMap
metadata:
  creationTimestamp: "2021-08-24T01:04:01Z"
  name: splunk-manual-app-update
  namespace: default
  ownerReferences:
  - apiVersion: enterprise.splunk.com/v3
    controller: false
    kind: Standalone
    name: s1
    uid: ddb9528f-2e25-49be-acd4-4fadde489849
  resourceVersion: "75406013"
  selfLink: /api/v1/namespaces/default/configmaps/splunk-manual-app-update
  uid: 413c6053-af4f-4cb3-97e0-6dbe7cd17721
```

To trigger a manual app check, update the `status` field to `on`:

```bash
kubectl patch cm/splunk-default-manual-app-update --type merge -p '{"data":{"Standalone":"status: on\nrefCount: 1"}}'
```

The App Framework will perform its checks, update or install apps as necessary, and reset the `status` to `off` upon completion.

#### Per Custom Resource ConfigMap

In addition to the namespace-specific `configMap`, the system now supports a `configMap` per custom resource. This provides finer control over app updates for individual CRs.

**ConfigMap Naming Convention:**

```
splunk-<namespace>-<custom-resource-name>-configmap
```

**Behavior:**

1. **Creation:** When a custom resource is created, a corresponding `configMap` is automatically created alongside other resources.
   
2. **Manual Update Check:**
   - The system first checks the namespace-specific `configMap` (`splunk-default-manual-app-update`).
   - If manual updates are not enabled in the namespace-specific `configMap`, it then checks the per CR `configMap` for the `manualUpdate` field.
   - If `manualUpdate: true` is set in the per CR `configMap`, the App Framework performs the app check and resets the field to `manualUpdate: false` after completing the task.
   - For Indexer Cluster update, use the Cluster Manager configmap.  Individual Indexer Cluster configmap cannot be used for manual app update.

**Example Per CR ConfigMap:**

```yaml
apiVersion: v1
data:
  manualUpdate: "true"
kind: ConfigMap
metadata:
  name: splunk-default-s1-configmap
  namespace: default
  ownerReferences:
  - apiVersion: enterprise.splunk.com/v3
    controller: true
    kind: Standalone
    name: s1
    uid: ddb9528f-2e25-49be-acd4-4fadde489849
```

To trigger a manual app check for a specific custom resource, update the `manualUpdate` field to `true`:

```bash
kubectl patch cm/splunk-default-s1-configmap --type merge -p '{"data":{"manualUpdate":"true"}}'
```

The App Framework will perform the necessary app checks and reset `manualUpdate` to `false` once completed.

### Reinstate Automatic Polling

If you wish to re-enable automatic polling, update the CR's `appsRepoPollIntervalSeconds` setting to a value greater than `0`.

```yaml
appsRepoPollIntervalSeconds: 60
```

### Important Considerations

- **Consistency Across CRs:** All CRs of the same type within a namespace must have polling either enabled or disabled uniformly. For example, if `appsRepoPollIntervalSeconds` is set to `0` for one Standalone CR, all other Standalone CRs in the same namespace must also have polling disabled.
  
- **Avoid Mixed Configurations:** Using a mix of enabled and disabled polling across CRs of the same type can lead to unexpected behavior. Use the `kubectl` command to identify and ensure consistent polling configurations across all CRs before making changes.

- **Namespace and CR-Specific Configuration:** The system prioritizes the namespace-specific `configMap` for manual updates. If it is not enabled, it falls back to the per CR `configMap`. This hierarchical approach ensures that manual updates can be managed both at the namespace level and for individual resources as needed.

By following these guidelines, you can effectively manage when and how the App Framework checks for and applies app updates, providing both broad and granular control over your application environment.


## App Framework Limitations

The App Framework does not preview, analyze, verify versions, or enable Splunk Apps and Add-ons. The administrator is responsible for previewing the app or add-on contents, verifying the app is enabled, and that the app is supported with the version of Splunk Enterprise deployed in the containers. For Splunk app packaging specifications see [Package apps for Splunk Cloud or Splunk Enterprise](https://dev.splunk.com/enterprise/docs/releaseapps/packageapps/) in the Splunk Enterprise Developer documentation. The app archive files must end with .spl or .tgz; all other files are ignored.

1. The App Framework has no support to remove an app or add-on once it’s been deployed. To disable an app, update the archive contents located in the App Source, and set the app.conf state to disabled.

2. The App Framework defines one worker per CR type. For example, if you have multiple clusters receiveing app updates, a delay while managing one cluster will delay the app updates to the other cluster.

## Setup Azure Blob Access with Managed Identity

Azure Managed Identities can be used to provide IAM access to the blobs. With managed identities, the AKS nodes that host the pods can retrieve an OAuth token that provides authorization for the Splunk Operator pod to read the app packages stored in the Azure Storage account. The key point here is that the AKS node is associated with a Managed Identity, and this managed identity is given a `role` for read access called `Storage Blob Data Reader` to the Azure Storage account.

### **Assumptions:**

- Familiarize yourself with [AKS managed identity concepts](https://learn.microsoft.com/en-us/azure/aks/use-managed-identity)
- The names used below, such as resource-group name and AKS cluster name, are for example purposes only. Please change them to the values as per your setup.
- These steps cover creating a resource group and AKS cluster; you can skip them if you already have them created.

### **Steps to Assign Managed Identity:**

1. **Create an Azure Resource Group**

    ```bash
    az group create --name splunkOperatorResourceGroup --location westus2
    ```

2. **Create AKS Cluster with Managed Identity Enabled**

    ```bash
    az aks create -g splunkOperatorResourceGroup -n splunkOperatorCluster --enable-managed-identity
    ```

3. **Get Credentials to Access Cluster**

    ```bash
    az aks get-credentials --resource-group splunkOperatorResourceGroup --name splunkOperatorCluster
    ```

4. **Get the Kubelet User Managed Identity**

    Run:

    ```bash
    az identity list
    ```

    Find the section that has `<AKS Cluster Name>-agentpool` under `name`. For example, look for the block that contains:

    ```json
    {
      "clientId": "a5890776-24e6-4f5b-9b6c-**************",
      "id": "/subscriptions/<subscription-id>/resourceGroups/MC_splunkOperatorResourceGroup_splunkOperatorCluster_westus2/providers/Microsoft.ManagedIdentity/userAssignedIdentities/splunkOperatorCluster-agentpool",
      "location": "westus2",
      "name": "splunkOperatorCluster-agentpool",
      "principalId": "f0f04120-6a36-49bc--**************",
      "resourceGroup": "MC_splunkOperatorResourceGroup_splunkOperatorCluster_westus2",
      "tags": {},
      "tenantId": "8add7810-b62a--**************",
      "type": "Microsoft.ManagedIdentity/userAssignedIdentities"
    }
    ```

    Extract the `principalId` value from the output above. Alternatively, use the following command to get the `principalId`:

    ```bash
    az identity show --name <identityName> --resource-group "<resourceGroup>" --query 'principalId' --output tsv
    ```

    **Example:**

    ```bash
    principalId=$(az identity show --name splunkOperatorCluster-agentpool --resource-group "MC_splunkOperatorResourceGroup_splunkOperatorCluster_westus2" --query 'principalId' --output tsv)
    echo $principalId
    ```

    Output:

    ```
    f0f04120-6a36-49bc--**************
    ```

5. **Assign Read Access for Kubelet User Managed Identity to the Storage Account**

    Use the `principalId` from the above section and assign it to the storage account:

    ```bash
    az role assignment create --assignee "<principalId>" --role 'Storage Blob Data Reader' --scope /subscriptions/<subscription_id>/resourceGroups/<storageAccountResourceGroup>/providers/Microsoft.Storage/storageAccounts/<storageAccountName>
    ```

    **For Example:**

    If `<storageAccountResourceGroup>` is `splunkOperatorResourceGroup` and `<storageAccountName>` is `mystorageaccount`, the command would be:

    ```bash
    az role assignment create --assignee "f0f04120-6a36-49bc--**************" --role 'Storage Blob Data Reader' --scope /subscriptions/f428689e-c379-4712--**************/resourceGroups/splunkOperatorResourceGroup/providers/Microsoft.Storage/storageAccounts/mystorageaccount
    ```

    After this command, you can use the App Framework for Azure Blob without secrets.

### **Azure Blob Authorization Recommendations:**

- **Granular Access:** Azure allows **"Managed Identities"** assignment at the **"storage accounts"** level as well as at specific containers (buckets) levels. A managed identity assigned read permissions at a storage account level will have read access for all containers within that storage account. As a good security practice, assign the managed identity to only the specific containers it needs to access, rather than the entire storage account.
  
- **Avoid Shared Access Keys:** In contrast to **"Managed Identities"**, Azure allows **"shared access keys"** configurable only at the storage accounts level. When using the `secretRef` configuration in the CRD, the underlying secret key will allow both read and write access to the storage account (and all containers within it). Based on your security needs, consider using "Managed Identities" instead of secrets. Additionally, there's no automated way to rotate the secret key, so if you're using these keys, rotate them regularly (e.g., every 90 days).

---

## Setup Azure Blob Access with Azure Workload Identity

Azure Workload Identity provides a Kubernetes-native approach to authenticate workloads running in your cluster to Azure services, such as Azure Blob Storage, without managing credentials manually. This section outlines how to set up Azure Workload Identity to securely access Azure Blob Storage from the Splunk Operator running on AKS.

### **Assumptions:**

- Familiarize yourself with [Azure AD Workload Identity concepts](https://learn.microsoft.com/en-us/azure/active-directory/workload-identity/overview)
- The names used below, such as resource-group name and AKS cluster name, are for example purposes only. Please change them to the values as per your setup.
- These steps cover creating a resource group and AKS cluster with Azure Workload Identity enabled; skip if already created.

### **Steps to Assign Azure Workload Identity:**

1. **Create an Azure Resource Group**

    ```bash
    az group create --name splunkOperatorWorkloadIdentityRG --location westus2
    ```

2. **Create AKS Cluster with Azure Workload Identity Enabled**

    ```bash
    az aks create -g splunkOperatorWorkloadIdentityRG -n splunkOperatorWICluster --enable-oidc-issuer --enable-managed-identity
    ```

    **Parameters:**
    - `--enable-oidc-issuer`: Enables the OIDC issuer required for Workload Identity.
    - `--enable-managed-identity`: Enables Managed Identity for the cluster.

3. **Get Credentials to Access Cluster**

    ```bash
    az aks get-credentials --resource-group splunkOperatorWorkloadIdentityRG --name splunkOperatorWICluster
    ```

4. **Install Azure AD Workload Identity in Kubernetes**

    Azure AD Workload Identity requires installing specific components into your Kubernetes cluster.

    **Using Helm:**

    ```bash
    helm repo add azure-workload-identity https://azure.github.io/azure-workload-identity/charts
    helm repo update

    # Create a namespace for workload identity (optional but recommended)
    kubectl create namespace workload-identity-system

    # Install the Azure Workload Identity Helm chart
    helm install azure-workload-identity azure-workload-identity/azure-workload-identity \
      --namespace workload-identity-system \
      --set azureIdentityBindingSelector="splunk-operator"
    ```

    **Parameters:**
    - `azureIdentityBindingSelector`: Selector used to bind `AzureIdentityBinding` resources to specific Kubernetes service accounts. In this case, it's set to `"splunk-operator"`.

5. **Create a User-Assigned Managed Identity**

    ```bash
    az identity create \
      --name splunkOperatorWIIdentity \
      --resource-group splunkOperatorWorkloadIdentityRG \
      --location westus2
    ```

    **Retrieve Managed Identity Details:**

    ```bash
    az identity show \
      --name splunkOperatorWIIdentity \
      --resource-group splunkOperatorWorkloadIdentityRG \
      --query "{clientId: clientId, principalId: principalId, id: id}" \
      --output json
    ```

    **Sample Output:**

    ```json
    {
      "clientId": "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
      "principalId": "yyyyyyyy-yyyy-yyyy-yyyy-yyyyyyyyyyyy",
      "id": "/subscriptions/<subscription-id>/resourceGroups/splunkOperatorWorkloadIdentityRG/providers/Microsoft.ManagedIdentity/userAssignedIdentities/splunkOperatorWIIdentity"
    }
    ```

6. **Assign the `Storage Blob Data Contributor` Role to the Managed Identity**

    ```bash
    az role assignment create \
      --assignee <clientId> \
      --role "Storage Blob Data Contributor" \
      --scope /subscriptions/<subscription-id>/resourceGroups/<storageAccountResourceGroup>/providers/Microsoft.Storage/storageAccounts/<storageAccountName>
    ```

    **Example:**

    ```bash
    az role assignment create \
      --assignee "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx" \
      --role "Storage Blob Data Contributor" \
      --scope /subscriptions/f428689e-c379-4712--**************/resourceGroups/splunkOperatorResourceGroup/providers/Microsoft.Storage/storageAccounts/mystorageaccount
    ```

7. **Create Kubernetes Service Account for Splunk Operator**

    Create a Kubernetes Service Account annotated to use Azure Workload Identity.

    ```yaml
    # splunk-operator-wi-serviceaccount.yaml

    apiVersion: v1
    kind: ServiceAccount
    metadata:
      name: bucket-admin-test-wi
      namespace: your-splunk-operator-namespace
      labels:
        azure.workload.identity/client-id: xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx # clientId from the Managed Identity
    ```

    **Apply the Service Account:**

    ```bash
    kubectl apply -f splunk-operator-wi-serviceaccount.yaml
    ```

8. **Create AzureIdentity and AzureIdentityBinding Resources**

    These resources link the Kubernetes Service Account to the Azure Managed Identity.

    ```yaml
    # azureidentity-wi.yaml

    apiVersion: workloadidentity.azure.com/v1alpha1
    kind: AzureIdentity
    metadata:
      name: splunkOperatorWIIdentity
      namespace: workload-identity-system
    spec:
      type: 0 # 0 for User Assigned Managed Identity
      resourceID: /subscriptions/<subscription-id>/resourceGroups/splunkOperatorWorkloadIdentityRG/providers/Microsoft.ManagedIdentity/userAssignedIdentities/splunkOperatorWIIdentity
      clientID: xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx # clientId from the Managed Identity
    ```

    ```yaml
    # azureidentitybinding-wi.yaml

    apiVersion: workloadidentity.azure.com/v1alpha1
    kind: AzureIdentityBinding
    metadata:
      name: splunkOperatorWIIdentityBinding
      namespace: workload-identity-system
    spec:
      azureIdentity: splunkOperatorWIIdentity
      selector: splunk-operator-wi
    ```

    **Apply the Resources:**

    ```bash
    kubectl apply -f azureidentity-wi.yaml
    kubectl apply -f azureidentitybinding-wi.yaml
    ```

9. **Annotate Kubernetes Service Account to Use Workload Identity**

    Update the Splunk Operator Deployment to use the annotated Service Account.

    ```yaml
    # splunk-operator-deployment-wi.yaml

    apiVersion: apps/v1
    kind: Deployment
    metadata:
      name: splunk-operator
      namespace: your-splunk-operator-namespace
      labels:
        app: splunk-operator
    spec:
      replicas: 1
      selector:
        matchLabels:
          app: splunk-operator
      template:
        metadata:
          labels:
            app: splunk-operator
          annotations:
            azure.workload.identity/use: "true"
        spec:
          serviceAccountName: bucket-admin-test-wi
          containers:
          - name: splunk-operator
            image: your-splunk-operator-image
            # ... other configurations
    ```

    **Apply the Updated Deployment:**

    ```bash
    kubectl apply -f splunk-operator-deployment-wi.yaml
    ```

10. **Verify the Setup**

    - **Check Pod Annotations:**

        ```bash
        kubectl get pods -n your-splunk-operator-namespace -o jsonpath='{.items[*].metadata.annotations}'
        ```

        You should see an annotation similar to:

        ```json
        {
          "azure.workload.identity/use": "true"
        }
        ```

    - **Test Azure Blob Storage Access from the Pod:**

        ```bash
        kubectl exec -it <splunk-operator-pod> -n your-splunk-operator-namespace -- /bin/bash
        ```

        Inside the pod, use the Azure CLI or Azure SDK to list blobs:

        ```bash
        az storage blob list --account-name mystorageaccount --container-name mycontainer --output table
        ```

        **Note:** Ensure that the Azure CLI is installed in the pod or use appropriate Azure SDK commands within your application code.

    - **Check Logs for Authentication Success:**

        ```bash
        kubectl logs deployment/splunk-operator -n your-splunk-operator-namespace
        ```

        Look for log entries indicating successful authentication and blob storage access.

### **Azure Workload Identity Authorization Recommendations:**

- **Granular Role Assignments:** Assign the Managed Identity the least privilege necessary. Prefer roles like `Storage Blob Data Reader` at the container level instead of the entire storage account to minimize exposure.
  
- **Avoid Shared Access Keys:** Similar to Managed Identities, avoid using shared access keys when possible. They grant broader access and require manual rotation.

- **Secure Service Accounts:** Ensure that Kubernetes Service Accounts used with Workload Identity are restricted to only the necessary namespaces and roles.

---

### **Azure Workload Identity Authorization Recommendations:**

Azure Workload Identity allows you to assign IAM roles at more granular levels, enhancing security by limiting access only to the necessary resources.

- **Granular Role Assignments:** Assign the Managed Identity the least privilege necessary. Prefer roles like `Storage Blob Data Reader` at the container level instead of the entire storage account to minimize exposure.
  
- **Avoid Shared Access Keys:** Similar to Managed Identities, avoid using shared access keys when possible. They grant broader access and require manual rotation.

- **Secure Service Accounts:** Ensure that Kubernetes Service Accounts used with Workload Identity are restricted to only the necessary namespaces and roles.

### **Benefits of Using Azure Workload Identity:**

- **Kubernetes-Native:** Seamlessly integrates with Kubernetes Service Accounts, allowing workloads to authenticate without managing secrets.
  
- **Enhanced Security:** Eliminates the need to store credentials in pods or Kubernetes secrets, reducing the attack surface.
  
- **Scalability:** Easily assign the same identity to multiple pods or workloads, simplifying management.

### **Comparison Between Managed Identity and Workload Identity:**

| Feature                     | Managed Identity                                 | Workload Identity                                   |
|-----------------------------|--------------------------------------------------|-----------------------------------------------------|
| **Scope**                   | Tied to the Azure resource (e.g., AKS node)      | Tied to Kubernetes Service Accounts                |
| **Credential Management**   | Azure manages credentials                        | Kubernetes manages Service Account credentials      |
| **Flexibility**             | Limited to Azure resources                       | More flexible, integrates with Kubernetes-native identities |
| **Granularity**             | Role assignments at Azure resource level         | Role assignments at Kubernetes namespace or service account level |
| **Use Cases**               | Simple scenarios where workloads share identities | Complex scenarios requiring granular access controls |

### **When to Use Which:**

- **Managed Identity:** Suitable for scenarios where workloads are tightly coupled with specific Azure resources and require straightforward IAM access.
  
- **Workload Identity:** Ideal for Kubernetes-native environments where fine-grained access control and integration with Kubernetes Service Accounts are essential.





## Setup Google Cloud Storage Access for App Framework

The Splunk Operator requires access to Google Cloud Storage (GCS) buckets to retrieve app packages and add-ons. You can configure this access using one of the following two methods:

1. **Using a Kubernetes Secret with a GCP Service Account JSON Key File**
2. **Using Workload Identity for Secure Access Without Service Account Keys**

### **Prerequisites**

Before proceeding, ensure you have the following:

- **Google Cloud Platform (GCP) Account**: Access to a GCP project with permissions to create and manage service accounts and IAM roles.
- **Kubernetes Cluster**: A running Kubernetes cluster (e.g., GKE) with `kubectl` configured.
- **Splunk Operator Installed**: The Splunk Operator should be installed and running in your Kubernetes cluster.
- **Google Cloud SDK (`gcloud`)**: Installed and authenticated with your GCP account. [Install Google Cloud SDK](https://cloud.google.com/sdk/docs/install)

---

## Option 1: Using a Kubernetes Secret for GCP Access


### Setup Google Cloud Storage Access for App Framework
The Splunk Operator requires access to Google Cloud Storage (GCS) buckets to retrieve app packages and add-ons. You can configure this access using one of the following two methods:

1. Using a Kubernetes Secret with a GCP Service Account JSON Key File
2. Using Workload Identity for Secure Access Without Service Account Keys

### Prerequisites
Before proceeding, ensure you have the following:

- Google Cloud Platform (GCP) Account: Access to a GCP project with permissions to create and manage service accounts and IAM roles.
- Kubernetes Cluster: A running Kubernetes cluster (e.g., GKE) with kubectl configured.
- Splunk Operator Installed: The Splunk Operator should be installed and running in your Kubernetes cluster.
- Google Cloud SDK (gcloud): Installed and authenticated with your GCP account. Install Google Cloud SDK

### Option 1: Using a Kubernetes Secret for GCP Access

This method involves creating a Kubernetes Secret that stores a GCP service account JSON key file. The Splunk Operator will use this secret to authenticate and access the GCS bucket.

#### **Steps to Configure Access Using a Kubernetes Secret**

1. **Create a GCP Service Account**

    - **Navigate to GCP Console**:
      - Go to the [Google Cloud Console](https://console.cloud.google.com/).
    
    - **Create Service Account**:
      - Navigate to **IAM & Admin > Service Accounts**.
      - Click **Create Service Account**.
      - **Service Account Details**:
        - **Name**: `splunk-app-framework-sa`
        - **Description**: (Optional) e.g., `Service account for Splunk Operator to access GCS buckets`
      - Click **Create and Continue**.
    
    - **Grant Service Account Permissions**:
      - Assign the **Storage Object Viewer** role to grant read access to the required GCS buckets.
      - Click **Done**.

2. **Download the Service Account Key**

    - **Locate the Service Account**:
      - In the **Service Accounts** page, find `splunk-app-framework-sa`.
    
    - **Generate Key**:
      - Click on **Actions (⋮) > Manage Keys**.
      - Click **Add Key > Create New Key**.
      - **Key Type**: Select **JSON**.
      - Click **Create**.
      - A JSON key file (`splunk-app-framework-sa-key.json`) will be downloaded. **Store this file securely**, as it contains sensitive credentials.

3. **Create a Kubernetes Secret**

    - **Upload the Service Account Key as a Secret**:
      - Use the downloaded JSON key file to create a Kubernetes Secret in the namespace where the Splunk Operator is installed (e.g., `splunk-operator`).
      
      ```bash
      kubectl create secret generic gcs-secret \
        --from-file=key.json=/path/to/splunk-app-framework-sa-key.json \
        -n splunk-operator
      ```
      
      - **Parameters**:
        - `gcs-secret`: Name of the Kubernetes Secret.
        - `/path/to/splunk-app-framework-sa-key.json`: Path to your downloaded JSON key file.
        - `-n splunk-operator`: Namespace where the Splunk Operator is deployed.

4. **Configure Splunk Operator to Use the Kubernetes Secret**

    - **Update Custom Resource Definition (CRD)**:
      - Ensure that your Splunk Operator's CRD references the `gcs-secret` for GCS access.
      
      ```yaml
      apiVersion: enterprise.splunk.com/v3
      kind: Standalone
      metadata:
        name: example-splunk-app
        namespace: splunk-operator
      spec:
        appRepo:
            appInstallPeriodSeconds: 90
            appSources:
            - location: c3appfw-idxc-mj00
              name: appframework-idxc-clusterypt
              premiumAppsProps:
                esDefaults: {}
              scope: cluster
              volumeName: appframework-test-volume-idxc-k3r
            appsRepoPollIntervalSeconds: 60
            defaults:
              premiumAppsProps:
                esDefaults: {}
              scope: cluster
              volumeName: appframework-test-volume-idxc-k3r
            installMaxRetries: 2
            volumes:
            - endpoint: https://storage.googleapis.com
              name: appframework-test-volume-idxc-k3r
              path: splk-integration-test-bucket
              provider: gcp
              region: ""
              secretRef: splunk-s3-index-masterc3appfw-iwz-vzv
              storageType: gcs
        # ... other configurations
      ```
      
      - **Explanation of Key Fields**:
        - **`secretRef`**: References the Kubernetes Secret (`gcs-secret`) created earlier, allowing the Splunk Operator to access the GCS bucket securely without embedding credentials directly in the CRD.
        - **`endpoint`**: Specifies the GCS endpoint.
        - **`path`**: Path to the GCS bucket (`splk-integration-test-bucket` in this example).
        - **`provider`**: Specifies the cloud provider (`gcp` for Google Cloud Platform).
        - **`storageType`**: Indicates the type of storage (`gcs` for Google Cloud Storage).

5. **Deploy or Update Splunk Operator Resources**

    - **Apply the Updated CRD**:
    
      ```bash
      kubectl apply -f splunk-app-crd.yaml
      ```
      
      - Replace `splunk-app-crd.yaml` with the path to your updated CRD file.

6. **Verify the Configuration**

    - **Check Pods**:
    
      ```bash
      kubectl get pods -n splunk-operator
      ```
      
      - Ensure that the Splunk Operator pods are running without errors.
    
    - **Inspect Logs**:
    
      ```bash
      kubectl logs <splunk-operator-pod-name> -n splunk-operator
      ```
      
      - Look for logs indicating successful access to the GCS bucket.

#### **Security Recommendations**

- **Least Privilege Principle**:
  - Assign only the necessary roles to the service account. In this case, `Storage Object Viewer` grants read access. If write access is required, consider `Storage Object Admin`.
  
- **Secure Storage of Keys**:
  - Protect the JSON key file and the Kubernetes Secret to prevent unauthorized access.
  
- **Regular Rotation of Keys**:
  - Periodically rotate the service account keys to enhance security.

---

### Option 2: Using Workload Identity for GCP Access

Workload Identity allows Kubernetes workloads to authenticate to GCP services without the need for managing service account keys. This method leverages GCP's Workload Identity to securely bind Kubernetes service accounts to GCP service accounts.

#### **Advantages of Using Workload Identity**

- **Enhanced Security**: Eliminates the need to handle service account keys, reducing the risk of key leakage.
- **Simplified Management**: Simplifies the authentication process by integrating directly with Kubernetes service accounts.
- **Automatic Key Rotation**: GCP manages the credentials, including rotation, ensuring up-to-date security practices.

#### **Steps to Configure Access Using Workload Identity**

1. **Enable Workload Identity on Your GKE Cluster**

    - **Prerequisite**: Ensure your GKE cluster is created with Workload Identity enabled. If not, enable it during cluster creation or update an existing cluster.
    
    - **During Cluster Creation**:
    
      ```bash
      gcloud container clusters create splunkOperatorWICluster \
        --resource-group splunkOperatorWorkloadIdentityRG \
        --workload-pool=<PROJECT_ID>.svc.id.goog \
        --enable-workload-identity
      ```
      
      - Replace `<PROJECT_ID>` with your GCP project ID.
    
    - **For Existing Clusters**:
    
      ```bash
      gcloud container clusters update splunkOperatorWICluster \
        --resource-group splunkOperatorWorkloadIdentityRG \
        --workload-pool=<PROJECT_ID>.svc.id.goog
      ```
      
      - **Note**: Enabling Workload Identity on an existing cluster might require cluster reconfiguration and could lead to temporary downtime.

2. **Create a GCP Service Account and Assign Permissions**

    - **Create Service Account**:
    
      ```bash
      gcloud iam service-accounts create splunk-app-framework-sa \
        --display-name "Splunk App Framework Service Account"
      ```
    
    - **Grant Required Roles**:
    
      ```bash
      gcloud projects add-iam-policy-binding <PROJECT_ID> \
        --member "serviceAccount:splunk-app-framework-sa@<PROJECT_ID>.iam.gserviceaccount.com" \
        --role "roles/storage.objectViewer"
      ```
      
      - Replace `<PROJECT_ID>` with your GCP project ID.

3. **Create a Kubernetes Service Account**

    - **Define Service Account**:
    
      ```bash
      kubectl create serviceaccount splunk-operator-sa \
        -n splunk-operator
      ```
      
      - **Parameters**:
        - `splunk-operator-sa`: Name of the Kubernetes Service Account.
        - `-n splunk-operator`: Namespace where the Splunk Operator is deployed.

4. **Associate the GCP Service Account with the Kubernetes Service Account**

    - **Establish IAM Policy Binding**:
    
      ```bash
      gcloud iam service-accounts add-iam-policy-binding splunk-app-framework-sa@<PROJECT_ID>.iam.gserviceaccount.com \
        --role roles/iam.workloadIdentityUser \
        --member "serviceAccount:<PROJECT_ID>.svc.id.goog[splunk-operator/splunk-operator-sa]"
      ```
      
      - **Parameters**:
        - `<PROJECT_ID>`: Your GCP project ID.
        - `splunk-operator`: Kubernetes namespace.
        - `splunk-operator-sa`: Kubernetes Service Account name.

5. **Annotate the Kubernetes Service Account**

    - **Add Annotation to Link Service Accounts**:
    
      ```bash
      kubectl annotate serviceaccount splunk-operator-sa \
        --namespace splunk-operator \
        iam.gke.io/gcp-service-account=splunk-app-framework-sa@<PROJECT_ID>.iam.gserviceaccount.com
      ```
      
      - **Parameters**:
        - `splunk-operator-sa`: Kubernetes Service Account name.
        - `splunk-operator`: Kubernetes namespace.
        - `<PROJECT_ID>`: Your GCP project ID.

6. **Update Splunk Operator Deployment to Use the Annotated Service Account**

    - **Modify Deployment YAML**:
    
      Replace the existing deployment configuration with the following YAML to use the annotated Kubernetes Service Account (`splunk-operator-sa`):

      ```yaml
      # splunk-operator-deployment-wi.yaml
      
      apiVersion: enterprise.splunk.com/v3
      kind: Standalone
      metadata:
        name: example-splunk-app
        namespace: splunk-operator
      spec:
        serviceAccount: splunk-operator-sa
        appRepo:
            appInstallPeriodSeconds: 90
            appSources:
            - location: c3appfw-idxc-mj00
              name: appframework-idxc-clusterypt
              premiumAppsProps:
                esDefaults: {}
              scope: cluster
              volumeName: appframework-test-volume-idxc-k3r
            appsRepoPollIntervalSeconds: 60
            defaults:
              premiumAppsProps:
                esDefaults: {}
              scope: cluster
              volumeName: appframework-test-volume-idxc-k3r
            installMaxRetries: 2
            volumes:
            - endpoint: https://storage.googleapis.com
              name: appframework-test-volume-idxc-k3r
              path: splk-integration-test-bucket
              provider: gcp
              region: ""
              storageType: gcs
        # ... other configurations
      ```
      
      - **Explanation of Key Fields**:
        - **`serviceAccount`**: References the Kubernetes Service Account (`splunk-operator-sa`) that is associated with the GCP Service Account via Workload Identity.
        - **`endpoint`**: Specifies the GCS endpoint.
        - **`path`**: Path to the GCS bucket (`splk-integration-test-bucket` in this example).
        - **`provider`**: Specifies the cloud provider (`gcp` for Google Cloud Platform).
        - **`storageType`**: Indicates the type of storage (`gcs` for Google Cloud Storage).

    - **Apply the Updated Deployment**:
    
      ```bash
      kubectl apply -f splunk-operator-deployment-wi.yaml
      ```

7. **Configure Splunk Operator to Use Workload Identity**

    - **Update Custom Resource Definition (CRD)**:
      - Ensure that your Splunk Operator's CRD is configured to utilize the Kubernetes Service Account `splunk-operator-sa` for GCS access.
      
      ```yaml
      apiVersion: enterprise.splunk.com/v3
      kind: Standalone
      metadata:
        name: example-splunk-app
        namespace: splunk-operator
      spec:
        appRepo:
            appInstallPeriodSeconds: 90
            appSources:
            - location: c3appfw-idxc-mj00
              name: appframework-idxc-clusterypt
              premiumAppsProps:
                esDefaults: {}
              scope: cluster
              volumeName: appframework-test-volume-idxc-k3r
            appsRepoPollIntervalSeconds: 60
            defaults:
              premiumAppsProps:
                esDefaults: {}
              scope: cluster
              volumeName: appframework-test-volume-idxc-k3r
            installMaxRetries: 2
            volumes:
            - endpoint: https://storage.googleapis.com
              name: appframework-test-volume-idxc-k3r
              path: splk-integration-test-bucket
              provider: gcp
              region: ""
              serviceAccount: splunk-operator-sa
              storageType: gcs
        # ... other configurations
      ```
      
      - **Parameters**:
        - `serviceAccount`: Name of the Kubernetes Service Account (`splunk-operator-sa`).

8. **Verify the Configuration**

    - **Check Pods**:
    
      ```bash
      kubectl get pods -n splunk-operator
      ```
      
      - Ensure that the Splunk Operator pods are running without errors.
    
    - **Inspect Logs**:
    
      ```bash
      kubectl logs <splunk-operator-pod-name> -n splunk-operator
      ```
      
      - Look for logs indicating successful access to the GCS bucket via Workload Identity.

#### **Security Recommendations**

- **Least Privilege Principle**:
  - Assign only the necessary roles to the GCP Service Account. Here, `Storage Object Viewer` grants read access. If write access is required, consider `Storage Object Admin`.
  
- **Secure Namespace Configuration**:
  - Ensure that the Kubernetes Service Account (`splunk-operator-sa`) is restricted to the `splunk-operator` namespace to prevent unauthorized access.
  
- **Regular Auditing**:
  - Periodically review IAM roles and permissions to ensure that they adhere to the least privilege principle.
  
- **Avoid Hardcoding Credentials**:
  - With Workload Identity, there's no need to manage or store service account keys, enhancing security.

---

### Comparison Between Service Account Keys and Workload Identity

| Feature                     | Service Account Keys                            | Workload Identity                                   |
|-----------------------------|-------------------------------------------------|-----------------------------------------------------|
| **Credential Management**   | Requires handling and securely storing JSON keys.| Eliminates the need to manage credentials manually.  |
| **Security**                | Higher risk due to potential key leakage.        | Enhanced security by using Kubernetes-native identities. |
| **Ease of Rotation**        | Manual rotation of keys is necessary.           | GCP manages credential rotation automatically.      |
| **Granularity**             | Access is tied to the service account key.      | Fine-grained access control via Kubernetes Service Accounts. |
| **Integration Complexity**  | Simpler to set up initially but harder to manage.| Requires additional setup but offers better security and manageability. |
| **Use Cases**               | Suitable for simpler setups or legacy systems.   | Ideal for Kubernetes-native environments requiring enhanced security. |

#### **When to Use Which:**

- **Service Account Keys**:
  - Use when simplicity is a priority, and the security implications are manageable.
  - Suitable for environments where Workload Identity is not supported or feasible.
  
- **Workload Identity**:
  - Preferable for Kubernetes-native deployments requiring robust security.
  - Ideal for scenarios where automatic credential management and rotation are beneficial.

---

### Best Practices for Google Cloud Storage Access

1. **Adhere to the Least Privilege Principle**:
    - Assign only the necessary roles to service accounts or Managed Identities to minimize security risks.

2. **Use Workload Identity Where Possible**:
    - Leverage Workload Identity for Kubernetes deployments to enhance security and simplify credential management.

3. **Secure Namespace Configuration**:
    - Limit Service Accounts to specific namespaces to prevent unauthorized access across the cluster.

4. **Regularly Audit IAM Roles and Permissions**:
    - Periodically review and adjust roles to ensure they align with current access requirements.

5. **Monitor Access Logs**:
    - Utilize GCP's logging and monitoring tools to track access patterns and detect any anomalies.

6. **Automate Infrastructure as Code (IaC)**:
    - Use tools like Terraform or Helm to manage service accounts, IAM roles, and Kubernetes configurations for consistency and repeatability.

7. **Implement Network Security Controls**:
    - Configure VPC Service Controls or firewall rules to restrict access to GCS buckets from authorized sources only.

---

## App Framework Troubleshooting

The AppFramework feature stores data about the installation of applications in Splunk Enterprise Custom Resources' Status subresource.

The field `cr.status.AppDeploymentContext.AppsSrcDeployStatus` stores the AppFramework deployment statuses of all Application sources listed in the CR spec. Further, each Application under every Application source has detailed deployment information in the field `cr.status.AppDeploymentContext.AppsSrcDeployStatus.AppDeploymentInfo`.

### App Framework Phase Information

The process of installing an application is divided into multiple sequential phases. Each Application has its `current` phase information stored in the field `cr.status.AppDeploymentContext.AppsSrcDeployStatus.AppDeploymentInfo.PhaseInfo`.

Here is a detailed chronological view of the list of phases.

#### Phase 1 - App package download

In this phase, the AppFramework authenticates with the storage provider to download the app/s onto the `Splunk Operator pod PVC`. Below is a description of the statuses during this phase:

| Status Code | Description |
| ------------ | --------- |
| 101 | App Package is pending download |
| 102 | App Package download is in progress |
| 103 | App Package download is complete |
| 199 | App Package is not downloaded after multiple retries |

#### Phase 2 - App package copy

In this phase, the AppFramework copies the application to the Splunk Enterprise pod PVCs'. Below is a description of the statuses during this phase:

| Status Code | Description |
| ------------| --------- |
| 201 | App Package is pending copy |
| 202 | App Package copy is in progress |
| 203 | App Package copy is complete |
| 298 | Downloaded App Package is missing on Operator pod PVC |
| 299 | App Package is not copied after multiple retries |

#### Phase 3 - App package install

In this phase, the AppFramework installs the application on the splunkd binary running inside of the Splunk Enterprise pods. Below is a description of the statuses during this phase:

| Status Code | Description |
| ----------- | --------- |
| 301 | App Package is pending install |
| 302 | App Package install is in progress |
| 303 | App Package install is complete |
| 398 | Copied App Package is missing on Splunk Enterprise pod PVC |
| 399 | App Package is not copied after multiple retries |

Below is an example of a Standalone with a successful Application install.

Standalone CR spec:

```
apiVersion: enterprise.splunk.com/v4
kind: Standalone
metadata:
  name: test
  finalizers:
  - enterprise.splunk.com/delete-pvc
spec:
  replicas: 1
  appRepo:
    appsRepoPollIntervalSeconds: 100
    defaults:
      volumeName: volume_app_repo_us
      scope: local
    appSources:
    - name: dummy
      location: dummy/
      volumeName: volume_app_repo_us
    volumes:
    - name: volume_app_repo_us
      storageType: s3
      provider: aws
      path: test/cspl_1250_apps/
      endpoint: https://s3-us-west-2.amazonaws.com
      region: us-west-2
      secretRef: s3-secret
```

Standalone CR Status:
```
bash# kubectl get stdaln -o yaml | grep -i appSrcDeployStatus -A 33
      appSrcDeployStatus:
        dummy:
          appDeploymentInfo:
          - appName: a.tgz
            appPackageTopFolder: testapp
            auxPhaseInfo:
            - phase: install
              status: 303
            deployStatus: 3
            isUpdate: false
            objectHash: ab78...89
            phaseInfo:
              phase: install
              status: 303
            repoState: 1
          - appName: b.tgz
            appPackageTopFolder: newapp
            auxPhaseInfo:
            - phase: install
              status: 303
            deployStatus: 3
            isUpdate: false
            objectHash: 8745a....876
            phaseInfo:
              phase: install
              status: 303
            repoState: 1
      appsRepoStatusPollIntervalSeconds: 100
      appsStatusMaxConcurrentAppDownloads: 5
      bundlePushStatus: {}
      isDeploymentInProgress: false
      lastAppInfoCheckTime: 1719277376
      version: 1
```

### App Framework Bundle Push Status

The AppFramework uses a bundle push to install applications in clustered environments such as IndexerCluster as well as SeachHeadCluster. The status of the bundle push is stored in the field `cr.status.AppDeploymentContext.BundlePushStatus.BundlePushStage`. 

Below is a description of the bundle push statuses:

| Status Code | Description |
| ----------- | --------- |
| 0 | Bundle push is uninitialized, to be scheduled |
| 1 | Bundle Push is pending, waiting for all the apps to be copied to the Pod |
| 2 | Bundle Push is in progress |
| 3 | Bundle Push is complete |

Below is an example of a SHC with a successful Application install using Bundle push.

SHC CR spec:
```
apiVersion: enterprise.splunk.com/v4
kind: SearchHeadCluster
metadata:
  name: shc
  finalizers:
  - enterprise.splunk.com/delete-pvc
spec:
  replicas: 1
  appRepo:
    appsRepoPollIntervalSeconds: 100
    defaults:
      volumeName: volume_app_repo_us
      scope: cluster
    appSources:
    - name: dummy
      location: dummy/
      volumeName: volume_app_repo_us
    volumes:
    - name: volume_app_repo_us
      storageType: s3
      provider: aws
      path: test/cspl_1250_apps/
      endpoint: https://s3-us-west-2.amazonaws.com
      region: us-west-2
      secretRef: s3-secret
```

SHC CR status:
```
bash# kubectl get shc -o yaml | grep -i appSrcDeployStatus -A 33
      appSrcDeployStatus:
        dummy:
          appDeploymentInfo:
          - appName: a.tgz
            appPackageTopFolder: "testapp"
            deployStatus: 1
            isUpdate: false
            objectHash: 67ab7....876
            phaseInfo:
              phase: install
              status: 303
            repoState: 1
          - appName: b.tgz
            appPackageTopFolder: "newapp"
            deployStatus: 1
            isUpdate: false
            objectHash: 876abc....987
            phaseInfo:
              phase: install
              status: 303
            repoState: 1
      appsRepoStatusPollIntervalSeconds: 100
      appsStatusMaxConcurrentAppDownloads: 5
      bundlePushStatus:
        bundlePushStage: 3
      isDeploymentInProgress: false
      lastAppInfoCheckTime: 1719281420
      version: 1
    captain: splunk-shc-search-head-0
    captainReady: true
    deployerPhase: Ready
    initialized: true
    maintenanceMode: true
    members:
```