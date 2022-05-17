// Copyright (c) 2018-2022 Splunk Inc. All rights reserved.

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
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	enterpriseApi "github.com/splunk/splunk-operator/api/v3"
	splclient "github.com/splunk/splunk-operator/pkg/splunk/client"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	splctrl "github.com/splunk/splunk-operator/pkg/splunk/controller"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"

	// Used to move files between pods
	_ "unsafe"
)

// kubernetes logger used by splunk.enterprise package
//var log = logf.Log.WithName("splunk.enterprise")

var operatorResourceTracker *globalResourceTracker = nil

// initialize operator level context
func init() {
	fmt.Printf("init is called here\n")
	defer fmt.Printf("init ends here\n")
	initGlobalResourceTracker()
}

// initGlobalResourceTracker initializes globalResourceTracker
func initGlobalResourceTracker() {
	operatorResourceTracker = &globalResourceTracker{}

	// initialize the storage tracker
	initStorageTracker()

	// initialize the resource tracker
	initCommonResourceTracker()
}

func initCommonResourceTracker() {
	operatorResourceTracker.commonResourceTracker = &commonResourceTracker{
		mutexMap: make(map[string]*sync.Mutex),
	}
}

// getResourceMutex returns the mutex for the given K8s object
func getResourceMutex(resourceName string) *sync.Mutex {
	commonResourceTracker := operatorResourceTracker.commonResourceTracker

	commonResourceTracker.mutex.Lock()
	defer commonResourceTracker.mutex.Unlock()

	if _, ok := commonResourceTracker.mutexMap[resourceName]; !ok {
		var mutex sync.Mutex
		commonResourceTracker.mutexMap[resourceName] = &mutex
	}
	return commonResourceTracker.mutexMap[resourceName]
}

func initStorageTracker() error {
	ctx := context.TODO()
	// For now, App framework is the only functionality using the storage space tracker
	availableDiskSpace, err := getAvailableDiskSpace(ctx)
	if err != nil {
		return err
	}

	operatorResourceTracker.storage = &storageTracker{
		availableDiskSpace: availableDiskSpace,
	}

	return err
}

// updateStorageTracker updates the storage tracker with the latest disk info
func updateStorageTracker(ctx context.Context) error {
	if !isPersistantVolConfigured() {
		return fmt.Errorf("operator resource tracker not initialized")

	}

	availableDiskSpace, err := getAvailableDiskSpace(ctx)
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
func GetRemoteStorageClient(ctx context.Context, client splcommon.ControllerClient, cr splcommon.MetaObject, appFrameworkRef *enterpriseApi.AppFrameworkSpec, vol *enterpriseApi.VolumeSpec, location string, fn splclient.GetInitFunc) (splclient.SplunkS3Client, error) {

	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("GetRemoteStorageClient").WithValues("name", cr.GetName(), "namespace", cr.GetNamespace())

	s3Client := splclient.SplunkS3Client{}
	//use the provider name to get the corresponding function pointer
	getClientWrapper := splclient.S3Clients[vol.Provider]
	getClient := getClientWrapper.GetS3ClientFuncPtr(ctx)

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
		s3ClientSecret, err := splutil.GetSecretByName(ctx, client, cr, appSecretRef)
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

	s3Client.Client, err = getClient(ctx, bucket, accessKeyID, secretAccessKey, prefix, prefix /* startAfter*/, vol.Region, vol.Endpoint, fn)

	if err != nil {
		scopedLog.Error(err, "Failed to get the S3 client")
		return s3Client, err
	}

	return s3Client, nil
}

// ApplySplunkConfig reconciles the state of Kubernetes Secrets, ConfigMaps and other general settings for Splunk Enterprise instances.
func ApplySplunkConfig(ctx context.Context, client splcommon.ControllerClient, cr splcommon.MetaObject, spec enterpriseApi.CommonSplunkSpec, instanceType InstanceType) (*corev1.Secret, error) {
	var err error

	// Creates/updates the namespace scoped "splunk-secrets" K8S secret object
	namespaceScopedSecret, err := splutil.ApplyNamespaceScopedSecretObject(ctx, client, cr.GetNamespace())
	if err != nil {
		return nil, err
	}

	// Set secret owner references
	err = splutil.SetSecretOwnerRef(ctx, client, namespaceScopedSecret.GetName(), cr)
	if err != nil {
		return nil, err
	}

	// create splunk defaults (for inline config)
	if spec.Defaults != "" {
		defaultsMap := getSplunkDefaults(cr.GetName(), cr.GetNamespace(), instanceType, spec.Defaults)
		defaultsMap.SetOwnerReferences(append(defaultsMap.GetOwnerReferences(), splcommon.AsOwner(cr, true)))
		_, err = splctrl.ApplyConfigMap(ctx, client, defaultsMap)
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
func getLicenseManagerURL(ctx context.Context, cr splcommon.MetaObject, spec *enterpriseApi.CommonSplunkSpec) []corev1.EnvVar {
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
func GetSmartstoreRemoteVolumeSecrets(ctx context.Context, volume enterpriseApi.VolumeSpec, client splcommon.ControllerClient, cr splcommon.MetaObject, smartstore *enterpriseApi.SmartStoreSpec) (string, string, string, error) {
	namespaceScopedSecret, err := splutil.GetSecretByName(ctx, client, cr, volume.SecretRef)
	if err != nil {
		return "", "", "", err
	}

	accessKey := string(namespaceScopedSecret.Data[s3AccessKey])
	secretKey := string(namespaceScopedSecret.Data[s3SecretKey])

	splutil.SetSecretOwnerRef(ctx, client, volume.SecretRef, cr)

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
func getLocalAppFileName(ctx context.Context, downloadPath, appName, etag string) string {
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
func extractAppNameFromKey(ctx context.Context, key string) string {
	nameAt := strings.LastIndex(key, "/")
	return key[nameAt+1:]
}

// getRemoteObjectFromS3Response returns the remote object for the app from S3Response
func getRemoteObjectFromS3Response(ctx context.Context, appName string, s3Response splclient.S3Response) *splclient.RemoteObject {
	for _, object := range s3Response.Objects {
		rcvdAppName := extractAppNameFromKey(ctx, *object.Key)
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
func bundlePushStateAsStr(ctx context.Context, state enterpriseApi.BundlePushStageType) string {
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
func setBundlePushState(ctx context.Context, afwPipeline *AppInstallPipeline, state enterpriseApi.BundlePushStageType) {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("setBundlePushState")

	scopedLog.Info("Setting the bundle push state", "old state", bundlePushStateAsStr(ctx, afwPipeline.appDeployContext.BundlePushStatus.BundlePushStage), "new state", bundlePushStateAsStr(ctx, state))
	afwPipeline.appDeployContext.BundlePushStatus.BundlePushStage = state
}

// getBundlePushState returns the current bundle push state
func getBundlePushState(afwPipeline *AppInstallPipeline) enterpriseApi.BundlePushStageType {
	return afwPipeline.appDeployContext.BundlePushStatus.BundlePushStage
}

// createAppDownloadDir creates the app download directory on the operator pod
func createAppDownloadDir(ctx context.Context, path string) error {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("createAppDownloadDir").WithValues("path", path)
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
func getAvailableDiskSpace(ctx context.Context) (uint64, error) {
	var availDiskSpace uint64
	var stat syscall.Statfs_t
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("getAvailableDiskSpace").WithValues("volume mount", splcommon.AppDownloadVolume)

	err := syscall.Statfs(splcommon.AppDownloadVolume, &stat)
	if err != nil {
		scopedLog.Error(err, fmt.Sprintf("There is no volume configured for the App framework, use the temporary location: %s", TmpAppDownloadDir))
		splcommon.AppDownloadVolume = TmpAppDownloadDir
		err = os.MkdirAll(splcommon.AppDownloadVolume, 0755)
		if err != nil {
			scopedLog.Error(err, fmt.Sprintf("Unable to create the directory %s", splcommon.AppDownloadVolume))
			return 0, err
		}
	}

	err = syscall.Statfs(splcommon.AppDownloadVolume, &stat)
	if err != nil {
		return 0, err
	}

	availDiskSpace = stat.Bavail * uint64(stat.Bsize)
	scopedLog.Info("current available disk space in GB", "availableDiskSpace(GB)", availDiskSpace/1024/1024/1024)

	return availDiskSpace, err
}

// getRemoteObjectKey gets the remote object key
func getRemoteObjectKey(ctx context.Context, cr splcommon.MetaObject, appFrameworkConfig *enterpriseApi.AppFrameworkSpec, appSrcName, appName string) (string, error) {
	var remoteObjectKey string
	var vol enterpriseApi.VolumeSpec

	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("getRemoteObjectKey").WithValues("crName", cr.GetName(), "namespace", cr.GetNamespace(), "appSrcName", appSrcName, "appName", appName)

	appSrc, err := getAppSrcSpec(appFrameworkConfig.AppSources, appSrcName)
	if err != nil {
		scopedLog.Error(err, "unable to get appSrcSpc")
		return remoteObjectKey, err
	}

	vol, err = splclient.GetAppSrcVolume(ctx, *appSrc, appFrameworkConfig)
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
func getS3ClientMgr(ctx context.Context, client splcommon.ControllerClient, cr splcommon.MetaObject, appFrameworkConfig *enterpriseApi.AppFrameworkSpec, appSrcName string) (*S3ClientManager, error) {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("getS3ClientMgr").WithValues("crName", cr.GetName(), "namespace", cr.GetNamespace(), "appSrcName", appSrcName)
	var vol enterpriseApi.VolumeSpec
	appSrc, err := getAppSrcSpec(appFrameworkConfig.AppSources, appSrcName)
	if err != nil {
		scopedLog.Error(err, "unable to get appSrcSpc")
		return nil, err
	}

	vol, err = splclient.GetAppSrcVolume(ctx, *appSrc, appFrameworkConfig)
	if err != nil {
		scopedLog.Error(err, "unable to get volume spec")
		return nil, err
	}

	s3ClientWrapper := splclient.S3Clients[vol.Provider]
	initFunc := s3ClientWrapper.GetS3ClientInitFuncPtr(ctx)
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

// getAppPackageLocalDir returns the Operator volume directory for a given app package
func getAppPackageLocalDir(cr splcommon.MetaObject, scope string, appSrcName string) string {
	return filepath.Join(splcommon.AppDownloadVolume, "downloadedApps", cr.GetNamespace(), cr.GroupVersionKind().Kind, cr.GetName(), scope, appSrcName) + "/"
}

// getAppPackageName returns the app package name
func getAppPackageName(worker *PipelineWorker) string {
	return worker.appDeployInfo.AppName + "_" + strings.Trim(worker.appDeployInfo.ObjectHash, "\"")
}

// getAppPackageLocalPath returns the app package path on Operator pod
func getAppPackageLocalPath(ctx context.Context, worker *PipelineWorker) string {
	if worker == nil {
		return ""
	}
	appSrcScope := getAppSrcScope(ctx, worker.afwConfig, worker.appSrcName)

	return getAppPackageLocalDir(worker.cr, appSrcScope, worker.appSrcName) + getAppPackageName(worker)

}

// ApplySmartstoreConfigMap creates the configMap with Smartstore config in INI format
func ApplySmartstoreConfigMap(ctx context.Context, client splcommon.ControllerClient, cr splcommon.MetaObject,
	smartstore *enterpriseApi.SmartStoreSpec) (*corev1.ConfigMap, bool, error) {

	var crKind string
	var configMapDataChanged bool
	crKind = cr.GetObjectKind().GroupVersionKind().Kind

	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("ApplySmartStoreConfigMap").WithValues("kind", crKind, "name", cr.GetName(), "namespace", cr.GetNamespace())

	// 1. Prepare the indexes.conf entries
	mapSplunkConfDetails := make(map[string]string)

	// Get the list of volumes in INI format
	volumesConfIni, err := GetSmartstoreVolumesConfig(ctx, client, cr, smartstore, mapSplunkConfDetails)
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

	// if existing configmap contains key conftoken then add that back
	namespacedName := types.NamespacedName{Namespace: cr.GetNamespace(), Name: configMapName}
	configMap, err := splctrl.GetConfigMap(ctx, client, namespacedName)
	if err == nil && configMap != nil && configMap.Data != nil && reflect.ValueOf(configMap.Data).Kind() == reflect.Map {
		if _, ok := configMap.Data[configToken]; ok {
			SplunkOperatorAppConfigMap.Data[configToken] = configMap.Data[configToken]
		}
	}

	configMapDataChanged, err = splctrl.ApplyConfigMap(ctx, client, SplunkOperatorAppConfigMap)
	if err != nil {
		scopedLog.Error(err, "config map create/update failed", "error", err.Error())
		return nil, configMapDataChanged, err
	} else if configMapDataChanged {
		// Create a token to check if the config is really populated to the pod
		SplunkOperatorAppConfigMap.Data[configToken] = fmt.Sprintf(`%d`, time.Now().Unix())

		// this is tricky call, I have seen update fail here  with error": "Operation cannot be fulfilled on configmaps
		// the object has been modified; please apply your changes to the latest version and try again"
		// now the problem here is if configmap data has changed we need to update configtoken, only way we can do that
		// is try at least few times before failing, I took random number of 10 times to try
		// FIXME ideally retryCnt should come from global const
		// Apply the configMap with a fresh token
		retryCnt := 10
		for i := 0; i < retryCnt; i++ {
			configMapDataChanged, err = splctrl.ApplyConfigMap(ctx, client, SplunkOperatorAppConfigMap)
			if (err != nil && !k8serrors.IsConflict(err)) || err == nil {
				break
			}
		}
		if err != nil {
			scopedLog.Error(err, "config map update failed", "error", err.Error())
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
func DeleteOwnerReferencesForResources(ctx context.Context, client splcommon.ControllerClient, cr splcommon.MetaObject, smartstore *enterpriseApi.SmartStoreSpec) error {
	var err error
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("DeleteOwnerReferencesForResources").WithValues("kind", cr.GetObjectKind().GroupVersionKind().Kind, "name", cr.GetName(), "namespace", cr.GetNamespace())

	if smartstore != nil {
		_ = DeleteOwnerReferencesForS3SecretObjects(ctx, client, cr, smartstore)
	}

	// Delete references to Default secret object
	defaultSecretName := splcommon.GetNamespaceScopedSecretName(cr.GetNamespace())
	_, err = splutil.RemoveSecretOwnerRef(ctx, client, defaultSecretName, cr)
	if err != nil {
		scopedLog.Error(err, fmt.Sprintf("Owner reference removal failed for Secret Object %s", defaultSecretName))
		return err
	}

	return nil
}

// DeleteOwnerReferencesForS3SecretObjects deletes owner references for all the secret objects referred by smartstore
// remote volume end points
func DeleteOwnerReferencesForS3SecretObjects(ctx context.Context, client splcommon.ControllerClient, cr splcommon.MetaObject, smartstore *enterpriseApi.SmartStoreSpec) error {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("DeleteOwnerReferencesForS3Secrets").WithValues("kind", cr.GetObjectKind().GroupVersionKind().Kind, "name", cr.GetName(), "namespace", cr.GetNamespace())

	var err error = nil
	if !isSmartstoreConfigured(smartstore) {
		return err
	}

	volList := smartstore.VolList
	for _, volume := range volList {
		_, err = splutil.RemoveSecretOwnerRef(ctx, client, volume.SecretRef, cr)
		if err == nil {
			scopedLog.Info("Success", "Removed references for Secret Object %s", volume.SecretRef)
		} else {
			scopedLog.Error(err, fmt.Sprintf("Owner reference removal failed for Secret Object %s", volume.SecretRef))
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
	getS3Client     func(ctx context.Context, client splcommon.ControllerClient, cr splcommon.MetaObject,
		appFrameworkRef *enterpriseApi.AppFrameworkSpec, vol *enterpriseApi.VolumeSpec,
		location string, fp splclient.GetInitFunc) (splclient.SplunkS3Client, error)
}

// GetAppsList gets the apps list
func (s3mgr *S3ClientManager) GetAppsList(ctx context.Context) (splclient.S3Response, error) {
	var s3Response splclient.S3Response

	c, err := s3mgr.getS3Client(ctx, s3mgr.client, s3mgr.cr, s3mgr.appFrameworkRef, s3mgr.vol, s3mgr.location, s3mgr.initFn)
	if err != nil {
		return s3Response, err
	}

	s3Response, err = c.Client.GetAppsList(ctx)
	if err != nil {
		return s3Response, err
	}
	return s3Response, nil
}

// DownloadApp downloads the app from remote storage
func (s3mgr *S3ClientManager) DownloadApp(ctx context.Context, remoteFile string, localFile string, etag string) error {

	c, err := s3mgr.getS3Client(ctx, s3mgr.client, s3mgr.cr, s3mgr.appFrameworkRef, s3mgr.vol, s3mgr.location, s3mgr.initFn)
	if err != nil {
		return err
	}

	_, err = c.Client.DownloadApp(ctx, remoteFile, localFile, etag)
	if err != nil {
		return err
	}
	return err
}

// GetAppsList this func pointer is to use this function in unit test cases
var GetAppsList = func(ctx context.Context, s3ClientMgr S3ClientManager) (splclient.S3Response, error) {
	s3Response, err := s3ClientMgr.GetAppsList(ctx)
	return s3Response, err
}

// GetAppListFromS3Bucket gets the list of apps from remote storage.
func GetAppListFromS3Bucket(ctx context.Context, client splcommon.ControllerClient, cr splcommon.MetaObject, appFrameworkRef *enterpriseApi.AppFrameworkSpec) (map[string]splclient.S3Response, error) {

	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("GetAppListFromS3Bucket").WithValues("name", cr.GetName(), "namespace", cr.GetNamespace())

	sourceToAppListMap := make(map[string]splclient.S3Response)

	scopedLog.Info("Getting the list of apps from remote storage...")

	var s3Response splclient.S3Response
	var vol enterpriseApi.VolumeSpec
	var err error
	var allSuccess bool = true

	for _, appSource := range appFrameworkRef.AppSources {
		vol, err = splclient.GetAppSrcVolume(ctx, appSource, appFrameworkRef)
		if err != nil {
			allSuccess = false
			continue
		}

		s3ClientWrapper := splclient.S3Clients[vol.Provider]
		initFunc := s3ClientWrapper.GetS3ClientInitFuncPtr(ctx)
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
		s3Response, err = GetAppsList(ctx, s3ClientMgr)
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

// updateAuxPhaseInfo updates the AuxPhaseInfo
func updateAuxPhaseInfo(appDeployInfo *enterpriseApi.AppDeploymentInfo, desiredReplicas int32) {

	auxPhaseInfoLen := len(appDeployInfo.AuxPhaseInfo)

	for i := auxPhaseInfoLen; i < int(desiredReplicas); i++ {
		phaseInfo := enterpriseApi.PhaseInfo{
			Phase:     enterpriseApi.PhasePodCopy,
			Status:    enterpriseApi.AppPkgPodCopyPending,
			FailCount: 0,
		}
		appDeployInfo.AuxPhaseInfo = append(appDeployInfo.AuxPhaseInfo, phaseInfo)
	}
}

// changePhaseInfo changes PhaseInfo and AuxPhaseInfo for each app to desired state
func changePhaseInfo(ctx context.Context, desiredReplicas int32, appSrc string, appSrcDeployStatus map[string]enterpriseApi.AppSrcDeployInfo) {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("changePhaseInfo")

	if appSrcDeploymentInfo, ok := appSrcDeployStatus[appSrc]; ok {
		appDeployInfoList := appSrcDeploymentInfo.AppDeploymentInfoList
		for idx := range appDeployInfoList {
			// no need to do anything if app is deleted already
			if appDeployInfoList[idx].RepoState == enterpriseApi.RepoStateDeleted {
				continue
			}

			// set the phase to download
			appDeployInfoList[idx].PhaseInfo.Phase = enterpriseApi.PhaseDownload

			// set the status to download pending
			appDeployInfoList[idx].PhaseInfo.Status = enterpriseApi.AppPkgDownloadPending

			if len(appDeployInfoList[idx].AuxPhaseInfo) != 0 {
				// update the aux phase info
				updateAuxPhaseInfo(&appDeployInfoList[idx], desiredReplicas)
			}
		}
	} else {
		// Ideally this should never happen, check if the "IsDeploymentInProgress" flag is handled correctly or not
		scopedLog.Error(nil, "Could not find the App Source in App context")
	}
}

func removeStaleEntriesFromAuxPhaseInfo(ctx context.Context, desiredReplicas int32, appSrc string, appSrcDeployStatus map[string]enterpriseApi.AppSrcDeployInfo) {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("changePhaseInfo")

	if appSrcDeploymentInfo, ok := appSrcDeployStatus[appSrc]; ok {
		appDeployInfoList := appSrcDeploymentInfo.AppDeploymentInfoList
		for idx := range appDeployInfoList {
			auxPhaseInfoLen := len(appDeployInfoList[idx].AuxPhaseInfo)
			if auxPhaseInfoLen != 0 && auxPhaseInfoLen > int(desiredReplicas) {
				// update the aux phase info
				appDeployInfoList[idx].AuxPhaseInfo = appDeployInfoList[idx].AuxPhaseInfo[:desiredReplicas]
			}
		}
	} else {
		// Ideally this should never happen, check if the "IsDeploymentInProgress" flag is handled correctly or not
		scopedLog.Error(nil, "Could not find the App Source in App context")
	}

}

// changeAppSrcDeployInfoStatus sets the new status to all the apps in an AppSrc if the given repo state and deploy status matches
// primarly used in Phase-3
func changeAppSrcDeployInfoStatus(ctx context.Context, appSrc string, appSrcDeployStatus map[string]enterpriseApi.AppSrcDeployInfo, repoState enterpriseApi.AppRepoState, oldDeployStatus enterpriseApi.AppDeploymentStatus, newDeployStatus enterpriseApi.AppDeploymentStatus) {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("changeAppSrcDeployInfoStatus").WithValues("Called for AppSource: ", appSrc, "repoState", repoState, "oldDeployStatus", oldDeployStatus, "newDeployStatus", newDeployStatus)

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
func handleAppRepoChanges(ctx context.Context, client splcommon.ControllerClient, cr splcommon.MetaObject,
	appDeployContext *enterpriseApi.AppDeploymentContext, remoteObjListingMap map[string]splclient.S3Response, appFrameworkConfig *enterpriseApi.AppFrameworkSpec) (bool, error) {
	crKind := cr.GetObjectKind().GroupVersionKind().Kind
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("handleAppRepoChanges").WithValues("kind", crKind, "name", cr.GetName(), "namespace", cr.GetNamespace())
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
					setStateAndStatusForAppDeployInfo(&currentList[appIdx], enterpriseApi.RepoStateDeleted, enterpriseApi.DeployStatusComplete)
				}
			}
		}

		// 2.2 Check for any App changes(Ex. A new App source, a new App added/updated)
		appsModified = AddOrUpdateAppSrcDeploymentInfoList(ctx, &appSrcDeploymentInfo, s3Response.Objects)
		scope := getAppSrcScope(ctx, appFrameworkConfig, appSrc)
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
func AddOrUpdateAppSrcDeploymentInfoList(ctx context.Context, appSrcDeploymentInfo *enterpriseApi.AppSrcDeployInfo, remoteS3ObjList []*splclient.RemoteObject) bool {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("AddOrUpdateAppSrcDeploymentInfoList").WithValues("Called with length: ", len(remoteS3ObjList))

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
					appList[idx].PhaseInfo.FailCount = 0
					appList[idx].AuxPhaseInfo = nil

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

			// Add it to a separate list so that we don't loop through the newly added entries
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
func markAppsStatusToComplete(ctx context.Context, client splcommon.ControllerClient, cr splcommon.MetaObject, appConf *enterpriseApi.AppFrameworkSpec, appSrcDeploymentStatus map[string]enterpriseApi.AppSrcDeployInfo) error {
	var err error
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("markAppsStatusToComplete")

	// ToDo: Passing appSrcDeploymentStatus is redundant, but this function will go away in phase-3, so ok for now.
	for appSrc := range appSrcDeploymentStatus {
		changeAppSrcDeployInfoStatus(ctx, appSrc, appSrcDeploymentStatus, enterpriseApi.RepoStateActive, enterpriseApi.DeployStatusPending, enterpriseApi.DeployStatusComplete)
		changeAppSrcDeployInfoStatus(ctx, appSrc, appSrcDeploymentStatus, enterpriseApi.RepoStateDeleted, enterpriseApi.DeployStatusPending, enterpriseApi.DeployStatusComplete)
	}

	scopedLog.Info("Marked the App deployment status to complete")
	// ToDo: Caller of this API also needs to set "IsDeploymentInProgress = false" once after completing this function call for all the app sources

	return err
}

// setupAppsStagingVolume creates the necessary volume on the Splunk pods, for the operator to copy all app packages in the appSources configured and make them locally available to the Splunk instance.
func setupAppsStagingVolume(ctx context.Context, client splcommon.ControllerClient, cr splcommon.MetaObject, podTemplateSpec *corev1.PodTemplateSpec, appFrameworkConfig *enterpriseApi.AppFrameworkSpec) {

	// Create shared volume and init containers for App Framework
	if len(appFrameworkConfig.AppSources) > 0 {
		// Create volume to on Splunk container to contain apps copied from Splunk Operator pod
		emptyVolumeSource := corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{},
		}

		initVol := corev1.Volume{
			Name:         appVolumeMntName,
			VolumeSource: emptyVolumeSource,
		}

		podTemplateSpec.Spec.Volumes = append(podTemplateSpec.Spec.Volumes, initVol)

		// Add apps staging mount to Splunk container
		initVolumeSpec := corev1.VolumeMount{
			Name:      appVolumeMntName,
			MountPath: fmt.Sprintf("/%s/", appVolumeMntName),
		}

		// This assumes the Splunk instance container is Containers[0], which I *believe* is valid
		podTemplateSpec.Spec.Containers[0].VolumeMounts = append(podTemplateSpec.Spec.Containers[0].VolumeMounts, initVolumeSpec)
	}
}

// isAppAlreadyDownloaded checks if the app is already present on the operator pod
func isAppAlreadyDownloaded(ctx context.Context, downloadWorker *PipelineWorker) bool {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("isAppAlreadyDownloaded").WithValues("app name", downloadWorker.appDeployInfo.AppName)

	scope := getAppSrcScope(ctx, downloadWorker.afwConfig, downloadWorker.appSrcName)
	kind := downloadWorker.cr.GetObjectKind().GroupVersionKind().Kind

	localPath := filepath.Join(splcommon.AppDownloadVolume, "downloadedApps", downloadWorker.cr.GetNamespace(), kind, downloadWorker.cr.GetName(), scope, downloadWorker.appSrcName) + "/"
	localAppFileName := getLocalAppFileName(ctx, localPath, downloadWorker.appDeployInfo.AppName, downloadWorker.appDeployInfo.ObjectHash)

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
func SetLastAppInfoCheckTime(ctx context.Context, appInfoStatus *enterpriseApi.AppDeploymentContext) {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("SetLastAppInfoCheckTime")
	currentEpoch := time.Now().Unix()

	scopedLog.Info("Setting the LastAppInfoCheckTime to current time", "current epoch time", currentEpoch)

	appInfoStatus.LastAppInfoCheckTime = currentEpoch
}

// HasAppRepoCheckTimerExpired checks if the polling interval has expired
func HasAppRepoCheckTimerExpired(ctx context.Context, appInfoContext *enterpriseApi.AppDeploymentContext) bool {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("HasAppRepoCheckTimerExpired")
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
func GetNextRequeueTime(ctx context.Context, appRepoPollInterval, lastCheckTime int64) time.Duration {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("GetNextRequeueTime")
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

func shouldCheckAppRepoStatus(ctx context.Context, client splcommon.ControllerClient, cr splcommon.MetaObject, appStatusContext *enterpriseApi.AppDeploymentContext, kind string, turnOffManualChecking *bool) bool {
	// If polling is disabled, check if manual update is on.
	if !isAppRepoPollingEnabled(appStatusContext) {
		configMapName := GetSplunkManualAppUpdateConfigMapName(cr.GetNamespace())

		// Check if we need to manually check for app updates for this CR kind
		if getManualUpdateStatus(ctx, client, cr, configMapName) == "on" {
			// There can be more than 1 CRs of this kind. We should only
			// turn off the status once all the CRs have finished the reconciles
			if getManualUpdateRefCount(ctx, client, cr, configMapName) == 1 {
				*turnOffManualChecking = true
			}
			return true
		}
	} else {
		return HasAppRepoCheckTimerExpired(ctx, appStatusContext)
	}
	return false
}

// getCleanObjectDigest returns only hexa-decimal portion of a string
// Ex. '\"b38a8f911e2b43982b71a979fe1d3c3f\"' is converted to b38a8f911e2b43982b71a979fe1d3c3f
func getCleanObjectDigest(rawObjectDigest *string) (*string, error) {
	// S3: In the case of multipart upload, '-' is an allowed character as part of the etag
	reg, err := regexp.Compile("[^A-Fa-f0-9\\-]+")
	if err != nil {
		return nil, err
	}

	cleanObjectHash := reg.ReplaceAllString(*rawObjectDigest, "")
	return &cleanObjectHash, nil
}

// updateManualAppUpdateConfigMapLocked updates the manual app update config map
func updateManualAppUpdateConfigMapLocked(ctx context.Context, client splcommon.ControllerClient, cr splcommon.MetaObject, appStatusContext *enterpriseApi.AppDeploymentContext, kind string, turnOffManualChecking bool) error {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("updateManualAppUpdateConfigMap").WithValues("name", cr.GetName(), "namespace", cr.GetNamespace())
	var status string

	configMapName := GetSplunkManualAppUpdateConfigMapName(cr.GetNamespace())
	namespacedName := types.NamespacedName{Namespace: cr.GetNamespace(), Name: configMapName}

	mux := getResourceMutex(configMapName)
	mux.Lock()
	defer mux.Unlock()
	configMap, err := splctrl.GetConfigMap(ctx, client, namespacedName)
	if err != nil {
		scopedLog.Error(err, "Unable to get configMap", "name", namespacedName.Name)
		return err
	}

	numOfObjects := getManualUpdateRefCount(ctx, client, cr, configMapName)

	// turn off the manual checking for this CR kind in the configMap
	if turnOffManualChecking {
		scopedLog.Info("Turning off manual checking of apps update", "Kind", kind)
		// reset the status back to "off" and
		// refCount to original count
		status = "off"
		numOfObjects = getNumOfOwnerRefsKind(configMap, kind)
	} else {
		//just decrement the refCount if the status is "on"
		status = getManualUpdateStatus(ctx, client, cr, configMapName)
		if status == "on" {
			numOfObjects--
		}
	}

	// prepare the configMapData
	configMapData := fmt.Sprintf(`status: %s
refCount: %d`, status, numOfObjects)

	configMap.Data[kind] = configMapData

	err = splutil.UpdateResource(ctx, client, configMap)
	if err != nil {
		scopedLog.Error(err, "Could not update the configMap", "name", namespacedName.Name)
		return err
	}
	return nil
}

// initAndCheckAppInfoStatus initializes the S3Clients and checks the status of apps on remote storage.
func initAndCheckAppInfoStatus(ctx context.Context, client splcommon.ControllerClient, cr splcommon.MetaObject,
	appFrameworkConf *enterpriseApi.AppFrameworkSpec, appStatusContext *enterpriseApi.AppDeploymentContext) error {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("initAndCheckAppInfoStatus").WithValues("name", cr.GetName(), "namespace", cr.GetNamespace())

	var err error
	// Register the S3 clients specific to providers if not done already
	// This is done to prevent the null pointer dereference in case when
	// operator crashes and comes back up and the status of app context was updated
	// to match the spec in the previous run.
	err = initAppFrameWorkContext(ctx, client, cr, appFrameworkConf, appStatusContext)
	if err != nil {
		scopedLog.Error(err, "Unable initialize app framework")
		return err
	}

	var turnOffManualChecking bool
	kind := cr.GetObjectKind().GroupVersionKind().Kind

	//check if the apps need to be downloaded from remote storage
	if shouldCheckAppRepoStatus(ctx, client, cr, appStatusContext, kind, &turnOffManualChecking) || !reflect.DeepEqual(appStatusContext.AppFrameworkConfig, *appFrameworkConf) {

		if appStatusContext.IsDeploymentInProgress {
			scopedLog.Info("App installation is already in progress. Not checking for any latest app repo changes")
			return nil
		}

		appStatusContext.IsDeploymentInProgress = true
		var sourceToAppsList map[string]splclient.S3Response

		scopedLog.Info("Checking status of apps on remote storage...")

		sourceToAppsList, err = GetAppListFromS3Bucket(ctx, client, cr, appFrameworkConf)
		// TODO: gaurav, we need to handle this case better in Phase-3. There can be a possibility
		// where if an appSource is missing in remote store, we mark it for deletion. But if it comes up
		// next time, we will recycle the pod to install the app. We need to find a way to reduce the pod recycles.
		if len(sourceToAppsList) != len(appFrameworkConf.AppSources) {
			scopedLog.Error(err, "Unable to get apps list, will retry in next reconcile...")
		} else {
			for _, appSource := range appFrameworkConf.AppSources {
				// Clean-up for the object digest value
				for i := range sourceToAppsList[appSource.Name].Objects {
					cleanDigest, err := getCleanObjectDigest(sourceToAppsList[appSource.Name].Objects[i].Etag)
					if err != nil {
						scopedLog.Error(err, "unable to fetch clean object digest value", "Object Hash", sourceToAppsList[appSource.Name].Objects[i].Etag)
						return err
					}

					sourceToAppsList[appSource.Name].Objects[i].Etag = cleanDigest
				}

				scopedLog.Info("Apps List retrieved from remote storage", "App Source", appSource.Name, "Content", sourceToAppsList[appSource.Name].Objects)
			}

			// Only handle the app repo changes if we were able to successfully get the apps list
			_, err = handleAppRepoChanges(ctx, client, cr, appStatusContext, sourceToAppsList, appFrameworkConf)
			if err != nil {
				scopedLog.Error(err, "Unable to use the App list retrieved from the remote storage")
				return err
			}

			appStatusContext.AppFrameworkConfig = *appFrameworkConf
		}

		// Set the last check time, irrespective of the polling type. This way, it is easy to switch
		// in between the manual and automatic polling
		SetLastAppInfoCheckTime(ctx, appStatusContext)

		if !isAppRepoPollingEnabled(appStatusContext) {
			err = updateManualAppUpdateConfigMapLocked(ctx, client, cr, appStatusContext, kind, turnOffManualChecking)
			if err != nil {
				scopedLog.Error(err, "failed to update the manual app udpate configMap")
				return err
			}
		}
	}

	return nil
}

// SetConfigMapOwnerRef sets the owner references for the configMap
func SetConfigMapOwnerRef(ctx context.Context, client splcommon.ControllerClient, cr splcommon.MetaObject, configMap *corev1.ConfigMap) error {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("SetConfigMapOwnerRef").WithValues("name", cr.GetName(), "namespace", cr.GetNamespace())

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
	err := splutil.UpdateResource(ctx, client, configMap)
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

// UpdateOrRemoveEntryFromConfigMapLocked removes/updates the entry for the CR type from the manual app update configMap
func UpdateOrRemoveEntryFromConfigMapLocked(ctx context.Context, c splcommon.ControllerClient, cr splcommon.MetaObject, instanceType InstanceType) error {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("UpdateOrRemoveEntryFromConfigMapLocked").WithValues("name", cr.GetName(), "namespace", cr.GetNamespace())

	configMapName := GetSplunkManualAppUpdateConfigMapName(cr.GetNamespace())
	namespacedName := types.NamespacedName{Namespace: cr.GetNamespace(), Name: configMapName}

	mux := getResourceMutex(configMapName)
	mux.Lock()
	defer mux.Unlock()
	configMap, err := splctrl.GetConfigMap(ctx, c, namespacedName)
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
refCount: %d`, getManualUpdateStatus(ctx, c, cr, configMapName), numOfObjects)

		configMap.Data[kind] = configMapData
	}

	// Update configMap now
	err = splutil.UpdateResource(ctx, c, configMap)
	if err != nil {
		scopedLog.Error(err, "Unable to update configMap", "name", namespacedName.Name)
		return err
	}

	return nil
}

// RemoveConfigMapOwnerRef removes the owner references for the configMap
func RemoveConfigMapOwnerRef(ctx context.Context, client splcommon.ControllerClient, cr splcommon.MetaObject, configMapName string) (uint, error) {
	var err error
	var refCount uint = 0

	namespacedName := types.NamespacedName{Namespace: cr.GetNamespace(), Name: configMapName}
	configMap, err := splctrl.GetConfigMap(ctx, client, namespacedName)
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
		err = splutil.UpdateResource(ctx, client, configMap)
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
func checkIfFileExistsOnPod(ctx context.Context, cr splcommon.MetaObject, filePath string, podExecClient splutil.PodExecClientImpl) bool {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("checkIfFileExistsOnPod").WithValues("podName", podExecClient.GetTargetPodName(), "namespace", cr.GetNamespace()).WithValues("filePath", filePath)
	// Make sure the destination directory is existing
	fPath := path.Clean(filePath)
	command := fmt.Sprintf("test -f %s; echo -n $?", fPath)
	streamOptions := splutil.NewStreamOptionsObject(command)

	stdOut, stdErr, err := podExecClient.RunPodExecCommand(ctx, streamOptions, []string{"/bin/sh"})
	if stdErr != "" || err != nil {
		scopedLog.Error(err, "error in checking the file availability on the Pod", "stdErr", stdErr, "stdOut", stdOut, "filePath", fPath)
		return false
	}

	fileTestResult, _ := strconv.Atoi(stdOut)
	return fileTestResult == 0
}

// createDirOnSplunkPods creates the required directory for the pod/s
func createDirOnSplunkPods(ctx context.Context, cr splcommon.MetaObject, replicas int32, path string, podExecClient splutil.PodExecClientImpl) error {
	var err error
	var stdOut, stdErr string

	command := fmt.Sprintf("mkdir -p %s", path)
	streamOptions := splutil.NewStreamOptionsObject(command)
	// create the directory on each replica pod
	for replicaIndex := 0; replicaIndex < int(replicas); replicaIndex++ {
		// get the target pod name
		podName := getApplicablePodNameForAppFramework(cr, replicaIndex)
		podExecClient.SetTargetPodName(ctx, podName)

		// CSPL-1639: reset the Stdin so that reader pipe can read from the correct offset of the string reader.
		// This is particularly needed in the cases where we are trying to run the same command across multiple pods
		// and we need to clear the reader pipe so that we can read the read buffer from the correct offset again.
		splutil.ResetStringReader(streamOptions, command)

		// Throw an error if we are not able to create the destination directory where we wish to copy the app package
		stdOut, stdErr, err = podExecClient.RunPodExecCommand(ctx, streamOptions, []string{"/bin/sh"})
		if stdErr != "" || err != nil {
			err = fmt.Errorf("unable to create directory on Pod at path=%s. stdout: %s, stdErr: %s, err: %s", path, stdOut, stdErr, err)
			break
		}
	}
	return err
}

// CopyFileToPod copies a file from Operator Pod to any given Pod of a custom resource
func CopyFileToPod(ctx context.Context, c splcommon.ControllerClient, namespace string, srcPath string, destPath string, podExecClient splutil.PodExecClientImpl) (string, string, error) {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("CopyFileToPod").WithValues("podName", podExecClient.GetTargetPodName(), "namespace", namespace).WithValues("srcPath", srcPath, "destPath", destPath)

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

	streamOptions := splutil.NewStreamOptionsObject(command)

	// If the Pod directory doesn't exist, do not try to create it. Instead throw an error
	// Otherwise, in case of invalid dest path, we may end up creating too many invalid directories/files
	stdOut, stdErr, err := podExecClient.RunPodExecCommand(ctx, streamOptions, []string{"/bin/sh"})
	dirTestResult, _ := strconv.Atoi(stdOut)
	if dirTestResult != 0 {
		return stdOut, stdErr, fmt.Errorf("directory on Pod doesn't exist. stdout: %s, stdErr: %s, err: %s", stdOut, stdErr, err)
	}

	go func() {
		defer writer.Close()
		err := cpMakeTar(localPath{file: srcPath}, remotePath{file: destPath}, writer)
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

	streamOptions.Stdin = reader

	return podExecClient.RunPodExecCommand(ctx, streamOptions, cmdArr)
}

//go:linkname cpMakeTar k8s.io/kubernetes/pkg/kubectl/cmd/cp.makeTar
//func cpMakeTar(srcPath, destPath string, writer io.Writer) error

//validateMonitoringConsoleRef validates the changes in monitoringConsoleRef
func validateMonitoringConsoleRef(ctx context.Context, c splcommon.ControllerClient, revised *appsv1.StatefulSet, serviceURLs []corev1.EnvVar) error {
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
			_, err = ApplyMonitoringConsoleEnvConfigMap(ctx, c, current.ObjectMeta.GetNamespace(), current.ObjectMeta.GetName(), cEnv.Value, serviceURLs, false)
			if err != nil {
				return err
			}
			_, err = ApplyMonitoringConsoleEnvConfigMap(ctx, c, current.ObjectMeta.GetNamespace(), current.ObjectMeta.GetName(), rEnv.Value, serviceURLs, true)
			if err != nil {
				return err
			}
		} else if cEnv.Value != "" && rEnv.Value == "" {
			//2. if revised Spec doesn't have mcRef defined
			_, err = ApplyMonitoringConsoleEnvConfigMap(ctx, c, current.ObjectMeta.GetNamespace(), current.ObjectMeta.GetName(), cEnv.Value, serviceURLs, false)
			if err != nil {
				return err
			}
		}
	}
	//if the sts doesn't exists no need for any change
	return nil
}

// setInstallStateForClusterScopedApps sets the install state for cluster scoped apps
func setInstallStateForClusterScopedApps(ctx context.Context, appDeployContext *enterpriseApi.AppDeploymentContext) {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("setInstallStateForClusterScopedApps")

	for appSrcName, appSrcDeployInfo := range appDeployContext.AppsSrcDeployStatus {
		// Mark only cluster scoped apps
		if enterpriseApi.ScopeCluster != getAppSrcScope(ctx, &appDeployContext.AppFrameworkConfig, appSrcName) {
			continue
		}

		deployInfoList := appSrcDeployInfo.AppDeploymentInfoList
		for i := range deployInfoList {
			if deployInfoList[i].PhaseInfo.Phase == enterpriseApi.PhasePodCopy && deployInfoList[i].PhaseInfo.Status == enterpriseApi.AppPkgPodCopyComplete {
				deployInfoList[i].PhaseInfo.Phase = enterpriseApi.PhaseInstall
				deployInfoList[i].PhaseInfo.Status = enterpriseApi.AppPkgInstallComplete
				scopedLog.Info("Cluster scoped app installed", "app name", deployInfoList[i].AppName, "digest", deployInfoList[i].ObjectHash)
			} else if deployInfoList[i].PhaseInfo.Phase != enterpriseApi.PhaseInstall || deployInfoList[i].PhaseInfo.Status != enterpriseApi.AppPkgInstallComplete {
				scopedLog.Error(nil, "app missing from bundle push", "app name", deployInfoList[i].AppName, "digest", deployInfoList[i].ObjectHash, "phase", deployInfoList[i].PhaseInfo.Phase, "status", deployInfoList[i].PhaseInfo.Status)
			}
		}
	}
}

// getAdminPasswordFromSecret retrieves the admin password from secret object
func getAdminPasswordFromSecret(ctx context.Context, client splcommon.ControllerClient, cr splcommon.MetaObject) ([]byte, error) {
	// get the admin password from the namespace scoped secret
	defaultSecretObjName := splcommon.GetNamespaceScopedSecretName(cr.GetNamespace())
	defaultSecret, err := splutil.GetSecretByName(ctx, client, cr, defaultSecretObjName)
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

// updateReconcileRequeueTime updates the reconcile requeue result
func updateReconcileRequeueTime(ctx context.Context, result *reconcile.Result, rqTime time.Duration, requeue bool) {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("updateReconcileRequeueTime")
	if result == nil {
		scopedLog.Error(nil, "invalid result")
		return
	}
	if rqTime <= 0 {
		scopedLog.Error(nil, fmt.Sprintf("invalid requeue time: %d", rqTime))
		return
	}

	result.Requeue = requeue

	// updated the requested, if it is lower than the one in hand
	if rqTime < result.RequeueAfter {
		result.RequeueAfter = rqTime
	}
}

// handleAppFrameworkActivity handles any pending app framework activity
func handleAppFrameworkActivity(ctx context.Context, client splcommon.ControllerClient, cr splcommon.MetaObject, appDeployContext *enterpriseApi.AppDeploymentContext, appFrameworkConfig *enterpriseApi.AppFrameworkSpec) *reconcile.Result {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("afwSchedulerEntry").WithValues("name", cr.GetName(), "namespace", cr.GetNamespace())
	finalResult := &reconcile.Result{
		Requeue:      false,
		RequeueAfter: maxRecDuration,
	}

	// Consider the polling interval for next reconcile
	if isAppRepoPollingEnabled(appDeployContext) {
		requeueAfter := GetNextRequeueTime(ctx, appDeployContext.AppsRepoStatusPollInterval, appDeployContext.LastAppInfoCheckTime)
		updateReconcileRequeueTime(ctx, finalResult, requeueAfter, true)
	}

	if appDeployContext.AppsSrcDeployStatus != nil {
		requeue, err := afwSchedulerEntry(ctx, client, cr, appDeployContext, appFrameworkConfig)
		if err != nil {
			scopedLog.Error(err, "app framework returned error")
		}
		if requeue {
			updateReconcileRequeueTime(ctx, finalResult, time.Second*5, true)
		}
	}

	return finalResult
}

// checkAndMigrateAppDeployStatus (if required) upgrades the appframework status context
func checkAndMigrateAppDeployStatus(ctx context.Context, client splcommon.ControllerClient, cr splcommon.MetaObject, afwStatusContext *enterpriseApi.AppDeploymentContext, afwConf *enterpriseApi.AppFrameworkSpec, isLocalScope bool) error {
	// If needed, Migrate the app framework status
	if isAppFrameworkMigrationNeeded(afwStatusContext) {
		// Spec validation updates the status with some of the defaults, which may not be there in older app framework versions
		err := ValidateAppFrameworkSpec(ctx, afwConf, afwStatusContext, isLocalScope)
		if err != nil {
			return err
		}

		if !migrateAfwStatus(ctx, client, cr, afwStatusContext) {
			return fmt.Errorf("app framework migration failed")
		}
	}

	return nil
}

// migrateAfwStatus migrates the appframework status context to the latest version
func migrateAfwStatus(ctx context.Context, client splcommon.ControllerClient, cr splcommon.MetaObject, afwStatusContext *enterpriseApi.AppDeploymentContext) bool {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("migrateAfwStatus").WithValues("name", cr.GetName(), "namespace", cr.GetNamespace())

	// Upgrade one version at a time
	// Start with the lowest version, then move towards the latest
	for afwStatusContext.Version < enterpriseApi.LatestAfwVersion {
		switch {
		// Always start with the lowest version
		case afwStatusContext.Version < enterpriseApi.AfwPhase3:
			scopedLog.Info("Migrating the App framework", "old version", afwStatusContext.Version, "new version", enterpriseApi.AfwPhase3)
			err := migrateAfwFromPhase2ToPhase3(ctx, client, cr, afwStatusContext)
			if err != nil {
				return false
			}

			// case: Add the higher versions below
		}
	}

	// Update the new status
	err := client.Status().Update(context.TODO(), cr)
	if err != nil {
		scopedLog.Error(err, "status update failed")
	}

	return err == nil
}

// migrateAfwFromPhase2ToPhase3 migrates app framework status from Phase-2 to Phase-3
func migrateAfwFromPhase2ToPhase3(ctx context.Context, client splcommon.ControllerClient, cr splcommon.MetaObject, afwStatusContext *enterpriseApi.AppDeploymentContext) error {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("migrateAfwFromPhase2ToPhase3").WithValues("name", cr.GetName(), "namespace", cr.GetNamespace())

	sts := afwGetReleventStatefulsetByKind(ctx, cr, client)

	for _, appSrcDeployInfo := range afwStatusContext.AppsSrcDeployStatus {
		deployInfoList := appSrcDeployInfo.AppDeploymentInfoList
		for i := range deployInfoList {
			// Remove the special characters from Object hash
			cleanDigest, err := getCleanObjectDigest(&deployInfoList[i].ObjectHash)
			if err != nil {
				scopedLog.Error(err, "clean-up failed", "digest", deployInfoList[i].ObjectHash)
				return err
			}
			deployInfoList[i].ObjectHash = *cleanDigest

			// If the app is already deleted, do not bother about the previous install state.
			if deployInfoList[i].RepoState != enterpriseApi.RepoStateActive {
				continue
			}

			// Set the PhaseInfo. Also, set the Aux Phase info, if applicable
			if deployInfoList[i].DeployStatus == enterpriseApi.DeployStatusComplete {
				deployInfoList[i].PhaseInfo.Phase = enterpriseApi.PhaseInstall
				deployInfoList[i].PhaseInfo.Status = enterpriseApi.AppPkgInstallComplete

				// Initialize the Aux Phase info with the install status
				if *sts.Spec.Replicas > 1 {
					deployInfoList[i].AuxPhaseInfo = make([]enterpriseApi.PhaseInfo, *sts.Spec.Replicas)
					for auxIdx := range deployInfoList[i].AuxPhaseInfo {
						deployInfoList[i].AuxPhaseInfo[auxIdx] = deployInfoList[i].PhaseInfo
					}
				}
			} else {
				deployInfoList[i].PhaseInfo.Phase = enterpriseApi.PhaseDownload
				deployInfoList[i].PhaseInfo.Status = enterpriseApi.AppPkgDownloadPending
			}
		}
	}

	afwStatusContext.Version = enterpriseApi.AfwPhase3
	scopedLog.Info("migration completed")
	return nil
}

// isAppFrameworkMigrationNeeded confirms if the app framework version migration is needed
func isAppFrameworkMigrationNeeded(afwStatusContext *enterpriseApi.AppDeploymentContext) bool {
	return afwStatusContext != nil && afwStatusContext.Version < currentAfwVersion && len(afwStatusContext.AppsSrcDeployStatus) > 0
}
