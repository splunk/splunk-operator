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
	"strconv"
	"strings"
	"sync"
	"time"

	enterpriseApi "github.com/splunk/splunk-operator/pkg/apis/enterprise/v2"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	splctrl "github.com/splunk/splunk-operator/pkg/splunk/controller"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/types"
)

// handle to hold the pipeline
var afwPipeline *AppInstallPipeline

// createPipelineWorker creates a pipeline worker for an app package
func createPipelineWorker(appDeployInfo *enterpriseApi.AppDeploymentInfo, appSrcName string, podName string,
	appFrameworkConfig *enterpriseApi.AppFrameworkSpec, client *splcommon.ControllerClient,
	cr splcommon.MetaObject, statefulSet *appsv1.StatefulSet) *PipelineWorker {
	return &PipelineWorker{
		appDeployInfo: appDeployInfo,
		appSrcName:    appSrcName,
		targetPodName: podName,
		afwConfig:     appFrameworkConfig,
		client:        client,
		cr:            cr,
		sts:           statefulSet,
	}
}

// createAndAddAWorker used to add a worker to the pipeline on reconcile re-entry
func (ppln *AppInstallPipeline) createAndAddPipelineWorker(phase enterpriseApi.AppPhaseType, appDeployInfo *enterpriseApi.AppDeploymentInfo,
	appSrcName string, podName string, appFrameworkConfig *enterpriseApi.AppFrameworkSpec,
	client splcommon.ControllerClient, cr splcommon.MetaObject, statefulSet *appsv1.StatefulSet) {

	scopedLog := log.WithName("createAndAddPipelineWorker").WithValues("name", cr.GetName(), "namespace", cr.GetNamespace())
	worker := createPipelineWorker(appDeployInfo, appSrcName, podName, appFrameworkConfig, &client, cr, statefulSet)

	if worker != nil {
		scopedLog.Info("Created new worker", "Pod name", worker.targetPodName, "App name", appDeployInfo.AppName, "digest", appDeployInfo.ObjectHash, "phase", appDeployInfo.PhaseInfo.Phase)
		ppln.addWorkersToPipelinePhase(phase, worker)
	}
}

// getApplicablePodNameForWorker gets the Pod name relevant for the CR under work
func getApplicablePodNameForWorker(cr splcommon.MetaObject, ordinalIdx int) string {
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
func (ppln *AppInstallPipeline) addWorkersToPipelinePhase(phaseID enterpriseApi.AppPhaseType, workers ...*PipelineWorker) {
	scopedLog := log.WithName("addWorkersToPipelinePhase").WithValues("phase", phaseID)

	for _, worker := range workers {
		scopedLog.Info("Adding worker", "name", worker.cr.GetName(), "namespace", worker.cr.GetNamespace(), "Pod name", worker.targetPodName, "App name", worker.appDeployInfo.AppName, "digest", worker.appDeployInfo.ObjectHash)
	}
	ppln.pplnPhases[phaseID].mutex.Lock()
	ppln.pplnPhases[phaseID].q = append(ppln.pplnPhases[phaseID].q, workers...)
	ppln.pplnPhases[phaseID].mutex.Unlock()
}

// deleteWorkerFromPipelinePhase deletes a given worker from a pipeline phase
func (ppln *AppInstallPipeline) deleteWorkerFromPipelinePhase(phaseID enterpriseApi.AppPhaseType, worker *PipelineWorker) bool {
	scopedLog := log.WithName("deleteWorkerFromPipelinePhase").WithValues("phase", phaseID)
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

// setContextForNewPhase makes the worker ready for the new phase
func setContextForNewPhase(worker *PipelineWorker, phaseInfo *enterpriseApi.PhaseInfo, nextPhase enterpriseApi.AppPhaseType) {
	phaseInfo.Phase = nextPhase
	phaseInfo.RetryCount = 0
	if nextPhase == enterpriseApi.PhaseDownload {
		phaseInfo.Status = enterpriseApi.AppPkgDownloadPending
	} else if nextPhase == enterpriseApi.PhasePodCopy {
		phaseInfo.Status = enterpriseApi.AppPkgPodCopyPending
	} else if nextPhase == enterpriseApi.PhaseInstall {
		phaseInfo.Status = enterpriseApi.AppPkgInstallPending
	}

	worker.isActive = false
	worker.waiter = nil
}

// TransitionWorkerPhase transitions a worker to new phase, and deletes from the current phase
// In the case of Standalone CR with multiple replicas, Fan-out `replicas` number of new workers
func (ppln *AppInstallPipeline) TransitionWorkerPhase(worker *PipelineWorker, currentPhase, nextPhase enterpriseApi.AppPhaseType) {
	kind := worker.cr.GetObjectKind().GroupVersionKind().Kind

	scopedLog := log.WithName("TransitionWorkerPhase").WithValues("name", worker.cr.GetName(), "namespace", worker.cr.GetNamespace(), "App name", worker.appDeployInfo.AppName, "digest", worker.appDeployInfo.ObjectHash, "pod name", worker.targetPodName, "current Phase", currentPhase, "next phase", nextPhase)

	var replicaCount int32
	if worker.sts != nil {
		replicaCount = *worker.sts.Spec.Replicas
	} else {
		replicaCount = 1
	}

	// For now Standalone is the only CR unique with multiple replicas that is applicable for the AFW
	// If the replica count is more than 1, and if it is Standalone, when transitioning from
	// download phase, create a separate worker for the Pod copy(which also transition to install worker)

	// Also, for whatever reason(say, standalone reset, and that way it lost the app package), if the Standalone
	// switches to download phase, once the download phase is complete, it will safely schedule a new pod copy worker,
	// without affecting other pods.
	appDeployInfo := worker.appDeployInfo
	if replicaCount == 1 {
		scopedLog.Info("Simple transition")

		setContextForNewPhase(worker, &appDeployInfo.PhaseInfo, nextPhase)
		ppln.addWorkersToPipelinePhase(nextPhase, worker)
	} else if kind == "Standalone" {

		// ToDo: sgontla: Strengthen this logic such that we don't depend the podName
		if worker.targetPodName != "" {
			podID, _ := getOrdinalValFromPodName(worker.targetPodName)
			phaseInfo := &worker.appDeployInfo.AuxPhaseInfo[podID]
			setContextForNewPhase(worker, phaseInfo, nextPhase)

		} else if currentPhase == enterpriseApi.PhaseDownload {
			// On a reconcile entry, processing the Standalone CR right after loading the appDeployContext from the CR status
			var copyWorkers, installWorkers []*PipelineWorker
			scopedLog.Info("Fan-out transition")

			// TBD, @Gaurav, As part of CSPL-1169, plz make sure that we are dealing with the right replica count in case of the scale-up scenario
			// Seems like the download just finished. Allocate Phase info
			if len(appDeployInfo.AuxPhaseInfo) == 0 {
				scopedLog.Info("Just finished the download phase")
				// Create Phase info for all the statefulset Pods.
				appDeployInfo.AuxPhaseInfo = make([]enterpriseApi.PhaseInfo, replicaCount)

				// Create a slice of corresponding worker nodes
				copyWorkers = make([]*PipelineWorker, replicaCount)

				//Create the Aux PhaseInfo for tracking all the Standalone Pods
				for podID := range appDeployInfo.AuxPhaseInfo {
					// Create a new copy worker
					copyWorkers[podID] = &PipelineWorker{}
					*copyWorkers[podID] = *worker
					copyWorkers[podID].targetPodName = getApplicablePodNameForWorker(worker.cr, podID)

					setContextForNewPhase(copyWorkers[podID], &appDeployInfo.AuxPhaseInfo[podID], enterpriseApi.PhasePodCopy)
					scopedLog.Info("Created a new fan-out pod copy worker", "pod name", worker.targetPodName)
				}
			} else {
				scopedLog.Info("Installation was already in progress for replica members")
				for podID := range appDeployInfo.AuxPhaseInfo {
					phaseInfo := &appDeployInfo.AuxPhaseInfo[podID]

					newWorker := &PipelineWorker{}
					*newWorker = *worker
					newWorker.targetPodName = getApplicablePodNameForWorker(worker.cr, podID)

					if phaseInfo.RetryCount < pipelinePhaseMaxRetryCount {
						if phaseInfo.Phase == enterpriseApi.PhaseInstall && phaseInfo.Status != enterpriseApi.AppPkgInstallComplete {
							// If the install is already complete for that app, nothing to be done
							scopedLog.Info("Created an install worker", "pod name", worker.targetPodName)
							setContextForNewPhase(newWorker, phaseInfo, enterpriseApi.PhaseInstall)
							installWorkers = append(installWorkers, newWorker)
						} else if phaseInfo.Phase == enterpriseApi.PhasePodCopy {
							scopedLog.Info("Created a pod copy worker", "pod name", worker.targetPodName)
							setContextForNewPhase(newWorker, phaseInfo, enterpriseApi.PhasePodCopy)
							copyWorkers = append(copyWorkers, newWorker)
						} else {
							scopedLog.Error(nil, "invalid phase info detected", "phase", phaseInfo.Phase, "phase status", phaseInfo.Status)
						}
					}
				}

			}

			ppln.addWorkersToPipelinePhase(enterpriseApi.PhasePodCopy, copyWorkers...)
			ppln.addWorkersToPipelinePhase(enterpriseApi.PhaseInstall, installWorkers...)
		} else {
			scopedLog.Error(nil, "Invalid phase detected")
		}

	}

	// We have already moved the worker(s) to the required queue.
	// Now, safely delete the worker from the current phase queue
	scopedLog.Info("Deleted worker", "phase", currentPhase)
	ppln.deleteWorkerFromPipelinePhase(currentPhase, worker)
}

// checkIfWorkerIsEligibleForRun confirms if the worker is eligible to run
func checkIfWorkerIsEligibleForRun(worker *PipelineWorker, phaseInfo *enterpriseApi.PhaseInfo, phaseStatus enterpriseApi.AppPhaseStatusType) bool {
	if !worker.isActive && phaseInfo.RetryCount < pipelinePhaseMaxRetryCount &&
		phaseInfo.Status != phaseStatus {
		return true
	}

	return false
}

// needToUseAuxPhaseInfo confirms if aux phase info to be used
func needToUseAuxPhaseInfo(worker *PipelineWorker, phaseType enterpriseApi.AppPhaseType) bool {
	if phaseType != enterpriseApi.PhaseDownload && worker.cr.GroupVersionKind().Kind == "Standalone" && worker.sts != nil && *worker.sts.Spec.Replicas > 1 {
		return true
	}
	return false
}

// getPhaseInfoByPhaseType gives the phase info suitable for a given phase
func getPhaseInfoByPhaseType(worker *PipelineWorker, phaseType enterpriseApi.AppPhaseType) *enterpriseApi.PhaseInfo {
	scopedLog := log.WithName("getPhaseInfoFromWorker")

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

// updatePplnWorkerPhaseInfo updates the in-memory PhaseInfo
func updatePplnWorkerPhaseInfo(appDeployInfo *enterpriseApi.AppDeploymentInfo, phaseType enterpriseApi.AppPhaseType, retryCount int32, statusType enterpriseApi.AppPhaseStatusType) {
	scopedLog := log.WithName("updatePplnWorkerPhaseInfo").WithValues("appName", appDeployInfo.AppName)

	scopedLog.Info("changing the status", "old status", appPhaseStatusAsStr(appDeployInfo.PhaseInfo.Status), "new status", appPhaseStatusAsStr(statusType))
	appDeployInfo.PhaseInfo.Phase = phaseType
	appDeployInfo.PhaseInfo.RetryCount = retryCount
	appDeployInfo.PhaseInfo.Status = statusType
}

func (downloadWorker *PipelineWorker) createDownloadDirOnOperator() (string, error) {
	scopedLog := log.WithName("createDownloadDirOnOperator").WithValues("appSrcName", downloadWorker.appSrcName, "appName", downloadWorker.appDeployInfo.AppName)
	scope := getAppSrcScope(downloadWorker.afwConfig, downloadWorker.appSrcName)

	kind := downloadWorker.cr.GetObjectKind().GroupVersionKind().Kind

	// This is how the path to download apps looks like -
	// /opt/splunk/appframework/downloadedApps/<namespace>/<CR_Kind>/<CR_Name>/<scope>/<appSrc_Name>/
	// For e.g., if the we are trying to download app app1.tgz under "admin" app source name, for a Standalone CR with name "stand1"
	// in default namespace, then it will be downloaded at the path -
	// /opt/splunk/appframework/downloadedApps/default/Standalone/stand1/local/admin/app1.tgz_<hash>
	localPath := filepath.Join(splcommon.AppDownloadVolume, "downloadedApps", downloadWorker.cr.GetNamespace(), kind, downloadWorker.cr.GetName(), scope, downloadWorker.appSrcName) + "/"
	// create the sub-directories on the volume for downloading scoped apps
	err := createAppDownloadDir(localPath)
	if err != nil {
		scopedLog.Error(err, "unable to create app download directory on operator")
	}
	return localPath, err
}

// Download API will do the actual work of downloading apps from remote storage
func (pplnWorker *PipelineWorker) Download(pplnPhase *PipelinePhase, s3ClientMgr S3ClientManager, activeWorkers *uint64, localPath string) {

	defer func() {
		// decrement the number of active workers
		pplnPhase.mutex.Lock()
		*activeWorkers--
		pplnPhase.mutex.Unlock()

		pplnWorker.isActive = false

		// decrement the waiter count
		pplnWorker.waiter.Done()
	}()

	splunkCR := pplnWorker.cr
	appSrcName := pplnWorker.appSrcName
	scopedLog := log.WithName("PipelineWorker.Download()").WithValues("name", splunkCR.GetName(), "namespace", splunkCR.GetNamespace(), "App name", pplnWorker.appDeployInfo.AppName, "objectHash", pplnWorker.appDeployInfo.ObjectHash)

	appDeployInfo := pplnWorker.appDeployInfo
	appName := appDeployInfo.AppName

	localFile := getLocalAppFileName(localPath, appName, appDeployInfo.ObjectHash)
	remoteFile, err := getRemoteObjectKey(splunkCR, pplnWorker.afwConfig, appSrcName, appName)
	if err != nil {
		scopedLog.Error(err, "unable to get remote object key", "appName", appName)
		// increment the retry count and mark this app as download pending
		updatePplnWorkerPhaseInfo(appDeployInfo, enterpriseApi.PhaseDownload, appDeployInfo.PhaseInfo.RetryCount+1, enterpriseApi.AppPkgDownloadPending)
		return
	}

	// download the app from remote storage
	err = s3ClientMgr.DownloadApp(remoteFile, localFile, appDeployInfo.ObjectHash)
	if err != nil {
		scopedLog.Error(err, "unable to download app", "appName", appName)
		// increment the retry count and mark this app as download pending
		updatePplnWorkerPhaseInfo(appDeployInfo, enterpriseApi.PhaseDownload, appDeployInfo.PhaseInfo.RetryCount+1, enterpriseApi.AppPkgDownloadPending)
		return
	}

	// download is successfull, update the state and reset the retry count
	updatePplnWorkerPhaseInfo(appDeployInfo, enterpriseApi.PhaseDownload, 0, enterpriseApi.AppPkgDownloadComplete)

	scopedLog.Info("Finished downloading app")
}

// scheduleDownloads schedules the download workers to download app/s
func (ppln *AppInstallPipeline) scheduleDownloads(pplnPhase *PipelinePhase, maxWorkers uint64, scheduleDownloadsWaiter *sync.WaitGroup) {
	var activeWorkers uint64

	scopedLog := log.WithName("scheduleDownloads")

downloadWork:
	for {
		if activeWorkers <= maxWorkers {
			select {
			case _, channelOpen := <-ppln.sigTerm:
				if !channelOpen {
					scopedLog.Info("Received the termination request from the scheduler")
					break downloadWork
				}

			case downloadWorker, ok := <-pplnPhase.msgChannel:
				// if channel is closed, then just break from here as we have nothing to read
				if !ok {
					scopedLog.Info("msgChannel is closed by downloadPhaseManager, hence nothing to read.")
					break downloadWork
				}

				// do not redownload the app if it is already downloaded
				if isAppAlreadyDownloaded(downloadWorker) {
					scopedLog.Info("app is already downloaded on operator pod, hence skipping it.", "appSrcName", downloadWorker.appSrcName, "appName", downloadWorker.appDeployInfo.AppName)
					// update the state to be download complete
					updatePplnWorkerPhaseInfo(downloadWorker.appDeployInfo, enterpriseApi.PhaseDownload, 0, enterpriseApi.AppPkgDownloadComplete)
					continue
				}

				ppln.pplnMutex.Lock()
				// do not proceed if we dont have enough disk space to download this app
				if int64(ppln.availableDiskSpace-downloadWorker.appDeployInfo.Size) <= 0 {
					// setting isActive to false here so that downloadPhaseManager can take care of it.
					downloadWorker.isActive = false
					ppln.pplnMutex.Unlock()
					continue
				}

				// update the available disk space
				ppln.availableDiskSpace = ppln.availableDiskSpace - downloadWorker.appDeployInfo.Size
				ppln.pplnMutex.Unlock()

				// increment the count in worker waitgroup
				downloadWorker.waiter.Add(1)

				// update the download state of app to be DownloadInProgress
				updatePplnWorkerPhaseInfo(downloadWorker.appDeployInfo, enterpriseApi.PhaseDownload, downloadWorker.appDeployInfo.PhaseInfo.RetryCount, enterpriseApi.AppPkgDownloadInProgress)

				appDeployInfo := downloadWorker.appDeployInfo

				// create the sub-directories on the volume for downloading scoped apps
				localPath, err := downloadWorker.createDownloadDirOnOperator()
				if err != nil {
					// increment the retry count and mark this app as download pending
					updatePplnWorkerPhaseInfo(appDeployInfo, enterpriseApi.PhaseDownload, appDeployInfo.PhaseInfo.RetryCount+1, enterpriseApi.AppPkgDownloadPending)
					continue
				}

				// get the S3ClientMgr instance
				s3ClientMgr, _ := getS3ClientMgr(*downloadWorker.client, downloadWorker.cr, downloadWorker.afwConfig, downloadWorker.appSrcName)

				// increment the number of active workers
				activeWorkers++

				// start the actual download
				go downloadWorker.Download(pplnPhase, *s3ClientMgr, &activeWorkers, localPath)

			default:
			}
		}
		time.Sleep(200 * time.Millisecond)
	}

	// wait for all the download threads to finish
	pplnPhase.workerWaiter.Wait()

	// we are done processing download jobs
	scheduleDownloadsWaiter.Done()
}

//downloadPhaseManager creates download phase manager for the install pipeline
func (ppln *AppInstallPipeline) downloadPhaseManager() {
	scopedLog := log.WithName("downloadPhaseManager")
	scopedLog.Info("Starting Download phase manager")
	pplnPhase := ppln.pplnPhases[enterpriseApi.PhaseDownload]

	maxWorkers := ppln.appDeployContext.AppsStatusMaxConcurrentAppDownloads

	scheduleDownloadsWaiter := new(sync.WaitGroup)

	scheduleDownloadsWaiter.Add(1)
	// schedule the download threads to do actual download work
	go ppln.scheduleDownloads(pplnPhase, maxWorkers, scheduleDownloadsWaiter)

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
				phaseInfo := getPhaseInfoByPhaseType(downloadWorker, enterpriseApi.PhaseDownload)
				if checkIfWorkerIsEligibleForRun(downloadWorker, phaseInfo, enterpriseApi.AppPkgDownloadComplete) {
					downloadWorker.waiter = &pplnPhase.workerWaiter
					pplnPhase.msgChannel <- downloadWorker

					scopedLog.Info("Download worker got a run slot", "name", downloadWorker.cr.GetName(), "namespace", downloadWorker.cr.GetNamespace(), "App name", downloadWorker.appDeployInfo.AppName, "digest", downloadWorker.appDeployInfo.ObjectHash)
					downloadWorker.isActive = true
				} else if downloadWorker.appDeployInfo.PhaseInfo.Status == enterpriseApi.AppPkgDownloadComplete {
					ppln.TransitionWorkerPhase(downloadWorker, enterpriseApi.PhaseDownload, enterpriseApi.PhasePodCopy)
				} else if phaseInfo.RetryCount >= pipelinePhaseMaxRetryCount {
					downloadWorker.appDeployInfo.PhaseInfo.Status = enterpriseApi.AppPkgDownloadError
					ppln.deleteWorkerFromPipelinePhase(phaseInfo.Phase, downloadWorker)
				}
			}
		}

		time.Sleep(200 * time.Millisecond)
	}

	close(pplnPhase.msgChannel)
	// First wait for my all download workers to finish
	scopedLog.Info("Wating for the download workers to finish")
	scheduleDownloadsWaiter.Wait()

	scopedLog.Info("All the download workers finished")
	// Signal that the download phase is complete
	ppln.phaseWaiter.Done()
}

// podCopyPhaseManager creates pod copy phase manager for the install pipeline
func (ppln *AppInstallPipeline) podCopyPhaseManager() {
	scopedLog := log.WithName("podCopyPhaseManager")
	scopedLog.Info("Starting Pod copy phase manager")
	pplnPhase := ppln.pplnPhases[enterpriseApi.PhasePodCopy]

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
				phaseInfo := getPhaseInfoByPhaseType(podCopyWorker, enterpriseApi.PhasePodCopy)
				if checkIfWorkerIsEligibleForRun(podCopyWorker, phaseInfo, enterpriseApi.AppPkgPodCopyComplete) {
					podCopyWorker.waiter = &pplnPhase.workerWaiter
					select {
					case pplnPhase.msgChannel <- podCopyWorker:
						scopedLog.Info("Pod copy worker got a run slot", "name", podCopyWorker.cr.GetName(), "namespace", podCopyWorker.cr.GetNamespace(), "pod name", podCopyWorker.targetPodName, "App name", podCopyWorker.appDeployInfo.AppName, "digest", podCopyWorker.appDeployInfo.ObjectHash)
						podCopyWorker.isActive = true
					default:
						podCopyWorker.waiter = nil
					}
				} else if phaseInfo.Status == enterpriseApi.AppPkgPodCopyComplete {
					appSrc, err := getAppSrcSpec(podCopyWorker.afwConfig.AppSources, podCopyWorker.appSrcName)
					if err != nil {
						// Error, should never happen
						scopedLog.Error(err, "Unable to find the App source", "app src name", appSrc)
						continue
					}

					// If cluster scoped apps, don't do any thing, just delete the worker. Yield logic knows when to push the bundle
					if appSrc.Scope != enterpriseApi.ScopeCluster {
						ppln.TransitionWorkerPhase(podCopyWorker, enterpriseApi.PhasePodCopy, enterpriseApi.PhaseInstall)
					} else {
						ppln.deleteWorkerFromPipelinePhase(phaseInfo.Phase, podCopyWorker)
					}
				} else if phaseInfo.RetryCount >= pipelinePhaseMaxRetryCount {
					podCopyWorker.appDeployInfo.PhaseInfo.Status = enterpriseApi.AppPkgPodCopyError
					ppln.deleteWorkerFromPipelinePhase(phaseInfo.Phase, podCopyWorker)
				}
			}
		}

		time.Sleep(200 * time.Millisecond)
	}
	scopedLog.Info("Wating for the pod copy workers to finish")
	// First wait for my all pod copy workers to finish
	pplnPhase.workerWaiter.Wait()

	//Signal that the Pod copy manager is done
	scopedLog.Info("All the pod copy workers finished")
	ppln.phaseWaiter.Done()
}

// installPhaseManager creates install phase manager for the afw installation pipeline
func (ppln *AppInstallPipeline) installPhaseManager() {
	scopedLog := log.WithName("installPhaseManager")
	scopedLog.Info("Starting Install phase manager")
	pplnPhase := ppln.pplnPhases[enterpriseApi.PhaseInstall]

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
				phaseInfo := getPhaseInfoByPhaseType(installWorker, enterpriseApi.PhaseInstall)
				if checkIfWorkerIsEligibleForRun(installWorker, phaseInfo, enterpriseApi.AppPkgInstallComplete) {
					installWorker.waiter = &pplnPhase.workerWaiter
					select {
					case pplnPhase.msgChannel <- installWorker:
						scopedLog.Info("Install worker got a run slot", "name", installWorker.cr.GetName(), "namespace", installWorker.cr.GetNamespace(), "pod name", installWorker.targetPodName, "App name", installWorker.appDeployInfo.AppName, "digest", installWorker.appDeployInfo.ObjectHash)
						installWorker.isActive = true
					default:
						installWorker.waiter = nil
					}
				} else if phaseInfo.Status == enterpriseApi.AppPkgInstallComplete {
					if installWorker.cr.GetObjectKind().GroupVersionKind().Kind == "Standalone" && isAppInstallationCompleteOnStandaloneReplicas(installWorker.appDeployInfo.AuxPhaseInfo) {
						installWorker.appDeployInfo.PhaseInfo.Phase = enterpriseApi.PhaseInstall
						installWorker.appDeployInfo.PhaseInfo.Status = enterpriseApi.AppPkgInstallComplete
						installWorker.appDeployInfo.DeployStatus = enterpriseApi.DeployStatusComplete
					}
					//For now, set the deploy status as complete. Eventually, we can phase it out
					ppln.deleteWorkerFromPipelinePhase(phaseInfo.Phase, installWorker)
				} else if phaseInfo.RetryCount >= pipelinePhaseMaxRetryCount {
					installWorker.appDeployInfo.PhaseInfo.Status = enterpriseApi.AppPkgInstallError
					ppln.deleteWorkerFromPipelinePhase(phaseInfo.Phase, installWorker)
				}

			}
		}

		time.Sleep(200 * time.Millisecond)
	}
	scopedLog.Info("Wating for the install workers to finish")
	// wait for all the install workers to finish
	pplnPhase.workerWaiter.Wait()

	// Signal that the Install phase manager is complete
	scopedLog.Info("All the install workers finished")
	ppln.phaseWaiter.Done()
}

// isPipelineEmpty checks if the pipeline is empty or not
func (ppln *AppInstallPipeline) isPipelineEmpty() bool {
	if ppln.pplnPhases == nil {
		return false
	}

	for _, phase := range ppln.pplnPhases {
		if len(phase.q) > 0 {
			return false
		}
	}
	return true
}

// isAppInstallationCompleteOnStandaloneReplicas confirms if an app package is installed on all the Standalone Pods or not
func isAppInstallationCompleteOnStandaloneReplicas(auxPhaseInfo []enterpriseApi.PhaseInfo) bool {
	for _, phaseInfo := range auxPhaseInfo {
		if phaseInfo.Phase != enterpriseApi.PhaseInstall || phaseInfo.Status != enterpriseApi.AppPkgInstallComplete {
			return false
		}
	}

	return true
}

// checkIfBundlePushNeeded confirms if the bundle push is needed or not
func checkIfBundlePushNeeded(clusterScopedApps []*enterpriseApi.AppDeploymentInfo) bool {
	for _, appDeployInfo := range clusterScopedApps {
		if appDeployInfo.PhaseInfo.Phase != enterpriseApi.PhasePodCopy || appDeployInfo.PhaseInfo.Status != enterpriseApi.AppPkgPodCopyComplete {
			return false
		}
	}

	return true
}

// initPipelinePhase initializes a given pipeline phase
func initPipelinePhase(phase enterpriseApi.AppPhaseType) {
	afwPipeline.pplnPhases[phase] = &PipelinePhase{
		q:          []*PipelineWorker{},
		msgChannel: make(chan *PipelineWorker),
	}
}

// initAppInstallPipeline creates the AFW scheduler pipeline
// TBD: Do we need to make it singleton? For now leave it till we have the clarity on
func initAppInstallPipeline(appDeployContext *enterpriseApi.AppDeploymentContext) *AppInstallPipeline {
	if afwPipeline != nil {
		return afwPipeline
	}

	afwPipeline = &AppInstallPipeline{}
	afwPipeline.pplnPhases = make(map[enterpriseApi.AppPhaseType]*PipelinePhase, 3)
	afwPipeline.sigTerm = make(chan struct{})
	afwPipeline.appDeployContext = appDeployContext

	// Allocate the Download phase
	initPipelinePhase(enterpriseApi.PhaseDownload)

	// Allocate the Pod Copy phase
	initPipelinePhase(enterpriseApi.PhasePodCopy)

	// Allocate the install phase
	initPipelinePhase(enterpriseApi.PhaseInstall)

	return afwPipeline
}

func afwGetReleventStatefulsetByKind(cr splcommon.MetaObject, client splcommon.ControllerClient) *appsv1.StatefulSet {
	scopedLog := log.WithName("getReleventStatefulsetByKind").WithValues("name", cr.GetName(), "namespace", cr.GetNamespace())
	var instanceID InstanceType

	switch cr.GetObjectKind().GroupVersionKind().Kind {
	case "Standalone":
		instanceID = SplunkStandalone
	case "LicenseMaster":
		instanceID = SplunkLicenseMaster
	case "SearchHeadCluster":
		instanceID = SplunkDeployer
	case "ClusterMaster":
		instanceID = SplunkClusterMaster
	case "MonitoringConsole":
		instanceID = SplunkMonitoringConsole
	default:
		return nil
	}

	statefulsetName := GetSplunkStatefulsetName(instanceID, cr.GetName())
	namespacedName := types.NamespacedName{Namespace: cr.GetNamespace(), Name: statefulsetName}
	sts, err := splctrl.GetStatefulSetByName(client, namespacedName)
	if err != nil {
		scopedLog.Error(err, "Unable to get the stateful set")
	}

	return sts
}

// afwSchedulerEntry Starts the scheduler Pipeline with the required phases
func afwSchedulerEntry(client splcommon.ControllerClient, cr splcommon.MetaObject, appDeployContext *enterpriseApi.AppDeploymentContext, appFrameworkConfig *enterpriseApi.AppFrameworkSpec) error {
	scopedLog := log.WithName("afwSchedulerEntry").WithValues("name", cr.GetName(), "namespace", cr.GetNamespace())
	afwPipeline = initAppInstallPipeline(appDeployContext)

	var clusterScopedApps []*enterpriseApi.AppDeploymentInfo

	afwEntryTime := time.Now().Unix()

	// get the current available disk space for downloading apps on operator pod
	availableDiskSpace, err := getAvailableDiskSpace()
	if err != nil {
		return err
	}
	afwPipeline.availableDiskSpace = availableDiskSpace

	// Start the download phase manager
	afwPipeline.phaseWaiter.Add(1)
	go afwPipeline.downloadPhaseManager()

	// Start the pod copy phase manager
	afwPipeline.phaseWaiter.Add(1)
	go afwPipeline.podCopyPhaseManager()

	// Start the install phase manager
	afwPipeline.phaseWaiter.Add(1)
	go afwPipeline.installPhaseManager()

	scopedLog.Info("Creating pipeline workers for pending app packages")
	for appSrcName, appSrcDeployInfo := range appDeployContext.AppsSrcDeployStatus {
		for index := range appSrcDeployInfo.AppDeploymentInfoList {
			var pplnPhase enterpriseApi.AppPhaseType
			var podName string
			deployInfo := appSrcDeployInfo.AppDeploymentInfoList[index]

			appSrc, err := getAppSrcSpec(appFrameworkConfig.AppSources, appSrcName)
			if err != nil {
				// Error, should never happen
				scopedLog.Error(err, "Unable to find App src", "App src name", appSrcName, "App name", deployInfo.AppName)
				continue
			}

			// Track the cluster scoped apps to track the bundle push
			if appSrc.Scope == enterpriseApi.ScopeCluster && deployInfo.DeployStatus != enterpriseApi.DeployStatusComplete {
				clusterScopedApps = append(clusterScopedApps, &deployInfo)
			}

			pplnPhase = ""
			// Push All the Intermediatory work to the Pipeline phases and let the corresponding phase manager take care of them
			if deployInfo.PhaseInfo.RetryCount < pipelinePhaseMaxRetryCount {
				phase := deployInfo.PhaseInfo.Phase
				switch phase {
				case enterpriseApi.PhaseDownload, enterpriseApi.PhasePodCopy:
					pplnPhase = phase

				case enterpriseApi.PhaseInstall:
					if deployInfo.PhaseInfo.Status != enterpriseApi.AppPkgInstallComplete {
						pplnPhase = phase
					}
				}
			}

			// Ignore any other apps that are not in progress
			podName = ""
			if pplnPhase != "" {
				// Don't worry about the standalone replicas at this time(auxPhaseInfo). Just queue it to the download phase, and
				// let the download phase take care of it. Also, make sure not provide the podname at this time, so that the worker
				// transision logic can fan-out new workers
				// ToDo: sgontla: bring in a better alternative to strengthen this piece of code
				if cr.GroupVersionKind().Kind != "Standalone" {
					podName = getApplicablePodNameForWorker(cr, 0)
				}
				sts := afwGetReleventStatefulsetByKind(cr, client)
				afwPipeline.createAndAddPipelineWorker(pplnPhase, &deployInfo, appSrcName, podName, appFrameworkConfig, client, cr, sts)
			}
		}
	}

	var clusterAppsList string
	for _, appDeployInfo := range clusterScopedApps {
		clusterAppsList = fmt.Sprintf("%s:%s, %s", appDeployInfo.AppName, appDeployInfo.ObjectHash, clusterAppsList)
	}
	scopedLog.Info("List of cluster scoped apps(appName:digest) for this reconcile entry", "apps", clusterAppsList)

	// Wait for the yield function to finish
	afwPipeline.phaseWaiter.Add(1)
	go func(afwEntryTime int64) {
		for {
			if afwEntryTime+maxRunTimeBeforeAttemptingYield < time.Now().Unix() || afwPipeline.isPipelineEmpty() {
				scopedLog.Info("Yielding from AFW scheduler", "time elapsed", time.Now().Unix()-afwEntryTime)

				// Trigger termination by closing the channel
				close(afwPipeline.sigTerm)
				afwPipeline.phaseWaiter.Done()
				break
			} else {
				if len(clusterScopedApps) > 0 && appDeployContext.BundlePushStatus.BudlePushStage < enterpriseApi.BundlePushPending {
					if checkIfBundlePushNeeded(clusterScopedApps) {
						// Trigger the bundle push playbook: CSPL-1332, CSPL-1333
					}
				}
				// sleep for one second
				time.Sleep(1 * time.Second)
			}
		}
	}(afwEntryTime)

	// ToDo: sgontla: for now, just make the UT happy, until we get the glue logic
	// check if this needs to be pure singleton for the entire reconcile span, considering CSPL-1169. CC: @Gaurav
	// Finally delete the pipeline
	defer func() {
		afwPipeline = nil
	}()
	scopedLog.Info("Waiting for the phase managers to finish")

	// Wait for all the pipeline managers to finish
	afwPipeline.phaseWaiter.Wait()
	scopedLog.Info("All the phase managers finished")

	return nil
}
