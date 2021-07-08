# App Framework Resource Guide

The Splunk Operator provides support for Splunk App and Add-on deployment using the App Framework (Beta Version). The App Framework specification adds support for S3-compatible app repository, authentication, and support for specific cluster and standalone [custom resources](https://splunk.github.io/splunk-operator/CustomResources.html) (CR).

### Prerequisites

Utilizing the App Framework requires:

* An Amazon S3 or S3-API-compliant remote object storage location. App framework requires read-only access to the path containing the apps.
* The remote object storage credentials.
* Splunk Apps and Add-ons in a .tgz or .spl archive format.


### How to use the App Framework on a Standalone CR

In this example, you'll deploy a Standalone CR with a storage volume, the location of the app archives, and set the installation location for the Splunk Enterprise Pod instance by using scope.

1. Confirm your S3-based storage volume path and URL.
2. Create a Kubernetes Secret Object with the storage credentials. 
   * Example: `kubectl create secret generic s3-secret --from-literal=s3_access_key=AKIAIOSFODNN7EXAMPLE --from-literal=s3_secret_key=wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY`
3. Create folders on the volume to use as App Source locations.
   * An App Source is a folder on the volume containing a subset of Splunk Apps and Add-ons. In this example, we split the network and authentication Splunk Apps into different folders and named them `networkApps` and `authApps`.

4. Copy your Splunk App or Add-on archive files to the App Source.
   * In this example, the Splunk Apps are located at `bucket-app-framework-us-west-2/Standalone-us/networkAppsLoc/` and `bucket-app-framework-us-west-2/Standalone-us/authAppsLoc/`, and are both accessible through the end point `https://s3-us-west-2.amazonaws.com`.

5. Update the standalone CR specification and append the volume, App Source configuration, and scope.
   * The scope determines where the apps and add-ons are placed into the Splunk Enterprise instance. For CR's where the Splunk Enterprise instance will run the apps locally, set the `scope: local ` The Standalone and License Master CR's always use a local scope.

Example: Standalone.yaml

```apiVersion: enterprise.splunk.com/v1
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
        path: bucket-app-framework-us-west-2/Standalone-us/
        endpoint: https://s3-us-west-2.amazonaws.com
        secretRef: s3-secret
```

6. Apply the Customer Resource specification: `kubectl -f apply Standalone.yaml`

The App Framework detects the Splunk App archive files available in the App Source locations, and deploys the apps to the standalone instance for local use. The App Framework will also scan for changes to the App Source folders based on the polling interval, and deploy updated archives to the instance. A Pod reset is triggered to install the new or modified apps.

Note: A similar approach can be used for installing apps on License Master using it's own CR.

For more information, see the [Description of App Framework Specification fields](#description-of-app-framework-specification-fields).


### How to use the App Framework on Indexer Cluster

This example describes the installation of apps on Indexer Cluster as well as Cluster Master. This is achieved by deploying a ClusterMaster CR with a storage volume, the location of the app archives, and set the installation scope to support both local and cluster app distribution.

1. Confirm your S3-based storage volume path and URL.
2. Create a Kubernetes Secret Object with the storage credentials. 
   * Example: `kubectl create secret generic s3-secret --from-literal=s3_access_key=AKIAIOSFODNN7EXAMPLE --from-literal=s3_secret_key=wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY`
3. Create folders on the volume to use as App Source locations.
   * An App Source is a folder on the volume containing a subset of Splunk Apps and Add-ons. In this example, we have Splunk apps that are installed and run locally on the cluster master, and apps that will be distributed to all cluster peers by the cluster master. 
   * The apps are split across 3 folders named `networkApps`, `clusterBase`, and `adminApps` . The apps placed into  `networkApps` and `clusterBase` are distributed to the cluster peers, but the apps in `adminApps` are for local use on the cluster master instance only.

4. Copy your Splunk App or Add-on archive files to the App Source.
   * In this example, the Splunk Apps for the cluster peers are located at `bucket-app-framework-us-west-2/Standalone-us/networkAppsLoc/`,  `bucket-app-framework-us-west-2/Standalone-us/clusterBaseLoc/`, and the apps for the cluster master are located at`bucket-app-framework-us-west-2/Standalone-us/adminAppsLoc/`. They are all accessible through the end point `https://s3-us-west-2.amazonaws.com`.

5. Update the ClusterMaster CR specification and append the volume, App Source configuration, and scope.
   * The scope determines where the apps and add-ons are placed into the Splunk Enterprise instance. For CR's where the Splunk Enterprise instance will deploy the apps to cluster peers, set the `scope:  cluster`. The ClusterMaster and SearchHeadCluster CR's support both cluster and local scopes.
   * In this example, the cluster master will run some apps locally, and deploy other apps to the cluster peers. The App Source folder `adminApps` are Splunk Apps that are installed on the cluster master, and will use a local scope. The apps in the App Source folders `networkApps` and `clusterBase` will be deployed from the cluster master to the peers, and will use a cluster scope.

Example: ClusterMaster.yaml

```apiVersion: enterprise.splunk.com/v1
kind: ClusterMaster
metadata:
  name: cm
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
        path: bucket-app-framework-us-west-2/Standalone-us/
        endpoint: https://s3-us-west-2.amazonaws.com
        secretRef: s3-secret
```

6. Apply the Customer Resource specification: `kubectl -f apply ClusterMaster.yaml`

The App Framework detects the Splunk App archive files available in the App Source locations, and deploys the apps from the `adminApps`  folder to the cluster master instance for local use. A Pod reset is triggered on the cluster master to install any new or modified apps. The App Framework will also scan for changes to the App Source folders based on the polling interval, and deploy updated archives to the instance.

The apps in the `networkApps` and `clusterBase` folders are deployed to the cluster master for use on the cluster. The cluster master is responsible for deploying those apps to the cluster peers. The Splunk cluster peer restarts are triggered by the contents of the Splunk apps deployed, and are not initiated by the App Framework.

For more information, see the [Description of App Framework Specification fields](#description-of-app-framework-specification-fields)

### How to use the App Framework on Search Head Cluster

This example describes the installation of apps on Search Head Cluster as well as Deployer. This is achieved by deploying a SearchHeadCluster CR with a storage volume, the location of the app archives, and set the installation scope to support both local and cluster app distribution.

1. Confirm your S3-based storage volume path and URL.
2. Create a Kubernetes Secret Object with the storage credentials. 
   * Example: `kubectl create secret generic s3-secret --from-literal=s3_access_key=AKIAIOSFODNN7EXAMPLE --from-literal=s3_secret_key=wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY`
3. Create folders on the volume to use as App Source locations.
   * An App Source is a folder on the volume containing a subset of Splunk Apps and Add-ons. In this example, we have Splunk apps that are installed and run locally on the Deployer, and apps that will be distributed to all cluster search heads by the Deployer. 
   * The apps are split across 3 folders named `searchApps`, `machineLearningApps`, and `adminApps`. The apps placed into  `searchApps` and `machineLearningApps` are distributed to the search heads, but the apps in `adminApps` are for local use on the Deployer instance only.

4. Copy your Splunk App or Add-on archive files to the App Source.
   * In this example, the Splunk Apps for the search heads are located at `bucket-app-framework-us-west-2/shcLoc-us/searchAppsLoc/`,  `bucket-app-framework-us-west-2/shcLoc-us/machineLearningAppsLoc/`, and the apps for the Deployer are located at`bucket-app-framework-us-west-2/shcLoc-us/adminAppsLoc/`. They are all accessible through the end point `https://s3-us-west-2.amazonaws.com`.

5. Update the SearchHeadCluster CR specification and append the volume, App Source configuration, and scope.
   * The scope determines where the apps and add-ons are placed into the Splunk Enterprise instance. For CR's where the Splunk Enterprise instance will deploy the apps to search heads, set the `scope:  cluster`. The ClusterMaster and SearchHeadCluster CR's support both cluster and local scopes.
   * In this example, the Deployer will run some apps locally, and deploy other apps to the search heads. The App Source folder `adminApps` are Splunk Apps that are installed on the Deployer, and will use a local scope. The apps in the App Source folders `searchApps` and `machineLearningApps` will be deployed from the Deployer to the search heads, and will use a cluster scope.

Example: SearchHeadCluster.yaml

```apiVersion: enterprise.splunk.com/v1
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
        path: bucket-app-framework-us-west-2/shcLoc-us/
        endpoint: https://s3-us-west-2.amazonaws.com
        secretRef: s3-secret
```

6. Apply the Customer Resource specification: `kubectl -f apply SearchHeadCluster.yaml`

The App Framework detects the Splunk App archive files available in the App Source locations, and deploys the apps from the `adminApps`  folder to the deployer instance for local use. A Pod reset is triggered on the deployer to install any new or modified apps. The App Framework will also scan for changes to the App Source folders based on the polling interval, and deploy updated archives to the instance.

The apps in the `searchApps` and `machineLearningApps` folders are deployed to the deployer for use on the search head cluster. The deployer is responsible for deploying those apps to the search heads. The Splunk Search Head restarts are triggered by the contents of the Splunk apps deployed, and are not initiated by the App Framework.

For more information, see the [Description of App Framework Specification fields](#description-of-app-framework-specification-fields).


## Description of App Framework Specification fields
App Framework configuration is supported on the following Custom Resources: Standalone, ClusterMaster, SearchHeadCluster, and LicenseMaster. Configuring the App framework involves the following steps:

* Remote Source of Apps: Define the remote location including the bucket(s) and path for each bucket
* Destination of Apps: Define where the Apps need to be installed (in other words, which Custom resources need to be configured)
* Scope of Apps: Define if the Apps need to be installed locally (such as Standalone) or cluster-wide (such as Indexer cluster

Here is a typical App framework configuration in a Custom resource definition:

```
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
                          description: 'Scope of the App deployment: cluster, local.
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
                          description: 'App Package Remote Store provider. For e.g.
                            aws, azure, minio, etc. Currently we are only supporting
                            aws. TODO: Support minio as well.'
                          type: string
                        secretRef:
                          description: Secret object name
                          type: string
                        storageType:
                          description: Remote Storage type.
                          type: string
                      type: object
                    type: array
                type: object
```

### appRepo

`appRepo` is the start of the App Framework specification, and contains all the configurations required for App framework to be successfully configured

### volumes

`volumes` helps configure the remote storage volumes. App framework expects apps that are to be installed in various Splunk deployments to be hosted in one or more remote storage volumes

* `name` uniquely identifies the volume name within a CR. This is to locally used by the Operator to identify the volume
* `storageType` describes the type of remote storage. Currently `s3` i:w
  s the only supported type
* `provider` describes the remote storage provider. Currently `aws` & `minio` are the supported providers 
* `endpoint` helps configure the URI/URL of the remote storage endpoint that hosts the apps
* `secretRef` refers to the K8s secret object containing the remote storage access key
* `path` describes the path (including the bucket) of one or more app sources on the remote store 

### appSources

`appSources` helps configure the name & scope of the appSource, as well as remote store's volume & location

* `name` uniquely identifies the App source configuration within a CR. This is to locally used by the Operator to identify the App source
* `scope` defines the scope of the App to be installed. 
  * If the scope is `local` the apps will be installed locally on the pod referred to by the CR
  * If the scope is `cluster` the apps will be installed across the cluster referred to by the CR
  * The cluster scope is only supported on CR's that manage cluster-wide app deployment
  
    | CRD Type          | Scope support  | App Framework support |
    | :---------------- | :------------- | :-------------------- |
    | ClusterManager    | cluster, local | Yes                   |
    | SearchHeadCluster | cluster, local | Yes                   |
    | Standalone        | local          | Yes                   |
    | LicenceMaster     | local          | Yes                   |
    | IndexerCluster    | N/A            | No                    |

* `volume` refers to the remote storage volume name configured under the `volumes` stanza (see previous section)
* `location` helps configure the specific appSource present under the `path` within the `volume`, containing the apps to be installed  

### appsRepoPollIntervalSeconds

`appsRepoPollIntervalSeconds` helps configure the polling interval(in seconds) to detect addition or modification of apps on the Remote Storage

## Impact of livenessInitialDelaySeconds and readinessInitialDelaySeconds

* Splunk Operator CRDs support the configuration of initialDelaySeconds(insert link to https://kubernetes.io/docs/tasks/configure-pod-container/configure-liveness-readiness-startup-probes/) for both Liveliness (livenessInitialDelaySeconds) and Readiness (readinessInitialDelaySeconds) probes 
* Default values are 300 seconds for livenessInitialDelaySeconds and 10 seconds for readinessInitialDelaySeconds
* When Appframework is configured as part of a CR, depending on the number of Apps being configured, Operator can also override the default or configured values for both probes with internally calculated higher values. This is to ensure that optimal values are being used to allow for successful installation or update of Apps especially in large scale deployments. 

## App Framework Limitations

The App Framework does not review, preview, analyze, or enable Splunk Apps and Add-ons. The administrator is responsible for previewing the app or add-on contents, verifying the app is enabled, and that the app is supported with the version of Splunk Enterprise used in the containers. For App packaging specifications see [Package apps for Splunk Cloud or Splunk Enterprise](https://dev.splunk.com/enterprise/docs/releaseapps/packageapps/) in the Splunk Enterprise Developer documentation. The app archive files must end with .spl or .tgz; all other files are ignored.

1. The App Framework has no support to remove an app or add-on once it’s been deployed. To disable an app, update the archive contents located in the App Source, and set the app.conf state to disabled.

2. The App Framework tracks the app installation state per CR. If you configure a Standalone CR to use more than one replicas, a new replica Pod will not receive any apps that were previously deployed.

3. When a change in the App Repo is detected by the App Framework, a pod reset is initiated to install the new or modified applications. For the ClusterMaster and SearchHeadCluster CR’s, a pod reset is applied to the cluster master and deployer instances only. A cluster peer restart might be triggered by the contents of the Splunk apps deployed, but are not initiated by the App Framework.