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
	enterpriseApiV3 "github.com/splunk/splunk-operator/api/v3"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
	splclient "github.com/splunk/splunk-operator/pkg/splunk/client"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	splctrl "github.com/splunk/splunk-operator/pkg/splunk/controller"
	spltest "github.com/splunk/splunk-operator/pkg/splunk/test"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestIsFanOutApplicableToCR(t *testing.T) {
	// Fan out is always applicable for Standalone case
	crStdln := &enterpriseApi.Standalone{
		TypeMeta: metav1.TypeMeta{
			Kind: "Standalone",
		},
	}

	if !isFanOutApplicableToCR(crStdln) {
		t.Errorf("For Standalone, fanout is always applicable")
	}

	// Fan out is not applicable for ClusterManager
	crClusterManager := &enterpriseApi.ClusterManager{
		TypeMeta: metav1.TypeMeta{
			Kind: "ClusterManager",
		},
	}

	if isFanOutApplicableToCR(crClusterManager) {
		t.Errorf("For ClusterManager, fanout is not applicable")
	}

	// When the CR kind is not "Standalone", fanout is not applicable
	crUnknownKind := &enterpriseApi.Standalone{}
	if isFanOutApplicableToCR(crUnknownKind) {
		t.Errorf("For the CR with otherthan Standalone, should return false ")
	}

}

func TestCreateAndAddPipelineWorker(t *testing.T) {
	ctx := context.TODO()
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
	cr := enterpriseApi.Standalone{
		TypeMeta: metav1.TypeMeta{
			Kind: "Standalone",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
	}

	statefulSetName := "splunk-stack1-standalone"
	var replicas int32 = 32

	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      statefulSetName,
			Namespace: "test",
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &replicas,
		},
	}

	client := spltest.NewMockClient()
	_, err := splctrl.ApplyStatefulSet(ctx, client, sts)
	if err != nil {
		t.Errorf("unable to apply statefulset")
	}

	appFrameworkConfig := &enterpriseApi.AppFrameworkSpec{
		PhaseMaxRetries: 3,
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
	var statefulSet *appsv1.StatefulSet = &appsv1.StatefulSet{}

	// Test for createAndAddPipelineWorker
	afwPpln := initAppInstallPipeline(ctx, &appFrameworkContext, client, &cr)

	afwPpln.createAndAddPipelineWorker(ctx, enterpriseApi.PhaseDownload, appDeployInfo, appSrcName, podName, appFrameworkConfig, client, &cr, statefulSet)
	if len(afwPpln.pplnPhases[enterpriseApi.PhaseDownload].q) != 1 {
		t.Errorf("Unable to add a worker to the pipeline phase")
	}
}

func TestMakeWorkerInActive(t *testing.T) {
	worker := &PipelineWorker{
		isActive: true,
		waiter:   &sync.WaitGroup{},
	}

	makeWorkerInActive(worker)

	if worker.isActive || worker.waiter != nil {
		t.Errorf("Failed to reest the worker")
	}
}

func TestCreateFanOutWorker(t *testing.T) {
	cr := enterpriseApi.Standalone{
		TypeMeta: metav1.TypeMeta{
			Kind: "Standalone",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
	}

	appFrameworkConfig := &enterpriseApi.AppFrameworkSpec{
		PhaseMaxRetries: 3,
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

	// create statefulset for the cluster master
	statefulSetName := "splunk-stack1-standalone"
	var replicas int32 = 1

	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      statefulSetName,
			Namespace: "test",
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &replicas,
		},
	}

	worker := &PipelineWorker{
		cr: &cr,
		appDeployInfo: &enterpriseApi.AppDeploymentInfo{
			AppName: "app1.tgz",
			PhaseInfo: enterpriseApi.PhaseInfo{
				Phase:     enterpriseApi.PhaseInstall,
				Status:    enterpriseApi.AppPkgInstallComplete,
				FailCount: 0,
			},
			ObjectHash: "abcd1234abcd",
		},
		sts:        sts,
		appSrcName: appFrameworkConfig.AppSources[0].Name,
		afwConfig:  appFrameworkConfig,
		fanOut:     false,
	}

	fanOutWorker := createFanOutWorker(worker, 0)

	if fanOutWorker == nil {
		t.Errorf("Unable to create a fanout worker")
	}

	if fanOutWorker.fanOut {
		t.Errorf("FanOut flag should be false on the new worker")
	}
	if fanOutWorker.targetPodName != "splunk-stack1-standalone-0" {
		t.Errorf("Unexpected pod name: %v", fanOutWorker.targetPodName)
	}

}

func TestGetApplicablePodNameForAppFramework(t *testing.T) {
	cr := enterpriseApi.ClusterManager{
		TypeMeta: metav1.TypeMeta{
			Kind: "ClusterManager",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
		Spec: enterpriseApi.ClusterManagerSpec{
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				Mock: true,
			},
		},
	}

	podID := 0

	expectedPodName := "splunk-stack1-cluster-manager-0"
	returnedPodName := getApplicablePodNameForAppFramework(&cr, podID)
	if expectedPodName != returnedPodName {
		t.Errorf("Unable to fetch correct pod name. Expected %s, returned %s", expectedPodName, returnedPodName)
	}

	cr.TypeMeta.Kind = "Standalone"
	expectedPodName = "splunk-stack1-standalone-0"
	returnedPodName = getApplicablePodNameForAppFramework(&cr, podID)
	if expectedPodName != returnedPodName {
		t.Errorf("Unable to fetch correct pod name. Expected %s, returned %s", expectedPodName, returnedPodName)
	}

	cr.TypeMeta.Kind = "LicenseManager"
	expectedPodName = "splunk-stack1-license-manager-0"
	returnedPodName = getApplicablePodNameForAppFramework(&cr, podID)
	if expectedPodName != returnedPodName {
		t.Errorf("Unable to fetch correct pod name. Expected %s, returned %s", expectedPodName, returnedPodName)
	}

	cr.TypeMeta.Kind = "SearchHeadCluster"
	expectedPodName = "splunk-stack1-deployer-0"
	returnedPodName = getApplicablePodNameForAppFramework(&cr, podID)
	if expectedPodName != returnedPodName {
		t.Errorf("Unable to fetch correct pod name. Expected %s, returned %s", expectedPodName, returnedPodName)
	}

	cr.TypeMeta.Kind = "MonitoringConsole"
	expectedPodName = "splunk-stack1-monitoring-console-0"
	returnedPodName = getApplicablePodNameForAppFramework(&cr, podID)
	if expectedPodName != returnedPodName {
		t.Errorf("Unable to fetch correct pod name. Expected %s, returned %s", "", getApplicablePodNameForAppFramework(&cr, 0))
	}
}

func TestInitAppInstallPipeline(t *testing.T) {
	ctx := context.TODO()
	cr := enterpriseApi.ClusterManager{}
	c := spltest.NewMockClient()
	appDeployContext := &enterpriseApi.AppDeploymentContext{}
	ppln := initAppInstallPipeline(ctx, appDeployContext, c, &cr)

	if ppln == nil {
		t.Errorf("Failed to create a new pipeline")
	}
}

func TestDeleteWorkerFromPipelinePhase(t *testing.T) {
	ctx := context.TODO()
	c := spltest.NewMockClient()
	cr := enterpriseApi.ClusterManager{
		TypeMeta: metav1.TypeMeta{
			Kind: "ClusterManager",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
		Spec: enterpriseApi.ClusterManagerSpec{
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				Mock: true,
			},
		},
	}
	appDeployContext := &enterpriseApi.AppDeploymentContext{}
	ppln := initAppInstallPipeline(ctx, appDeployContext, c, &cr)

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
	ppln.deleteWorkerFromPipelinePhase(ctx, enterpriseApi.PhaseDownload, workerList[capacity-1])
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
	ppln.deleteWorkerFromPipelinePhase(ctx, enterpriseApi.PhaseDownload, workerList[0])
	if len(ppln.pplnPhases[enterpriseApi.PhaseDownload].q) == capacity {
		t.Errorf("Deleting the first element failed")
	}

	if ppln.pplnPhases[enterpriseApi.PhaseDownload].q[0] == workerList[0] {
		t.Errorf("Deleting the first element failed")
	}
	ppln.pplnPhases[enterpriseApi.PhaseDownload].q = nil
	ppln.pplnPhases[enterpriseApi.PhaseDownload].q = append(ppln.pplnPhases[enterpriseApi.PhaseDownload].q, workerList...)

	// Make sure deleting an element in the middle works fine
	ppln.deleteWorkerFromPipelinePhase(ctx, enterpriseApi.PhaseDownload, workerList[2])
	for _, worker := range ppln.pplnPhases[enterpriseApi.PhaseDownload].q {
		if worker == workerList[2] {
			t.Errorf("Failed to delete the correct element from the list")
		}
	}

	// Call to delete a non-existing worker should return false
	if ppln.deleteWorkerFromPipelinePhase(ctx, enterpriseApi.PhaseDownload, &PipelineWorker{}) {
		t.Errorf("Deletion of non-existing worker should return false, but received true")
	}

}

func TestTransitionWorkerPhase(t *testing.T) {
	ctx := context.TODO()
	cr := enterpriseApi.ClusterManager{
		TypeMeta: metav1.TypeMeta{
			Kind: "ClusterManager",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
		Spec: enterpriseApi.ClusterManagerSpec{
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				Mock: true,
			},
		},
	}
	appDeployContext := &enterpriseApi.AppDeploymentContext{}
	appFrameworkConfig := &enterpriseApi.AppFrameworkSpec{
		PhaseMaxRetries: 3,
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
	ppln := initAppInstallPipeline(ctx, appDeployContext, c, &cr)

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
			afwConfig:  appFrameworkConfig,
			appSrcName: appFrameworkConfig.AppSources[0].Name,
		}
	}

	// Make sure that the workers move from the download phase to the pod Copy phase
	ppln.pplnPhases[enterpriseApi.PhaseDownload].q = append(ppln.pplnPhases[enterpriseApi.PhaseDownload].q, workerList...)
	for i := range workerList {
		ppln.transitionWorkerPhase(ctx, workerList[i], enterpriseApi.PhaseDownload, enterpriseApi.PhasePodCopy)
	}

	if len(ppln.pplnPhases[enterpriseApi.PhasePodCopy].q) != len(workerList) {
		t.Errorf("Unable to move the worker phases")
	}

	if len(ppln.pplnPhases[enterpriseApi.PhaseDownload].q) != 0 {
		t.Errorf("Unable to delete the worker from previous phase")
	}

	// Make sure that the workers move from the podCopy phase to the install phase
	for i := range workerList {
		ppln.transitionWorkerPhase(ctx, workerList[i], enterpriseApi.PhasePodCopy, enterpriseApi.PhaseInstall)
	}

	if len(ppln.pplnPhases[enterpriseApi.PhaseInstall].q) != len(workerList) {
		t.Errorf("Unable to move the worker phases")
	}

	if len(ppln.pplnPhases[enterpriseApi.PhasePodCopy].q) != 0 {
		t.Errorf("Unable to delete the worker from previous phase")
	}

	// In the case of Standalone, make sure that there are new workers created for every replicaset member, once after the download phase is completed
	ppln.pplnPhases[enterpriseApi.PhaseDownload].q = nil
	ppln.pplnPhases[enterpriseApi.PhasePodCopy].q = nil
	cr.TypeMeta.Kind = "Standalone"
	replicas = 5
	workerList[0] = &PipelineWorker{
		sts: sts,
		cr:  &cr,
		appDeployInfo: &enterpriseApi.AppDeploymentInfo{
			AppName: "app1.tgz",
			PhaseInfo: enterpriseApi.PhaseInfo{
				Phase:  enterpriseApi.PhaseDownload,
				Status: enterpriseApi.AppPkgDownloadComplete,
			},
		},
		fanOut:     cr.TypeMeta.Kind == "Standalone",
		afwConfig:  appFrameworkConfig,
		appSrcName: appFrameworkConfig.AppSources[0].Name,
	}

	ppln.pplnPhases[enterpriseApi.PhaseDownload].q = append(ppln.pplnPhases[enterpriseApi.PhaseDownload].q, workerList[0])
	ppln.transitionWorkerPhase(ctx, workerList[0], enterpriseApi.PhaseDownload, enterpriseApi.PhasePodCopy)
	if len(ppln.pplnPhases[enterpriseApi.PhasePodCopy].q) != int(replicas) {
		t.Errorf("Failed to create a separate worker for each replica member of a standalone statefulset")
	}

	if len(ppln.pplnPhases[enterpriseApi.PhaseDownload].q) != 0 {
		t.Errorf("Failed to delete the worker from current phase, after moving it to new phase")
	}

	// Make sure that the existing Aux phase info is honoured in case of the Stanalone replicas
	ppln.pplnPhases[enterpriseApi.PhaseDownload].q = nil
	ppln.pplnPhases[enterpriseApi.PhasePodCopy].q = nil
	ppln.pplnPhases[enterpriseApi.PhaseInstall].q = nil
	ppln.pplnPhases[enterpriseApi.PhaseDownload].q = append(ppln.pplnPhases[enterpriseApi.PhaseDownload].q, workerList[0])
	ppln.pplnPhases[enterpriseApi.PhaseDownload].q[0].appDeployInfo.AuxPhaseInfo = make([]enterpriseApi.PhaseInfo, replicas)
	// Mark one pod for installation pending, all others as pod copy pending
	for i := 0; i < int(replicas); i++ {
		ppln.pplnPhases[enterpriseApi.PhaseDownload].q[0].appDeployInfo.AuxPhaseInfo[i].Phase = enterpriseApi.PhasePodCopy
	}
	ppln.pplnPhases[enterpriseApi.PhaseDownload].q[0].appDeployInfo.AuxPhaseInfo[2].Phase = enterpriseApi.PhaseInstall

	ppln.pplnPhases[enterpriseApi.PhaseDownload].q[0].appDeployInfo.AuxPhaseInfo[3].Phase = enterpriseApi.PhaseInstall
	ppln.pplnPhases[enterpriseApi.PhaseDownload].q[0].appDeployInfo.AuxPhaseInfo[3].Status = enterpriseApi.AppPkgInstallComplete

	ppln.transitionWorkerPhase(ctx, workerList[0], enterpriseApi.PhaseDownload, enterpriseApi.PhasePodCopy)
	if len(ppln.pplnPhases[enterpriseApi.PhasePodCopy].q) != int(replicas)-2 {
		t.Errorf("Failed to create the pod copy workers, according to the AuxPhaseInfo")
	}
	if len(ppln.pplnPhases[enterpriseApi.PhaseInstall].q) != 1 {
		t.Errorf("Failed to create the install workers, according to the AuxPhaseInfo")
	}
}

func TestCheckIfWorkerIsEligibleForRun(t *testing.T) {
	ctx := context.TODO()
	worker := &PipelineWorker{
		isActive: false,
		afwConfig: &enterpriseApi.AppFrameworkSpec{
			PhaseMaxRetries: 3,
		},
	}
	phaseInfo := &enterpriseApi.PhaseInfo{
		FailCount: 0,
		Status:    enterpriseApi.AppPkgDownloadPending,
	}

	// test for an eligible worker
	if !checkIfWorkerIsEligibleForRun(ctx, worker, phaseInfo, enterpriseApi.AppPkgDownloadComplete) {
		t.Errorf("Unable to detect an eligible worker")
	}

	// test for an active worker
	worker.isActive = true
	if checkIfWorkerIsEligibleForRun(ctx, worker, phaseInfo, enterpriseApi.AppPkgDownloadComplete) {
		t.Errorf("Unable to detect an ineligible worker(already active")
	}
	worker.isActive = false

	// test for max retry
	phaseInfo.FailCount = 4
	if checkIfWorkerIsEligibleForRun(ctx, worker, phaseInfo, enterpriseApi.AppPkgDownloadComplete) {
		t.Errorf("Unable to detect an ineligible worker(max retries exceeded)")
	}
	phaseInfo.FailCount = 0

	// test for already completed worker
	phaseInfo.Status = enterpriseApi.AppPkgDownloadComplete
	if checkIfWorkerIsEligibleForRun(ctx, worker, phaseInfo, enterpriseApi.AppPkgDownloadComplete) {
		t.Errorf("Unable to detect an ineligible worker(already completed)")
	}
}

func TestPhaseManagersTermination(t *testing.T) {
	ctx := context.TODO()
	c := spltest.NewMockClient()
	cr := &enterpriseApi.ClusterManager{
		TypeMeta: metav1.TypeMeta{
			Kind: "Standalone",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stand1",
			Namespace: "test",
		},
		Spec: enterpriseApi.ClusterManagerSpec{
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				Mock: true,
			},
		},
	}

	statefulSetName := fmt.Sprintf("splunk-%s-standalone", cr.GetName())
	var replicas int32 = 2

	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      statefulSetName,
			Namespace: "test",
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &replicas,
		},
	}

	_, err := splctrl.ApplyStatefulSet(ctx, c, sts)
	if err != nil {
		t.Errorf("unable to apply statefulset")
	}
	appDeployContext := &enterpriseApi.AppDeploymentContext{}
	ppln := initAppInstallPipeline(ctx, appDeployContext, c, cr)

	ppln.appDeployContext.AppsStatusMaxConcurrentAppDownloads = 1
	ppln.phaseWaiter.Add(1)
	go ppln.downloadPhaseManager(ctx)

	ppln.phaseWaiter.Add(1)
	go ppln.podCopyPhaseManager(ctx)

	ppln.phaseWaiter.Add(1)
	go ppln.installPhaseManager(ctx)

	// Make sure that the pipeline is not blocked and comes out after termination
	// Terminate the scheduler, by closing the channel
	close(ppln.sigTerm)
	// Crossign wait() indicates the termination logic works.
	ppln.phaseWaiter.Wait()
}

func TestPhaseManagersMsgChannels(t *testing.T) {
	ctx := context.TODO()
	appDeployContext := &enterpriseApi.AppDeploymentContext{
		AppsStatusMaxConcurrentAppDownloads: 1,
	}

	// Test for each phase can send the worker to down stream
	cr := enterpriseApi.ClusterManager{
		TypeMeta: metav1.TypeMeta{
			Kind: "Standalone",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
		Spec: enterpriseApi.ClusterManagerSpec{
			AppFrameworkConfig: enterpriseApi.AppFrameworkSpec{
				PhaseMaxRetries:           3,
				AppsRepoPollInterval:      60,
				MaxConcurrentAppDownloads: 5,

				VolList: []enterpriseApi.VolumeSpec{
					{
						Name:      "test_volume",
						Endpoint:  "https://s3-eu-west-2.amazonaws.com",
						Path:      "testbucket-rs-london",
						SecretRef: "s3-secret",
						Provider:  "aws",
					},
				},
				AppSources: []enterpriseApi.AppSourceSpec{
					{
						Name:     "appSrc1",
						Location: "adminAppsRepo",
						AppSourceDefaultSpec: enterpriseApi.AppSourceDefaultSpec{
							VolName: "test_volume",
							Scope:   "local",
						},
					},
				},
			},
		},
	}

	statefulSetName := fmt.Sprintf("splunk-%s-standalone", cr.GetName())
	var replicas int32 = 1
	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      statefulSetName,
			Namespace: "test",
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &replicas,
		},
	}

	// Create client and add object
	client := spltest.NewMockClient()
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-stack1-standalone-0",
			Namespace: "test",
			Labels: map[string]string{
				"controller-revision-hash": "v0",
			},
		},
	}

	// Add object
	client.AddObject(pod)

	// Create the statefulset
	_, err := splctrl.ApplyStatefulSet(ctx, client, sts)
	if err != nil {
		t.Errorf("unable to apply statefulset")
	}

	// Just make the lint conversion checks happy
	//var client splcommon.ControllerClient = getConvertedClient(c)

	capacity := 1
	var workerList []*PipelineWorker = make([]*PipelineWorker, capacity)
	for i := range workerList {
		workerList[i] = &PipelineWorker{
			appSrcName:    cr.Spec.AppFrameworkConfig.AppSources[0].Name,
			targetPodName: "splunk-stack1-standalone-0",
			sts:           sts,
			cr:            &cr,
			appDeployInfo: &enterpriseApi.AppDeploymentInfo{
				AppName: fmt.Sprintf("app%d.tgz", i),
				PhaseInfo: enterpriseApi.PhaseInfo{
					Phase:     enterpriseApi.PhaseDownload,
					Status:    enterpriseApi.AppPkgDownloadPending,
					FailCount: 2,
				},
			},
			afwConfig: &cr.Spec.AppFrameworkConfig,
			client:    client,
			fanOut:    cr.GetObjectKind().GroupVersionKind().Kind == "Standalone",
		}
	}

	// test  all the pipeline phases are able to send the worker to the downstreams
	ppln := initAppInstallPipeline(ctx, appDeployContext, client, &cr)
	// Make sure that the workers move from the download phase to the pod Copy phase
	ppln.pplnPhases[enterpriseApi.PhaseDownload].q = append(ppln.pplnPhases[enterpriseApi.PhaseDownload].q, workerList...)

	// Start the download phase manager
	ppln.phaseWaiter.Add(1)
	go ppln.downloadPhaseManager(ctx)
	// drain the download phase channel
	var worker *PipelineWorker
	var i int
	for i = 0; i < int(replicas); i++ {
		worker = <-ppln.pplnPhases[enterpriseApi.PhaseDownload].msgChannel
	}

	if worker != workerList[i-1] {
		t.Errorf("Unable to flush a download worker")
	}
	worker.appDeployInfo.PhaseInfo.FailCount = 4
	// Let the phase hop on empty channel, to get more coverage
	time.Sleep(600 * time.Millisecond)
	ppln.pplnPhases[enterpriseApi.PhaseDownload].q = nil

	// add the worker to the pod copy phase
	worker.appDeployInfo.PhaseInfo.FailCount = 0
	worker.isActive = false
	worker.fanOut = false
	worker.appDeployInfo.AuxPhaseInfo = append(worker.appDeployInfo.AuxPhaseInfo,
		enterpriseApi.PhaseInfo{
			Phase:  enterpriseApi.PhasePodCopy,
			Status: enterpriseApi.AppPkgPodCopyPending,
		})
	ppln.pplnPhases[enterpriseApi.PhasePodCopy].q = append(ppln.pplnPhases[enterpriseApi.PhasePodCopy].q, workerList...)

	go ppln.podCopyPhaseManager(ctx)
	ppln.phaseWaiter.Add(1)

	// drain the pod copy channel
	for i := 0; i < int(replicas); i++ {
		worker = <-ppln.pplnPhases[enterpriseApi.PhasePodCopy].msgChannel
	}

	if worker != workerList[0] {
		t.Errorf("Unable to flush a pod copy worker")
	}
	worker.appDeployInfo.PhaseInfo.FailCount = 4
	// Let the phase hop on empty channel, to get more coverage
	time.Sleep(600 * time.Millisecond)
	ppln.pplnPhases[enterpriseApi.PhasePodCopy].q = nil

	// add the worker to the install phase
	worker.appDeployInfo.PhaseInfo.FailCount = 0
	worker.isActive = false
	worker.fanOut = false
	worker.appDeployInfo.AuxPhaseInfo = append(worker.appDeployInfo.AuxPhaseInfo,
		enterpriseApi.PhaseInfo{
			Phase:  enterpriseApi.PhaseInstall,
			Status: enterpriseApi.AppPkgInstallPending,
		})
	ppln.pplnPhases[enterpriseApi.PhaseInstall].q = append(ppln.pplnPhases[enterpriseApi.PhaseInstall].q, workerList...)

	// Start the install phase manager
	ppln.phaseWaiter.Add(1)
	go ppln.installPhaseManager(ctx)

	worker.appDeployInfo.PhaseInfo.FailCount = 4
	// Let the phase hop on empty channel, to get more coverage
	time.Sleep(600 * time.Millisecond)

	close(ppln.sigTerm)

	// wait for all the phases to return
	ppln.phaseWaiter.Wait()
}

func TestIsPipelineEmpty(t *testing.T) {
	ctx := context.TODO()
	c := spltest.NewMockClient()
	cr := &enterpriseApi.ClusterManager{
		TypeMeta: metav1.TypeMeta{
			Kind: "ClusterManager",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
		Spec: enterpriseApi.ClusterManagerSpec{
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				Mock: true,
			},
		},
	}

	appDeployContext := &enterpriseApi.AppDeploymentContext{}
	ppln := initAppInstallPipeline(ctx, appDeployContext, c, cr)

	if !ppln.isPipelineEmpty() {
		t.Errorf("Expected empty pipeline, but found some workers in pipeline")
	}

	ppln.pplnPhases[enterpriseApi.PhaseDownload].q = append(ppln.pplnPhases[enterpriseApi.PhaseDownload].q, &PipelineWorker{})
	if ppln.isPipelineEmpty() {
		t.Errorf("Expected pipeline to have elements, but the pipeline is empty")
	}
}

func TestGetOrdinalValFromPodName(t *testing.T) {
	var ordinal int = 2

	// Proper pod name should return ordinal value
	var podName = fmt.Sprintf("splunk-s2apps-standalone-%d", ordinal)

	retOrdinal, err := getOrdinalValFromPodName(podName)
	if ordinal != retOrdinal || err != nil {
		t.Errorf("Unable to get the ordinal value from the pod name. error: %v", err)
	}

	// Invalid pod name should return error
	podName = fmt.Sprintf("splunks2apps-standalone-%d", ordinal)

	_, err = getOrdinalValFromPodName(podName)
	if err == nil {
		t.Errorf("Invalid pod name should return error, but didn't receive an error")
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
	if isAppInstallationCompleteOnAllReplicas(auxPhaseInfo) {
		t.Errorf("Expected App installation incomplete, but returned as complete")
	}

	for i := range auxPhaseInfo {
		auxPhaseInfo[i].Phase = enterpriseApi.PhaseInstall
		auxPhaseInfo[i].Status = enterpriseApi.AppPkgInstallComplete
	}

	if !isAppInstallationCompleteOnAllReplicas(auxPhaseInfo) {
		t.Errorf("Expected App installation complete, but returned as incomplete")
	}

}

func TestCheckIfBundlePushIsDone(t *testing.T) {
	if !checkIfBundlePushIsDone("Standalone", enterpriseApi.BundlePushPending) {
		t.Errorf("checkIfBundlePushIsDone should have returned true")
	}

	if checkIfBundlePushIsDone("ClusterManager", enterpriseApi.BundlePushPending) {
		t.Errorf("checkIfBundlePushIsDone should have returned false")
	}
}

func TestNeedToUseAuxPhaseInfo(t *testing.T) {
	cr := &enterpriseApi.ClusterManager{
		TypeMeta: metav1.TypeMeta{
			Kind: "Standalone",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
		Spec: enterpriseApi.ClusterManagerSpec{
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				Mock: true,
			},
		},
	}

	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-stack1-cluster-manager",
			Namespace: "test",
		},
	}
	sts.Spec.Replicas = new(int32)
	*sts.Spec.Replicas = 5
	worker := &PipelineWorker{}
	worker.cr = cr

	// for the download phase, aux. phase info not needed
	if needToUseAuxPhaseInfo(worker, enterpriseApi.PhaseDownload) {
		t.Errorf("Suggesting Aux phase info, when it is not needed")
	}

	// for the phase pod copy, aux. phase info is needed
	worker.sts = sts
	if !needToUseAuxPhaseInfo(worker, enterpriseApi.PhasePodCopy) {
		t.Errorf("Not suggesting Aux phase info, when it is needed")
	}

	*sts.Spec.Replicas = 1
	// Irrespective of the replica member count, aux. phase info should be used for any phase other than download
	if !needToUseAuxPhaseInfo(worker, enterpriseApi.PhasePodCopy) {
		t.Errorf("Suggesting Aux phase info, when it is not needed")
	}
}

func TestSetPhaseStatusToPending(t *testing.T) {
	phaseInfo := &enterpriseApi.PhaseInfo{
		Phase: enterpriseApi.PhaseDownload,
	}

	setPhaseStatusToPending(phaseInfo)
	if phaseInfo.Status != enterpriseApi.AppPkgDownloadPending {
		t.Errorf("Expected status %v, but set to: %v", enterpriseApi.AppPkgDownloadPending, phaseInfo.Status)
	}

	phaseInfo.Phase = enterpriseApi.PhasePodCopy
	setPhaseStatusToPending(phaseInfo)
	if phaseInfo.Status != enterpriseApi.AppPkgPodCopyPending {
		t.Errorf("Expected status %v, but set to: %v", enterpriseApi.AppPkgPodCopyPending, phaseInfo.Status)
	}

	phaseInfo.Phase = enterpriseApi.PhaseInstall
	setPhaseStatusToPending(phaseInfo)
	if phaseInfo.Status != enterpriseApi.AppPkgInstallPending {
		t.Errorf("Expected status %v, but set to: %v", enterpriseApi.AppPkgInstallPending, phaseInfo.Status)
	}
}

func TestIsPhaseStatusComplete(t *testing.T) {
	phaseInfo := &enterpriseApi.PhaseInfo{
		Phase: enterpriseApi.PhaseDownload,
	}

	// incomplete phaseInfo should always return false
	if isPhaseStatusComplete(phaseInfo) {
		t.Errorf("When the status is not set, should return false")
	}

	phaseInfo = &enterpriseApi.PhaseInfo{
		Status: enterpriseApi.AppPkgDownloadPending,
	}
	if isPhaseStatusComplete(phaseInfo) {
		t.Errorf("When the phase is invalid, should return false")
	}

	// If the status is not complete, should return false
	phaseInfo.Phase = enterpriseApi.PhaseDownload
	if isPhaseStatusComplete(phaseInfo) {
		t.Errorf("When the status is not complete, should return false")
	}

	// When the status is complete, should return true
	phaseInfo.Status = enterpriseApi.AppPkgDownloadComplete
	if !isPhaseStatusComplete(phaseInfo) {
		t.Errorf("When the status is complete, should return true")
	}

	phaseInfo.Phase = enterpriseApi.PhasePodCopy
	phaseInfo.Status = enterpriseApi.AppPkgPodCopyComplete
	if !isPhaseStatusComplete(phaseInfo) {
		t.Errorf("When the status is complete, should return true")
	}

	phaseInfo.Phase = enterpriseApi.PhaseInstall
	phaseInfo.Status = enterpriseApi.AppPkgInstallComplete
	if !isPhaseStatusComplete(phaseInfo) {
		t.Errorf("When the status is complete, should return true")
	}
}

func TestIsPhaseMaxRetriesReached(t *testing.T) {
	ctx := context.TODO()
	phaseInfo := &enterpriseApi.PhaseInfo{
		FailCount: 1,
	}

	afwConfig := &enterpriseApi.AppFrameworkSpec{
		PhaseMaxRetries: 3,
	}

	// should return false, when the retries are not maxed out
	if isPhaseMaxRetriesReached(ctx, phaseInfo, afwConfig) {
		t.Errorf("Should return false, when the max retries are not reached")
	}

	// should return true, whe the max. retries are reached
	phaseInfo.FailCount = afwConfig.PhaseMaxRetries + 1
	if !isPhaseMaxRetriesReached(ctx, phaseInfo, afwConfig) {
		t.Errorf("Should return true, when the max retries reached")
	}
}

func TestIsPhaseInfoEligibleForSchedulerEntry(t *testing.T) {
	ctx := context.TODO()
	afwConfig := &enterpriseApi.AppFrameworkSpec{
		PhaseMaxRetries: 3,
		AppSources: []enterpriseApi.AppSourceSpec{
			{
				Name:     "appSrc1",
				Location: "adminAppsRepo",
				AppSourceDefaultSpec: enterpriseApi.AppSourceDefaultSpec{
					VolName: "test_volume",
					Scope:   "local",
				},
			},
			{
				Name:     "appSrc2",
				Location: "securityAppsRepo",
				AppSourceDefaultSpec: enterpriseApi.AppSourceDefaultSpec{
					VolName: "test_volume",
					Scope:   "cluster",
				},
			},
		},
	}

	phaseInfo := &enterpriseApi.PhaseInfo{
		Phase:     enterpriseApi.PhaseDownload,
		Status:    enterpriseApi.AppPkgDownloadComplete,
		FailCount: afwConfig.PhaseMaxRetries + 1,
	}

	// Should not be eligible once the max. retries are reached
	if isPhaseInfoEligibleForSchedulerEntry(ctx, afwConfig.AppSources[0].Name, phaseInfo, afwConfig) {
		t.Errorf("Should not be eligible once the max. retries reached")
	}

	phaseInfo.FailCount = 0
	// For local scope, if the phase and status are not install complete, should return true
	if !isPhaseInfoEligibleForSchedulerEntry(ctx, afwConfig.AppSources[0].Name, phaseInfo, afwConfig) {
		t.Errorf("Local scope: If the install is not complete, should be eligible to run")
	}

	// For local scope, once the app is install complete, should not be eligible to run
	phaseInfo.Phase = enterpriseApi.PhaseInstall
	phaseInfo.Status = enterpriseApi.AppPkgInstallComplete
	if isPhaseInfoEligibleForSchedulerEntry(ctx, afwConfig.AppSources[0].Name, phaseInfo, afwConfig) {
		t.Errorf("Local scope: When the install is complete, should not be eligible to run")
	}

	// For cluster scope, once the app is install complete, should not be eligible to run
	phaseInfo.Phase = enterpriseApi.PhaseInstall
	phaseInfo.Status = enterpriseApi.AppPkgInstallComplete
	if isPhaseInfoEligibleForSchedulerEntry(ctx, afwConfig.AppSources[1].Name, phaseInfo, afwConfig) {
		t.Errorf("Cluster scope: when the install is complete, should not be eligible to run")
	}

	// For cluster scope, once the podcopy is complete, should not be eligible to run
	phaseInfo.Phase = enterpriseApi.PhasePodCopy
	phaseInfo.Status = enterpriseApi.AppPkgPodCopyComplete
	if isPhaseInfoEligibleForSchedulerEntry(ctx, afwConfig.AppSources[1].Name, phaseInfo, afwConfig) {
		t.Errorf("Cluster scope: On podcopy complete, should not be eligible to run")
	}

	// For cluster scope, if the podCopy is not complete, should be able to run
	phaseInfo.Phase = enterpriseApi.PhasePodCopy
	phaseInfo.Status = enterpriseApi.AppPkgPodCopyPending
	if !isPhaseInfoEligibleForSchedulerEntry(ctx, afwConfig.AppSources[1].Name, phaseInfo, afwConfig) {
		t.Errorf("Cluster scope: If the pod copy is not complete, should be eligible to run")
	}
}

func TestGetPhaseInfoByPhaseType(t *testing.T) {
	ctx := context.TODO()
	cr := &enterpriseApi.ClusterManager{
		TypeMeta: metav1.TypeMeta{
			Kind: "Standalone",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
		Spec: enterpriseApi.ClusterManagerSpec{
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				Mock: true,
			},
		},
	}

	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-stack1-cluster-manager",
			Namespace: "test",
		},
	}
	sts.Spec.Replicas = new(int32)
	*sts.Spec.Replicas = 5
	worker := &PipelineWorker{}
	worker.cr = cr
	worker.sts = sts
	worker.appDeployInfo = &enterpriseApi.AppDeploymentInfo{
		AuxPhaseInfo: make([]enterpriseApi.PhaseInfo, *sts.Spec.Replicas),
	}

	worker.targetPodName = "splunk-stack1-standalone-3"

	// For download phase we should not use Aux phase info
	expectedPhaseInfo := &worker.appDeployInfo.PhaseInfo
	retPhaseInfo := getPhaseInfoByPhaseType(ctx, worker, enterpriseApi.PhaseDownload)
	if expectedPhaseInfo != retPhaseInfo {
		t.Errorf("Got the wrong phase info for download phase of Standalone")
	}

	// For non-download phases we should use Aux phase info
	expectedPhaseInfo = &worker.appDeployInfo.AuxPhaseInfo[3]
	retPhaseInfo = getPhaseInfoByPhaseType(ctx, worker, enterpriseApi.PhasePodCopy)
	if expectedPhaseInfo != retPhaseInfo {
		t.Errorf("Got the wrong phase info for Standalone")
	}

	// For CRs that are not standalone, we should not use the Aux phase info
	expectedPhaseInfo = &worker.appDeployInfo.PhaseInfo
	cr.Kind = "ClusterManager"
	retPhaseInfo = getPhaseInfoByPhaseType(ctx, worker, enterpriseApi.PhasePodCopy)
	if expectedPhaseInfo != retPhaseInfo {
		t.Errorf("Got the incorrect phase info for CM")
	}

	worker.targetPodName = "invalid-podName"
	// For CRs that are not standalone, we should not use the Aux phase info
	expectedPhaseInfo = nil
	cr.Kind = "Standalone"
	retPhaseInfo = getPhaseInfoByPhaseType(ctx, worker, enterpriseApi.PhasePodCopy)
	if expectedPhaseInfo != retPhaseInfo {
		t.Errorf("Should get nil for invalid pod name of Standalone")
	}
}
func TestAfwGetReleventStatefulsetByKind(t *testing.T) {
	ctx := context.TODO()
	cr := enterpriseApi.ClusterManager{
		TypeMeta: metav1.TypeMeta{
			Kind: "ClusterManager",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
		Spec: enterpriseApi.ClusterManagerSpec{
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				Mock: true,
			},
		},
	}

	c := spltest.NewMockClient()

	// Test if STS works for cluster master
	current := appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-stack1-cluster-manager",
			Namespace: "test",
		},
	}

	_, err := splctrl.ApplyStatefulSet(ctx, c, &current)
	if err != nil {
		return
	}
	if afwGetReleventStatefulsetByKind(ctx, &cr, c) == nil {
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

	_, _ = splctrl.ApplyStatefulSet(ctx, c, &current)
	if afwGetReleventStatefulsetByKind(ctx, &cr, c) == nil {
		t.Errorf("Unable to get the sts for SHC deployer")
	}

	// Test if STS works for LM
	cr.TypeMeta.Kind = "LicenseManager"
	current = appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-stack1-license-manager",
			Namespace: "test",
		},
	}

	_, _ = splctrl.ApplyStatefulSet(ctx, c, &current)
	if afwGetReleventStatefulsetByKind(ctx, &cr, c) == nil {
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

	_, _ = splctrl.ApplyStatefulSet(ctx, c, &current)
	if afwGetReleventStatefulsetByKind(ctx, &cr, c) == nil {
		t.Errorf("Unable to get the sts for SHC deployer")
	}

}

func createOrTruncateAppFileLocally(appFileName string, size int64) error {
	appFile, err := os.Create(appFileName)
	if err != nil {
		return fmt.Errorf("failed to create app file %s", appFile.Name())
	}

	if err := appFile.Truncate(size); err != nil {
		return fmt.Errorf("failed to truncate app file %s", appFile.Name())
	}

	appFile.Close()

	return nil
}

func areAppsDownloadedSuccessfully(appDeployInfoList []*enterpriseApi.AppDeploymentInfo) (bool, error) {
	for _, appInfo := range appDeployInfoList {
		if appInfo.PhaseInfo.Status != enterpriseApi.AppPkgDownloadComplete {
			err := fmt.Errorf("App:%s is not downloaded yet", appInfo.AppName)
			return false, err
		}
	}
	return true, nil
}

func TestPipelineWorkerDownloadShouldPass(t *testing.T) {
	ctx := context.TODO()
	cr := enterpriseApi.Standalone{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "s1",
			Namespace: "test",
		},
		TypeMeta: metav1.TypeMeta{
			Kind: "Standalone",
		},
		Spec: enterpriseApi.StandaloneSpec{
			Replicas: 1,
			AppFrameworkConfig: enterpriseApi.AppFrameworkSpec{
				PhaseMaxRetries:           3,
				AppsRepoPollInterval:      60,
				MaxConcurrentAppDownloads: 5,
				VolList: []enterpriseApi.VolumeSpec{
					{
						Name:      "test_volume",
						Endpoint:  "https://s3-eu-west-2.amazonaws.com",
						Path:      "testbucket-rs-london",
						SecretRef: "s3-secret",
						Provider:  "aws",
					},
				},
				AppSources: []enterpriseApi.AppSourceSpec{
					{
						Name:     "appSrc1",
						Location: "adminAppsRepo",
						AppSourceDefaultSpec: enterpriseApi.AppSourceDefaultSpec{
							VolName: "test_volume",
							Scope:   "local",
						},
					},
					{
						Name:     "appSrc2",
						Location: "securityAppsRepo",
						AppSourceDefaultSpec: enterpriseApi.AppSourceDefaultSpec{
							VolName: "test_volume",
							Scope:   "local",
						},
					},
					{
						Name:     "appSrc3",
						Location: "authenticationAppsRepo",
						AppSourceDefaultSpec: enterpriseApi.AppSourceDefaultSpec{
							VolName: "test_volume",
							Scope:   "local",
						},
					},
				},
			},
		},
	}
	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-s1-standalone",
			Namespace: "test",
		},
	}
	sts.Spec.Replicas = new(int32)
	*sts.Spec.Replicas = 1

	testApps := []string{"app1.tgz", "app2.tgz", "app3.tgz"}
	testHashes := []string{"abcd1111", "efgh2222", "ijkl3333"}
	testSizes := []int64{10, 20, 30}

	appDeployInfoList := make([]*enterpriseApi.AppDeploymentInfo, 3)
	for index := range testApps {
		appDeployInfoList[index] = &enterpriseApi.AppDeploymentInfo{
			AppName: testApps[index],
			PhaseInfo: enterpriseApi.PhaseInfo{
				Phase:     enterpriseApi.PhaseDownload,
				Status:    enterpriseApi.AppPkgDownloadPending,
				FailCount: 0,
			},
			ObjectHash: testHashes[index],
			Size:       uint64(testSizes[index]),
		}
	}

	client := spltest.NewMockClient()

	// Create S3 secret
	s3Secret := spltest.GetMockS3SecretKeys("s3-secret")

	client.AddObject(&s3Secret)

	// Create namespace scoped secret
	_, err := splutil.ApplyNamespaceScopedSecretObject(ctx, client, "test")
	if err != nil {
		t.Errorf(err.Error())
	}

	splclient.RegisterS3Client(ctx, "aws")

	for index, appSrc := range cr.Spec.AppFrameworkConfig.AppSources {

		localPath := filepath.Join(splcommon.AppDownloadVolume, "downloadedApps", cr.Namespace, cr.Kind, cr.Name, appSrc.Scope, appSrc.Name) + "/"
		// create the app download directory locally
		err := createAppDownloadDir(ctx, localPath)
		if err != nil {
			t.Errorf("Unable to create the download directory")
		}
		defer os.RemoveAll(splcommon.AppDownloadVolume)

		// create the dummy app packages locally
		appFileName := testApps[index] + "_" + testHashes[index]
		appLoc := filepath.Join(localPath, appFileName)
		err = createOrTruncateAppFileLocally(appLoc, testSizes[index])
		if err != nil {
			t.Errorf("Unable to create the app files locally")
		}
		defer os.Remove(appLoc)

		// Update the GetS3Client with our mock call which initializes mock AWS client
		getClientWrapper := splclient.S3Clients["aws"]
		getClientWrapper.SetS3ClientFuncPtr(ctx, "aws", splclient.NewMockAWSS3Client)

		initFunc := getClientWrapper.GetS3ClientInitFuncPtr(ctx)

		s3ClientMgr, err := getS3ClientMgr(ctx, client, &cr, &cr.Spec.AppFrameworkConfig, appSrc.Name)
		if err != nil {
			t.Errorf("unable to get S3ClientMgr instance")
		}

		s3ClientMgr.initFn = initFunc

		pplnPhase := &PipelinePhase{}
		worker := &PipelineWorker{
			appSrcName:    appSrc.Name,
			cr:            &cr,
			sts:           sts,
			afwConfig:     &cr.Spec.AppFrameworkConfig,
			appDeployInfo: appDeployInfoList[index],
			waiter:        new(sync.WaitGroup),
		}
		var downloadWorkersRunPool = make(chan struct{}, 1)
		downloadWorkersRunPool <- struct{}{}
		worker.waiter.Add(1)
		go worker.download(ctx, pplnPhase, *s3ClientMgr, localPath, downloadWorkersRunPool)
		worker.waiter.Wait()
	}

	// verify if all the apps are in DownloadComplete state
	if ok, err := areAppsDownloadedSuccessfully(appDeployInfoList); !ok {
		t.Errorf("All apps should have been downloaded successfully, error=%v", err)
	}
}

func TestPipelineWorkerDownloadShouldFail(t *testing.T) {
	ctx := context.TODO()
	cr := enterpriseApi.Standalone{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "s1",
			Namespace: "test",
		},
		TypeMeta: metav1.TypeMeta{
			Kind: "Standalone",
		},
		Spec: enterpriseApi.StandaloneSpec{
			Replicas: 1,
			AppFrameworkConfig: enterpriseApi.AppFrameworkSpec{
				PhaseMaxRetries:           3,
				AppsRepoPollInterval:      60,
				MaxConcurrentAppDownloads: 5,
				VolList: []enterpriseApi.VolumeSpec{
					{
						Name:      "test_volume",
						Endpoint:  "https://s3-eu-west-2.amazonaws.com",
						Path:      "testbucket-rs-london",
						SecretRef: "s3-secret",
						Provider:  "aws",
					},
				},
				AppSources: []enterpriseApi.AppSourceSpec{
					{
						Name:     "appSrc1",
						Location: "adminAppsRepo",
						AppSourceDefaultSpec: enterpriseApi.AppSourceDefaultSpec{
							VolName: "test_volume",
							Scope:   "local",
						},
					},
				},
			},
		},
	}
	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-s1-standalone",
			Namespace: "test",
		},
	}
	sts.Spec.Replicas = new(int32)
	*sts.Spec.Replicas = 1

	testApps := []string{"app1.tgz"}
	testHashes := []string{"abcd1111"}
	testSizes := []int64{10}

	appDeployInfoList := make([]*enterpriseApi.AppDeploymentInfo, 1)
	for index := range testApps {
		appDeployInfoList[index] = &enterpriseApi.AppDeploymentInfo{
			AppName: testApps[index],
			PhaseInfo: enterpriseApi.PhaseInfo{
				Phase:     enterpriseApi.PhaseDownload,
				Status:    enterpriseApi.AppPkgDownloadPending,
				FailCount: 0,
			},
			ObjectHash: testHashes[index],
			Size:       uint64(testSizes[index]),
		}
	}

	s3ClientMgr := &S3ClientManager{}

	// Test1. Invalid appSrcName
	worker := &PipelineWorker{
		appSrcName:    "invalidAppSrcName",
		cr:            &cr,
		sts:           sts,
		afwConfig:     &cr.Spec.AppFrameworkConfig,
		appDeployInfo: appDeployInfoList[0],
		waiter:        new(sync.WaitGroup),
	}

	pplnPhase := &PipelinePhase{}
	var downloadWorkersRunPool = make(chan struct{}, 1)
	downloadWorkersRunPool <- struct{}{}
	worker.waiter.Add(1)
	go worker.download(ctx, pplnPhase, *s3ClientMgr, "", downloadWorkersRunPool)
	worker.waiter.Wait()

	// we should return error here
	if ok, _ := areAppsDownloadedSuccessfully(appDeployInfoList); ok {
		t.Errorf("We should have returned error here since appSrcName is invalid in the worker")
	}

	worker.appSrcName = "appSrc1"

	// Test2. Empty Object hash
	worker.appDeployInfo.ObjectHash = ""

	client := spltest.NewMockClient()

	// Create S3 secret
	s3Secret := spltest.GetMockS3SecretKeys("s3-secret")

	client.AddObject(&s3Secret)

	// Create namespace scoped secret
	_, err := splutil.ApplyNamespaceScopedSecretObject(ctx, client, "test")
	if err != nil {
		t.Errorf(err.Error())
	}

	splclient.RegisterS3Client(ctx, "aws")
	// Update the GetS3Client with our mock call which initializes mock AWS client
	getClientWrapper := splclient.S3Clients["aws"]
	getClientWrapper.SetS3ClientFuncPtr(ctx, "ctx, aws", splclient.NewMockAWSS3Client)

	initFunc := getClientWrapper.GetS3ClientInitFuncPtr(ctx)

	s3ClientMgr, err = getS3ClientMgr(ctx, client, &cr, &cr.Spec.AppFrameworkConfig, "appSrc1")
	if err != nil {
		t.Errorf("unable to get S3ClientMgr instance")
	}

	s3ClientMgr.initFn = initFunc

	worker.waiter.Add(1)
	downloadWorkersRunPool <- struct{}{}
	go worker.download(ctx, pplnPhase, *s3ClientMgr, "", downloadWorkersRunPool)
	worker.waiter.Wait()
	// we should return error here
	if ok, _ := areAppsDownloadedSuccessfully(appDeployInfoList); ok {
		t.Errorf("We should have returned error here since objectHash is empty in the worker")
	}

}

func TestScheduleDownloads(t *testing.T) {
	ctx := context.TODO()
	var ppln *AppInstallPipeline
	appDeployContext := &enterpriseApi.AppDeploymentContext{}

	client := spltest.NewMockClient()
	cr := enterpriseApi.Standalone{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "s1",
			Namespace: "test",
		},
		TypeMeta: metav1.TypeMeta{
			Kind: "Standalone",
		},
		Spec: enterpriseApi.StandaloneSpec{
			Replicas: 1,
			AppFrameworkConfig: enterpriseApi.AppFrameworkSpec{
				PhaseMaxRetries:           3,
				AppsRepoPollInterval:      60,
				MaxConcurrentAppDownloads: 5,
				VolList: []enterpriseApi.VolumeSpec{
					{
						Name:      "test_volume",
						Endpoint:  "https://s3-eu-west-2.amazonaws.com",
						Path:      "testbucket-rs-london",
						SecretRef: "s3-secret",
						Provider:  "aws",
					},
				},
				AppSources: []enterpriseApi.AppSourceSpec{
					{
						Name:     "appSrc1",
						Location: "adminAppsRepo",
						AppSourceDefaultSpec: enterpriseApi.AppSourceDefaultSpec{
							VolName: "test_volume",
							Scope:   "local",
						},
					},
					{
						Name:     "appSrc2",
						Location: "securityAppsRepo",
						AppSourceDefaultSpec: enterpriseApi.AppSourceDefaultSpec{
							VolName: "test_volume",
							Scope:   "local",
						},
					},
					{
						Name:     "appSrc3",
						Location: "authenticationAppsRepo",
						AppSourceDefaultSpec: enterpriseApi.AppSourceDefaultSpec{
							VolName: "test_volume",
							Scope:   "local",
						},
					},
				},
			},
		},
	}
	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-s1-standalone",
			Namespace: "test",
		},
	}
	sts.Spec.Replicas = new(int32)
	*sts.Spec.Replicas = 1

	ppln = initAppInstallPipeline(ctx, appDeployContext, client, &cr)
	pplnPhase := ppln.pplnPhases[enterpriseApi.PhaseDownload]

	testApps := []string{"app1.tgz", "app2.tgz", "app3.tgz"}
	testHashes := []string{"abcd1111", "efgh2222", "ijkl3333"}
	testSizes := []int64{10, 20, 30}

	appDeployInfoList := make([]*enterpriseApi.AppDeploymentInfo, 3)
	for index := range testApps {
		appDeployInfoList[index] = &enterpriseApi.AppDeploymentInfo{
			AppName: testApps[index],
			PhaseInfo: enterpriseApi.PhaseInfo{
				Phase:     enterpriseApi.PhaseDownload,
				Status:    enterpriseApi.AppPkgDownloadPending,
				FailCount: 0,
			},
			ObjectHash: testHashes[index],
			Size:       uint64(testSizes[index]),
		}
	}

	// create the local directory
	localPath := filepath.Join(splcommon.AppDownloadVolume, "downloadedApps", "test" /*namespace*/, "Standalone", cr.Name, "local", "appSrc1") + "/"
	err := createAppDownloadDir(ctx, localPath)
	if err != nil {
		t.Errorf("Unable to create the download directory")
	}
	defer os.RemoveAll(splcommon.AppDownloadVolume)

	// create the dummy app package for appSrc1 locally, to test the case
	// where an app is already downloaded and hence we dont re-download it
	appFileName := testApps[0] + "_" + testHashes[0]
	appLoc := filepath.Join(localPath, appFileName)
	err = createOrTruncateAppFileLocally(appLoc, testSizes[0])
	if err != nil {
		t.Errorf("Unable to create the app files locally")
	}
	defer os.Remove(appLoc)

	// create pipeline workers
	for index, appSrc := range cr.Spec.AppFrameworkConfig.AppSources {
		ppln.createAndAddPipelineWorker(ctx, enterpriseApi.PhaseDownload, appDeployInfoList[index], appSrc.Name, "", &cr.Spec.AppFrameworkConfig, client, &cr, sts)
	}

	maxWorkers := 3
	downloadPhaseWaiter := new(sync.WaitGroup)

	downloadPhaseWaiter.Add(1)
	// schedule the download threads to do actual download work
	go pplnPhase.downloadWorkerHandler(ctx, ppln, uint64(maxWorkers), downloadPhaseWaiter)

	// add the workers to msgChannel so that scheduleDownlads thread can pick them up
	for _, downloadWorker := range pplnPhase.q {
		downloadWorker.waiter = &pplnPhase.workerWaiter
		pplnPhase.msgChannel <- downloadWorker
		downloadWorker.isActive = true
	}

	close(pplnPhase.msgChannel)

	downloadPhaseWaiter.Add(1)
	close(ppln.sigTerm)
	// schedule the download threads to do actual download work
	go pplnPhase.downloadWorkerHandler(ctx, ppln, uint64(maxWorkers), downloadPhaseWaiter)

	downloadPhaseWaiter.Wait()
}

func TestCreateDownloadDirOnOperator(t *testing.T) {
	ctx := context.TODO()
	standalone := enterpriseApi.Standalone{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "s1",
			Namespace: "test",
		},
		TypeMeta: metav1.TypeMeta{
			Kind: "Standalone",
		},
		Spec: enterpriseApi.StandaloneSpec{
			Replicas: 1,
			AppFrameworkConfig: enterpriseApi.AppFrameworkSpec{
				PhaseMaxRetries:           3,
				AppsRepoPollInterval:      60,
				MaxConcurrentAppDownloads: 5,
				VolList: []enterpriseApi.VolumeSpec{
					{
						Name:      "test_volume",
						Endpoint:  "https://s3-eu-west-2.amazonaws.com",
						Path:      "testbucket-rs-london",
						SecretRef: "s3-secret",
						Provider:  "aws",
					},
				},
				AppSources: []enterpriseApi.AppSourceSpec{
					{
						Name:     "appSrc1",
						Location: "adminAppsRepo",
						AppSourceDefaultSpec: enterpriseApi.AppSourceDefaultSpec{
							VolName: "test_volume",
							Scope:   "local",
						},
					},
				},
			},
		},
	}

	worker := &PipelineWorker{
		appSrcName: "appSrc1",
		cr:         &standalone,
		afwConfig:  &standalone.Spec.AppFrameworkConfig,
		appDeployInfo: &enterpriseApi.AppDeploymentInfo{
			AppName: "testApp1",
		},
	}

	localPath, err := worker.createDownloadDirOnOperator(ctx)
	defer os.Remove(localPath)
	if err != nil {
		t.Errorf("we should have created the download directory=%s, err=%v", localPath, err)
	}
}

func getConvertedClient(client splcommon.ControllerClient) splcommon.ControllerClient {
	return client
}

func TestExtractClusterScopedAppOnPod(t *testing.T) {

	ctx := context.TODO()
	cr := enterpriseApi.ClusterManager{
		TypeMeta: metav1.TypeMeta{
			Kind: "Standalone",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
		Spec: enterpriseApi.ClusterManagerSpec{
			AppFrameworkConfig: enterpriseApi.AppFrameworkSpec{
				PhaseMaxRetries:           3,
				AppsRepoPollInterval:      60,
				MaxConcurrentAppDownloads: 5,
				VolList: []enterpriseApi.VolumeSpec{
					{
						Name:      "test_volume",
						Endpoint:  "https://s3-eu-west-2.amazonaws.com",
						Path:      "testbucket-rs-london",
						SecretRef: "s3-secret",
						Provider:  "aws",
					},
				},
				AppSources: []enterpriseApi.AppSourceSpec{
					{
						Name:     "appSrc1",
						Location: "adminAppsRepo",
						AppSourceDefaultSpec: enterpriseApi.AppSourceDefaultSpec{
							VolName: "test_volume",
							Scope:   "cluster",
						},
					},
				},
			},
		},
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-stack1-clustermanager-0",
			Namespace: "test",
			Labels: map[string]string{
				"controller-revision-hash": "v0",
			},
		},
	}

	// Create client and add object
	c := spltest.NewMockClient()
	// Add object
	c.AddObject(pod)

	// Just make the lint conversion checks happy
	var client splcommon.ControllerClient = getConvertedClient(c)

	//var client splcommon.ControllerClient
	worker := &PipelineWorker{
		appSrcName:    "appSrc1",
		afwConfig:     &cr.Spec.AppFrameworkConfig,
		cr:            &cr,
		targetPodName: "splunk-stack1-clustermanager-0",
		appDeployInfo: &enterpriseApi.AppDeploymentInfo{
			AppName: "app1.tgz",
			PhaseInfo: enterpriseApi.PhaseInfo{
				Phase:     enterpriseApi.PhasePodCopy,
				Status:    enterpriseApi.AppPkgPodCopyPending,
				FailCount: 0,
			},
		},
		client: client,
	}

	srcPath := "/opt/splunk/operator/app1.tgz"
	dstPath := fmt.Sprintf("/%s/xyz/app1.tgz", appVolumeMntName)

	podExecCommands := []string{
		"tar -xzf",
	}

	mockPodExecReturnContexts := []*spltest.MockPodExecReturnContext{
		{
			StdOut: "",
			StdErr: "",
		},
	}

	var mockPodExecClient *spltest.MockPodExecClient = &spltest.MockPodExecClient{}
	mockPodExecClient.AddMockPodExecReturnContexts(ctx, podExecCommands, mockPodExecReturnContexts...)

	// Calling with wrong scope should just return, without error
	err := extractClusterScopedAppOnPod(ctx, worker, enterpriseApi.ScopeLocal, dstPath, srcPath, mockPodExecClient)
	if err != nil {
		t.Errorf("Calling with non-cluster scope should just return, without error, but got error %v", err)
	}

	// CR kind other than SearchHeadCluster or Cluster Master should just return without an error
	// Calling with wrong scope should just return, without error
	err = extractClusterScopedAppOnPod(ctx, worker, enterpriseApi.ScopeCluster, dstPath, srcPath, mockPodExecClient)
	if err != nil {
		t.Errorf("Calling with non-cluster scope should just return, without error, but got error %v", err)
	}

	// Calling with correct params should not cause an error
	cr.TypeMeta.Kind = "ClusterManager"
	err = extractClusterScopedAppOnPod(ctx, worker, enterpriseApi.ScopeCluster, dstPath, srcPath, mockPodExecClient)
	if err != nil {
		t.Errorf("Calling with correct parameters should not cause an error, but got error %v", err)
	}

	// now just introduce a StdErr so that we cover the error scenario too
	mockPodExecReturnContexts[0].StdErr = "dummy error"
	err = extractClusterScopedAppOnPod(ctx, worker, enterpriseApi.ScopeCluster, dstPath, srcPath, mockPodExecClient)
	if err == nil {
		t.Errorf("extractClusterScopedAppOnPod should have returned error since mockPodExecClient returns error")
	}

	// just check if we received the required podExecClient
	mockPodExecClient.CheckPodExecCommands(t, "extractClusterScopedAppOnPod")
}

func TestRunPodCopyWorker(t *testing.T) {
	ctx := context.TODO()
	cr := enterpriseApi.ClusterManager{
		TypeMeta: metav1.TypeMeta{
			Kind: "ClusterManager",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
		Spec: enterpriseApi.ClusterManagerSpec{
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				Mock: true,
			},
		},
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-stack1-clustermanager-0",
			Namespace: "test",
			Labels: map[string]string{
				"controller-revision-hash": "v0",
			},
		},
	}

	appFrameworkConfig := &enterpriseApi.AppFrameworkSpec{
		PhaseMaxRetries: 3,
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

	// Create client and add object
	c := spltest.NewMockClient()
	// Add object
	c.AddObject(pod)

	// Just make the lint conversion checks happy
	var client splcommon.ControllerClient = getConvertedClient(c)
	var waiter sync.WaitGroup

	//var client splcommon.ControllerClient
	worker := &PipelineWorker{
		cr:            &cr,
		targetPodName: "splunk-stack1-clustermanager-0",
		appDeployInfo: &enterpriseApi.AppDeploymentInfo{
			AppName: "app1.tgz",
			PhaseInfo: enterpriseApi.PhaseInfo{
				Phase:     enterpriseApi.PhasePodCopy,
				Status:    enterpriseApi.AppPkgPodCopyPending,
				FailCount: 0,
			},
			ObjectHash: "abcd1234abcd",
		},
		client:     client,
		afwConfig:  appFrameworkConfig,
		waiter:     &waiter,
		appSrcName: appFrameworkConfig.AppSources[0].Name,
	}

	var ch chan struct{} = make(chan struct{}, 1)
	// occupy the worker
	waiter.Add(1)
	ch <- struct{}{}

	defaultVol := splcommon.AppDownloadVolume
	splcommon.AppDownloadVolume = "/tmp/"
	defer func() {
		splcommon.AppDownloadVolume = defaultVol
	}()

	runPodCopyWorker(ctx, worker, ch)
	if worker.appDeployInfo.PhaseInfo.Status != enterpriseApi.AppPkgMissingFromOperator {
		t.Errorf("When the app pkg is not present on the Operator Pod volume, should throw an error")
	}

	appPkgFileName := worker.appDeployInfo.AppName + "_" + strings.Trim(worker.appDeployInfo.ObjectHash, "\"")

	appSrcScope := getAppSrcScope(ctx, worker.afwConfig, worker.appSrcName)
	appPkgLocalDir := getAppPackageLocalDir(&cr, appSrcScope, worker.appSrcName)
	appPkgLocalPath := filepath.Join(appPkgLocalDir, appPkgFileName)

	_, err := os.Stat(appPkgLocalDir)
	if os.IsNotExist(err) {
		err = os.MkdirAll(appPkgLocalDir, 0755)
		if err != nil {
			t.Errorf("Unable to create the directory, error: %v", err)
		}
	}

	_, err = os.Stat(appPkgLocalPath)
	if os.IsNotExist(err) {
		_, err := os.Create(appPkgLocalPath)
		if err != nil {
			t.Errorf("Unable to create the local package file, error: %v", err)
		}
	}
	defer os.Remove(appPkgLocalPath)
	waiter.Add(1)
	ch <- struct{}{}

	runPodCopyWorker(ctx, worker, ch)
}

func TestPodCopyWorkerHandler(t *testing.T) {
	ctx := context.TODO()
	cr := enterpriseApi.ClusterManager{
		TypeMeta: metav1.TypeMeta{
			Kind: "ClusterManager",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
		Spec: enterpriseApi.ClusterManagerSpec{
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				Mock: true,
			},
		},
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-stack1-clustermanager-0",
			Namespace: "test",
			Labels: map[string]string{
				"controller-revision-hash": "v0",
			},
		},
	}

	appFrameworkConfig := &enterpriseApi.AppFrameworkSpec{
		PhaseMaxRetries: 3,
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

	// Create client and add object
	client := spltest.NewMockClient()
	// Add object
	client.AddObject(pod)

	worker := &PipelineWorker{
		cr:            &cr,
		targetPodName: "splunk-stack1-clustermanager-0",
		appDeployInfo: &enterpriseApi.AppDeploymentInfo{
			AppName: "app1.tgz",
			PhaseInfo: enterpriseApi.PhaseInfo{
				Phase:     enterpriseApi.PhasePodCopy,
				Status:    enterpriseApi.AppPkgPodCopyPending,
				FailCount: 0,
			},
			ObjectHash: "abcd1234abcd",
		},
		client:     client,
		afwConfig:  appFrameworkConfig,
		appSrcName: appFrameworkConfig.AppSources[0].Name,
	}

	defaultVol := splcommon.AppDownloadVolume
	splcommon.AppDownloadVolume = "/tmp/splunk/"
	defer func() {
		os.RemoveAll(splcommon.AppDownloadVolume)
		splcommon.AppDownloadVolume = defaultVol
	}()

	appPkgFileName := worker.appDeployInfo.AppName + "_" + strings.Trim(worker.appDeployInfo.ObjectHash, "\"")

	appSrcScope := getAppSrcScope(ctx, worker.afwConfig, worker.appSrcName)
	appPkgLocalDir := getAppPackageLocalDir(&cr, appSrcScope, worker.appSrcName)
	appPkgLocalPath := filepath.Join(appPkgLocalDir, appPkgFileName)

	_, err := os.Stat(appPkgLocalDir)
	if os.IsNotExist(err) {
		err = os.MkdirAll(appPkgLocalDir, 0755)
		if err != nil {
			t.Errorf("Unable to create the directory, error: %v", err)
		}
	}

	_, err = os.Stat(appPkgLocalPath)
	if os.IsNotExist(err) {
		_, err := os.Create(appPkgLocalPath)
		if err != nil {
			t.Errorf("Unable to create the local package file, error: %v", err)
		}
	}
	defer os.Remove(appPkgLocalPath)

	var appDeployContext *enterpriseApi.AppDeploymentContext = &enterpriseApi.AppDeploymentContext{
		AppsStatusMaxConcurrentAppDownloads: 5,
	}
	ppln := initAppInstallPipeline(ctx, appDeployContext, client, &cr)

	var handlerWaiter sync.WaitGroup
	handlerWaiter.Add(1)
	defer handlerWaiter.Wait()
	go ppln.pplnPhases[enterpriseApi.PhaseInstall].podCopyWorkerHandler(ctx, &handlerWaiter, 5)

	worker.waiter = &ppln.pplnPhases[enterpriseApi.PhaseInstall].workerWaiter

	ppln.pplnPhases[enterpriseApi.PhaseInstall].msgChannel <- worker

	time.Sleep(2 * time.Second)

	// sending null worker should not cause a crash
	ppln.pplnPhases[enterpriseApi.PhaseInstall].msgChannel <- nil

	// sending more than podCopy workers should not squeeze the podCopyWorkerHandler
	for i := 0; i < 20; i++ {
		ppln.pplnPhases[enterpriseApi.PhaseInstall].msgChannel <- worker
	}

	// wait for the handler to consue the worker
	time.Sleep(2 * time.Second)

	// Closing the channels should exit podCopyWorkerHandler test cleanly
	close(ppln.pplnPhases[enterpriseApi.PhaseInstall].msgChannel)
}

func TestIDXCRunPlaybook(t *testing.T) {
	ctx := context.TODO()
	cr := enterpriseApi.ClusterManager{
		TypeMeta: metav1.TypeMeta{
			Kind: "ClusterManager",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
	}

	c := spltest.NewMockClient()
	var appDeployContext *enterpriseApi.AppDeploymentContext = &enterpriseApi.AppDeploymentContext{
		AppsStatusMaxConcurrentAppDownloads: 10,
	}

	appDeployContext.AppsSrcDeployStatus = make(map[string]enterpriseApi.AppSrcDeployInfo)
	testApps := []string{"app1.tgz"}
	testHashes := []string{"abcd1111"}
	testSizes := []int64{10}

	appDeployInfoList := make([]enterpriseApi.AppDeploymentInfo, 1)
	for index := range testApps {
		appDeployInfoList[index] = enterpriseApi.AppDeploymentInfo{
			AppName: testApps[index],
			PhaseInfo: enterpriseApi.PhaseInfo{
				Phase:     enterpriseApi.PhasePodCopy,
				Status:    enterpriseApi.AppPkgPodCopyComplete,
				FailCount: 0,
			},
			ObjectHash: testHashes[index],
			Size:       uint64(testSizes[index]),
		}
	}

	var appSrcDeployInfo enterpriseApi.AppSrcDeployInfo
	appSrcDeployInfo.AppDeploymentInfoList = appDeployInfoList
	appDeployContext.AppsSrcDeployStatus["appSrc1"] = appSrcDeployInfo

	appDeployContext.BundlePushStatus.BundlePushStage = enterpriseApi.BundlePushPending
	afwPipeline := initAppInstallPipeline(ctx, appDeployContext, c, &cr)
	// get the target pod name
	targetPodName := getApplicablePodNameForAppFramework(&cr, 0)

	kind := cr.GetObjectKind().GroupVersionKind().Kind
	podExecClient := splutil.GetPodExecClient(c, &cr, targetPodName)
	playbookContext := getClusterScopePlaybookContext(ctx, c, &cr, afwPipeline, targetPodName, kind, podExecClient)
	err := playbookContext.runPlaybook(ctx)
	if err == nil {
		t.Errorf("runPlaybook() should have returned error, since we dont get the required output")
	}

	// now replace the pod exec client with our mock client
	podExecCommands := []string{
		fmt.Sprintf(cmdSetFilePermissionsToRW, idxcAppsLocationOnClusterManager),
		applyIdxcBundleCmdStr,
		idxcShowClusterBundleStatusStr,
	}
	mockPodExecReturnContexts := []*spltest.MockPodExecReturnContext{
		{
			StdOut: "",
			StdErr: "",
		},
		// this is for applying the cluster bundle
		{
			StdOut: "",
			StdErr: "OK\n",
		},
		// this is for checking the status of cluster bundle
		{
			StdOut: "",
			StdErr: "",
		},
	}

	var mockPodExecClient *spltest.MockPodExecClient = &spltest.MockPodExecClient{Cr: &cr}
	mockPodExecClient.AddMockPodExecReturnContexts(ctx, podExecCommands, mockPodExecReturnContexts...)

	playbookContext = getClusterScopePlaybookContext(ctx, c, &cr, afwPipeline, targetPodName, kind, mockPodExecClient)

	// If the podExec failed for changing the file permissions, should return an error
	mockPodExecReturnContexts[0].StdErr = "Failed"
	err = playbookContext.runPlaybook(ctx)
	if err == nil {
		t.Errorf("runPlaybook() should return an error if the command %v execution fails", podExecCommands[0])
	}
	mockPodExecReturnContexts[0].StdErr = ""

	// Test bundle push status to move to In progress
	err = playbookContext.runPlaybook(ctx)
	if err != nil || getBundlePushState(afwPipeline) != enterpriseApi.BundlePushInProgress {
		t.Errorf("runPlaybook() should not have returned error or wrong bundle push state, err=%v, bundle push state=%s", err, bundlePushStateAsStr(ctx, getBundlePushState(afwPipeline)))
	}

	// test the case where we try to push same bundle again
	appDeployContext.BundlePushStatus.BundlePushStage = enterpriseApi.BundlePushPending
	mockPodExecReturnContexts[1].StdErr = idxcBundleAlreadyPresentStr
	err = playbookContext.runPlaybook(ctx)
	if err != nil || getBundlePushState(afwPipeline) != enterpriseApi.BundlePushInProgress {
		t.Errorf("runPlaybook() should have not returned error since we did not get desired output")
	}

	// test the case where bundle push is still in progress
	mockPodExecReturnContexts[2].StdErr = ""
	err = playbookContext.runPlaybook(ctx)
	if err != nil || getBundlePushState(afwPipeline) != enterpriseApi.BundlePushInProgress {
		t.Errorf("runPlaybook() should have not returned error since we did not get desired output")
	}

	// test the case where bundle push is complete
	mockPodExecReturnContexts[2].StdOut = "cluster_status=None"
	err = playbookContext.runPlaybook(ctx)
	if err != nil || getBundlePushState(afwPipeline) != enterpriseApi.BundlePushComplete {
		t.Errorf("runPlaybook() should have not returned error since we did not get desired output")
	}

	// now test the error scenario where we did not get OK in stdErr
	afwPipeline.appDeployContext.BundlePushStatus.BundlePushStage = enterpriseApi.BundlePushPending
	mockPodExecReturnContexts[1].StdErr = ""
	err = playbookContext.runPlaybook(ctx)
	if err == nil {
		t.Errorf("runPlaybook() should have returned error since we did not get desired output")
	}

	// now test the error scenario where we passed invalid bundle push state
	afwPipeline.appDeployContext.BundlePushStatus.BundlePushStage = enterpriseApi.BundlePushComplete
	err = playbookContext.runPlaybook(ctx)
	if err == nil {
		t.Errorf("runPlaybook() should have returned error since we passed invalid bundle push state")
	}

	mockPodExecReturnContexts[2].StdOut = ""
	afwPipeline.appDeployContext.BundlePushStatus.BundlePushStage = enterpriseApi.BundlePushInProgress
	idxcplaybookContext := playbookContext.(*IdxcPlaybookContext)
	// invalid scenario, where stdOut!="cluster_status=None"
	if idxcplaybookContext.isBundlePushComplete(ctx) {
		t.Errorf("isBundlePushComplete() should have returned error since we did not get desired stdOut.")
	}

	// invalid scenario, where stdErr != ""
	mockPodExecReturnContexts[2].StdErr = "error"
	if idxcplaybookContext.isBundlePushComplete(ctx) {
		t.Errorf("isBundlePushComplete() should have returned false since we did not get desired stdOut.")
	}

	// valid scenario where bundle push is complete
	mockPodExecReturnContexts[2].StdErr = ""
	mockPodExecReturnContexts[2].StdOut = "cluster_status=None"
	if !idxcplaybookContext.isBundlePushComplete(ctx) {
		t.Errorf("isBundlePushComplete() should not have returned false.")
	}

	mockPodExecClient.CheckPodExecCommands(t, "idxcPlayBookContext")
}

func TestSHCRunPlaybook(t *testing.T) {
	ctx := context.TODO()
	cr := &enterpriseApi.SearchHeadCluster{
		TypeMeta: metav1.TypeMeta{
			Kind: "SearchHeadCluster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
	}

	c := spltest.NewMockClient()
	var appDeployContext *enterpriseApi.AppDeploymentContext = &enterpriseApi.AppDeploymentContext{
		AppsStatusMaxConcurrentAppDownloads: 10,
	}

	appDeployContext.AppsSrcDeployStatus = make(map[string]enterpriseApi.AppSrcDeployInfo)
	testApps := []string{"app1.tgz"}
	testHashes := []string{"abcd1111"}
	testSizes := []int64{10}

	appDeployInfoList := make([]enterpriseApi.AppDeploymentInfo, 1)
	for index := range testApps {
		appDeployInfoList[index] = enterpriseApi.AppDeploymentInfo{
			AppName: testApps[index],
			PhaseInfo: enterpriseApi.PhaseInfo{
				Phase:     enterpriseApi.PhasePodCopy,
				Status:    enterpriseApi.AppPkgPodCopyComplete,
				FailCount: 0,
			},
			ObjectHash: testHashes[index],
			Size:       uint64(testSizes[index]),
		}
	}

	cr.Status.Phase = enterpriseApi.PhaseReady
	var appSrcDeployInfo enterpriseApi.AppSrcDeployInfo
	appSrcDeployInfo.AppDeploymentInfoList = appDeployInfoList
	appDeployContext.AppsSrcDeployStatus["appSrc1"] = appSrcDeployInfo

	appDeployContext.BundlePushStatus.BundlePushStage = enterpriseApi.BundlePushPending
	afwPipeline := initAppInstallPipeline(ctx, appDeployContext, c, cr)
	// get the target pod name
	targetPodName := getApplicablePodNameForAppFramework(cr, 0)

	// get the CR kind
	kind := cr.GetObjectKind().GroupVersionKind().Kind

	podExecCommands := []string{
		fmt.Sprintf(cmdSetFilePermissionsToRW, shcAppsLocationOnDeployer),
		"/opt/splunk/bin/splunk apply shcluster-bundle",
		fmt.Sprintf("cat %s", shcBundlePushStatusCheckFile),
		fmt.Sprintf("rm %s", shcBundlePushStatusCheckFile),
	}

	mockPodExecReturnContexts := []*spltest.MockPodExecReturnContext{
		// this is for changing the file permissions
		{
			StdOut: "",
			StdErr: "",
		},
		// this is for issuing bundle push command
		{
			StdOut: shcBundlePushCompleteStr,
			StdErr: "",
			Err:    fmt.Errorf("some dummy error"),
		},
		// this is for checking the content of bundle push status file
		{
			StdOut: "",
			StdErr: "error checking content of status file",
		},
		// this is for removing the status file
		{
			StdOut: "",
			StdErr: "dummyError",
		},
	}

	// now replace the pod exec client with our mock client
	var mockPodExecClient *spltest.MockPodExecClient = &spltest.MockPodExecClient{Cr: cr}

	mockPodExecClient.AddMockPodExecReturnContexts(ctx, podExecCommands, mockPodExecReturnContexts...)

	// Test1: just for the sake of code coverage, pass invalid kind to get nil context
	playbookContext := getClusterScopePlaybookContext(ctx, c, cr, afwPipeline, targetPodName, "InvalidKind", mockPodExecClient)
	if playbookContext != nil {
		t.Errorf("playbookContext should be nil here since we passed invalid kind")
	}

	playbookContext = getClusterScopePlaybookContext(ctx, c, cr, afwPipeline, targetPodName, kind, mockPodExecClient)

	// Test2: If the poxExec failed for changing the file permissions, should return an error
	mockPodExecReturnContexts[0].StdErr = "Failed"
	err := playbookContext.runPlaybook(ctx)
	if err == nil {
		t.Errorf("runPlaybook() should should return an error if the command %v execution fails", podExecCommands[0])
	}
	mockPodExecReturnContexts[0].StdErr = ""

	// Test3: invalid scenario where we get error while applying SHC bundle push
	err = playbookContext.runPlaybook(ctx)
	if err == nil || getBundlePushState(afwPipeline) != enterpriseApi.BundlePushPending {
		t.Errorf("runPlaybook() should not have returned error, err=%v; expected bundle push state=Bundle push Pending, got=%s", err, bundlePushStateAsStr(ctx, getBundlePushState(afwPipeline)))
	}

	// Test4: valid scenario where bundle push state moves from Pending -> In Progress
	mockPodExecReturnContexts[1].Err = nil
	err = playbookContext.runPlaybook(ctx)
	if err != nil {
		t.Errorf("runPlaybook() should not have returned error, err=%v", err)
	}

	// Test5: Invalid scenario where checking the status file and removing it returned error
	err = playbookContext.runPlaybook(ctx)
	if err == nil || getBundlePushState(afwPipeline) != enterpriseApi.BundlePushPending {
		t.Errorf("runPlaybook() should have returned error or wrong bundle push state, err=%v, bundle push state=%s", err, bundlePushStateAsStr(ctx, getBundlePushState(afwPipeline)))
	}

	// Test6: Invalid scenario where checking the status file returned error but removing it is successful
	appDeployContext.BundlePushStatus.BundlePushStage = enterpriseApi.BundlePushInProgress
	mockPodExecReturnContexts[3].StdErr = ""
	err = playbookContext.runPlaybook(ctx)
	if err == nil || getBundlePushState(afwPipeline) != enterpriseApi.BundlePushPending {
		t.Errorf("runPlaybook() should have returned error or wrong bundle push state, err=%v, bundle push state=%s", err, bundlePushStateAsStr(ctx, getBundlePushState(afwPipeline)))
	}

	// Test7: Bundle push is still in progress since stdOut = ""
	appDeployContext.BundlePushStatus.BundlePushStage = enterpriseApi.BundlePushInProgress
	mockPodExecReturnContexts[2].StdErr = ""
	err = playbookContext.runPlaybook(ctx)
	if err != nil || getBundlePushState(afwPipeline) != enterpriseApi.BundlePushInProgress {
		t.Errorf("runPlaybook() should not have returned error or wrong bundle push state, err=%v, bundle push state=%s", err, bundlePushStateAsStr(ctx, getBundlePushState(afwPipeline)))
	}

	// Test8: Bundle push is still in progress since stdOut != shcBundlePushCompleteStr
	mockPodExecReturnContexts[2].StdOut = "Error while deploying apps"
	err = playbookContext.runPlaybook(ctx)
	if err == nil || getBundlePushState(afwPipeline) != enterpriseApi.BundlePushPending {
		t.Errorf("runPlaybook() should have returned error or wrong bundle push state, err=%v, bundle push state=%s", err, bundlePushStateAsStr(ctx, getBundlePushState(afwPipeline)))
	}

	// Test9: SHC status file should have the desired SHC bundle push complete message in it now. But removing the status file
	// will have an error hence we won't mark the whole bundle push state as complete yet.
	mockPodExecReturnContexts[2].StdOut = shcBundlePushCompleteStr
	mockPodExecReturnContexts[3].StdErr = "some dummy error"
	appDeployContext.BundlePushStatus.BundlePushStage = enterpriseApi.BundlePushInProgress
	err = playbookContext.runPlaybook(ctx)
	if err != nil || getBundlePushState(afwPipeline) != enterpriseApi.BundlePushInProgress {
		t.Errorf("runPlaybook() should not have returned error or wrong bundle push state, err=%v,  got bundle push state=%s, expected shc bundle push state=Bundle Push In Progress", err, bundlePushStateAsStr(ctx, getBundlePushState(afwPipeline)))
	}

	// Test10: now removing the bundle push status file should return success and hence the bundle push should be complete now.
	mockPodExecReturnContexts[3].StdErr = ""
	err = playbookContext.runPlaybook(ctx)
	if err != nil || getBundlePushState(afwPipeline) != enterpriseApi.BundlePushComplete {
		t.Errorf("runPlaybook() should not have returned error or wrong bundle push state, err=%v,  got bundle push state=%s, expected shc bundle push state=Bundle Push Complete", err, bundlePushStateAsStr(ctx, getBundlePushState(afwPipeline)))
	}

	// Test11: now test the error scenario where we passed invalid bundle push state
	afwPipeline.appDeployContext.BundlePushStatus.BundlePushStage = enterpriseApi.BundlePushComplete
	err = playbookContext.runPlaybook(ctx)
	if err == nil {
		t.Errorf("runPlaybook() should have returned error since we passed invalid bundle push state")
	}

	mockPodExecClient.CheckPodExecCommands(t, "shcPlayBookContext.runPlayBook")
}

func TestRunLocalScopedPlaybook(t *testing.T) {
	ctx := context.TODO()
	// Test for each phase can send the worker to down stream
	cr := enterpriseApi.ClusterManager{
		TypeMeta: metav1.TypeMeta{
			Kind: "ClusterManager",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
		Spec: enterpriseApi.ClusterManagerSpec{
			AppFrameworkConfig: enterpriseApi.AppFrameworkSpec{
				AppsRepoPollInterval:      60,
				MaxConcurrentAppDownloads: 5,
				PhaseMaxRetries:           3,
				VolList: []enterpriseApi.VolumeSpec{
					{
						Name:      "test_volume",
						Endpoint:  "https://s3-eu-west-2.amazonaws.com",
						Path:      "testbucket-rs-london",
						SecretRef: "s3-secret",
						Provider:  "aws",
					},
				},
				AppSources: []enterpriseApi.AppSourceSpec{
					{
						Name:     "appSrc1",
						Location: "adminAppsRepo",
						AppSourceDefaultSpec: enterpriseApi.AppSourceDefaultSpec{
							VolName: "test_volume",
							Scope:   "local",
						},
					},
				},
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

	// Create client and add object
	c := spltest.NewMockClient()

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-stack1-clustermanager-0",
			Namespace: "test",
			Labels: map[string]string{
				"controller-revision-hash": "v0",
			},
		},
	}

	// Add object
	c.AddObject(pod)

	podExecCommands := []string{
		"test -f",
		"/opt/splunk/bin/splunk install app",
		"rm -f",
	}

	mockPodExecReturnContexts := []*spltest.MockPodExecReturnContext{
		// this is for testing if file is present on pod
		{
			StdOut: "",
			StdErr: "",
			Err:    fmt.Errorf("some dummy error"),
		},
		// this is for installing the app
		{
			StdOut: "",
			StdErr: "random dummy error",
		},
		// this is for removing the app package from pod
		{
			StdOut: "",
			StdErr: "dummyError",
		},
	}

	// now replace the pod exec client with our mock client
	var mockPodExecClient *spltest.MockPodExecClient = &spltest.MockPodExecClient{}

	mockPodExecClient.AddMockPodExecReturnContexts(ctx, podExecCommands, mockPodExecReturnContexts...)

	// Just make the lint conversion checks happy
	var client splcommon.ControllerClient = getConvertedClient(c)

	var waiter sync.WaitGroup
	var localInstallCtxt *localScopePlaybookContext = &localScopePlaybookContext{
		worker: &PipelineWorker{
			appSrcName:    cr.Spec.AppFrameworkConfig.AppSources[0].Name,
			targetPodName: "splunk-stack1-clustermanager-0",
			sts:           sts,
			cr:            &cr,
			appDeployInfo: &enterpriseApi.AppDeploymentInfo{
				AppName:    "app1.tgz",
				ObjectHash: "abcdef12345abcdef",
				PhaseInfo: enterpriseApi.PhaseInfo{
					Phase:     enterpriseApi.PhaseDownload,
					Status:    enterpriseApi.AppPkgDownloadPending,
					FailCount: 2,
				},
			},
			afwConfig: &cr.Spec.AppFrameworkConfig,
			client:    client,
			waiter:    &waiter,
		},
		sem:           make(chan struct{}, 1),
		podExecClient: mockPodExecClient,
	}

	// Test1: checkIfFileExistsOnPod returns error
	localInstallCtxt.sem <- struct{}{}
	waiter.Add(1)
	err := localInstallCtxt.runPlaybook(ctx)
	if err == nil {
		t.Errorf("Failed to detect missingApp pkg: err: %s", err.Error())
	}

	// Test2: checkIfFileExistsOnPod passes but install command returns error
	mockPodExecReturnContexts[0].Err = nil
	localInstallCtxt.sem <- struct{}{}
	waiter.Add(1)
	err = localInstallCtxt.runPlaybook(ctx)
	if err == nil {
		t.Errorf("Failed to detect missingApp pkg: err: %s", err.Error())
	}

	// Test3: install command passes but removing app package from pod returns error
	mockPodExecReturnContexts[1].StdErr = ""
	localInstallCtxt.sem <- struct{}{}
	waiter.Add(1)
	err = localInstallCtxt.runPlaybook(ctx)
	if err == nil {
		t.Errorf("Failed to detect missingApp pkg: err: %s", err.Error())
	}

	// Test4: successful scenario where everything succeeds
	mockPodExecReturnContexts[2].StdErr = ""
	localInstallCtxt.sem <- struct{}{}
	waiter.Add(1)
	err = localInstallCtxt.runPlaybook(ctx)
	if err != nil {
		t.Errorf("runPlayBook should not have returned error. err=%s", err.Error())
	}

	mockPodExecClient.CheckPodExecCommands(t, "localInstallCtxt.runPlayBook")
}

func TestDeleteAppPkgFromOperator(t *testing.T) {
	ctx := context.TODO()
	cr := enterpriseApi.ClusterManager{
		TypeMeta: metav1.TypeMeta{
			Kind: "ClusterManager",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
	}

	operatorResourceTracker = &globalResourceTracker{
		storage: &storageTracker{
			availableDiskSpace: 1024 * 1024 * 1024,
		},
	}

	appFrameworkConfig := &enterpriseApi.AppFrameworkSpec{
		PhaseMaxRetries: 3,
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
	cr.Spec.AppFrameworkConfig = *appFrameworkConfig

	var worker *PipelineWorker = &PipelineWorker{
		cr:         &cr,
		appSrcName: cr.Spec.AppFrameworkConfig.AppSources[0].Name,
		appDeployInfo: &enterpriseApi.AppDeploymentInfo{
			AppName:    "testApp.spl",
			ObjectHash: "bcda23232a89",
			Size:       1000,
		},
		afwConfig: &cr.Spec.AppFrameworkConfig,
	}

	defaultVol := splcommon.AppDownloadVolume
	splcommon.AppDownloadVolume = "/tmp/"
	defer func() {
		splcommon.AppDownloadVolume = defaultVol
	}()

	appSrcScope := appFrameworkConfig.AppSources[0].Scope

	appPkgLocalDir := getAppPackageLocalDir(worker.cr, appSrcScope, worker.appSrcName)

	appPkgLocalPath := filepath.Join(appPkgLocalDir, getAppPackageName(worker))

	_, err := os.Stat(appPkgLocalDir)
	if os.IsNotExist(err) {
		err = os.MkdirAll(appPkgLocalDir, 0755)
		if err != nil {
			t.Errorf("Unable to create the directory, error: %v", err)
		}
	}

	_, err = os.Stat(appPkgLocalPath)
	if os.IsNotExist(err) {
		_, err := os.Create(appPkgLocalPath)
		if err != nil {
			t.Errorf("Unable to create the local package file, error: %v", err)
		}
	}
	defer os.Remove(appPkgLocalPath)

	diskSpaceBeforeRemoval := operatorResourceTracker.storage.availableDiskSpace
	deleteAppPkgFromOperator(ctx, worker)

	if operatorResourceTracker.storage.availableDiskSpace != diskSpaceBeforeRemoval+worker.appDeployInfo.Size {
		t.Errorf("Unable to clean up the app package on Operator pod")
	}
}

func TestGetInstallSlotForPod(t *testing.T) {
	ctx := context.TODO()
	podInstallTracker := make([]chan struct{}, 10)
	for i := range podInstallTracker {
		podInstallTracker[i] = make(chan struct{}, maxParallelInstallsPerPod)
	}

	var podName string
	if getInstallSlotForPod(ctx, podInstallTracker, podName) {
		t.Errorf("invalid pod name should not return a install slot")
	}

	podName = "splunk-s2apps-standalone-0"

	if !getInstallSlotForPod(ctx, podInstallTracker, podName) {
		t.Errorf("Unable to allocate an install slot, when the slot is available")
	}

	if getInstallSlotForPod(ctx, podInstallTracker, podName) {
		t.Errorf("Should not allocate an install slot, when the slot is already occupied")
	}
}

func TestFreeInstallSlotForPod(t *testing.T) {
	ctx := context.TODO()
	podInstallTracker := make([]chan struct{}, 10)
	for i := range podInstallTracker {
		podInstallTracker[i] = make(chan struct{}, maxParallelInstallsPerPod)
	}

	var podName string
	// invalid pod name should not block
	freeInstallSlotForPod(ctx, podInstallTracker, podName)

	podName = "splunk-s2apps-standalone-0"

	// when the slot is not allocated, should not block
	freeInstallSlotForPod(ctx, podInstallTracker, podName)

	// when the slot is allocated, should free the slot without blocking
	podInstallTracker[0] <- struct{}{}
	freeInstallSlotForPod(ctx, podInstallTracker, podName)
}

func TestIsPendingClusterScopeWork(t *testing.T) {
	cr := &enterpriseApi.ClusterManager{
		TypeMeta: metav1.TypeMeta{
			Kind: "Standalone",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
		Spec: enterpriseApi.ClusterManagerSpec{
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				Mock: true,
			},
		},
	}
	afwPipeline := &AppInstallPipeline{
		cr: cr,
		appDeployContext: &enterpriseApi.AppDeploymentContext{
			BundlePushStatus: enterpriseApi.BundlePushTracker{
				BundlePushStage: enterpriseApi.BundlePushInProgress,
			},
		},
	}

	// For Standalone CR, should not detect cluster scoped work
	if isPendingClusterScopeWork(afwPipeline) {
		t.Errorf("There should not be any cluster scoped work for a CR without cluster scope")
	}

	// For cluster scoped CR, cluster scope pending activity should be detected
	cr.TypeMeta.Kind = "ClusterManager"
	afwPipeline.cr = cr
	if !isPendingClusterScopeWork(afwPipeline) {
		t.Errorf("When the bundle push activity is there, should return true")
	}

	// When there is no pending bundle push, should return false
	afwPipeline.appDeployContext.BundlePushStatus.BundlePushStage = enterpriseApi.BundlePushComplete
	// For cluster scoped CR, cluster scope pending activity should be detected
	afwPipeline.cr.GetObjectMeta().SetName("ClusterManager")
	if isPendingClusterScopeWork(afwPipeline) {
		t.Errorf("Should return false, when there is no bundle push activity")
	}

}

func TestNeedToRunClusterScopedPlaybook(t *testing.T) {
	ctx := context.TODO()
	cr := &enterpriseApi.ClusterManager{
		TypeMeta: metav1.TypeMeta{
			Kind: "Standalone",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
		Spec: enterpriseApi.ClusterManagerSpec{
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				Mock: true,
			},
		},
	}
	afwPipeline := &AppInstallPipeline{
		cr: cr,
		appDeployContext: &enterpriseApi.AppDeploymentContext{
			BundlePushStatus: enterpriseApi.BundlePushTracker{
				BundlePushStage: enterpriseApi.BundlePushInProgress,
			},
		},
	}

	// For local only scoped CRs, should not trigger cluster scoped playbook
	if needToRunClusterScopedPlaybook(afwPipeline) {
		t.Errorf("Should not return true for local only scoped CRs")
	}

	statefulSetName := "splunk-stack1-clustermanager"
	var replicas int32 = 1

	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      statefulSetName,
			Namespace: "test",
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &replicas,
		},
	}

	client := spltest.NewMockClient()
	_, err := splctrl.ApplyStatefulSet(ctx, client, sts)
	if err != nil {
		t.Errorf("unable to apply statefulset")
	}

	var appDeployContext *enterpriseApi.AppDeploymentContext = &enterpriseApi.AppDeploymentContext{
		AppsStatusMaxConcurrentAppDownloads: 10,
	}

	cr.TypeMeta.Kind = "ClusterManager"
	afwPipeline = initAppInstallPipeline(ctx, appDeployContext, client, cr)
	podCopyWorker := &PipelineWorker{appDeployInfo: &enterpriseApi.AppDeploymentInfo{PhaseInfo: enterpriseApi.PhaseInfo{FailCount: 10}}}

	afwPipeline.pplnPhases[enterpriseApi.PhasePodCopy].q = append(afwPipeline.pplnPhases[enterpriseApi.PhasePodCopy].q, podCopyWorker)

	// When the pipeline are not empty, should return false
	if needToRunClusterScopedPlaybook(afwPipeline) {
		t.Errorf("When pipelines are not empty, should return false")
	}

	// When there is a app framework pending activity, revisit to the app framework scheduler should happen
	if !needToRevisitAppFramework(afwPipeline) {
		t.Errorf("When there is pending app framework activity, should return true")
	}

	// When there is no app framework pending activity, revisit to the app framework scheduler should not happen
	appDeployContext.IsDeploymentInProgress = false
	afwPipeline.pplnPhases[enterpriseApi.PhasePodCopy].q = nil
	appDeployContext.BundlePushStatus.BundlePushStage = enterpriseApi.BundlePushComplete
	if needToRevisitAppFramework(afwPipeline) {
		t.Errorf("When there is no pending app framework activity, should return false")
	}

	// When there is no app framework pending activity, same should reflect in the appDeployContext progress tracking
	appDeployContext.IsDeploymentInProgress = true
	checkAndUpdateAppFrameworkProgressFlag(afwPipeline)

	if appDeployContext.IsDeploymentInProgress {
		t.Errorf("Failed to mark the app deployment to completion")
	}
}

func TestHandleAppPkgInstallComplete(t *testing.T) {
	ctx := context.TODO()
	cr := enterpriseApi.Standalone{
		TypeMeta: metav1.TypeMeta{
			Kind: "Standalone",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
	}

	appFrameworkConfig := &enterpriseApi.AppFrameworkSpec{
		PhaseMaxRetries: 3,
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

	client := spltest.NewMockClient()
	worker := &PipelineWorker{
		cr:            &cr,
		targetPodName: "splunk-stack1-standalone-0",
		appDeployInfo: &enterpriseApi.AppDeploymentInfo{
			AppName: "app1.tgz",
			PhaseInfo: enterpriseApi.PhaseInfo{
				Phase:     enterpriseApi.PhaseInstall,
				Status:    enterpriseApi.AppPkgInstallComplete,
				FailCount: 0,
			},
			ObjectHash: "abcd1234abcd",
		},
		client:     client,
		appSrcName: appFrameworkConfig.AppSources[0].Name,
		afwConfig:  appFrameworkConfig,
	}

	defaultVol := splcommon.AppDownloadVolume
	splcommon.AppDownloadVolume = "/tmp/splunk/"
	defer func() {
		os.RemoveAll(splcommon.AppDownloadVolume)
		splcommon.AppDownloadVolume = defaultVol
	}()

	appPkgLocalPath := getAppPackageLocalPath(ctx, worker)
	appPkgLocalDir := path.Dir(appPkgLocalPath)

	_, err := os.Stat(appPkgLocalDir)
	if os.IsNotExist(err) {
		err = os.MkdirAll(appPkgLocalDir, 0755)
		if err != nil {
			t.Errorf("Unable to create the directory, error: %v", err)
		}
	}

	_, err = os.Stat(appPkgLocalPath)
	if os.IsNotExist(err) {
		_, err := os.Create(appPkgLocalPath)
		if err != nil {
			t.Errorf("Unable to create the local package file, error: %v", err)
		}
	}
	defer os.Remove(appPkgLocalPath)

	// Standalone with only one replicas
	// Case-1: App pkg should not be deleted, when the installation is not complete on the Pod.
	worker.appDeployInfo.AuxPhaseInfo = append(worker.appDeployInfo.AuxPhaseInfo, enterpriseApi.PhaseInfo{
		Phase:  enterpriseApi.PhaseInstall,
		Status: enterpriseApi.AppPkgInstallInProgress})

	_, err = os.Create(appPkgLocalPath)
	if err != nil {
		t.Errorf("Unable to create the local package file, error: %v", err)
	}

	tryAppPkgCleanupFromOperatorPod(ctx, worker)
	_, err = os.Stat(appPkgLocalPath)
	if os.IsNotExist(err) {
		t.Errorf("App pkg should not be deleted when the install is not complete on all the Pods ")
	}

	// Case-2: When only one replica exists, should delete the app  package and also should set the main phase info to install
	_, err = os.Create(appPkgLocalPath)
	if err != nil {
		t.Errorf("Unable to create the local package file, error: %v", err)
	}

	worker.appDeployInfo.AuxPhaseInfo[0] = enterpriseApi.PhaseInfo{
		Phase:  enterpriseApi.PhaseInstall,
		Status: enterpriseApi.AppPkgInstallComplete}

	tryAppPkgCleanupFromOperatorPod(ctx, worker)
	_, err = os.Stat(appPkgLocalPath)
	if err == nil || !os.IsNotExist(err) {
		t.Errorf("App pkg should be deleted when the install is complete on all the Pods ")
	}

	if worker.appDeployInfo.PhaseInfo.Phase != enterpriseApi.PhaseInstall || worker.appDeployInfo.PhaseInfo.Status != enterpriseApi.AppPkgInstallComplete {
		t.Errorf("When app is installed on all the replica pods, same should be updated in the Phase info")
	}

	// Standalone with only multiple replicas
	// Case-1: App pkg should not be deleted, when the installation is not complete on all the replica members.
	worker.appDeployInfo.AuxPhaseInfo = append(worker.appDeployInfo.AuxPhaseInfo, enterpriseApi.PhaseInfo{
		Phase:  enterpriseApi.PhasePodCopy,
		Status: enterpriseApi.AppPkgPodCopyComplete})

	_, err = os.Create(appPkgLocalPath)
	if err != nil {
		t.Errorf("Unable to create the local package file, error: %v", err)
	}

	tryAppPkgCleanupFromOperatorPod(ctx, worker)
	_, err = os.Stat(appPkgLocalPath)
	if os.IsNotExist(err) {
		t.Errorf("App pkg should not be deleted when the install is not complete on all the Pods ")
	}

	// Case-2: When all the install is complete on all the replica members, should delete the app  package and also should set the main phase info to install
	_, err = os.Create(appPkgLocalPath)
	if err != nil {
		t.Errorf("Unable to create the local package file, error: %v", err)
	}

	worker.appDeployInfo.AuxPhaseInfo[1] = enterpriseApi.PhaseInfo{
		Phase:  enterpriseApi.PhaseInstall,
		Status: enterpriseApi.AppPkgInstallComplete}

	tryAppPkgCleanupFromOperatorPod(ctx, worker)
	_, err = os.Stat(appPkgLocalPath)
	if err == nil || !os.IsNotExist(err) {
		t.Errorf("App pkg should be deleted when the install is complete on all the Pods ")
	}

	if worker.appDeployInfo.PhaseInfo.Phase != enterpriseApi.PhaseInstall || worker.appDeployInfo.PhaseInfo.Status != enterpriseApi.AppPkgInstallComplete {
		t.Errorf("When app is installed on all the replica pods, same should be updated in the Phase info")
	}

	// For a CR, other than standalone, whenever the install is complete, should also delete the app pkg from operator pod
	cr.TypeMeta.Kind = "ClusterManager"
	worker.appDeployInfo.AuxPhaseInfo = nil
	appPkgLocalPath = getAppPackageLocalPath(ctx, worker)
	appPkgLocalDir = path.Dir(appPkgLocalPath)

	_, err = os.Stat(appPkgLocalDir)
	if os.IsNotExist(err) {
		err = os.MkdirAll(appPkgLocalDir, 0755)
		if err != nil {
			t.Errorf("Unable to create the directory, error: %v", err)
		}
	}

	_, err = os.Create(appPkgLocalPath)
	if err != nil {
		t.Errorf("Unable to create the local package file, error: %v", err)
	}

	tryAppPkgCleanupFromOperatorPod(ctx, worker)
	_, err = os.Stat(appPkgLocalPath)
	if err == nil || !os.IsNotExist(err) {
		t.Errorf("App pkg should be deleted when the install is complete on all the Pods ")
	}
}

func TestInstallWorkerHandler(t *testing.T) {
	ctx := context.TODO()
	cr := enterpriseApi.ClusterManager{
		TypeMeta: metav1.TypeMeta{
			Kind: "ClusterManager",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
		Spec: enterpriseApi.ClusterManagerSpec{
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				Mock: true,
			},
		},
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-stack1-clustermanager-0",
			Namespace: "test",
			Labels: map[string]string{
				"controller-revision-hash": "v0",
			},
		},
	}

	appFrameworkConfig := &enterpriseApi.AppFrameworkSpec{
		PhaseMaxRetries: 3,
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

	// Create client and add object
	client := spltest.NewMockClient()
	// Add object
	client.AddObject(pod)

	// create statefulset for the cluster master
	statefulSetName := "splunk-stack1-clustermanager"
	var replicas int32 = 1

	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      statefulSetName,
			Namespace: "test",
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &replicas,
		},
	}

	_, err := splctrl.ApplyStatefulSet(ctx, client, sts)
	if err != nil {
		t.Errorf("unable to apply statefulset")
	}

	worker := &PipelineWorker{
		cr:            &cr,
		targetPodName: "splunk-stack1-clustermanager-0",
		appDeployInfo: &enterpriseApi.AppDeploymentInfo{
			AppName: "app1.tgz",
			PhaseInfo: enterpriseApi.PhaseInfo{
				Phase:     enterpriseApi.PhaseInstall,
				Status:    enterpriseApi.AppPkgInstallPending,
				FailCount: 0,
			},
			ObjectHash: "abcd1234abcd",
		},
		client:     client,
		afwConfig:  appFrameworkConfig,
		sts:        sts,
		appSrcName: appFrameworkConfig.AppSources[0].Name,
	}

	var appDeployContext *enterpriseApi.AppDeploymentContext = &enterpriseApi.AppDeploymentContext{
		AppsStatusMaxConcurrentAppDownloads: 5,
	}
	ppln := initAppInstallPipeline(ctx, appDeployContext, client, &cr)

	podInstallTracker := make([]chan struct{}, *sts.Spec.Replicas)
	for i := range podInstallTracker {
		podInstallTracker[i] = make(chan struct{}, maxParallelInstallsPerPod)
	}

	var handlerWaiter sync.WaitGroup
	handlerWaiter.Add(1)
	defer handlerWaiter.Wait()
	go ppln.pplnPhases[enterpriseApi.PhaseInstall].installWorkerHandler(ctx, ppln, &handlerWaiter, podInstallTracker)

	// Send a worker to the install handler
	podInstallTracker[0] <- struct{}{}
	worker.waiter = &ppln.pplnPhases[enterpriseApi.PhaseInstall].workerWaiter
	ppln.pplnPhases[enterpriseApi.PhaseInstall].msgChannel <- worker

	time.Sleep(2 * time.Second)

	// sending null worker should not cause a crash
	ppln.pplnPhases[enterpriseApi.PhaseInstall].msgChannel <- nil

	appDeployContext.BundlePushStatus.BundlePushStage = enterpriseApi.BundlePushPending
	// Closing the channels should exit podCopyWorkerHandler test cleanly
	close(ppln.pplnPhases[enterpriseApi.PhaseInstall].msgChannel)

	time.Sleep(2 * time.Second)
	// Now that the local scoped app should have failed, let us remove from the queue
	// and make sure that the install handler exits cleanly
	ppln.pplnPhases[enterpriseApi.PhaseInstall].q = nil
	appDeployContext.BundlePushStatus.BundlePushStage = enterpriseApi.BundlePushComplete
}

func TestAfwYieldWatcher(t *testing.T) {
	ctx := context.TODO()
	cr := enterpriseApi.ClusterManager{
		TypeMeta: metav1.TypeMeta{
			Kind: "ClusterManager",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
		Spec: enterpriseApi.ClusterManagerSpec{
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				Mock: true,
			},
		},
	}

	afwPipeline := &AppInstallPipeline{
		appDeployContext: &enterpriseApi.AppDeploymentContext{
			AppFrameworkConfig: enterpriseApi.AppFrameworkSpec{
				SchedulerYieldInterval: 300,
			},
		},
	}
	afwPipeline.cr = &cr
	afwPipeline.sigTerm = make(chan struct{})
	afwPipeline.afwEntryTime = time.Now().Unix()

	// add a waiter
	afwPipeline.phaseWaiter.Add(1)

	// When the pipeline is empty, should break
	afwPipeline.afwYieldWatcher(ctx)
	afwPipeline.phaseWaiter.Wait()

	_, channelOpen := <-afwPipeline.sigTerm

	// This test should also fail, if the yield is really not happening for 300 seconds
	if channelOpen {
		t.Errorf("When the pipelines are empty, should yield immediately")
	}

	// When the pipeline is not empty, should wait for the max. yield time.
	afwPipeline.pplnPhases = make(map[enterpriseApi.AppPhaseType]*PipelinePhase, 1)
	initPipelinePhase(afwPipeline, enterpriseApi.PhaseDownload)
	afwPipeline.pplnPhases[enterpriseApi.PhaseDownload].q = append(afwPipeline.pplnPhases[enterpriseApi.PhaseDownload].q, &PipelineWorker{})
	afwPipeline.sigTerm = make(chan struct{})
	afwPipeline.afwEntryTime = time.Now().Unix()
	currentTime := afwPipeline.afwEntryTime

	// Add a waiter
	afwPipeline.phaseWaiter.Add(1)
	// When the pipeline is empty, should break
	afwPipeline.appDeployContext.AppFrameworkConfig.SchedulerYieldInterval = 10
	afwPipeline.afwYieldWatcher(ctx)

	afwPipeline.phaseWaiter.Wait()

	_, channelOpen = <-afwPipeline.sigTerm
	if channelOpen {
		t.Errorf("Channel should be closed by the time we reach hear, OR else, this test should have failed due to max. timeout")
	}

	yieldTimeSpent := time.Now().Unix() - currentTime
	if yieldTimeSpent < int64(afwPipeline.appDeployContext.AppFrameworkConfig.SchedulerYieldInterval) {
		t.Errorf("When the pipelines are not empty, yield should happen only on timer expiry. Time spent by scheduler: %v", yieldTimeSpent)
	}
}

// Following test doesn't have anything to test in particular.
// When there are no workers pending, the scheduler should return immediately.
// If the scheduler is blocked for an extended period of time, it should fail at infra level.
func TestAfwSchedulerEntry(t *testing.T) {
	ctx := context.TODO()
	cr := enterpriseApi.ClusterManager{
		TypeMeta: metav1.TypeMeta{
			Kind: "Standalone",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
		Spec: enterpriseApi.ClusterManagerSpec{
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				Mock: true,
			},
		},
	}

	appFrameworkConfig := &enterpriseApi.AppFrameworkSpec{
		PhaseMaxRetries: 3,
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

	var appDeployContext *enterpriseApi.AppDeploymentContext = &enterpriseApi.AppDeploymentContext{
		AppsStatusMaxConcurrentAppDownloads: 10,
	}

	statefulSetName := "splunk-stack1-standalone"
	var replicas int32 = 32

	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      statefulSetName,
			Namespace: "test",
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &replicas,
		},
	}

	client := spltest.NewMockClient()
	_, err := splctrl.ApplyStatefulSet(ctx, client, sts)
	if err != nil {
		t.Errorf("unable to apply statefulset")
	}

	startTime := time.Now().Unix()
	afwSchedulerEntry(ctx, client, &cr, appDeployContext, appFrameworkConfig)
	// Assuming the test is returning sooner, testing it for a 2 seconds value, which should be optimal for now
	waitTime := time.Now().Unix() - startTime
	if waitTime > 2 {
		t.Errorf("For an empty pipeline, scheduler should return immediately, but spent: %v", waitTime)
	}
}

func TestGetClusterScopedAppsLocOnPod(t *testing.T) {
	cr := &enterpriseApi.ClusterManager{
		TypeMeta: metav1.TypeMeta{
			Kind: "Standalone",
		},
	}

	// Should return empty cluster apps location
	if getClusterScopedAppsLocOnPod(cr) != "" {
		t.Errorf("When the clustering is not applicable to the CR, location should be empty")
	}

	// should return master apps location
	cr.TypeMeta.Kind = "ClusterManager"
	retLoc := getClusterScopedAppsLocOnPod(cr)
	if retLoc != idxcAppsLocationOnClusterManager {
		t.Errorf("ClusterManager: Expected location: %v, but got: %v", idxcAppsLocationOnClusterManager, retLoc)
	}

	// should return shc cluster apps location
	cr.TypeMeta.Kind = "SearchHeadCluster"
	retLoc = getClusterScopedAppsLocOnPod(cr)
	if retLoc != shcAppsLocationOnDeployer {
		t.Errorf("SHC: Expected location: %v, but got: %v", idxcAppsLocationOnClusterManager, retLoc)
	}
}

func TestAdjustClusterAppsFilePermissions(t *testing.T) {
	ctx := context.TODO()
	cr := &enterpriseApi.ClusterManager{
		TypeMeta: metav1.TypeMeta{
			Kind: "Standalone",
		},
	}

	podExecCommands := []string{
		fmt.Sprintf(cmdSetFilePermissionsToRW, idxcAppsLocationOnClusterManager),
		fmt.Sprintf(cmdSetFilePermissionsToRW, shcAppsLocationOnDeployer),
	}
	mockPodExecReturnContexts := []*spltest.MockPodExecReturnContext{
		{
			StdOut: "",
			StdErr: "",
		},
		{
			StdOut: "",
			StdErr: "",
		},
	}

	var mockPodExecClient *spltest.MockPodExecClient = &spltest.MockPodExecClient{Cr: cr}
	mockPodExecClient.AddMockPodExecReturnContexts(ctx, podExecCommands, mockPodExecReturnContexts...)

	// For a CR with no cluster scope, should return an error
	err := adjustClusterAppsFilePermissions(ctx, mockPodExecClient)
	if err == nil || strings.Compare(err.Error(), "invalid Cluster apps location") != 0 {
		t.Errorf("For CR kind Standalone, should return an error")
	}

	// For CM, should not return an error
	cr.TypeMeta.Kind = "ClusterManager"
	err = adjustClusterAppsFilePermissions(ctx, mockPodExecClient)
	if err != nil {
		t.Errorf("For CM CR kind should not cause an error, but got error: %v", err)
	}

	// When the permissions changes fails, should return an error
	mockPodExecReturnContexts[0].StdErr = "Failed"
	err = adjustClusterAppsFilePermissions(ctx, mockPodExecClient)
	if err == nil {
		t.Errorf("When the file permissions can't be modified, should return an error")
	}
	mockPodExecReturnContexts[0].StdErr = ""

	// For SHC, should not return an error
	cr.TypeMeta.Kind = "SearchHeadCluster"
	err = adjustClusterAppsFilePermissions(ctx, mockPodExecClient)
	if err != nil {
		t.Errorf("For SHC CR kind should not cause an error, but got error: %v", err)
	}

	// When the permissions changes fails, should return an error
	mockPodExecReturnContexts[1].StdErr = "Invalid path"
	err = adjustClusterAppsFilePermissions(ctx, mockPodExecClient)
	if err == nil {
		t.Errorf("When the file permissions can't be modified, should return an error")
	}
	mockPodExecReturnContexts[0].StdErr = ""
}

func TestGetTelAppNameExtension(t *testing.T) {
	crKinds := map[string]string{
		"Standalone":        "stdaln",
		"LicenseMaster":     "lmaster",
		"LicenseManager":    "lmanager",
		"SearchHeadCluster": "shc",
		"ClusterMaster":     "cmaster",
		"ClusterManager":    "cmanager",
	}

	// Test all CR kinds
	for k, v := range crKinds {
		val, _ := getTelAppNameExtension(k)
		if v != val {
			t.Errorf("Invalid extension crkind %v, extension %v", k, v)
		}
	}

	// Test error code
	_, err := getTelAppNameExtension("incorrect value")
	if err == nil {
		t.Errorf("Expected error")
	}
}

func TestAddTelAppCMaster(t *testing.T) {
	ctx := context.TODO()

	// Define CRs
	cmCr := &enterpriseApiV3.ClusterMaster{
		TypeMeta: metav1.TypeMeta{
			Kind: "ClusterMaster",
		},
	}

	shcCr := &enterpriseApi.SearchHeadCluster{
		TypeMeta: metav1.TypeMeta{
			Kind: "SearchHeadCluster",
		},
	}

	// Define mock podexec context
	podExecCommands := []string{
		fmt.Sprintf(createTelAppNonShcString, "cmaster", telAppConfString, "cmaster"),
		telAppReloadString,
	}

	mockPodExecReturnContexts := []*spltest.MockPodExecReturnContext{
		{
			StdOut: "",
		},
		{
			StdOut: "",
		},
	}

	var mockPodExecClient *spltest.MockPodExecClient = &spltest.MockPodExecClient{Cr: cmCr}
	mockPodExecClient.AddMockPodExecReturnContexts(ctx, podExecCommands, mockPodExecReturnContexts...)

	// Test non-shc
	err := addTelApp(ctx, mockPodExecClient, 1, cmCr)
	if err != nil {
		t.Errorf("Tel app not added successfully, error: %v", err)
	}

	// Test shc
	podExecCommands = []string{
		fmt.Sprintf(createTelAppShcString, shcAppsLocationOnDeployer, "shc", telAppConfString, shcAppsLocationOnDeployer, "shc"),
		fmt.Sprintf(applySHCBundleCmdStr, GetSplunkStatefulsetURL(shcCr.GetNamespace(), SplunkSearchHead, shcCr.GetName(), 0, false), "/tmp/status.txt"),
	}

	mockPodExecClient.AddMockPodExecReturnContexts(ctx, podExecCommands, mockPodExecReturnContexts...)
	mockPodExecClient.Cr = shcCr

	err = addTelApp(ctx, mockPodExecClient, 1, shcCr)
	if err != nil {
		t.Errorf("Tel app not added successfully, error: %v", err)
	}

	// Testing error handling

	// Test non-shc error 1
	podExecCommandsError := []string{
		fmt.Sprintf(createTelAppNonShcString, "cmerror", telAppConfString, "cmerror"),
	}

	mockPodExecReturnContextsError := []*spltest.MockPodExecReturnContext{
		{
			StdOut: "",
		},
	}

	var mockPodExecClientError1 *spltest.MockPodExecClient = &spltest.MockPodExecClient{Cr: cmCr}
	mockPodExecClientError1.AddMockPodExecReturnContexts(ctx, podExecCommandsError, mockPodExecReturnContextsError...)

	err = addTelApp(ctx, mockPodExecClientError1, 1, cmCr)
	if err == nil {
		t.Errorf("Expected error")
	}

	// Test non-shc error 2
	podExecCommandsError = []string{
		fmt.Sprintf(createTelAppNonShcString, "cm", telAppConfString, "cm"),
	}
	var mockPodExecClientError2 *spltest.MockPodExecClient = &spltest.MockPodExecClient{Cr: cmCr}
	mockPodExecClientError2.AddMockPodExecReturnContexts(ctx, podExecCommandsError, mockPodExecReturnContextsError...)

	err = addTelApp(ctx, mockPodExecClientError2, 1, cmCr)
	if err == nil {
		t.Errorf("Expected error")
	}

	// Test shc error 1
	podExecCommandsError = []string{
		fmt.Sprintf(createTelAppShcString, shcAppsLocationOnDeployer, "shcerror", telAppConfString, shcAppsLocationOnDeployer, "shcerror"),
	}

	var mockPodExecClientError3 *spltest.MockPodExecClient = &spltest.MockPodExecClient{Cr: shcCr}
	mockPodExecClientError3.AddMockPodExecReturnContexts(ctx, podExecCommandsError, mockPodExecReturnContextsError...)

	err = addTelApp(ctx, mockPodExecClientError3, 1, shcCr)
	if err == nil {
		t.Errorf("Expected error")
	}

	// Test shc error 2
	podExecCommandsError = []string{
		fmt.Sprintf(createTelAppShcString, shcAppsLocationOnDeployer, "shc", telAppConfString, shcAppsLocationOnDeployer, "shc"),
	}
	var mockPodExecClientError4 *spltest.MockPodExecClient = &spltest.MockPodExecClient{Cr: shcCr}
	mockPodExecClientError4.AddMockPodExecReturnContexts(ctx, podExecCommandsError, mockPodExecReturnContextsError...)

	err = addTelApp(ctx, mockPodExecClientError4, 1, shcCr)
	if err == nil {
		t.Errorf("Expected error")
	}
}

func TestAddTelAppCManager(t *testing.T) {
	ctx := context.TODO()

	// Define CRs
	cmCr := &enterpriseApi.ClusterManager{
		TypeMeta: metav1.TypeMeta{
			Kind: "ClusterManager",
		},
	}

	shcCr := &enterpriseApi.SearchHeadCluster{
		TypeMeta: metav1.TypeMeta{
			Kind: "SearchHeadCluster",
		},
	}

	// Define mock podexec context
	podExecCommands := []string{
		fmt.Sprintf(createTelAppNonShcString, "cmanager", telAppConfString, "cmanager"),
		telAppReloadString,
	}

	mockPodExecReturnContexts := []*spltest.MockPodExecReturnContext{
		{
			StdOut: "",
		},
		{
			StdOut: "",
		},
	}

	var mockPodExecClient *spltest.MockPodExecClient = &spltest.MockPodExecClient{Cr: cmCr}
	mockPodExecClient.AddMockPodExecReturnContexts(ctx, podExecCommands, mockPodExecReturnContexts...)

	// Test non-shc
	err := addTelApp(ctx, mockPodExecClient, 1, cmCr)
	if err != nil {
		t.Errorf("Tel app not added successfully, error: %v", err)
	}

	// Test shc
	podExecCommands = []string{
		fmt.Sprintf(createTelAppShcString, shcAppsLocationOnDeployer, "shc", telAppConfString, shcAppsLocationOnDeployer, "shc"),
		fmt.Sprintf(applySHCBundleCmdStr, GetSplunkStatefulsetURL(shcCr.GetNamespace(), SplunkSearchHead, shcCr.GetName(), 0, false), "/tmp/status.txt"),
	}

	mockPodExecClient.AddMockPodExecReturnContexts(ctx, podExecCommands, mockPodExecReturnContexts...)
	mockPodExecClient.Cr = shcCr

	err = addTelApp(ctx, mockPodExecClient, 1, shcCr)
	if err != nil {
		t.Errorf("Tel app not added successfully, error: %v", err)
	}

	// Testing error handling

	// Test non-shc error 1
	podExecCommandsError := []string{
		fmt.Sprintf(createTelAppNonShcString, "cmerror", telAppConfString, "cmerror"),
	}

	mockPodExecReturnContextsError := []*spltest.MockPodExecReturnContext{
		{
			StdOut: "",
		},
	}

	var mockPodExecClientError1 *spltest.MockPodExecClient = &spltest.MockPodExecClient{Cr: cmCr}
	mockPodExecClientError1.AddMockPodExecReturnContexts(ctx, podExecCommandsError, mockPodExecReturnContextsError...)

	err = addTelApp(ctx, mockPodExecClientError1, 1, cmCr)
	if err == nil {
		t.Errorf("Expected error")
	}

	// Test non-shc error 2
	podExecCommandsError = []string{
		fmt.Sprintf(createTelAppNonShcString, "cm", telAppConfString, "cm"),
	}
	var mockPodExecClientError2 *spltest.MockPodExecClient = &spltest.MockPodExecClient{Cr: cmCr}
	mockPodExecClientError2.AddMockPodExecReturnContexts(ctx, podExecCommandsError, mockPodExecReturnContextsError...)

	err = addTelApp(ctx, mockPodExecClientError2, 1, cmCr)
	if err == nil {
		t.Errorf("Expected error")
	}

	// Test shc error 1
	podExecCommandsError = []string{
		fmt.Sprintf(createTelAppShcString, shcAppsLocationOnDeployer, "shcerror", telAppConfString, shcAppsLocationOnDeployer, "shcerror"),
	}

	var mockPodExecClientError3 *spltest.MockPodExecClient = &spltest.MockPodExecClient{Cr: shcCr}
	mockPodExecClientError3.AddMockPodExecReturnContexts(ctx, podExecCommandsError, mockPodExecReturnContextsError...)

	err = addTelApp(ctx, mockPodExecClientError3, 1, shcCr)
	if err == nil {
		t.Errorf("Expected error")
	}

	// Test shc error 2
	podExecCommandsError = []string{
		fmt.Sprintf(createTelAppShcString, shcAppsLocationOnDeployer, "shc", telAppConfString, shcAppsLocationOnDeployer, "shc"),
	}
	var mockPodExecClientError4 *spltest.MockPodExecClient = &spltest.MockPodExecClient{Cr: shcCr}
	mockPodExecClientError4.AddMockPodExecReturnContexts(ctx, podExecCommandsError, mockPodExecReturnContextsError...)

	err = addTelApp(ctx, mockPodExecClientError4, 1, shcCr)
	if err == nil {
		t.Errorf("Expected error")
	}
}
