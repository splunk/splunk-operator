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
	"reflect"
	"testing"
	"time"

	enterpriseApi "github.com/splunk/splunk-operator/pkg/apis/enterprise/v2"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	splctrl "github.com/splunk/splunk-operator/pkg/splunk/controller"
	spltest "github.com/splunk/splunk-operator/pkg/splunk/test"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestCreateAndAddPipelineWorker(t *testing.T) {
	appDeployInfo := &enterpriseApi.AppDeploymentInfo{
		AppName:          "testapp.spl",
		LastModifiedTime: "testtime",
		ObjectHash:       "abc0123",
		Size:             1234,
		RepoState:        enterpriseApi.RepoStateActive,
		DeployStatus:     enterpriseApi.DeployStatusPending,
		PhaseInfo: enterpriseApi.PhaseInfo{
			Phase:  enterpriseApi.PhaseDownload,
			Status: enterpriseApi.AppPkgDownloadPending,
		},
	}

	podName := "splunk-s2apps-standalone-0"
	cr := enterpriseApi.ClusterMaster{
		TypeMeta: metav1.TypeMeta{
			Kind: "ClusterMaster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
		Spec: enterpriseApi.ClusterMasterSpec{
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				Mock: true,
			},
		},
	}

	appFrameworkConfig := &enterpriseApi.AppFrameworkSpec{
		VolList: []enterpriseApi.VolumeSpec{
			{Name: "msos_s2s3_vol", Endpoint: "https://s3-eu-west-2.amazonaws.com", Path: "testbucket-rs-london", SecretRef: "s3-secret", Type: "s3", Provider: "aws"},
		},
		AppSources: []enterpriseApi.AppSourceSpec{
			{Name: "adminApps",
				Location: "adminAppsRepo",
				AppSourceDefaultSpec: enterpriseApi.AppSourceDefaultSpec{
					VolName: "msos_s2s3_vol",
					Scope:   enterpriseApi.ScopeLocal},
			},
			{Name: "securityApps",
				Location: "securityAppsRepo",
				AppSourceDefaultSpec: enterpriseApi.AppSourceDefaultSpec{
					VolName: "msos_s2s3_vol",
					Scope:   enterpriseApi.ScopeLocal},
			},
			{Name: "authenticationApps",
				Location: "authenticationAppsRepo",
				AppSourceDefaultSpec: enterpriseApi.AppSourceDefaultSpec{
					VolName: "msos_s2s3_vol",
					Scope:   enterpriseApi.ScopeLocal},
			},
		},
	}

	var appFrameworkContext enterpriseApi.AppDeploymentContext = enterpriseApi.AppDeploymentContext{}
	appFrameworkContext.AppFrameworkConfig = *appFrameworkConfig

	appSrcName := appFrameworkConfig.AppSources[0].Name
	var client splcommon.ControllerClient
	var statefulSet *appsv1.StatefulSet = &appsv1.StatefulSet{}
	worker := &PipelineWorker{
		appDeployInfo:    appDeployInfo,
		appSrcName:       appSrcName,
		targetPodName:    podName,
		afwConfig:        appFrameworkConfig,
		afwDeployContext: &appFrameworkContext,
		client:           &client,
		cr:               &cr,
		sts:              statefulSet,
	}

	if !reflect.DeepEqual(worker, createPipelineWorker(appDeployInfo, appSrcName, podName, appFrameworkConfig, &appFrameworkContext, &client, &cr, statefulSet)) {
		t.Errorf("Expected and Returned objects are not the same")
	}

	// Test for createAndAddPipelineWorker
	afwPpln := initAppInstallPipeline()
	defer func() {
		afwPipeline = nil
	}()

	afwPpln.createAndAddPipelineWorker(enterpriseApi.PhaseDownload, appDeployInfo, appSrcName, podName, appFrameworkConfig, &appFrameworkContext, client, &cr, statefulSet)
	if len(afwPpln.pplnPhases[enterpriseApi.PhaseDownload].q) != 1 {
		t.Errorf("Unable to add a worker to the pipeline phase")
	}
}

func TestGetApplicablePodNameForWorker(t *testing.T) {
	cr := enterpriseApi.ClusterMaster{
		TypeMeta: metav1.TypeMeta{
			Kind: "ClusterMaster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
		Spec: enterpriseApi.ClusterMasterSpec{
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				Mock: true,
			},
		},
	}

	podID := 0

	expectedPodName := "splunk-stack1-cluster-master-0"
	returnedPodName := getApplicablePodNameForWorker(&cr, podID)
	if expectedPodName != returnedPodName {
		t.Errorf("Unable to fetch correct pod name. Expected %s, returned %s", expectedPodName, returnedPodName)
	}

	cr.TypeMeta.Kind = "Standalone"
	expectedPodName = "splunk-stack1-standalone-0"
	returnedPodName = getApplicablePodNameForWorker(&cr, podID)
	if expectedPodName != returnedPodName {
		t.Errorf("Unable to fetch correct pod name. Expected %s, returned %s", expectedPodName, returnedPodName)
	}

	cr.TypeMeta.Kind = "LicenseMaster"
	expectedPodName = "splunk-stack1-license-master-0"
	returnedPodName = getApplicablePodNameForWorker(&cr, podID)
	if expectedPodName != returnedPodName {
		t.Errorf("Unable to fetch correct pod name. Expected %s, returned %s", expectedPodName, returnedPodName)
	}

	cr.TypeMeta.Kind = "SearchHeadCluster"
	expectedPodName = "splunk-stack1-deployer-0"
	returnedPodName = getApplicablePodNameForWorker(&cr, podID)
	if expectedPodName != returnedPodName {
		t.Errorf("Unable to fetch correct pod name. Expected %s, returned %s", expectedPodName, returnedPodName)
	}

	cr.TypeMeta.Kind = "MonitoringConsole"
	expectedPodName = "splunk-stack1-monitoring-console-0"
	returnedPodName = getApplicablePodNameForWorker(&cr, podID)
	if expectedPodName != returnedPodName {
		t.Errorf("Unable to fetch correct pod name. Expected %s, returned %s", "", getApplicablePodNameForWorker(&cr, 0))
	}
}

func TestInitAppInstallPipeline(t *testing.T) {
	afwPipeline = &AppInstallPipeline{}
	defer func() {
		afwPipeline = nil
	}()

	tmpPtr := afwPipeline

	// Should not modify the pipeline, if it is already exists

	retPtr := initAppInstallPipeline()

	if retPtr != tmpPtr {
		t.Errorf("When the Pipeline is existing, should not overwrite it")
	}

	// if the pipeline doesn't exist, new pipeline should be created
	afwPipeline = nil
	retPtr = initAppInstallPipeline()
	if retPtr == nil {
		t.Errorf("Failed to create a new pipeline")
	}

	// Finally delete the pipeline
	afwPipeline = nil
}

func TestDeleteWorkerFromPipelinePhase(t *testing.T) {
	ppln := initAppInstallPipeline()
	defer func() {
		afwPipeline = nil
	}()

	cr := enterpriseApi.ClusterMaster{
		TypeMeta: metav1.TypeMeta{
			Kind: "ClusterMaster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
		Spec: enterpriseApi.ClusterMasterSpec{
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				Mock: true,
			},
		},
	}

	if len(ppln.pplnPhases[enterpriseApi.PhaseDownload].q)+len(ppln.pplnPhases[enterpriseApi.PhasePodCopy].q)+len(ppln.pplnPhases[enterpriseApi.PhaseInstall].q) > 0 {
		t.Errorf("Initially Pipeline must be empty, but found as non-empty")
	}

	// Create a list of 5 workers
	capacity := 5
	var workerList []*PipelineWorker = make([]*PipelineWorker, capacity)
	for i := range workerList {
		workerList[i] = &PipelineWorker{
			appDeployInfo: &enterpriseApi.AppDeploymentInfo{
				AppName:    fmt.Sprintf("app%d.tgz", i),
				ObjectHash: fmt.Sprintf("123456%d", i),
			},
			cr: &cr,
		}
	}

	ppln.pplnPhases[enterpriseApi.PhaseDownload].q = append(ppln.pplnPhases[enterpriseApi.PhaseDownload].q, workerList...)

	// Make sure that the last element gets deleted
	ppln.deleteWorkerFromPipelinePhase(enterpriseApi.PhaseDownload, workerList[capacity-1])
	if len(ppln.pplnPhases[enterpriseApi.PhaseDownload].q) == capacity {
		t.Errorf("Deleting the last element failed")
	}

	for _, worker := range ppln.pplnPhases[enterpriseApi.PhaseDownload].q {
		if worker == workerList[capacity-1] {
			t.Errorf("Failed to delete the correct element")
		}
	}
	ppln.pplnPhases[enterpriseApi.PhaseDownload].q = append(ppln.pplnPhases[enterpriseApi.PhaseDownload].q, workerList[capacity-1])

	// Make sure the first element gets deleted
	ppln.deleteWorkerFromPipelinePhase(enterpriseApi.PhaseDownload, workerList[0])
	if len(ppln.pplnPhases[enterpriseApi.PhaseDownload].q) == capacity {
		t.Errorf("Deleting the first element failed")
	}

	if ppln.pplnPhases[enterpriseApi.PhaseDownload].q[0] == workerList[0] {
		t.Errorf("Deleting the first element failed")
	}
	ppln.pplnPhases[enterpriseApi.PhaseDownload].q = nil
	ppln.pplnPhases[enterpriseApi.PhaseDownload].q = append(ppln.pplnPhases[enterpriseApi.PhaseDownload].q, workerList...)

	// Make sure deleting an element in the middle works fine
	ppln.deleteWorkerFromPipelinePhase(enterpriseApi.PhaseDownload, workerList[2])
	for _, worker := range ppln.pplnPhases[enterpriseApi.PhaseDownload].q {
		if worker == workerList[2] {
			t.Errorf("Failed to delete the correct element from the list")
		}
	}

	// Call to delete a non-existing worker should return false
	if ppln.deleteWorkerFromPipelinePhase(enterpriseApi.PhaseDownload, &PipelineWorker{}) {
		t.Errorf("Deletion of non-existing worker should return false, but received true")
	}

}

func TestTransitionWorkerPhase(t *testing.T) {
	ppln := initAppInstallPipeline()
	defer func() {
		afwPipeline = nil
	}()

	cr := enterpriseApi.ClusterMaster{
		TypeMeta: metav1.TypeMeta{
			Kind: "ClusterMaster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
		Spec: enterpriseApi.ClusterMasterSpec{
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				Mock: true,
			},
		},
	}

	var replicas int32 = 1
	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-stack1",
			Namespace: "test",
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &replicas,
		},
	}

	capacity := 5
	var workerList []*PipelineWorker = make([]*PipelineWorker, capacity)
	for i := range workerList {
		workerList[i] = &PipelineWorker{
			sts: sts,
			cr:  &cr,
			appDeployInfo: &enterpriseApi.AppDeploymentInfo{
				AppName: fmt.Sprintf("app%d.tgz", i),
				PhaseInfo: enterpriseApi.PhaseInfo{
					Phase:  enterpriseApi.PhaseDownload,
					Status: enterpriseApi.AppPkgDownloadComplete,
				},
			},
		}
	}

	// Make sure that the workers move from the download phase to the pod Copy phase
	ppln.pplnPhases[enterpriseApi.PhaseDownload].q = append(ppln.pplnPhases[enterpriseApi.PhaseDownload].q, workerList...)
	for i := range workerList {
		ppln.TransitionWorkerPhase(workerList[i], enterpriseApi.PhaseDownload, enterpriseApi.PhasePodCopy)
	}

	if len(ppln.pplnPhases[enterpriseApi.PhasePodCopy].q) != len(workerList) {
		t.Errorf("Unable to move the worker phases")
	}

	if len(ppln.pplnPhases[enterpriseApi.PhaseDownload].q) != 0 {
		t.Errorf("Unable to delete the worker from previous phase")
	}

	// Make sure that the workers move from the podCopy phase to the install phase
	for i := range workerList {
		ppln.TransitionWorkerPhase(workerList[i], enterpriseApi.PhasePodCopy, enterpriseApi.PhaseInstall)
	}

	if len(ppln.pplnPhases[enterpriseApi.PhaseInstall].q) != len(workerList) {
		t.Errorf("Unable to move the worker phases")
	}

	if len(ppln.pplnPhases[enterpriseApi.PhasePodCopy].q) != 0 {
		t.Errorf("Unable to delete the worker from previous phase")
	}

	// In the case of Standalone, make sure that there are new workers created for every replicaset member, once after the download phase is completed
	afwPipeline.pplnPhases[enterpriseApi.PhaseDownload].q = nil
	afwPipeline.pplnPhases[enterpriseApi.PhasePodCopy].q = nil
	cr.TypeMeta.Kind = "Standalone"
	replicas = 5
	ppln.pplnPhases[enterpriseApi.PhaseDownload].q = append(ppln.pplnPhases[enterpriseApi.PhaseDownload].q, workerList[0])
	ppln.TransitionWorkerPhase(workerList[0], enterpriseApi.PhaseDownload, enterpriseApi.PhasePodCopy)
	if len(ppln.pplnPhases[enterpriseApi.PhasePodCopy].q) != int(replicas) {
		t.Errorf("Failed to create a separate worker for each replica member of a standalone statefulset")
	}

	if len(ppln.pplnPhases[enterpriseApi.PhaseDownload].q) != 0 {
		t.Errorf("Failed to delete the worker from current phase, after moving it to new phase")
	}

	// Make sure that the existing Aux phase info is honoured in case of the Stanalone replicas
	afwPipeline.pplnPhases[enterpriseApi.PhaseDownload].q = nil
	afwPipeline.pplnPhases[enterpriseApi.PhasePodCopy].q = nil
	afwPipeline.pplnPhases[enterpriseApi.PhaseInstall].q = nil
	ppln.pplnPhases[enterpriseApi.PhaseDownload].q = append(ppln.pplnPhases[enterpriseApi.PhaseDownload].q, workerList[0])
	ppln.pplnPhases[enterpriseApi.PhaseDownload].q[0].appDeployInfo.AuxPhaseInfo = make([]enterpriseApi.PhaseInfo, replicas)
	ppln.pplnPhases[enterpriseApi.PhaseDownload].q[0].appDeployInfo.AuxPhaseInfo[2].Phase = enterpriseApi.PhaseInstall
	ppln.TransitionWorkerPhase(workerList[0], enterpriseApi.PhaseDownload, enterpriseApi.PhasePodCopy)
	if len(ppln.pplnPhases[enterpriseApi.PhasePodCopy].q) != int(replicas-1) {
		t.Errorf("Failed to create the pod copy workers, according to the AuxPhaseInfo")
	}
	if len(ppln.pplnPhases[enterpriseApi.PhaseInstall].q) != 1 {
		t.Errorf("Failed to create the install workers, according to the AuxPhaseInfo")
	}
}

func TestPhaseManagers(t *testing.T) {
	ppln := initAppInstallPipeline()
	defer func() {
		afwPipeline = nil
	}()
	var appDeployContext *enterpriseApi.AppDeploymentContext = &enterpriseApi.AppDeploymentContext{}

	cr := enterpriseApi.ClusterMaster{
		TypeMeta: metav1.TypeMeta{
			Kind: "ClusterMaster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
		Spec: enterpriseApi.ClusterMasterSpec{
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				Mock: true,
			},
		},
	}

	var replicas int32 = 1
	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-stack1",
			Namespace: "test",
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &replicas,
		},
	}

	capacity := 1
	var workerList []*PipelineWorker = make([]*PipelineWorker, capacity)
	for i := range workerList {
		workerList[i] = &PipelineWorker{
			sts: sts,
			cr:  &cr,
			appDeployInfo: &enterpriseApi.AppDeploymentInfo{
				AppName: fmt.Sprintf("app%d.tgz", i),
				PhaseInfo: enterpriseApi.PhaseInfo{
					Phase:  enterpriseApi.PhaseDownload,
					Status: enterpriseApi.AppPkgDownloadPending,
				},
			},
		}
	}

	// Make sure that the workers move from the download phase to the pod Copy phase
	ppln.pplnPhases[enterpriseApi.PhaseDownload].q = append(ppln.pplnPhases[enterpriseApi.PhaseDownload].q, workerList...)

	go ppln.downloadPhaseManager(&ppln.phaseWaiter, ppln.sigTerm, appDeployContext)
	ppln.phaseWaiter.Add(1)

	// drain the download phase channel
	var worker *PipelineWorker
	for i := 0; i < int(replicas); i++ {
		worker = <-ppln.pplnPhases[enterpriseApi.PhaseDownload].msgChannel
		//ppln.pplnPhases[enterpriseApi.PhaseDownload].workerWaiter.Done()
	}

	if worker != workerList[0] {
		t.Errorf("Unable to flush a download worker")
	}
	worker.appDeployInfo.PhaseInfo.RetryCount = 4
	// Let the phase hop on empty channel, to get more coverage
	time.Sleep(600 * time.Millisecond)
	ppln.pplnPhases[enterpriseApi.PhaseDownload].q = nil

	// add the worker to the pod copy phase
	worker.appDeployInfo.PhaseInfo.RetryCount = 0
	worker.isActive = false
	worker.appDeployInfo.PhaseInfo.Phase = enterpriseApi.PhasePodCopy
	worker.appDeployInfo.PhaseInfo.Status = enterpriseApi.AppPkgPodCopyPending
	ppln.pplnPhases[enterpriseApi.PhasePodCopy].q = append(ppln.pplnPhases[enterpriseApi.PhasePodCopy].q, workerList...)

	go ppln.podCopyPhaseManager(&ppln.phaseWaiter, ppln.sigTerm, appDeployContext)
	ppln.phaseWaiter.Add(1)

	// drain the pod copy channel
	for i := 0; i < int(replicas); i++ {
		worker = <-ppln.pplnPhases[enterpriseApi.PhasePodCopy].msgChannel
		//ppln.pplnPhases[enterpriseApi.PhasePodCopy].workerWaiter.Done()
	}

	if worker != workerList[0] {
		t.Errorf("Unable to flush a pod copy worker")
	}
	worker.appDeployInfo.PhaseInfo.RetryCount = 4
	// Let the phase hop on empty channel, to get more coverage
	time.Sleep(600 * time.Millisecond)
	ppln.pplnPhases[enterpriseApi.PhasePodCopy].q = nil

	// add the worker to the install phase
	worker.appDeployInfo.PhaseInfo.RetryCount = 0
	worker.isActive = false
	worker.appDeployInfo.PhaseInfo.Phase = enterpriseApi.PhaseInstall
	worker.appDeployInfo.PhaseInfo.Status = enterpriseApi.AppPkgInstallPending
	ppln.pplnPhases[enterpriseApi.PhaseInstall].q = append(ppln.pplnPhases[enterpriseApi.PhaseInstall].q, workerList...)

	// drain the install channel
	go ppln.installPhaseManager(&ppln.phaseWaiter, ppln.sigTerm, appDeployContext)
	ppln.phaseWaiter.Add(1)

	for i := 0; i < int(replicas); i++ {
		worker = <-ppln.pplnPhases[enterpriseApi.PhaseInstall].msgChannel
		//ppln.pplnPhases[enterpriseApi.PhaseInstall].workerWaiter.Done()
	}

	if worker != workerList[0] {
		t.Errorf("Unable to flush install worker")
	}
	worker.appDeployInfo.PhaseInfo.RetryCount = 4
	// Let the phase hop on empty channel, to get more coverage
	time.Sleep(600 * time.Millisecond)

	// terminate the download manager
	ppln.sigTerm <- true
	// terminate the pod copy manager
	ppln.sigTerm <- true
	// terminate the install manager
	ppln.sigTerm <- true
	close(ppln.sigTerm)
	// wait for all the phases to return
	ppln.phaseWaiter.Wait()
}

func TestIsPipelineEmpty(t *testing.T) {
	ppln := initAppInstallPipeline()
	defer func() {
		afwPipeline = nil
	}()

	if !ppln.isPipelineEmpty() {
		t.Errorf("Expected empty pipeline, but found some workers in pipeline")
	}

	ppln.pplnPhases[enterpriseApi.PhaseDownload].q = append(ppln.pplnPhases[enterpriseApi.PhaseDownload].q, &PipelineWorker{})
	if ppln.isPipelineEmpty() {
		t.Errorf("Expected pipeline to have elements, but the pipeline is empty")
	}
}

func TestIsAppInstallationCompleteOnStandaloneReplicas(t *testing.T) {
	worker := &PipelineWorker{
		appDeployInfo: &enterpriseApi.AppDeploymentInfo{
			AuxPhaseInfo: make([]enterpriseApi.PhaseInfo, 5),
		},
	}

	auxPhaseInfo := worker.appDeployInfo.AuxPhaseInfo
	auxPhaseInfo[3].Phase = enterpriseApi.PhaseDownload
	if isAppInstallationCompleteOnStandaloneReplicas(auxPhaseInfo) {
		t.Errorf("Expected App installation incomplete, but returned as complete")
	}

	for i := range auxPhaseInfo {
		auxPhaseInfo[i].Phase = enterpriseApi.PhaseInstall
		auxPhaseInfo[i].Status = enterpriseApi.AppPkgInstallComplete
	}

	if !isAppInstallationCompleteOnStandaloneReplicas(auxPhaseInfo) {
		t.Errorf("Expected App installation complete, but returned as incomplete")
	}

}

func TestCheckIfBundlePushNeeded(t *testing.T) {
	var clusterScopedApps []*enterpriseApi.AppDeploymentInfo = make([]*enterpriseApi.AppDeploymentInfo, 3)

	for i := range clusterScopedApps {
		clusterScopedApps[i] = &enterpriseApi.AppDeploymentInfo{}
	}

	if checkIfBundlePushNeeded(clusterScopedApps) {
		t.Errorf("Expected bundle push not required, but got bundle push required")
	}

	for i := range clusterScopedApps {
		clusterScopedApps[i].PhaseInfo.Phase = enterpriseApi.PhasePodCopy
		clusterScopedApps[i].PhaseInfo.Status = enterpriseApi.AppPkgPodCopyComplete
	}

	if !checkIfBundlePushNeeded(clusterScopedApps) {
		t.Errorf("Expected bundle push required, but got bundle push not required")
	}

}

func TestAfwGetReleventStatefulsetByKind(t *testing.T) {
	cr := enterpriseApi.ClusterMaster{
		TypeMeta: metav1.TypeMeta{
			Kind: "ClusterMaster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
		Spec: enterpriseApi.ClusterMasterSpec{
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				Mock: true,
			},
		},
	}

	c := spltest.NewMockClient()

	// Test if STS works for cluster master
	current := appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-stack1-cluster-master",
			Namespace: "test",
		},
	}

	_, err := splctrl.ApplyStatefulSet(c, &current)
	if err != nil {
		return
	}
	if afwGetReleventStatefulsetByKind(&cr, c) == nil {
		t.Errorf("Unable to get the sts for CM")
	}

	// Test if STS works for deployer
	cr.TypeMeta.Kind = "SearchHeadCluster"
	current = appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-stack1-deployer",
			Namespace: "test",
		},
	}

	_, _ = splctrl.ApplyStatefulSet(c, &current)
	if afwGetReleventStatefulsetByKind(&cr, c) == nil {
		t.Errorf("Unable to get the sts for SHC deployer")
	}

	// Test if STS works for LM
	cr.TypeMeta.Kind = "LicenseMaster"
	current = appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-stack1-license-master",
			Namespace: "test",
		},
	}

	_, _ = splctrl.ApplyStatefulSet(c, &current)
	if afwGetReleventStatefulsetByKind(&cr, c) == nil {
		t.Errorf("Unable to get the sts for SHC deployer")
	}

	// Test if STS works for Standalone
	cr.TypeMeta.Kind = "Standalone"
	current = appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-stack1-standalone",
			Namespace: "test",
		},
	}

	_, _ = splctrl.ApplyStatefulSet(c, &current)
	if afwGetReleventStatefulsetByKind(&cr, c) == nil {
		t.Errorf("Unable to get the sts for SHC deployer")
	}

}

// TODO: gaurav/subba, commenting this UT for now.
// It needs to be revisited once we have all the glue logic for all pipelines
/*func TestAfwSchedulerEntry(t *testing.T) {
	cr := enterpriseApi.ClusterMaster{
		TypeMeta: metav1.TypeMeta{
			Kind: "ClusterMaster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
		Spec: enterpriseApi.ClusterMasterSpec{
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				Mock: true,
			},
		},
	}

	appFrameworkConfig := &enterpriseApi.AppFrameworkSpec{
		VolList: []enterpriseApi.VolumeSpec{
			{Name: "msos_s2s3_vol", Endpoint: "https://s3-eu-west-2.amazonaws.com", Path: "testbucket-rs-london", SecretRef: "s3-secret", Type: "s3", Provider: "aws"},
		},
		AppSources: []enterpriseApi.AppSourceSpec{
			{Name: "adminApps",
				Location: "adminAppsRepo",
				AppSourceDefaultSpec: enterpriseApi.AppSourceDefaultSpec{
					VolName: "msos_s2s3_vol",
					Scope:   enterpriseApi.ScopeLocal},
			},
		},
	}

	c := spltest.NewMockClient()
	var appDeployContext *enterpriseApi.AppDeploymentContext = &enterpriseApi.AppDeploymentContext{}
	// Create 2 apps for each app source, a total of 6 apps
	appDeployContext.AppsSrcDeployStatus = make(map[string]enterpriseApi.AppSrcDeployInfo, 3)

	appSrcDeploymentInfo := enterpriseApi.AppSrcDeployInfo{}
	appSrcDeploymentInfo.AppDeploymentInfoList = append(appSrcDeploymentInfo.AppDeploymentInfoList, enterpriseApi.AppDeploymentInfo{
		AppName:    fmt.Sprintf("app1.tgz"),
		ObjectHash: fmt.Sprintf("abcd1234abcd"),
		PhaseInfo: enterpriseApi.PhaseInfo{
			Phase:  enterpriseApi.PhaseDownload,
			Status: enterpriseApi.AppPkgDownloadPending,
		},
	})
	appDeployContext.AppsSrcDeployStatus[appFrameworkConfig.AppSources[0].Name] = appSrcDeploymentInfo

	appDeployContext.AppFrameworkConfig = *appFrameworkConfig

	afwPipeline = nil
	afwPipeline = initAppInstallPipeline()

	//var schedulerWaiter sync.WaitGroup
	//defer schedulerWaiter.Wait()
	var remoteObjListingMap map[string]splclient.S3Response

	schedulerWaiter.Add(1)
	go func() {
		for {
			//worker := <-afwPipeline.pplnPhases[enterpriseApi.PhaseDownload].msgChannel
			if len(afwPipeline.pplnPhases[enterpriseApi.PhaseDownload].q) > 0 && afwPipeline.pplnPhases[enterpriseApi.PhaseDownload].q[0].appDeployInfo.PhaseInfo.RetryCount >= 3 {
				afwPipeline.pplnPhases[enterpriseApi.PhaseDownload].q[0].appDeployInfo.PhaseInfo.Status = enterpriseApi.AppPkgDownloadComplete
				afwPipeline.pplnPhases[enterpriseApi.PhaseDownload].q[0].isActive = false
				break
			}
			time.Sleep(2 * time.Second)
		}
		// wait for the glue logic
		//afwPipeline.pplnPhases[enterpriseApi.PhaseDownload].workerWaiter.Done()
		schedulerWaiter.Done()
	}()

	schedulerWaiter.Add(1)
	go func() {
		worker := <-afwPipeline.pplnPhases[enterpriseApi.PhasePodCopy].msgChannel
		worker.appDeployInfo.PhaseInfo.Status = enterpriseApi.AppPkgPodCopyComplete
		worker.isActive = false
		// wait for the glue logic
		//afwPipeline.pplnPhases[enterpriseApi.PhasePodCopy].workerWaiter.Done()
		schedulerWaiter.Done()
	}()

	schedulerWaiter.Add(1)
	go func() {
		worker := <-afwPipeline.pplnPhases[enterpriseApi.PhaseInstall].msgChannel
		worker.appDeployInfo.PhaseInfo.Status = enterpriseApi.AppPkgInstallComplete
		worker.isActive = false
		// wait for the glue logic
		// afwPipeline.pplnPhases[enterpriseApi.PhaseInstall].workerWaiter.Done()
		schedulerWaiter.Done()
	}()
	// to pass the validation stage, add the directory to download apps
	err := os.MkdirAll(splcommon.AppDownloadVolume, 0755)
	defer os.RemoveAll(splcommon.AppDownloadVolume)

	if err != nil {
		t.Errorf("Unable to create download directory for apps :%s", splcommon.AppDownloadVolume)
	}
	afwSchedulerEntry(c, &cr, appDeployContext, appFrameworkConfig, remoteObjListingMap)
}*/
