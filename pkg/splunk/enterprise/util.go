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
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/remotecommand"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	enterpriseApi "github.com/splunk/splunk-operator/pkg/apis/enterprise/v3"
	splclient "github.com/splunk/splunk-operator/pkg/splunk/client"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	splctrl "github.com/splunk/splunk-operator/pkg/splunk/controller"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"

	// Used to move files between pods
	_ "unsafe"

	// Import kubectl cmd cp utils
	_ "k8s.io/kubernetes/pkg/kubectl/cmd/cp"
)

// kubernetes logger used by splunk.enterprise package
var log = logf.Log.WithName("splunk.enterprise")

var operatorResourceTracker *globalResourceTracker = nil

// initialize operator level context
func init() {
	operatorResourceTracker = &globalResourceTracker{}

	// initialize the storage tracker
	initStorageTracker()
}

func initStorageTracker() error {
	// For now, App framework is the only functionality using the storage space tracker
	availableDiskSpace, err := getAvailableDiskSpace()
	if err != nil {
		return err
	}

	operatorResourceTracker.storage = &storageTracker{
		availableDiskSpace: availableDiskSpace,
	}

	return err
}

// updateStorageTracker updates the storage tracker with the latest disk info
func updateStorageTracker() error {
	if !isPersistantVolConfigured() {
		return fmt.Errorf("operator resource tracker not initialized")

	}

	availableDiskSpace, err := getAvailableDiskSpace()
	if err != nil {
		return err
	}

	return func() error {
		operatorResourceTracker.storage.mutex.Lock()
		defer operatorResourceTracker.storage.mutex.Unlock()

		operatorResourceTracker.storage.availableDiskSpace = availableDiskSpace
		return err
	}()
}

// GetRemoteStorageClient returns the corresponding S3Client
func GetRemoteStorageClient(client splcommon.ControllerClient, cr splcommon.MetaObject, appFrameworkRef *enterpriseApi.AppFrameworkSpec, vol *enterpriseApi.VolumeSpec, location string, fn splclient.GetInitFunc) (splclient.SplunkS3Client, error) {

	scopedLog := log.WithName("GetRemoteStorageClient").WithValues("name", cr.GetName(), "namespace", cr.GetNamespace())

	s3Client := splclient.SplunkS3Client{}
	//use the provider name to get the corresponding function pointer
	getClientWrapper := splclient.S3Clients[vol.Provider]
	getClient := getClientWrapper.GetS3ClientFuncPtr()

	appSecretRef := vol.SecretRef
	var accessKeyID string
	var secretAccessKey string
	if appSecretRef == "" {
		// No secretRef means we should try to use the credentials available in the pod already via kube2iam or something similar
		scopedLog.Info("No secrectRef provided.  Attempt to access remote storage client without access/secret keys")
		accessKeyID = ""
		secretAccessKey = ""
	} else {
		// Get credentials through the secretRef
		s3ClientSecret, err := splutil.GetSecretByName(client, cr, appSecretRef)
		if err != nil {
			return s3Client, err
		}

		// Get access keys
		accessKeyID = string(s3ClientSecret.Data["s3_access_key"])
		secretAccessKey = string(s3ClientSecret.Data["s3_secret_key"])

		// Do we need to handle if IAM_ROLE is set in the secret as well?
		if accessKeyID == "" {
			err = fmt.Errorf("accessKey missing")
			return s3Client, err
		}
		if secretAccessKey == "" {
			err = fmt.Errorf("s3 Secret Key is missing")
			return s3Client, err
		}
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

	var err error
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
			Value: GetSplunkServiceName(SplunkClusterManager, cr.GetName(), false),
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

// getLicenseManagerURL returns URL of license manager
func getLicenseManagerURL(cr splcommon.MetaObject, spec *enterpriseApi.CommonSplunkSpec) []corev1.EnvVar {
	if spec.LicenseMasterRef.Name != "" {
		licenseManagerURL := GetSplunkServiceName(SplunkLicenseManager, spec.LicenseMasterRef.Name, false)
		if spec.LicenseMasterRef.Namespace != "" {
			licenseManagerURL = splcommon.GetServiceFQDN(spec.LicenseMasterRef.Namespace, licenseManagerURL)
		}
		return []corev1.EnvVar{
			{
				Name:  "SPLUNK_LICENSE_MASTER_URL",
				Value: licenseManagerURL,
			},
		}
	}
	return []corev1.EnvVar{
		{
			Name:  "SPLUNK_LICENSE_MASTER_URL",
			Value: GetSplunkServiceName(SplunkLicenseManager, cr.GetName(), false),
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
		return "", "", "", fmt.Errorf("s3 Access Key is missing")
	} else if secretKey == "" {
		return "", "", "", fmt.Errorf("s3 Secret Key is missing")
	}

	return accessKey, secretKey, namespaceScopedSecret.ResourceVersion, nil
}

// getLocalAppFileName generates the local app file name
// For e.g., if the app package name is sample_app.tgz
// and etag is "abcd1234", then it will be downloaded locally as sample_app.tgz_abcd1234
func getLocalAppFileName(downloadPath, appName, etag string) string {
	return downloadPath + appName + "_" + strings.Trim(etag, "\"")
}

// getObjectsAsPointers converts and returns a slice of pointers to objects.
// For e.g., if we have a slice of ints as []int, then this API will return []*int
func getObjectsAsPointers(v interface{}) interface{} {
	in := reflect.ValueOf(v)
	out := reflect.MakeSlice(reflect.SliceOf(reflect.PtrTo(in.Type().Elem())), in.Len(), in.Len())
	for i := 0; i < in.Len(); i++ {
		out.Index(i).Set(in.Index(i).Addr())
	}
	return out.Interface()
}

// extractAppNameFromKey extracts the app name from Key received from remote storage
func extractAppNameFromKey(key string) string {
	nameAt := strings.LastIndex(key, "/")
	return key[nameAt+1:]
}

// getRemoteObjectFromS3Response returns the remote object for the app from S3Response
func getRemoteObjectFromS3Response(appName string, s3Response splclient.S3Response) *splclient.RemoteObject {
	for _, object := range s3Response.Objects {
		rcvdAppName := extractAppNameFromKey(*object.Key)
		if rcvdAppName == appName {
			return object
		}
	}
	return nil
}

// appPhaseStatusAsStr converts the state enum to corresponding string
func appPhaseStatusAsStr(status enterpriseApi.AppPhaseStatusType) string {
	switch status {
	case enterpriseApi.AppPkgDownloadPending:
		return "Download Pending"
	case enterpriseApi.AppPkgDownloadInProgress:
		return "Download In Progress"
	case enterpriseApi.AppPkgDownloadComplete:
		return "Download Complete"
	case enterpriseApi.AppPkgDownloadError:
		return "Download Error"
	case enterpriseApi.AppPkgPodCopyPending:
		return "Pod Copy Pending"
	case enterpriseApi.AppPkgPodCopyInProgress:
		return "Pod Copy In Progress"
	case enterpriseApi.AppPkgPodCopyComplete:
		return "Pod Copy Complete"
	case enterpriseApi.AppPkgPodCopyError:
		return "Pod Copy Error"
	case enterpriseApi.AppPkgInstallPending:
		return "Install Pending"
	case enterpriseApi.AppPkgInstallInProgress:
		return "Install In Progress"
	case enterpriseApi.AppPkgInstallComplete:
		return "Install Complete"
	case enterpriseApi.AppPkgInstallError:
		return "Install Error"
	default:
		return "Invalid Status"
	}
}

// bundlePushStateAsStr converts the bundle push state enum to corresponding string
func bundlePushStateAsStr(state enterpriseApi.BundlePushStageType) string {
	switch state {
	case enterpriseApi.BundlePushPending:
		return "Bundle Push Pending"
	case enterpriseApi.BundlePushInProgress:
		return "Bundle Push In Progress"
	case enterpriseApi.BundlePushComplete:
		return "Bundle Push Complete"
	default:
		return "Invalid bundle push state"
	}
}

// setBundlePushState sets the bundle push state to the new state
func setBundlePushState(afwPipeline *AppInstallPipeline, state enterpriseApi.BundlePushStageType) {
	scopedLog := log.WithName("setBundlePushState")

	scopedLog.Info("Setting the bundle push state", "old state", bundlePushStateAsStr(afwPipeline.appDeployContext.BundlePushStatus.BundlePushStage), "new state", bundlePushStateAsStr(state))
	afwPipeline.appDeployContext.BundlePushStatus.BundlePushStage = state
}

// getBundlePushState returns the current bundle push state
func getBundlePushState(afwPipeline *AppInstallPipeline) enterpriseApi.BundlePushStageType {
	return afwPipeline.appDeployContext.BundlePushStatus.BundlePushStage
}

// createAppDownloadDir creates the app download directory on the operator pod
func createAppDownloadDir(path string) error {
	scopedLog := log.WithName("createAppDownloadDir").WithValues("path", path)
	_, err := os.Stat(path)
	if os.IsNotExist(err) {
		errDir := os.MkdirAll(path, 0755)
		if errDir != nil {
			scopedLog.Error(errDir, "Unable to create directory at path")
			return errDir
		}
	}
	return nil
}

// getAvailableDiskSpace returns the disk space available to download apps at volume "/opt/splunk/appframework"
func getAvailableDiskSpace() (uint64, error) {
	var availDiskSpace uint64
	var stat syscall.Statfs_t
	scopedLog := log.WithName("getAvailableDiskSpace").WithValues("volume mount", splcommon.AppDownloadVolume)

	err := syscall.Statfs(splcommon.AppDownloadVolume, &stat)
	if err != nil {
		scopedLog.Error(err, "unable to get the info about disk space for volume")
	} else {
		availDiskSpace = stat.Bavail * uint64(stat.Bsize)
		scopedLog.Info("current available disk space in GB", "availableDiskSpace(GB)", availDiskSpace/1024/1024/1024)
	}

	return availDiskSpace, err
}

// getRemoteObjectKey gets the remote object key
func getRemoteObjectKey(cr splcommon.MetaObject, appFrameworkConfig *enterpriseApi.AppFrameworkSpec, appSrcName, appName string) (string, error) {
	var remoteObjectKey string
	var vol enterpriseApi.VolumeSpec

	scopedLog := log.WithName("getRemoteObjectKey").WithValues("crName", cr.GetName(), "namespace", cr.GetNamespace(), "appSrcName", appSrcName, "appName", appName)

	appSrc, err := getAppSrcSpec(appFrameworkConfig.AppSources, appSrcName)
	if err != nil {
		scopedLog.Error(err, "unable to get appSrcSpc")
		return remoteObjectKey, err
	}

	vol, err = splclient.GetAppSrcVolume(*appSrc, appFrameworkConfig)
	if err != nil {
		scopedLog.Error(err, "unable to get volume spec")
		return remoteObjectKey, err
	}

	volumePath := vol.Path
	index := strings.Index(volumePath, "/")
	// CSPL-1528: If volume path only contains the bucket name,
	// then don't append the bucket name to the remote key
	if index < 0 {
		volumePath = ""
	} else {
		volumePath = volumePath[index+1:]
	}
	location := appSrc.Location

	remoteObjectKey = filepath.Join(volumePath, location, appName)

	return remoteObjectKey, nil
}

// getS3ClientMgr gets the S3ClientMgr instance to download apps
func getS3ClientMgr(client splcommon.ControllerClient, cr splcommon.MetaObject, appFrameworkConfig *enterpriseApi.AppFrameworkSpec, appSrcName string) (*S3ClientManager, error) {
	scopedLog := log.WithName("getS3ClientMgr").WithValues("crName", cr.GetName(), "namespace", cr.GetNamespace(), "appSrcName", appSrcName)
	var vol enterpriseApi.VolumeSpec
	appSrc, err := getAppSrcSpec(appFrameworkConfig.AppSources, appSrcName)
	if err != nil {
		scopedLog.Error(err, "unable to get appSrcSpc")
		return nil, err
	}

	vol, err = splclient.GetAppSrcVolume(*appSrc, appFrameworkConfig)
	if err != nil {
		scopedLog.Error(err, "unable to get volume spec")
		return nil, err
	}

	s3ClientWrapper := splclient.S3Clients[vol.Provider]
	initFunc := s3ClientWrapper.GetS3ClientInitFuncPtr()
	s3ClientMgr := &S3ClientManager{
		client:          client,
		cr:              cr,
		appFrameworkRef: appFrameworkConfig,
		vol:             &vol,
		location:        appSrc.Location,
		initFn:          initFunc,
		getS3Client:     GetRemoteStorageClient,
	}
	return s3ClientMgr, nil
}

// ApplyAppListingConfigMap creates the configMap  with two entries:
// (1) app-list-local.yaml
// (2) app-list-cluster.yaml
// Once the configMap is mounted on the Pod, Ansible handles the apps listed in these files
// ToDo: Deletes to be handled for phase-3
func ApplyAppListingConfigMap(client splcommon.ControllerClient, cr splcommon.MetaObject,
	appConf *enterpriseApi.AppFrameworkSpec, appsSrcDeployStatus map[string]enterpriseApi.AppSrcDeployInfo, appsModified bool) (*corev1.ConfigMap, bool, error) {

	var err error
	var crKind string
	var configMapDataChanged bool
	crKind = cr.GetObjectKind().GroupVersionKind().Kind

	scopedLog := log.WithName("ApplyAppListingConfigMap").WithValues("kind", crKind, "name", cr.GetName(), "namespace", cr.GetNamespace())

	mapAppListing := make(map[string]string)

	// Locally scoped apps for CM/Deployer require the latest splunk-ansible with apps_location_local.  Prior to this,
	// there was no method to install local apps for these roles.  If the apps_location_local variable is not available,
	// it will be ignored and revert back to no locally scoped apps for CM/Deployer.
	yamlConfIdcHeader := fmt.Sprintf(`splunk:
  app_paths_install:
    idxc:`)

	yamlConfShcHeader := fmt.Sprintf(`splunk:
  app_paths_install:
    shc:`)

	yamlConfLocalHeader := fmt.Sprintf(`splunk:
  app_paths_install:
    default:`)

	// Used for apps requiring pre-configuration before installing on the cluster
	// Example: Splunk Enterprise Security App
	yamlClusterAppsWithPreConfHeader := fmt.Sprintf(`splunk:
  apps_location:`)

	var localAppsConf, clusterAppsConf string
	if crKind == "ClusterMaster" {
		clusterAppsConf = yamlConfIdcHeader
	} else if crKind == "SearchHeadCluster" {
		clusterAppsConf = yamlConfShcHeader
	} else {
		clusterAppsConf = ""
	}

	localAppsConf = yamlConfLocalHeader
	clusterAppsWithPreConf := yamlClusterAppsWithPreConfHeader

	var mapKeys []string

	// Map order is not guaranteed, so use the sorted keys to go through the map entries
	for appSrc := range appsSrcDeployStatus {
		mapKeys = append(mapKeys, appSrc)
	}
	sort.Strings(mapKeys)

	for _, appSrc := range mapKeys {
		appDeployList := appsSrcDeployStatus[appSrc].AppDeploymentInfoList

		switch scope := getAppSrcScope(appConf, appSrc); scope {
		case enterpriseApi.ScopeLocal:
			for idx := range appDeployList {
				if appDeployList[idx].DeployStatus == enterpriseApi.DeployStatusPending &&
					appDeployList[idx].RepoState == enterpriseApi.RepoStateActive {
					localAppsConf = fmt.Sprintf(`%s
      - "/init-apps/%s/%s"`, localAppsConf, appSrc, appDeployList[idx].AppName)
				}
			}

		case enterpriseApi.ScopeCluster:
			for idx := range appDeployList {
				if appDeployList[idx].DeployStatus == enterpriseApi.DeployStatusPending &&
					appDeployList[idx].RepoState == enterpriseApi.RepoStateActive {
					clusterAppsConf = fmt.Sprintf(`%s
      - "/init-apps/%s/%s"`, clusterAppsConf, appSrc, appDeployList[idx].AppName)
				}
			}

		case enterpriseApi.ScopeClusterWithPreConfig:
			for idx := range appDeployList {
				if appDeployList[idx].DeployStatus == enterpriseApi.DeployStatusPending &&
					appDeployList[idx].RepoState == enterpriseApi.RepoStateActive {
					clusterAppsWithPreConf = fmt.Sprintf(`%s
      - "/init-apps/%s/%s"`, clusterAppsWithPreConf, appSrc, appDeployList[idx].AppName)
				}
			}

		default:
			scopedLog.Error(nil, "Invalid scope detected")
		}
	}

	// Don't update the configMap if there is nothing to write.
	if localAppsConf != yamlConfLocalHeader {
		mapAppListing["app-list-local.yaml"] = localAppsConf
	}

	if clusterAppsConf != yamlConfIdcHeader && clusterAppsConf != yamlConfShcHeader && clusterAppsConf != "" {
		mapAppListing["app-list-cluster.yaml"] = clusterAppsConf
	}

	if clusterAppsWithPreConf != yamlClusterAppsWithPreConfHeader {
		mapAppListing["app-list-cluster-with-pre-config.yaml"] = clusterAppsWithPreConf
	}
	// Create App list config map
	configMapName := GetSplunkAppsConfigMapName(cr.GetName(), crKind)
	appListingConfigMap := splctrl.PrepareConfigMap(configMapName, cr.GetNamespace(), mapAppListing)

	appListingConfigMap.SetOwnerReferences(append(appListingConfigMap.GetOwnerReferences(), splcommon.AsOwner(cr, true)))

	if len(appListingConfigMap.Data) > 0 {
		if appsModified {
			// App packages are modified, reset configmap to ensure a new resourceVersion
			scopedLog.Info("Resetting App ConfigMap to force new resourceVersion", "configMapName", configMapName)
			savedData := appListingConfigMap.Data
			appListingConfigMap.Data = make(map[string]string)
			_, err = splctrl.ApplyConfigMap(client, appListingConfigMap)
			if err != nil {
				scopedLog.Error(err, "failed reset of configmap", "configMapName", configMapName)
			}
			appListingConfigMap.Data = savedData
		}

		configMapDataChanged, err = splctrl.ApplyConfigMap(client, appListingConfigMap)

		if err != nil {
			return nil, configMapDataChanged, err
		}
	}

	return appListingConfigMap, configMapDataChanged, nil
}

// getAppPackageLocalDir returns the Operator volume directory for a given app package
func getAppPackageLocalDir(cr splcommon.MetaObject, scope string, appSrcName string) string {
	return filepath.Join(splcommon.AppDownloadVolume, "downloadedApps", cr.GetNamespace(), cr.GroupVersionKind().Kind, cr.GetName(), scope, appSrcName) + "/"
}

// getAppPackageName returns the app package name
func getAppPackageName(worker *PipelineWorker) string {
	return worker.appDeployInfo.AppName + "_" + strings.Trim(worker.appDeployInfo.ObjectHash, "\"")
}

// getAppPackageLocalPath returns the app package path on Operator pod
func getAppPackageLocalPath(worker *PipelineWorker) string {
	if worker == nil {
		return ""
	}
	appSrcScope := getAppSrcScope(worker.afwConfig, worker.appSrcName)

	return getAppPackageLocalDir(worker.cr, appSrcScope, worker.appSrcName) + getAppPackageName(worker)
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
		return nil, configMapDataChanged, fmt.Errorf("indexes without Volume configuration is not allowed")
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
		_ = DeleteOwnerReferencesForS3SecretObjects(client, cr, smartstore)
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
	if !isSmartstoreConfigured(smartstore) {
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

// DownloadApp downloads the app from remote storage
func (s3mgr *S3ClientManager) DownloadApp(remoteFile string, localFile string, etag string) error {

	c, err := s3mgr.getS3Client(s3mgr.client, s3mgr.cr, s3mgr.appFrameworkRef, s3mgr.vol, s3mgr.location, s3mgr.initFn)
	if err != nil {
		return err
	}

	_, err = c.Client.DownloadApp(remoteFile, localFile, etag)
	if err != nil {
		return err
	}
	return err
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

	if !allSuccess {
		err = fmt.Errorf("unable to get apps list from remote storage list for all the apps")
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

func isAppRepoStateDeleted(appDeployInfo enterpriseApi.AppDeploymentInfo) bool {
	return appDeployInfo.RepoState == enterpriseApi.RepoStateDeleted
}

// handleAppRepoChanges parses the remote storage listing and updates the repoState and deployStatus accordingly
// client and cr are used when we put the glue logic to hand-off to the side car
func handleAppRepoChanges(client splcommon.ControllerClient, cr splcommon.MetaObject,
	appDeployContext *enterpriseApi.AppDeploymentContext, remoteObjListingMap map[string]splclient.S3Response, appFrameworkConfig *enterpriseApi.AppFrameworkSpec) (bool, error) {
	crKind := cr.GetObjectKind().GroupVersionKind().Kind
	scopedLog := log.WithName("handleAppRepoChanges").WithValues("kind", crKind, "name", cr.GetName(), "namespace", cr.GetNamespace())
	var err error
	appsModified := false

	scopedLog.Info("received App listing", "for App sources", len(remoteObjListingMap))
	if len(remoteObjListingMap) == 0 {
		scopedLog.Error(nil, "remoteObjectList is empty. Any apps that are already deployed will be disabled")
	}

	// Check if the appSource is still valid in the config
	for appSrc := range remoteObjListingMap {
		if !CheckIfAppSrcExistsInConfig(appFrameworkConfig, appSrc) {
			err = fmt.Errorf("app source: %s no more exists, this should never happen", appSrc)
			return appsModified, err
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
				if !isAppRepoStateDeleted(appSrcDeploymentInfo.AppDeploymentInfoList[appIdx]) && !checkIfAnAppIsActiveOnRemoteStore(currentList[appIdx].AppName, s3Response.Objects) {
					scopedLog.Info("App change", "deleting/disabling the App: ", currentList[appIdx].AppName, "as it is missing in the remote listing", nil)
					setStateAndStatusForAppDeployInfo(&currentList[appIdx], enterpriseApi.RepoStateDeleted, enterpriseApi.DeployStatusPending)
				}
			}
		}

		// 2.2 Check for any App changes(Ex. A new App source, a new App added/updated)
		appsModified = AddOrUpdateAppSrcDeploymentInfoList(&appSrcDeploymentInfo, s3Response.Objects)
		scope := getAppSrcScope(appFrameworkConfig, appSrc)
		// if some apps were modified or added, and we have cluster scoped apps,
		// then set the bundle push state to Pending
		if appsModified && scope == enterpriseApi.ScopeCluster {
			appDeployContext.BundlePushStatus.BundlePushStage = enterpriseApi.BundlePushPending
		}

		// Finally update the Map entry with latest info
		appDeployContext.AppsSrcDeployStatus[appSrc] = appSrcDeploymentInfo
	}

	return appsModified, err
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
	scopedLog := log.WithName("AddOrUpdateAppSrcDeploymentInfoList").WithValues("Called with length", len(remoteS3ObjList))

	var found bool
	var appName string
	var newAppInfoList []enterpriseApi.AppDeploymentInfo
	var appChangesDetected bool
	var appDeployInfo enterpriseApi.AppDeploymentInfo

	for _, remoteObj := range remoteS3ObjList {
		receivedKey := *remoteObj.Key
		if !isAppExtentionValid(receivedKey) {
			scopedLog.Error(nil, "App name Parsing: Ignoring the key with invalid extention", "receivedKey", receivedKey)
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
					scopedLog.Info("App change detected.  Marking for an update.", "appName", appName)
					appList[idx].ObjectHash = *remoteObj.Etag
					appList[idx].IsUpdate = true
					appList[idx].DeployStatus = enterpriseApi.DeployStatusPending
					appList[idx].PhaseInfo.Phase = enterpriseApi.PhaseDownload
					appList[idx].PhaseInfo.Status = enterpriseApi.AppPkgDownloadPending

					// Make the state active for an app that was deleted earlier, and got activated again
					if appList[idx].RepoState == enterpriseApi.RepoStateDeleted {
						scopedLog.Info("App change.  Enabling the App that was previously disabled/deleted", "appName", appName)
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
			scopedLog.Info("New App found", "appName", appName)
			appDeployInfo.AppName = appName
			appDeployInfo.ObjectHash = *remoteObj.Etag
			appDeployInfo.RepoState = enterpriseApi.RepoStateActive
			appDeployInfo.DeployStatus = enterpriseApi.DeployStatusPending
			appDeployInfo.PhaseInfo.Phase = enterpriseApi.PhaseDownload
			appDeployInfo.PhaseInfo.Status = enterpriseApi.AppPkgDownloadPending

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
func markAppsStatusToComplete(client splcommon.ControllerClient, cr splcommon.MetaObject, appConf *enterpriseApi.AppFrameworkSpec, appSrcDeploymentStatus map[string]enterpriseApi.AppSrcDeployInfo) error {
	var err error
	scopedLog := log.WithName("markAppsStatusToComplete")

	// ToDo: Passing appSrcDeploymentStatus is redundant, but this function will go away in phase-3, so ok for now.
	for appSrc := range appSrcDeploymentStatus {
		changeAppSrcDeployInfoStatus(appSrc, appSrcDeploymentStatus, enterpriseApi.RepoStateActive, enterpriseApi.DeployStatusPending, enterpriseApi.DeployStatusComplete)
		changeAppSrcDeployInfoStatus(appSrc, appSrcDeploymentStatus, enterpriseApi.RepoStateDeleted, enterpriseApi.DeployStatusPending, enterpriseApi.DeployStatusComplete)
	}

	// ToDo: For now disabling the configMap reset, as it causes unnecessary pod resets due to annotations change
	// Now that all the apps are deployed. Update the same in the App listing configMap
	// _, _, err = ApplyAppListingConfigMap(client, cr, appConf, appSrcDeploymentStatus)
	// if err != nil {
	// 	return err
	// }

	scopedLog.Info("Marked the App deployment status to complete")
	// ToDo: Caller of this API also needs to set "IsDeploymentInProgress = false" once after completing this function call for all the app sources

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

			initEnv := []corev1.EnvVar{}
			if appSecretRef != "" {
				initEnv = append(initEnv, corev1.EnvVar{
					Name: "AWS_ACCESS_KEY_ID",
					ValueFrom: &corev1.EnvVarSource{
						SecretKeyRef: &corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: appSecretRef,
							},
							Key: s3AccessKey,
						},
					},
				})
				initEnv = append(initEnv, corev1.EnvVar{
					Name: "AWS_SECRET_ACCESS_KEY",
					ValueFrom: &corev1.EnvVarSource{
						SecretKeyRef: &corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: appSecretRef,
							},
							Key: s3SecretKey,
						},
					},
				})
			}

			// Setup init container
			initContainerSpec := corev1.Container{
				Image:           s3Client.Client.GetInitContainerImage(),
				ImagePullPolicy: "IfNotPresent",
				Name:            initContainerName,
				Args:            s3Client.Client.GetInitContainerCmd(appS3Endpoint, appBkt, appSrcPath, appSrcName, appBktMnt),
				Env:             initEnv,
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

// isAppAlreadyDownloaded checks if the app is already present on the operator pod
func isAppAlreadyDownloaded(downloadWorker *PipelineWorker) bool {
	scopedLog := log.WithName("isAppAlreadyDownloaded").WithValues("app name", downloadWorker.appDeployInfo.AppName)

	scope := getAppSrcScope(downloadWorker.afwConfig, downloadWorker.appSrcName)
	kind := downloadWorker.cr.GetObjectKind().GroupVersionKind().Kind

	localPath := filepath.Join(splcommon.AppDownloadVolume, "downloadedApps", downloadWorker.cr.GetNamespace(), kind, downloadWorker.cr.GetName(), scope, downloadWorker.appSrcName) + "/"
	localAppFileName := getLocalAppFileName(localPath, downloadWorker.appDeployInfo.AppName, downloadWorker.appDeployInfo.ObjectHash)

	// check if the app is present on operator pod
	fileInfo, err := os.Stat(localAppFileName)

	if os.IsNotExist(err) {
		scopedLog.Info("App not present on operator pod")
		return false
	}

	localSize := fileInfo.Size()
	remoteSize := int64(downloadWorker.appDeployInfo.Size)
	if localSize != remoteSize {
		err = fmt.Errorf("local size does not match with size on remote storage. localSize=%d, remoteSize=%d", localSize, remoteSize)
		scopedLog.Error(err, "incorrect app size")
		return false
	}
	return true
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
	if isTimerExpired {
		scopedLog.Info("App repo polling interval timer has expired", "LastAppInfoCheckTime", strconv.FormatInt(appInfoContext.LastAppInfoCheckTime, 10), "current epoch time", strconv.FormatInt(currentEpoch, 10))
	}

	return isTimerExpired
}

// GetNextRequeueTime gets the next reconcile requeue time based on the appRepoPollInterval.
// There can be some time elapsed between when we first set lastAppInfoCheckTime and when the CR is in Ready state.
// Hence we need to subtract the delta time elapsed from the actual polling interval,
// so that the next reconcile would happen at the right time.
func GetNextRequeueTime(appRepoPollInterval, lastCheckTime int64) time.Duration {
	scopedLog := log.WithName("GetNextRequeueTime")
	currentEpoch := time.Now().Unix()

	var nextRequeueTimeInSec int64
	nextRequeueTimeInSec = appRepoPollInterval - (currentEpoch - lastCheckTime)
	if nextRequeueTimeInSec < 0 {
		nextRequeueTimeInSec = 5
	}

	scopedLog.Info("Getting next requeue time", "LastAppInfoCheckTime", lastCheckTime, "Current Epoch time", currentEpoch, "nextRequeueTimeInSec", nextRequeueTimeInSec)

	return time.Second * (time.Duration(nextRequeueTimeInSec))
}

// isAppRepoPollingEnabled checks whether automatic polling for apps repo changes
// is enabled or not. If the value is 0, then we fallback to on-demand polling of apps
// repo changes.
func isAppRepoPollingEnabled(appStatusContext *enterpriseApi.AppDeploymentContext) bool {
	return appStatusContext.AppsRepoStatusPollInterval != 0
}

func shouldCheckAppRepoStatus(client splcommon.ControllerClient, cr splcommon.MetaObject, appStatusContext *enterpriseApi.AppDeploymentContext, kind string, turnOffManualChecking *bool) bool {
	// If polling is disabled, check if manual update is on.
	if !isAppRepoPollingEnabled(appStatusContext) {
		configMapName := GetSplunkManualAppUpdateConfigMapName(cr.GetNamespace())

		// Check if we need to manually check for app updates for this CR kind
		if getManualUpdateStatus(client, cr, configMapName) == "on" {
			// There can be more than 1 CRs of this kind. We should only
			// turn off the status once all the CRs have finished the reconciles
			if getManualUpdateRefCount(client, cr, configMapName) == 1 {
				*turnOffManualChecking = true
			}
			return true
		}
	} else {
		return HasAppRepoCheckTimerExpired(appStatusContext)
	}
	return false
}

// initAndCheckAppInfoStatus initializes the S3Clients and checks the status of apps on remote storage.
func initAndCheckAppInfoStatus(client splcommon.ControllerClient, cr splcommon.MetaObject, appFrameworkConf *enterpriseApi.AppFrameworkSpec, appStatusContext *enterpriseApi.AppDeploymentContext) error {
	scopedLog := log.WithName("initAndCheckAppInfoStatus").WithValues("name", cr.GetName(), "namespace", cr.GetNamespace())

	var err error
	// Register the S3 clients specific to providers if not done already
	// This is done to prevent the null pointer dereference in case when
	// operator crashes and comes back up and the status of app context was updated
	// to match the spec in the previous run.
	err = initAppFrameWorkContext(client, cr, appFrameworkConf, appStatusContext)
	if err != nil {
		scopedLog.Error(err, "Unable initialize app framework")
		return err
	}

	var turnOffManualChecking bool
	kind := cr.GetObjectKind().GroupVersionKind().Kind

	//check if the apps need to be downloaded from remote storage
	if shouldCheckAppRepoStatus(client, cr, appStatusContext, kind, &turnOffManualChecking) || !reflect.DeepEqual(appStatusContext.AppFrameworkConfig, *appFrameworkConf) {
		if appStatusContext.IsDeploymentInProgress {
			scopedLog.Info("App installation is already in progress. Not checking for any latest app repo changes")
			return nil
		}

		appStatusContext.IsDeploymentInProgress = true
		var sourceToAppsList map[string]splclient.S3Response
		appsModified := false

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
			_, err = handleAppRepoChanges(client, cr, appStatusContext, sourceToAppsList, appFrameworkConf)
			if err != nil {
				scopedLog.Error(err, "Unable to use the App list retrieved from the remote storage")
				return err
			}

			// TODO: gaurav, this needs to be removed eventually in favor of downloading apps locally
			_, _, err = ApplyAppListingConfigMap(client, cr, appFrameworkConf, appStatusContext.AppsSrcDeployStatus, appsModified)
			if err != nil {
				return err
			}

			appStatusContext.AppFrameworkConfig = *appFrameworkConf
		}

		// set the last check time to current time only if the polling is enabled
		if isAppRepoPollingEnabled(appStatusContext) {
			SetLastAppInfoCheckTime(appStatusContext)
		} else {
			var status string
			configMapName := GetSplunkManualAppUpdateConfigMapName(cr.GetNamespace())
			namespacedName := types.NamespacedName{Namespace: cr.GetNamespace(), Name: configMapName}
			configMap, err := splctrl.GetConfigMap(client, namespacedName)
			if err != nil {
				scopedLog.Error(err, "Unable to get configMap", "name", namespacedName.Name)
				return err
			}

			// reset the LastAppInfoCheckTime to 0 so that we don't reconcile again and poll for apps status
			appStatusContext.LastAppInfoCheckTime = 0

			numOfObjects := getManualUpdateRefCount(client, cr, configMapName)

			// turn off the manual checking for this CR kind in the configMap
			if turnOffManualChecking {
				scopedLog.Info("Turning off manual checking of apps update", "Kind", kind)
				// reset the status back to "off" and
				// refCount to original count
				status = "off"
				numOfObjects = getNumOfOwnerRefsKind(configMap, kind)
			} else {
				//just decrement the refCount if the status is "on"
				status = getManualUpdateStatus(client, cr, configMapName)
				if status == "on" {
					numOfObjects--
				}
			}

			// prepare the configMapData
			configMapData := fmt.Sprintf(`status: %s
refCount: %d`, status, numOfObjects)

			configMap.Data[kind] = configMapData

			err = splutil.UpdateResource(client, configMap)
			if err != nil {
				scopedLog.Error(err, "Could not update the configMap", "name", namespacedName.Name)
				return err
			}
		}
	}

	return nil
}

// SetConfigMapOwnerRef sets the owner references for the configMap
func SetConfigMapOwnerRef(client splcommon.ControllerClient, cr splcommon.MetaObject, configMap *corev1.ConfigMap) error {
	scopedLog := log.WithName("SetConfigMapOwnerRef").WithValues("name", cr.GetName(), "namespace", cr.GetNamespace())

	currentOwnerRef := configMap.GetOwnerReferences()
	// Check if owner ref exists
	for i := 0; i < len(currentOwnerRef); i++ {
		if reflect.DeepEqual(currentOwnerRef[i], splcommon.AsOwner(cr, false)) {
			return nil
		}
	}

	// Owner ref doesn't exist, update configMap with owner references
	configMap.SetOwnerReferences(append(configMap.GetOwnerReferences(), splcommon.AsOwner(cr, false)))

	// Update the configMap now
	err := splutil.UpdateResource(client, configMap)
	if err != nil {
		scopedLog.Error(err, "Unable to update configMap", "name", configMap.Name)
		return err
	}

	return nil

}

func getNumOfOwnerRefsKind(configMap *corev1.ConfigMap, kind string) int {
	var numOfObjects int
	currentOwnerRefs := configMap.GetOwnerReferences()
	// Get the nubmer of owners of this kind
	for i := 0; i < len(currentOwnerRefs); i++ {
		if currentOwnerRefs[i].Kind == kind {
			numOfObjects++
		}
	}
	return numOfObjects
}

// UpdateOrRemoveEntryFromConfigMap removes/updates the entry for the CR type from the manual app update configMap
func UpdateOrRemoveEntryFromConfigMap(c splcommon.ControllerClient, cr splcommon.MetaObject, instanceType InstanceType) error {
	scopedLog := log.WithName("UpdateOrRemoveEntryFromConfigMap").WithValues("name", cr.GetName(), "namespace", cr.GetNamespace())

	configMapName := GetSplunkManualAppUpdateConfigMapName(cr.GetNamespace())
	namespacedName := types.NamespacedName{Namespace: cr.GetNamespace(), Name: configMapName}
	configMap, err := splctrl.GetConfigMap(c, namespacedName)
	if err != nil {
		scopedLog.Error(err, "Unable to get config map", "name", namespacedName.Name)
		return err
	}

	kind := cr.GetObjectKind().GroupVersionKind().Kind

	numOfObjects := getNumOfOwnerRefsKind(configMap, kind)
	if numOfObjects == 0 {
		err = fmt.Errorf("error getting objects for this type: %s", instanceType.ToString())
		return err
	}

	// if this is the last of its kind, remove its entry from the config map
	if numOfObjects == 1 {
		delete(configMap.Data, kind)
	} else {
		// just decrement the refCount in the configMap
		numOfObjects--

		configMapData := fmt.Sprintf(`status: %s
refCount: %d`, getManualUpdateStatus(c, cr, configMapName), numOfObjects)

		configMap.Data[kind] = configMapData
	}

	// Update configMap now
	err = splutil.UpdateResource(c, configMap)
	if err != nil {
		scopedLog.Error(err, "Unable to update configMap", "name", namespacedName.Name)
		return err
	}

	return nil
}

// RemoveConfigMapOwnerRef removes the owner references for the configMap
func RemoveConfigMapOwnerRef(client splcommon.ControllerClient, cr splcommon.MetaObject, configMapName string) (uint, error) {
	var err error
	var refCount uint = 0

	namespacedName := types.NamespacedName{Namespace: cr.GetNamespace(), Name: configMapName}
	configMap, err := splctrl.GetConfigMap(client, namespacedName)
	if err != nil {
		return 0, err
	}

	ownerRef := configMap.GetOwnerReferences()
	for i := 0; i < len(ownerRef); i++ {
		if reflect.DeepEqual(ownerRef[i], splcommon.AsOwner(cr, false)) {
			ownerRef = append(ownerRef[:i], ownerRef[i+1:]...)
			refCount++
		}
	}

	// Update the modified owner reference list
	if refCount > 0 {
		configMap.SetOwnerReferences(ownerRef)
		err = splutil.UpdateResource(client, configMap)
		if err != nil {
			return 0, err
		}
	}

	return refCount, nil
}

func extractFieldFromConfigMapData(fieldRegex, data string) string {

	var result string
	pattern := regexp.MustCompile(fieldRegex)
	if len(pattern.FindStringSubmatch(data)) > 0 {
		result = pattern.FindStringSubmatch(data)[1]
	}
	return result
}

// checkIfFileExistsOnPod confirms if the given file path exits on a given Pod
func checkIfFileExistsOnPod(c splcommon.ControllerClient, namespace string, podName string, filePath string) bool {
	scopedLog := log.WithName("checkIfFileExistsOnPod").WithValues("podName", podName, "namespace", namespace).WithValues("filePath", filePath)
	// Make sure the destination directory is existing
	fPath := path.Clean(filePath)
	command := fmt.Sprintf("test -f %s; echo -n $?", fPath)
	streamOptions := &remotecommand.StreamOptions{
		Stdin: strings.NewReader(command),
	}

	stdOut, stdErr, err := splutil.PodExecCommand(c, podName, namespace, []string{"/bin/sh"}, streamOptions, false, false)

	if stdErr != "" || err != nil {
		scopedLog.Error(err, "error in checking the file availability on the Pod", "stdErr", stdErr, "stdOut", stdOut, "filePath", fPath)
		return false
	}

	fileTestResult, _ := strconv.Atoi(stdOut)
	return fileTestResult == 0
}

// CopyFileToPod copies a file from Operator Pod to any given Pod of a custom resource
func CopyFileToPod(c splcommon.ControllerClient, namespace string, podName string, srcPath string, destPath string) (string, string, error) {
	scopedLog := log.WithName("CopyFileToPod").WithValues("podName", podName, "namespace", namespace).WithValues("srcPath", srcPath, "destPath", destPath)

	var err error
	reader, writer := io.Pipe()

	// Check if the source file path is valid
	if strings.HasSuffix(srcPath, "/") {
		return "", "", fmt.Errorf("invalid file name %s", srcPath)
	}

	// Do not accept relative path for source file path
	srcPath = path.Clean(srcPath)
	if !strings.HasPrefix(srcPath, "/") {
		return "", "", fmt.Errorf("relative paths are not supported for source path: %s", srcPath)
	}

	// Make sure that the source file exists
	_, err = os.Stat(srcPath)
	if err != nil {
		return "", "", fmt.Errorf("unable to get the info for file: %s, error: %s", srcPath, err)
	}

	// If the Pod destination path is a directory, use the source file name
	if strings.HasSuffix(destPath, "/") {
		destPath = destPath + path.Base(srcPath)
	}

	destPath = path.Clean(destPath)
	// Do not accept relative path for Pod destination path
	if !strings.HasPrefix(destPath, "/") {
		return "", "", fmt.Errorf("relative paths are not supported for dest path: %s", destPath)
	}

	// Make sure the destination directory is existing
	destDir := path.Dir(destPath)
	command := fmt.Sprintf("test -d %s; echo -n $?", destDir)
	streamOptions := &remotecommand.StreamOptions{
		Stdin: strings.NewReader(command),
	}

	// If the Pod directory doesn't exist, do not try to create it. Instead throw an error
	// Otherwise, in case of invalid dest path, we may end up creating too many invalid directories/files
	stdOut, stdErr, err := splutil.PodExecCommand(c, podName, namespace, []string{"/bin/sh"}, streamOptions, false, false)
	dirTestResult, _ := strconv.Atoi(stdOut)
	if dirTestResult != 0 {
		return stdOut, stdErr, fmt.Errorf("directory on Pod doesn't exist. stdout: %s, stdErr: %s, err: %s", stdOut, stdErr, err)
	}

	go func() {
		defer writer.Close()
		err := cpMakeTar(srcPath, destPath, writer)
		if err != nil {
			scopedLog.Error(err, "Failed to send file on writer pipe", "srcPath", srcPath, "destPath", destPath)
			return
		}
	}()
	var cmdArr []string

	// Untar the input stream on the Pod
	cmdArr = []string{"tar", "-xf", "-"}
	if len(destDir) > 0 {
		cmdArr = append(cmdArr, "-C", destDir)
	}

	streamOptions = &remotecommand.StreamOptions{
		Stdin: reader,
	}

	return splutil.PodExecCommand(c, podName, namespace, cmdArr, streamOptions, false, false)
}

//go:linkname cpMakeTar k8s.io/kubernetes/pkg/kubectl/cmd/cp.makeTar
func cpMakeTar(srcPath, destPath string, writer io.Writer) error

//validateMonitoringConsoleRef validates the changes in monitoringConsoleRef
func validateMonitoringConsoleRef(c splcommon.ControllerClient, revised *appsv1.StatefulSet, serviceURLs []corev1.EnvVar) error {
	var err error
	namespacedName := types.NamespacedName{Namespace: revised.GetNamespace(), Name: revised.GetName()}
	var current appsv1.StatefulSet

	err = c.Get(context.TODO(), namespacedName, &current)
	if err == nil {
		currEnv := current.Spec.Template.Spec.Containers[0].Env
		revEnv := revised.Spec.Template.Spec.Containers[0].Env

		var cEnv, rEnv corev1.EnvVar

		for _, cEnvTemp := range currEnv {
			if cEnvTemp.Name == "SPLUNK_MONITORING_CONSOLE_REF" {
				cEnv.Value = cEnvTemp.Value
			}
		}

		for _, rEnvTemp := range revEnv {
			if rEnvTemp.Name == "SPLUNK_MONITORING_CONSOLE_REF" {
				rEnv.Value = rEnvTemp.Value
			}
		}

		if cEnv.Value != "" && rEnv.Value != "" && cEnv.Value != rEnv.Value {
			//1. if revised Spec has different mcRef defined
			_, err = ApplyMonitoringConsoleEnvConfigMap(c, current.ObjectMeta.GetNamespace(), current.ObjectMeta.GetName(), cEnv.Value, serviceURLs, false)
			if err != nil {
				return err
			}
			_, err = ApplyMonitoringConsoleEnvConfigMap(c, current.ObjectMeta.GetNamespace(), current.ObjectMeta.GetName(), rEnv.Value, serviceURLs, true)
			if err != nil {
				return err
			}
		} else if cEnv.Value != "" && rEnv.Value == "" {
			//2. if revised Spec doesn't have mcRef defined
			_, err = ApplyMonitoringConsoleEnvConfigMap(c, current.ObjectMeta.GetNamespace(), current.ObjectMeta.GetName(), cEnv.Value, serviceURLs, false)
			if err != nil {
				return err
			}
		}
	}
	//if the sts doesn't exists no need for any change
	return nil
}

// setInstallStateForClusterScopedApps sets the install state for cluster scoped apps
func setInstallStateForClusterScopedApps(appDeployContext *enterpriseApi.AppDeploymentContext, status enterpriseApi.AppPhaseStatusType) {
	scopedLog := log.WithName("setInstallStateForClusterScopedApps")

	var isClusterScoped bool
	for appSrcName, appSrcDeployInfo := range appDeployContext.AppsSrcDeployStatus {
		appSrc, err := getAppSrcSpec(appDeployContext.AppFrameworkConfig.AppSources, appSrcName)
		if err != nil {
			// Error, should never happen
			scopedLog.Error(err, "Unable to find App src", "App src name", appSrcName)
			continue
		}
		if appSrc.Scope == enterpriseApi.ScopeCluster {
			isClusterScoped = true
		}

		deployInfoList := appSrcDeployInfo.AppDeploymentInfoList
		for i := range deployInfoList {
			if isClusterScoped {
				deployInfoList[i].PhaseInfo.Phase = enterpriseApi.PhaseInstall
				deployInfoList[i].PhaseInfo.Status = status
			}
		}
	}
}

// getAdminPasswordFromSecret retrieves the admin password from secret object
func getAdminPasswordFromSecret(client splcommon.ControllerClient, cr splcommon.MetaObject) ([]byte, error) {
	// get the admin password from the namespace scoped secret
	defaultSecretObjName := splcommon.GetNamespaceScopedSecretName(cr.GetNamespace())
	defaultSecret, err := splutil.GetSecretByName(client, cr, defaultSecretObjName)
	if err != nil {
		return nil, fmt.Errorf("could not access default secret object to fetch admin password. Reason %v", err)
	}

	//Get the admin password from the secret object
	adminPwd, foundSecret := defaultSecret.Data["password"]
	if !foundSecret {
		return nil, fmt.Errorf("could not find admin password while trying to push the manager apps bundle")
	}
	return adminPwd, nil
}

// isPersistantVolConfigured confirms if the Operator Pod is configured with storage
func isPersistantVolConfigured() bool {
	return operatorResourceTracker != nil && operatorResourceTracker.storage != nil
}

// reserveStorage tries to reserve the amount of requested storage
func reserveStorage(allocSize uint64) error {
	if !isPersistantVolConfigured() {
		return fmt.Errorf("storageTracker was not initialized")
	}

	sTracker := operatorResourceTracker.storage
	return func() error {
		sTracker.mutex.Lock()
		defer sTracker.mutex.Unlock()
		if sTracker.availableDiskSpace < allocSize {
			return fmt.Errorf("requested disk space not available. requested: %d Bytes, available: %d Bytes", allocSize, sTracker.availableDiskSpace)
		}

		sTracker.availableDiskSpace -= allocSize
		return nil
	}()
}

// releaseStorage releases the reserved storage
func releaseStorage(releaseSize uint64) error {
	if !isPersistantVolConfigured() {
		return fmt.Errorf("storageTracker was not initialized")
	}

	sTracker := operatorResourceTracker.storage
	return func() error {
		sTracker.mutex.Lock()
		defer sTracker.mutex.Unlock()

		sTracker.availableDiskSpace += releaseSize
		return nil
	}()
}
