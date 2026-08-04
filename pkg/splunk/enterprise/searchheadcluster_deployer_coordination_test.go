// Copyright (c) 2026 Splunk Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package enterprise

import (
	"context"
	"testing"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	splclient "github.com/splunk/splunk-operator/pkg/splunk/client/splunk"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	pkgruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const shcDeployerCoordinationAnnotation = "test.splunk.com/queued-template"

func TestApplySearchHeadClusterDefersDeployerTemplateDuringMemberLifecycle(
	t *testing.T,
) {
	ctx, controllerClient, cr := newSHCDeployerCoordinationFixture(t)
	deployer, deployerPod := makeSHCDeployerStable(
		t,
		ctx,
		controllerClient,
		cr,
		"deployer-revision-a",
	)

	replicas := int32(3)
	targetOrdinal := int32(2)
	cr.Status.LastStableReplicas = &replicas
	cr.Status.LifecycleOperation =
		&enterpriseApi.SearchHeadClusterLifecycleOperationStatus{
			Intent: enterpriseApi.SearchHeadClusterLifecycleIntentPodUpdate,
			Stage: enterpriseApi.
				SearchHeadClusterLifecycleStageWaitingForContainer,
			TargetOrdinal: &targetOrdinal,
			TargetPod:     "splunk-stack1-search-head-2",
		}
	cr.Spec.PodAnnotations = map[string]string{
		shcDeployerCoordinationAnnotation: "queued",
	}

	// Later Search Head reconciliation can wait or fail in this deliberately
	// incomplete fixture. The assertion boundary is that the active member
	// owner prevented the earlier Deployer manager from applying its queued
	// template or replacing its Pod.
	_, _ = ApplySearchHeadCluster(ctx, controllerClient, cr)

	stored := &appsv1.StatefulSet{}
	if err := controllerClient.Get(
		ctx,
		client.ObjectKeyFromObject(deployer),
		stored,
	); err != nil {
		t.Fatalf("get Deployer StatefulSet: %v", err)
	}
	if _, found := stored.Spec.Template.Annotations[shcDeployerCoordinationAnnotation]; found {
		t.Fatal("active Search Head lifecycle applied queued Deployer template")
	}
	storedCR := &enterpriseApi.SearchHeadCluster{}
	if err := controllerClient.Get(
		ctx,
		client.ObjectKeyFromObject(cr),
		storedCR,
	); err != nil {
		t.Fatalf("get persisted SearchHeadCluster status: %v", err)
	}
	if storedCR.Status.DeployerPhase != enterpriseApi.PhaseReady {
		t.Fatalf(
			"Deployer phase = %q, want %q while update is deferred",
			storedCR.Status.DeployerPhase,
			enterpriseApi.PhaseReady,
		)
	}
	observedPod := &corev1.Pod{}
	if err := controllerClient.Get(
		ctx,
		client.ObjectKeyFromObject(deployerPod),
		observedPod,
	); err != nil {
		t.Fatalf("get stable Deployer Pod: %v", err)
	}
	if observedPod.UID != deployerPod.UID ||
		observedPod.DeletionTimestamp != nil {
		t.Fatalf(
			"stable Deployer Pod changed while member lifecycle owned disruption: uid=%q deleting=%v",
			observedPod.UID,
			observedPod.DeletionTimestamp,
		)
	}
}

func TestApplySearchHeadClusterWaitsForActiveDeployerBeforeSearchHeadMutation(
	t *testing.T,
) {
	ctx, controllerClient, cr := newSHCDeployerCoordinationFixture(t)
	deployer, _ := makeSHCDeployerStable(
		t,
		ctx,
		controllerClient,
		cr,
		"deployer-revision-b",
	)

	deployer.Status.UpdateRevision = "deployer-revision-c"
	if err := controllerClient.Status().Update(ctx, deployer); err != nil {
		t.Fatalf("mark Deployer update active: %v", err)
	}

	searchHeadKey := types.NamespacedName{
		Namespace: cr.Namespace,
		Name:      GetSplunkStatefulsetName(SplunkSearchHead, cr.Name),
	}
	searchHead := &appsv1.StatefulSet{}
	if err := controllerClient.Get(ctx, searchHeadKey, searchHead); err != nil {
		t.Fatalf("get initial Search Head StatefulSet: %v", err)
	}
	if err := controllerClient.Delete(ctx, searchHead); err != nil {
		t.Fatalf("delete Search Head StatefulSet fixture: %v", err)
	}

	replicas := int32(3)
	cr.Status.LastStableReplicas = &replicas
	cr.Status.LifecycleOperation = nil
	cr.Spec.PodAnnotations = map[string]string{
		shcDeployerCoordinationAnnotation: "pending-behind-deployer",
	}

	if _, err := ApplySearchHeadCluster(ctx, controllerClient, cr); err != nil {
		t.Fatalf("reconcile active Deployer update: %v", err)
	}
	if err := controllerClient.Get(
		ctx,
		searchHeadKey,
		&appsv1.StatefulSet{},
	); !k8serrors.IsNotFound(err) {
		t.Fatalf(
			"Search Head StatefulSet read after active Deployer update = %v, want NotFound",
			err,
		)
	}
	storedCR := &enterpriseApi.SearchHeadCluster{}
	if err := controllerClient.Get(
		ctx,
		client.ObjectKeyFromObject(cr),
		storedCR,
	); err != nil {
		t.Fatalf("get persisted SearchHeadCluster status: %v", err)
	}
	if storedCR.Status.DeployerPhase == enterpriseApi.PhaseReady {
		t.Fatalf(
			"Deployer phase = %q while Pod revision has not converged",
			storedCR.Status.DeployerPhase,
		)
	}
	if storedCR.Status.Message !=
		"SHC RollingUpdate DeployerUpdateActive: waiting for the Deployer Pod update to complete before changing Search Head Pods" {
		t.Fatalf(
			"persisted coordination message = %q",
			storedCR.Status.Message,
		)
	}
}

func newSHCDeployerCoordinationFixture(
	t *testing.T,
) (context.Context, client.Client, *enterpriseApi.SearchHeadCluster) {
	t.Helper()
	setLifecyclePolicyTestGates(t, true, true)
	t.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")
	oldGetMemberInfo := GetSearchHeadClusterMemberInfo
	oldGetCaptainInfo := GetSearchHeadCaptainInfo
	GetSearchHeadClusterMemberInfo = func(
		context.Context,
		*searchHeadClusterPodManager,
		int32,
	) (*splclient.SearchHeadClusterMemberInfo, error) {
		return &splclient.SearchHeadClusterMemberInfo{
			Status:     "Up",
			Registered: true,
		}, nil
	}
	GetSearchHeadCaptainInfo = func(
		context.Context,
		*searchHeadClusterPodManager,
		int32,
	) (*splclient.SearchHeadCaptainInfo, error) {
		return &splclient.SearchHeadCaptainInfo{
			Label:          "splunk-stack1-search-head-0",
			ServiceReady:   true,
			Initialized:    true,
			MinPeersJoined: true,
		}, nil
	}
	t.Cleanup(func() {
		GetSearchHeadClusterMemberInfo = oldGetMemberInfo
		GetSearchHeadCaptainInfo = oldGetCaptainInfo
	})

	scheme := pkgruntime.NewScheme()
	utilruntime.Must(enterpriseApi.AddToScheme(scheme))
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	controllerClient := newFakeClientBuilder(scheme).
		WithStatusSubresource(
			&enterpriseApi.SearchHeadCluster{},
			&appsv1.StatefulSet{},
			&corev1.Pod{},
		).
		Build()
	ctx := context.Background()
	cr := &enterpriseApi.SearchHeadCluster{
		TypeMeta: metav1.TypeMeta{
			Kind:       "SearchHeadCluster",
			APIVersion: "enterprise.splunk.com/v4",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
		Spec: enterpriseApi.SearchHeadClusterSpec{Replicas: 3},
	}
	if err := controllerClient.Create(ctx, cr); err != nil {
		t.Fatalf("create SearchHeadCluster: %v", err)
	}
	if _, err := ApplySearchHeadCluster(ctx, controllerClient, cr); err != nil {
		t.Fatalf("create initial SHC resources: %v", err)
	}
	return ctx, controllerClient, cr
}

func makeSHCDeployerStable(
	t *testing.T,
	ctx context.Context,
	controllerClient client.Client,
	cr *enterpriseApi.SearchHeadCluster,
	revision string,
) (*appsv1.StatefulSet, *corev1.Pod) {
	t.Helper()
	key := types.NamespacedName{
		Namespace: cr.Namespace,
		Name:      GetSplunkStatefulsetName(SplunkDeployer, cr.Name),
	}
	deployer := &appsv1.StatefulSet{}
	if err := controllerClient.Get(ctx, key, deployer); err != nil {
		t.Fatalf("get initial Deployer StatefulSet: %v", err)
	}
	deployer.Status = appsv1.StatefulSetStatus{
		ObservedGeneration: deployer.Generation,
		Replicas:           1,
		ReadyReplicas:      1,
		CurrentReplicas:    1,
		UpdatedReplicas:    1,
		CurrentRevision:    revision,
		UpdateRevision:     revision,
	}
	if err := controllerClient.Status().Update(ctx, deployer); err != nil {
		t.Fatalf("mark Deployer StatefulSet stable: %v", err)
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      key.Name + "-0",
			Namespace: key.Namespace,
			UID:       types.UID("stable-deployer-uid"),
			Labels: map[string]string{
				"controller-revision-hash": revision,
			},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			Conditions: []corev1.PodCondition{
				{Type: corev1.PodReady, Status: corev1.ConditionTrue},
			},
			ContainerStatuses: []corev1.ContainerStatus{
				{Name: "splunk", Ready: true},
			},
		},
	}
	if err := controllerClient.Create(ctx, pod); err != nil {
		t.Fatalf("create stable Deployer Pod: %v", err)
	}
	if err := controllerClient.Status().Update(ctx, pod); err != nil {
		t.Fatalf("mark Deployer Pod stable: %v", err)
	}
	return deployer, pod
}
