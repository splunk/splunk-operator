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

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"

	"github.com/pkg/errors"
	"github.com/splunk/splunk-operator/pkg/logging"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	"github.com/splunk/splunk-operator/pkg/splunk/k8sops"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/types"
)

var (
	phaseManagerBusyWaitDuration  = 1 * time.Second
	phaseManagerLoopSleepDuration = 200 * time.Millisecond
)

var appPhaseInfoStatuses = map[enterpriseApi.AppPhaseStatusType]bool{
	enterpriseApi.AppPkgDownloadPending:     true,
	enterpriseApi.AppPkgDownloadInProgress:  true,
	enterpriseApi.AppPkgDownloadComplete:    true,
	enterpriseApi.AppPkgDownloadError:       true,
	enterpriseApi.AppPkgPodCopyPending:      true,
	enterpriseApi.AppPkgPodCopyInProgress:   true,
	enterpriseApi.AppPkgPodCopyComplete:     true,
	enterpriseApi.AppPkgMissingFromOperator: true,
	enterpriseApi.AppPkgPodCopyError:        true,
	enterpriseApi.AppPkgInstallPending:      true,
	enterpriseApi.AppPkgInstallInProgress:   true,
	enterpriseApi.AppPkgInstallComplete:     true,
	enterpriseApi.AppPkgMissingOnPodError:   true,
	enterpriseApi.AppPkgInstallError:        true,
}

// isFanOutApplicableToCR confirms if a given CR needs fanOut support
func isFanOutApplicableToCR(cr splcommon.MetaObject) bool {
	switch cr.GetObjectKind().GroupVersionKind().Kind {
	case "Standalone", "IngestorCluster":
		return true
	default:
		return false
	}
}

// createAndAddPipelineWorker used to add a worker to the pipeline on reconcile re-entry
func (ppln *AppInstallPipeline) createAndAddPipelineWorker(ctx context.Context, phase enterpriseApi.AppPhaseType, appDeployInfo *enterpriseApi.AppDeploymentInfo,
	appSrcName string, podName string, appFrameworkConfig *enterpriseApi.AppFrameworkSpec,
	client splcommon.ControllerClient, cr splcommon.MetaObject, statefulSet *appsv1.StatefulSet) {

	scopedLog := logging.FromContext(ctx).With("func", "createAndAddPipelineWorker", "name", cr.GetName(), "namespace", cr.GetNamespace())

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

	scopedLog.InfoContext(ctx, "created new worker", "podName", worker.targetPodName, "appName", appDeployInfo.AppName, "digest", appDeployInfo.ObjectHash, "phase", appDeployInfo.PhaseInfo.Phase, "fanOut", worker.fanOut)

	ppln.addWorkersToPipelinePhase(ctx, phase, worker)
}

// getApplicablePodNameForAppFramework gets the Pod name relevant for the CR under work
func getApplicablePodNameForAppFramework(cr splcommon.MetaObject, ordinalIdx int) string {
	var podType string

	switch cr.GetObjectKind().GroupVersionKind().Kind {
	case "Standalone":
		podType = "standalone"
	case "LicenseManager":
		podType = "license-manager"
	case "LicenseMaster":
		podType = "license-master"
	case "SearchHeadCluster":
		podType = "deployer"
	case "IndexerCluster":
		return ""
	case "ClusterMaster":
		podType = "cluster-master"
	case "ClusterManager":
		podType = "cluster-manager"
	case "MonitoringConsole":
		podType = "monitoring-console"
	case "IngestorCluster":
		podType = "ingestor"
	}

	return fmt.Sprintf("splunk-%s-%s-%d", cr.GetName(), podType, ordinalIdx)
}

// runCustomCommandOnSplunkPods  runs the specified custom command on the pod/s
func runCustomCommandOnSplunkPods(ctx context.Context, cr splcommon.MetaObject, replicas int32, command string, adminPwd string, podExecClient splutil.PodExecClientImpl) error {
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
			err = fmt.Errorf("unable to run command %s. stdout: %s, err: %s", redactSplunkAuth(command, adminPwd), stdOut, err)
			break
		}
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

// canAppScopeHaveInstallWorker tells us if the given scope can have an install worker
// Only local and premium app scopes can have install workers in pipeline q
func canAppScopeHaveInstallWorker(scope string) bool {
	// Check if scope is appropriate
	if scope == enterpriseApi.ScopeLocal || scope == enterpriseApi.ScopePremiumApps {
		return true
	}
	return false
}

// addWorkersToPipelinePhase adds a worker to a given pipeline phase
func (ppln *AppInstallPipeline) addWorkersToPipelinePhase(ctx context.Context, phaseID enterpriseApi.AppPhaseType, workers ...*PipelineWorker) {

	scopedLog := logging.FromContext(ctx).With("func", "addWorkersToPipelinePhase", "phase", phaseID)

	for _, worker := range workers {
		scopedLog.InfoContext(ctx, "adding worker", "name", worker.cr.GetName(), "namespace", worker.cr.GetNamespace(), "podName", worker.targetPodName, "appName", worker.appDeployInfo.AppName, "digest", worker.appDeployInfo.ObjectHash)
	}
	ppln.pplnPhases[phaseID].mutex.Lock()
	ppln.pplnPhases[phaseID].q = append(ppln.pplnPhases[phaseID].q, workers...)
	ppln.pplnPhases[phaseID].mutex.Unlock()
}

// deleteWorkerFromPipelinePhase deletes a given worker from a pipeline phase
func (ppln *AppInstallPipeline) deleteWorkerFromPipelinePhase(ctx context.Context, phaseID enterpriseApi.AppPhaseType, worker *PipelineWorker) bool {

	scopedLog := logging.FromContext(ctx).With("func", "deleteWorkerFromPipelinePhase", "phase", phaseID)
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

			scopedLog.InfoContext(ctx, "deleted worker", "name", worker.cr.GetName(), "namespace", worker.cr.GetNamespace(), "podName", worker.targetPodName, "phase", phaseID, "appName", worker.appDeployInfo.AppName, "digest", worker.appDeployInfo.ObjectHash)
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

	scopedLog := logging.FromContext(ctx).With("func", "transitionWorkerPhase", "name", worker.cr.GetName(), "namespace", worker.cr.GetNamespace(), "appName", worker.appDeployInfo.AppName, "digest", worker.appDeployInfo.ObjectHash, "podName", worker.targetPodName, "currentPhase", currentPhase, "nextPhase", nextPhase)

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
		scopedLog.InfoContext(ctx, "fan-out transition")
		if currentPhase == enterpriseApi.PhaseDownload {
			// On a reconcile entry, processing the Standalone CR right after loading the appDeployContext from the CR status
			var podCopyWorkers, installWorkers []*PipelineWorker

			// Seems like the download just finished. Allocate Phase info
			if len(appDeployInfo.AuxPhaseInfo) == 0 {
				scopedLog.InfoContext(ctx, "just finished the download phase")
				// Create Phase info for all the statefulset Pods.
				appDeployInfo.AuxPhaseInfo = make([]enterpriseApi.PhaseInfo, replicaCount)

				// Create a slice of corresponding worker nodes
				podCopyWorkers = make([]*PipelineWorker, replicaCount)

				//Create the Aux PhaseInfo for tracking all the Standalone Pods
				for podID := range appDeployInfo.AuxPhaseInfo {
					// Create a new copy worker
					podCopyWorkers[podID] = createFanOutWorker(worker, podID)

					setContextForNewPhase(&appDeployInfo.AuxPhaseInfo[podID], enterpriseApi.PhasePodCopy)
					scopedLog.InfoContext(ctx, "created a new fan-out pod copy worker", "podName", worker.targetPodName)
				}
			} else {
				scopedLog.InfoContext(ctx, "installation was already in progress for replica members")

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
						scopedLog.ErrorContext(ctx, "invalid phase info detected", "phase", phaseInfo.Phase, "phaseStatus", phaseInfo.Status)
					}
				}
			}

			ppln.addWorkersToPipelinePhase(ctx, enterpriseApi.PhasePodCopy, podCopyWorkers...)
			ppln.addWorkersToPipelinePhase(ctx, enterpriseApi.PhaseInstall, installWorkers...)
		} else {
			scopedLog.ErrorContext(ctx, "invalid phase detected")
		}

	} else {
		scopedLog.InfoContext(ctx, "simple transition")
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
	scopedLog.InfoContext(ctx, "deleted worker", "phase", currentPhase)
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

	scopedLog := logging.FromContext(ctx).With("func", "getPhaseInfoFromWorker")

	if needToUseAuxPhaseInfo(worker, phaseType) {
		podID, err := getOrdinalValFromPodName(worker.targetPodName)
		if err != nil {
			scopedLog.ErrorContext(ctx, "unable to get the pod Id", "podName", worker.targetPodName, "error", err)
			return nil
		}

		return &worker.appDeployInfo.AuxPhaseInfo[podID]
	}

	return &worker.appDeployInfo.PhaseInfo
}

// updatePplnWorkerPhaseInfo updates the in-memory PhaseInfo(specifically status and retryCount)
func updatePplnWorkerPhaseInfo(ctx context.Context, appDeployInfo *enterpriseApi.AppDeploymentInfo, failCount uint32, statusType enterpriseApi.AppPhaseStatusType) {

	scopedLog := logging.FromContext(ctx).With("func", "updatePplnWorkerPhaseInfo", "appName", appDeployInfo.AppName)

	scopedLog.InfoContext(ctx, "changing the status", "oldStatus", appPhaseStatusAsStr(appDeployInfo.PhaseInfo.Status), "newStatus", appPhaseStatusAsStr(statusType))
	appDeployInfo.PhaseInfo.FailCount = failCount
	appDeployInfo.PhaseInfo.Status = statusType
}

func (downloadWorker *PipelineWorker) createDownloadDirOnOperator(ctx context.Context) (string, error) {

	scopedLog := logging.FromContext(ctx).With("func", "createDownloadDirOnOperator", "appSrcName", downloadWorker.appSrcName, "appName", downloadWorker.appDeployInfo.AppName)
	scope := getAppSrcScope(ctx, downloadWorker.afwConfig, downloadWorker.appSrcName)

	kind := downloadWorker.cr.GetObjectKind().GroupVersionKind().Kind

	// This is how the path to download apps looks like -
	// /opt/splunk/appframework/downloadedApps/<namespace>/<CR_Kind>/<CR_Name>/<scope>/<appSrc_Name>/
	// For e.g., if the we are trying to download app app1.tgz under "admin" app source name, for a Standalone CR with name "stand1"
	// in default namespace, then it will be downloaded at the path -
	// /opt/splunk/appframework/downloadedApps/default/Standalone/stand1/local/admin/app1.tgz_<hash>
	localPath := filepath.Join(getResolvedAppDownloadVolume(), "downloadedApps", downloadWorker.cr.GetNamespace(), kind, downloadWorker.cr.GetName(), scope, downloadWorker.appSrcName) + "/"
	// create the sub-directories on the volume for downloading scoped apps
	err := createAppDownloadDir(ctx, localPath)
	if err != nil {
		scopedLog.ErrorContext(ctx, "unable to create app download directory on operator", "error", err)
	}
	return localPath, err
}

// download API will do the actual work of downloading apps from remote storage
func (downloadWorker *PipelineWorker) download(ctx context.Context, pplnPhase *PipelinePhase, remoteDataClientMgr RemoteDataClientManager, localPath string, downloadWorkersRunPool chan struct{}) {

	defer func() {
		downloadWorker.isActive = false

		<-downloadWorkersRunPool
		// decrement the waiter count
		downloadWorker.waiter.Done()
	}()

	splunkCR := downloadWorker.cr
	appSrcName := downloadWorker.appSrcName

	scopedLog := logging.FromContext(ctx).With("func", "PipelineWorker.Download", "name", splunkCR.GetName(), "namespace", splunkCR.GetNamespace(), "appName", downloadWorker.appDeployInfo.AppName, "objectHash", downloadWorker.appDeployInfo.ObjectHash)

	appDeployInfo := downloadWorker.appDeployInfo
	appName := appDeployInfo.AppName

	localFile := getLocalAppFileName(ctx, localPath, appName, appDeployInfo.ObjectHash)
	remoteFile, err := getRemoteObjectKey(ctx, splunkCR, downloadWorker.afwConfig, appSrcName, appName)
	if err != nil {
		scopedLog.ErrorContext(ctx, "unable to get remote object key", "appName", appName, "error", err)
		// increment the retry count and mark this app as download pending
		updatePplnWorkerPhaseInfo(ctx, appDeployInfo, appDeployInfo.PhaseInfo.FailCount+1, enterpriseApi.AppPkgDownloadPending)

		return
	}

	// download the app from remote storage
	err = remoteDataClientMgr.DownloadApp(ctx, remoteFile, localFile, appDeployInfo.ObjectHash)
	if err != nil {
		scopedLog.ErrorContext(ctx, "unable to download app", "appName", appName, "error", err)

		// remove the local file
		err = os.RemoveAll(localFile)
		if err != nil {
			scopedLog.ErrorContext(ctx, "unable to remove local file from operator", "error", err)
		}

		// increment the retry count and mark this app as download pending
		updatePplnWorkerPhaseInfo(ctx, appDeployInfo, appDeployInfo.PhaseInfo.FailCount+1, enterpriseApi.AppPkgDownloadPending)
		return
	}

	// download is successful, update the state and reset the retry count
	updatePplnWorkerPhaseInfo(ctx, appDeployInfo, 0, enterpriseApi.AppPkgDownloadComplete)

	scopedLog.InfoContext(ctx, "finished downloading app")
}

// downloadWorkerHandler schedules the download workers to download app/s
func (pplnPhase *PipelinePhase) downloadWorkerHandler(ctx context.Context, ppln *AppInstallPipeline, maxWorkers int64, scheduleDownloadsWaiter *sync.WaitGroup) {

	scopedLog := logging.FromContext(ctx).With("func", "downloadWorkerHandler")

	// derive a counting semaphore from the channel to represent worker run pool
	var downloadWorkersRunPool = make(chan struct{}, maxWorkers)

downloadWork:
	for {
		select {
		case <-ctx.Done():
			scopedLog.InfoContext(ctx, "context cancelled, stopping download worker handler")
			break downloadWork
		// get an idle worker
		case downloadWorkersRunPool <- struct{}{}:
			select {
			case downloadWorker, ok := <-pplnPhase.msgChannel:
				// if channel is closed, then just break from here as we have nothing to read
				if !ok {
					scopedLog.InfoContext(ctx, "msgChannel is closed by downloadPhaseManager, hence nothing to read")
					break downloadWork
				}

				// do not redownload the app if it is already downloaded
				if isAppAlreadyDownloaded(ctx, downloadWorker) {
					scopedLog.InfoContext(ctx, "app is already downloaded on operator pod, hence skipping it", "appSrcName", downloadWorker.appSrcName, "appName", downloadWorker.appDeployInfo.AppName)
					// update the state to be download complete
					updatePplnWorkerPhaseInfo(ctx, downloadWorker.appDeployInfo, 0, enterpriseApi.AppPkgDownloadComplete)
					<-downloadWorkersRunPool
					continue
				}

				// do not proceed if we dont have enough disk space to download this app

				err := reserveStorage(downloadWorker.appDeployInfo.Size)
				if err != nil {
					scopedLog.ErrorContext(ctx, "insufficient storage for the app pkg download", "appSrcName", downloadWorker.appSrcName, "appName", downloadWorker.appDeployInfo.AppName, "appSize", downloadWorker.appDeployInfo.Size, "error", err)
					// setting isActive to false here so that downloadPhaseManager can take care of it.
					downloadWorker.isActive = false
					<-downloadWorkersRunPool
					continue
				}

				// update the download state of app to be DownloadInProgress
				updatePplnWorkerPhaseInfo(ctx, downloadWorker.appDeployInfo, downloadWorker.appDeployInfo.PhaseInfo.FailCount, enterpriseApi.AppPkgDownloadInProgress)

				appDeployInfo := downloadWorker.appDeployInfo

				// create the sub-directories on the volume for downloading scoped apps
				localPath, err := downloadWorker.createDownloadDirOnOperator(ctx)
				if err != nil {
					scopedLog.ErrorContext(ctx, "unable to create download directory on operator", "appSrcName", downloadWorker.appSrcName, "appName", appDeployInfo.AppName, "error", err)

					// increment the retry count and mark this app as download pending
					updatePplnWorkerPhaseInfo(ctx, appDeployInfo, appDeployInfo.PhaseInfo.FailCount+1, enterpriseApi.AppPkgDownloadPending)

					<-downloadWorkersRunPool
					continue
				}

				// get the remoteDataClientMgr instance
				remoteDataClientMgr, err := getRemoteDataClientMgr(ctx, downloadWorker.client, downloadWorker.cr, downloadWorker.afwConfig, downloadWorker.appSrcName)
				if err != nil {
					scopedLog.ErrorContext(ctx, "unable to get remote data client manager", "error", err)
					// increment the retry count and mark this app as download error
					updatePplnWorkerPhaseInfo(ctx, appDeployInfo, appDeployInfo.PhaseInfo.FailCount+1, enterpriseApi.AppPkgDownloadError)

					<-downloadWorkersRunPool
					continue
				}

				// increment the count in worker waitgroup
				downloadWorker.waiter.Add(1)

				// start the actual download
				go downloadWorker.download(ctx, pplnPhase, *remoteDataClientMgr, localPath, downloadWorkersRunPool)

			default:
				<-downloadWorkersRunPool
			}
		default:
			// All the workers are busy, check after one second
			scopedLog.InfoContext(ctx, "all the workers are busy, we will check again after one second")
			time.Sleep(phaseManagerBusyWaitDuration)
		}

		time.Sleep(phaseManagerLoopSleepDuration)
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

	scopedLog := logging.FromContext(ctx).With("func", phaseManager)

	// close the msgChannel
	close(pplnPhase.msgChannel)

	// wait for the handler code to finish its work
	scopedLog.InfoContext(ctx, "waiting for the workers to finish")
	perPhaseWaiter.Wait()

	// mark the phase as done/complete
	scopedLog.InfoContext(ctx, "all the workers finished")
	ppln.phaseWaiter.Done()
}

// downloadPhaseManager creates download phase manager for the install pipeline
func (ppln *AppInstallPipeline) downloadPhaseManager(ctx context.Context) {

	scopedLog := logging.FromContext(ctx).With("func", "downloadPhaseManager")
	scopedLog.InfoContext(ctx, "starting Download phase manager")

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
		case <-ctx.Done():
			// Stop scheduling/waiting on new download work immediately so the
			// operator pod doesn't hold S3 connections open past shutdown.
			scopedLog.Info("Context cancelled, stopping download phase manager")
			break downloadPhase
		case _, channelOpen := <-ppln.sigTerm:
			if !channelOpen {
				scopedLog.InfoContext(ctx, "received the termination request from the scheduler")
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
						scopedLog.InfoContext(ctx, "download worker got a run slot", "name", downloadWorker.cr.GetName(), "namespace", downloadWorker.cr.GetNamespace(), "appName", downloadWorker.appDeployInfo.AppName, "digest", downloadWorker.appDeployInfo.ObjectHash)
						downloadWorker.isActive = true
					default:
						downloadWorker.waiter = nil
					}
				}
			}
		}

		time.Sleep(phaseManagerLoopSleepDuration)
	}
}

// markWorkerPhaseInstallationComplete marks the worker phase as app package installation complete
func markWorkerPhaseInstallationComplete(ctx context.Context, phaseInfo *enterpriseApi.PhaseInfo, worker *PipelineWorker) {

	scopedLog := logging.FromContext(ctx).With("func", "markWorkerPhaseInstallationComplete")

	// Set auxphase info status for fanout CRs and phaseinfo status for others
	phaseInfo.Status = enterpriseApi.AppPkgInstallComplete
	phaseInfo.FailCount = 0

	// For fanout CRs, once all the replicas have the app installed, mark the main
	// phaseinfo as install complete
	if isFanOutApplicableToCR(worker.cr) {
		if isAppInstallationCompleteOnAllReplicas(worker.appDeployInfo.AuxPhaseInfo) {
			scopedLog.InfoContext(ctx, "app pkg installed on all the pods", "appPkg", worker.appDeployInfo.AppName)
			worker.appDeployInfo.PhaseInfo.Phase = enterpriseApi.PhaseInstall
			worker.appDeployInfo.PhaseInfo.Status = enterpriseApi.AppPkgInstallComplete

			//For now, set the deploy status as complete. Eventually, we can phase it out
			worker.appDeployInfo.DeployStatus = enterpriseApi.DeployStatusComplete
		}
	}
}

// installApp installs an app for an install worker
func installApp(rctx context.Context, localCtx *localScopePlaybookContext, cr splcommon.MetaObject, phaseInfo *enterpriseApi.PhaseInfo) error {
	worker := localCtx.worker

	scopedLog := logging.FromContext(rctx).With("func", "installApp", "name", cr.GetName(), "namespace", cr.GetNamespace(), "pod", worker.targetPodName, "appName", worker.appDeployInfo.AppName)

	// if the app name is app1.tgz and hash is "abcd1234", then appPkgFileName is app1.tgz_abcd1234
	appPkgFileName := getAppPackageName(worker)

	// if appsrc is "appSrc1", then appPkgPathOnPod is /operator-staging/appframework/appSrc1/app1.tgz_abcd1234
	appPkgPathOnPod := filepath.Join(appBktMnt, worker.appSrcName, appPkgFileName)

	if !checkIfFileExistsOnPod(rctx, cr, appPkgPathOnPod, localCtx.podExecClient) {
		scopedLog.ErrorContext(rctx, "app pkg missing on Pod", "appPkgPath", appPkgPathOnPod)
		phaseInfo.Status = enterpriseApi.AppPkgMissingOnPodError

		return fmt.Errorf("app pkg missing on Pod. app pkg path: %s", appPkgPathOnPod)
	}

	if worker.appDeployInfo.AppPackageTopFolder == "" {
		appTopFolder, err := getAppTopFolderFromPackage(rctx, cr, appPkgPathOnPod, localCtx.podExecClient)
		if err != nil {
			scopedLog.ErrorContext(rctx, "local scoped app package install failed while getting name of installed app", "error", err)
			return err
		}
		scopedLog.InfoContext(rctx, "app top folder", "name", appTopFolder)

		worker.appDeployInfo.AppPackageTopFolder = appTopFolder
	}

	adminPwd, err := splutil.GetAdminPasswordFromNamespaceScopedSecret(rctx, worker.client, cr.GetNamespace())
	if err != nil {
		scopedLog.ErrorContext(rctx, "failed to retrieve admin password", "error", err)
		return err
	}

	var command string
	if worker.appDeployInfo.IsUpdate {
		// App was already installed, update scenario
		command = fmt.Sprintf("/opt/splunk/bin/splunk install app %s -update 1 -auth admin:%s", appPkgPathOnPod, shellQuote(adminPwd))
	} else {
		// install the app only if it was not already installed
		// we can come to this block if post installation failed
		// e.g. es post installation failed but es app was already installed

		scopedLog.InfoContext(rctx, "check if app is already installed ", "name", worker.appDeployInfo.AppPackageTopFolder)

		appInstalled, err := isAppAlreadyInstalled(rctx, cr, localCtx.podExecClient, worker.appDeployInfo.AppPackageTopFolder)
		if err != nil {
			scopedLog.ErrorContext(rctx, "local scoped app package install failed while checking if app is already installed", "error", err)
			return err
		}

		if appInstalled {
			scopedLog.InfoContext(rctx, "not reinstalling app as it is already installed")
			return nil
		}

		command = fmt.Sprintf("/opt/splunk/bin/splunk install app %s -auth admin:%s", appPkgPathOnPod, shellQuote(adminPwd))
	}

	streamOptions := splutil.NewStreamOptionsObject(command)

	stdOut, stdErr, err := localCtx.podExecClient.RunPodExecCommand(rctx, streamOptions, []string{"/bin/sh"})

	// TODO(patrykw-splunk): remove this once we have confirm that we are not using stderr for error detection at all
	// Log stderr content for debugging but don't use it for error detection
	if stdErr != "" {
		scopedLog.InfoContext(rctx, "app install command stderr output (informational only)", "stderr", stdErr)
	}

	// Check only the actual command execution error, not stderr content
	if err != nil {
		phaseInfo.FailCount++
		scopedLog.ErrorContext(rctx, "local scoped app package install failed", "stdout", stdOut, "stderr", stdErr, "command", redactSplunkAuth(command, adminPwd), "appPkgPath", appPkgPathOnPod, "failCount", phaseInfo.FailCount, "error", err)
		return fmt.Errorf("local scoped app package install failed. stdOut: %s, stdErr: %s, command: %s, app pkg path: %s, failCount: %d", stdOut, stdErr, redactSplunkAuth(command, adminPwd), appPkgPathOnPod, phaseInfo.FailCount)
	}

	return nil
}

// check if the given app is already installed and enabled.
// the installed app name is supposed to be same as
// name of top folder (AppTopFolder)
func isAppAlreadyInstalled(rctx context.Context, cr splcommon.MetaObject, podExecClient splutil.PodExecClientImpl, appTopFolder string) (bool, error) {

	scopedLog := logging.FromContext(rctx).With("func", "isAppAlreadyInstalled", "podName", podExecClient.GetTargetPodName(), "namespace", cr.GetNamespace(), "AppTopFolder", appTopFolder)

	scopedLog.InfoContext(rctx, "check app's installation state")

	adminPwd, err := splutil.GetAdminPasswordFromNamespaceScopedSecret(rctx, podExecClient.GetClient(), cr.GetNamespace())
	if err != nil {
		scopedLog.ErrorContext(rctx, "failed to retrieve admin password", "error", err)
		return false, err
	}

	command := fmt.Sprintf("/opt/splunk/bin/splunk list app %s -auth admin:%s| grep ENABLED", appTopFolder, shellQuote(adminPwd))

	streamOptions := splutil.NewStreamOptionsObject(command)

	stdOut, stdErr, err := podExecClient.RunPodExecCommand(rctx, streamOptions, []string{"/bin/sh"})

	// Handle specific stderr cases first
	if strings.Contains(stdErr, "Could not find object") {
		// when app is not installed you will see something like on StdErr:
		// "Could not find object id=<app_name>"
		// which mean app is not installed (no need to check enabled at this time)
		return false, nil
	}

	// Log any other stderr content for debugging but don't use it for error detection
	if stdErr != "" {
		scopedLog.InfoContext(rctx, "command stderr output (informational only)", "stderr", stdErr)
	}

	// Now check the actual command result
	if err != nil {
		// The command pipeline ends with 'grep ENABLED', so exit codes follow grep semantics:
		// For grep: exit code 1 = pattern not found, exit code 2+ = actual error
		errMsg := err.Error()

		// Check for grep exit code 1 (pattern not found)
		if strings.Contains(errMsg, "exit status 1") || strings.Contains(errMsg, "command terminated with exit code 1") {
			// grep exit code 1 means "ENABLED" pattern not found - app exists but is not enabled
			scopedLog.InfoContext(rctx, "app not enabled - grep pattern not found", "stdout", stdOut, "stderr", stdErr)
			return false, nil
		}

		// Any other exit code indicates a real error (splunk command failed, etc.)
		return false, fmt.Errorf("could not get installed app status stdOut: %s, stdErr: %s, error: %v, command: %s", stdOut, stdErr, err, command)
	}

	// If we reach here, grep found "ENABLED" (exit code 0)
	// stdOut should contain the app status line with "ENABLED"
	if stdOut == "" {
		// This shouldn't happen if grep succeeded, but let's be safe
		return false, fmt.Errorf("command succeeded but no output received, command: %s", command)
	}

	scopedLog.InfoContext(rctx, "app installation state check successful - app is enabled", "appStatus", strings.TrimSpace(stdOut))
	return true, nil
}

// get the name of top folder from the package.
// this name is later used as installed app name
func getAppTopFolderFromPackage(rctx context.Context, cr splcommon.MetaObject, appPkgPathOnPod string, podExecClient splutil.PodExecClientImpl) (string, error) {
	scopedLog := logging.FromContext(rctx).With("func", "getAppTopFolderFromPackage", "name", cr.GetName(), "namespace", cr.GetNamespace(), "appPkgPathOnPod", appPkgPathOnPod)

	command := fmt.Sprintf("tar tf %s|head -1|cut -d/ -f1", appPkgPathOnPod)

	streamOptions := splutil.NewStreamOptionsObject(command)

	stdOut, stdErr, err := podExecClient.RunPodExecCommand(rctx, streamOptions, []string{"/bin/sh"})
	scopedLog.InfoContext(rctx, "pod exec result", "stdOut", stdOut)

	if stdErr != "" || err != nil {
		// CSPL-2598 - Log warnings/errors.
		// Return an error only when an empty app name is extracted.
		// In a scenario where there is a non-empty incorrect name,
		// we are ok just error logging as this API is used
		// only to avoid re-installation of apps(Eg. ES post install failures).
		// The onus falls on the user to make sure the app packages are tarred appropriately
		// to avoid the re-installation cycles as it is prudent to continue
		// to the install step for harmless warnings
		scopedLog.ErrorContext(rctx, "error in tar contents list, but app installation will continue", "stdOut", stdOut, "stdErr", stdErr, "command", command, "appPkgPathOnPod", appPkgPathOnPod, "error", err)
		if stdOut == "" {
			return "Empty app package name, could not get installed app name", err
		}
	}

	//output contains a trailing \n also, something like "SplunkEnterpriseSecuritySuite\n"
	stdOut = strings.Trim(stdOut, "\n")
	return stdOut, nil
}

// cleanupApp cleans up the package on operator and target pod
func cleanupApp(rctx context.Context, localCtx *localScopePlaybookContext, cr splcommon.MetaObject, phaseInfo *enterpriseApi.PhaseInfo) error {
	worker := localCtx.worker

	scopedLog := logging.FromContext(rctx).With("func", "cleanupApp", "name", cr.GetName(), "namespace", cr.GetNamespace(), "pod", worker.targetPodName, "appName", worker.appDeployInfo.AppName)

	// if the app name is app1.tgz and hash is "abcd1234", then appPkgFileName is app1.tgz_abcd1234
	appPkgFileName := getAppPackageName(worker)

	// if appsrc is "appSrc1", then appPkgPathOnPod is /operator-staging/appframework/appSrc1/app1.tgz_abcd1234
	appPkgPathOnPod := filepath.Join(appBktMnt, worker.appSrcName, appPkgFileName)

	// Delete the app package from the target pod /operator-staging/appframework/ location
	command := fmt.Sprintf("rm -f %s", appPkgPathOnPod)
	streamOptions := splutil.NewStreamOptionsObject(command)
	stdOut, stdErr, err := localCtx.podExecClient.RunPodExecCommand(rctx, streamOptions, []string{"/bin/sh"})
	if stdErr != "" || err != nil {
		scopedLog.ErrorContext(rctx, "app pkg deletion failed", "stdout", stdOut, "stderr", stdErr, "appPkgPath", appPkgPathOnPod, "error", err)
		return fmt.Errorf("app pkg deletion failed.  stdOut: %s, stdErr: %s, app pkg path: %s", stdOut, stdErr, appPkgPathOnPod)
	}
	scopedLog.InfoContext(rctx, "app package deleted from target pod", "command", command)

	// Try to remove the app package from the Operator Pod
	tryAppPkgCleanupFromOperatorPod(rctx, worker)

	return nil
}

// runPlaybook implements the playbook for local scoped app install
func (localCtx *localScopePlaybookContext) runPlaybook(rctx context.Context) error {
	worker := localCtx.worker
	cr := worker.cr
	scopedLog := logging.FromContext(rctx).With("func", "localScopePlaybookContext.runPlaybook", "name", cr.GetName(), "namespace", cr.GetNamespace(), "pod", worker.targetPodName, "appName", worker.appDeployInfo.AppName)

	defer func() {
		<-localCtx.sem
		worker.isActive = false
		worker.waiter.Done()
	}()

	// Get phase info
	phaseInfo := getPhaseInfoByPhaseType(rctx, worker, enterpriseApi.PhaseInstall)

	// Call the API to install an app
	err := installApp(rctx, localCtx, cr, phaseInfo)
	if err != nil {
		scopedLog.ErrorContext(rctx, "app package installation error", "error", err)
		return fmt.Errorf("app pkg installation failed. error %s", err.Error())
	}

	// Mark the worker for install complete status
	markWorkerPhaseInstallationComplete(rctx, phaseInfo, worker)

	// Call the API to cleanup the app
	err = cleanupApp(rctx, localCtx, cr, phaseInfo)
	if err != nil {
		scopedLog.ErrorContext(rctx, "app package cleanup error", "error", err)
		return fmt.Errorf("app pkg cleanup failed. error %s", err.Error())
	}

	return nil
}

// extractClusterScopedAppOnPod untars the given app package to the bundle push location
func extractClusterScopedAppOnPod(ctx context.Context, worker *PipelineWorker, appSrcScope string, appPkgPathOnPod, appPkgLocalPath string, podExecClient splutil.PodExecClientImpl) error {
	cr := worker.cr

	scopedLog := logging.FromContext(ctx).With("func", "extractClusterScopedAppOnPod", "name", cr.GetName(), "namespace", cr.GetNamespace(), "appName", worker.appDeployInfo.AppName)

	var stdOut, stdErr string
	var err error

	clusterAppsPath := getClusterScopedAppsLocOnPod(worker.cr)
	if clusterAppsPath == "" {
		// This should never happen
		scopedLog.ErrorContext(ctx, "could not find the cluster scoped apps location on the Pod")
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

	scopedLog := logging.FromContext(ctx).With("func", "runPodCopyWorker", "name", cr.GetName(), "namespace", cr.GetNamespace(), "appName", worker.appDeployInfo.AppName, "pod", worker.targetPodName)
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
		scopedLog.ErrorContext(ctx, "app package is missing", "podName", worker.targetPodName, "error", err)
		phaseInfo.Status = enterpriseApi.AppPkgMissingFromOperator
		return
	}

	// get the podExecClient to be used for copying file to pod
	// Use injected client if available (for testing), otherwise create real client
	podExecClient := worker.podExecClient
	if podExecClient == nil {
		podExecClient = splutil.GetPodExecClient(worker.client, cr, worker.targetPodName)
	}
	stdOut, stdErr, err := CopyFileToPod(ctx, worker.client, cr.GetNamespace(), appPkgLocalPath, appPkgPathOnPod, podExecClient)
	if err != nil {
		phaseInfo.FailCount++
		scopedLog.ErrorContext(ctx, "app package pod copy failed", "stdout", stdOut, "stderr", stdErr, "failCount", phaseInfo.FailCount, "error", err)
		return
	}

	if appSrcScope == enterpriseApi.ScopeCluster {
		err = extractClusterScopedAppOnPod(ctx, worker, appSrcScope, appPkgPathOnPod, appPkgLocalPath, podExecClient)
		if err != nil {
			phaseInfo.FailCount++
			scopedLog.ErrorContext(ctx, "extracting the app package on pod failed", "failCount", phaseInfo.FailCount, "error", err)
			return
		}
	}

	scopedLog.InfoContext(ctx, "podCopy complete", "appPkgPath", appPkgPathOnPod)
	phaseInfo.Status = enterpriseApi.AppPkgPodCopyComplete
}

// podCopyWorkerHandler fetches and runs the pod copy workers
func (pplnPhase *PipelinePhase) podCopyWorkerHandler(ctx context.Context, handlerWaiter *sync.WaitGroup, numPodCopyRunners int) {

	scopedLog := logging.FromContext(ctx).With("func", "podCopyWorkerHandler")
	defer handlerWaiter.Done()

	// Using the channel, derive a counting semaphore called podCopyRunPool that represents worker run pool
	// Try to get an active worker by queuing a msg to podCopyRunPool. Once the worker finishes it drains a msg from the channel.
	// So, indirectly serving the counting semaphore functionality. At any point in time, only numPodCopyRunners workers can
	// be running, as that is the channel max. capacity.
	var podCopyWorkerPool = make(chan struct{}, numPodCopyRunners)

podCopyHandler:
	for {
		select {
		case <-ctx.Done():
			scopedLog.InfoContext(ctx, "context cancelled, stopping pod copy worker handler")
			break podCopyHandler
		// get an idle worker
		case podCopyWorkerPool <- struct{}{}:
			select {
			case worker, channelOpen := <-pplnPhase.msgChannel:
				if !channelOpen {
					// Channel is closed, so, do not handle any more workers
					scopedLog.InfoContext(ctx, "worker channel closed")
					break podCopyHandler
				}

				if worker != nil {
					worker.waiter.Add(1)
					go runPodCopyWorker(ctx, worker, podCopyWorkerPool)
				} else {
					/// This should never happen
					scopedLog.ErrorContext(ctx, "invalid worker reference")
					<-podCopyWorkerPool
				}

			default:
				<-podCopyWorkerPool
			}
		default:
			// All the workers are busy, check after one second
			time.Sleep(phaseManagerBusyWaitDuration)
		}

		time.Sleep(phaseManagerLoopSleepDuration)
	}

	// Wait for all the workers to finish
	scopedLog.InfoContext(ctx, "waiting for all the workers to finish")
	pplnPhase.workerWaiter.Wait()
	scopedLog.InfoContext(ctx, "all the workers finished")
}

// podCopyPhaseManager creates pod copy phase manager for the install pipeline
func (ppln *AppInstallPipeline) podCopyPhaseManager(ctx context.Context) {

	scopedLog := logging.FromContext(ctx).With("func", "podCopyPhaseManager")
	scopedLog.InfoContext(ctx, "starting Pod copy phase manager")
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
		case <-ctx.Done():
			scopedLog.Info("Context cancelled, stopping pod copy phase manager")
			break podCopyPhase
		case _, channelOpen := <-ppln.sigTerm:
			if !channelOpen {
				scopedLog.InfoContext(ctx, "received the termination request from the scheduler")
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
						scopedLog.InfoContext(ctx, "pod copy worker got a run slot", "name", podCopyWorker.cr.GetName(), "namespace", podCopyWorker.cr.GetNamespace(), "podName", podCopyWorker.targetPodName, "appName", podCopyWorker.appDeployInfo.AppName, "digest", podCopyWorker.appDeployInfo.ObjectHash)
						podCopyWorker.isActive = true
					default:
						podCopyWorker.waiter = nil
					}
				}
			}
		}

		time.Sleep(phaseManagerLoopSleepDuration)
	}
}

// getInstallSlotForPod tries to allocate a local scoped install slot for a pod
func getInstallSlotForPod(ctx context.Context, installTracker []chan struct{}, podName string) bool {

	scopedLog := logging.FromContext(ctx).With("func", "getInstallSlotForPod")
	podID, err := getOrdinalValFromPodName(podName)
	if err != nil {
		scopedLog.ErrorContext(ctx, "unable to derive podId for podname", "podName", podName, "error", err)
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

	scopedLog := logging.FromContext(ctx).With("func", "freeInstallSlotForPod")
	podID, err := getOrdinalValFromPodName(podName)
	if err != nil {
		scopedLog.ErrorContext(ctx, "unable to derive podId for podname", "podName", podName, "error", err)
		return
	}

	select {
	case <-installTracker[podID]:
	default:
		scopedLog.ErrorContext(ctx, "trying to free an install slot without even allocating it")
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
	if afwPipeline.afwEntryTime+afwPipeline.appDeployContext.AppFrameworkConfig.SchedulerYieldInterval < time.Now().Unix() {
		return false
	}

	return true
}

// tryAppPkgCleanupFromOperatorPod tries to change the app install status, also cleans the app pkg from Operator Pod
func tryAppPkgCleanupFromOperatorPod(ctx context.Context, installWorker *PipelineWorker) {
	// Check for fanout CRs(standalone for now) and delete only
	// when installation is complete on all replicas
	if isFanOutApplicableToCR(installWorker.cr) {
		if isAppInstallationCompleteOnAllReplicas(installWorker.appDeployInfo.AuxPhaseInfo) {
			deleteAppPkgFromOperator(ctx, installWorker)
		}
	} else {
		deleteAppPkgFromOperator(ctx, installWorker)
	}
}

// installWorkerHandler fetches and runs the install workers
// local scope installs are handled first, then the cluster scoped apps are considered for bundle push
func (pplnPhase *PipelinePhase) installWorkerHandler(ctx context.Context, ppln *AppInstallPipeline, handlerWaiter *sync.WaitGroup, installTracker []chan struct{}) {

	scopedLog := logging.FromContext(ctx).With("func", "installWorkerHandler")
	defer handlerWaiter.Done()

installHandler:
	for {
		select {
		case <-ctx.Done():
			scopedLog.InfoContext(ctx, "context cancelled, stopping install worker handler")
			break installHandler
		case installWorker, channelOpen := <-pplnPhase.msgChannel:
			if !channelOpen {
				// Channel is closed, so, do not handle any more workers
				scopedLog.InfoContext(ctx, "worker channel closed")
				break installHandler
			}

			// Install workers can exist for local scope and premium app scopes
			if installWorker != nil {
				// Use injected client if available (for testing), otherwise create real client
				podExecClient := installWorker.podExecClient
				if podExecClient == nil {
					podExecClient = splutil.GetPodExecClient(installWorker.client, installWorker.cr, installWorker.targetPodName)
				}
				podID, _ := getOrdinalValFromPodName(installWorker.targetPodName)
				// Get app source spec
				appSrcSpec, err := getAppSrcSpec(installWorker.afwConfig.AppSources, installWorker.appSrcName)
				if err != nil {
					scopedLog.ErrorContext(ctx, "getting app source spec failed while installing app", "appSrcName", installWorker.appSrcName, "error", err)
				}

				// Get app source scope
				var appSrcScope string
				if appSrcSpec.Scope != "" {
					appSrcScope = appSrcSpec.Scope
				} else {
					appSrcScope = installWorker.afwConfig.Defaults.Scope
				}

				// Get insall worker playbook context, only support local or premiumApp scope context currently
				iwctx := getInsallWorkerPlaybookContext(ctx, installWorker, installTracker[podID], podExecClient, appSrcSpec, appSrcScope, ppln)
				if iwctx != nil {
					// Handle install work
					installWorker.waiter.Add(1)
					go iwctx.runPlaybook(ctx)
				} else {
					<-installTracker[podID]
					scopedLog.ErrorContext(ctx, "unable to get install worker context", "appName", installWorker.appDeployInfo.AppName)
				}
			} else {
				// This should never happen
				scopedLog.ErrorContext(ctx, "invalid worker reference")
			}

		default:
			time.Sleep(phaseManagerBusyWaitDuration)
		}

		time.Sleep(phaseManagerLoopSleepDuration)
	}

	for {
		select {
		case <-ctx.Done():
			scopedLog.InfoContext(ctx, "context cancelled, skipping cluster scoped playbook loop")
			goto clusterScopeDone
		default:
		}

		if needToRunClusterScopedPlaybook(ppln) {
			targetPodName := getApplicablePodNameForAppFramework(ppln.cr, 0)
			podExecClient := splutil.GetPodExecClient(ppln.client, ppln.cr, targetPodName)

			// sgontla: can we just pass the CR???
			ctxt := getClusterScopePlaybookContext(ctx, ppln.client, ppln.cr, ppln, targetPodName, ppln.cr.GetObjectKind().GroupVersionKind().Kind, podExecClient)
			if ctxt != nil {
				ctxt.runPlaybook(ctx)
			} else {
				scopedLog.ErrorContext(ctx, "unable to get the cluster scoped playbook context", "kind", ppln.cr.GroupVersionKind().Kind, "name", ppln.cr.GetName())
			}
		} else {
			break
		}

		// Sleep for a second before retry
		time.Sleep(phaseManagerBusyWaitDuration)
	}
clusterScopeDone:

	// Wait for all the workers to finish
	scopedLog.InfoContext(ctx, "waiting for all the workers to finish")
	pplnPhase.workerWaiter.Wait()
	scopedLog.InfoContext(ctx, "all the workers finished")
}

// installPhaseManager creates install phase manager for the afw installation pipeline
func (ppln *AppInstallPipeline) installPhaseManager(ctx context.Context) {

	scopedLog := logging.FromContext(ctx).With("func", "installPhaseManager")
	scopedLog.InfoContext(ctx, "starting Install phase manager")

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
		case <-ctx.Done():
			scopedLog.Info("Context cancelled, stopping install phase manager")
			break installPhase
		case _, channelOpen := <-ppln.sigTerm:
			if !channelOpen {
				scopedLog.InfoContext(ctx, "received the termination request from the scheduler")
				break installPhase
			}

		default:
			for _, installWorker := range pplnPhase.q {
				// Only local scope and premium apps scope can have install workers
				// Cluster scope has only bundle push no workers to install
				appScope := getAppSrcScope(ctx, installWorker.afwConfig, installWorker.appSrcName)
				if !canAppScopeHaveInstallWorker(appScope) {
					scopedLog.ErrorContext(ctx, "install worker with incorrect scope", "name", installWorker.cr.GetName(), "namespace", installWorker.cr.GetNamespace(), "podName", installWorker.targetPodName, "appName", installWorker.appDeployInfo.AppName, "digest", installWorker.appDeployInfo.ObjectHash, "scope", appScope)
					continue
				}

				phaseInfo := getPhaseInfoByPhaseType(ctx, installWorker, enterpriseApi.PhaseInstall)
				if isPhaseMaxRetriesReached(ctx, phaseInfo, installWorker.afwConfig) {
					phaseInfo.Status = enterpriseApi.AppPkgInstallError

					// For fanout CRs, also update the main PhaseInfo to reflect the failure
					if isFanOutApplicableToCR(installWorker.cr) {
						scopedLog.InfoContext(ctx, "max retries reached for fanout CR - updating main phase info", "app", installWorker.appDeployInfo.AppName, "failCount", phaseInfo.FailCount)
						installWorker.appDeployInfo.PhaseInfo.Phase = enterpriseApi.PhaseInstall
						installWorker.appDeployInfo.PhaseInfo.Status = enterpriseApi.AppPkgInstallError
						installWorker.appDeployInfo.DeployStatus = enterpriseApi.DeployStatusError
					}

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
						scopedLog.InfoContext(ctx, "install worker got a run slot", "name", installWorker.cr.GetName(), "namespace", installWorker.cr.GetNamespace(), "podName", installWorker.targetPodName, "appName", installWorker.appDeployInfo.AppName, "digest", installWorker.appDeployInfo.ObjectHash)

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

		time.Sleep(phaseManagerLoopSleepDuration)
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

// validatePhaseInfo validates if phase and status in phaseInfo is valid
func validatePhaseInfo(ctx context.Context, phaseInfo *enterpriseApi.PhaseInfo) bool {

	scopedLog := logging.FromContext(ctx).With("func", "validatePhaseInfo", "phaseInfo", phaseInfo)

	// Check for phase in phaseInfo
	phases := string(
		enterpriseApi.PhaseDownload +
			enterpriseApi.PhasePodCopy +
			enterpriseApi.PhaseInstall)

	if !strings.Contains(phases, string(phaseInfo.Phase)) {
		scopedLog.ErrorContext(ctx, "invalid phase in PhaseInfo", "phase", string(phaseInfo.Phase))
		return false
	}

	if ok := appPhaseInfoStatuses[phaseInfo.Status]; !ok {
		scopedLog.ErrorContext(ctx, "invalid status in PhaseInfo", "phase", string(phaseInfo.Phase), "status", phaseInfo.Status)
		return false
	}
	return true
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
	return kind == "ClusterMaster" || kind == "ClusterManager" || kind == "SearchHeadCluster"
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

	scopedLog := logging.FromContext(ctx).With("func", "deleteAppPkgFromOperator", "name", worker.cr.GetName(), "namespace", worker.cr.GetNamespace(), "appPkg", worker.appDeployInfo.AppName)

	appPkgLocalPath := getAppPackageLocalPath(ctx, worker)
	err := os.Remove(appPkgLocalPath)
	if err != nil {
		// Issue is local, so just log an error msg and return
		// ToDo: sgontla: For any transient errors, handle the clean-up at the end of the install
		scopedLog.ErrorContext(ctx, "failed to delete app pkg from Operator", "appPkgPath", appPkgLocalPath, "error", err)
		return
	}

	scopedLog.InfoContext(ctx, "deleted app package from the operator", "appPkgPath", appPkgLocalPath)
	releaseStorage(worker.appDeployInfo.Size)
}

func afwGetReleventStatefulsetByKind(ctx context.Context, cr splcommon.MetaObject, client splcommon.ControllerClient) *appsv1.StatefulSet {

	scopedLog := logging.FromContext(ctx).With("func", "getReleventStatefulsetByKind", "name", cr.GetName(), "namespace", cr.GetNamespace())
	var instanceID InstanceType

	switch cr.GetObjectKind().GroupVersionKind().Kind {
	case "Standalone":
		instanceID = SplunkStandalone
	case "LicenseManager":
		instanceID = SplunkLicenseManager
	case "LicenseMaster":
		instanceID = SplunkLicenseMaster
	case "SearchHeadCluster":
		instanceID = SplunkDeployer
	case "ClusterMaster":
		instanceID = SplunkClusterMaster
	case "ClusterManager":
		instanceID = SplunkClusterManager
	case "MonitoringConsole":
		instanceID = SplunkMonitoringConsole
	case "IngestorCluster":
		instanceID = SplunkIngestor
	default:
		return nil
	}

	statefulsetName := GetSplunkStatefulsetName(instanceID, cr.GetName())
	namespacedName := types.NamespacedName{Namespace: cr.GetNamespace(), Name: statefulsetName}
	sts, err := k8sops.GetStatefulSetByName(ctx, client, namespacedName)
	if err != nil {
		scopedLog.ErrorContext(ctx, "unable to get the stateful set", "error", err)
	}

	return sts
}

// getPremiumAppScopePlaybookContext returns the premium apps scope playbook context
func getPremiumAppScopePlaybookContext(ctx context.Context, localCtx *localScopePlaybookContext, appSrcSpec *enterpriseApi.AppSourceSpec, client splcommon.ControllerClient, cr splcommon.MetaObject, afwPipeline *AppInstallPipeline) *premiumAppScopePlaybookContext {
	return &premiumAppScopePlaybookContext{
		localCtx:    localCtx,
		appSrcSpec:  appSrcSpec,
		client:      client,
		cr:          cr,
		afwPipeline: afwPipeline,
	}
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
func getLocalScopePlaybookContext(ctx context.Context, installWorker *PipelineWorker, sem chan struct{}, podExecClient splutil.PodExecClientImpl) *localScopePlaybookContext {
	return &localScopePlaybookContext{
		worker:        installWorker,
		sem:           sem,
		podExecClient: podExecClient,
	}
}

// getInsallWorkerPlaybookContext returns the playbook context for install workers i.e either local
// or premiumApps scope for now
func getInsallWorkerPlaybookContext(ctx context.Context, worker *PipelineWorker, sem chan struct{}, podExecClient splutil.PodExecClientImpl, appSrcSpec *enterpriseApi.AppSourceSpec, appSrcScope string, ppln *AppInstallPipeline) PlaybookImpl {

	scopedLog := logging.FromContext(ctx).With("func", "getInsallWorkerPlaybookContext", "crName", ppln.cr.GetName(), "namespace", ppln.cr.GetNamespace())

	// Since local app context is needed for premiumAppContext we retrieve it for both cases
	localCtx := getLocalScopePlaybookContext(ctx, worker, sem, podExecClient)
	if appSrcScope == enterpriseApi.ScopeLocal {
		return localCtx
	} else if appSrcScope == enterpriseApi.ScopePremiumApps {
		return getPremiumAppScopePlaybookContext(ctx, localCtx, appSrcSpec, ppln.client, ppln.cr, ppln)
	}

	// Invalid scope
	scopedLog.ErrorContext(ctx, "install workers can have only local or premium apps scope", "appSrcScope", appSrcScope)

	return nil
}

// getClusterScopePlaybookContext returns the context for running playbook
func getClusterScopePlaybookContext(ctx context.Context, client splcommon.ControllerClient, cr splcommon.MetaObject, afwPipeline *AppInstallPipeline, podName string, kind string, podExecClient splutil.PodExecClientImpl) PlaybookImpl {

	switch kind {
	case "ClusterManager", "ClusterMaster":
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

	scopedLog := logging.FromContext(ctx).With("func", "isBundlePushComplete", "crName", shcPlaybookContext.cr.GetName(), "namespace", shcPlaybookContext.cr.GetNamespace())

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
			err = errors.Wrap(err, removeErr.Error())
		}
		return false, err
	}

	// Check if we did not get the desired output in the status file. There can be 3 scenarios -
	// 1. stdOut is empty, which means bundle push is still in progress
	// 2. stdOut contains only informational lines (e.g. the FIPS provider banner written to
	//    stderr by the Splunk CLI on FIPS-enabled clusters, captured via the &> shell redirect
	//    in applySHCBundleCmdStr before the actual push output is written)
	// 3. stdOut has some other string other than the bundle push success message
	if stdOut == "" {
		scopedLog.InfoContext(ctx, "SHC Bundle Push is still in progress")
		return false, nil
	} else if !strings.Contains(stdOut, shcBundlePushCompleteStr) {
		// Check whether the file contains only known informational lines. On FIPS-enabled
		// clusters the Splunk binary immediately writes the FIPS provider banner (and SSL
		// warnings) to stderr at startup; because the bundle push command uses &> to
		// redirect all output to the status file, these lines appear in the file before the
		// actual push result. Treat such content as "still in progress" so we do not
		// prematurely abort a running push and trigger a retry storm.
		//
		// IMPORTANT: SSL certificate warnings ("WARNING: Server Certificate ...") are only
		// treated as informational when the FIPS provider banner is also present. On non-FIPS
		// clusters the Splunk CLI can also emit SSL warnings (e.g. when hostname validation is
		// disabled), but if those warnings are the only content in the status file it means the
		// push failed silently — we must fall through to error/retry rather than waiting forever.
		hasFIPSContent := strings.Contains(stdOut, splunkFIPSProviderBannerStr)
		hasMeaningfulContent := false
		for _, line := range strings.Split(stdOut, "\n") {
			trimmed := strings.TrimSpace(line)
			if trimmed == "" ||
				strings.HasPrefix(trimmed, splunkFIPSProviderBannerStr) ||
				(hasFIPSContent && strings.HasPrefix(trimmed, splunkSSLCertWarnStr)) {
				continue
			}
			hasMeaningfulContent = true
			break
		}
		if !hasMeaningfulContent {
			scopedLog.InfoContext(ctx, "SHC Bundle Push is still in progress (status file contains only informational messages)")
			return false, nil
		}

		// this means there was an error in bundle push command
		err = fmt.Errorf("there was an error in applying SHC Bundle, err=\"%v\"", stdOut)
		scopedLog.ErrorContext(ctx, "SHC Bundle push status file reported an error while applying bundle", "error", err)

		// reset the bundle push state to Pending, so that we retry again.
		setBundlePushState(ctx, shcPlaybookContext.afwPipeline, enterpriseApi.BundlePushPending)

		// remove the status file too, so that we dont have any stale status
		removeErr := shcPlaybookContext.removeSHCBundlePushStatusFile(ctx)
		if removeErr != nil {
			err = errors.Wrap(err, removeErr.Error())
		}
		return false, err
	}

	// now that bundle push is complete, remove the status file
	err = shcPlaybookContext.removeSHCBundlePushStatusFile(ctx)
	if err != nil {
		scopedLog.ErrorContext(ctx, "removing SHC Bundle Push status file failed, will retry again", "error", err)

		// reset the state to BundlePushInProgress so that we can check the status of file again.
		setBundlePushState(ctx, shcPlaybookContext.afwPipeline, enterpriseApi.BundlePushInProgress)

		// don't return error from here, so that we can retry cleaning the file in next run
		return false, nil
	}

	return true, nil
}

// triggerBundlePush triggers the bundle push operation for SHC
func (shcPlaybookContext *SHCPlaybookContext) triggerBundlePush(ctx context.Context) error {

	scopedLog := logging.FromContext(ctx).With("func", "shcPlaybookContext.triggerBundlePush",
		"shcCaptainUrl", shcPlaybookContext.searchHeadCaptainURL,
		"cr", shcPlaybookContext.cr.GetName())

	// Reduce the liveness probe level
	shcPlaybookContext.setLivenessProbeLevel(ctx, livenessProbeLevelOne)

	// Trigger bundle push
	adminPwd, err := splutil.GetAdminPasswordFromNamespaceScopedSecret(ctx, shcPlaybookContext.client, shcPlaybookContext.cr.GetNamespace())
	if err != nil {
		scopedLog.ErrorContext(ctx, "failed to retrieve admin password", "error", err)
		return err
	}
	cmd := fmt.Sprintf(applySHCBundleCmdStr, shcPlaybookContext.searchHeadCaptainURL, shellQuote(adminPwd), shcBundlePushStatusCheckFile)
	scopedLog.Info("Triggering bundle push", "command", redactSplunkAuth(cmd, adminPwd))

	streamOptions := splutil.NewStreamOptionsObject(cmd)
	stdOut, stdErr, err := shcPlaybookContext.podExecClient.RunPodExecCommand(ctx, streamOptions, []string{"/bin/sh"})
	if err != nil || stdErr != "" {
		err = fmt.Errorf("error while applying SHC Bundle. stdout: %s, stderr: %s, err: %v", stdOut, stdErr, err)
		return err
	}
	return nil
}

// setLivenessProbeLevel sets the liveness probe level across all the Search Head Pods.
func (shcPlaybookContext *SHCPlaybookContext) setLivenessProbeLevel(ctx context.Context, probeLevel int) error {

	scopedLog := logging.FromContext(ctx).With("func", "shcPlaybookContext.setLivenessProbeLevel")

	shcStsName := GetSplunkStatefulsetName(SplunkSearchHead, shcPlaybookContext.cr.GetName())
	shcStsNamespaceName := types.NamespacedName{Namespace: shcPlaybookContext.cr.GetNamespace(), Name: shcStsName}
	shcSts, err := k8sops.GetStatefulSetByName(ctx, shcPlaybookContext.client, shcStsNamespaceName)
	if err != nil {
		scopedLog.ErrorContext(ctx, "unable to get the stateful set", "error", err)
		return err
	}

	err = func() error {
		// playbook context uses fixed CR and target pod names, but, when it comes to the
		// probes tuning, we are mostly dealing with different pods, and also CRs,
		// so, backup and then restore
		cr := shcPlaybookContext.podExecClient.GetCR()
		targetPodname := shcPlaybookContext.podExecClient.GetTargetPodName()

		defer func() {
			shcPlaybookContext.podExecClient.SetCR(cr)
			shcPlaybookContext.podExecClient.SetTargetPodName(ctx, targetPodname)
		}()

		err = setProbeLevelOnCRPods(ctx, shcPlaybookContext.cr, *shcSts.Spec.Replicas, shcPlaybookContext.podExecClient, probeLevel)
		if err != nil {
			scopedLog.ErrorContext(ctx, "unable to set the Liveness probe level", "error", err)
			return err
		}

		return err
	}()

	return err
}

// getClusterScopedAppsLocOnPod returns the cluster apps directory
func getClusterScopedAppsLocOnPod(cr splcommon.MetaObject) string {
	switch cr.GetObjectKind().GroupVersionKind().Kind {
	case "ClusterManager", "ClusterMaster":
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

	scopedLog := logging.FromContext(ctx).With("func", "runPlaybook", "crName", shcPlaybookContext.cr.GetName(), "namespace", shcPlaybookContext.cr.GetNamespace())

	var err error
	var ok bool
	cr, ok := shcPlaybookContext.cr.(*enterpriseApi.SearchHeadCluster)
	if !ok {
		return nil
	}
	if cr.Status.Phase != enterpriseApi.PhaseReady {
		scopedLog.InfoContext(ctx, "SHC is not ready yet")
		return nil
	}

	appDeployContext := shcPlaybookContext.afwPipeline.appDeployContext

	switch appDeployContext.BundlePushStatus.BundlePushStage {
	// if the bundle push is already in progress, check the status
	case enterpriseApi.BundlePushInProgress:
		scopedLog.InfoContext(ctx, "checking the status of SHC Bundle Push")
		// check if the bundle push is complete
		ok, err = shcPlaybookContext.isBundlePushComplete(ctx)
		if ok {
			scopedLog.InfoContext(ctx, "bundle push complete, setting bundle push state in CR")

			// set the bundle push status to complete
			setBundlePushState(ctx, shcPlaybookContext.afwPipeline, enterpriseApi.BundlePushComplete)

			// reset the retry count
			shcPlaybookContext.afwPipeline.appDeployContext.BundlePushStatus.RetryCount = 0

			// set the state to install complete for all the cluster scoped apps
			setInstallStateForClusterScopedApps(ctx, appDeployContext)

			// set the liveness probe to default
			shcPlaybookContext.setLivenessProbeLevel(ctx, livenessProbeLevelDefault)
		} else if err != nil {
			scopedLog.ErrorContext(ctx, "there was an error in SHC bundle push, will retry again", "error", err)
		} else {
			scopedLog.InfoContext(ctx, "SHC Bundle Push is still in progress, will check back again")
		}
	case enterpriseApi.BundlePushPending:
		// run the command to apply cluster bundle
		scopedLog.InfoContext(ctx, "running command to apply SHC Bundle")

		// Adjust the file permissions
		err = adjustClusterAppsFilePermissions(ctx, shcPlaybookContext.podExecClient)
		if err != nil {
			scopedLog.ErrorContext(ctx, "failed to adjust the file permissions", "error", err)
			return err
		}

		err = shcPlaybookContext.triggerBundlePush(ctx)
		if err != nil {
			scopedLog.ErrorContext(ctx, "failed to apply SHC Bundle", "error", err)
			return err
		}

		scopedLog.InfoContext(ctx, "SHC Bundle Push is in progress")

		// set the state to bundle push complete since SHC bundle push is a sync call
		setBundlePushState(ctx, shcPlaybookContext.afwPipeline, enterpriseApi.BundlePushInProgress)
	default:
		err = fmt.Errorf("invalid bundle push state=%s", bundlePushStateAsStr(ctx, appDeployContext.BundlePushStatus.BundlePushStage))
	}

	return err
}

// isBundlePushComplete checks the status of bundle push
func (idxcPlaybookContext *IdxcPlaybookContext) isBundlePushComplete(ctx context.Context) bool {

	scopedLog := logging.FromContext(ctx).With("func", "isBundlePushComplete", "crName", idxcPlaybookContext.cr.GetName(), "namespace", idxcPlaybookContext.cr.GetNamespace())

	adminPwd, err := splutil.GetAdminPasswordFromNamespaceScopedSecret(ctx, idxcPlaybookContext.client, idxcPlaybookContext.cr.GetNamespace())
	if err != nil {
		scopedLog.ErrorContext(ctx, "failed to retrieve admin password", "error", err)
		return false
	}
	streamOptions := splutil.NewStreamOptionsObject(fmt.Sprintf(idxcShowClusterBundleStatusStr, shellQuote(adminPwd)))
	stdOut, stdErr, err := idxcPlaybookContext.podExecClient.RunPodExecCommand(ctx, streamOptions, []string{"/bin/sh"})
	if err == nil && strings.Contains(stdOut, "cluster_status=None") && !strings.Contains(stdOut, "last_bundle_validation_status=failure") {
		scopedLog.InfoContext(ctx, "IndexerCluster Bundle push complete")
		return true
	}

	if err != nil || stdErr != "" {
		scopedLog.ErrorContext(ctx, "show cluster-bundle-status failed", "stdout", stdOut, "stderr", stdErr, "error", err)
		return false
	}

	scopedLog.InfoContext(ctx, "IndexerCluster Bundle push is still in progress")
	return false
}

// triggerBundlePush triggers the bundle push for indexer cluster
func (idxcPlaybookContext *IdxcPlaybookContext) triggerBundlePush(ctx context.Context) error {

	scopedLog := logging.FromContext(ctx).With("func", "idxcPlaybookContext.triggerBundlePush")

	// Reduce the liveness probe level
	idxcPlaybookContext.setLivenessProbeLevel(ctx, livenessProbeLevelOne)
	adminPwd, err := splutil.GetAdminPasswordFromNamespaceScopedSecret(ctx, idxcPlaybookContext.client, idxcPlaybookContext.cr.GetNamespace())
	if err != nil {
		scopedLog.ErrorContext(ctx, "failed to retrieve admin password", "error", err)
		return err
	}
	streamOptions := splutil.NewStreamOptionsObject(fmt.Sprintf(applyIdxcBundleCmdStr, shellQuote(adminPwd)))
	stdOut, stdErr, err := idxcPlaybookContext.podExecClient.RunPodExecCommand(ctx, streamOptions, []string{"/bin/sh"})

	// If the error is due to a bundle which is already present, don't do anything.
	// In the next reconcile we will mark it as bundle push complete
	if strings.Contains(stdErr, idxcBundleAlreadyPresentStr) {
		scopedLog.InfoContext(ctx, "bundle already present on peers")
	} else if err != nil || !strings.Contains(stdErr, "OK\n") {
		err = fmt.Errorf("error while applying cluster bundle. stdout: %s, stderr: %s, err: %v", stdOut, stdErr, err)
		return err
	}

	return nil
}

// setLivenessProbeLevel sets the liveness probe level across all the indexer pods
func (idxcPlaybookContext *IdxcPlaybookContext) setLivenessProbeLevel(ctx context.Context, probeLevel int) error {

	scopedLog := logging.FromContext(ctx).With("func", "idxcPlaybookContext.setLivenessProbeLevel")
	var err error

	managerSts := afwGetReleventStatefulsetByKind(ctx, idxcPlaybookContext.cr, idxcPlaybookContext.client)
	if managerSts == nil {
		return fmt.Errorf("not able to retrieve Cluster Manager STS")
	}

	err = func() error {
		// playbook context uses fixed CR and target pod names, but, when it comes to the
		// probes tuning, we are mostly dealing with different pods, and also CRs,
		// so, backup and then restore
		cr := idxcPlaybookContext.podExecClient.GetCR()
		targetPodname := idxcPlaybookContext.podExecClient.GetTargetPodName()

		defer func() {
			idxcPlaybookContext.podExecClient.SetCR(cr)
			idxcPlaybookContext.podExecClient.SetTargetPodName(ctx, targetPodname)
		}()

		managerOwnerRefs := managerSts.GetOwnerReferences()
		for i := 0; i < len(managerOwnerRefs); i++ {
			// We are only interested for Indexer pods, skip all other references
			if managerOwnerRefs[i].Kind != "IndexerCluster" {
				continue
			}

			idxcNameSpaceName := types.NamespacedName{Namespace: idxcPlaybookContext.cr.GetNamespace(), Name: managerOwnerRefs[i].Name}
			var idxcCR enterpriseApi.IndexerCluster
			err = idxcPlaybookContext.client.Get(ctx, idxcNameSpaceName, &idxcCR)
			if err != nil {
				// Probably a dangling owner reference, just ignore and continue
				scopedLog.ErrorContext(ctx, "unable to fetch the CR", "Name", managerOwnerRefs[i].Name, "Namespace", idxcPlaybookContext.cr.GetNamespace(), "error", err)
				continue
			}

			idxcStsName := GetSplunkStatefulsetName(SplunkIndexer, idxcCR.GetName())
			idxcStsNamespaceName := types.NamespacedName{Namespace: idxcCR.GetNamespace(), Name: idxcStsName}
			idxcSts, err := k8sops.GetStatefulSetByName(ctx, idxcPlaybookContext.client, idxcStsNamespaceName)
			if err != nil {
				scopedLog.ErrorContext(ctx, "unable to get the stateful set", "error", err)
				// Probably a dangling owner reference, just ignore and continue
				continue
			}

			err = setProbeLevelOnCRPods(ctx, &idxcCR, *idxcSts.Spec.Replicas, idxcPlaybookContext.podExecClient, probeLevel)
			if err != nil {
				scopedLog.ErrorContext(ctx, "unable to set the Liveness probe level", "error", err)
				return err
			}
		}
		return err
	}()

	return err
}

// runPlaybook will implement the following logic(and set the bundle push state accordingly)  -
// 1. If the bundle push is not in progress, run the logic to push the bundle from CM to indexer peers
// 2. OR else, if the bundle push is already in progress, check the status of bundle push
func (idxcPlaybookContext *IdxcPlaybookContext) runPlaybook(ctx context.Context) error {

	scopedLog := logging.FromContext(ctx).With("func", "RunPlaybook", "crName", idxcPlaybookContext.cr.GetName(), "namespace", idxcPlaybookContext.cr.GetNamespace())

	appDeployContext := idxcPlaybookContext.afwPipeline.appDeployContext

	switch appDeployContext.BundlePushStatus.BundlePushStage {
	// if the bundle push is already in progress, check the status
	case enterpriseApi.BundlePushInProgress:
		scopedLog.InfoContext(ctx, "checking the status of IndexerCluster Bundle Push")
		// check if the bundle push is complete
		if idxcPlaybookContext.isBundlePushComplete(ctx) {
			// set the bundle push status to complete
			setBundlePushState(ctx, idxcPlaybookContext.afwPipeline, enterpriseApi.BundlePushComplete)

			// reset the retry count
			idxcPlaybookContext.afwPipeline.appDeployContext.BundlePushStatus.RetryCount = 0

			// set the state to install complete for all the cluster scoped apps
			setInstallStateForClusterScopedApps(ctx, appDeployContext)
			idxcPlaybookContext.setLivenessProbeLevel(ctx, livenessProbeLevelDefault)
		} else {
			scopedLog.InfoContext(ctx, "IndexerCluster Bundle Push is still in progress, will check back again in next reconcile")
		}

	case enterpriseApi.BundlePushPending:
		// Adjust the file permissions
		err := adjustClusterAppsFilePermissions(ctx, idxcPlaybookContext.podExecClient)
		if err != nil {
			scopedLog.ErrorContext(ctx, "failed to adjust the file permissions", "error", err)
			return err
		}

		// run the command to apply cluster bundle
		scopedLog.InfoContext(ctx, "running command to apply IndexerCluster Bundle")
		err = idxcPlaybookContext.triggerBundlePush(ctx)
		if err != nil {
			scopedLog.ErrorContext(ctx, "failed to apply IndexerCluster Bundle", "error", err)
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

// getSslCliOption gets the ssl cli option for installing ES app.
// Returns `strict` if not configured. Note: Validation of spec done already
// Reference: https://docs.splunk.com/Documentation/ES/latest/Install/InstallEnterpriseSecuritySHC
func getSslCliOption(appSrcSpec *enterpriseApi.AppSourceSpec) string {
	sslEn := appSrcSpec.PremiumAppsProps.EsDefaults.SslEnablement
	if sslEn != "" {
		return sslEn
	}

	return enterpriseApi.SslEnablementStrict
}

// Handles ES app post install steps
func handleEsappPostinstall(rctx context.Context, preCtx *premiumAppScopePlaybookContext, phaseInfo *enterpriseApi.PhaseInfo) error {
	worker := preCtx.localCtx.worker
	cr := preCtx.cr
	appSrcSpec := preCtx.appSrcSpec

	scopedLog := logging.FromContext(rctx).With("func", "handleEsappPostinstall", "name", cr.GetName(), "namespace", cr.GetNamespace(), "pod", worker.targetPodName, "appName", worker.appDeployInfo.AppName)

	// For ES app, run post-install commands
	var command string

	// Create CLI command
	adminPwd, err := splutil.GetAdminPasswordFromNamespaceScopedSecret(rctx, preCtx.client, cr.GetNamespace())
	if err != nil {
		scopedLog.ErrorContext(rctx, "failed to retrieve admin password", "error", err)
		return err
	}
	sslEn := getSslCliOption(appSrcSpec)
	if cr.GetObjectKind().GroupVersionKind().Kind != "SearchHeadCluster" {
		command = fmt.Sprintf("/opt/splunk/bin/splunk search '| essinstall --ssl_enablement %s' -auth admin:%s", sslEn, shellQuote(adminPwd))
	} else {
		// Pass an extra parameter for SHC deployer in post install command
		command = fmt.Sprintf("/opt/splunk/bin/splunk search '| essinstall --ssl_enablement %s --deployment_type shc_deployer' -auth admin:%s", sslEn, shellQuote(adminPwd))
	}

	streamOptions := splutil.NewStreamOptionsObject(command)
	stdOut, stdErr, err := preCtx.localCtx.podExecClient.RunPodExecCommand(rctx, streamOptions, []string{"/bin/sh"})

	// Log stderr content for debugging but don't use it for error detection.
	// On FIPS-enabled clusters the Splunk CLI always writes the FIPS provider
	// banner and related informational messages to stderr on every invocation,
	// so a non-empty stderr does not indicate failure.
	if stdErr != "" {
		scopedLog.InfoContext(rctx, "Post install command stderr output (informational only)", "stdout", stdOut, "stderr", stdErr, "command", redactSplunkAuth(command, adminPwd))
	}

	if err != nil {
		phaseInfo.FailCount++
		scopedLog.ErrorContext(rctx, "premium scoped app package install failed", "stdout", stdOut, "stderr", stdErr, "command", redactSplunkAuth(command, adminPwd), "failCount", phaseInfo.FailCount, "error", err)
		return fmt.Errorf("premium scoped app package install failed. stdOut: %s, stdErr: %s, command: %s, failCount: %d", stdOut, stdErr, redactSplunkAuth(command, adminPwd), phaseInfo.FailCount)
	}

	return nil
}

// runPlaybook implements installing the app for premiumApps
// For ES app:
//  1. Installs the app like any other app on standalone/SHC deployer
//  2. Runs the post install command for the ES app
//  3. Sets the bundle push flag for the deployer only
func (preCtx *premiumAppScopePlaybookContext) runPlaybook(rctx context.Context) error {
	cr := preCtx.cr
	worker := preCtx.localCtx.worker
	appSrcSpec := preCtx.appSrcSpec

	scopedLog := logging.FromContext(rctx).With("func", "premiumAppScopePlaybookContext.runPlaybook", "name", cr.GetName(), "namespace", cr.GetNamespace(), "pod", worker.targetPodName, "appName", worker.appDeployInfo.AppName)

	defer func() {
		<-preCtx.localCtx.sem
		worker.isActive = false
		worker.waiter.Done()
	}()

	// Get phase info
	phaseInfo := getPhaseInfoByPhaseType(rctx, worker, enterpriseApi.PhaseInstall)

	// Call the API to install an app
	err := installApp(rctx, preCtx.localCtx, cr, phaseInfo)
	if err != nil {
		scopedLog.ErrorContext(rctx, "premium app package installation error", "error", err)
		return fmt.Errorf("app pkg installation failed. error %s", err.Error())
	}

	// Handle post install for ES app
	if appSrcSpec.PremiumAppsProps.Type == enterpriseApi.PremiumAppsTypeEs {
		err = handleEsappPostinstall(rctx, preCtx, phaseInfo)
		if err != nil {
			scopedLog.ErrorContext(rctx, "app package post installation error", "error", err)
			return fmt.Errorf("app pkg post installation failed. error %s", err.Error())
		}
	}

	// Mark app package installation complete
	markWorkerPhaseInstallationComplete(rctx, phaseInfo, worker)

	// Call the API to clean up app
	err = cleanupApp(rctx, preCtx.localCtx, cr, phaseInfo)
	if err != nil {
		scopedLog.ErrorContext(rctx, "premium app package installation error", "error", err)
		return fmt.Errorf("app pkg installation failed. error %s", err.Error())
	}

	// Mark afw pipeline for bundle push on shc deployer
	if cr.GetObjectKind().GroupVersionKind().Kind == "SearchHeadCluster" {
		preCtx.afwPipeline.appDeployContext.BundlePushStatus.BundlePushStage = enterpriseApi.BundlePushPending
	}

	// All good!
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

	// check if phase, status in phaseInfo is valid
	if !validatePhaseInfo(ctx, phaseInfo) {
		return false
	}
	return true
}

// afwSchedulerEntry Starts the scheduler Pipeline with the required phases
func afwSchedulerEntry(ctx context.Context, client splcommon.ControllerClient, cr splcommon.MetaObject, appDeployContext *enterpriseApi.AppDeploymentContext, appFrameworkConfig *enterpriseApi.AppFrameworkSpec) (bool, error) {

	scopedLog := logging.FromContext(ctx).With("func", "afwSchedulerEntry", "name", cr.GetName(), "namespace", cr.GetNamespace())

	// return error, if there is no storage defined for the Operator pod
	if !isPersistentVolConfigured() {
		return true, fmt.Errorf("persistent volume required for the App framework, but not provisioned")
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

	scopedLog.InfoContext(ctx, "creating pipeline workers for pending app packages")

	for appSrcName, appSrcDeployInfo := range appDeployContext.AppsSrcDeployStatus {

		deployInfoList := appSrcDeployInfo.AppDeploymentInfoList

		sts := afwGetReleventStatefulsetByKind(ctx, cr, client)
		podName := getApplicablePodNameForAppFramework(cr, 0)

		podExecClient := splutil.GetPodExecClient(client, cr, podName)
		appsPathOnPod := filepath.Join(appBktMnt, appSrcName)

		// create the dir on Splunk pod/s where app/s will be copied from operator pod
		err = createDirOnSplunkPods(ctx, cr, *sts.Spec.Replicas, appsPathOnPod, podExecClient)
		if err != nil {
			scopedLog.ErrorContext(ctx, "unable to create directory on splunk pod", "error", err)
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

	scopedLog.InfoContext(ctx, "waiting for the phase managers to finish")

	// Wait for all the pipeline managers to finish
	afwPipeline.phaseWaiter.Wait()
	scopedLog.InfoContext(ctx, "all the phase managers finished")

	// Finally mark if all the App framework is complete
	checkAndUpdateAppFrameworkProgressFlag(afwPipeline)

	return needToRevisitAppFramework(afwPipeline), nil
}

// afwYieldWatcher issues termination request to the scheduler when the yield time expires or the pipelines become empty.
func (ppln *AppInstallPipeline) afwYieldWatcher(ctx context.Context) {

	scopedLog := logging.FromContext(ctx).With("func", "afwYieldWatcher", "name", ppln.cr.GetName(), "namespace", ppln.cr.GetNamespace())
	yieldTrigger := time.After(time.Duration(ppln.appDeployContext.AppFrameworkConfig.SchedulerYieldInterval) * time.Second)

yieldScheduler:
	for {
		select {
		case <-ctx.Done():
			// Observe cancellation directly instead of relying on the phase
			// managers to drain the pipeline first — they may still have
			// queued workers when SIGTERM lands, which would otherwise delay
			// closing sigTerm until SchedulerYieldInterval expires.
			scopedLog.InfoContext(ctx, "yielding from AFW scheduler due to context cancellation")
			break yieldScheduler
		case <-yieldTrigger:
			scopedLog.InfoContext(ctx, "yielding from AFW scheduler", "timeElapsed", time.Now().Unix()-ppln.afwEntryTime)
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
	scopedLog.InfoContext(ctx, "termination issued")
}
