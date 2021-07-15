// Copyright (c) 2018-2021 Splunk Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// 	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package enterprise

import (
	"fmt"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	enterpriseApi "github.com/splunk/splunk-operator/pkg/apis/enterprise/v2"
	splclient "github.com/splunk/splunk-operator/pkg/splunk/client"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	splctrl "github.com/splunk/splunk-operator/pkg/splunk/controller"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
)

// kubernetes logger used by splunk.enterprise package
var log = logf.Log.WithName("splunk.enterprise")

// GetRemoteStorageClient returns the corresponding S3Client
func GetRemoteStorageClient(client splcommon.ControllerClient, cr splcommon.MetaObject, appFrameworkRef *enterpriseApi.AppFrameworkSpec, vol *enterpriseApi.VolumeSpec, location string, fn splclient.GetInitFunc) (splclient.SplunkS3Client, error) {

	scopedLog := log.WithName("GetRemoteStorageClient").WithValues("name", cr.GetName(), "namespace", cr.GetNamespace())

	s3Client := splclient.SplunkS3Client{}
	//use the provider name to get the corresponding function pointer
	getClientWrapper := splclient.S3Clients[vol.Provider]
	getClient := getClientWrapper.GetS3ClientFuncPtr()

	appSecretRef := vol.SecretRef
	s3ClientSecret, err := splutil.GetSecretByName(client, cr, appSecretRef)
	if err != nil {
		return s3Client, err
	}

	// Get access keys
	accessKeyID := string(s3ClientSecret.Data["s3_access_key"])
	secretAccessKey := string(s3ClientSecret.Data["s3_secret_key"])

	if accessKeyID == "" {
		err = fmt.Errorf("accessKey missing")
		return s3Client, err
	}
	if secretAccessKey == "" {
		err = fmt.Errorf("S3 Secret Key is missing")
		return s3Client, err
	}

	// Get the bucket name form the "path" field
	bucket := strings.Split(vol.Path, "/")[0]

	//Get the prefix from the "path" field
	basePrefix := strings.TrimPrefix(vol.Path, bucket+"/")
	// if vol.Path contains just the bucket name(i.e without ending "/"), TrimPrefix returns the vol.Path
	// So, just reset the basePrefix to null
	if basePrefix == bucket {
		basePrefix = ""
	}

	// Join takes care of merging two paths and returns a clean result
	// Ex. ("a/b" + "c"),  ("a/b/" + "c"),  ("a/b/" + "/c"),  ("a/b/" + "/c"), ("a/b//", + "c/././") ("a/b/../b", + "c/../c") all are joined as "a/b/c"
	prefix := filepath.Join(basePrefix, location) + "/"

	scopedLog.Info("Creating the client", "volume", vol.Name, "bucket", bucket, "bucket path", prefix)

	s3Client.Client, err = getClient(bucket, accessKeyID, secretAccessKey, prefix, prefix /* startAfter*/, vol.Endpoint, fn)
	if err != nil {
		scopedLog.Error(err, "Failed to get the S3 client")
		return s3Client, err
	}

	return s3Client, nil
}

// ApplySplunkConfig reconciles the state of Kubernetes Secrets, ConfigMaps and other general settings for Splunk Enterprise instances.
func ApplySplunkConfig(client splcommon.ControllerClient, cr splcommon.MetaObject, spec enterpriseApi.CommonSplunkSpec, instanceType InstanceType) (*corev1.Secret, error) {
	var err error

	// Creates/updates the namespace scoped "splunk-secrets" K8S secret object
	namespaceScopedSecret, err := splutil.ApplyNamespaceScopedSecretObject(client, cr.GetNamespace())
	if err != nil {
		return nil, err
	}

	// Set secret owner references
	err = splutil.SetSecretOwnerRef(client, namespaceScopedSecret.GetName(), cr)
	if err != nil {
		return nil, err
	}

	// create splunk defaults (for inline config)
	if spec.Defaults != "" {
		defaultsMap := getSplunkDefaults(cr.GetName(), cr.GetNamespace(), instanceType, spec.Defaults)
		defaultsMap.SetOwnerReferences(append(defaultsMap.GetOwnerReferences(), splcommon.AsOwner(cr, true)))
		_, err = splctrl.ApplyConfigMap(client, defaultsMap)
		if err != nil {
			return nil, err
		}
	}

	return namespaceScopedSecret, nil
}

// getIndexerExtraEnv returns extra environment variables used by indexer clusters
func getIndexerExtraEnv(cr splcommon.MetaObject, replicas int32) []corev1.EnvVar {
	return []corev1.EnvVar{
		{
			Name:  "SPLUNK_INDEXER_URL",
			Value: GetSplunkStatefulsetUrls(cr.GetNamespace(), SplunkIndexer, cr.GetName(), replicas, false),
		},
	}
}

// getClusterMasterExtraEnv returns extra environment variables used by indexer clusters
func getClusterMasterExtraEnv(cr splcommon.MetaObject, spec *enterpriseApi.CommonSplunkSpec) []corev1.EnvVar {
	return []corev1.EnvVar{
		{
			Name:  "SPLUNK_CLUSTER_MASTER_URL",
			Value: GetSplunkServiceName(SplunkClusterMaster, cr.GetName(), false),
		},
	}
}

// getStandaloneExtraEnv returns extra environment variables used by monitoring console
func getStandaloneExtraEnv(cr splcommon.MetaObject, replicas int32) []corev1.EnvVar {
	return []corev1.EnvVar{
		{
			Name:  "SPLUNK_STANDALONE_URL",
			Value: GetSplunkStatefulsetUrls(cr.GetNamespace(), SplunkStandalone, cr.GetName(), replicas, false),
		},
	}
}

// getLicenseMasterURL returns URL of license master
func getLicenseMasterURL(cr splcommon.MetaObject, spec *enterpriseApi.CommonSplunkSpec) []corev1.EnvVar {
	if spec.LicenseMasterRef.Name != "" {
		licenseMasterURL := GetSplunkServiceName(SplunkLicenseMaster, spec.LicenseMasterRef.Name, false)
		if spec.LicenseMasterRef.Namespace != "" {
			licenseMasterURL = splcommon.GetServiceFQDN(spec.LicenseMasterRef.Namespace, licenseMasterURL)
		}
		return []corev1.EnvVar{
			{
				Name:  "SPLUNK_LICENSE_MASTER_URL",
				Value: licenseMasterURL,
			},
		}
	}
	return []corev1.EnvVar{
		{
			Name:  "SPLUNK_LICENSE_MASTER_URL",
			Value: GetSplunkServiceName(SplunkLicenseMaster, cr.GetName(), false),
		},
	}
}

// getSearchHeadExtraEnv returns extra environment variables used by search head clusters
func getSearchHeadEnv(cr *enterpriseApi.SearchHeadCluster) []corev1.EnvVar {

	// get search head env variables with deployer
	env := getSearchHeadExtraEnv(cr, cr.Spec.Replicas)
	env = append(env, corev1.EnvVar{
		Name:  "SPLUNK_DEPLOYER_URL",
		Value: GetSplunkServiceName(SplunkDeployer, cr.GetName(), false),
	})

	return env
}

// getSearchHeadExtraEnv returns extra environment variables used by search head clusters
func getSearchHeadExtraEnv(cr splcommon.MetaObject, replicas int32) []corev1.EnvVar {
	return []corev1.EnvVar{
		{
			Name:  "SPLUNK_SEARCH_HEAD_URL",
			Value: GetSplunkStatefulsetUrls(cr.GetNamespace(), SplunkSearchHead, cr.GetName(), replicas, false),
		}, {
			Name:  "SPLUNK_SEARCH_HEAD_CAPTAIN_URL",
			Value: GetSplunkStatefulsetURL(cr.GetNamespace(), SplunkSearchHead, cr.GetName(), 0, false),
		},
	}
}

// GetSmartstoreRemoteVolumeSecrets is used to retrieve S3 access key and secrete keys.
func GetSmartstoreRemoteVolumeSecrets(volume enterpriseApi.VolumeSpec, client splcommon.ControllerClient, cr splcommon.MetaObject, smartstore *enterpriseApi.SmartStoreSpec) (string, string, string, error) {
	namespaceScopedSecret, err := splutil.GetSecretByName(client, cr, volume.SecretRef)
	if err != nil {
		return "", "", "", err
	}

	accessKey := string(namespaceScopedSecret.Data[s3AccessKey])
	secretKey := string(namespaceScopedSecret.Data[s3SecretKey])

	splutil.SetSecretOwnerRef(client, volume.SecretRef, cr)

	if accessKey == "" {
		return "", "", "", fmt.Errorf("S3 Access Key is missing")
	} else if secretKey == "" {
		return "", "", "", fmt.Errorf("S3 Secret Key is missing")
	}

	return accessKey, secretKey, namespaceScopedSecret.ResourceVersion, nil
}

// ApplyAppListingConfigMap creates the configMap  with two entries:
// (1) app-list-local.yaml
// (2) app-list-cluster.yaml
// Once the configMap is mounted on the Pod, Ansible handles the apps listed in these files
// ToDo: Deletes to be handled for phase-3
func ApplyAppListingConfigMap(client splcommon.ControllerClient, cr splcommon.MetaObject,
	appConf *enterpriseApi.AppFrameworkSpec, appsSrcDeployStatus map[string]enterpriseApi.AppSrcDeployInfo) (*corev1.ConfigMap, bool, error) {

	var err error
	var crKind string
	var configMapDataChanged bool
	crKind = cr.GetObjectKind().GroupVersionKind().Kind

	scopedLog := log.WithName("ApplyAppListingConfigMap").WithValues("kind", crKind, "name", cr.GetName(), "namespace", cr.GetNamespace())

	mapAppListing := make(map[string]string)

	// Locally scoped apps for CM/Deployer require the latest splunk-ansible with apps_location_local.  Prior to this,
	// there was no method to install local apps for these roles.  If the apps_location_local variable is not available,
	// it will be ignored and revert back to no locally scoped apps for CM/Deployer.
	yamlConfHeader := fmt.Sprintf(`splunk:
  apps_location:`)
	yamlConfLocalHeader := fmt.Sprintf(`splunk:
  apps_location_local:`)

	var localAppsConf, clusterAppsConf string
	if crKind == "ClusterMaster" || crKind == "SearchHeadCluster" {
		localAppsConf = yamlConfLocalHeader
	} else {
		localAppsConf = yamlConfHeader
	}
	clusterAppsConf = yamlConfHeader
	var mapKeys []string

	// Map order is not guaranteed, so use the sorted keys to go through the map entries
	for appSrc := range appsSrcDeployStatus {
		mapKeys = append(mapKeys, appSrc)
	}
	sort.Strings(mapKeys)

	for _, appSrc := range mapKeys {
		appDeployList := appsSrcDeployStatus[appSrc].AppDeploymentInfoList

		switch scope := getAppSrcScope(appConf, appSrc); scope {
		case "local":
			for idx := range appDeployList {
				if appDeployList[idx].DeployStatus == enterpriseApi.DeployStatusPending &&
					appDeployList[idx].RepoState == enterpriseApi.RepoStateActive {
					localAppsConf = fmt.Sprintf(`%s
    - "/init-apps/%s/%s"`, localAppsConf, appSrc, appDeployList[idx].AppName)
				}
			}

		case "cluster":
			for idx := range appDeployList {
				if appDeployList[idx].DeployStatus == enterpriseApi.DeployStatusPending &&
					appDeployList[idx].RepoState == enterpriseApi.RepoStateActive {
					clusterAppsConf = fmt.Sprintf(`%s
    - "/init-apps/%s/%s"`, clusterAppsConf, appSrc, appDeployList[idx].AppName)
				}
			}

		default:
			scopedLog.Error(nil, "Invalid scope detected")
		}
	}

	if localAppsConf != yamlConfHeader && localAppsConf != yamlConfLocalHeader {
		mapAppListing["app-list-local.yaml"] = localAppsConf
	}

	if clusterAppsConf != yamlConfHeader {
		mapAppListing["app-list-cluster.yaml"] = clusterAppsConf
	}

	// Create App list config map
	configMapName := GetSplunkAppsConfigMapName(cr.GetName(), crKind)
	appListingConfigMap := splctrl.PrepareConfigMap(configMapName, cr.GetNamespace(), mapAppListing)

	appListingConfigMap.SetOwnerReferences(append(appListingConfigMap.GetOwnerReferences(), splcommon.AsOwner(cr, true)))

	if len(appListingConfigMap.Data) > 0 {
		configMapDataChanged, err = splctrl.ApplyConfigMap(client, appListingConfigMap)

		if err != nil {
			return nil, configMapDataChanged, err
		}
	}

	return appListingConfigMap, configMapDataChanged, nil
}

// ApplySmartstoreConfigMap creates the configMap with Smartstore config in INI format
func ApplySmartstoreConfigMap(client splcommon.ControllerClient, cr splcommon.MetaObject,
	smartstore *enterpriseApi.SmartStoreSpec) (*corev1.ConfigMap, bool, error) {

	var crKind string
	var configMapDataChanged bool
	crKind = cr.GetObjectKind().GroupVersionKind().Kind

	scopedLog := log.WithName("ApplySmartStoreConfigMap").WithValues("kind", crKind, "name", cr.GetName(), "namespace", cr.GetNamespace())

	// 1. Prepare the indexes.conf entries
	mapSplunkConfDetails := make(map[string]string)

	// Get the list of volumes in INI format
	volumesConfIni, err := GetSmartstoreVolumesConfig(client, cr, smartstore, mapSplunkConfDetails)
	if err != nil {
		return nil, configMapDataChanged, err
	}

	if volumesConfIni == "" {
		scopedLog.Info("Volume stanza list is empty")
	}

	// Get the list of indexes in INI format
	indexesConfIni := GetSmartstoreIndexesConfig(smartstore.IndexList)

	if indexesConfIni == "" {
		scopedLog.Info("Index stanza list is empty")
	} else if volumesConfIni == "" {
		return nil, configMapDataChanged, fmt.Errorf("Indexes without Volume configuration is not allowed")
	}

	defaultsConfIni := GetSmartstoreIndexesDefaults(smartstore.Defaults)

	iniSmartstoreConf := fmt.Sprintf(`%s %s %s`, defaultsConfIni, volumesConfIni, indexesConfIni)
	mapSplunkConfDetails["indexes.conf"] = iniSmartstoreConf

	// 2. Prepare server.conf entries
	iniServerConf := GetServerConfigEntries(&smartstore.DeepCopy().CacheManagerConf)
	mapSplunkConfDetails["server.conf"] = iniServerConf

	// Create smartstore config consisting indexes.conf
	configMapName := GetSplunkSmartstoreConfigMapName(cr.GetName(), crKind)
	SplunkOperatorAppConfigMap := splctrl.PrepareConfigMap(configMapName, cr.GetNamespace(), mapSplunkConfDetails)

	SplunkOperatorAppConfigMap.SetOwnerReferences(append(SplunkOperatorAppConfigMap.GetOwnerReferences(), splcommon.AsOwner(cr, true)))
	configMapDataChanged, err = splctrl.ApplyConfigMap(client, SplunkOperatorAppConfigMap)
	if err != nil {
		return nil, configMapDataChanged, err
	} else if configMapDataChanged {
		// Create a token to check if the config is really populated to the pod
		mapSplunkConfDetails[configToken] = fmt.Sprintf(`%d`, time.Now().Unix())

		// Apply the configMap with a fresh token
		configMapDataChanged, err = splctrl.ApplyConfigMap(client, SplunkOperatorAppConfigMap)
		if err != nil {
			return nil, configMapDataChanged, err
		}
	}

	return SplunkOperatorAppConfigMap, configMapDataChanged, nil
}

//  setupInitContainer modifies the podTemplateSpec object
func setupInitContainer(podTemplateSpec *corev1.PodTemplateSpec, Image string, imagePullPolicy string, commandOnContainer string) {
	containerSpec := corev1.Container{
		Image:           Image,
		ImagePullPolicy: corev1.PullPolicy(imagePullPolicy),
		Name:            "init",

		Command: []string{"bash", "-c", commandOnContainer},
		VolumeMounts: []corev1.VolumeMount{
			{Name: "pvc-etc", MountPath: "/opt/splk/etc"},
		},

		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("0.25"),
				corev1.ResourceMemory: resource.MustParse("128Mi"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("1"),
				corev1.ResourceMemory: resource.MustParse("512Mi"),
			},
		},
	}
	podTemplateSpec.Spec.InitContainers = append(podTemplateSpec.Spec.InitContainers, containerSpec)
}

// DeleteOwnerReferencesForResources used to delete any outstanding owner references
// Ideally we should be removing the owner reference wherever the CR is not controller for the resource
func DeleteOwnerReferencesForResources(client splcommon.ControllerClient, cr splcommon.MetaObject, smartstore *enterpriseApi.SmartStoreSpec) error {
	var err error
	scopedLog := log.WithName("DeleteOwnerReferencesForResources").WithValues("kind", cr.GetObjectKind().GroupVersionKind().Kind, "name", cr.GetName(), "namespace", cr.GetNamespace())

	if smartstore != nil {
		err = DeleteOwnerReferencesForS3SecretObjects(client, cr, smartstore)
	}

	// Delete references to Default secret object
	defaultSecretName := splcommon.GetNamespaceScopedSecretName(cr.GetNamespace())
	_, err = splutil.RemoveSecretOwnerRef(client, defaultSecretName, cr)
	if err != nil {
		scopedLog.Error(err, "Owner reference removal failed for Secret Object %s", defaultSecretName)
		return err
	}

	return nil
}

// DeleteOwnerReferencesForS3SecretObjects deletes owner references for all the secret objects referred by smartstore
// remote volume end points
func DeleteOwnerReferencesForS3SecretObjects(client splcommon.ControllerClient, cr splcommon.MetaObject, smartstore *enterpriseApi.SmartStoreSpec) error {
	scopedLog := log.WithName("DeleteOwnerReferencesForS3Secrets").WithValues("kind", cr.GetObjectKind().GroupVersionKind().Kind, "name", cr.GetName(), "namespace", cr.GetNamespace())

	var err error = nil
	if isSmartstoreConfigured(smartstore) == false {
		return err
	}

	volList := smartstore.VolList
	for _, volume := range volList {
		_, err = splutil.RemoveSecretOwnerRef(client, volume.SecretRef, cr)
		if err == nil {
			scopedLog.Info("Success", "Removed references for Secret Object %s", volume.SecretRef)
		} else {
			scopedLog.Error(err, "Owner reference removal failed for Secret Object %s", volume.SecretRef)
		}
	}

	return err
}

// S3ClientManager is used to manage all the S3 storage clients and their connections.
type S3ClientManager struct {
	client          splcommon.ControllerClient
	cr              splcommon.MetaObject
	appFrameworkRef *enterpriseApi.AppFrameworkSpec
	vol             *enterpriseApi.VolumeSpec
	location        string
	initFn          splclient.GetInitFunc
	getS3Client     func(client splcommon.ControllerClient, cr splcommon.MetaObject,
		appFrameworkRef *enterpriseApi.AppFrameworkSpec, vol *enterpriseApi.VolumeSpec,
		location string, fp splclient.GetInitFunc) (splclient.SplunkS3Client, error)
}

// GetAppsList gets the apps list
func (s3mgr *S3ClientManager) GetAppsList() (splclient.S3Response, error) {
	var s3Response splclient.S3Response

	c, err := s3mgr.getS3Client(s3mgr.client, s3mgr.cr, s3mgr.appFrameworkRef, s3mgr.vol, s3mgr.location, s3mgr.initFn)
	if err != nil {
		return s3Response, err
	}

	s3Response, err = c.Client.GetAppsList()
	if err != nil {
		return s3Response, err
	}
	return s3Response, nil
}

// GetAppListFromS3Bucket gets the list of apps from remote storage.
func GetAppListFromS3Bucket(client splcommon.ControllerClient, cr splcommon.MetaObject, appFrameworkRef *enterpriseApi.AppFrameworkSpec) (map[string]splclient.S3Response, error) {

	scopedLog := log.WithName("GetAppListFromS3Bucket").WithValues("name", cr.GetName(), "namespace", cr.GetNamespace())

	sourceToAppListMap := make(map[string]splclient.S3Response)

	scopedLog.Info("Getting the list of apps from remote storage...")

	var s3Response splclient.S3Response
	var vol enterpriseApi.VolumeSpec
	var err error
	var allSuccess bool = true

	for _, appSource := range appFrameworkRef.AppSources {
		vol, err = splclient.GetAppSrcVolume(appSource, appFrameworkRef)
		if err != nil {
			allSuccess = false
			continue
		}

		s3ClientWrapper := splclient.S3Clients[vol.Provider]
		initFunc := s3ClientWrapper.GetS3ClientInitFuncPtr()
		s3ClientMgr := S3ClientManager{
			client:          client,
			cr:              cr,
			appFrameworkRef: appFrameworkRef,
			vol:             &vol,
			location:        appSource.Location,
			initFn:          initFunc,
			getS3Client:     GetRemoteStorageClient,
		}

		// Now, get the apps list from remote storage
		s3Response, err = s3ClientMgr.GetAppsList()
		if err != nil {
			// move on to the next appSource if we are not able to get apps list
			scopedLog.Error(err, "Unable to get apps list", "appSource", appSource.Name)
			allSuccess = false
			continue
		}

		sourceToAppListMap[appSource.Name] = s3Response
	}

	if allSuccess == false {
		err = fmt.Errorf("Unable to get apps list from remote storage list for all the apps")
	}

	return sourceToAppListMap, err
}

// checkIfAnAppIsActiveOnRemoteStore checks if the App is listed as part of the AppSrc listing
func checkIfAnAppIsActiveOnRemoteStore(appName string, list []*splclient.RemoteObject) bool {
	for i := range list {
		if strings.HasSuffix(*list[i].Key, appName) {
			return true
		}
	}

	return false
}

// checkIfAppSrcExistsWithRemoteListing checks if a given AppSrc is part of the remote listing
func checkIfAppSrcExistsWithRemoteListing(appSrc string, remoteObjListingMap map[string]splclient.S3Response) bool {
	if _, ok := remoteObjListingMap[appSrc]; ok {
		return true
	}

	return false
}

// changeAppSrcDeployInfoStatus sets the new status to all the apps in an AppSrc if the given repo state and deploy status matches
// primarly used in Phase-3
func changeAppSrcDeployInfoStatus(appSrc string, appSrcDeployStatus map[string]enterpriseApi.AppSrcDeployInfo, repoState enterpriseApi.AppRepoState, oldDeployStatus enterpriseApi.AppDeploymentStatus, newDeployStatus enterpriseApi.AppDeploymentStatus) {
	scopedLog := log.WithName("changeAppSrcDeployInfoStatus").WithValues("Called for AppSource: ", appSrc, "repoState", repoState, "oldDeployStatus", oldDeployStatus, "newDeployStatus", newDeployStatus)

	if appSrcDeploymentInfo, ok := appSrcDeployStatus[appSrc]; ok {
		appDeployInfoList := appSrcDeploymentInfo.AppDeploymentInfoList
		for idx := range appDeployInfoList {
			// Modify the app status if the state and status matches
			if appDeployInfoList[idx].RepoState == repoState && appDeployInfoList[idx].DeployStatus == oldDeployStatus {
				appDeployInfoList[idx].DeployStatus = newDeployStatus
			}
		}

		// Update the Map entry again
		appSrcDeployStatus[appSrc] = appSrcDeploymentInfo
		scopedLog.Info("Complete")
	} else {
		// Ideally this should never happen, check if the "IsDeploymentInProgress" flag is handled correctly or not
		scopedLog.Error(nil, "Could not find the App Source in App context")
	}
}

// setStateAndStatusForAppDeployInfo sets the state and status for an App
func setStateAndStatusForAppDeployInfo(appDeployInfo *enterpriseApi.AppDeploymentInfo, repoState enterpriseApi.AppRepoState, deployStatus enterpriseApi.AppDeploymentStatus) {
	appDeployInfo.RepoState = repoState
	appDeployInfo.DeployStatus = deployStatus
}

// setStateAndStatusForAppDeployInfoList sets the state and status for a given list of Apps
func setStateAndStatusForAppDeployInfoList(appDeployList []enterpriseApi.AppDeploymentInfo, state enterpriseApi.AppRepoState, status enterpriseApi.AppDeploymentStatus) (bool, []enterpriseApi.AppDeploymentInfo) {
	var modified bool
	for idx := range appDeployList {
		setStateAndStatusForAppDeployInfo(&appDeployList[idx], state, status)
		modified = true
	}

	return modified, appDeployList
}

// handleAppRepoChanges parses the remote storage listing and updates the repoState and deployStatus accordingly
// client and cr are used when we put the glue logic to hand-off to the side car
func handleAppRepoChanges(client splcommon.ControllerClient, cr splcommon.MetaObject,
	appDeployContext *enterpriseApi.AppDeploymentContext, remoteObjListingMap map[string]splclient.S3Response, appFrameworkConfig *enterpriseApi.AppFrameworkSpec) error {
	crKind := cr.GetObjectKind().GroupVersionKind().Kind
	scopedLog := log.WithName("handleAppRepoChanges").WithValues("kind", crKind, "name", cr.GetName(), "namespace", cr.GetNamespace())
	var err error

	scopedLog.Info("received App listing", "for App sources", len(remoteObjListingMap))
	if remoteObjListingMap == nil || len(remoteObjListingMap) == 0 {
		scopedLog.Error(nil, "remoteObjectList is empty. Any apps that are already deployed will be disabled")
	}

	// Check if the appSource is still valid in the config
	for appSrc := range remoteObjListingMap {
		if !CheckIfAppSrcExistsInConfig(appFrameworkConfig, appSrc) {
			err = fmt.Errorf("App source: %s no more exists, this should never happen", appSrc)
			return err
		}
	}

	// ToDo: Ideally, this check should go to the reconcile entry point once the glue logic in place.
	if appDeployContext.AppsSrcDeployStatus == nil {
		appDeployContext.AppsSrcDeployStatus = make(map[string]enterpriseApi.AppSrcDeployInfo)
	}

	// 1. Check if the AppSrc is deleted in latest config, OR missing with the remote listing.
	for appSrc, appSrcDeploymentInfo := range appDeployContext.AppsSrcDeployStatus {
		// If the AppSrc is missing mark all the corresponding apps for deletion
		if !CheckIfAppSrcExistsInConfig(appFrameworkConfig, appSrc) ||
			!checkIfAppSrcExistsWithRemoteListing(appSrc, remoteObjListingMap) {
			scopedLog.Info("App change", "deleting/disabling all the apps for App source: ", appSrc, "Reason: App source is mising in config or remote listing")
			curAppDeployList := appSrcDeploymentInfo.AppDeploymentInfoList
			var modified bool

			modified, appSrcDeploymentInfo.AppDeploymentInfoList = setStateAndStatusForAppDeployInfoList(curAppDeployList, enterpriseApi.RepoStateDeleted, enterpriseApi.DeployStatusPending)

			if modified {
				appDeployContext.IsDeploymentInProgress = true
				// Finally update the Map entry with latest info
				appDeployContext.AppsSrcDeployStatus[appSrc] = appSrcDeploymentInfo
			}
		}
	}

	// 2. Go through each AppSrc from the remote listing
	for appSrc, s3Response := range remoteObjListingMap {
		// 2.1 Mark Apps for deletion if they are missing in remote listing
		appSrcDeploymentInfo, appSrcExistsLocally := appDeployContext.AppsSrcDeployStatus[appSrc]

		if appSrcExistsLocally {
			currentList := appSrcDeploymentInfo.AppDeploymentInfoList
			for appIdx := range currentList {
				if !checkIfAnAppIsActiveOnRemoteStore(currentList[appIdx].AppName, s3Response.Objects) {
					scopedLog.Info("App change", "deleting/disabling the App: ", currentList[appIdx].AppName, "as it is missing in the remote listing", nil)
					setStateAndStatusForAppDeployInfo(&currentList[appIdx], enterpriseApi.RepoStateDeleted, enterpriseApi.DeployStatusPending)
					appDeployContext.IsDeploymentInProgress = true
				}
			}
		}

		// 2.2 Check for any App changes(Ex. A new App source, a new App added/updated)
		if AddOrUpdateAppSrcDeploymentInfoList(&appSrcDeploymentInfo, s3Response.Objects) {
			appDeployContext.IsDeploymentInProgress = true
		}

		// Finally update the Map entry with latest info
		appDeployContext.AppsSrcDeployStatus[appSrc] = appSrcDeploymentInfo
	}

	return err
}

// isAppExtentionValid checks if an app extention is supported or not
func isAppExtentionValid(receivedKey string) bool {
	appExtIdx := strings.LastIndex(receivedKey, ".")
	if appExtIdx < 0 {
		return false
	}

	switch appExt := receivedKey[appExtIdx+1:]; appExt {
	case "spl":
		return true

	case "tgz":
		return true

	default:
		return false
	}
}

// AddOrUpdateAppSrcDeploymentInfoList  modifies the App deployment status as perceived from the remote object listing
func AddOrUpdateAppSrcDeploymentInfoList(appSrcDeploymentInfo *enterpriseApi.AppSrcDeployInfo, remoteS3ObjList []*splclient.RemoteObject) bool {
	scopedLog := log.WithName("AddOrUpdateAppSrcDeploymentInfoList").WithValues("Called with length: ", len(remoteS3ObjList))

	var found bool
	var appName string
	var newAppInfoList []enterpriseApi.AppDeploymentInfo
	var appChangesDetected bool
	var appDeployInfo enterpriseApi.AppDeploymentInfo

	for _, remoteObj := range remoteS3ObjList {
		receivedKey := *remoteObj.Key
		if !isAppExtentionValid(receivedKey) {
			scopedLog.Error(nil, "App name Parsing: Ignoring the key: ", receivedKey, "with invalid extention")
			continue
		}

		nameAt := strings.LastIndex(receivedKey, "/")
		appName = receivedKey[nameAt+1:]

		// Now update App status as seen in the remote listing
		found = false
		appList := appSrcDeploymentInfo.AppDeploymentInfoList
		for idx := range appList {
			if appList[idx].AppName == appName {
				found = true
				if appList[idx].ObjectHash != *remoteObj.Etag || appList[idx].RepoState == enterpriseApi.RepoStateDeleted {
					scopedLog.Info("App change detected.", "App name: ", appName, "marking for an update")
					appList[idx].ObjectHash = *remoteObj.Etag
					appList[idx].DeployStatus = enterpriseApi.DeployStatusPending

					// Make the state active for an app that was deleted earlier, and got activated again
					if appList[idx].RepoState == enterpriseApi.RepoStateDeleted {
						scopedLog.Info("App change", "enabling the App name: ", appName, "that was previously disabled/deleted")
						appList[idx].RepoState = enterpriseApi.RepoStateActive
					}
					appChangesDetected = true
				}

				// Found the App and finished the needed work. we can break here
				break
			}
		}

		// Update our local list if it is a new app
		if !found {
			scopedLog.Info("New App", "found: ", appName)
			appDeployInfo.AppName = appName
			appDeployInfo.ObjectHash = *remoteObj.Etag
			appDeployInfo.RepoState = enterpriseApi.RepoStateActive
			appDeployInfo.DeployStatus = enterpriseApi.DeployStatusPending

			// Add it to a seperate list so that we don't loop through the newly added entries
			newAppInfoList = append(newAppInfoList, appDeployInfo)
			appChangesDetected = true
		}
	}

	// Add the newly discovered Apps to the App source group
	appSrcDeploymentInfo.AppDeploymentInfoList = append(appSrcDeploymentInfo.AppDeploymentInfoList, newAppInfoList...)

	return appChangesDetected
}

// markAppsStatusToComplete sets the required status for a given state.
// Gets called from glue logic based on how we want to hand-off to init/side car, and look for the return status
// For now, two possible cases:
// 1. Completing the changes for Deletes. Called with state=AppStateDeleted, and status=DeployStatusPending
// 2. Completing the changes for Active(Apps newly added, apps modified, Apps previously deleted, and now active).
// Note:- Used in only for Phase-2
func markAppsStatusToComplete(appSrcDeplymentStatus map[string]enterpriseApi.AppSrcDeployInfo) error {
	var err error
	scopedLog := log.WithName("markAppsStatusToComplete")

	// ToDo: Passing appSrcDeplymentStatus is redundant, but this function will go away in phase-3, so ok for now.
	for appSrc := range appSrcDeplymentStatus {
		changeAppSrcDeployInfoStatus(appSrc, appSrcDeplymentStatus, enterpriseApi.RepoStateActive, enterpriseApi.DeployStatusPending, enterpriseApi.DeployStatusComplete)
		changeAppSrcDeployInfoStatus(appSrc, appSrcDeplymentStatus, enterpriseApi.RepoStateDeleted, enterpriseApi.DeployStatusPending, enterpriseApi.DeployStatusComplete)
	}

	scopedLog.Info("Marked the App deployment status to complete")
	// ToDo: sgontla: Caller of this API also needs to set "IsDeploymentInProgress = false" once after completing this function call for all the app sources

	return err
}

// setupAppInitContainers creates the necessary shared volume and init containers to download all
// app packages in the appSources configured and make them locally available to the Splunk instance.
func setupAppInitContainers(client splcommon.ControllerClient, cr splcommon.MetaObject, podTemplateSpec *corev1.PodTemplateSpec, appFrameworkConfig *enterpriseApi.AppFrameworkSpec) {
	scopedLog := log.WithName("setupAppInitContainers")
	// Create shared volume and init containers for App Framework
	if len(appFrameworkConfig.AppSources) > 0 {
		// Create volume to shared between init and Splunk container to contain downloaded apps
		emptyVolumeSource := corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{},
		}

		initVol := corev1.Volume{
			Name:         appVolumeMntName,
			VolumeSource: emptyVolumeSource,
		}

		podTemplateSpec.Spec.Volumes = append(podTemplateSpec.Spec.Volumes, initVol)

		// Add init apps mount to Splunk container
		initVolumeSpec := corev1.VolumeMount{
			Name:      appVolumeMntName,
			MountPath: appBktMnt,
		}

		// This assumes the Splunk instance container is Containers[0], which I *believe* is valid
		podTemplateSpec.Spec.Containers[0].VolumeMounts = append(podTemplateSpec.Spec.Containers[0].VolumeMounts, initVolumeSpec)

		// Add app framework init containers per app source and attach the init volume
		for i, appSrc := range appFrameworkConfig.AppSources {
			// Get volume info from appSrc

			var volSpecPos int
			var err error
			if appSrc.VolName != "" {
				volSpecPos, err = splclient.CheckIfVolumeExists(appFrameworkConfig.VolList, appSrc.VolName)
			} else {
				volSpecPos, err = splclient.CheckIfVolumeExists(appFrameworkConfig.VolList, appFrameworkConfig.Defaults.VolName)
			}

			if err != nil {
				// Invalid appFramework config.  This shouldn't happen
				scopedLog.Info("Invalid appSrc volume spec, moving to the next one", "appSrc.VolName", appSrc.VolName, "err", err)
				continue
			}
			appRepoVol := appFrameworkConfig.VolList[volSpecPos]

			s3ClientWrapper := splclient.S3Clients[appRepoVol.Provider]
			initFunc := s3ClientWrapper.GetS3ClientInitFuncPtr()
			// Use the provider name to get the corresponding function pointer
			s3Client, err := GetRemoteStorageClient(client, cr, appFrameworkConfig, &appRepoVol, appSrc.Location, initFunc)
			if err != nil {
				// move on to the next appSource if we are not able to get the required client
				scopedLog.Info("Invalid Remote Storage Client", "appRepoVol.Name", appRepoVol.Name, "err", err)
				continue
			}

			// Prepare app source/repo values
			appBkt := appRepoVol.Path
			appS3Endpoint := appRepoVol.Endpoint
			appSecretRef := appRepoVol.SecretRef
			appSrcName := appSrc.Name
			appSrcPath := appSrc.Location
			appSrcScope := getAppSrcScope(appFrameworkConfig, appSrc.Name)
			initContainerName := strings.ToLower(fmt.Sprintf(initContainerTemplate, appSrcName, i, appSrcScope))

			// Setup init container
			initContainerSpec := corev1.Container{
				Image:           s3Client.Client.GetInitContainerImage(),
				ImagePullPolicy: "IfNotPresent",
				Name:            initContainerName,
				Args:            s3Client.Client.GetInitContainerCmd(appS3Endpoint, appBkt, appSrcPath, appSrcName, appBktMnt),
				Env: []corev1.EnvVar{
					{
						Name: "AWS_ACCESS_KEY_ID",
						ValueFrom: &corev1.EnvVarSource{
							SecretKeyRef: &corev1.SecretKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{
									Name: appSecretRef,
								},
								Key: s3AccessKey,
							},
						},
					},
					{
						Name: "AWS_SECRET_ACCESS_KEY",
						ValueFrom: &corev1.EnvVarSource{
							SecretKeyRef: &corev1.SecretKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{
									Name: appSecretRef,
								},
								Key: s3SecretKey,
							},
						},
					},
				},
			}

			// Add mount to initContainer, same mount used for Splunk instance container as well
			initContainerSpec.VolumeMounts = []corev1.VolumeMount{
				{
					Name:      appVolumeMntName,
					MountPath: appBktMnt,
				},
			}
			podTemplateSpec.Spec.InitContainers = append(podTemplateSpec.Spec.InitContainers, initContainerSpec)
		}
	}
}

// SetLastAppInfoCheckTime sets the last check time to current time
func SetLastAppInfoCheckTime(appInfoStatus *enterpriseApi.AppDeploymentContext) {
	scopedLog := log.WithName("SetLastAppInfoCheckTime")
	currentEpoch := time.Now().Unix()

	scopedLog.Info("Setting the LastAppInfoCheckTime to current time", "current epoch time", currentEpoch)

	appInfoStatus.LastAppInfoCheckTime = currentEpoch
}

// HasAppRepoCheckTimerExpired checks if the polling interval has expired
func HasAppRepoCheckTimerExpired(appInfoContext *enterpriseApi.AppDeploymentContext) bool {
	scopedLog := log.WithName("HasAppRepoCheckTimerExpired")
	currentEpoch := time.Now().Unix()

	isTimerExpired := appInfoContext.LastAppInfoCheckTime+appInfoContext.AppsRepoStatusPollInterval <= currentEpoch
	if isTimerExpired == true {
		scopedLog.Info("App repo polling interval timer has expired", "LastAppInfoCheckTime", strconv.FormatInt(appInfoContext.LastAppInfoCheckTime, 10), "current epoch time", strconv.FormatInt(currentEpoch, 10))
	}

	return isTimerExpired
}

// GetNextRequeueTime gets the next reconcile requeue time based on the appRepoPollInterval.
// There can be some time elapsed between when we first set lastAppInfoCheckTime and when the CR is in Ready state.
// Hence we need to subtract the delta time elapsed from the actual polling interval,
// so that the next reconile would happen at the right time.
func GetNextRequeueTime(appRepoPollInterval, lastCheckTime int64) time.Duration {
	scopedLog := log.WithName("GetNextRequeueTime")
	currentEpoch := time.Now().Unix()

	var nextRequeueTimeInSec int64
	nextRequeueTimeInSec = appRepoPollInterval - (currentEpoch - lastCheckTime)

	scopedLog.Info("Getting next requeue time", "LastAppInfoCheckTime", lastCheckTime, "Current Epoch time", currentEpoch, "nextRequeueTimeInSec", nextRequeueTimeInSec)

	return time.Second * (time.Duration(nextRequeueTimeInSec))
}

// initAndCheckAppInfoStatus initializes the S3Clients and checks the status of apps on remote storage.
func initAndCheckAppInfoStatus(client splcommon.ControllerClient, cr splcommon.MetaObject, appFrameworkConf *enterpriseApi.AppFrameworkSpec, appStatusContext *enterpriseApi.AppDeploymentContext) error {
	scopedLog := log.WithName("initAndCheckAppInfoStatus").WithValues("name", cr.GetName(), "namespace", cr.GetNamespace())

	var err error
	// Register the S3 clients specific to providers if not done already
	// This is done to prevent the null pointer dereference in case when
	// operator crashes and comes back up and the status of app context was updated
	// to match the spec in the previous run.
	initAppFrameWorkContext(appFrameworkConf, appStatusContext)

	//check if the apps need to be downloaded from remote storage
	if HasAppRepoCheckTimerExpired(appStatusContext) || !reflect.DeepEqual(appStatusContext.AppFrameworkConfig, *appFrameworkConf) {
		var sourceToAppsList map[string]splclient.S3Response

		scopedLog.Info("Checking status of apps on remote storage...")

		sourceToAppsList, err = GetAppListFromS3Bucket(client, cr, appFrameworkConf)
		// TODO: gaurav, we need to handle this case better in Phase-3. There can be a possibility
		// where if an appSource is missing in remote store, we mark it for deletion. But if it comes up
		// next time, we will recycle the pod to install the app. We need to find a way to reduce the pod recycles.
		if len(sourceToAppsList) != len(appFrameworkConf.AppSources) {
			scopedLog.Error(err, "Unable to get apps list, will retry in next reconcile...")
		} else {

			for _, appSource := range appFrameworkConf.AppSources {
				scopedLog.Info("Apps List retrieved from remote storage", "App Source", appSource.Name, "Content", sourceToAppsList[appSource.Name].Objects)
			}

			// Only handle the app repo changes if we were able to successfully get the apps list
			err = handleAppRepoChanges(client, cr, appStatusContext, sourceToAppsList, appFrameworkConf)
			if err != nil {
				scopedLog.Error(err, "Unable to use the App list retrieved from the remote storage")
				return err
			}

			_, _, err = ApplyAppListingConfigMap(client, cr, appFrameworkConf, appStatusContext.AppsSrcDeployStatus)
			if err != nil {
				return err
			}

			appStatusContext.AppFrameworkConfig = *appFrameworkConf
		}

		// set the last check time to current time
		SetLastAppInfoCheckTime(appStatusContext)
	}

	return nil
}
