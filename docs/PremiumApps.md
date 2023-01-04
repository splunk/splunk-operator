# Premium App Installation Guide

The Splunk Operator automates the installation of Enterprise Security with support for other premium apps coming in the future. This page documents the prerequisites, installation steps, and limitations of deploying premium apps using the Splunk Operator.

## Enterprise Security

### Prerequisites

Installing Enterprise Security in a Kubernetes cluster with the Splunk Operator requires the following:

* Ability to utilize the Splunk Operator [app framework](https://splunk.github.io/splunk-operator/AppFramework.html) method of installation.
* Access to the [Splunk Enterprise Security](https://splunkbase.splunk.com/app/263/) app package.
* Splunk Enterprise Security version compatible to the Splunk Enterprise version used in your Splunk Operator installation. For more information regarding Splunk Enterprise and Enterprise Security compatibility, see the [version compatibility matrix](https://docs.splunk.com/Documentation/VersionCompatibility/current/Matrix/CompatMatrix). Note that Splunk Operator requires Splunk Enterprise 8.2.2 or later.
* Pod resource specs that meet the [Enterprise Security hardware requirements](https://docs.splunk.com/Documentation/ES/latest/Install/DeploymentPlanning#Hardware_requirements).
* In the following sections, aws s3 remote bucket is used for placing the splunk apps, but as given in the [app framework doc](https://splunk.github.io/splunk-operator/AppFramework.html), you can use Azure blob remote buckets also.

### Supported Deployment Types

The architectures that support automated deployment of Enterprise Security by using the Splunk Operator include:

* Standalone Splunk Instance
* Standalone Search Head with Indexer Cluster
* Search Head Cluster with Indexer Cluster

Notably, if deploying a distributed search environment, the use of indexer clustering is required to ensure that the necessary Enterprise Security specific configuration is pushed to the indexers via the Cluster Manager.

### What is and what is not automated by the Splunk Operator

The Splunk Operator installs the necessary Enterprise Security components depending on the architecture specified by the applied CRDs.

#### Standalone Splunk Instance/ Standalone Search Heads
For Standalone Splunk instances and standalone search heads the Operator will install Splunk Enterprise Security and all associated domain add-ons (DAs), and supporting add-ons (SAs).

#### Search Head Cluster
When installing Enterprise Security in a Search Head Cluster, the Operator will perform the following tasks: 
1) Install the splunk enterprise app in Deployer's etc/apps directory.
2) Run the ES post install command `essinstall` that stages the Splunk Enterprise Security and all associated domain add-ons (DAs) and supporting add-ons (SAs) to the etc/shcapps.
3) Push the Search Head Cluster bundle from the deployer to all the SHs.

#### Indexer Cluster
When installing ES in an indexer clustering environment through the Splunk Operator it is necessary to deploy the supplemental [Splunk_TA_ForIndexers](https://docs.splunk.com/Documentation/ES/latest/Install/InstallTechnologyAdd-ons#Create_the_Splunk_TA_ForIndexers_and_manage_deployment_manually) app from the ES package to the indexer cluster members. This can be achieved using the AppFramework app deployment steps using appSources scope of "cluster".

### How to Install Enterprise Security using the Splunk Operator

#### Considerations for using the Splunk Operator

When crafting your Custom Resource(CR) to create a Splunk Enterprise Deployment it is necessary to take the following configurations into account.

##### [appSources](https://splunk.github.io/splunk-operator/AppFramework.html#appsources) scope
   
   - When deploying ES to a Standalone or to a Search Head Cluster, it must be configured with an appSources scope of "premiumApps".
   - When deploying the Splunk_TA_ForIndexers app to an Indexer Cluster, it must be configured with an appSources scope of "cluster".

#####  SSL Enablement

When you install ES versions 6.3.0 or higher, you must supply a value for the parameter ssl_enablement that is required by the ES post installation command `essinstall`. By default, if you don't set any value for ssl_enablement, the value of `strict` is used which requires Splunk to have SSL enabled in web.conf (refer to setting `enableSplunkWebSSL`). The below table can be used for reference of available values of ssl enablement. 

| SSL mode  | Description | 
| --------- | ----------- | 
|strict     | `Default mode`. Ensure that SSL is enabled in the web.conf configuration file to use this mode. Otherwise, the installer exists with an error. Note that for the SHC, the ES post install command `essinstall` is run on the deployer, and this command looks into web.conf files under etc/shcapps to validate that enableSplunkWebSSL is set to true or not. This is done with the assumption that you have already pushed a web.conf through etc/shcapps to all the SHC members.| 
| auto     | Enables SSL in the etc/system/local/web.conf configuration file. This mode is not supported by SHC |
| ignore     | Ignores whether SSL is enabled or disabled. This option may be handy if you don't want operator and essinstall to check SSL is enabled for Splunk web and continue ES installation without interruption. For example, you may have some processes outside of the operator where you are already checking that your Splunk deployment is web SSL enabled.|

The operator uses the following CR spec parameters to install the ES app on Splunk:

scope - use `premiumApps`
premiumAppsProps --> type: use `enterpriseSecurity`
esDefaults --> sslEnablement: possible value  `ignore`, `auto` or `strict` -- please see more details ssl_enablement values in the table above.

```yaml
  appSources:
        - name: esApp
          location: es_app/
          scope: premiumApps             
          premiumAppsProps:
            type: enterpriseSecurity
            esDefaults:
              sslEnablement: ignore
```

### Example YAMLs

In the following examples, you can find the YAML files for installing ES in a Splunk Standalone, Splunk SHC, and Indexer Cluster.

#### Install ES on a Standalone Splunk Instance

This example sets sslEnablement=ignore for a standalone CR. Change the setting to suit your requirements, either auto or strict. Before you use this example, copy the ES app package into the folder "security-team-apps/es_app" in your S3 bucket

```yaml
apiVersion: enterprise.splunk.com/v4
kind: Standalone
metadata:
  name: example
  finalizers:
  - enterprise.splunk.com/delete-pvc
spec:
  replicas: 1
  appRepo:
    appsRepoPollIntervalSeconds: 60
    defaults:
      volumeName: volume_app_repo
      scope: local
    appSources:
      - name: esApp
        location: es_app/
        scope: premiumApps
        premiumAppsProps:
          type: enterpriseSecurity
          esDefaults:
             sslEnablement: ignore
    volumes:
      - name: volume_app_repo
        storageType: s3
        provider: aws
        path: security-team-apps/
        endpoint: https://s3-us-west-2.amazonaws.com
        region: us-west-2
        secretRef: splunk-s3-secret
```

In order to use strict mode of sslEnablment, you can enable SSL on splunkd using the extraEnv variable SPLUNK_HTTP_ENABLESSL as shown below: 

```yaml
apiVersion: enterprise.splunk.com/v4
kind: Standalone
metadata:
  name: example
  finalizers:
  - enterprise.splunk.com/delete-pvc
spec:
  extraEnv:
    - name: SPLUNK_HTTP_ENABLESSL
      value : "true"
  replicas: 1
  appRepo:
    appsRepoPollIntervalSeconds: 60
    defaults:
      volumeName: volume_app_repo
      scope: local
    appSources:
      - name: esApp
        location: es_app/
        scope: premiumApps
        premiumAppsProps:
          type: enterpriseSecurity
          esDefaults:
             sslEnablement: strict
    volumes:
      - name: volume_app_repo
        storageType: s3
        provider: aws
        path: security-team-apps/
        endpoint: https://s3-us-west-2.amazonaws.com
        region: us-west-2
        secretRef: splunk-s3-secret
```


#### Install ES on a Search Head Cluster and Indexer Cluster splunk deployment

Use the following steps to install ES on a Splunk deployment with an SHC integrated with Indexer Cluster:

1. Downloaded ES app from https://splunkbase.splunk.com/app/263 and save the package in the S3 path security-team-apps/es-app
2. Use kubectl to apply the following YAML file
2. Wait for the SHC, CM, and Indexers pods are in ready state.
3. Login to an SH and verify that Enterprise Security App is installed.
3. Extract the Splunk_TA_ForIndexers using the steps given here: [https://docs.splunk.com/Documentation/ES/7.0.2/Install/InstallTechnologyAdd-ons]
4. Upload the extracted Splunk_TA_ForIndexers package to the S3 bucket folder named "es_app_indexer_ta"

The operator will poll this bucket after configured appsRepoPollIntervalSeconds and install the Splunk_TA_ForIndexers.
 
Following example shows creating a Splunk deployment with a SHC, ClusterManager and a IndexerCluster.
Note the difference between the values of "scope" property for the SHC and ClusterManager. For the SHC, scope is set to "premiumApps" whereas for the ClusterManager, scope is set to "cluster"

```yaml
apiVersion: enterprise.splunk.com/v4
kind: SearchHeadCluster
metadata:
  name: shc-es
  finalizers:
  - enterprise.splunk.com/delete-pvc
spec:
  appRepo:
    appsRepoPollIntervalSeconds: 60
    defaults:
      volumeName: volume_app_repo
      scope: local
    appSources:
      - name: esApp
        location: es_app/
        scope: premiumApps
        premiumAppsProps:
          type: enterpriseSecurity
          esDefaults:
             sslEnablement: ignore
    volumes:
      - name: volume_app_repo
        storageType: s3
        provider: aws
        path: security-team-apps/
        endpoint: https://s3-us-west-2.amazonaws.com
        region: us-west-2
        secretRef: splunk-s3-secret
  clusterManagerRef:
    name: cm-es
---
apiVersion: enterprise.splunk.com/v4
kind: ClusterManager
metadata:
  name: cm-es
  finalizers:
  - enterprise.splunk.com/delete-pvc
spec:
  appRepo:
    appsRepoPollIntervalSeconds: 60
    defaults:
      volumeName: volume_app_repo
      scope: local
    appSources:
      - name: esAppIndexer
        location: es_app_indexer_ta/
        scope: cluster
    volumes:
      - name: volume_app_repo
        storageType: s3
        provider: aws
        path: security-team-apps/
        endpoint: https://s3-us-west-2.amazonaws.com
        region: us-west-2
        secretRef: splunk-s3-secret
---
apiVersion: enterprise.splunk.com/v2
kind: IndexerCluster
metadata:
  name: idc-es
  finalizers:
  - enterprise.splunk.com/delete-pvc
spec:
  clusterManagerRef:
    name: cm-es
  replicas: 3
```

#### Consideration when using strict mode for "sslEnablment" in SHC

When using strict mode, the following additional steps are required so that the "splunk web ssl" is enabled for the SHC. These steps are necessary before you can install ES app with the strict mode of sslEnablement. Alternatively, if you are managing enableSplunkWebSSL setting outside of the Operator scope by pushing an app bundle through the deployer, then you can skip these steps.

Steps:
1. Create a SHC app (e.g., shccoreapp.spl) that contains a local/web.conf setting with `enableSplunkWebSSL=true`
2. Place this app under security-team-apps/coreapps in your s3 bucket.
3. Use the following YAML file as an example to deploy this app through app framework. 
4. Note: you also need to include the extraEnv:SPLUNK_HTTP_ENABLESSL with the value true in the YAML. This step will be revised in upcoming releases so it may not be needed as you are already pushing a web.conf with ssl setting.

Following is an example to enable Splunk Web SSL through operator on SHC:

```yaml
apiVersion: enterprise.splunk.com/v4
kind: SearchHeadCluster
metadata:
  name: shcssl
  finalizers:
  - enterprise.splunk.com/delete-pvc
spec:
  extraEnv:
    - name: SPLUNK_HTTP_ENABLESSL
      value : "true"
  appRepo:
    appsRepoPollIntervalSeconds: 60
    defaults:
      volumeName: volume_app_repo
      scope: local
    appSources:
      - name: coreapps
        scope: cluster
        location: coreapps/
    volumes:
      - name: volume_app_repo
        storageType: s3
        provider: aws
        path: security-team-apps/
        endpoint: https://s3-us-west-2.amazonaws.com
        region: us-west-2
        secretRef: splunk-s3-secret
```

#### Installation steps

1. Ensure that the Enterprise Security app package is present in the specified AppFramework S3 location with the correct appSources scope. Additionally, if configuring an indexer cluster, ensure that the Splunk_TA_ForIndexers app is present in the ClusterManager AppFramework s3 location with the appSources "cluster" scope.
   
2. Apply the specified custom resource(s), the Splunk Operator will handle installation and the environment will be ready to use once all pods are in the "Ready" state. Please refer to the examples above for creating your YAML files.
   
**Important Considerations**
* Installation may take upwards of 30 minutes.

#### Post Installation Configuration

After installing Enterprise Security you have other steps to complete:

* [Deploy add-ons to Splunk Enterprise Security](https://docs.splunk.com/Documentation/ES/latest/Install/InstallTechnologyAdd-ons) - Technology add-ons (TAs) which need to be installed to indexers can be installed via AppFramework, while TAs that reside on forwarders will need to be installed manually or via third party configuration management.

* [Setup Integration with Splunk Stream](https://docs.splunk.com/Documentation/ES/latest/Install/IntegrateSplunkStream) (optional)

* [Configure and deploy indexes](https://docs.splunk.com/Documentation/ES/latest/Install/Indexes) - The indexes associated with the packaged DAs and SAs will automatically be pushed to indexers when using indexer clustering, this step is only necessary if it is desired to configure any [custom index configuration](https://docs.splunk.com/Documentation/ES/latest/Install/Indexes#Index_configuration). Additionally, any newly installed technical ad-ons which are not included with the ES package may require index deployment.

* [Configure Users and Roles as desired](https://docs.splunk.com/Documentation/ES/latest/Install/ConfigureUsersRoles)

* [Configure Datamodels](https://docs.splunk.com/Documentation/ES/latest/Install/Datamodels)

### Upgrade Steps

To upgrade ES, move the new ES package into the specified AppFramework bucket. This will initiate a pod reset and begin the process of upgrading the new version. In indexer clustering environments, it is also necessary to move the new Splunk_TA_ForIndexers app to the Cluster Manager's AppFramework bucket that deploys apps to cluster members.

* The upgrade process will preserve any knowledge objects that exist in app local directories.

* Be sure to check the [ES upgrade notes](https://docs.splunk.com/Documentation/ES/latest/Install/Upgradetonewerversion#Version-specific_upgrade_notes) for any version specific changes.

### Troubleshooting

The following logs can be useful to check the ES app installation progress:

Splunk operator log:

```
kubectl logs <operator_pod_name>
```

Checking if there were any errors during ES install

ES post install failures:
Log shows one or more entries with the following content:
"premium scoped app package install failed" followed by (specific failure reasons show up here)

Example of error when you strict mode for sslEnablement and Splunk web is not SSL enabled 

```
2022-12-07T00:17:36.780549729Z  ERROR   handleEsappPostinstall  premium scoped app package install failed   {"controller": "searchheadcluster", "controllerGroup": "enterprise.splunk.com", "controllerKind": "SearchHeadCluster", "SearchHeadCluster": {"name":"shc1","namespace":"default"}, "namespace": "default", "name": "shc1", "reconcileID": "83133a29-ca0d-46cc-9ae5-6f26385d4506", "name": "shc1", "namespace": "default", "pod": "splunk-shc1-deployer-0", "app name": "splunk-enterprise-security_702.spl", "stdout": "", "stderr": "FATAL: Error in 'essinstall' command: You must have SSL enabled to continue\n", "post install command": "/opt/splunk/bin/splunk search '| essinstall --ssl_enablement strict --deployment_type shc_deployer' -auth admin:`cat /mnt/splunk-secrets/password`", "failCount": 1, "error": "command terminated with exit code 17"}
```
To fix this error, you have one of the following options:
1) Enable splunk web SSL- details on the section [Special consideration while using ssl enabled mode of strict in SHC](#special-consideration-while-using-ssl-enabled-mode-of-strict-in-shc)
2) Or, use sslEnablment=ignore

Example of a connection timeout error:

```
2022-12-07T00:48:11.542927588Z	ERROR	handleEsappPostinstall	premium scoped app package install failed	{"controller": "searchheadcluster", "controllerGroup": "enterprise.splunk.com", "controllerKind": "SearchHeadCluster", "SearchHeadCluster": {"name":"shc1it","namespace":"default"}, "namespace": "default", "name": "shc1it", "reconcileID": "b34966a2-e716-428f-a0c6-7611812e6b24", "name": "shc1it", "namespace": "default", "pod": "splunk-shc1it-deployer-0", "app name": "splunk-enterprise-security_702.spl", "stdout": "", "stderr": "FATAL: Error in 'essinstall' command: (InstallException) \"install_apps\" stage failed - Splunkd daemon is not responding: ('Error connecting to /services/apps/shc/es_deployer: The read operation timed out',)\n", "post install command": "/opt/splunk/bin/splunk search '| essinstall --ssl_enablement ignore --deployment_type shc_deployer' -auth admin:`cat /mnt/splunk-secrets/password`", "failCount": 2, "error": "command terminated with exit code 17"}
```

To fix this error, check the following areas:
1) Check the splunkd log in the deployer pod for any issues.
2) Check the splunkdConnectionTimeout setting in web.conf.

Logs of the respective pods:

```
kubectl logs <pod_name>
```

Check the pod log (e.g deployer pod) in case you want to monitor the pod while it is coming up to ready state or it has gone to an error state.


Common issues that may be encountered are : 
* ES installation failed as you used default sslEnablement mode ("strict") - enable Splunk Web SSL in web.conf. See the section [Special consideration while using ssl enabled mode of strict in SHC](#special-consideration-while-using-ssl-enabled-mode-of-strict-in-shc)
* Ansible task timeouts - raise associated timeout (splunkdConnectionTimeout in web.conf, rcv_timeout, send_timeeout, cxn_timeeout etc values in server.conf)
* Pod Recycles - raise livenessProbe value. More details on this at [Health Check doc](HealthCheck.md)

### Limitations

* For indexer clustering environments, you need to manually extract Splunk_TA_ForIndexers app and place in Cluster Manager AppFramework bucket to be deployed to indexers.

* Need to deploy add-ons to forwarders manually (or through your own methods).

* Need to deploy Stream App Manually
