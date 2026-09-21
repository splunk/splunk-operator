// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.

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

package appframework

import (
	"archive/tar"
	"context"
	"errors"
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

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/splunk/splunk-operator/pkg/logging"
	splstorage "github.com/splunk/splunk-operator/pkg/splunk/client/storage"
	storageaws "github.com/splunk/splunk-operator/pkg/splunk/client/storage/aws"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	"github.com/splunk/splunk-operator/pkg/splunk/k8sops"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
)

// kubernetes logger used by splunk.enterprise package
//var log = logf.Log.WithName("splunk.enterprise")

var operatorResourceTracker *globalResourceTracker = nil

// initialize operator level context
func init() {
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

// InitGlobalResourceTracker resets the shared App Framework resource tracker
// for callers that exercise the workflow from another package.
func InitGlobalResourceTracker() {
	initGlobalResourceTracker()
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
	availableDiskSpace, resolvedPath, err := getAvailableDiskSpace(ctx)
	if err != nil {
		return err
	}

	operatorResourceTracker.storage = &storageTracker{
		availableDiskSpace:        availableDiskSpace,
		resolvedAppDownloadVolume: resolvedPath,
	}

	return err
}

// updateStorageTracker updates the storage tracker with the latest disk info
func updateStorageTracker(ctx context.Context) error {
	if !isPersistentVolConfigured() {
		return fmt.Errorf("operator resource tracker not initialized")

	}

	availableDiskSpace, resolvedPath, err := getAvailableDiskSpace(ctx)
	if err != nil {
		return err
	}

	return func() error {
		operatorResourceTracker.storage.mutex.Lock()
		defer operatorResourceTracker.storage.mutex.Unlock()

		operatorResourceTracker.storage.availableDiskSpace = availableDiskSpace
		operatorResourceTracker.storage.resolvedAppDownloadVolume = resolvedPath
		return err
	}()
}

// getResolvedAppDownloadVolume returns the app download path actually in use: either
// splcommon.AppDownloadVolume, or TmpAppDownloadDir if that volume isn't mounted. Falls back
// to splcommon.AppDownloadVolume if the storage tracker hasn't been initialized yet.
func getResolvedAppDownloadVolume() string {
	if !isPersistentVolConfigured() {
		return splcommon.AppDownloadVolume
	}

	operatorResourceTracker.storage.mutex.Lock()
	defer operatorResourceTracker.storage.mutex.Unlock()
	return operatorResourceTracker.storage.resolvedAppDownloadVolume
}

// initAppFrameWorkContext initializes the app framework status and remote data clients.
func initAppFrameWorkContext(ctx context.Context, client splcommon.ControllerClient, cr splcommon.MetaObject, appFrameworkConf *enterpriseApi.AppFrameworkSpec, appStatusContext *enterpriseApi.AppDeploymentContext) error {
	if appStatusContext.AppsSrcDeployStatus == nil {
		appStatusContext.AppsSrcDeployStatus = make(map[string]enterpriseApi.AppSrcDeployInfo)
		appStatusContext.Version = enterpriseApi.LatestAfwVersion

		_, err := createOrUpdateAppUpdateConfigMap(ctx, client, cr)
		if err != nil {
			return err
		}
	}

	for _, vol := range appFrameworkConf.VolList {
		if _, ok := splstorage.RemoteDataClientsMap[vol.Provider]; !ok {
			splstorage.RegisterRemoteDataClient(ctx, vol.Provider)
		}
	}
	return nil
}

// GetRemoteStorageClient returns the corresponding RemoteDataClient
func GetRemoteStorageClient(ctx context.Context, client splcommon.ControllerClient, cr splcommon.MetaObject, appFrameworkRef *enterpriseApi.AppFrameworkSpec, vol *enterpriseApi.VolumeSpec, location string, fn splcommon.GetInitFunc) (splstorage.SplunkRemoteDataClient, error) {

	scopedLog := logging.FromContext(ctx).With("func", "GetRemoteStorageClient", "name", cr.GetName(), "namespace", cr.GetNamespace())

	// Get event publisher from context. Keep this interface-based so the workflow
	// can use publishers supplied by either the legacy or k8sops adapter.
	eventPublisher := splcommon.GetEventPublisher(ctx)

	remoteDataClient := splstorage.SplunkRemoteDataClient{}
	//use the provider name to get the corresponding function pointer
	getClientWrapper := splstorage.RemoteDataClientsMap[vol.Provider]
	getClient := getClientWrapper.GetRemoteDataClientFuncPtr(ctx)

	appSecretRef := vol.SecretRef
	var accessKeyID string
	var secretAccessKey string
	var sessionToken string
	if appSecretRef == "" {
		// No secretRef means we should try to use the credentials available in the pod already via kube2iam or something similar
		scopedLog.InfoContext(ctx, "no secrectRef provided.  Attempt to access remote storage client without access/secret keys")
		accessKeyID = ""
		secretAccessKey = ""
	} else {
		// Get credentials through the secretRef
		remoteDataClientSecret, err := splutil.GetSecretByName(ctx, client, cr.GetNamespace(), appSecretRef)
		if err != nil {
			// Emit event for missing secret
			if k8serrors.IsNotFound(err) {
				if eventPublisher != nil {
					eventPublisher.Warning(ctx, splcommon.EventReasonSecretMissing,
						fmt.Sprintf("Required secret '%s' not found in namespace '%s'. Create secret to proceed.", appSecretRef, cr.GetNamespace()))
				}
			}

			return remoteDataClient, err
		}

		// Get access keys
		if vol.Provider == "azure" {
			accessKeyID = string(remoteDataClientSecret.Data["azure_sa_name"])
			secretAccessKey = string(remoteDataClientSecret.Data["azure_sa_secret_key"])
		} else if vol.Provider == "gcp" {
			accessKeyID = "key.json"
			secretAccessKey = string(remoteDataClientSecret.Data[accessKeyID])
		} else {
			accessKeyID = string(remoteDataClientSecret.Data["s3_access_key"])
			secretAccessKey = string(remoteDataClientSecret.Data["s3_secret_key"])
			sessionToken = string(remoteDataClientSecret.Data["s3_session_token"])
		}

		// Do we need to handle if IAM_ROLE is set in the secret as well?
		if accessKeyID == "" {
			err = fmt.Errorf("accessKey missing")
			return remoteDataClient, err
		}
		if secretAccessKey == "" {
			err = fmt.Errorf("s3 Secret Key is missing")
			return remoteDataClient, err
		}
	}
	if vol.Provider == "aws" && sessionToken != "" {
		ctx = storageaws.WithSessionToken(ctx, sessionToken)
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

	scopedLog.InfoContext(ctx, "creating the client", "volume", vol.Name, "bucket", bucket, "bucketPath", prefix)

	var err error

	remoteDataClient.Client, err = getClient(ctx, bucket, accessKeyID, secretAccessKey, prefix, prefix /* startAfter*/, vol.Region, vol.Endpoint, fn)

	if err != nil {
		scopedLog.ErrorContext(ctx, "failed to get the S3 client", "error", err)
		// Emit event when operator cannot connect to the remote app repository
		if eventPublisher != nil {
			eventPublisher.Warning(ctx, splcommon.EventReasonAppRepoConnFailed,
				fmt.Sprintf("Failed to connect to app repository '%s': %s. Check credentials and network.", vol.Name, err.Error()))
		}
		return remoteDataClient, err
	}

	return remoteDataClient, nil
}

func getLocalAppFileName(ctx context.Context, downloadPath, appName, etag string) string {
	return downloadPath + appName + "_" + strings.Trim(etag, "\"")
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

	scopedLog := logging.FromContext(ctx).With("func", "setBundlePushState")

	scopedLog.InfoContext(ctx, "setting the bundle push state", "oldState", bundlePushStateAsStr(ctx, afwPipeline.appDeployContext.BundlePushStatus.BundlePushStage), "newState", bundlePushStateAsStr(ctx, state))
	afwPipeline.appDeployContext.BundlePushStatus.BundlePushStage = state
}

// getBundlePushState returns the current bundle push state
func getBundlePushState(afwPipeline *AppInstallPipeline) enterpriseApi.BundlePushStageType {
	return afwPipeline.appDeployContext.BundlePushStatus.BundlePushStage
}

// createAppDownloadDir creates the app download directory on the operator pod
func createAppDownloadDir(_ context.Context, path string) error {
	_, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return os.MkdirAll(path, 0700)
	}
	return err
}

// getAvailableDiskSpace returns the disk space available to download apps, along with the
// resolved volume path used (splcommon.AppDownloadVolume, falling back to TmpAppDownloadDir
// if that volume isn't mounted on the operator pod).
func getAvailableDiskSpace(ctx context.Context) (int64, string, error) {
	var availDiskSpace int64
	var stat syscall.Statfs_t

	scopedLog := logging.FromContext(ctx).With("func", "getAvailableDiskSpace", "volume mount", splcommon.AppDownloadVolume)

	resolvedVolume := splcommon.AppDownloadVolume
	err := syscall.Statfs(resolvedVolume, &stat)
	if err != nil {
		scopedLog.ErrorContext(ctx, "there is no default volume configured for the App framework, use the temporary location", "dir", TmpAppDownloadDir, "error", err)
		resolvedVolume = TmpAppDownloadDir
		err = os.MkdirAll(resolvedVolume, 0700)
		if err != nil {
			scopedLog.ErrorContext(ctx, "unable to create the directory", "dir", resolvedVolume, "error", err)
			return 0, resolvedVolume, err
		}
	}

	err = syscall.Statfs(resolvedVolume, &stat)
	if err != nil {
		return 0, resolvedVolume, err
	}

	availDiskSpace = int64(stat.Bavail) * int64(stat.Bsize)
	scopedLog.InfoContext(ctx, "current available disk space in GB", "availableDiskSpace(GB)", availDiskSpace/1024/1024/1024)

	return availDiskSpace, resolvedVolume, err
}

// getRemoteObjectKey gets the remote object key
func getRemoteObjectKey(ctx context.Context, cr splcommon.MetaObject, appFrameworkConfig *enterpriseApi.AppFrameworkSpec, appSrcName, appName string) (string, error) {
	var remoteObjectKey string
	var vol enterpriseApi.VolumeSpec

	scopedLog := logging.FromContext(ctx).With("func", "getRemoteObjectKey", "crName", cr.GetName(), "namespace", cr.GetNamespace(), "appSrcName", appSrcName, "appName", appName)

	appSrc, err := getAppSrcSpec(appFrameworkConfig.AppSources, appSrcName)
	if err != nil {
		scopedLog.ErrorContext(ctx, "unable to get appSourceSpec", "error", err)
		return remoteObjectKey, err
	}

	vol, err = splutil.GetAppSrcVolume(ctx, *appSrc, appFrameworkConfig)
	if err != nil {
		scopedLog.ErrorContext(ctx, "unable to get volume spec", "error", err)
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

// getRemoteDataClientMgr gets the RemoteDataClientMgr instance to download apps
func getRemoteDataClientMgr(ctx context.Context, client splcommon.ControllerClient, cr splcommon.MetaObject, appFrameworkConfig *enterpriseApi.AppFrameworkSpec, appSrcName string) (*RemoteDataClientManager, error) {

	scopedLog := logging.FromContext(ctx).With("func", "RemoteDataClientMgr", "crName", cr.GetName(), "namespace", cr.GetNamespace(), "appSrcName", appSrcName)
	var vol enterpriseApi.VolumeSpec
	appSrc, err := getAppSrcSpec(appFrameworkConfig.AppSources, appSrcName)
	if err != nil {
		scopedLog.ErrorContext(ctx, "unable to get appSrcSpc", "error", err)
		return nil, err
	}

	vol, err = splutil.GetAppSrcVolume(ctx, *appSrc, appFrameworkConfig)
	if err != nil {
		scopedLog.ErrorContext(ctx, "unable to get volume spec", "error", err)
		return nil, err
	}

	remoteDataClientWrapper := splstorage.RemoteDataClientsMap[vol.Provider]
	initFunc := remoteDataClientWrapper.GetRemoteDataClientInitFuncPtr(ctx)
	remoteDataClientMgr := &RemoteDataClientManager{
		client:              client,
		cr:                  cr,
		appFrameworkRef:     appFrameworkConfig,
		vol:                 &vol,
		location:            appSrc.Location,
		initFn:              initFunc,
		getRemoteDataClient: GetRemoteStorageClient,
	}
	return remoteDataClientMgr, nil
}

// getAppPackageLocalDir returns the Operator volume directory for a given app package
func getAppPackageLocalDir(cr splcommon.MetaObject, scope string, appSrcName string) string {
	return filepath.Join(getResolvedAppDownloadVolume(), "downloadedApps", cr.GetNamespace(), cr.GroupVersionKind().Kind, cr.GetName(), scope, appSrcName) + "/"
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

type RemoteDataClientManager struct {
	client              splcommon.ControllerClient
	cr                  splcommon.MetaObject
	appFrameworkRef     *enterpriseApi.AppFrameworkSpec
	vol                 *enterpriseApi.VolumeSpec
	location            string
	initFn              splcommon.GetInitFunc
	getRemoteDataClient func(ctx context.Context, client splcommon.ControllerClient, cr splcommon.MetaObject,
		appFrameworkRef *enterpriseApi.AppFrameworkSpec, vol *enterpriseApi.VolumeSpec,
		location string, fp splcommon.GetInitFunc) (splstorage.SplunkRemoteDataClient, error)
}

// GetAppsList gets the apps list
func (rdcMgr *RemoteDataClientManager) GetAppsList(ctx context.Context) (splcommon.RemoteDataListResponse, error) {
	var remoteDataListResponse splcommon.RemoteDataListResponse

	c, err := rdcMgr.getRemoteDataClient(ctx, rdcMgr.client, rdcMgr.cr, rdcMgr.appFrameworkRef, rdcMgr.vol, rdcMgr.location, rdcMgr.initFn)
	if err != nil {
		return remoteDataListResponse, err
	}

	remoteDataListResponse, err = c.Client.GetAppsList(ctx)
	if err != nil {
		return remoteDataListResponse, err
	}
	return remoteDataListResponse, nil
}

// DownloadApp downloads the app from remote storage
func (rdcMgr *RemoteDataClientManager) DownloadApp(ctx context.Context, remoteFile string, localFile string, etag string) error {

	c, err := rdcMgr.getRemoteDataClient(ctx, rdcMgr.client, rdcMgr.cr, rdcMgr.appFrameworkRef, rdcMgr.vol, rdcMgr.location, rdcMgr.initFn)
	if err != nil {
		return err
	}

	downloadRequest := splcommon.RemoteDataDownloadRequest{
		LocalFile:  localFile,
		RemoteFile: remoteFile,
		Etag:       etag,
	}

	_, err = c.Client.DownloadApp(ctx, downloadRequest)
	if err != nil {
		return err
	}
	return err
}

// GetAppsList this func pointer is to use this function in unit test cases
var GetAppsList = func(ctx context.Context, RemoteDataClientMgr RemoteDataClientManager) (splcommon.RemoteDataListResponse, error) {
	remoteDataListResponse, err := RemoteDataClientMgr.GetAppsList(ctx)
	return remoteDataListResponse, err
}

// GetAppListFromRemoteBucket gets the list of apps from remote storage.
func GetAppListFromRemoteBucket(ctx context.Context, client splcommon.ControllerClient, cr splcommon.MetaObject, appFrameworkRef *enterpriseApi.AppFrameworkSpec) (map[string]splcommon.RemoteDataListResponse, error) {

	scopedLog := logging.FromContext(ctx).With("func", "GetAppListFromRemoteBucket", "name", cr.GetName(), "namespace", cr.GetNamespace())

	sourceToAppListMap := make(map[string]splcommon.RemoteDataListResponse)

	scopedLog.InfoContext(ctx, "getting the list of apps from remote storage")

	var remoteDataListResponse splcommon.RemoteDataListResponse
	var vol enterpriseApi.VolumeSpec
	var err error
	var allSuccess bool = true

	for _, appSource := range appFrameworkRef.AppSources {
		vol, err = splutil.GetAppSrcVolume(ctx, appSource, appFrameworkRef)
		if err != nil {
			allSuccess = false
			continue
		}

		remoteDataClientWrapper := splstorage.RemoteDataClientsMap[vol.Provider]
		initFunc := remoteDataClientWrapper.GetRemoteDataClientInitFuncPtr(ctx)
		remoteDataClientMgr := RemoteDataClientManager{
			client:              client,
			cr:                  cr,
			appFrameworkRef:     appFrameworkRef,
			vol:                 &vol,
			location:            appSource.Location,
			initFn:              initFunc,
			getRemoteDataClient: GetRemoteStorageClient,
		}

		// Now, get the apps list from remote storage
		remoteDataListResponse, err = GetAppsList(ctx, remoteDataClientMgr)
		if err != nil {
			// move on to the next appSource if we are not able to get apps list
			scopedLog.ErrorContext(ctx, "unable to get apps list", "appSource", appSource.Name, "error", err)
			allSuccess = false
			continue
		}

		sourceToAppListMap[appSource.Name] = remoteDataListResponse
	}

	if !allSuccess {
		err = fmt.Errorf("unable to get apps list from remote storage list for all the apps")
	}

	return sourceToAppListMap, err
}

// checkIfAnAppIsActiveOnRemoteStore checks if the App is listed as part of the AppSrc listing
func checkIfAnAppIsActiveOnRemoteStore(appName string, list []*splcommon.RemoteObject) bool {
	for i := range list {
		if strings.HasSuffix(*list[i].Key, appName) {
			return true
		}
	}

	return false
}

// checkIfAppSrcExistsWithRemoteListing checks if a given AppSrc is part of the remote listing
func checkIfAppSrcExistsWithRemoteListing(appSrc string, remoteObjListingMap map[string]splcommon.RemoteDataListResponse) bool {
	if _, ok := remoteObjListingMap[appSrc]; ok {
		return true
	}

	return false
}

// ChangeAppSrcDeployInfoStatus sets the new status to all the apps in an AppSrc if the given repo state and deploy status matches.
// primarily used in Phase-3
func ChangeAppSrcDeployInfoStatus(ctx context.Context, appSrc string, appSrcDeployStatus map[string]enterpriseApi.AppSrcDeployInfo, repoState enterpriseApi.AppRepoState, oldDeployStatus enterpriseApi.AppDeploymentStatus, newDeployStatus enterpriseApi.AppDeploymentStatus) {

	scopedLog := logging.FromContext(ctx).With("func", "changeAppSrcDeployInfoStatus", "appSource", appSrc, "repoState", repoState, "oldDeployStatus", oldDeployStatus, "newDeployStatus", newDeployStatus)

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
		scopedLog.InfoContext(ctx, "complete")
	} else {
		// Ideally this should never happen, check if the "IsDeploymentInProgress" flag is handled correctly or not
		scopedLog.ErrorContext(ctx, "could not find the App Source in App context")
	}
}

// ChangePhaseInfo changes PhaseInfo and AuxPhaseInfo for each app to desired state.
func ChangePhaseInfo(ctx context.Context, desiredReplicas int32, appSrc string, appSrcDeployStatus map[string]enterpriseApi.AppSrcDeployInfo) {

	scopedLog := logging.FromContext(ctx).With("func", "changePhaseInfo")

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
		scopedLog.ErrorContext(ctx, "could not find the App Source in App context")
	}
}

// RemoveStaleEntriesFromAuxPhaseInfo removes stale auxiliary phase entries after a scale down.
func RemoveStaleEntriesFromAuxPhaseInfo(ctx context.Context, desiredReplicas int32, appSrc string, appSrcDeployStatus map[string]enterpriseApi.AppSrcDeployInfo) {

	scopedLog := logging.FromContext(ctx).With("func", "changePhaseInfo")

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
		scopedLog.ErrorContext(ctx, "could not find the App Source in App context")
	}

}

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
	appDeployContext *enterpriseApi.AppDeploymentContext, remoteObjListingMap map[string]splcommon.RemoteDataListResponse, appFrameworkConfig *enterpriseApi.AppFrameworkSpec) (bool, error) {
	crKind := cr.GetObjectKind().GroupVersionKind().Kind

	scopedLog := logging.FromContext(ctx).With("func", "handleAppRepoChanges", "kind", crKind, "name", cr.GetName(), "namespace", cr.GetNamespace())
	var err error
	appsModified := false

	scopedLog.InfoContext(ctx, "received App listing", "for App sources", len(remoteObjListingMap))
	if len(remoteObjListingMap) == 0 {
		scopedLog.ErrorContext(ctx, "remoteObjectList is empty. Any apps that are already deployed will be disabled")
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
			scopedLog.InfoContext(ctx, "app change: App source is missing in config or remote listing, deleting/disabling all the apps", "appSource", appSrc)
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
	for appSrc, remoteDataListResponse := range remoteObjListingMap {
		// 2.1 Mark Apps for deletion if they are missing in remote listing
		appSrcDeploymentInfo, appSrcExistsLocally := appDeployContext.AppsSrcDeployStatus[appSrc]

		if appSrcExistsLocally {
			currentList := appSrcDeploymentInfo.AppDeploymentInfoList
			for appIdx := range currentList {
				if !isAppRepoStateDeleted(appSrcDeploymentInfo.AppDeploymentInfoList[appIdx]) && !checkIfAnAppIsActiveOnRemoteStore(currentList[appIdx].AppName, remoteDataListResponse.Objects) {
					scopedLog.InfoContext(ctx, "app change: deleting/disabling app missing in remote listing", "appName", currentList[appIdx].AppName)
					setStateAndStatusForAppDeployInfo(&currentList[appIdx], enterpriseApi.RepoStateDeleted, enterpriseApi.DeployStatusComplete)
				}
			}
		}

		// 2.2 Check for any App changes(Ex. A new App source, a new App added/updated)
		appsModified = AddOrUpdateAppSrcDeploymentInfoList(ctx, &appSrcDeploymentInfo, remoteDataListResponse.Objects)
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

// isAppExtensionValid checks if an app extension is supported or not
func isAppExtensionValid(receivedKey string) bool {
	validExtensions := []string{".spl", ".tgz", ".tar.gz"}

	for _, ext := range validExtensions {
		if strings.HasSuffix(receivedKey, ext) {
			return true
		}
	}
	return false
}

// AddOrUpdateAppSrcDeploymentInfoList  modifies the App deployment status as perceived from the remote object listing
func AddOrUpdateAppSrcDeploymentInfoList(ctx context.Context, appSrcDeploymentInfo *enterpriseApi.AppSrcDeployInfo, remoteS3ObjList []*splcommon.RemoteObject) bool {

	scopedLog := logging.FromContext(ctx).With("func", "AddOrUpdateAppSrcDeploymentInfoList", "listLength", len(remoteS3ObjList))

	var found bool
	var appName string
	var newAppInfoList []enterpriseApi.AppDeploymentInfo
	var appChangesDetected bool
	var appDeployInfo enterpriseApi.AppDeploymentInfo

	for _, remoteObj := range remoteS3ObjList {
		receivedKey := *remoteObj.Key
		if !isAppExtensionValid(receivedKey) {
			scopedLog.ErrorContext(ctx, "app name Parsing: Ignoring the key with invalid extension", "receivedKey", receivedKey)
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
					scopedLog.InfoContext(ctx, "app change detected.  Marking for an update", "appName", appName)
					appList[idx].ObjectHash = *remoteObj.Etag
					appList[idx].IsUpdate = true
					appList[idx].DeployStatus = enterpriseApi.DeployStatusPending
					appList[idx].PhaseInfo.Phase = enterpriseApi.PhaseDownload
					appList[idx].PhaseInfo.Status = enterpriseApi.AppPkgDownloadPending
					appList[idx].PhaseInfo.FailCount = 0
					appList[idx].AuxPhaseInfo = nil

					// Make the state active for an app that was deleted earlier, and got activated again
					if appList[idx].RepoState == enterpriseApi.RepoStateDeleted {
						scopedLog.InfoContext(ctx, "app change.  Enabling the App that was previously disabled/deleted", "appName", appName)
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
			scopedLog.InfoContext(ctx, "new App found", "appName", appName)
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

	scopedLog := logging.FromContext(ctx).With("func", "markAppsStatusToComplete")

	// ToDo: Passing appSrcDeploymentStatus is redundant, but this function will go away in phase-3, so ok for now.
	for appSrc := range appSrcDeploymentStatus {
		ChangeAppSrcDeployInfoStatus(ctx, appSrc, appSrcDeploymentStatus, enterpriseApi.RepoStateActive, enterpriseApi.DeployStatusPending, enterpriseApi.DeployStatusComplete)
		ChangeAppSrcDeployInfoStatus(ctx, appSrc, appSrcDeploymentStatus, enterpriseApi.RepoStateDeleted, enterpriseApi.DeployStatusPending, enterpriseApi.DeployStatusComplete)
	}

	scopedLog.InfoContext(ctx, "marked the App deployment status to complete")
	// ToDo: Caller of this API also needs to set "IsDeploymentInProgress = false" once after completing this function call for all the app sources

	return err
}

// isAppAlreadyDownloaded checks if the app is already present on the operator pod
func isAppAlreadyDownloaded(ctx context.Context, downloadWorker *PipelineWorker) bool {

	scopedLog := logging.FromContext(ctx).With("func", "isAppAlreadyDownloaded", "appName", downloadWorker.appDeployInfo.AppName)

	scope := getAppSrcScope(ctx, downloadWorker.afwConfig, downloadWorker.appSrcName)
	kind := downloadWorker.cr.GetObjectKind().GroupVersionKind().Kind

	localPath := filepath.Join(getResolvedAppDownloadVolume(), "downloadedApps", downloadWorker.cr.GetNamespace(), kind, downloadWorker.cr.GetName(), scope, downloadWorker.appSrcName) + "/"
	localAppFileName := getLocalAppFileName(ctx, localPath, downloadWorker.appDeployInfo.AppName, downloadWorker.appDeployInfo.ObjectHash)

	// check if the app is present on operator pod
	fileInfo, err := os.Stat(localAppFileName)

	if errors.Is(err, os.ErrNotExist) {
		scopedLog.InfoContext(ctx, "app not present on operator pod")
		return false
	}

	localSize := fileInfo.Size()
	remoteSize := int64(downloadWorker.appDeployInfo.Size)
	if localSize != remoteSize {
		err = fmt.Errorf("local size does not match with size on remote storage. localSize=%d, remoteSize=%d", localSize, remoteSize)
		scopedLog.ErrorContext(ctx, "incorrect app size", "error", err)
		return false
	}
	return true
}

// SetLastAppInfoCheckTime sets the last check time to current time
func SetLastAppInfoCheckTime(ctx context.Context, appInfoStatus *enterpriseApi.AppDeploymentContext) {

	scopedLog := logging.FromContext(ctx).With("func", "SetLastAppInfoCheckTime")
	currentEpoch := time.Now().Unix()

	scopedLog.InfoContext(ctx, "setting the LastAppInfoCheckTime to current time", "current epoch time", currentEpoch)

	appInfoStatus.LastAppInfoCheckTime = currentEpoch
}

// HasAppRepoCheckTimerExpired checks if the polling interval has expired
func HasAppRepoCheckTimerExpired(ctx context.Context, appInfoContext *enterpriseApi.AppDeploymentContext) bool {

	scopedLog := logging.FromContext(ctx).With("func", "HasAppRepoCheckTimerExpired")
	currentEpoch := time.Now().Unix()

	isTimerExpired := appInfoContext.LastAppInfoCheckTime+appInfoContext.AppsRepoStatusPollInterval <= currentEpoch
	if isTimerExpired {
		scopedLog.InfoContext(ctx, "app repo polling interval timer has expired", "LastAppInfoCheckTime", strconv.FormatInt(appInfoContext.LastAppInfoCheckTime, 10), "current epoch time", strconv.FormatInt(currentEpoch, 10))
	}

	return isTimerExpired
}

// GetNextRequeueTime gets the next reconcile requeue time based on the appRepoPollInterval.
// There can be some time elapsed between when we first set lastAppInfoCheckTime and when the CR is in Ready state.
// Hence we need to subtract the delta time elapsed from the actual polling interval,
// so that the next reconcile would happen at the right time.
func GetNextRequeueTime(ctx context.Context, appRepoPollInterval, lastCheckTime int64) time.Duration {

	scopedLog := logging.FromContext(ctx).With("func", "GetNextRequeueTime")
	currentEpoch := time.Now().Unix()

	var nextRequeueTimeInSec int64
	nextRequeueTimeInSec = appRepoPollInterval - (currentEpoch - lastCheckTime)
	if nextRequeueTimeInSec < 0 {
		nextRequeueTimeInSec = 5
	}

	scopedLog.InfoContext(ctx, "getting next requeue time", "LastAppInfoCheckTime", lastCheckTime, "Current Epoch time", currentEpoch, "nextRequeueTimeInSec", nextRequeueTimeInSec)

	return time.Second * (time.Duration(nextRequeueTimeInSec))
}

// isAppRepoPollingEnabled checks whether automatic polling for apps repo changes
// is enabled or not. If the value is 0, then we fallback to on-demand polling of apps
// repo changes.
func isAppRepoPollingEnabled(appStatusContext *enterpriseApi.AppDeploymentContext) bool {
	return appStatusContext.AppsRepoStatusPollInterval != 0
}

// shouldCheckAppRepoStatus
func shouldCheckAppRepoStatus(ctx context.Context, client splcommon.ControllerClient, cr splcommon.MetaObject, appStatusContext *enterpriseApi.AppDeploymentContext, kind string, turnOffManualChecking *bool) bool {

	scopedLog := logging.FromContext(ctx).With("func", "shouldCheckAppRepoStatus")
	// If polling is disabled, check if manual update is on.
	if !isAppRepoPollingEnabled(appStatusContext) {
		configMapName := splutil.GetSplunkManualAppUpdateConfigMapName(cr.GetNamespace())

		// Check if we need to manually check for app updates for this CR kind
		scopedLog.InfoContext(ctx, "checking if namespace specific configmap contains manualUpdate settings")
		if getManualUpdateStatus(ctx, client, cr, configMapName) == "on" {
			scopedLog.InfoContext(ctx, "namespace specific configmap contains manualUpdate is set to on", "configMapName", configMapName)
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

func IsManualUpdateSetInCRConfig(ctx context.Context, client splcommon.ControllerClient, cr splcommon.MetaObject, appStatusContext *enterpriseApi.AppDeploymentContext, kind string, turnOffManualChecking *bool) bool {

	scopedLog := logging.FromContext(ctx).With("func", "shouldCheckAppRepoStatusPerCR")

	configMapName := splutil.GetSplunkPerCRConfigMapName(splcommon.KindToInstanceString(cr.GroupVersionKind().Kind), cr.GetName())
	scopedLog.InfoContext(ctx, "checking if per CR specific configmap contains manualUpdate settings")
	if getManualUpdatePerCrStatus(ctx, client, cr, configMapName) == "on" {
		scopedLog.InfoContext(ctx, "CR specific configmap contains manualUpdate set to on", "configMapName", configMapName)
		//*turnOffManualChecking = true
		return true
	}
	return false
}

// getCleanObjectDigest returns only hexa-decimal portion of a string
// Ex. '\"b38a8f911e2b43982b71a979fe1d3c3f\"' is converted to b38a8f911e2b43982b71a979fe1d3c3f
func getCleanObjectDigest(rawObjectDigest *string) (*string, error) {
	// S3: In the case of multipart upload, '-' is an allowed character as part of the etag
	reg, err := regexp.Compile("[^A-Fa-f0-9...-]+")
	if err != nil {
		return nil, err
	}

	cleanObjectHash := reg.ReplaceAllString(*rawObjectDigest, "")
	return &cleanObjectHash, nil
}

// updateManualAppUpdateConfigMapLocked updates the manual app update configuration map for a given custom resource (CR).
// It locks the resource mutex for the config map, retrieves the config map, and updates the status and reference count
// based on whether manual checking is turned off or not.
//
// Parameters:
// - ctx: The context for the request.
// - client: The controller client to interact with the Kubernetes API.
// - cr: The custom resource meta object.
// - appStatusContext: The application deployment context.
// - kind: The kind of the custom resource.
// - turnOffManualChecking: A boolean indicating whether to turn off manual checking.
//
// Returns:
// - error: An error if the operation fails, otherwise nil.
func updateManualAppUpdateConfigMapLocked(ctx context.Context, client splcommon.ControllerClient, cr splcommon.MetaObject, appStatusContext *enterpriseApi.AppDeploymentContext, kind string, turnOffManualChecking bool) error {

	scopedLog := logging.FromContext(ctx).With("func", "updateManualAppUpdateConfigMap", "name", cr.GetName(), "namespace", cr.GetNamespace())
	var status string

	configMapName := splutil.GetSplunkManualAppUpdateConfigMapName(cr.GetNamespace())
	namespacedName := types.NamespacedName{Namespace: cr.GetNamespace(), Name: configMapName}

	{
		mux := getResourceMutex(configMapName)
		mux.Lock()
		defer mux.Unlock()
		configMap, err := k8sops.GetConfigMap(ctx, client, namespacedName)
		if err != nil {
			scopedLog.ErrorContext(ctx, "unable to get configMap", "name", namespacedName.Name, "error", err)
			return err
		}

		// first check the refCount and status
		//
		numOfObjects := getManualUpdateRefCount(ctx, client, cr, configMapName)

		// turn off the manual checking for this CR kind in the configMap
		if turnOffManualChecking {
			scopedLog.InfoContext(ctx, "turning off manual checking of apps update", "Kind", kind)
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
			scopedLog.ErrorContext(ctx, "could not update the configMap", "name", namespacedName.Name, "error", err)
			return err
		}
	}

	return nil
}

func updateCrSpecificManualAppUpdateConfigMap(ctx context.Context, client splcommon.ControllerClient, cr splcommon.MetaObject, appStatusContext *enterpriseApi.AppDeploymentContext, kind string, turnOffManualChecking bool) error {

	scopedLog := logging.FromContext(ctx).With("func", "updateManualAppUpdateConfigMap", "name", cr.GetName(), "namespace", cr.GetNamespace())
	configMapName := splutil.GetSplunkManualAppUpdateConfigMapName(cr.GetNamespace())
	namespacedName := types.NamespacedName{Namespace: cr.GetNamespace(), Name: configMapName}
	// now check namespace specific configmap if it contains manualUpdate settings
	crScopedConfigMapName := splutil.GetSplunkPerCRConfigMapName(splcommon.KindToInstanceString(cr.GroupVersionKind().Kind), cr.GetName())
	crNamespacedName := types.NamespacedName{Namespace: cr.GetNamespace(), Name: crScopedConfigMapName}
	configMap, err := k8sops.GetConfigMap(ctx, client, crNamespacedName)
	if err != nil {
		scopedLog.ErrorContext(ctx, "unable to get configMap", "name", namespacedName.Name, "error", err)
		return err
	}
	if configMap.Data["manualUpdate"] == "on" {
		scopedLog.InfoContext(ctx, "turning off manual checking of apps update in per CR configmap", "Kind", kind)
		configMap.Data["manualUpdate"] = "off"
	}

	err = splutil.UpdateResource(ctx, client, configMap)
	if err != nil {
		scopedLog.ErrorContext(ctx, "could not update the per CR configMap", "name", crNamespacedName.Name, "error", err)
		return err
	}
	return err
}

// InitAndCheckAppInfoStatus initializes the RemoteDataClients and checks the status of apps on remote storage.
func InitAndCheckAppInfoStatus(ctx context.Context, client splcommon.ControllerClient, cr splcommon.MetaObject,
	appFrameworkConf *enterpriseApi.AppFrameworkSpec, appStatusContext *enterpriseApi.AppDeploymentContext) error {

	scopedLog := logging.FromContext(ctx).With("func", "InitAndCheckAppInfoStatus", "name", cr.GetName(), "namespace", cr.GetNamespace())

	var err error
	// Register the RemoteData Clients specific to providers if not done already
	// This is done to prevent the null pointer dereference in case when
	// operator crashes and comes back up and the status of app context was updated
	// to match the spec in the previous run.
	err = initAppFrameWorkContext(ctx, client, cr, appFrameworkConf, appStatusContext)
	if err != nil {
		scopedLog.ErrorContext(ctx, "unable initialize app framework", "error", err)
		return err
	}

	var turnOffManualChecking bool
	kind := cr.GetObjectKind().GroupVersionKind().Kind

	//check if the apps need to be downloaded from remote storage
	if shouldCheckAppRepoStatus(ctx, client, cr, appStatusContext, kind, &turnOffManualChecking) ||
		IsManualUpdateSetInCRConfig(ctx, client, cr, appStatusContext, kind, &turnOffManualChecking) ||
		!reflect.DeepEqual(appStatusContext.AppFrameworkConfig, *appFrameworkConf) {

		if appStatusContext.IsDeploymentInProgress {
			scopedLog.InfoContext(ctx, "app installation is already in progress. Not checking for any latest app repo changes")
			return nil
		}

		appStatusContext.IsDeploymentInProgress = true
		var sourceToAppsList map[string]splcommon.RemoteDataListResponse

		scopedLog.InfoContext(ctx, "checking status of apps on remote storage")

		sourceToAppsList, err = GetAppListFromRemoteBucket(ctx, client, cr, appFrameworkConf)
		// TODO: gaurav, we need to handle this case better in Phase-3. There can be a possibility
		// where if an appSource is missing in remote store, we mark it for deletion. But if it comes up
		// next time, we will recycle the pod to install the app. We need to find a way to reduce the pod recycles.
		if len(sourceToAppsList) != len(appFrameworkConf.AppSources) {
			scopedLog.ErrorContext(ctx, "unable to get apps list, will retry in next reconcile", "error", err)
		} else {
			for _, appSource := range appFrameworkConf.AppSources {
				// Clean-up for the object digest value
				for i := range sourceToAppsList[appSource.Name].Objects {
					cleanDigest, err := getCleanObjectDigest(sourceToAppsList[appSource.Name].Objects[i].Etag)
					if err != nil {
						scopedLog.ErrorContext(ctx, "unable to fetch clean object digest value", "objectHash", sourceToAppsList[appSource.Name].Objects[i].Etag, "error", err)
						return err
					}

					sourceToAppsList[appSource.Name].Objects[i].Etag = cleanDigest
				}

				scopedLog.InfoContext(ctx, "apps List retrieved from remote storage", "appSource", appSource.Name, "content", sourceToAppsList[appSource.Name].Objects)
			}

			// Only handle the app repo changes if we were able to successfully get the apps list
			_, err = handleAppRepoChanges(ctx, client, cr, appStatusContext, sourceToAppsList, appFrameworkConf)
			if err != nil {
				scopedLog.ErrorContext(ctx, "unable to use the App list retrieved from the remote storage", "error", err)
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
				scopedLog.ErrorContext(ctx, "failed to update the manual app udpate configMap", "error", err)
				return err
			}
			err = updateCrSpecificManualAppUpdateConfigMap(ctx, client, cr, appStatusContext, kind, turnOffManualChecking)
			if err != nil {
				scopedLog.ErrorContext(ctx, "failed to update the manual app udpate CR specific configMap", "error", err)
				return err
			}
		}
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

	scopedLog := logging.FromContext(ctx).With("func", "UpdateOrRemoveEntryFromConfigMapLocked", "name", cr.GetName(), "namespace", cr.GetNamespace())

	configMapName := splutil.GetSplunkManualAppUpdateConfigMapName(cr.GetNamespace())
	namespacedName := types.NamespacedName{Namespace: cr.GetNamespace(), Name: configMapName}

	mux := getResourceMutex(configMapName)
	mux.Lock()
	defer mux.Unlock()
	configMap, err := k8sops.GetConfigMap(ctx, c, namespacedName)
	if err != nil {
		scopedLog.ErrorContext(ctx, "unable to get config map", "name", namespacedName.Name, "error", err)
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
		scopedLog.ErrorContext(ctx, "unable to update configMap", "name", namespacedName.Name, "error", err)
		return err
	}

	return nil
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

	scopedLog := logging.FromContext(ctx).With("func", "checkIfFileExistsOnPod", "podName", podExecClient.GetTargetPodName(), "namespace", cr.GetNamespace(), "filePath", filePath)
	// Make sure the destination directory is existing
	fPath := path.Clean(filePath)
	command := fmt.Sprintf("test -f %s; echo -n $?", fPath)
	streamOptions := splutil.NewStreamOptionsObject(command)

	stdOut, stdErr, err := podExecClient.RunPodExecCommand(ctx, streamOptions, []string{"/bin/sh"})
	if stdErr != "" || err != nil {
		scopedLog.ErrorContext(ctx, "error in checking the file availability on the Pod", "stdErr", stdErr, "stdOut", stdOut, "filePath", fPath, "error", err)
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

var cpMakeTar = func(src localPath, dest remotePath, writer io.Writer) error {
	// TODO: use compression here?
	tarWriter := tar.NewWriter(writer)
	defer tarWriter.Close()

	srcPath := src.Clean()
	destPath := dest.Clean()
	return recursiveTar(srcPath.Dir(), srcPath.Base(), destPath.Dir(), destPath.Base(), tarWriter)
}

func recursiveTar(srcDir, srcFile localPath, destDir, destFile remotePath, tw *tar.Writer) error {
	matchedPaths, err := srcDir.Join(srcFile).Glob()
	if err != nil {
		return err
	}
	for _, fpath := range matchedPaths {
		stat, err := os.Lstat(fpath)
		if err != nil {
			return err
		}
		if stat.IsDir() {
			files, err := os.ReadDir(fpath)
			if err != nil {
				return err
			}
			if len(files) == 0 {
				//case empty directory
				hdr, _ := tar.FileInfoHeader(stat, fpath)
				hdr.Name = destFile.String()
				if err := tw.WriteHeader(hdr); err != nil {
					return err
				}
			}
			for _, f := range files {
				if err := recursiveTar(srcDir, srcFile.Join(newLocalPath(f.Name())),
					destDir, destFile.Join(newRemotePath(f.Name())), tw); err != nil {
					return err
				}
			}
			return nil
		} else if stat.Mode()&os.ModeSymlink != 0 {
			//case soft link
			hdr, _ := tar.FileInfoHeader(stat, fpath)
			target, err := os.Readlink(fpath)
			if err != nil {
				return err
			}

			hdr.Linkname = target
			hdr.Name = destFile.String()
			if err := tw.WriteHeader(hdr); err != nil {
				return err
			}
		} else {
			//case regular file or other file type like pipe
			hdr, err := tar.FileInfoHeader(stat, fpath)
			if err != nil {
				return err
			}
			hdr.Name = destFile.String()

			if err := tw.WriteHeader(hdr); err != nil {
				return err
			}
			fpath = filepath.Clean(fpath)
			f, err := os.Open(fpath)
			if err != nil {
				return err
			}
			defer f.Close()

			if _, err := io.Copy(tw, f); err != nil {
				return err
			}
			return f.Close()
		}
	}
	return nil
}

// CopyFileToPod copies a file from Operator Pod to any given Pod of a custom resource
func CopyFileToPod(ctx context.Context, c splcommon.ControllerClient, namespace string, srcPath string, destPath string, podExecClient splutil.PodExecClientImpl) (string, string, error) {

	scopedLog := logging.FromContext(ctx).With("func", "CopyFileToPod", "podName", podExecClient.GetTargetPodName(), "namespace", namespace, "srcPath", srcPath, "destPath", destPath)

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
			scopedLog.ErrorContext(ctx, "failed to send file on writer pipe", "srcPath", srcPath, "destPath", destPath, "error", err)
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

func setInstallStateForClusterScopedApps(ctx context.Context, appDeployContext *enterpriseApi.AppDeploymentContext) {

	scopedLog := logging.FromContext(ctx).With("func", "setInstallStateForClusterScopedApps")

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
				scopedLog.InfoContext(ctx, "cluster scoped app installed", "appName", deployInfoList[i].AppName, "digest", deployInfoList[i].ObjectHash)
			} else if deployInfoList[i].PhaseInfo.Phase != enterpriseApi.PhaseInstall || deployInfoList[i].PhaseInfo.Status != enterpriseApi.AppPkgInstallComplete {
				scopedLog.ErrorContext(ctx, "app missing from bundle push", "appName", deployInfoList[i].AppName, "digest", deployInfoList[i].ObjectHash, "phase", deployInfoList[i].PhaseInfo.Phase, "status", deployInfoList[i].PhaseInfo.Status)
			}
		}
	}
}

// isPersistentVolConfigured confirms if the Operator Pod is configured with storage
func isPersistentVolConfigured() bool {
	return operatorResourceTracker != nil && operatorResourceTracker.storage != nil
}

// reserveStorage tries to reserve the amount of requested storage
func reserveStorage(allocSize int64) error {
	if !isPersistentVolConfigured() {
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
func releaseStorage(releaseSize int64) error {
	if !isPersistentVolConfigured() {
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

	scopedLog := logging.FromContext(ctx).With("func", "updateReconcileRequeueTime")
	if result == nil {
		scopedLog.ErrorContext(ctx, "invalid result")
		return
	}
	if rqTime <= 0 {
		scopedLog.ErrorContext(ctx, "invalid requeue time", "timeValue", rqTime)
		return
	}

	result.Requeue = requeue

	// updated the requested, if it is lower than the one in hand
	if rqTime < result.RequeueAfter {
		result.RequeueAfter = rqTime
	}
}

// HandleAppFrameworkActivity handles any pending app framework activity.
func HandleAppFrameworkActivity(ctx context.Context, client splcommon.ControllerClient, cr splcommon.MetaObject, appDeployContext *enterpriseApi.AppDeploymentContext, appFrameworkConfig *enterpriseApi.AppFrameworkSpec) *reconcile.Result {

	scopedLog := logging.FromContext(ctx).With("func", "HandleAppFrameworkActivity", "name", cr.GetName(), "namespace", cr.GetNamespace())
	finalResult := &reconcile.Result{
		Requeue:      false,
		RequeueAfter: maxRecDuration,
	}

	clearAppContextIfSourcesRemoved(appFrameworkConfig, appDeployContext)

	// Consider the polling interval for next reconcile
	if isAppRepoPollingEnabled(appDeployContext) {
		requeueAfter := GetNextRequeueTime(ctx, appDeployContext.AppsRepoStatusPollInterval, appDeployContext.LastAppInfoCheckTime)
		updateReconcileRequeueTime(ctx, finalResult, requeueAfter, true)
	}

	if appDeployContext.AppsSrcDeployStatus != nil {
		requeue, err := afwSchedulerEntry(ctx, client, cr, appDeployContext, appFrameworkConfig)
		if err != nil {
			scopedLog.ErrorContext(ctx, "app framework returned error", "error", err)
		}
		if requeue {
			updateReconcileRequeueTime(ctx, finalResult, time.Second*5, true)
		}
	}

	return finalResult
}

// clearAppContextIfSourcesRemoved clears stale app deploy status when all AppSources have
// been removed from spec. Without this, the scheduler loops every ~5s from stale
// AppsSrcDeployStatus while /operator-staging is not mounted (no volume injected when
// spec is empty), causing a permanent Permission Denied loop.
func clearAppContextIfSourcesRemoved(appFrameworkConfig *enterpriseApi.AppFrameworkSpec, appDeployContext *enterpriseApi.AppDeploymentContext) bool {
	if len(appFrameworkConfig.AppSources) != 0 || appDeployContext.AppsSrcDeployStatus == nil {
		return false
	}
	appDeployContext.AppsSrcDeployStatus = nil
	appDeployContext.AppFrameworkConfig = enterpriseApi.AppFrameworkSpec{}
	appDeployContext.IsDeploymentInProgress = false
	appDeployContext.LastAppInfoCheckTime = 0
	appDeployContext.AppsRepoStatusPollInterval = 0
	appDeployContext.AppsStatusMaxConcurrentAppDownloads = 0
	appDeployContext.BundlePushStatus = enterpriseApi.BundlePushTracker{}
	return true
}

// CheckAndMigrateAppDeployStatus upgrades the app framework status context when required.
func CheckAndMigrateAppDeployStatus(ctx context.Context, client splcommon.ControllerClient, cr splcommon.MetaObject, afwStatusContext *enterpriseApi.AppDeploymentContext, afwConf *enterpriseApi.AppFrameworkSpec, isLocalScope bool) error {
	// If needed, Migrate the app framework status
	if isAppFrameworkMigrationNeeded(afwStatusContext) {
		// Spec validation updates the status with some of the defaults, which may not be there in older app framework versions
		err := ValidateAppFrameworkSpec(ctx, afwConf, afwStatusContext, isLocalScope, cr.GetObjectKind().GroupVersionKind().Kind)
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

	scopedLog := logging.FromContext(ctx).With("func", "migrateAfwStatus", "name", cr.GetName(), "namespace", cr.GetNamespace())

	// Upgrade one version at a time
	// Start with the lowest version, then move towards the latest
	for afwStatusContext.Version < enterpriseApi.LatestAfwVersion {
		switch {
		// Always start with the lowest version
		case afwStatusContext.Version < enterpriseApi.AfwPhase3:
			scopedLog.InfoContext(ctx, "migrating the App framework", "oldVersion", afwStatusContext.Version, "newVersion", enterpriseApi.AfwPhase3)
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
		scopedLog.ErrorContext(ctx, "status update failed", "error", err)
	}

	return err == nil
}

// migrateAfwFromPhase2ToPhase3 migrates app framework status from Phase-2 to Phase-3
func migrateAfwFromPhase2ToPhase3(ctx context.Context, client splcommon.ControllerClient, cr splcommon.MetaObject, afwStatusContext *enterpriseApi.AppDeploymentContext) error {

	scopedLog := logging.FromContext(ctx).With("func", "migrateAfwFromPhase2ToPhase3", "name", cr.GetName(), "namespace", cr.GetNamespace())

	sts := afwGetReleventStatefulsetByKind(ctx, cr, client)

	for _, appSrcDeployInfo := range afwStatusContext.AppsSrcDeployStatus {
		deployInfoList := appSrcDeployInfo.AppDeploymentInfoList
		for i := range deployInfoList {
			// Remove the special characters from Object hash
			cleanDigest, err := getCleanObjectDigest(&deployInfoList[i].ObjectHash)
			if err != nil {
				scopedLog.ErrorContext(ctx, "clean-up failed", "digest", deployInfoList[i].ObjectHash, "error", err)
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
	scopedLog.InfoContext(ctx, "migration completed")
	return nil
}

// isAppFrameworkMigrationNeeded confirms if the app framework version migration is needed
func isAppFrameworkMigrationNeeded(afwStatusContext *enterpriseApi.AppDeploymentContext) bool {
	return afwStatusContext != nil && afwStatusContext.Version < currentAfwVersion && len(afwStatusContext.AppsSrcDeployStatus) > 0
}
