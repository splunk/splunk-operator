# App Framework Resource Guide

The Splunk Operator provides support for Splunk app and add-on deployment using the App Framework. The App Framework specification supports configuration management using the Splunk Enterprise cluster and standalone [custom resources](https://splunk.github.io/splunk-operator/CustomResources.html) (CR). 

### Prerequisites

Utilizing the App Framework requires:

* An Amazon S3 or S3-API-compliant remote object storage location. The App framework requires read-only access to the path used to host the apps. DO NOT give any other access to the operator to maintain the integrity of data in S3 bucket
* Create role and role-binding for splunk-operator service account, to provide read-only access for S3 credentials
* The remote object storage credentials provided as a kubernetes secret, or in an IAM role.
* Splunk apps and add-ons in a .tgz or .spl archive format.  
* Connections to the remote object storage endpoint need to be secured using a minimum version of TLS 1.2.
* A persistent storage volume and path for the Operator Pod. See [Add a persistent storage volume to the Operator pod](#add-a-persistent-storage-volume-to-the-operator-pod).

Splunk apps and add-ons deployed or installed outside of the App Framework are not managed, and are unsupported.

Note: For the App Framework to detect that an app or add-on had changed, the updated app must use the same archive file name as the previously deployed one. 

### How to use the App Framework on a Standalone CR

In this example, you'll deploy a Standalone CR with a remote storage volume, the location of the app archive, and set the installation location for the Splunk Enterprise Pod instance by using `scope`.

1. Confirm your S3-based remote storage volume path and URL.

2. Configure credentials to connect to remote store by:
   * Configuring an IAM role for the Operator and Splunk instance pods using a service account or annotations. 
   * Or, create a Kubernetes Secret Object with the static storage credentials.
     * Example: `kubectl create secret generic s3-secret --from-literal=s3_access_key=AKIAIOSFODNN7EXAMPLE --from-literal=s3_secret_key=wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY`

3. Create unique folders on the remote storage volume to use as App Source locations.
   * An App Source is a folder on the remote storage volume containing a select subset of Splunk apps and add-ons. In this example, the network and authentication Splunk Apps are split into different folders and named `networkApps` and `authApps`.

4. Copy your Splunk App or Add-on archive files to the App Source.
   * In this example, the Splunk Apps are located at `bucket-app-framework-us-west-2/Standalone-us/networkAppsLoc/` and `bucket-app-framework-us-west-2/Standalone-us/authAppsLoc/`, and are both accessible through the end point `https://s3-us-west-2.amazonaws.com`.

5. Update the standalone CR specification and append the volume, App Source configuration, and scope.
   * The scope determines where the apps and add-ons are placed into the Splunk Enterprise instance. For CRs where the Splunk Enterprise instance will run the apps locally, set the `scope: local ` The Standalone, Monitoring Console and License Manager CRs always use a local scope.

Example: Standalone.yaml

```yaml
apiVersion: enterprise.splunk.com/v3
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
        region: us-west-2
        secretRef: s3-secret
```

6. Apply the Custom Resource specification: `kubectl apply -f Standalone.yaml`

The App Framework detects the Splunk app or add-on archive files available in the App Source locations, and deploys them to the standalone instance path for local use. 

The App Framework maintains a checksum for each app or add-on archive file in the App Source location. The app name and checksum is recorded in the CR, and used to compare the deployed apps to the app archive files in the App Source location. The App Framework will scan for changes to the App Source folders using the polling interval, and deploy any updated apps to the instance. For the App Framework to detect that an app or add-on had changed, the updated app must use the same archive file name as the previously deployed one. 

By default, the App Framework polls the remote object storage location for new or changed apps at the `appsRepoPollIntervalSeconds` interval. To disable the interval check, and manage app updates manually, see the [Manual initiation of app management](#manual-initiation-of-app-management).

For more information, see the [Description of App Framework Specification fields](#description-of-app-framework-specification-fields).

### How to use the App Framework on Indexer Cluster

This example describes the installation of apps on an Indexer Cluster and Cluster Manager. This is achieved by deploying a ClusterManager CR with a remote storage volume, setting the location of the app archives, and the installation scope to support both local and cluster app path distribution.

1. Confirm your S3-based remote storage volume path and URL.

2. Configure credentials to connect to remote store by:
   * Configuring an IAM role for the Operator and Splunk instance pods using a service account or annotations. 
   * Or, create a Kubernetes Secret Object with the static storage credentials.
     * Example: `kubectl create secret generic s3-secret --from-literal=s3_access_key=AKIAIOSFODNN7EXAMPLE --from-literal=s3_secret_key=wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY`

3. Create unique folders on the remote storage volume to use as App Source locations.
   * An App Source is a folder on the remote storage volume containing a select subset of Splunk apps and add-ons. In this example, there are Splunk apps installed and run locally on the cluster manager, and select apps that will be distributed to all cluster peers by the cluster manager. 
   * The apps are split across 3 folders named `networkApps`, `clusterBase`, and `adminApps` . The apps placed into  `networkApps` and `clusterBase` are distributed to the cluster peers, but the apps in `adminApps` are for local use on the cluster manager instance only.

4. Copy your Splunk app or add-on archive files to the App Source.
   * In this example, the Splunk apps for the cluster peers are located at `bucket-app-framework-us-west-2/idxcAndCmApps/networkAppsLoc/`,  `bucket-app-framework-us-west-2/idxcAndCmApps/clusterBaseLoc/`, and the apps for the cluster manager are located at`bucket-app-framework-us-west-2/idxcAndCmApps/adminAppsLoc/`. They are all accessible through the end point `https://s3-us-west-2.amazonaws.com`.

5. Update the ClusterManager CR specification and append the volume, App Source configuration, and scope.
   * The scope determines where the apps and add-ons are placed into the Splunk Enterprise instance. For CRs where the Splunk Enterprise instance will deploy the apps to cluster peers, set the `scope:  cluster`. The ClusterManager and SearchHeadCluster CRs support both cluster and local scopes.
   * In this example, the cluster manager will install some apps locally, and deploy other apps to the cluster peers. The App Source folder `adminApps` contains Splunk apps that are installed and run on the cluster manager, and will use a local scope. The apps in the App Source folders `networkApps` and `clusterBase` will be deployed from the cluster manager to the peers, and will use a cluster scope.

Example: ClusterManager.yaml

```yaml
apiVersion: enterprise.splunk.com/v3
kind: ClusterManager
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
        region: us-west-2
        secretRef: s3-secret
```

6. Apply the Custom Resource specification: `kubectl apply -f ClusterManager.yaml`

The App Framework detects the Splunk app or add-on archive files available in the App Source locations, and deploys the apps from the `adminApps` folder to the cluster manager instance for local use. 

The apps in the `networkApps` and `clusterBase` folders are deployed to the cluster manager for use on the cluster peers. The cluster manager is responsible for deploying those apps to the cluster peers. 

Note: The Splunk cluster peer restarts are triggered by the contents of the Splunk apps deployed, and are not initiated by the App Framework.

The App Framework maintains a checksum for each app or add-on archive file in the App Source location. The app name and checksum is recorded in the CR, and used to compare the deployed apps to the app archive files in the App Source location. The App Framework will scan for changes to the App Source folders using the polling interval, and deploy any updated apps to the instance. For the App Framework to detect that an app or add-on had changed, the updated app must use the same archive file name as the previously deployed one. 

By default, the App Framework polls the remote object storage location for new or changed apps at the `appsRepoPollIntervalSeconds` interval. To disable the interval check, and manage app updates manually, see the [Manual initiation of app management](#manual-initiation-of-app-management).

For more information, see the [Description of App Framework Specification fields](#description-of-app-framework-specification-fields)

### How to use the App Framework on Search Head Cluster

This example describes the installation of apps on the Deployer and the Search Head Cluster. This is achieved by deploying a SearchHeadCluster CR with a storage volume, the location of the app archives, and set the installation scope to support both local and cluster app distribution.

1. Confirm your S3-based remote storage volume path and URL.

2. Configure credentials to connect to remote store by:
   * Configuring an IAM role for the Operator and Splunk instance pods using a service account or annotations. 
   * Or, create a Kubernetes Secret Object with the static storage credentials.
     * Example: `kubectl create secret generic s3-secret --from-literal=s3_access_key=AKIAIOSFODNN7EXAMPLE --from-literal=s3_secret_key=wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY`

3. Create unique folders on the remote storage volume to use as App Source locations.
   * An App Source is a folder on the remote storage volume containing a select subset of Splunk apps and add-ons. In this example, there are Splunk apps installed and run locally on the Deployer, and select apps that will be distributed to all cluster search heads by the Deployer. 
   * The apps are split across 3 folders named `searchApps`, `machineLearningApps`, `adminApps` and `ESapps`. The apps placed into  `searchApps`, `machineLearningApps` and `ESapps` are distributed to the search heads, but the apps in `adminApps` are for local use on the Deployer instance only.

4. Copy your Splunk app or add-on archive files to the App Source.
   * In this example, the Splunk apps for the search heads are located at `bucket-app-framework-us-west-2/shcLoc-us/searchAppsLoc/`,  `bucket-app-framework-us-west-2/shcLoc-us/machineLearningAppsLoc/`, and the apps for the Deployer are located at `bucket-app-framework-us-west-2/shcLoc-us/adminAppsLoc/`. Apps that need pre-configuration by the deployer before installing to the search heads (for example, Splunk Enterprise Security) are located at `bucket-app-framework-us-west-2/shcLoc-us/ESappsLoc/`. They are all accessible through the end point `https://s3-us-west-2.amazonaws.com`.

5. Update the SearchHeadCluster CR specification, and append the volume, App Source configuration, and scope.
   * The scope determines where the apps and add-ons are placed into the Splunk Enterprise instance. 
      * For CRs where the Splunk Enterprise instance will pre-configure an app before deploying it to the search heads (for example, Splunk Enterprise Security,) set the `scope: clusterWithPreConfig`. The ClusterManager and SearchHeadCluster CRs support the clusterWithPreConfig scope. 
      * For CRs where the Splunk Enterprise instance will deploy the apps without pre-configuration to search heads, set the `scope:  cluster`. The ClusterManager and SearchHeadCluster CRs support both cluster and local scopes. 
   * In this example, the Deployer will run some apps locally, and deploy other apps to the clustered search heads. The App Source folder `adminApps` contains Splunk apps that are installed and run on the Deployer, and will use a local scope. The apps in the App Source folders `searchApps` and `machineLearningApps` will be deployed from the Deployer to the search heads, and will use a cluster scope. For the apps in the App Source folder `ESappsLoc`, the Deployer will run a pre-configuration step before deploying those apps to the search heads.

Example: SearchHeadCluster.yaml

```yaml
apiVersion: enterprise.splunk.com/v3
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
        region: us-west-2
        secretRef: s3-secret
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
                          description: 'App Package Remote Store provider. For e.g.
                            aws, azure, minio, etc. Currently we are only supporting
                            aws. TODO: Support minio as well.'
                          type: string
                        region:
                          description: Region of the remote storage volume where apps
                            reside
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

`appRepo` is the start of the App Framework specification, and contains all the configurations required for App Framework to be successfully configured.

### volumes

`volumes` defines the remote storage configurations. The App Framework expects any apps to be installed in various Splunk deployments to be hosted in one or more remote storage volumes.

* `name` uniquely identifies the remote storage volume name within a CR. This is used by the Operator to identify the local volume.
* `storageType` describes the type of remote storage. Currently, `s3` is the only supported storage type.
* `provider` describes the remote storage provider. Currently, `aws` and `minio` are the supported providers.
* `endpoint` describes the URI/URL of the remote storage endpoint that hosts the apps.
* `secretRef` refers to the K8s secret object containing the static remote storage access key.  This parameter is not required if using IAM role based credentials.
* `path` describes the path (including the folder) of one or more app sources on the remote store.

### appSources

`appSources` defines the name and scope of the appSource, the remote storage volume, and its location.

* `name` uniquely identifies the App source configuration within a CR. This used locally by the Operator to identify the App source.
* `scope` defines the scope of the app to be installed. 
  * If the scope is `local`, the apps will be installed and run locally on the pod referred to by the CR. 
  * If the scope is `cluster`, the apps will be placed onto the configuration management node (Deployer, Cluster Manager) for deployment across the cluster referred to by the CR.
  * If the scope is `clusterWithPreConfig`, the apps will be placed onto the configuration management node (Deployer, Cluster Manager) and run through a pre-configuration step before being installing across the cluster referred to by the CR.
  * The cluster scope is only supported on CRs that manage cluster-wide app deployment.
  
    | CRD Type          | Scope support                          | App Framework support |
    | :---------------- | :------------------------------------- | :-------------------- |
    | ClusterManager    | cluster, clusterWithPreConfig,  local  | Yes                   |
    | SearchHeadCluster | cluster, clusterWithPreConfig, local   | Yes                   |
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
        image: "docker.io/splunk/splunk-operator:2.0.0"
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
          value: "docker.io/splunk/splunk:9.0.0"

      volumes:
      - name: app-staging
        persistentVolumeClaim:
          claimName: operator-volume-claim
```


## Manual initiation of app management
You can prevent the App Framework from automatically polling the remote storage for app changes. By configuring the `appsRepoPollIntervalSeconds` setting to `0`, the App Framework polling is disabled, and the configMap is updated with a new `status` field. The App Framework will perform an initial poll of the remote storage, even when the CR is initialized with polling disabled.

When you're ready to initiate an app check using the App Framework, manually update the `status` field in the configMap for that CR type to `on`. The 'status' field defaults to 'off'.

For example, you deployed one Standalone CR with app framework enabled. 

```
kubectl get standalone
NAME   PHASE   DESIRED   READY   AGE
s1     Ready   1         1       13h
```
As mentioned above, Splunk Operator will create the configMap (assuming `default` namespace) `splunk-default-manual-app-update` with an entry for Standalone CR as below:

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

To trigger manual checking of apps, update the configMap, and set the `status` field to `on` for the Standalone CR as below:

```kubectl patch cm/splunk-default-manual-app-update --type merge -p '{"data":{"Standalone":"status: on\nrefCount: 1"}}'```

The App Framework will perform its checks, update or install apps, and reset the `status` to `off` when it has completed its tasks.

To reinstate automatic polling, update the CR `appsRepoPollIntervalSeconds` setting to a value greater than 0.

NOTE: All CRs of the same type must have polling enabled, or disabled. For example, if `appsRepoPollIntervalSeconds` is set to '0' for one Standalone CR, all other Standalone CRs must also have polling disabled. Use the `kubectl` command to identify all CRs of the same type before updating the polling interval. You can experience unexpected polling behavior if there are CRs configured with a mix of polling enabled and disabled.

## App Framework Limitations

The App Framework does not preview, analyze, verify versions, or enable Splunk Apps and Add-ons. The administrator is responsible for previewing the app or add-on contents, verifying the app is enabled, and that the app is supported with the version of Splunk Enterprise deployed in the containers. For Splunk app packaging specifications see [Package apps for Splunk Cloud or Splunk Enterprise](https://dev.splunk.com/enterprise/docs/releaseapps/packageapps/) in the Splunk Enterprise Developer documentation. The app archive files must end with .spl or .tgz; all other files are ignored.

1. The App Framework has no support to remove an app or add-on once it’s been deployed. To disable an app, update the archive contents located in the App Source, and set the app.conf state to disabled.

2. The App Framework defines one worker per CR type. For example, if you have multiple clusters receiveing app updates, a delay while managing one cluster will delay the app updates to the other cluster. 

