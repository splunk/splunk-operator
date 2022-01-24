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
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/pkg/errors"
	enterpriseApi "github.com/splunk/splunk-operator/pkg/apis/enterprise/v3"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	splctrl "github.com/splunk/splunk-operator/pkg/splunk/controller"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/remotecommand"
)

// createPipelineWorker creates a pipeline worker for an app package
func createPipelineWorker(appDeployInfo *enterpriseApi.AppDeploymentInfo, appSrcName string, podName string,
	appFrameworkConfig *enterpriseApi.AppFrameworkSpec, client splcommon.ControllerClient,
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
	worker := createPipelineWorker(appDeployInfo, appSrcName, podName, appFrameworkConfig, client, cr, statefulSet)

	if worker != nil {
		scopedLog.Info("Created new worker", "Pod name", worker.targetPodName, "App name", appDeployInfo.AppName, "digest", appDeployInfo.ObjectHash, "phase", appDeployInfo.PhaseInfo.Phase)
		ppln.addWorkersToPipelinePhase(phase, worker)
	}
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

// transitionWorkerPhase transitions a worker to new phase, and deletes from the current phase
// In the case of Standalone CR with multiple replicas, Fan-out `replicas` number of new workers
func (ppln *AppInstallPipeline) transitionWorkerPhase(worker *PipelineWorker, currentPhase, nextPhase enterpriseApi.AppPhaseType) {
	kind := worker.cr.GetObjectKind().GroupVersionKind().Kind

	scopedLog := log.WithName("transitionWorkerPhase").WithValues("name", worker.cr.GetName(), "namespace", worker.cr.GetNamespace(), "App name", worker.appDeployInfo.AppName, "digest", worker.appDeployInfo.ObjectHash, "pod name", worker.targetPodName, "current Phase", currentPhase, "next phase", nextPhase)

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
					copyWorkers[podID].targetPodName = getApplicablePodNameForAppFramework(worker.cr, podID)

					setContextForNewPhase(copyWorkers[podID], &appDeployInfo.AuxPhaseInfo[podID], enterpriseApi.PhasePodCopy)
					scopedLog.Info("Created a new fan-out pod copy worker", "pod name", worker.targetPodName)
				}
			} else {
				scopedLog.Info("Installation was already in progress for replica members")
				for podID := range appDeployInfo.AuxPhaseInfo {
					phaseInfo := &appDeployInfo.AuxPhaseInfo[podID]

					newWorker := &PipelineWorker{}
					*newWorker = *worker
					newWorker.targetPodName = getApplicablePodNameForAppFramework(worker.cr, podID)

					if phaseInfo.RetryCount < pipelinePhaseMaxRetryCount {
						if phaseInfo.Phase == enterpriseApi.PhaseInstall {
							// If the install is already complete for that app, nothing to be done
							if phaseInfo.Status == enterpriseApi.AppPkgInstallComplete {
								scopedLog.Info("app already installed")
								continue
							} else {
								scopedLog.Info("Created an install worker", "pod name", worker.targetPodName)
								setContextForNewPhase(newWorker, phaseInfo, enterpriseApi.PhaseInstall)
								installWorkers = append(installWorkers, newWorker)
							}
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

// updatePplnWorkerPhaseInfo updates the in-memory PhaseInfo(specifically status and retryCount)
func updatePplnWorkerPhaseInfo(appDeployInfo *enterpriseApi.AppDeploymentInfo, retryCount int32, statusType enterpriseApi.AppPhaseStatusType) {
	scopedLog := log.WithName("updatePplnWorkerPhaseInfo").WithValues("appName", appDeployInfo.AppName)

	scopedLog.Info("changing the status", "old status", appPhaseStatusAsStr(appDeployInfo.PhaseInfo.Status), "new status", appPhaseStatusAsStr(statusType))
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

// download API will do the actual work of downloading apps from remote storage
func (downloadWorker *PipelineWorker) download(pplnPhase *PipelinePhase, s3ClientMgr S3ClientManager, localPath string, downloadWorkersRunPool chan struct{}) {

	defer func() {
		downloadWorker.isActive = false

		<-downloadWorkersRunPool
		// decrement the waiter count
		downloadWorker.waiter.Done()
	}()

	splunkCR := downloadWorker.cr
	appSrcName := downloadWorker.appSrcName
	scopedLog := log.WithName("PipelineWorker.Download()").WithValues("name", splunkCR.GetName(), "namespace", splunkCR.GetNamespace(), "App name", downloadWorker.appDeployInfo.AppName, "objectHash", downloadWorker.appDeployInfo.ObjectHash)

	appDeployInfo := downloadWorker.appDeployInfo
	appName := appDeployInfo.AppName

	localFile := getLocalAppFileName(localPath, appName, appDeployInfo.ObjectHash)
	remoteFile, err := getRemoteObjectKey(splunkCR, downloadWorker.afwConfig, appSrcName, appName)
	if err != nil {
		scopedLog.Error(err, "unable to get remote object key", "appName", appName)
		// increment the retry count and mark this app as download pending
		updatePplnWorkerPhaseInfo(appDeployInfo, appDeployInfo.PhaseInfo.RetryCount+1, enterpriseApi.AppPkgDownloadPending)
		return
	}

	// download the app from remote storage
	err = s3ClientMgr.DownloadApp(remoteFile, localFile, appDeployInfo.ObjectHash)
	if err != nil {
		scopedLog.Error(err, "unable to download app", "appName", appName)

		// remove the local file
		err = os.RemoveAll(localFile)
		if err != nil {
			scopedLog.Error(err, "unable to remove local file from operator")
		}

		// increment the retry count and mark this app as download pending
		updatePplnWorkerPhaseInfo(appDeployInfo, appDeployInfo.PhaseInfo.RetryCount+1, enterpriseApi.AppPkgDownloadPending)
		return
	}

	// download is successfull, update the state and reset the retry count
	updatePplnWorkerPhaseInfo(appDeployInfo, 0, enterpriseApi.AppPkgDownloadComplete)

	scopedLog.Info("Finished downloading app")
}

// scheduleDownloads schedules the download workers to download app/s
func (pplnPhase *PipelinePhase) scheduleDownloads(ppln *AppInstallPipeline, maxWorkers uint64, scheduleDownloadsWaiter *sync.WaitGroup) {

	scopedLog := log.WithName("scheduleDownloads")

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
				if isAppAlreadyDownloaded(downloadWorker) {
					scopedLog.Info("app is already downloaded on operator pod, hence skipping it.", "appSrcName", downloadWorker.appSrcName, "appName", downloadWorker.appDeployInfo.AppName)
					// update the state to be download complete
					updatePplnWorkerPhaseInfo(downloadWorker.appDeployInfo, 0, enterpriseApi.AppPkgDownloadComplete)
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
				updatePplnWorkerPhaseInfo(downloadWorker.appDeployInfo, downloadWorker.appDeployInfo.PhaseInfo.RetryCount, enterpriseApi.AppPkgDownloadInProgress)

				appDeployInfo := downloadWorker.appDeployInfo

				// create the sub-directories on the volume for downloading scoped apps
				localPath, err := downloadWorker.createDownloadDirOnOperator()
				if err != nil {
					// increment the retry count and mark this app as download pending
					updatePplnWorkerPhaseInfo(appDeployInfo, appDeployInfo.PhaseInfo.RetryCount+1, enterpriseApi.AppPkgDownloadPending)

					<-downloadWorkersRunPool
					continue
				}

				// get the S3ClientMgr instance
				s3ClientMgr, _ := getS3ClientMgr(downloadWorker.client, downloadWorker.cr, downloadWorker.afwConfig, downloadWorker.appSrcName)

				// start the actual download
				go downloadWorker.download(pplnPhase, *s3ClientMgr, localPath, downloadWorkersRunPool)

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
func (ppln *AppInstallPipeline) shutdownPipelinePhase(phaseManager string, pplnPhase *PipelinePhase, perPhaseWaiter *sync.WaitGroup) {
	scopedLog := log.WithName(phaseManager)

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
func (ppln *AppInstallPipeline) downloadPhaseManager() {
	scopedLog := log.WithName("downloadPhaseManager")
	scopedLog.Info("Starting Download phase manager")

	pplnPhase := ppln.pplnPhases[enterpriseApi.PhaseDownload]

	maxWorkers := ppln.appDeployContext.AppsStatusMaxConcurrentAppDownloads

	scheduleDownloadsWaiter := new(sync.WaitGroup)

	scheduleDownloadsWaiter.Add(1)
	// schedule the download threads to do actual download work
	go pplnPhase.scheduleDownloads(ppln, maxWorkers, scheduleDownloadsWaiter)
	defer func() {
		ppln.shutdownPipelinePhase(string(enterpriseApi.PhaseDownload), pplnPhase, scheduleDownloadsWaiter)
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
				phaseInfo := getPhaseInfoByPhaseType(downloadWorker, enterpriseApi.PhaseDownload)
				if phaseInfo.RetryCount >= pipelinePhaseMaxRetryCount {
					downloadWorker.appDeployInfo.PhaseInfo.Status = enterpriseApi.AppPkgDownloadError
					ppln.deleteWorkerFromPipelinePhase(phaseInfo.Phase, downloadWorker)
				} else if downloadWorker.appDeployInfo.PhaseInfo.Status == enterpriseApi.AppPkgDownloadComplete {
					ppln.transitionWorkerPhase(downloadWorker, enterpriseApi.PhaseDownload, enterpriseApi.PhasePodCopy)
				} else if checkIfWorkerIsEligibleForRun(downloadWorker, phaseInfo, enterpriseApi.AppPkgDownloadComplete) {
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
func (ctx *localScopePlaybookContext) runPlaybook() error {
	worker := ctx.worker
	cr := worker.cr
	scopedLog := log.WithName("localScopeInstallContext.runPlaybook").WithValues("name", cr.GetName(), "namespace", cr.GetNamespace(), "pod", worker.targetPodName, "app name", worker.appDeployInfo.AppName)

	defer func() {
		<-ctx.sem
		worker.isActive = false
		worker.waiter.Done()
	}()

	// if the app name is app1.tgz and hash is "abcd1234", then appPkgFileName is app1.tgz_abcd1234
	appPkgFileName := getAppPackageName(worker)

	// if appsrc is "appSrc1", then appPkgPathOnPod is /init-apps/appSrc1/app1.tgz_abcd1234
	appPkgPathOnPod := filepath.Join(appBktMnt, worker.appSrcName, appPkgFileName)

	phaseInfo := getPhaseInfoByPhaseType(worker, enterpriseApi.PhaseInstall)

	podExecClient := splutil.GetPodExecClient(worker.client, worker.cr, worker.targetPodName)

	if !checkIfFileExistsOnPod(cr, appPkgPathOnPod, podExecClient) {
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

	streamOptions := &remotecommand.StreamOptions{
		Stdin: strings.NewReader(command),
	}

	stdOut, stdErr, err := ctx.podExecClient.RunPodExecCommand(streamOptions, []string{"/bin/sh"})
	// if the app was already installed previously, then just mark it for install complete
	if stdErr != "" || err != nil {
		phaseInfo.RetryCount++
		scopedLog.Error(err, "local scoped app package install failed", "stdout", stdOut, "stderr", stdErr, "app pkg path", appPkgPathOnPod, "retry count", phaseInfo.RetryCount)
		return fmt.Errorf("local scoped app package install failed. stdOut: %s, stdErr: %s, app pkg path: %s, retry count: %d", stdOut, stdErr, appPkgPathOnPod, phaseInfo.RetryCount)
	}

	// Mark the worker for install complete status
	scopedLog.Info("App pkg installation complete")
	phaseInfo.Status = enterpriseApi.AppPkgInstallComplete
	phaseInfo.RetryCount = 0

	// Delete the app package from the target pod /init-apps/ location
	// ToDo: sgontla: rename the "init-apps" to a different name, as the init-container is going away.
	command = fmt.Sprintf("rm -f %s", appPkgPathOnPod)
	streamOptions.Stdin = strings.NewReader(command)
	stdOut, stdErr, err = ctx.podExecClient.RunPodExecCommand(streamOptions, []string{"/bin/sh"})
	if stdErr != "" || err != nil {
		scopedLog.Error(err, "app pkg deletion failed", "stdout", stdOut, "stderr", stdErr, "app pkg path", appPkgPathOnPod)
		return fmt.Errorf("app pkg deletion failed.  stdOut: %s, stdErr: %s, app pkg path: %s", stdOut, stdErr, appPkgPathOnPod)
	}

	// Try to remove the app package from the Operator Pod
	tryAppPkgCleanupFromOperatorPod(worker)

	return nil
}

// extractClusterScopedAppOnPod untars the given app package to the bundle push location
func extractClusterScopedAppOnPod(worker *PipelineWorker, appSrcScope string, appPkgPathOnPod, appPkgLocalPath string, podExecClient splutil.PodExecClientImpl) error {
	cr := worker.cr
	scopedLog := log.WithName("extractClusterScopedAppOnPod").WithValues("name", cr.GetName(), "namespace", cr.GetNamespace(), "app name", worker.appDeployInfo.AppName)

	var clusterAppsPath string
	kind := worker.cr.GroupVersionKind().Kind
	if kind == "SearchHeadCluster" {
		clusterAppsPath = "/opt/splunk/etc/shcluster/apps/"
	} else if kind == "ClusterMaster" {
		clusterAppsPath = "/opt/splunk/etc/master-apps/"
	} else {
		// Do not return an error
		scopedLog.Error(nil, "app extraction should not be called", "kind", kind)
		return nil
	}

	// untar the package to the cluster apps location, then delete it
	// ToDo: sgontla: cd, tar, and rm commands are trivial commands. packing together to avoid spanning multiple processes.
	// A better alternative is to maintain a script (that can give us the status of each command that we can map into a logical error, and copy if when needed.). Alternatively, we can mount it through a configMap
	command := fmt.Sprintf("cd %s; tar -xzf %s; rm -rf %s", clusterAppsPath, appPkgPathOnPod, appPkgPathOnPod)
	streamOptions := &remotecommand.StreamOptions{
		Stdin: strings.NewReader(command),
	}

	stdOut, stdErr, err := podExecClient.RunPodExecCommand(streamOptions, []string{"/bin/sh"})
	if stdErr != "" || err != nil {
		scopedLog.Error(err, "app package untar & delete failed", "stdout", stdOut, "stderr", stdErr)
		return err
	}

	// Now that the App package was moved to the persistent location on the Pod.
	// Remove the app package from the Operator storage area
	// Note:- local scoped app packages are removed once the installation is complete for entire statefulset
	deleteAppPkgFromOperator(worker)

	return nil
}

// runPodCopyWorker runs one pod copy worker
func runPodCopyWorker(worker *PipelineWorker, ch chan struct{}) {
	cr := worker.cr
	scopedLog := log.WithName("runPodCopyWorker").WithValues("name", cr.GetName(), "namespace", cr.GetNamespace(), "app name", worker.appDeployInfo.AppName, "pod", worker.targetPodName)
	defer func() {
		<-ch
		worker.isActive = false
		worker.waiter.Done()
	}()

	appPkgFileName := worker.appDeployInfo.AppName + "_" + strings.Trim(worker.appDeployInfo.ObjectHash, "\"")

	appSrcScope := getAppSrcScope(worker.afwConfig, worker.appSrcName)
	appPkgLocalDir := getAppPackageLocalDir(cr, appSrcScope, worker.appSrcName)
	appPkgLocalPath := appPkgLocalDir + appPkgFileName

	// ToDo: sgontla: Don't do redundant checks for the directory existence here.
	// Handle it only once, even before getting into the scheduler, once there is clarity for cspl-1296
	appPkgPathOnPod := filepath.Join(appBktMnt, worker.appSrcName, appPkgFileName)

	phaseInfo := getPhaseInfoByPhaseType(worker, enterpriseApi.PhasePodCopy)
	_, err := os.Stat(appPkgLocalPath)
	if err != nil {
		// Move the worker to download phase
		scopedLog.Error(err, "app package is missing", "pod name", worker.targetPodName)
		phaseInfo.Status = enterpriseApi.AppPkgMissingFromOperator
		return
	}

	// get the podExecClient to be used for copying file to pod
	podExecClient := splutil.GetPodExecClient(worker.client, cr, worker.targetPodName)
	stdOut, stdErr, err := CopyFileToPod(worker.client, cr.GetNamespace(), appPkgLocalPath, appPkgPathOnPod, podExecClient)
	if err != nil {
		phaseInfo.RetryCount++
		scopedLog.Error(err, "app package pod copy failed", "stdout", stdOut, "stderr", stdErr, "retry count", phaseInfo.RetryCount)
		return
	}

	if appSrcScope == enterpriseApi.ScopeCluster {
		err = extractClusterScopedAppOnPod(worker, appSrcScope, appPkgPathOnPod, appPkgLocalPath, podExecClient)
		if err != nil {
			phaseInfo.RetryCount++
			scopedLog.Error(err, "extracting the app package on pod failed", "retry count", phaseInfo.RetryCount)
			return
		}
	}

	scopedLog.Info("podCopy complete", "app pkg path", appPkgPathOnPod)
	phaseInfo.Status = enterpriseApi.AppPkgPodCopyComplete
}

// podCopyWorkerHandler fetches and runs the pod copy workers
func (pplnPhase *PipelinePhase) podCopyWorkerHandler(handlerWaiter *sync.WaitGroup, numPodCopyRunners int) {
	scopedLog := log.WithName("podCopyWorkerHandler")
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
					go runPodCopyWorker(worker, podCopyWorkerPool)
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
func (ppln *AppInstallPipeline) podCopyPhaseManager() {
	scopedLog := log.WithName("podCopyPhaseManager")
	scopedLog.Info("Starting Pod copy phase manager")
	var handlerWaiter sync.WaitGroup

	pplnPhase := ppln.pplnPhases[enterpriseApi.PhasePodCopy]

	// Start podCopy worker handler
	// workerWaiter is used to wait for both the podCopyWorkerHandler and all of its children as they are all correlated
	// For now, for the number of parallel pod copy, use the max. concurrent downloads. Standalone is something unique, but at the same time
	// limited by the Operator n/w bw, so hopefullye its Ok.
	handlerWaiter.Add(1)
	go pplnPhase.podCopyWorkerHandler(&handlerWaiter, int(ppln.appDeployContext.AppsStatusMaxConcurrentAppDownloads))
	defer func() {
		ppln.shutdownPipelinePhase(string(enterpriseApi.PhasePodCopy), pplnPhase, &handlerWaiter)
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
				phaseInfo := getPhaseInfoByPhaseType(podCopyWorker, enterpriseApi.PhasePodCopy)
				if phaseInfo.RetryCount >= pipelinePhaseMaxRetryCount {
					podCopyWorker.appDeployInfo.PhaseInfo.Status = enterpriseApi.AppPkgPodCopyError
					ppln.deleteWorkerFromPipelinePhase(phaseInfo.Phase, podCopyWorker)
				} else if phaseInfo.Status == enterpriseApi.AppPkgPodCopyComplete {
					// For cluster scoped apps, just delete the worker. install handler will trigger the bundle push
					if enterpriseApi.ScopeCluster != getAppSrcScope(podCopyWorker.afwConfig, podCopyWorker.appSrcName) {
						ppln.transitionWorkerPhase(podCopyWorker, enterpriseApi.PhasePodCopy, enterpriseApi.PhaseInstall)
					} else {
						ppln.deleteWorkerFromPipelinePhase(phaseInfo.Phase, podCopyWorker)
					}
				} else if phaseInfo.Status == enterpriseApi.AppPkgMissingFromOperator {
					ppln.transitionWorkerPhase(podCopyWorker, enterpriseApi.PhasePodCopy, enterpriseApi.PhaseDownload)
				} else if checkIfWorkerIsEligibleForRun(podCopyWorker, phaseInfo, enterpriseApi.AppPkgPodCopyComplete) {
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
func getInstallSlotForPod(installTracker []chan struct{}, podName string) bool {
	scopedLog := log.WithName("getInstallSlotForPod")
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
func freeInstallSlotForPod(installTracker []chan struct{}, podName string) {
	scopedLog := log.WithName("freeInstallSlotForPod")
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

	// There is no cluster scoped apps pending for bundle push
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
	if afwPipeline.afwEntryTime+maxRunTimeBeforeAttemptingYield < time.Now().Unix() {
		return false
	}

	return true
}

// tryAppPkgCleanupFromOperatorPod tries to change the app install status, also cleans the app pkg from Operator Pod
func tryAppPkgCleanupFromOperatorPod(installWorker *PipelineWorker) {
	scopedLog := log.WithName("tryAppPkgCleanupFromOperatorPod")
	if installWorker.sts == nil {
		scopedLog.Error(nil, "sts is missing", "cr", installWorker.cr.GetName(), "kind", installWorker.cr.GroupVersionKind().Kind, "app pkg", installWorker.appDeployInfo.AppName)
		deleteAppPkgFromOperator(installWorker)
		return
	}

	if *installWorker.sts.Spec.Replicas > 1 {
		if isAppInstallationCompleteOnAllReplicas(installWorker.appDeployInfo.AuxPhaseInfo) {
			scopedLog.Info("app pkg installed on all the pods", "app pkg", installWorker.appDeployInfo.AppName)
			installWorker.appDeployInfo.PhaseInfo.Phase = enterpriseApi.PhaseInstall
			installWorker.appDeployInfo.PhaseInfo.Status = enterpriseApi.AppPkgInstallComplete

			//For now, set the deploy status as complete. Eventually, we can phase it out
			installWorker.appDeployInfo.DeployStatus = enterpriseApi.DeployStatusComplete
			deleteAppPkgFromOperator(installWorker)
		}
	} else {
		deleteAppPkgFromOperator(installWorker)
	}
}

// installWorkerHandler fetches and runs the install workers
// local scope installs are handled first, then the cluster scoped apps are considered for bundle push
func (pplnPhase *PipelinePhase) installWorkerHandler(ppln *AppInstallPipeline, handlerWaiter *sync.WaitGroup, installTracker []chan struct{}) {
	scopedLog := log.WithName("installWorkerHandler")
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

				ctxt := getLocalScopePlaybookContext(installWorker, installTracker[podID], podExecClient)
				if ctxt != nil {
					installWorker.waiter.Add(1)
					go ctxt.runPlaybook()
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
			ctxt := getClusterScopePlaybookContext(ppln.client, ppln.cr, ppln, targetPodName, ppln.cr.GetObjectKind().GroupVersionKind().Kind, podExecClient)
			if ctxt != nil {
				ctxt.runPlaybook()
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
func (ppln *AppInstallPipeline) installPhaseManager() {
	scopedLog := log.WithName("installPhaseManager")
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
	go pplnPhase.installWorkerHandler(ppln, &handlerWaiter, podInstallTracker)
	defer func() {
		ppln.shutdownPipelinePhase(string(enterpriseApi.PhaseInstall), pplnPhase, &handlerWaiter)
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
				appScope := getAppSrcScope(installWorker.afwConfig, installWorker.appSrcName)
				if enterpriseApi.ScopeLocal != appScope {
					scopedLog.Error(nil, "Install worker with non-local scope", "name", installWorker.cr.GetName(), "namespace", installWorker.cr.GetNamespace(), "pod name", installWorker.targetPodName, "App name", installWorker.appDeployInfo.AppName, "digest", installWorker.appDeployInfo.ObjectHash, "scope", appScope)
					continue
				}

				phaseInfo := getPhaseInfoByPhaseType(installWorker, enterpriseApi.PhaseInstall)
				if phaseInfo.RetryCount >= pipelinePhaseMaxRetryCount {
					installWorker.appDeployInfo.PhaseInfo.Status = enterpriseApi.AppPkgInstallError
					ppln.deleteWorkerFromPipelinePhase(phaseInfo.Phase, installWorker)
				} else if phaseInfo.Status == enterpriseApi.AppPkgInstallComplete {
					ppln.deleteWorkerFromPipelinePhase(phaseInfo.Phase, installWorker)
				} else if phaseInfo.Status == enterpriseApi.AppPkgMissingOnPodError {
					ppln.transitionWorkerPhase(installWorker, enterpriseApi.PhaseInstall, enterpriseApi.PhasePodCopy)
				} else if checkIfWorkerIsEligibleForRun(installWorker, phaseInfo, enterpriseApi.AppPkgInstallComplete) &&
					getInstallSlotForPod(podInstallTracker, installWorker.targetPodName) {
					installWorker.waiter = &pplnPhase.workerWaiter
					select {
					case pplnPhase.msgChannel <- installWorker:
						scopedLog.Info("Install worker got a run slot", "name", installWorker.cr.GetName(), "namespace", installWorker.cr.GetNamespace(), "pod name", installWorker.targetPodName, "App name", installWorker.appDeployInfo.AppName, "digest", installWorker.appDeployInfo.ObjectHash)

						// Always set the isActive in Phase manager itself, to avoid any delay in the install handler, otherwise it can
						// cause running the same playbook multiple times.
						installWorker.isActive = true

					default:
						freeInstallSlotForPod(podInstallTracker, installWorker.targetPodName)
						installWorker.waiter = nil
					}
				}
			}
		}

		time.Sleep(200 * time.Millisecond)
	}
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

// initAppInstallPipeline creates the AFW scheduler pipeline
// TBD: Do we need to make it singleton? For now leave it till we have the clarity on
func initAppInstallPipeline(appDeployContext *enterpriseApi.AppDeploymentContext, client splcommon.ControllerClient, cr splcommon.MetaObject) *AppInstallPipeline {

	afwPipeline := &AppInstallPipeline{}
	afwPipeline.pplnPhases = make(map[enterpriseApi.AppPhaseType]*PipelinePhase, 3)
	afwPipeline.sigTerm = make(chan struct{})
	afwPipeline.appDeployContext = appDeployContext
	afwPipeline.afwEntryTime = time.Now().Unix()
	afwPipeline.cr = cr
	afwPipeline.client = client
	afwPipeline.sts = afwGetReleventStatefulsetByKind(cr, client)

	// Allocate the Download phase
	initPipelinePhase(afwPipeline, enterpriseApi.PhaseDownload)

	// Allocate the Pod Copy phase
	initPipelinePhase(afwPipeline, enterpriseApi.PhasePodCopy)

	// Allocate the install phase
	initPipelinePhase(afwPipeline, enterpriseApi.PhaseInstall)

	return afwPipeline
}

// deleteAppPkgFromOperator removes the app pkg from the Operator Pod
func deleteAppPkgFromOperator(worker *PipelineWorker) {
	scopedLog := log.WithName("deleteAppPkgFromOperator").WithValues("name", worker.cr.GetName(), "namespace", worker.cr.GetNamespace(), "app pkg", worker.appDeployInfo.AppName)

	appPkgLocalPath := getAppPackageLocalPath(worker)
	err := os.Remove(appPkgLocalPath)
	if err != nil {
		// Issue is local, so just log an error msg and return
		// ToDo: sgontla: For any transient errors, handle the clean-up at the end of the install
		scopedLog.Error(err, "failed to delete app pkg from Operator", "app pkg path", appPkgLocalPath)
		return
	}

	releaseStorage(worker.appDeployInfo.Size)
}

func afwGetReleventStatefulsetByKind(cr splcommon.MetaObject, client splcommon.ControllerClient) *appsv1.StatefulSet {
	scopedLog := log.WithName("getReleventStatefulsetByKind").WithValues("name", cr.GetName(), "namespace", cr.GetNamespace())
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
	sts, err := splctrl.GetStatefulSetByName(client, namespacedName)
	if err != nil {
		scopedLog.Error(err, "Unable to get the stateful set")
	}

	return sts
}

// getIdxcPlaybookContext returns the idxc playbook context
func getIdxcPlaybookContext(client splcommon.ControllerClient, cr splcommon.MetaObject, afwPipeline *AppInstallPipeline, podName string, podExecClient splutil.PodExecClientImpl) *IdxcPlaybookContext {
	return &IdxcPlaybookContext{
		client:        client,
		cr:            cr,
		afwPipeline:   afwPipeline,
		targetPodName: podName,
		podExecClient: podExecClient,
	}
}

// getSHCPlaybookContext returns the shc playbook context
func getSHCPlaybookContext(client splcommon.ControllerClient, cr splcommon.MetaObject, afwPipeline *AppInstallPipeline, podName string, podExecClient splutil.PodExecClientImpl) *SHCPlaybookContext {
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
func getLocalScopePlaybookContext(installWorker *PipelineWorker, sem chan struct{}, podExecClient splutil.PodExecClientImpl) PlaybookImpl {
	return &localScopePlaybookContext{
		worker:        installWorker,
		sem:           sem,
		podExecClient: podExecClient,
	}
}

// getClusterScopePlaybookContext returns the context for running playbook
func getClusterScopePlaybookContext(client splcommon.ControllerClient, cr splcommon.MetaObject, afwPipeline *AppInstallPipeline, podName string, kind string, podExecClient splutil.PodExecClientImpl) PlaybookImpl {

	switch kind {
	case "ClusterMaster":
		return getIdxcPlaybookContext(client, cr, afwPipeline, podName, podExecClient)
	case "SearchHeadCluster":
		return getSHCPlaybookContext(client, cr, afwPipeline, podName, podExecClient)
	default:
		return nil
	}
}

// removeSHCBundlePushStatusFile removes the SHC Bundle status file from deployer pod
func (shcPlaybookContext *SHCPlaybookContext) removeSHCBundlePushStatusFile() error {
	scopedLog := log.WithName("removeSHCBundlePushStatusFile").WithValues("crName", shcPlaybookContext.cr.GetName(), "namespace", shcPlaybookContext)

	cmd := fmt.Sprintf("rm %s", shcBundlePushStatusCheckFile)
	streamOptions := &remotecommand.StreamOptions{
		Stdin: strings.NewReader(cmd),
	}
	_, stdErr, err := shcPlaybookContext.podExecClient.RunPodExecCommand(streamOptions, []string{"/bin/sh"})
	if err != nil || stdErr != "" {
		scopedLog.Error(err, "unable to remove SHC Bundle Push status file")
		// don't return error from here, so that we can retry cleaning the file in next run
		return err
	}
	return nil
}

// isBundlePushComplete checks whether the SHC bundle push is complete or still pending
func (shcPlaybookContext *SHCPlaybookContext) isBundlePushComplete() (bool, error) {
	scopedLog := log.WithName("isBundlePushComplete").WithValues("crName", shcPlaybookContext.cr.GetName(), "namespace", shcPlaybookContext.cr.GetNamespace())

	cmd := fmt.Sprintf("cat %s", shcBundlePushStatusCheckFile)
	streamOptions := &remotecommand.StreamOptions{
		Stdin: strings.NewReader(cmd),
	}
	// check the content of the status file
	stdOut, stdErr, err := shcPlaybookContext.podExecClient.RunPodExecCommand(streamOptions, []string{"/bin/sh"})
	// check if there is an error returned from running the pod exec command
	if err != nil || stdErr != "" {
		scopedLog.Error(err, "checking the status of SHC Bundle Push failed", "stdout", stdOut, "stderr", stdErr)
		// reset the bundle push state to Pending, so that we retry again.
		setBundlePushState(shcPlaybookContext.afwPipeline, enterpriseApi.BundlePushPending)

		// remove the status file too, so that we dont have any stale status
		removeErr := shcPlaybookContext.removeSHCBundlePushStatusFile()
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
		setBundlePushState(shcPlaybookContext.afwPipeline, enterpriseApi.BundlePushPending)

		// remove the status file too, so that we dont have any stale status
		removeErr := shcPlaybookContext.removeSHCBundlePushStatusFile()
		if removeErr != nil {
			errors.Wrap(err, removeErr.Error())
		}
		return false, err
	}

	// now that bundle push is complete, remove the status file
	err = shcPlaybookContext.removeSHCBundlePushStatusFile()
	if err != nil {
		scopedLog.Error(err, "removing SHC Bundle Push status file failed, will retry again.")
		// don't return error from here, so that we can retry cleaning the file in next run
		return false, nil
	}

	return true, nil
}

// triggerBundlePush triggers the bundle push operation for SHC
func (shcPlaybookContext *SHCPlaybookContext) triggerBundlePush() error {
	cmd := fmt.Sprintf(applySHCBundleCmdStr, shcPlaybookContext.searchHeadCaptainURL, shcBundlePushStatusCheckFile)
	streamOptions := &remotecommand.StreamOptions{
		Stdin: strings.NewReader(cmd),
	}
	stdOut, stdErr, err := shcPlaybookContext.podExecClient.RunPodExecCommand(streamOptions, []string{"/bin/sh"})
	if err != nil || stdErr != "" {
		err = fmt.Errorf("error while applying SHC Bundle. stdout: %s, stderr: %s, err: %v", stdOut, stdErr, err)
		return err
	}
	return nil
}

// runPlaybook will implement the bundle push logic for SHC
func (shcPlaybookContext *SHCPlaybookContext) runPlaybook() error {
	scopedLog := log.WithName("runPlaybook").WithValues("crName", shcPlaybookContext.cr.GetName(), "namespace", shcPlaybookContext.cr.GetNamespace())

	var err error
	var ok bool
	cr := shcPlaybookContext.cr.(*enterpriseApi.SearchHeadCluster)
	if cr.Status.Phase != splcommon.PhaseReady {
		scopedLog.Info("SHC is not ready yet.")
		return nil
	}

	appDeployContext := shcPlaybookContext.afwPipeline.appDeployContext

	switch appDeployContext.BundlePushStatus.BundlePushStage {
	// if the bundle push is already in progress, check the status
	case enterpriseApi.BundlePushInProgress:
		scopedLog.Info("checking the status of SHC Bundle Push")
		// check if the bundle push is complete
		ok, err = shcPlaybookContext.isBundlePushComplete()
		if ok {
			// set the bundle push status to complete
			setBundlePushState(shcPlaybookContext.afwPipeline, enterpriseApi.BundlePushComplete)

			// reset the retry count
			shcPlaybookContext.afwPipeline.appDeployContext.BundlePushStatus.RetryCount = 0

			// set the state to install complete for all the cluster scoped apps
			setInstallStateForClusterScopedApps(appDeployContext)
		} else if err != nil {
			scopedLog.Error(err, "there was an error in SHC bundle push, will retry again.")
		} else {
			scopedLog.Info("SHC Bundle Push is still in progress, will check back again in next reconcile..")
		}
	case enterpriseApi.BundlePushPending:
		// run the command to apply cluster bundle
		scopedLog.Info("running command to apply SHC Bundle")
		err = shcPlaybookContext.triggerBundlePush()
		if err != nil {
			scopedLog.Error(err, "failed to apply SHC Bundle")
		}

		scopedLog.Info("SHC Bundle Push is in progress")

		// set the state to bundle push complete since SHC bundle push is a sync call
		setBundlePushState(shcPlaybookContext.afwPipeline, enterpriseApi.BundlePushInProgress)
	default:
		err = fmt.Errorf("invalid bundle push state=%s", bundlePushStateAsStr(appDeployContext.BundlePushStatus.BundlePushStage))
	}

	return err
}

// isBundlePushComplete checks the status of bundle push
func (idxcPlaybookContext *IdxcPlaybookContext) isBundlePushComplete() bool {
	scopedLog := log.WithName("isBundlePushComplete").WithValues("crName", idxcPlaybookContext.cr.GetName(), "namespace", idxcPlaybookContext.cr.GetNamespace())

	streamOptions := &remotecommand.StreamOptions{
		Stdin: strings.NewReader(idxcShowClusterBundleStatusStr),
	}
	stdOut, stdErr, err := idxcPlaybookContext.podExecClient.RunPodExecCommand(streamOptions, []string{"/bin/sh"})
	if err != nil || stdErr != "" {
		scopedLog.Error(err, "show cluster-bundle-status failed", "stdout", stdOut, "stderr", stdErr)
		return false
	}

	if !strings.Contains(stdOut, "cluster_status=None") {
		scopedLog.Info("IndexerCluster Bundle push is still in progress")
		return false
	}

	// bundle push is complete
	scopedLog.Info("IndexerCluster Bundle push complete")
	return true
}

// triggerBundlePush triggers the bundle push for indexer cluster
func (idxcPlaybookContext *IdxcPlaybookContext) triggerBundlePush() error {
	scopedLog := log.WithName("idxcPlaybookContext.triggerBundlePush()")
	streamOptions := &remotecommand.StreamOptions{
		Stdin: strings.NewReader(applyIdxcBundleCmdStr),
	}
	stdOut, stdErr, err := idxcPlaybookContext.podExecClient.RunPodExecCommand(streamOptions, []string{"/bin/sh"})

	// If the error is due to a bundle which is already present, don't do anything.
	// In the next reconcile we will mark it as bundle push complete
	if strings.Contains(stdErr, idxcBundleAlreadyPresentStr) {
		scopedLog.Info("bundle already present on peers")
	} else if err != nil || stdErr != "OK\n" {
		err = fmt.Errorf("error while applying cluster bundle. stdout: %s, stderr: %s, err: %v", stdOut, stdErr, err)
		return err
	}

	return nil
}

// runPlaybook will implement the following logic(and set the bundle push state accordingly)  -
// 1. If the bundle push is not in progress, run the logic to push the bundle from CM to indexer peers
// 2. OR else, if the bundle push is already in progress, check the status of bundle push
func (idxcPlaybookContext *IdxcPlaybookContext) runPlaybook() error {

	scopedLog := log.WithName("RunPlaybook").WithValues("crName", idxcPlaybookContext.cr.GetName(), "namespace", idxcPlaybookContext.cr.GetNamespace())

	appDeployContext := idxcPlaybookContext.afwPipeline.appDeployContext

	switch appDeployContext.BundlePushStatus.BundlePushStage {
	// if the bundle push is already in progress, check the status
	case enterpriseApi.BundlePushInProgress:
		scopedLog.Info("checking the status of IndexerCluster Bundle Push")
		// check if the bundle push is complete
		if idxcPlaybookContext.isBundlePushComplete() {
			// set the bundle push status to complete
			setBundlePushState(idxcPlaybookContext.afwPipeline, enterpriseApi.BundlePushComplete)

			// reset the retry count
			idxcPlaybookContext.afwPipeline.appDeployContext.BundlePushStatus.RetryCount = 0

			// set the state to install complete for all the cluster scoped apps
			setInstallStateForClusterScopedApps(appDeployContext)
		} else {
			scopedLog.Info("IndexerCluster Bundle Push is still in progress, will check back again in next reconcile..")
		}

	case enterpriseApi.BundlePushPending:
		// run the command to apply cluster bundle
		scopedLog.Info("running command to apply IndexerCluster Bundle")
		err := idxcPlaybookContext.triggerBundlePush()
		if err != nil {
			scopedLog.Error(err, "failed to apply IndexerCluster Bundle")
			return err
		}

		// set the state to bundle push in progress
		setBundlePushState(idxcPlaybookContext.afwPipeline, enterpriseApi.BundlePushInProgress)

	default:
		err := fmt.Errorf("invalid Bundle push state=%s", bundlePushStateAsStr(appDeployContext.BundlePushStatus.BundlePushStage))
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

// afwSchedulerEntry Starts the scheduler Pipeline with the required phases
func afwSchedulerEntry(client splcommon.ControllerClient, cr splcommon.MetaObject, appDeployContext *enterpriseApi.AppDeploymentContext, appFrameworkConfig *enterpriseApi.AppFrameworkSpec) (bool, error) {
	scopedLog := log.WithName("afwSchedulerEntry").WithValues("name", cr.GetName(), "namespace", cr.GetNamespace())

	// return error, if there is no storage defined for the Operator pod
	if !isPersistantVolConfigured() {
		return true, fmt.Errorf("persistant volume required for the App framework, but not provisioned")
	}

	// Operator pod storage is not fully under operator control
	// for now, update on every scheduler entry
	err := updateStorageTracker()
	if err != nil {
		return true, fmt.Errorf("failed to update storage tracker, error: %v", err)
	}

	afwPipeline := initAppInstallPipeline(appDeployContext, client, cr)

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
		deployInfoList := appSrcDeployInfo.AppDeploymentInfoList
		for i := range deployInfoList {
			var pplnPhase enterpriseApi.AppPhaseType
			var podName string

			pplnPhase = ""
			// Push All the Intermediatory work to the Pipeline phases and let the corresponding phase manager take care of them
			if deployInfoList[i].PhaseInfo.RetryCount < pipelinePhaseMaxRetryCount {
				phase := deployInfoList[i].PhaseInfo.Phase

				switch phase {
				case enterpriseApi.PhaseDownload, enterpriseApi.PhasePodCopy:
					pplnPhase = phase

				case enterpriseApi.PhaseInstall:
					if deployInfoList[i].PhaseInfo.Status != enterpriseApi.AppPkgInstallComplete {
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
				sts := afwGetReleventStatefulsetByKind(cr, client)
				if *sts.Spec.Replicas == 1 || cr.GroupVersionKind().Kind != "Standalone" {
					podName = getApplicablePodNameForAppFramework(cr, 0)
				}
				afwPipeline.createAndAddPipelineWorker(pplnPhase, &deployInfoList[i], appSrcName, podName, appFrameworkConfig, client, cr, sts)
			}
		}
	}

	// To avoid any premature termination, start the yield routine only after setting up all the Pipelines. It might be
	// few milliseconds before reaching this far, but that is OK. Otherwise, we may pre-maturely close the phases for any delays
	// while setting up the pipeline phases.
	// Wait for the yield function to finish.
	afwPipeline.phaseWaiter.Add(1)
	go func(afwEntryTime int64) {
		yieldTrigger := time.After(maxRunTimeBeforeAttemptingYield * time.Second)

	yieldScheduler:
		for {
			select {
			case <-yieldTrigger:
				scopedLog.Info("Yielding from AFW scheduler", "time elapsed", time.Now().Unix()-afwEntryTime)
				break yieldScheduler
			default:
				if afwPipeline.isPipelineEmpty() {
					break yieldScheduler
				}
			}

			time.Sleep(100 * time.Millisecond)
		}

		// Trigger the pipeline termination by closing the channel
		close(afwPipeline.sigTerm)
		afwPipeline.phaseWaiter.Done()
	}(afwPipeline.afwEntryTime)

	scopedLog.Info("Waiting for the phase managers to finish")

	// Wait for all the pipeline managers to finish
	afwPipeline.phaseWaiter.Wait()
	scopedLog.Info("All the phase managers finished")

	// Finally mark if all the App framework is complete
	checkAndUpdateAppFrameworkProgressFlag(afwPipeline)

	return needToRevisitAppFramework(afwPipeline), nil
}
