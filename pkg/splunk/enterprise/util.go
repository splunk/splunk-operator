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
	"regexp"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	enterprisev1 "github.com/splunk/splunk-operator/pkg/apis/enterprise/v1"
	splclient "github.com/splunk/splunk-operator/pkg/splunk/client"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	splctrl "github.com/splunk/splunk-operator/pkg/splunk/controller"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
)

// kubernetes logger used by splunk.enterprise package
var log = logf.Log.WithName("splunk.enterprise")

// regex to extract the region from the s3 endpoint
var regionRegex = ".*.s3[-,.](?P<region>.*).amazonaws.com"

//getRegion extracts the region from the endpoint field
func getRegion(endpoint string) string {
	pattern := regexp.MustCompile(regionRegex)
	return pattern.FindStringSubmatch(endpoint)[1]
}

// GetRemoteStorageClient returns the corresponding S3Client
func GetRemoteStorageClient(client splcommon.ControllerClient, cr splcommon.MetaObject, appFrameworkRef *enterprisev1.AppFrameworkSpec, vol *enterprisev1.VolumeSpec, location string) (splclient.S3Client, error) {

	scopedLog := log.WithName("GetRemoteStorageClient").WithValues("name", cr.GetName(), "namespace", cr.GetNamespace())

	//use the provider name to get the corresponding function pointer
	getClient, _ := splclient.S3Clients[vol.Provider]

	appSecretRef := vol.SecretRef
	s3ClientSecret, err := splutil.GetSecretByName(client, cr, appSecretRef)
	if err != nil {
		return nil, err
	}

	// Get access keys
	accessKeyID := string(s3ClientSecret.Data["s3_access_key"])
	secretAccessKey := string(s3ClientSecret.Data["s3_secret_key"])

	if accessKeyID == "" {
		scopedLog.Error(err, "accessKey missing")
		return nil, err
	}
	if secretAccessKey == "" {
		scopedLog.Error(err, "S3 Secret Key is missing")
		return nil, err
	}

	// Get region from "endpoint" field
	region := getRegion(vol.Endpoint)

	// Get the bucket name form the "path" field
	bucket := strings.Split(vol.Path, "/")[0]

	//Get the prefix from the "path" field
	prefix := strings.TrimPrefix(vol.Path, bucket+"/") + location

	return getClient(region, bucket, accessKeyID, secretAccessKey, prefix, prefix /* startAfter*/), nil
}

// ApplySplunkConfig reconciles the state of Kubernetes Secrets, ConfigMaps and other general settings for Splunk Enterprise instances.
func ApplySplunkConfig(client splcommon.ControllerClient, cr splcommon.MetaObject, spec enterprisev1.CommonSplunkSpec, instanceType InstanceType) (*corev1.Secret, error) {
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
func getClusterMasterExtraEnv(cr splcommon.MetaObject, spec *enterprisev1.CommonSplunkSpec) []corev1.EnvVar {
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
func getLicenseMasterURL(cr splcommon.MetaObject, spec *enterprisev1.CommonSplunkSpec) []corev1.EnvVar {
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
func getSearchHeadEnv(cr *enterprisev1.SearchHeadCluster) []corev1.EnvVar {

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
func GetSmartstoreRemoteVolumeSecrets(volume enterprisev1.VolumeSpec, client splcommon.ControllerClient, cr splcommon.MetaObject, smartstore *enterprisev1.SmartStoreSpec) (string, string, string, error) {
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

// ApplySmartstoreConfigMap creates the configMap with Smartstore config in INI format
func ApplySmartstoreConfigMap(client splcommon.ControllerClient, cr splcommon.MetaObject,
	smartstore *enterprisev1.SmartStoreSpec) (*corev1.ConfigMap, bool, error) {

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
	SplunkOperatorAppConfigMap := prepareSplunkSmartstoreConfigMap(cr.GetName(), cr.GetNamespace(), crKind, mapSplunkConfDetails)

	SplunkOperatorAppConfigMap.SetOwnerReferences(append(SplunkOperatorAppConfigMap.GetOwnerReferences(), splcommon.AsOwner(cr, true)))
	configMapDataChanged, err = splctrl.ApplyConfigMap(client, SplunkOperatorAppConfigMap)
	if err != nil {
		return nil, configMapDataChanged, err
	} else if configMapDataChanged {
		// Create a token to check if the config is really populated to the pod
		mapSplunkConfDetails[configToken] = fmt.Sprintf(`%d`, time.Now().Unix())

		// Apply the configMap with a fresh token
		SplunkOperatorAppConfigMap = prepareSplunkSmartstoreConfigMap(cr.GetName(), cr.GetNamespace(), crKind, mapSplunkConfDetails)
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
func DeleteOwnerReferencesForResources(client splcommon.ControllerClient, cr splcommon.MetaObject, smartstore *enterprisev1.SmartStoreSpec) error {
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
func DeleteOwnerReferencesForS3SecretObjects(client splcommon.ControllerClient, cr splcommon.MetaObject, smartstore *enterprisev1.SmartStoreSpec) error {
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

// GetAppListFromS3Bucket gets the list of apps from remote storage.
func GetAppListFromS3Bucket(client splcommon.ControllerClient, cr splcommon.MetaObject, appFrameworkRef *enterprisev1.AppFrameworkSpec) map[string]splclient.S3Response {

	scopedLog := log.WithName("GetAppListFromS3Bucket").WithValues("name", cr.GetName(), "namespace", cr.GetNamespace())

	sourceToAppListMap := make(map[string]splclient.S3Response)

	scopedLog.Info("Getting the list of apps from remote storage...")

	var s3Response splclient.S3Response
	var vol enterprisev1.VolumeSpec
	var err error
	var s3Client splclient.S3Client
	var index int

	for _, appSource := range appFrameworkRef.AppSources {
		var volName string
		// get the volume spec from the volume name
		if appSource.VolName != "" {
			volName = appSource.VolName
		} else {
			volName = appFrameworkRef.Defaults.VolName
		}

		index, err = checkIfVolumeExists(appFrameworkRef.VolList, volName)
		if err != nil {
			scopedLog.Error(err, "Invalid volume name provided. Please specify a valid volume name.", "App source", appSource.Name, "Volume name", volName)
			continue
		}
		vol = appFrameworkRef.VolList[index]

		//get the corresponding S3 client
		s3Client, err = GetRemoteStorageClient(client, cr, appFrameworkRef, &vol, appSource.Location)
		if err != nil {
			// move on to the next appSource if we are not able to get the rquired client
			scopedLog.Error(err, "Unable to get remote storage client", "appSource", appSource.Name)
			continue
		}

		// Now, get the apps list from remote storage
		s3Response, err = s3Client.GetAppsList()
		if err != nil {
			// move on to the next appSource if we are not able to get apps list
			scopedLog.Error(err, "Unable to get apps list", "appSource", appSource.Name)
			continue
		}

		sourceToAppListMap[appSource.Name] = s3Response
	}

	return sourceToAppListMap
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
func changeAppSrcDeployInfoStatus(appSrc string, appSrcDeployStatus map[string]enterprisev1.AppSrcDeployInfo, repoState enterprisev1.AppRepoState, oldDeployStatus enterprisev1.AppDeploymentStatus, newDeployStatus enterprisev1.AppDeploymentStatus) {
	scopedLog := log.WithName("changeAppSrcDeployInfoStatus").WithValues("Called for AppSource: ", appSrc, "repoState", repoState, "oldDeployStatus", oldDeployStatus, "newDeployStatus", newDeployStatus)

	if appSrcDeploymentInfo, ok := appSrcDeployStatus[appSrc]; ok {
		appDeployInfoList := appSrcDeploymentInfo.AppDeploymentInfoList
		for _, appDeployInfo := range appDeployInfoList {
			// Modify the app status if the state and status matches
			if appDeployInfo.RepoState == repoState && appDeployInfo.DeployStatus == oldDeployStatus {
				appDeployInfo.DeployStatus = newDeployStatus
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
func setStateAndStatusForAppDeployInfo(appDeployInfo enterprisev1.AppDeploymentInfo, repoState enterprisev1.AppRepoState, deployStatus enterprisev1.AppDeploymentStatus) {
	appDeployInfo.RepoState = repoState
	appDeployInfo.DeployStatus = deployStatus
}

// setStateAndStatusForAppDeployInfoList sets the state and status for a given list of Apps
// Do not change the list lenth in this. ToDo: sgontla: just to be safe return the list
func setStateAndStatusForAppDeployInfoList(appDeployList []enterprisev1.AppDeploymentInfo, state enterprisev1.AppRepoState, status enterprisev1.AppDeploymentStatus) bool {
	var modified bool
	for _, appInfo := range appDeployList {
		setStateAndStatusForAppDeployInfo(appInfo, state, status)
		modified = true
	}

	return modified
}

// handleAppRepoChanges parses the remote storage listing and updates the repoState and deployStatus accordingly
// clinet and cr are used when we put the glue logic to hand-off to the side car
func handleAppRepoChanges(client splcommon.ControllerClient, cr splcommon.MetaObject,
	appDeployContext *enterprisev1.AppDeploymentContext, remoteObjListingMap map[string]splclient.S3Response, appFrameworkConfig *enterprisev1.AppFrameworkSpec) error {
	crKind := cr.GetObjectKind().GroupVersionKind().Kind
	scopedLog := log.WithName("handleAppRepoChanges").WithValues("kind", crKind, "name", cr.GetName(), "namespace", cr.GetNamespace())
	var err error

	scopedLog.Info("received App listing for %d app sources", len(remoteObjListingMap))
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

	// ToDo: sgontla: Ideally, this check should go to the reconcile entry point once the glue logic in place.
	if appDeployContext.AppsSrcDeployStatus == nil {
		appDeployContext.AppsSrcDeployStatus = make(map[string]enterprisev1.AppSrcDeployInfo)
	}

	// 1. Check if the AppSrc is deleted in latest config, OR missing with the remote listing.
	for appSrc, appSrcDeploymentInfo := range appDeployContext.AppsSrcDeployStatus {
		// If the AppSrc is missing mark all the corresponding apps for deletion
		if !CheckIfAppSrcExistsInConfig(appFrameworkConfig, appSrc) ||
			!checkIfAppSrcExistsWithRemoteListing(appSrc, remoteObjListingMap) {
			scopedLog.Info("App change", "deleting/disabling all the apps for App source: ", appSrc, "Reason: App source is mising in config or remote listing")
			curAppDeployList := appSrcDeploymentInfo.AppDeploymentInfoList
			if setStateAndStatusForAppDeployInfoList(curAppDeployList, enterprisev1.RepoStateDeleted, enterprisev1.DeployStatusPending) {
				appDeployContext.IsDeploymentInProgress = true
			}
		}

		// Finally update the Map entry with latest info
		appDeployContext.AppsSrcDeployStatus[appSrc] = appSrcDeploymentInfo
	}

	// 2. Go through each AppSrc from the remote listing
	for appSrc, s3Response := range remoteObjListingMap {
		// 2.1 Mark Apps for deletion if they are missing in remote listing
		appSrcDeploymentInfo, appSrcExistsLocally := appDeployContext.AppsSrcDeployStatus[appSrc]

		if appSrcExistsLocally {
			currentList := appSrcDeploymentInfo.AppDeploymentInfoList
			for appIdx := range currentList {
				if !checkIfAnAppIsActiveOnRemoteStore(currentList[appIdx].AppName, s3Response.Objects) {
					scopedLog.Info("App change", "deleting/disabling the App: ", currentList[appIdx].AppName, "as it is missing in the remote listing")
					setStateAndStatusForAppDeployInfo(currentList[appIdx], enterprisev1.RepoStateDeleted, enterprisev1.DeployStatusPending)
					appDeployContext.IsDeploymentInProgress = true
				}
			}
		}

		// 2.2 Check for any App changes(Ex. A new App source, a new App added/updated)
		if createOrUpdateAppSrcDeploymentInfo(&appSrcDeploymentInfo, s3Response.Objects) {
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

// createOrUpdateAppSrcDeploymentInfo  modifies the App deployment status as perceived from the remote object listing
func createOrUpdateAppSrcDeploymentInfo(appSrcDeploymentInfo *enterprisev1.AppSrcDeployInfo, remoteS3ObjList []*splclient.RemoteObject) bool {
	scopedLog := log.WithName("createOrUpdateAppSrcDeploymentInfo").WithValues("Called with length: ", len(remoteS3ObjList))

	var found bool
	var appName string
	var newAppInfoList []enterprisev1.AppDeploymentInfo
	var appChangesDetected bool
	var appDeployInfo enterprisev1.AppDeploymentInfo

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
		for _, appDeployInfo = range appSrcDeploymentInfo.AppDeploymentInfoList {
			if appDeployInfo.AppName == appName {
				found = true
				if appDeployInfo.ObjectHash != *remoteObj.Etag || appDeployInfo.RepoState == enterprisev1.RepoStateDeleted {
					scopedLog.Info("App change detected.", "App name: ", appName, "marking for an update")
					appDeployInfo.ObjectHash = *remoteObj.Etag
					appDeployInfo.DeployStatus = enterprisev1.DeployStatusPending

					// Make the state active for an app that was deleted earlier, and got activated again
					if appDeployInfo.RepoState == enterprisev1.RepoStateDeleted {
						scopedLog.Info("App change", "enabling the App name: ", appName, "that was previously disabled/deleted")
						appDeployInfo.RepoState = enterprisev1.RepoStateActive
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
			appDeployInfo.RepoState = enterprisev1.RepoStateActive
			appDeployInfo.DeployStatus = enterprisev1.DeployStatusPending
			appChangesDetected = true

			// Add it to a seperate list so that we don't loop through the newly added entries
			newAppInfoList = append(newAppInfoList, appDeployInfo)
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
func markAppsStatusToComplete(appSrcDeplymentStatus map[string]enterprisev1.AppSrcDeployInfo) error {
	var err error
	scopedLog := log.WithName("markAppsStatusToComplete")

	// ToDo: sgontla: Passing appSrcDeplymentStatus is redundant, but this function will go away in phase-3, so ok for now.
	for appSrc := range appSrcDeplymentStatus {
		changeAppSrcDeployInfoStatus(appSrc, appSrcDeplymentStatus, enterprisev1.RepoStateActive, enterprisev1.DeployStatusInProgress, enterprisev1.DeployStatusComplete)
		changeAppSrcDeployInfoStatus(appSrc, appSrcDeplymentStatus, enterprisev1.RepoStateDeleted, enterprisev1.DeployStatusInProgress, enterprisev1.DeployStatusComplete)
	}

	scopedLog.Info("Marked the App deployment status to complete")
	// ToDo: sgontla: Caller of this API also needs to set "IsDeploymentInProgress = false" once after completing this function call for all the app sources

	return err
}
