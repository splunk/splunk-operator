# App Framework Resource Guide

The Splunk Operator provides support for Splunk App and Add-on deployment using the App Framework (Beta Version). The App Framework specification adds support for S3-compatible app repository, authentication, and supports specific Splunk Enterprise cluster and standalone [custom resources](https://splunk.github.io/splunk-operator/CustomResources.html) (CR).

### Prerequisites

Utilizing the App Framework requires:

* An Amazon S3 or S3-API-compliant remote object storage location. App framework requires read-only access to the path containing the apps.
* The remote object storage credentials.
* Splunk Apps and Add-ons in a .tgz or .spl archive format.
* Connections to the remote object storage endpoint need to be secure using a minimum version of TLS 1.2.


### How to use the App Framework on a Standalone CR

In this example, you'll deploy a Standalone CR with a remote storage volume, the location of the app archives, and set the installation location for the Splunk Enterprise Pod instance by using `scope`.

1. Confirm your S3-based remote storage volume path and URL.
2. Create a Kubernetes Secret Object with the storage credentials. 
   * Example: `kubectl create secret generic s3-secret --from-literal=s3_access_key=AKIAIOSFODNN7EXAMPLE --from-literal=s3_secret_key=wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY`
3. Create folders on the remote storage volume to use as App Source locations.
   * An App Source is a folder on the remote storage volume containing a subset of Splunk Apps and Add-ons. In this example, we split the network and authentication Splunk Apps into different folders and named them `networkApps` and `authApps`.

4. Copy your Splunk App or Add-on archive files to the App Source.
   * In this example, the Splunk Apps are located at `bucket-app-framework-us-west-2/Standalone-us/networkAppsLoc/` and `bucket-app-framework-us-west-2/Standalone-us/authAppsLoc/`, and are both accessible through the end point `https://s3-us-west-2.amazonaws.com`.

5. Update the standalone CR specification and append the volume, App Source configuration, and scope.
   * The scope determines where the apps and add-ons are placed into the Splunk Enterprise instance. For CRs where the Splunk Enterprise instance will run the apps locally, set the `scope: local ` The Standalone and License Master CRs always use a local scope.

Example: Standalone.yaml

```yaml
apiVersion: enterprise.splunk.com/v2
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

6. Apply the Custom Resource specification: `kubectl apply -f Standalone.yaml`

The App Framework detects the Splunk App archive files available in the App Source locations, and deploys the apps to the standalone instance for local use. The App Framework will also scan for changes to the App Source folders based on the polling interval, and deploy updated archives to the instance. A Pod reset is triggered to install the new or modified apps.

Note: A similar approach can be used for installing apps on License Master using it's own CR.

For more information, see the [Description of App Framework Specification fields](#description-of-app-framework-specification-fields).


### How to use the App Framework on Indexer Cluster

This example describes the installation of apps on Indexer Cluster as well as Cluster Master. This is achieved by deploying a ClusterMaster CR with a remote storage volume, the location of the app archives, and set the installation scope to support both local and cluster app distribution.

1. Confirm your S3-based remote storage volume path and URL.
2. Create a Kubernetes Secret Object with the storage credentials. 
   * Example: `kubectl create secret generic s3-secret --from-literal=s3_access_key=AKIAIOSFODNN7EXAMPLE --from-literal=s3_secret_key=wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY`
3. Create folders on the remote storage volume to use as App Source locations.
   * An App Source is a folder on the remote storage volume containing a subset of Splunk Apps and Add-ons. In this example, we have Splunk apps that are installed and run locally on the cluster master, and apps that will be distributed to all cluster peers by the cluster master. 
   * The apps are split across 3 folders named `networkApps`, `clusterBase`, and `adminApps` . The apps placed into  `networkApps` and `clusterBase` are distributed to the cluster peers, but the apps in `adminApps` are for local use on the cluster master instance only.

4. Copy your Splunk App or Add-on archive files to the App Source.
   * In this example, the Splunk Apps for the cluster peers are located at `bucket-app-framework-us-west-2/idxcAndCmApps/networkAppsLoc/`,  `bucket-app-framework-us-west-2/idxcAndCmApps/clusterBaseLoc/`, and the apps for the cluster master are located at`bucket-app-framework-us-west-2/idxcAndCmApps/adminAppsLoc/`. They are all accessible through the end point `https://s3-us-west-2.amazonaws.com`.

5. Update the ClusterMaster CR specification and append the volume, App Source configuration, and scope.
   * The scope determines where the apps and add-ons are placed into the Splunk Enterprise instance. For CR's where the Splunk Enterprise instance will deploy the apps to cluster peers, set the `scope:  cluster`. The ClusterMaster and SearchHeadCluster CR's support both cluster and local scopes.
   * In this example, the cluster master will run some apps locally, and deploy other apps to the cluster peers. The App Source folder `adminApps` are Splunk Apps that are installed on the cluster master, and will use a local scope. The apps in the App Source folders `networkApps` and `clusterBase` will be deployed from the cluster master to the peers, and will use a cluster scope.

Example: ClusterMaster.yaml

```yaml
apiVersion: enterprise.splunk.com/v2
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
        path: bucket-app-framework-us-west-2/idxcAndCmApps/
        endpoint: https://s3-us-west-2.amazonaws.com
        secretRef: s3-secret
```

6. Apply the Custom Resource specification: `kubectl apply -f ClusterMaster.yaml`

The App Framework detects the Splunk App archive files available in the App Source locations, and deploys the apps from the `adminApps`  folder to the cluster master instance for local use. A Pod reset is triggered on the cluster master to install any new or modified apps. The App Framework will also scan for changes to the App Source folders based on the polling interval, and deploy updated archives to the instance.

The apps in the `networkApps` and `clusterBase` folders are deployed to the cluster master for use on the cluster. The cluster master is responsible for deploying those apps to the cluster peers. The Splunk cluster peer restarts are triggered by the contents of the Splunk apps deployed, and are not initiated by the App Framework.

For more information, see the [Description of App Framework Specification fields](#description-of-app-framework-specification-fields)

### How to use the App Framework on Search Head Cluster

This example describes the installation of apps on Search Head Cluster as well as Deployer. This is achieved by deploying a SearchHeadCluster CR with a storage volume, the location of the app archives, and set the installation scope to support both local and cluster app distribution.

1. Confirm your S3-based remote storage volume path and URL.
2. Create a Kubernetes Secret Object with the storage credentials. 
   * Example: `kubectl create secret generic s3-secret --from-literal=s3_access_key=AKIAIOSFODNN7EXAMPLE --from-literal=s3_secret_key=wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY`
3. Create folders on the remote storage volume to use as App Source locations.
   * An App Source is a folder on the remote storage volume containing a subset of Splunk Apps and Add-ons. In this example, we have Splunk apps that are installed and run locally on the Deployer, and apps that will be distributed to all cluster search heads by the Deployer. 
   * The apps are split across 3 folders named `searchApps`, `machineLearningApps`, `adminApps` and `ESapps`. The apps placed into  `searchApps`, `machineLearningApps` and `ESapps` are distributed to the search heads, but the apps in `adminApps` are for local use on the Deployer instance only.

4. Copy your Splunk App or Add-on archive files to the App Source.
   * In this example, the Splunk Apps for the search heads are located at `bucket-app-framework-us-west-2/shcLoc-us/searchAppsLoc/`,  `bucket-app-framework-us-west-2/shcLoc-us/machineLearningAppsLoc/`, and the apps for the Deployer are located at `bucket-app-framework-us-west-2/shcLoc-us/adminAppsLoc/`. Apps that need pre-configuration by the deployer(Ex. Enterprise Security App) before installing to the search heads are located at `bucket-app-framework-us-west-2/shcLoc-us/ESappsLoc/`. They are all accessible through the end point `https://s3-us-west-2.amazonaws.com`.

5. Update the SearchHeadCluster CR specification and append the volume, App Source configuration, and scope.
   * The scope determines where the apps and add-ons are placed into the Splunk Enterprise instance. 
      * For CR's where the Splunk Enterprise instance will pre-configure before deploying to the search heads,  set the `scope: clusterWithPreConfig`.
      * For CR's where the Splunk Enterprise instance will deploy the apps(without pre-configuring) to search heads set the `scope:  cluster`. The ClusterMaster and SearchHeadCluster CR's support both cluster and local scopes. 
   * In this example, the Deployer will run some apps locally, and deploy other apps to the search heads. The App Source folder `adminApps` are Splunk Apps that are installed on the Deployer, and will use a local scope. The apps in the App Source folders `searchApps` and `machineLearningApps` will be deployed from the Deployer to the search heads, and will use a cluster scope. For the apps in the App Source folder ESapps, deployer pre-configures them, and then deploys them to the search heads.

Example: SearchHeadCluster.yaml

```yaml
apiVersion: enterprise.splunk.com/v2
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
      - name: ESapps
        location: ESappsLoc/
        scope: clusterWithPreConfig
    volumes:
      - name: volume_app_repo_us
        storageType: s3
        provider: aws
        path: bucket-app-framework-us-west-2/shcLoc-us/
        endpoint: https://s3-us-west-2.amazonaws.com
        secretRef: s3-secret
```

6. Apply the Custom Resource specification: `kubectl apply -f SearchHeadCluster.yaml`

The App Framework detects the Splunk App archive files available in the App Source locations, and deploys the apps from the `adminApps`  folder to the deployer instance for local use. A Pod reset is triggered on the deployer to install any new or modified apps. The App Framework will also scan for changes to the App Source folders based on the polling interval, and deploy updated archives to the instance.

The apps in the `searchApps` and `machineLearningApps` folders are deployed to the deployer for use on the search head cluster. The deployer is responsible for deploying those apps to the search heads. The Splunk Search Head restarts are triggered by the contents of the Splunk apps deployed, and are not initiated by the App Framework.

For more information, see the [Description of App Framework Specification fields](#description-of-app-framework-specification-fields).


## Description of App Framework Specification fields
App Framework configuration is supported on the following Custom Resources: Standalone, ClusterMaster, SearchHeadCluster, and LicenseMaster. Configuring the App framework involves the following steps:

* Remote Source of Apps: Define the remote location including the bucket(s) and path for each bucket
* Destination of Apps: Define where the Apps need to be installed (in other words, which Custom resources need to be configured)
* Scope of Apps: Define if the Apps need to be installed locally (such as Standalone) or cluster-wide (such as Indexer cluster, Search Head Cluster)

Here is a typical App framework configuration in a Custom resource definition:

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

* `name` uniquely identifies the remote storage volume name within a CR. This is to locally used by the Operator to identify the volume
* `storageType` describes the type of remote storage. Currently `s3` is the only supported type
* `provider` describes the remote storage provider. Currently `aws` & `minio` are the supported providers 
* `endpoint` helps configure the URI/URL of the remote storage endpoint that hosts the apps
* `secretRef` refers to the K8s secret object containing the remote storage access key
* `path` describes the path (including the bucket) of one or more app sources on the remote store 

### appSources

`appSources` helps configure the name & scope of the appSource, as well as remote storage volume & location

* `name` uniquely identifies the App source configuration within a CR. This is to locally used by the Operator to identify the App source
* `scope` defines the scope of the App to be installed. 
  * If the scope is `local` the apps will be installed locally on the pod referred to by the CR
  * If the scope is `cluster` the apps will be installed across the cluster referred to by the CR
  * If the scope is `clusterWithPreConfig` the apps will be pre-configured before installing across the cluster referred to by the CR
  * The cluster scope is only supported on CR's that manage cluster-wide app deployment
  
    | CRD Type          | Scope support                          | App Framework support |
    | :---------------- | :------------------------------------- | :-------------------- |
    | ClusterManager    | cluster, clusterWithPreConfig,  local  | Yes                   |
    | SearchHeadCluster | cluster, clusterWithPreConfig, local   | Yes                   |
    | Standalone        | local                                  | Yes                   |
    | LicenceMaster     | local                                  | Yes                   |
    | IndexerCluster    | N/A                                    | No                    |

* `volume` refers to the remote storage volume name configured under the `volumes` stanza (see previous section)
* `location` helps configure the specific appSource present under the `path` within the `volume`, containing the apps to be installed  

### appsRepoPollIntervalSeconds

The App Framework uses the polling interval `appsRepoPollIntervalSeconds` to check for additional apps, or modified apps on the remote object storage.  If app framework is enabled, the Splunk Operator creates a namespace scoped configMap named **splunk-\<namespace\>-manual-app-update**, which is used to manually trigger the app updates. When `appsRepoPollIntervalSeconds` is set to `0` for a CR, the App Framework will not perform a check until the configMap `status` field is updated manually. See [Manual initiation of app management](#manual_initiation_of_app_management).


## Manual initiation of app management
You can prevent the App Framework from automatically polling the remote storage for changes. By setting the CR setting `appsRepoPollIntervalSeconds` to `0`, the App Framework polling is disabled, and the configMap is updated with a new `status` field. The App Framework always performs an initial poll of the remote storage, even when the CR is initialized with polling disabled.

The 'status' field defaults to 'off'. When you're ready to initiate an app check using the App Framework, manually update the `status` field in the configMap for that CR type to `on`. 
For example, you deployed one Standalone CR with app framework enabled. 

```
kubectl get standalone
NAME   PHASE   DESIRED   READY   AGE
s1     Ready   1         1       13h
```
As mentioned above, Splunk Operator will create the configMap(assuming `default` namespace) `splunk-default-manual-app-update` with an entry for Standalone CR as below -

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
  - apiVersion: enterprise.splunk.com/v2
    controller: false
    kind: Standalone
    name: s1
    uid: ddb9528f-2e25-49be-acd4-4fadde489849
  resourceVersion: "75406013"
  selfLink: /api/v1/namespaces/default/configmaps/splunk-manual-app-update
  uid: 413c6053-af4f-4cb3-97e0-6dbe7cd17721
  ```

To trigger manual checking of app/s, update the configMap and set the `status` field to `on` for the Standalone CR as below:

```kubectl patch cm/splunk-default-manual-app-update --type merge -p '{"data":{"Standalone":"status: on\nrefCount: 1"}}'```

The App Framework will perform its checks and updates, and reset the `status` to `off` when it has completed its tasks.

To re-enable the polling, update the CR `appsRepoPollIntervalSeconds` setting to a value greater than 0.

NOTE: All CR's of the same type must have polling enabled, or disabled. For example, if `appsRepoPollIntervalSeconds` is set to '0' for one Standalone CR, all other Standalone CRs must also have polling disabled. Use the `kubectl` command to identify all CR's of the same type before updating the polling interval. We can have unexpected behavior of polling if we have CRs with a mix of polling enabled and disabled.

## Impact of livenessInitialDelaySeconds and readinessInitialDelaySeconds

* Splunk Operator CRDs support the configuration of [initialDelaySeconds](https://kubernetes.io/docs/tasks/configure-pod-container/configure-liveness-readiness-startup-probes/) for both Liveliness (livenessInitialDelaySeconds) and Readiness (readinessInitialDelaySeconds) probes
* When App Framework is NOT configured, default values are 300 seconds for livenessInitialDelaySeconds and 10 seconds for readinessInitialDelaySeconds (for all CRs)
* When App Framework is configured, default values are 1800 seconds for livenessInitialDelaySeconds and 10 seconds for readinessInitialDelaySeconds (only for Deployer, Cluster Master, Standalone and License Master CRs). The higher value of livenessInitialDelaySeconds is to ensure sufficient time is allocated for installing most apps. This configuration can further be managed depending on the number & size of Apps to be installed

## App Framework Limitations

The App Framework does not review, preview, analyze, or enable Splunk Apps and Add-ons. The administrator is responsible for previewing the app or add-on contents, verifying the app is enabled, and that the app is supported with the version of Splunk Enterprise used in the containers. For App packaging specifications see [Package apps for Splunk Cloud or Splunk Enterprise](https://dev.splunk.com/enterprise/docs/releaseapps/packageapps/) in the Splunk Enterprise Developer documentation. The app archive files must end with .spl or .tgz; all other files are ignored.

1. The App Framework has no support to remove an app or add-on once it’s been deployed. To disable an app, update the archive contents located in the App Source, and set the app.conf state to disabled.

2. The App Framework tracks the app installation state per CR. Whenever you scale up a Standalone CR, all the existing pods will recycle and all the apps in app sources will be re-installed. This is done so that the new replica(s) can install all the apps and not just the apps that were changed recently.

3. When a change in the App Repo is detected by the App Framework, a pod reset is initiated to install the new or modified applications. For the ClusterMaster and SearchHeadCluster CR’s, a pod reset is applied to the cluster master and deployer instances only. A cluster peer restart might be triggered by the contents of the Splunk apps deployed, but are not initiated by the App Framework.