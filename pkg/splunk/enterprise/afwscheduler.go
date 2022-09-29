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
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/pkg/errors"
	enterpriseApi "github.com/splunk/splunk-operator/api/v3"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	splctrl "github.com/splunk/splunk-operator/pkg/splunk/controller"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// isFanOutApplicableToCR confirms if a given CR needs fanOut support
func isFanOutApplicableToCR(cr splcommon.MetaObject) bool {
	switch cr.GetObjectKind().GroupVersionKind().Kind {
	case "Standalone":
		return true
	default:
		return false
	}
}

// createAndAddPipelineWorker used to add a worker to the pipeline on reconcile re-entry
func (ppln *AppInstallPipeline) createAndAddPipelineWorker(ctx context.Context, phase enterpriseApi.AppPhaseType, appDeployInfo *enterpriseApi.AppDeploymentInfo,
	appSrcName string, podName string, appFrameworkConfig *enterpriseApi.AppFrameworkSpec,
	client splcommon.ControllerClient, cr splcommon.MetaObject, statefulSet *appsv1.StatefulSet) {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("createAndAddPipelineWorker").WithValues("name", cr.GetName(), "namespace", cr.GetNamespace())

	worker := &PipelineWorker{
		appDeployInfo: appDeployInfo,
		appSrcName:    appSrcName,
		targetPodName: podName,
		afwConfig:     appFrameworkConfig,
		client:        client,
		cr:            cr,
		sts:           statefulSet,
		fanOut:        isFanOutApplicableToCR(cr),
	}

	scopedLog.Info("Created new worker", "Pod name", worker.targetPodName, "App name", appDeployInfo.AppName, "digest", appDeployInfo.ObjectHash, "phase", appDeployInfo.PhaseInfo.Phase, "fan out", worker.fanOut)

	ppln.addWorkersToPipelinePhase(ctx, phase, worker)
}

// getApplicablePodNameForAppFramework gets the Pod name relevant for the CR under work
func getApplicablePodNameForAppFramework(cr splcommon.MetaObject, ordinalIdx int) string {
	var podType string

	switch cr.GetObjectKind().GroupVersionKind().Kind {
	case "Standalone":
		podType = "standalone"
	case "LicenseMaster":
		podType = "license-master"
	case "SearchHeadCluster":
		podType = "deployer"
	case "IndexerCluster":
		return ""
	case "ClusterMaster":
		podType = "cluster-master"
	case "MonitoringConsole":
		podType = "monitoring-console"
	}

	return fmt.Sprintf("splunk-%s-%s-%d", cr.GetName(), podType, ordinalIdx)
}

// runCustomCommandOnSplunkPods  runs the specified custom command on the pod/s
func runCustomCommandOnSplunkPods(ctx context.Context, cr splcommon.MetaObject, replicas int32, command string, podExecClient splutil.PodExecClientImpl) error {
	var err error
	var stdOut string

	streamOptions := splutil.NewStreamOptionsObject(command)
	// Run the command on each replica pod
	for replicaIndex := 0; replicaIndex < int(replicas); replicaIndex++ {
		// get the target pod name
		podName := getApplicablePodNameForAppFramework(cr, replicaIndex)
		podExecClient.SetTargetPodName(ctx, podName)

		// CSPL-1639: reset the Stdin so that reader pipe can read from the correct offset of the string reader.
		// This is particularly needed in the cases where we are trying to run the same command across multiple pods
		// and we need to clear the reader pipe so that we can read the read buffer from the correct offset again.
		splutil.ResetStringReader(streamOptions, command)

		// Throw an error if we are not able to run the command
		stdOut, _, err = podExecClient.RunPodExecCommand(ctx, streamOptions, []string{"/bin/sh"})
		if err != nil {
			err = fmt.Errorf("unable to run command %s. stdout: %s, err: %s", command, stdOut, err)
			break
		}
	}
	return err
}

// Get extension for name of telemetry app
func getTelAppNameExtension(crKind string) (string, error) {
	switch crKind {
	case "Standalone":
		return "stdaln", nil
	case "LicenseMaster":
		return "lm", nil
	case "SearchHeadCluster":
		return "shc", nil
	case "ClusterMaster":
		return "cm", nil
	default:
		return "", errors.New("Invalid CR kind for telemetry app")
	}
}

// addTelApp adds a telemetry app
var addTelApp = func(ctx context.Context, podExecClient splutil.PodExecClientImpl, replicas int32, cr splcommon.MetaObject) error {
	var err error

	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("addTelApp").WithValues(
		"name", cr.GetObjectMeta().GetName(),
		"namespace", cr.GetObjectMeta().GetNamespace())

	// Create pod exec client
	crKind := cr.GetObjectKind().GroupVersionKind().Kind

	// Get Tel App Name Extension
	appNameExt, err := getTelAppNameExtension(crKind)
	if err != nil {
		return err
	}

	// Commands to run on pods
	var command1, command2 string

	// Handle non SHC scenarios(Standalone, CM, LM)
	if crKind != "SearchHeadCluster" {
		// Create dir on pods
		command1 = fmt.Sprintf(createTelAppNonShcString, appNameExt, telAppConfString, appNameExt)

		// App reload
		command2 = telAppReloadString

	} else {
		// Create dir on pods
		command1 = fmt.Sprintf(createTelAppShcString, shcAppsLocationOnDeployer, appNameExt, telAppConfString, shcAppsLocationOnDeployer, appNameExt)

		// Bundle push
		command2 = fmt.Sprintf(applySHCBundleCmdStr, GetSplunkStatefulsetURL(cr.GetNamespace(), SplunkSearchHead, cr.GetName(), 0, false), "/tmp/status.txt")
	}

	// Run the commands on Splunk pods
	err = runCustomCommandOnSplunkPods(ctx, cr, replicas, command1, podExecClient)
	if err != nil {
		scopedLog.Error(err, "unable to run command on splunk pod")
		return err
	}

	err = runCustomCommandOnSplunkPods(ctx, cr, replicas, command2, podExecClient)
	if err != nil {
		scopedLog.Error(err, "unable to run command on splunk pod")
		return err
	}

	return err
}

// getOrdinalValFromPodName returns the pod ordinal value
func getOrdinalValFromPodName(podName string) (int, error) {
	// K8 pod name should contain at least 3 occurrences of character "-"
	if strings.Count(podName, "-") < 3 {
		return 0, fmt.Errorf("invalid pod name %s", podName)
	}

	var tokens []string = strings.Split(podName, "-")
	return strconv.Atoi(tokens[len(tokens)-1])
}

// addWorkersToPipelinePhase adds a worker to a given pipeline phase
func (ppln *AppInstallPipeline) addWorkersToPipelinePhase(ctx context.Context, phaseID enterpriseApi.AppPhaseType, workers ...*PipelineWorker) {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("addWorkersToPipelinePhase").WithValues("phase", phaseID)

	for _, worker := range workers {
		scopedLog.Info("Adding worker", "name", worker.cr.GetName(), "namespace", worker.cr.GetNamespace(), "Pod name", worker.targetPodName, "App name", worker.appDeployInfo.AppName, "digest", worker.appDeployInfo.ObjectHash)
	}
	ppln.pplnPhases[phaseID].mutex.Lock()
	ppln.pplnPhases[phaseID].q = append(ppln.pplnPhases[phaseID].q, workers...)
	ppln.pplnPhases[phaseID].mutex.Unlock()
}

// deleteWorkerFromPipelinePhase deletes a given worker from a pipeline phase
func (ppln *AppInstallPipeline) deleteWorkerFromPipelinePhase(ctx context.Context, phaseID enterpriseApi.AppPhaseType, worker *PipelineWorker) bool {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("deleteWorkerFromPipelinePhase").WithValues("phase", phaseID)
	ppln.pplnPhases[phaseID].mutex.Lock()
	defer ppln.pplnPhases[phaseID].mutex.Unlock()

	phaseQ := ppln.pplnPhases[phaseID].q
	for i, qWorker := range phaseQ {
		if worker == qWorker {
			if i != len(phaseQ)-1 {
				phaseQ[i] = phaseQ[len(phaseQ)-1]
			}
			phaseQ = phaseQ[:len(phaseQ)-1]
			ppln.pplnPhases[phaseID].q = phaseQ

			scopedLog.Info("Deleted worker", "name", worker.cr.GetName(), "namespace", worker.cr.GetNamespace(), "Pod name", worker.targetPodName, "phase", phaseID, "App name", worker.appDeployInfo.AppName, "digest", worker.appDeployInfo.ObjectHash)
			return true
		}
	}
	return false
}

// setContextForNewPhase sets the PhaseInfo to new phase
func setContextForNewPhase(phaseInfo *enterpriseApi.PhaseInfo, newPhase enterpriseApi.AppPhaseType) {
	phaseInfo.Phase = newPhase
	phaseInfo.FailCount = 0
	setPhaseStatusToPending(phaseInfo)
}

// makeWorkerInActive removes any pipeline specific context from the worker
func makeWorkerInActive(worker *PipelineWorker) {
	worker.isActive = false
	worker.waiter = nil
}

// createFanOutWorker creates a fan-out worker
func createFanOutWorker(seedWorker *PipelineWorker, ordinalIdx int) *PipelineWorker {
	if seedWorker == nil {
		return nil
	}

	if int32(ordinalIdx) >= *seedWorker.sts.Spec.Replicas {
		return nil
	}

	newWorker := &PipelineWorker{}
	*newWorker = *seedWorker
	newWorker.fanOut = false
	newWorker.targetPodName = getApplicablePodNameForAppFramework(seedWorker.cr, ordinalIdx)
	return newWorker
}

// transitionWorkerPhase transitions a worker to new phase, and deletes from the current phase
// In the case of Standalone CR with multiple replicas, Fan-out `replicas` number of new workers
func (ppln *AppInstallPipeline) transitionWorkerPhase(ctx context.Context, worker *PipelineWorker, currentPhase, nextPhase enterpriseApi.AppPhaseType) {

	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("transitionWorkerPhase").WithValues("name", worker.cr.GetName(), "namespace", worker.cr.GetNamespace(), "App name", worker.appDeployInfo.AppName, "digest", worker.appDeployInfo.ObjectHash, "pod name", worker.targetPodName, "current Phase", currentPhase, "next phase", nextPhase)

	var replicaCount int32
	if worker.sts != nil {
		replicaCount = *worker.sts.Spec.Replicas
	} else {
		replicaCount = 1
	}

	// Disable the  existing worker, so that either it can be safely transitioned to new pipeline or can act as a base for fan-out workers
	makeWorkerInActive(worker)

	// For now Standalone is the only CR unique with multiple replicas that is applicable for the AFW
	// If the replica count is more than 1, and if it is Standalone, when transitioning from
	// download phase, create a separate worker for the Pod copy(which also transition to install worker)

	// Also, for whatever reason(say, standalone reset, and that way it lost the app package), if the Standalone
	// switches to download phase, once the download phase is complete, it will safely schedule a new pod copy worker,
	// without affecting other pods.
	appDeployInfo := worker.appDeployInfo
	if worker.fanOut {
		scopedLog.Info("Fan-out transition")
		if currentPhase == enterpriseApi.PhaseDownload {
			// On a reconcile entry, processing the Standalone CR right after loading the appDeployContext from the CR status
			var podCopyWorkers, installWorkers []*PipelineWorker

			// Seems like the download just finished. Allocate Phase info
			if len(appDeployInfo.AuxPhaseInfo) == 0 {
				scopedLog.Info("Just finished the download phase")
				// Create Phase info for all the statefulset Pods.
				appDeployInfo.AuxPhaseInfo = make([]enterpriseApi.PhaseInfo, replicaCount)

				// Create a slice of corresponding worker nodes
				podCopyWorkers = make([]*PipelineWorker, replicaCount)

				//Create the Aux PhaseInfo for tracking all the Standalone Pods
				for podID := range appDeployInfo.AuxPhaseInfo {
					// Create a new copy worker
					podCopyWorkers[podID] = createFanOutWorker(worker, podID)

					setContextForNewPhase(&appDeployInfo.AuxPhaseInfo[podID], enterpriseApi.PhasePodCopy)
					scopedLog.Info("Created a new fan-out pod copy worker", "pod name", worker.targetPodName)
				}
			} else {
				scopedLog.Info("Installation was already in progress for replica members")

				for podID := range appDeployInfo.AuxPhaseInfo {
					phaseInfo := &appDeployInfo.AuxPhaseInfo[podID]
					if !isPhaseInfoEligibleForSchedulerEntry(ctx, worker.appSrcName, phaseInfo, worker.afwConfig) {
						continue
					}

					newWorker := createFanOutWorker(worker, podID)
					// reset the phase status
					setPhaseStatusToPending(phaseInfo)
					if phaseInfo.Phase == enterpriseApi.PhaseInstall {
						installWorkers = append(installWorkers, newWorker)
					} else if phaseInfo.Phase == enterpriseApi.PhasePodCopy {
						podCopyWorkers = append(podCopyWorkers, newWorker)
					} else {
						scopedLog.Error(nil, "invalid phase info detected", "phase", phaseInfo.Phase, "phase status", phaseInfo.Status)
					}
				}
			}

			ppln.addWorkersToPipelinePhase(ctx, enterpriseApi.PhasePodCopy, podCopyWorkers...)
			ppln.addWorkersToPipelinePhase(ctx, enterpriseApi.PhaseInstall, installWorkers...)
		} else {
			scopedLog.Error(nil, "Invalid phase detected")
		}

	} else {
		scopedLog.Info("Simple transition")
		var phaseInfo *enterpriseApi.PhaseInfo

		if isFanOutApplicableToCR(worker.cr) {
			podID, _ := getOrdinalValFromPodName(worker.targetPodName)
			phaseInfo = &worker.appDeployInfo.AuxPhaseInfo[podID]
		} else {
			phaseInfo = &appDeployInfo.PhaseInfo
		}

		setContextForNewPhase(phaseInfo, nextPhase)
		ppln.addWorkersToPipelinePhase(ctx, nextPhase, worker)
	}

	// We have already moved the worker(s) to the required queue.
	// Now, safely delete the worker from the current phase queue
	scopedLog.Info("Deleted worker", "phase", currentPhase)
	ppln.deleteWorkerFromPipelinePhase(ctx, currentPhase, worker)
}

// checkIfWorkerIsEligibleForRun confirms if the worker is eligible to run
func checkIfWorkerIsEligibleForRun(ctx context.Context, worker *PipelineWorker, phaseInfo *enterpriseApi.PhaseInfo, phaseStatus enterpriseApi.AppPhaseStatusType) bool {
	if !worker.isActive && !isPhaseMaxRetriesReached(ctx, phaseInfo, worker.afwConfig) &&
		phaseInfo.Status != phaseStatus {
		return true
	}

	return false
}

// needToUseAuxPhaseInfo confirms if aux phase info to be used
// currently applicable only for Standalone deployment
func needToUseAuxPhaseInfo(worker *PipelineWorker, phaseType enterpriseApi.AppPhaseType) bool {
	if phaseType != enterpriseApi.PhaseDownload && isFanOutApplicableToCR(worker.cr) {
		return true
	}

	return false
}

// getPhaseInfoByPhaseType gives the phase info suitable for a given phase
func getPhaseInfoByPhaseType(ctx context.Context, worker *PipelineWorker, phaseType enterpriseApi.AppPhaseType) *enterpriseApi.PhaseInfo {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("getPhaseInfoFromWorker")

	if needToUseAuxPhaseInfo(worker, phaseType) {
		podID, err := getOrdinalValFromPodName(worker.targetPodName)
		if err != nil {
			scopedLog.Error(err, "unable to get the pod Id", "pod name", worker.targetPodName)
			return nil
		}

		return &worker.appDeployInfo.AuxPhaseInfo[podID]
	}

	return &worker.appDeployInfo.PhaseInfo
}

// updatePplnWorkerPhaseInfo updates the in-memory PhaseInfo(specifically status and retryCount)
func updatePplnWorkerPhaseInfo(ctx context.Context, appDeployInfo *enterpriseApi.AppDeploymentInfo, failCount uint32, statusType enterpriseApi.AppPhaseStatusType) {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("updatePplnWorkerPhaseInfo").WithValues("appName", appDeployInfo.AppName)

	scopedLog.Info("changing the status", "old status", appPhaseStatusAsStr(appDeployInfo.PhaseInfo.Status), "new status", appPhaseStatusAsStr(statusType))
	appDeployInfo.PhaseInfo.FailCount = failCount
	appDeployInfo.PhaseInfo.Status = statusType
}

func (downloadWorker *PipelineWorker) createDownloadDirOnOperator(ctx context.Context) (string, error) {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("createDownloadDirOnOperator").WithValues("appSrcName", downloadWorker.appSrcName, "appName", downloadWorker.appDeployInfo.AppName)
	scope := getAppSrcScope(ctx, downloadWorker.afwConfig, downloadWorker.appSrcName)

	kind := downloadWorker.cr.GetObjectKind().GroupVersionKind().Kind

	// This is how the path to download apps looks like -
	// /opt/splunk/appframework/downloadedApps/<namespace>/<CR_Kind>/<CR_Name>/<scope>/<appSrc_Name>/
	// For e.g., if the we are trying to download app app1.tgz under "admin" app source name, for a Standalone CR with name "stand1"
	// in default namespace, then it will be downloaded at the path -
	// /opt/splunk/appframework/downloadedApps/default/Standalone/stand1/local/admin/app1.tgz_<hash>
	localPath := filepath.Join(splcommon.AppDownloadVolume, "downloadedApps", downloadWorker.cr.GetNamespace(), kind, downloadWorker.cr.GetName(), scope, downloadWorker.appSrcName) + "/"
	// create the sub-directories on the volume for downloading scoped apps
	err := createAppDownloadDir(ctx, localPath)
	if err != nil {
		scopedLog.Error(err, "unable to create app download directory on operator")
	}
	return localPath, err
}

// download API will do the actual work of downloading apps from remote storage
func (downloadWorker *PipelineWorker) download(ctx context.Context, pplnPhase *PipelinePhase, s3ClientMgr S3ClientManager, localPath string, downloadWorkersRunPool chan struct{}) {

	defer func() {
		downloadWorker.isActive = false

		<-downloadWorkersRunPool
		// decrement the waiter count
		downloadWorker.waiter.Done()
	}()

	splunkCR := downloadWorker.cr
	appSrcName := downloadWorker.appSrcName
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("PipelineWorker.Download()").WithValues("name", splunkCR.GetName(), "namespace", splunkCR.GetNamespace(), "App name", downloadWorker.appDeployInfo.AppName, "objectHash", downloadWorker.appDeployInfo.ObjectHash)

	appDeployInfo := downloadWorker.appDeployInfo
	appName := appDeployInfo.AppName

	localFile := getLocalAppFileName(ctx, localPath, appName, appDeployInfo.ObjectHash)
	remoteFile, err := getRemoteObjectKey(ctx, splunkCR, downloadWorker.afwConfig, appSrcName, appName)
	if err != nil {
		scopedLog.Error(err, "unable to get remote object key", "appName", appName)
		// increment the retry count and mark this app as download pending
		updatePplnWorkerPhaseInfo(ctx, appDeployInfo, appDeployInfo.PhaseInfo.FailCount+1, enterpriseApi.AppPkgDownloadPending)

		return
	}

	// download the app from remote storage
	err = s3ClientMgr.DownloadApp(ctx, remoteFile, localFile, appDeployInfo.ObjectHash)
	if err != nil {
		scopedLog.Error(err, "unable to download app", "appName", appName)

		// remove the local file
		err = os.RemoveAll(localFile)
		if err != nil {
			scopedLog.Error(err, "unable to remove local file from operator")
		}

		// increment the retry count and mark this app as download pending
		updatePplnWorkerPhaseInfo(ctx, appDeployInfo, appDeployInfo.PhaseInfo.FailCount+1, enterpriseApi.AppPkgDownloadPending)
		return
	}

	// download is successfull, update the state and reset the retry count
	updatePplnWorkerPhaseInfo(ctx, appDeployInfo, 0, enterpriseApi.AppPkgDownloadComplete)

	scopedLog.Info("Finished downloading app")
}

// downloadWorkerHandler schedules the download workers to download app/s
func (pplnPhase *PipelinePhase) downloadWorkerHandler(ctx context.Context, ppln *AppInstallPipeline, maxWorkers uint64, scheduleDownloadsWaiter *sync.WaitGroup) {

	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("downloadWorkerHandler")

	// derive a counting semaphore from the channel to represent worker run pool
	var downloadWorkersRunPool = make(chan struct{}, maxWorkers)

downloadWork:
	for {
		select {
		// get an idle worker
		case downloadWorkersRunPool <- struct{}{}:
			select {
			case downloadWorker, ok := <-pplnPhase.msgChannel:
				// if channel is closed, then just break from here as we have nothing to read
				if !ok {
					scopedLog.Info("msgChannel is closed by downloadPhaseManager, hence nothing to read.")
					break downloadWork
				}

				// do not redownload the app if it is already downloaded
				if isAppAlreadyDownloaded(ctx, downloadWorker) {
					scopedLog.Info("app is already downloaded on operator pod, hence skipping it.", "appSrcName", downloadWorker.appSrcName, "appName", downloadWorker.appDeployInfo.AppName)
					// update the state to be download complete
					updatePplnWorkerPhaseInfo(ctx, downloadWorker.appDeployInfo, 0, enterpriseApi.AppPkgDownloadComplete)
					<-downloadWorkersRunPool
					continue
				}

				// do not proceed if we dont have enough disk space to download this app

				err := reserveStorage(downloadWorker.appDeployInfo.Size)
				if err != nil {
					scopedLog.Error(err, "insufficient storage for the app pkg download. appSrcName: %s, app name: %s, app size: %d Bytes", downloadWorker.appSrcName, downloadWorker.appDeployInfo.AppName, downloadWorker.appDeployInfo.Size)
					// setting isActive to false here so that downloadPhaseManager can take care of it.
					downloadWorker.isActive = false
					<-downloadWorkersRunPool
					continue
				}

				// increment the count in worker waitgroup
				downloadWorker.waiter.Add(1)

				// update the download state of app to be DownloadInProgress
				updatePplnWorkerPhaseInfo(ctx, downloadWorker.appDeployInfo, downloadWorker.appDeployInfo.PhaseInfo.FailCount, enterpriseApi.AppPkgDownloadInProgress)

				appDeployInfo := downloadWorker.appDeployInfo

				// create the sub-directories on the volume for downloading scoped apps
				localPath, err := downloadWorker.createDownloadDirOnOperator(ctx)
				if err != nil {

					// increment the retry count and mark this app as download pending
					updatePplnWorkerPhaseInfo(ctx, appDeployInfo, appDeployInfo.PhaseInfo.FailCount+1, enterpriseApi.AppPkgDownloadPending)

					<-downloadWorkersRunPool
					continue
				}

				// get the S3ClientMgr instance
				s3ClientMgr, _ := getS3ClientMgr(ctx, downloadWorker.client, downloadWorker.cr, downloadWorker.afwConfig, downloadWorker.appSrcName)

				// start the actual download
				go downloadWorker.download(ctx, pplnPhase, *s3ClientMgr, localPath, downloadWorkersRunPool)

			default:
				<-downloadWorkersRunPool
			}
		default:
			// All the workers are busy, check after one second
			scopedLog.Info("All the workers are busy, we will check again after one second")
			time.Sleep(1 * time.Second)
		}

		time.Sleep(200 * time.Millisecond)
	}

	// wait for all the download threads to finish
	pplnPhase.workerWaiter.Wait()

	// we are done processing download jobs
	scheduleDownloadsWaiter.Done()
}

// shutdownPipelinePhase does the following things as part of the cleanup phase:
// 1. close the msg channel
// 2. wait for the handler to finish all its work
// 3. mark the phase as done/complete
func (ppln *AppInstallPipeline) shutdownPipelinePhase(ctx context.Context, phaseManager string, pplnPhase *PipelinePhase, perPhaseWaiter *sync.WaitGroup) {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName(phaseManager)

	// close the msgChannel
	close(pplnPhase.msgChannel)

	// wait for the handler code to finish its work
	scopedLog.Info("Waiting for the workers to finish")
	perPhaseWaiter.Wait()

	// mark the phase as done/complete
	scopedLog.Info("All the workers finished")
	ppln.phaseWaiter.Done()
}

//downloadPhaseManager creates download phase manager for the install pipeline
func (ppln *AppInstallPipeline) downloadPhaseManager(ctx context.Context) {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("downloadPhaseManager")
	scopedLog.Info("Starting Download phase manager")

	pplnPhase := ppln.pplnPhases[enterpriseApi.PhaseDownload]

	maxWorkers := ppln.appDeployContext.AppsStatusMaxConcurrentAppDownloads

	scheduleDownloadsWaiter := new(sync.WaitGroup)

	scheduleDownloadsWaiter.Add(1)
	// schedule the download threads to do actual download work
	go pplnPhase.downloadWorkerHandler(ctx, ppln, maxWorkers, scheduleDownloadsWaiter)
	defer func() {
		ppln.shutdownPipelinePhase(ctx, string(enterpriseApi.PhaseDownload), pplnPhase, scheduleDownloadsWaiter)
	}()

downloadPhase:
	for {
		select {
		case _, channelOpen := <-ppln.sigTerm:
			if !channelOpen {
				scopedLog.Info("Received the termination request from the scheduler")
				break downloadPhase
			}

		default:
			for _, downloadWorker := range pplnPhase.q {
				phaseInfo := getPhaseInfoByPhaseType(ctx, downloadWorker, enterpriseApi.PhaseDownload)
				if isPhaseMaxRetriesReached(ctx, phaseInfo, downloadWorker.afwConfig) {

					downloadWorker.appDeployInfo.PhaseInfo.Status = enterpriseApi.AppPkgDownloadError
					ppln.deleteWorkerFromPipelinePhase(ctx, phaseInfo.Phase, downloadWorker)
				} else if isPhaseStatusComplete(phaseInfo) {
					ppln.transitionWorkerPhase(ctx, downloadWorker, enterpriseApi.PhaseDownload, enterpriseApi.PhasePodCopy)
				} else if checkIfWorkerIsEligibleForRun(ctx, downloadWorker, phaseInfo, enterpriseApi.AppPkgDownloadComplete) {
					downloadWorker.waiter = &pplnPhase.workerWaiter
					select {
					case pplnPhase.msgChannel <- downloadWorker:
						scopedLog.Info("Download worker got a run slot", "name", downloadWorker.cr.GetName(), "namespace", downloadWorker.cr.GetNamespace(), "App name", downloadWorker.appDeployInfo.AppName, "digest", downloadWorker.appDeployInfo.ObjectHash)
						downloadWorker.isActive = true
					default:
						downloadWorker.waiter = nil
					}
				}
			}
		}

		time.Sleep(200 * time.Millisecond)
	}
}

// runPlaybook implements the playbook for local scoped app install
func (ctx *localScopePlaybookContext) runPlaybook(rctx context.Context) error {
	worker := ctx.worker
	cr := worker.cr
	reqLogger := log.FromContext(rctx)
	scopedLog := reqLogger.WithName("localScopeInstallContext.runPlaybook").WithValues("name", cr.GetName(), "namespace", cr.GetNamespace(), "pod", worker.targetPodName, "app name", worker.appDeployInfo.AppName)

	defer func() {
		<-ctx.sem
		worker.isActive = false
		worker.waiter.Done()
	}()

	// if the app name is app1.tgz and hash is "abcd1234", then appPkgFileName is app1.tgz_abcd1234
	appPkgFileName := getAppPackageName(worker)

	// if appsrc is "appSrc1", then appPkgPathOnPod is /operator-staging/appframework/appSrc1/app1.tgz_abcd1234
	appPkgPathOnPod := filepath.Join(appBktMnt, worker.appSrcName, appPkgFileName)

	phaseInfo := getPhaseInfoByPhaseType(rctx, worker, enterpriseApi.PhaseInstall)

	if !checkIfFileExistsOnPod(rctx, cr, appPkgPathOnPod, ctx.podExecClient) {
		scopedLog.Error(nil, "app pkg missing on Pod", "app pkg path", appPkgPathOnPod)
		phaseInfo.Status = enterpriseApi.AppPkgMissingOnPodError

		return fmt.Errorf("app pkg missing on Pod. app pkg path: %s", appPkgPathOnPod)
	}

	var command string
	if worker.appDeployInfo.IsUpdate {
		// App was already installed, update scenario
		command = fmt.Sprintf("/opt/splunk/bin/splunk install app %s -update 1 -auth admin:`cat /mnt/splunk-secrets/password`", appPkgPathOnPod)
	} else {
		command = fmt.Sprintf("/opt/splunk/bin/splunk install app %s -auth admin:`cat /mnt/splunk-secrets/password`", appPkgPathOnPod)
	}

	streamOptions := splutil.NewStreamOptionsObject(command)

	stdOut, stdErr, err := ctx.podExecClient.RunPodExecCommand(rctx, streamOptions, []string{"/bin/sh"})
	// if the app was already installed previously, then just mark it for install complete
	if stdErr != "" || err != nil {
		phaseInfo.FailCount++
		scopedLog.Error(err, "local scoped app package install failed", "stdout", stdOut, "stderr", stdErr, "app pkg path", appPkgPathOnPod, "failCount", phaseInfo.FailCount)
		return fmt.Errorf("local scoped app package install failed. stdOut: %s, stdErr: %s, app pkg path: %s, failCount: %d", stdOut, stdErr, appPkgPathOnPod, phaseInfo.FailCount)
	}

	// Mark the worker for install complete status
	scopedLog.Info("App pkg installation complete")
	phaseInfo.Status = enterpriseApi.AppPkgInstallComplete
	phaseInfo.FailCount = 0

	// Delete the app package from the target pod /operator-staging/appframework/ location
	command = fmt.Sprintf("rm -f %s", appPkgPathOnPod)
	streamOptions = splutil.NewStreamOptionsObject(command)
	stdOut, stdErr, err = ctx.podExecClient.RunPodExecCommand(rctx, streamOptions, []string{"/bin/sh"})
	if stdErr != "" || err != nil {
		scopedLog.Error(err, "app pkg deletion failed", "stdout", stdOut, "stderr", stdErr, "app pkg path", appPkgPathOnPod)
		return fmt.Errorf("app pkg deletion failed.  stdOut: %s, stdErr: %s, app pkg path: %s", stdOut, stdErr, appPkgPathOnPod)
	}

	// Try to remove the app package from the Operator Pod
	tryAppPkgCleanupFromOperatorPod(rctx, worker)

	return nil
}

// extractClusterScopedAppOnPod untars the given app package to the bundle push location
func extractClusterScopedAppOnPod(ctx context.Context, worker *PipelineWorker, appSrcScope string, appPkgPathOnPod, appPkgLocalPath string, podExecClient splutil.PodExecClientImpl) error {
	cr := worker.cr
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("extractClusterScopedAppOnPod").WithValues("name", cr.GetName(), "namespace", cr.GetNamespace(), "app name", worker.appDeployInfo.AppName)

	var stdOut, stdErr string
	var err error

	clusterAppsPath := getClusterScopedAppsLocOnPod(worker.cr)
	if clusterAppsPath == "" {
		// This should never happen
		scopedLog.Error(nil, "could not find the cluster scoped apps location on the Pod")
		return err
	}

	// untar the package to the cluster apps location, then delete it
	// ToDo: sgontla: cd, tar, and rm commands are trivial commands. packing together to avoid spanning multiple processes.
	// A better alternative is to maintain a script (that can give us the status of each command that we can map into a logical error, and copy if when needed.). Alternatively, we can mount it through a configMap
	command := fmt.Sprintf("cd %s; tar -xzf %s; rm -rf %s", clusterAppsPath, appPkgPathOnPod, appPkgPathOnPod)
	streamOptions := splutil.NewStreamOptionsObject(command)

	stdOut, stdErr, err = podExecClient.RunPodExecCommand(ctx, streamOptions, []string{"/bin/sh"})
	if stdErr != "" || err != nil {
		err = fmt.Errorf("app package untar & delete failed with stdErr = %s, stdOut=%s, err=%v", stdErr, stdOut, err)
		return err
	}

	// Now that the App package was moved to the persistent location on the Pod.
	// Remove the app package from the Operator storage area
	// Note:- local scoped app packages are removed once the installation is complete for entire statefulset
	deleteAppPkgFromOperator(ctx, worker)

	return err
}

// runPodCopyWorker runs one pod copy worker
func runPodCopyWorker(ctx context.Context, worker *PipelineWorker, ch chan struct{}) {
	cr := worker.cr
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("runPodCopyWorker").WithValues("name", cr.GetName(), "namespace", cr.GetNamespace(), "app name", worker.appDeployInfo.AppName, "pod", worker.targetPodName)
	defer func() {
		<-ch
		worker.isActive = false
		worker.waiter.Done()
	}()

	appPkgFileName := worker.appDeployInfo.AppName + "_" + worker.appDeployInfo.ObjectHash

	appSrcScope := getAppSrcScope(ctx, worker.afwConfig, worker.appSrcName)
	appPkgLocalDir := getAppPackageLocalDir(cr, appSrcScope, worker.appSrcName)
	appPkgLocalPath := appPkgLocalDir + appPkgFileName

	appPkgPathOnPod := filepath.Join(appBktMnt, worker.appSrcName, appPkgFileName)

	phaseInfo := getPhaseInfoByPhaseType(ctx, worker, enterpriseApi.PhasePodCopy)
	_, err := os.Stat(appPkgLocalPath)
	if err != nil {
		// Move the worker to download phase
		scopedLog.Error(err, "app package is missing", "pod name", worker.targetPodName)
		phaseInfo.Status = enterpriseApi.AppPkgMissingFromOperator
		return
	}

	// get the podExecClient to be used for copying file to pod
	podExecClient := splutil.GetPodExecClient(worker.client, cr, worker.targetPodName)
	stdOut, stdErr, err := CopyFileToPod(ctx, worker.client, cr.GetNamespace(), appPkgLocalPath, appPkgPathOnPod, podExecClient)
	if err != nil {
		phaseInfo.FailCount++
		scopedLog.Error(err, "app package pod copy failed", "stdout", stdOut, "stderr", stdErr, "failCount", phaseInfo.FailCount)
		return
	}

	if appSrcScope == enterpriseApi.ScopeCluster {
		err = extractClusterScopedAppOnPod(ctx, worker, appSrcScope, appPkgPathOnPod, appPkgLocalPath, podExecClient)
		if err != nil {
			phaseInfo.FailCount++
			scopedLog.Error(err, "extracting the app package on pod failed", "failCount", phaseInfo.FailCount)
			return
		}
	}

	scopedLog.Info("podCopy complete", "app pkg path", appPkgPathOnPod)
	phaseInfo.Status = enterpriseApi.AppPkgPodCopyComplete
}

// podCopyWorkerHandler fetches and runs the pod copy workers
func (pplnPhase *PipelinePhase) podCopyWorkerHandler(ctx context.Context, handlerWaiter *sync.WaitGroup, numPodCopyRunners int) {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("podCopyWorkerHandler")
	defer handlerWaiter.Done()

	// Using the channel, derive a counting semaphore called podCopyRunPool that represents worker run pool
	// Try to get an active worker by queuing a msg to podCopyRunPool. Once the worker finishes it drains a msg from the channel.
	// So, indirectly serving the counting semaphore functionality. At any point in time, only numPodCopyRunners workers can
	// be running, as that is the channel max. capacity.
	var podCopyWorkerPool = make(chan struct{}, numPodCopyRunners)

podCopyHandler:
	for {
		select {
		// get an idle worker
		case podCopyWorkerPool <- struct{}{}:
			select {
			case worker, channelOpen := <-pplnPhase.msgChannel:
				if !channelOpen {
					// Channel is closed, so, do not handle any more workers
					scopedLog.Info("worker channel closed")
					break podCopyHandler
				}

				if worker != nil {
					worker.waiter.Add(1)
					go runPodCopyWorker(ctx, worker, podCopyWorkerPool)
				} else {
					/// This should never happen
					scopedLog.Error(nil, "invalid worker reference")
					<-podCopyWorkerPool
				}

			default:
				<-podCopyWorkerPool
			}
		default:
			// All the workers are busy, check after one second
			time.Sleep(1 * time.Second)
		}

		time.Sleep(200 * time.Millisecond)
	}

	// Wait for all the workers to finish
	scopedLog.Info("Waiting for all the workers to finish")
	pplnPhase.workerWaiter.Wait()
	scopedLog.Info("All the workers finished")
}

// podCopyPhaseManager creates pod copy phase manager for the install pipeline
func (ppln *AppInstallPipeline) podCopyPhaseManager(ctx context.Context) {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("podCopyPhaseManager")
	scopedLog.Info("Starting Pod copy phase manager")
	var handlerWaiter sync.WaitGroup

	pplnPhase := ppln.pplnPhases[enterpriseApi.PhasePodCopy]

	// Start podCopy worker handler
	// workerWaiter is used to wait for both the podCopyWorkerHandler and all of its children as they are all correlated
	// For now, for the number of parallel pod copy, use the max. concurrent downloads. Standalone is something unique, but at the same time
	// limited by the Operator n/w bw, so hopefullye its Ok.
	handlerWaiter.Add(1)
	go pplnPhase.podCopyWorkerHandler(ctx, &handlerWaiter, int(ppln.appDeployContext.AppsStatusMaxConcurrentAppDownloads))
	defer func() {
		ppln.shutdownPipelinePhase(ctx, string(enterpriseApi.PhasePodCopy), pplnPhase, &handlerWaiter)
	}()

podCopyPhase:
	for {
		select {
		case _, channelOpen := <-ppln.sigTerm:
			if !channelOpen {
				scopedLog.Info("Received the termination request from the scheduler")
				break podCopyPhase
			}

		default:
			for _, podCopyWorker := range pplnPhase.q {
				phaseInfo := getPhaseInfoByPhaseType(ctx, podCopyWorker, enterpriseApi.PhasePodCopy)
				if isPhaseMaxRetriesReached(ctx, phaseInfo, podCopyWorker.afwConfig) {

					podCopyWorker.appDeployInfo.PhaseInfo.Status = enterpriseApi.AppPkgPodCopyError
					ppln.deleteWorkerFromPipelinePhase(ctx, phaseInfo.Phase, podCopyWorker)
				} else if isPhaseStatusComplete(phaseInfo) {
					// For cluster scoped apps, just delete the worker. install handler will trigger the bundle push
					if enterpriseApi.ScopeCluster != getAppSrcScope(ctx, podCopyWorker.afwConfig, podCopyWorker.appSrcName) {
						ppln.transitionWorkerPhase(ctx, podCopyWorker, enterpriseApi.PhasePodCopy, enterpriseApi.PhaseInstall)
					} else {
						ppln.deleteWorkerFromPipelinePhase(ctx, phaseInfo.Phase, podCopyWorker)
					}
				} else if phaseInfo.Status == enterpriseApi.AppPkgMissingFromOperator {
					ppln.transitionWorkerPhase(ctx, podCopyWorker, enterpriseApi.PhasePodCopy, enterpriseApi.PhaseDownload)
				} else if checkIfWorkerIsEligibleForRun(ctx, podCopyWorker, phaseInfo, enterpriseApi.AppPkgPodCopyComplete) {
					podCopyWorker.waiter = &pplnPhase.workerWaiter
					select {
					case pplnPhase.msgChannel <- podCopyWorker:
						scopedLog.Info("Pod copy worker got a run slot", "name", podCopyWorker.cr.GetName(), "namespace", podCopyWorker.cr.GetNamespace(), "pod name", podCopyWorker.targetPodName, "App name", podCopyWorker.appDeployInfo.AppName, "digest", podCopyWorker.appDeployInfo.ObjectHash)
						podCopyWorker.isActive = true
					default:
						podCopyWorker.waiter = nil
					}
				}
			}
		}

		time.Sleep(200 * time.Millisecond)
	}
}

// getInstallSlotForPod tries to allocate a local scoped install slot for a pod
func getInstallSlotForPod(ctx context.Context, installTracker []chan struct{}, podName string) bool {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("getInstallSlotForPod")
	podID, err := getOrdinalValFromPodName(podName)
	if err != nil {
		scopedLog.Error(err, "unable to derive podId for podname", podName)
		return false
	}

	select {
	case installTracker[podID] <- struct{}{}:
		return true
	default:
		return false
	}
}

// freeInstallSlotForPod frees up an install slot for a pod
func freeInstallSlotForPod(ctx context.Context, installTracker []chan struct{}, podName string) {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("freeInstallSlotForPod")
	podID, err := getOrdinalValFromPodName(podName)
	if err != nil {
		scopedLog.Error(err, "unable to derive podId for podname", podName)
		return
	}

	select {
	case <-installTracker[podID]:
	default:
		scopedLog.Error(nil, "trying to free an install slot without even allocating it")
	}
}

// isPendingClusterScopeWork confirms if there is any pending cluster scoped app work
func isPendingClusterScopeWork(afwPipeline *AppInstallPipeline) bool {
	// CR doesn't deal with cluster scoped apps
	if !isClusterScoped(afwPipeline.cr.GetObjectKind().GroupVersionKind().Kind) {
		return false
	}

	// There are no cluster scoped apps pending for bundle push
	if afwPipeline.appDeployContext.BundlePushStatus.BundlePushStage == enterpriseApi.BundlePushComplete || afwPipeline.appDeployContext.BundlePushStatus.BundlePushStage == enterpriseApi.BundlePushUninitialized {
		return false
	}

	return true
}

// needToRunClusterScopedPlaybook confirms if the cluster scoped playbooks to be run
func needToRunClusterScopedPlaybook(afwPipeline *AppInstallPipeline) bool {
	if !isPendingClusterScopeWork(afwPipeline) {
		return false
	}

	// Its already time to yield the current reconcile
	if afwPipeline.afwEntryTime+int64(afwPipeline.appDeployContext.AppFrameworkConfig.SchedulerYieldInterval) < time.Now().Unix() {
		return false
	}

	return true
}

// tryAppPkgCleanupFromOperatorPod tries to change the app install status, also cleans the app pkg from Operator Pod
func tryAppPkgCleanupFromOperatorPod(ctx context.Context, installWorker *PipelineWorker) {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("tryAppPkgCleanupFromOperatorPod")

	if isFanOutApplicableToCR(installWorker.cr) {
		if isAppInstallationCompleteOnAllReplicas(installWorker.appDeployInfo.AuxPhaseInfo) {
			scopedLog.Info("app pkg installed on all the pods", "app pkg", installWorker.appDeployInfo.AppName)
			installWorker.appDeployInfo.PhaseInfo.Phase = enterpriseApi.PhaseInstall
			installWorker.appDeployInfo.PhaseInfo.Status = enterpriseApi.AppPkgInstallComplete

			//For now, set the deploy status as complete. Eventually, we can phase it out
			installWorker.appDeployInfo.DeployStatus = enterpriseApi.DeployStatusComplete
			deleteAppPkgFromOperator(ctx, installWorker)
		}
	} else {
		deleteAppPkgFromOperator(ctx, installWorker)
	}
}

// installWorkerHandler fetches and runs the install workers
// local scope installs are handled first, then the cluster scoped apps are considered for bundle push
func (pplnPhase *PipelinePhase) installWorkerHandler(ctx context.Context, ppln *AppInstallPipeline, handlerWaiter *sync.WaitGroup, installTracker []chan struct{}) {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("installWorkerHandler")
	defer handlerWaiter.Done()

installHandler:
	for {
		select {
		case installWorker, channelOpen := <-pplnPhase.msgChannel:
			if !channelOpen {
				// Channel is closed, so, do not handle any more workers
				scopedLog.Info("worker channel closed")
				break installHandler
			}

			if installWorker != nil {
				podExecClient := splutil.GetPodExecClient(installWorker.client, installWorker.cr, installWorker.targetPodName)
				podID, _ := getOrdinalValFromPodName(installWorker.targetPodName)

				ctxt := getLocalScopePlaybookContext(ctx, installWorker, installTracker[podID], podExecClient)
				if ctxt != nil {
					installWorker.waiter.Add(1)
					go ctxt.runPlaybook(ctx)
				} else {
					<-installTracker[podID]
					scopedLog.Error(nil, "unable to get the local scoped context. app name %s", installWorker.appDeployInfo.AppName)
				}
			} else {
				// This should never happen
				scopedLog.Error(nil, "invalid worker reference")
			}

		default:
			time.Sleep(1 * time.Second)
		}

		time.Sleep(200 * time.Millisecond)
	}

	for {
		if needToRunClusterScopedPlaybook(ppln) {
			targetPodName := getApplicablePodNameForAppFramework(ppln.cr, 0)
			podExecClient := splutil.GetPodExecClient(ppln.client, ppln.cr, targetPodName)

			// sgontla: can we just pass the CR???
			ctxt := getClusterScopePlaybookContext(ctx, ppln.client, ppln.cr, ppln, targetPodName, ppln.cr.GetObjectKind().GroupVersionKind().Kind, podExecClient)
			if ctxt != nil {
				ctxt.runPlaybook(ctx)
			} else {
				scopedLog.Error(nil, "unable to get the cluster scoped playbook context, kind: %s, name: %s", ppln.cr.GroupVersionKind().Kind, ppln.cr.GetName())
			}
		} else {
			break
		}

		// Sleep for a second before retry
		time.Sleep(1 * time.Second)
	}

	// Wait for all the workers to finish
	scopedLog.Info("Waiting for all the workers to finish")
	pplnPhase.workerWaiter.Wait()
	scopedLog.Info("All the workers finished")
}

// installPhaseManager creates install phase manager for the afw installation pipeline
func (ppln *AppInstallPipeline) installPhaseManager(ctx context.Context) {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("installPhaseManager")
	scopedLog.Info("Starting Install phase manager")

	var handlerWaiter sync.WaitGroup

	pplnPhase := ppln.pplnPhases[enterpriseApi.PhaseInstall]

	// Unlike other phases of the pipeline, Install phase is constrained by number of parallel installs per pod(ideally one)
	// So, pick an install worker, if and only if there is no active install going on for a given pod.
	// installWorkerPool is used to track the active installs for a given CR replica members
	// Note:- In future, it will be much simplified to use a trylock(sync package mutex supports trylock in version 1.18)
	replicas := *ppln.sts.Spec.Replicas

	podInstallTracker := make([]chan struct{}, replicas)
	for i := range podInstallTracker {
		podInstallTracker[i] = make(chan struct{}, maxParallelInstallsPerPod)
	}

	// Set the msgChannel that matches the installWorkerPool size.
	pplnPhase.msgChannel = make(chan *PipelineWorker, replicas)

	// Start install worker handler
	// workerWaiter is used to wait for both the installWorkerHandler and all of its children as they are all correlated
	handlerWaiter.Add(1)
	go pplnPhase.installWorkerHandler(ctx, ppln, &handlerWaiter, podInstallTracker)
	defer func() {
		ppln.shutdownPipelinePhase(ctx, string(enterpriseApi.PhaseInstall), pplnPhase, &handlerWaiter)
	}()

installPhase:
	for {
		select {
		case _, channelOpen := <-ppln.sigTerm:
			if !channelOpen {
				scopedLog.Info("Received the termination request from the scheduler")
				break installPhase
			}

		default:
			for _, installWorker := range pplnPhase.q {
				appScope := getAppSrcScope(ctx, installWorker.afwConfig, installWorker.appSrcName)
				if enterpriseApi.ScopeLocal != appScope {
					scopedLog.Error(nil, "Install worker with non-local scope", "name", installWorker.cr.GetName(), "namespace", installWorker.cr.GetNamespace(), "pod name", installWorker.targetPodName, "App name", installWorker.appDeployInfo.AppName, "digest", installWorker.appDeployInfo.ObjectHash, "scope", appScope)
					continue
				}

				phaseInfo := getPhaseInfoByPhaseType(ctx, installWorker, enterpriseApi.PhaseInstall)
				if isPhaseMaxRetriesReached(ctx, phaseInfo, installWorker.afwConfig) {

					phaseInfo.Status = enterpriseApi.AppPkgInstallError
					ppln.deleteWorkerFromPipelinePhase(ctx, phaseInfo.Phase, installWorker)
				} else if isPhaseStatusComplete(phaseInfo) {
					ppln.deleteWorkerFromPipelinePhase(ctx, phaseInfo.Phase, installWorker)
				} else if phaseInfo.Status == enterpriseApi.AppPkgMissingOnPodError {
					ppln.transitionWorkerPhase(ctx, installWorker, enterpriseApi.PhaseInstall, enterpriseApi.PhasePodCopy)
				} else if checkIfWorkerIsEligibleForRun(ctx, installWorker, phaseInfo, enterpriseApi.AppPkgInstallComplete) &&
					getInstallSlotForPod(ctx, podInstallTracker, installWorker.targetPodName) {
					installWorker.waiter = &pplnPhase.workerWaiter
					select {
					case pplnPhase.msgChannel <- installWorker:
						scopedLog.Info("Install worker got a run slot", "name", installWorker.cr.GetName(), "namespace", installWorker.cr.GetNamespace(), "pod name", installWorker.targetPodName, "App name", installWorker.appDeployInfo.AppName, "digest", installWorker.appDeployInfo.ObjectHash)

						// Always set the isActive in Phase manager itself, to avoid any delay in the install handler, otherwise it can
						// cause running the same playbook multiple times.
						installWorker.isActive = true

					default:
						freeInstallSlotForPod(ctx, podInstallTracker, installWorker.targetPodName)
						installWorker.waiter = nil
					}
				}
			}
		}

		time.Sleep(200 * time.Millisecond)
	}
}

// resetPhaseStatusToPending sets the phase status to pending
func setPhaseStatusToPending(phaseInfo *enterpriseApi.PhaseInfo) {
	switch phaseInfo.Phase {
	case enterpriseApi.PhaseDownload:
		phaseInfo.Status = enterpriseApi.AppPkgDownloadPending
	case enterpriseApi.PhasePodCopy:
		phaseInfo.Status = enterpriseApi.AppPkgPodCopyPending
	case enterpriseApi.PhaseInstall:
		phaseInfo.Status = enterpriseApi.AppPkgInstallPending
	}
}

// isPhaseStatusComplete confirms if the given Phase status is complete or not
func isPhaseStatusComplete(phaseInfo *enterpriseApi.PhaseInfo) bool {
	switch phaseInfo.Phase {
	case enterpriseApi.PhaseDownload:
		return phaseInfo.Status == enterpriseApi.AppPkgDownloadComplete
	case enterpriseApi.PhasePodCopy:
		return phaseInfo.Status == enterpriseApi.AppPkgPodCopyComplete
	case enterpriseApi.PhaseInstall:
		return phaseInfo.Status == enterpriseApi.AppPkgInstallComplete
	default:
		return false
	}
}

// isPhaseMaxRetriesReached confirms if the max retries reached
func isPhaseMaxRetriesReached(ctx context.Context, phaseInfo *enterpriseApi.PhaseInfo, afwConfig *enterpriseApi.AppFrameworkSpec) bool {
	return (afwConfig.PhaseMaxRetries < phaseInfo.FailCount)
}

// isPipelineEmpty checks if the pipeline is empty or not
func (ppln *AppInstallPipeline) isPipelineEmpty() bool {
	if ppln.pplnPhases == nil {
		return true
	}

	for _, phase := range ppln.pplnPhases {
		if len(phase.q) > 0 {
			return false
		}
	}
	return true
}

// isAppInstallationCompleteOnAllReplicas confirms if an app package is installed on all the Standalone Pods or not
func isAppInstallationCompleteOnAllReplicas(auxPhaseInfo []enterpriseApi.PhaseInfo) bool {
	for _, phaseInfo := range auxPhaseInfo {
		if phaseInfo.Phase != enterpriseApi.PhaseInstall || phaseInfo.Status != enterpriseApi.AppPkgInstallComplete {
			return false
		}
	}

	return true
}

// isClusterScoped checks whether current cr is a SHC or a CM
func isClusterScoped(kind string) bool {
	return kind == "ClusterMaster" || kind == "SearchHeadCluster"
}

// checkIfBundlePushIsDone checks if the bundle push is done, if there are cluster scoped apps
func checkIfBundlePushIsDone(kind string, bundlePushState enterpriseApi.BundlePushStageType) bool {
	if !isClusterScoped(kind) || bundlePushState == enterpriseApi.BundlePushComplete {
		return true
	}
	return false
}

// initPipelinePhase initializes a given pipeline phase
func initPipelinePhase(afwPipeline *AppInstallPipeline, phase enterpriseApi.AppPhaseType) {
	afwPipeline.pplnPhases[phase] = &PipelinePhase{
		q:          []*PipelineWorker{},
		msgChannel: make(chan *PipelineWorker, 1),
	}
}

// initAppInstallPipeline creates the AFW scheduler pipelines
func initAppInstallPipeline(ctx context.Context, appDeployContext *enterpriseApi.AppDeploymentContext, client splcommon.ControllerClient, cr splcommon.MetaObject) *AppInstallPipeline {

	afwPipeline := &AppInstallPipeline{}
	afwPipeline.pplnPhases = make(map[enterpriseApi.AppPhaseType]*PipelinePhase, 3)
	afwPipeline.sigTerm = make(chan struct{})
	afwPipeline.appDeployContext = appDeployContext
	afwPipeline.afwEntryTime = time.Now().Unix()
	afwPipeline.cr = cr
	afwPipeline.client = client
	afwPipeline.sts = afwGetReleventStatefulsetByKind(ctx, cr, client)

	// Allocate the Download phase
	initPipelinePhase(afwPipeline, enterpriseApi.PhaseDownload)

	// Allocate the Pod Copy phase
	initPipelinePhase(afwPipeline, enterpriseApi.PhasePodCopy)

	// Allocate the install phase
	initPipelinePhase(afwPipeline, enterpriseApi.PhaseInstall)

	return afwPipeline
}

// deleteAppPkgFromOperator removes the app pkg from the Operator Pod
func deleteAppPkgFromOperator(ctx context.Context, worker *PipelineWorker) {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("deleteAppPkgFromOperator").WithValues("name", worker.cr.GetName(), "namespace", worker.cr.GetNamespace(), "app pkg", worker.appDeployInfo.AppName)

	appPkgLocalPath := getAppPackageLocalPath(ctx, worker)
	err := os.Remove(appPkgLocalPath)
	if err != nil {
		// Issue is local, so just log an error msg and return
		// ToDo: sgontla: For any transient errors, handle the clean-up at the end of the install
		scopedLog.Error(err, "failed to delete app pkg from Operator", "app pkg path", appPkgLocalPath)
		return
	}

	releaseStorage(worker.appDeployInfo.Size)
}

func afwGetReleventStatefulsetByKind(ctx context.Context, cr splcommon.MetaObject, client splcommon.ControllerClient) *appsv1.StatefulSet {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("getReleventStatefulsetByKind").WithValues("name", cr.GetName(), "namespace", cr.GetNamespace())
	var instanceID InstanceType

	switch cr.GetObjectKind().GroupVersionKind().Kind {
	case "Standalone":
		instanceID = SplunkStandalone
	case "LicenseMaster":
		instanceID = SplunkLicenseManager
	case "SearchHeadCluster":
		instanceID = SplunkDeployer
	case "ClusterMaster":
		instanceID = SplunkClusterManager
	case "MonitoringConsole":
		instanceID = SplunkMonitoringConsole
	default:
		return nil
	}

	statefulsetName := GetSplunkStatefulsetName(instanceID, cr.GetName())
	namespacedName := types.NamespacedName{Namespace: cr.GetNamespace(), Name: statefulsetName}
	sts, err := splctrl.GetStatefulSetByName(ctx, client, namespacedName)
	if err != nil {
		scopedLog.Error(err, "Unable to get the stateful set")
	}

	return sts
}

// getIdxcPlaybookContext returns the idxc playbook context
func getIdxcPlaybookContext(ctx context.Context, client splcommon.ControllerClient, cr splcommon.MetaObject, afwPipeline *AppInstallPipeline, podName string, podExecClient splutil.PodExecClientImpl) *IdxcPlaybookContext {
	return &IdxcPlaybookContext{
		client:        client,
		cr:            cr,
		afwPipeline:   afwPipeline,
		targetPodName: podName,
		podExecClient: podExecClient,
	}
}

// getSHCPlaybookContext returns the shc playbook context
func getSHCPlaybookContext(ctx context.Context, client splcommon.ControllerClient, cr splcommon.MetaObject, afwPipeline *AppInstallPipeline, podName string, podExecClient splutil.PodExecClientImpl) *SHCPlaybookContext {
	return &SHCPlaybookContext{
		client:               client,
		cr:                   cr,
		afwPipeline:          afwPipeline,
		targetPodName:        podName,
		searchHeadCaptainURL: GetSplunkStatefulsetURL(cr.GetNamespace(), SplunkSearchHead, cr.GetName(), 0, false),
		podExecClient:        podExecClient,
	}
}

// getLocalScopePlaybookContext returns the local scoped app install playbook context
func getLocalScopePlaybookContext(ctx context.Context, installWorker *PipelineWorker, sem chan struct{}, podExecClient splutil.PodExecClientImpl) PlaybookImpl {
	return &localScopePlaybookContext{
		worker:        installWorker,
		sem:           sem,
		podExecClient: podExecClient,
	}
}

// getClusterScopePlaybookContext returns the context for running playbook
func getClusterScopePlaybookContext(ctx context.Context, client splcommon.ControllerClient, cr splcommon.MetaObject, afwPipeline *AppInstallPipeline, podName string, kind string, podExecClient splutil.PodExecClientImpl) PlaybookImpl {

	switch kind {
	case "ClusterMaster":
		return getIdxcPlaybookContext(ctx, client, cr, afwPipeline, podName, podExecClient)
	case "SearchHeadCluster":
		return getSHCPlaybookContext(ctx, client, cr, afwPipeline, podName, podExecClient)
	default:
		return nil
	}
}

// removeSHCBundlePushStatusFile removes the SHC Bundle status file from deployer pod
func (shcPlaybookContext *SHCPlaybookContext) removeSHCBundlePushStatusFile(ctx context.Context) error {
	var err error

	cmd := fmt.Sprintf("rm %s", shcBundlePushStatusCheckFile)
	streamOptions := splutil.NewStreamOptionsObject(cmd)

	_, stdErr, err := shcPlaybookContext.podExecClient.RunPodExecCommand(ctx, streamOptions, []string{"/bin/sh"})
	if stdErr != "" || err != nil {
		err = fmt.Errorf("unable to remove SHC Bundle Push status file due to stdErr=%s, err=%v", stdErr, err)
	}

	return err
}

// isBundlePushComplete checks whether the SHC bundle push is complete or still pending
func (shcPlaybookContext *SHCPlaybookContext) isBundlePushComplete(ctx context.Context) (bool, error) {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("isBundlePushComplete").WithValues("crName", shcPlaybookContext.cr.GetName(), "namespace", shcPlaybookContext.cr.GetNamespace())

	cmd := fmt.Sprintf("cat %s", shcBundlePushStatusCheckFile)
	streamOptions := splutil.NewStreamOptionsObject(cmd)
	// check the content of the status file
	stdOut, stdErr, err := shcPlaybookContext.podExecClient.RunPodExecCommand(ctx, streamOptions, []string{"/bin/sh"})
	if err != nil || stdErr != "" {
		err = fmt.Errorf("checking the status of SHC Bundle Push failed, stdOut=%s, stdErr=%s, err=%v", stdOut, stdErr, err)
		// reset the bundle push state to Pending, so that we retry again.
		setBundlePushState(ctx, shcPlaybookContext.afwPipeline, enterpriseApi.BundlePushPending)

		// remove the status file too, so that we dont have any stale status
		removeErr := shcPlaybookContext.removeSHCBundlePushStatusFile(ctx)
		if removeErr != nil {
			errors.Wrap(err, removeErr.Error())
		}
		return false, err
	}

	// Check if we did not get the desired output in the status file. There can be 2 scenarios -
	// 1. stdOut is empty, which means bundle push is still in progress
	// 2. stdOut has some other string other than the bundle push success message
	if stdOut == "" {
		scopedLog.Info("SHC Bundle Push is still in progress")
		return false, nil
	} else if !strings.Contains(stdOut, shcBundlePushCompleteStr) {
		// this means there was an error in bundle push command
		err = fmt.Errorf("there was an error in applying SHC Bundle, err=\"%v\"", stdOut)
		scopedLog.Error(err, "SHC Bundle push status file reported an error while applying bundle")

		// reset the bundle push state to Pending, so that we retry again.
		setBundlePushState(ctx, shcPlaybookContext.afwPipeline, enterpriseApi.BundlePushPending)

		// remove the status file too, so that we dont have any stale status
		removeErr := shcPlaybookContext.removeSHCBundlePushStatusFile(ctx)
		if removeErr != nil {
			errors.Wrap(err, removeErr.Error())
		}
		return false, err
	}

	// now that bundle push is complete, remove the status file
	err = shcPlaybookContext.removeSHCBundlePushStatusFile(ctx)
	if err != nil {
		scopedLog.Error(err, "removing SHC Bundle Push status file failed, will retry again.")

		// reset the state to BundlePushInProgress so that we can check the status of file again.
		setBundlePushState(ctx, shcPlaybookContext.afwPipeline, enterpriseApi.BundlePushInProgress)

		// don't return error from here, so that we can retry cleaning the file in next run
		return false, nil
	}

	return true, nil
}

// triggerBundlePush triggers the bundle push operation for SHC
func (shcPlaybookContext *SHCPlaybookContext) triggerBundlePush(ctx context.Context) error {
	// Reduce the liveness probe level
	shcPlaybookContext.setLivenessProbeLevel(ctx, livenessProbeLevel_1)
	cmd := fmt.Sprintf(applySHCBundleCmdStr, shcPlaybookContext.searchHeadCaptainURL, shcBundlePushStatusCheckFile)
	streamOptions := splutil.NewStreamOptionsObject(cmd)
	stdOut, stdErr, err := shcPlaybookContext.podExecClient.RunPodExecCommand(ctx, streamOptions, []string{"/bin/sh"})
	if err != nil || stdErr != "" {
		err = fmt.Errorf("error while applying SHC Bundle. stdout: %s, stderr: %s, err: %v", stdOut, stdErr, err)
		return err
	}
	return nil
}

// setLivenessProbeLevel sets the liveness probe level across all the Search Head Pods.
func (shcPlaybookContext *SHCPlaybookContext) setLivenessProbeLevel(ctx context.Context, probeLevel int) {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("shcPlaybookContext.setLivenessProbeLevel()")

	shcStsName := GetSplunkStatefulsetName(SplunkSearchHead, shcPlaybookContext.cr.GetName())
	shcStsNamespaceName := types.NamespacedName{Namespace: shcPlaybookContext.cr.GetNamespace(), Name: shcStsName}
	shcSts, err := splctrl.GetStatefulSetByName(ctx, shcPlaybookContext.client, shcStsNamespaceName)
	if err != nil {
		scopedLog.Error(err, "Unable to get the stateful set")
		return
	}

	podExecClient := splutil.GetPodExecClient(shcPlaybookContext.client, shcPlaybookContext.cr, "")
	err = setProbeLevelOnCRPods(ctx, shcPlaybookContext.cr, *shcSts.Spec.Replicas, podExecClient, probeLevel)
	if err != nil {
		scopedLog.Error(err, "Unable to set the Liveness probe level")
	}
}

// getClusterScopedAppsLocOnPod returns the cluster apps directory
func getClusterScopedAppsLocOnPod(cr splcommon.MetaObject) string {
	switch cr.GetObjectKind().GroupVersionKind().Kind {
	case "ClusterMaster":
		return idxcAppsLocationOnClusterManager
	case "SearchHeadCluster":
		return shcAppsLocationOnDeployer
	default:
		return ""
	}
}

// adjustClusterAppsFilePermissions sets the file permissions to +550
func adjustClusterAppsFilePermissions(ctx context.Context, podExecClient splutil.PodExecClientImpl) error {
	dirPath := getClusterScopedAppsLocOnPod(podExecClient.GetCR())
	if dirPath == "" {
		return fmt.Errorf("invalid Cluster apps location")
	}

	cmd := fmt.Sprintf(cmdSetFilePermissionsToRW, dirPath)
	streamOptions := splutil.NewStreamOptionsObject(cmd)
	stdOut, stdErr, err := podExecClient.RunPodExecCommand(ctx, streamOptions, []string{"/bin/sh"})
	if err != nil || stdErr != "" {
		return fmt.Errorf("command failed. cmd: %s, stdout: %s, stderr: %s, err: %v", cmd, stdOut, stdErr, err)
	}

	return nil
}

// runPlaybook will implement the bundle push logic for SHC
func (shcPlaybookContext *SHCPlaybookContext) runPlaybook(ctx context.Context) error {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("runPlaybook").WithValues("crName", shcPlaybookContext.cr.GetName(), "namespace", shcPlaybookContext.cr.GetNamespace())

	var err error
	var ok bool
	cr := shcPlaybookContext.cr.(*enterpriseApi.SearchHeadCluster)
	if cr.Status.Phase != enterpriseApi.PhaseReady {
		scopedLog.Info("SHC is not ready yet.")
		return nil
	}

	appDeployContext := shcPlaybookContext.afwPipeline.appDeployContext

	switch appDeployContext.BundlePushStatus.BundlePushStage {
	// if the bundle push is already in progress, check the status
	case enterpriseApi.BundlePushInProgress:
		scopedLog.Info("checking the status of SHC Bundle Push")
		// check if the bundle push is complete
		ok, err = shcPlaybookContext.isBundlePushComplete(ctx)
		if ok {
			// set the bundle push status to complete
			setBundlePushState(ctx, shcPlaybookContext.afwPipeline, enterpriseApi.BundlePushComplete)

			// reset the retry count
			shcPlaybookContext.afwPipeline.appDeployContext.BundlePushStatus.RetryCount = 0

			// set the state to install complete for all the cluster scoped apps
			setInstallStateForClusterScopedApps(ctx, appDeployContext)

			// set the liveness probe to default
			shcPlaybookContext.setLivenessProbeLevel(ctx, livenessProbeLevel_default)
		} else if err != nil {
			scopedLog.Error(err, "there was an error in SHC bundle push, will retry again")
		} else {
			scopedLog.Info("SHC Bundle Push is still in progress, will check back again")
		}
	case enterpriseApi.BundlePushPending:
		// run the command to apply cluster bundle
		scopedLog.Info("running command to apply SHC Bundle")

		// Adjust the file permissions
		err = adjustClusterAppsFilePermissions(ctx, shcPlaybookContext.podExecClient)
		if err != nil {
			scopedLog.Error(err, "failed to adjust the file permissions")
			return err
		}

		err = shcPlaybookContext.triggerBundlePush(ctx)
		if err != nil {
			scopedLog.Error(err, "failed to apply SHC Bundle")
			return err
		}

		scopedLog.Info("SHC Bundle Push is in progress")

		// set the state to bundle push complete since SHC bundle push is a sync call
		setBundlePushState(ctx, shcPlaybookContext.afwPipeline, enterpriseApi.BundlePushInProgress)
	default:
		err = fmt.Errorf("invalid bundle push state=%s", bundlePushStateAsStr(ctx, appDeployContext.BundlePushStatus.BundlePushStage))
	}

	return err
}

// isBundlePushComplete checks the status of bundle push
func (idxcPlaybookContext *IdxcPlaybookContext) isBundlePushComplete(ctx context.Context) bool {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("isBundlePushComplete").WithValues("crName", idxcPlaybookContext.cr.GetName(), "namespace", idxcPlaybookContext.cr.GetNamespace())

	streamOptions := splutil.NewStreamOptionsObject(idxcShowClusterBundleStatusStr)
	stdOut, stdErr, err := idxcPlaybookContext.podExecClient.RunPodExecCommand(ctx, streamOptions, []string{"/bin/sh"})
	if err == nil && strings.Contains(stdOut, "cluster_status=None") && !strings.Contains(stdOut, "last_bundle_validation_status=failure") {
		scopedLog.Info("IndexerCluster Bundle push complete")
		return true
	}

	if err != nil || stdErr != "" {
		scopedLog.Error(err, "show cluster-bundle-status failed", "stdout", stdOut, "stderr", stdErr)
		return false
	}

	scopedLog.Info("IndexerCluster Bundle push is still in progress")
	return false
}

// triggerBundlePush triggers the bundle push for indexer cluster
func (idxcPlaybookContext *IdxcPlaybookContext) triggerBundlePush(ctx context.Context) error {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("idxcPlaybookContext.triggerBundlePush()")

	// Reduce the liveness probe level
	idxcPlaybookContext.setLivenessProbeLevel(ctx, livenessProbeLevel_1)
	streamOptions := splutil.NewStreamOptionsObject(applyIdxcBundleCmdStr)
	stdOut, stdErr, err := idxcPlaybookContext.podExecClient.RunPodExecCommand(ctx, streamOptions, []string{"/bin/sh"})

	// If the error is due to a bundle which is already present, don't do anything.
	// In the next reconcile we will mark it as bundle push complete
	if strings.Contains(stdErr, idxcBundleAlreadyPresentStr) {
		scopedLog.Info("bundle already present on peers")
	} else if err != nil || !strings.Contains(stdErr, "OK\n") {
		err = fmt.Errorf("error while applying cluster bundle. stdout: %s, stderr: %s, err: %v", stdOut, stdErr, err)
		return err
	}

	return nil
}

// setLivenessProbeLevel sets the liveness probe level across all the indexer pods
func (idxcPlaybookContext *IdxcPlaybookContext) setLivenessProbeLevel(ctx context.Context, probeLevel int) {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("idxcPlaybookContext.setLivenessProbeLevel()")

	managerSts := afwGetReleventStatefulsetByKind(ctx, idxcPlaybookContext.cr, idxcPlaybookContext.client)
	if managerSts == nil {
		return
	}
	managerOwnerRefs := managerSts.GetOwnerReferences()

	for i := 0; i < len(managerOwnerRefs); i++ {
		// We are only interested for Indexer pods, skip all other references
		if managerOwnerRefs[i].Kind != "IndexerCluster" {
			continue
		}

		idxcNameSpaceName := types.NamespacedName{Namespace: idxcPlaybookContext.cr.GetNamespace(), Name: managerOwnerRefs[i].Name}
		var idxcCR enterpriseApi.IndexerCluster
		err := idxcPlaybookContext.client.Get(ctx, idxcNameSpaceName, &idxcCR)
		if err != nil {
			scopedLog.Error(err, "Unable to fetch the CR", "Name", managerOwnerRefs[i].Name, "Namespace", idxcPlaybookContext.cr.GetNamespace())
			continue
		}

		idxcStsName := GetSplunkStatefulsetName(SplunkIndexer, idxcCR.GetName())
		idxcStsNamespaceName := types.NamespacedName{Namespace: idxcCR.GetNamespace(), Name: idxcStsName}
		idxcSts, err := splctrl.GetStatefulSetByName(ctx, idxcPlaybookContext.client, idxcStsNamespaceName)
		if err != nil {
			scopedLog.Error(err, "Unable to get the stateful set")
			continue
		}

		podExecClient := splutil.GetPodExecClient(idxcPlaybookContext.client, &idxcCR, "")
		err = setProbeLevelOnCRPods(ctx, &idxcCR, *idxcSts.Spec.Replicas, podExecClient, probeLevel)
		if err != nil {
			scopedLog.Error(err, "Unable to set the Liveness probe level")
		}
	}
}

// runPlaybook will implement the following logic(and set the bundle push state accordingly)  -
// 1. If the bundle push is not in progress, run the logic to push the bundle from CM to indexer peers
// 2. OR else, if the bundle push is already in progress, check the status of bundle push
func (idxcPlaybookContext *IdxcPlaybookContext) runPlaybook(ctx context.Context) error {

	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("RunPlaybook").WithValues("crName", idxcPlaybookContext.cr.GetName(), "namespace", idxcPlaybookContext.cr.GetNamespace())

	appDeployContext := idxcPlaybookContext.afwPipeline.appDeployContext

	switch appDeployContext.BundlePushStatus.BundlePushStage {
	// if the bundle push is already in progress, check the status
	case enterpriseApi.BundlePushInProgress:
		scopedLog.Info("checking the status of IndexerCluster Bundle Push")
		// check if the bundle push is complete
		if idxcPlaybookContext.isBundlePushComplete(ctx) {
			// set the bundle push status to complete
			setBundlePushState(ctx, idxcPlaybookContext.afwPipeline, enterpriseApi.BundlePushComplete)

			// reset the retry count
			idxcPlaybookContext.afwPipeline.appDeployContext.BundlePushStatus.RetryCount = 0

			// set the state to install complete for all the cluster scoped apps
			setInstallStateForClusterScopedApps(ctx, appDeployContext)
			idxcPlaybookContext.setLivenessProbeLevel(ctx, livenessProbeLevel_default)
		} else {
			scopedLog.Info("IndexerCluster Bundle Push is still in progress, will check back again in next reconcile..")
		}

	case enterpriseApi.BundlePushPending:
		// Adjust the file permissions
		err := adjustClusterAppsFilePermissions(ctx, idxcPlaybookContext.podExecClient)
		if err != nil {
			scopedLog.Error(err, "failed to adjust the file permissions")
			return err
		}

		// run the command to apply cluster bundle
		scopedLog.Info("running command to apply IndexerCluster Bundle")
		err = idxcPlaybookContext.triggerBundlePush(ctx)
		if err != nil {
			scopedLog.Error(err, "failed to apply IndexerCluster Bundle")
			return err
		}

		// set the state to bundle push in progress
		setBundlePushState(ctx, idxcPlaybookContext.afwPipeline, enterpriseApi.BundlePushInProgress)

	default:
		err := fmt.Errorf("invalid Bundle push state=%s", bundlePushStateAsStr(ctx, appDeployContext.BundlePushStatus.BundlePushStage))
		return err
	}

	return nil
}

// needToRevisitAppFramework confirms if the app framework needs another entry for the reconcile
func needToRevisitAppFramework(afwPipeline *AppInstallPipeline) bool {
	return !afwPipeline.isPipelineEmpty() || afwPipeline.appDeployContext.IsDeploymentInProgress || isPendingClusterScopeWork(afwPipeline)
}

// checkAndUpdateAppFrameworkProgressFlag sets the app framework completion status
func checkAndUpdateAppFrameworkProgressFlag(afwPipeline *AppInstallPipeline) {
	if afwPipeline.isPipelineEmpty() && !isPendingClusterScopeWork(afwPipeline) {
		afwPipeline.appDeployContext.IsDeploymentInProgress = false
	}
}

// isPhaseInfoEligibleForSchedulerEntry confirms if there is any pending work
func isPhaseInfoEligibleForSchedulerEntry(ctx context.Context, appSrcName string, phaseInfo *enterpriseApi.PhaseInfo, afwConfig *enterpriseApi.AppFrameworkSpec) bool {
	if isPhaseMaxRetriesReached(ctx, phaseInfo, afwConfig) {
		return false
	}

	// if an app is already install complete, do not schedule a worker
	if phaseInfo.Phase == enterpriseApi.PhaseInstall && phaseInfo.Status == enterpriseApi.AppPkgInstallComplete {
		return false
	}

	scope := getAppSrcScope(ctx, afwConfig, appSrcName)
	// For cluster scoped apps, if pod copy is complete, do not schedule a worker
	if scope == enterpriseApi.ScopeCluster && phaseInfo.Phase == enterpriseApi.PhasePodCopy && phaseInfo.Status == enterpriseApi.AppPkgPodCopyComplete {
		return false
	}

	return true
}

// afwSchedulerEntry Starts the scheduler Pipeline with the required phases
func afwSchedulerEntry(ctx context.Context, client splcommon.ControllerClient, cr splcommon.MetaObject, appDeployContext *enterpriseApi.AppDeploymentContext, appFrameworkConfig *enterpriseApi.AppFrameworkSpec) (bool, error) {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("afwSchedulerEntry").WithValues("name", cr.GetName(), "namespace", cr.GetNamespace())

	// return error, if there is no storage defined for the Operator pod
	if !isPersistantVolConfigured() {
		return true, fmt.Errorf("persistant volume required for the App framework, but not provisioned")
	}

	// Operator pod storage is not fully under operator control
	// for now, update on every scheduler entry
	err := updateStorageTracker(ctx)
	if err != nil {
		return true, fmt.Errorf("failed to update storage tracker, error: %v", err)
	}

	afwPipeline := initAppInstallPipeline(ctx, appDeployContext, client, cr)

	// Start the download phase manager
	afwPipeline.phaseWaiter.Add(1)
	go afwPipeline.downloadPhaseManager(ctx)

	// Start the pod copy phase manager
	afwPipeline.phaseWaiter.Add(1)
	go afwPipeline.podCopyPhaseManager(ctx)

	// Start the install phase manager
	afwPipeline.phaseWaiter.Add(1)
	go afwPipeline.installPhaseManager(ctx)

	scopedLog.Info("Creating pipeline workers for pending app packages")

	for appSrcName, appSrcDeployInfo := range appDeployContext.AppsSrcDeployStatus {

		deployInfoList := appSrcDeployInfo.AppDeploymentInfoList

		sts := afwGetReleventStatefulsetByKind(ctx, cr, client)
		podName := getApplicablePodNameForAppFramework(cr, 0)

		podExecClient := splutil.GetPodExecClient(client, cr, podName)
		appsPathOnPod := filepath.Join(appBktMnt, appSrcName)
		// create the dir on Splunk pod/s where app/s will be copied from operator pod
		err = createDirOnSplunkPods(ctx, cr, *sts.Spec.Replicas, appsPathOnPod, podExecClient)
		if err != nil {
			scopedLog.Error(err, "unable to create directory on splunk pod")
			// break from here and let yield logic take care of everything
			break
		}

		for i := range deployInfoList {
			// Ignore any apps if there is no pending work
			if !isPhaseInfoEligibleForSchedulerEntry(ctx, appSrcName, &deployInfoList[i].PhaseInfo, appFrameworkConfig) {
				continue
			}
			afwPipeline.createAndAddPipelineWorker(ctx, deployInfoList[i].PhaseInfo.Phase, &deployInfoList[i], appSrcName, podName, appFrameworkConfig, client, cr, sts)
		}
	}

	// To avoid any premature termination, start the yield routine only after setting up all the Pipelines. It might be
	// few milliseconds before reaching this far, but that is OK. Otherwise, we may pre-maturely close the phases for any delays
	// while setting up the pipeline phases.
	// Wait for the yield function to finish.
	afwPipeline.phaseWaiter.Add(1)
	go afwPipeline.afwYieldWatcher(ctx)

	scopedLog.Info("Waiting for the phase managers to finish")

	// Wait for all the pipeline managers to finish
	afwPipeline.phaseWaiter.Wait()
	scopedLog.Info("All the phase managers finished")

	// Finally mark if all the App framework is complete
	checkAndUpdateAppFrameworkProgressFlag(afwPipeline)

	return needToRevisitAppFramework(afwPipeline), nil
}

// afwYieldWatcher issues termination request to the scheduler when the yield time expires or the pipelines become empty.
func (ppln *AppInstallPipeline) afwYieldWatcher(ctx context.Context) {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("afwYieldWatcher").WithValues("name", ppln.cr.GetName(), "namespace", ppln.cr.GetNamespace())
	yieldTrigger := time.After(time.Duration(ppln.appDeployContext.AppFrameworkConfig.SchedulerYieldInterval) * time.Second)

yieldScheduler:
	for {
		select {
		case <-yieldTrigger:
			scopedLog.Info("Yielding from AFW scheduler", "time elapsed", time.Now().Unix()-ppln.afwEntryTime)
			break yieldScheduler
		default:
			if ppln.isPipelineEmpty() {
				break yieldScheduler
			}
		}

		time.Sleep(100 * time.Millisecond)
	}

	// Trigger the pipeline termination by closing the channel
	close(ppln.sigTerm)
	ppln.phaseWaiter.Done()
	scopedLog.Info("Termination issued")
}
