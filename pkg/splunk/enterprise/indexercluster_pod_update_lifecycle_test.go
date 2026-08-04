// Copyright (c) 2026 Splunk Inc. All rights reserved.

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
	"testing"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/splunk/splunk-operator/pkg/config"
	"github.com/splunk/splunk-operator/pkg/logging"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	spltest "github.com/splunk/splunk-operator/pkg/splunk/test"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func enableIndexerLifecycleForTest(t *testing.T) {
	t.Helper()
	oldSearchPeerCheck := checkIndexerSearchPeerConvergence
	oldPodLifecycle :=
		config.DefaultMutableFeatureGate.Enabled(config.SplunkPodLifecycle)
	oldIndexerLifecycle :=
		config.DefaultMutableFeatureGate.Enabled(config.IndexerClusterLifecycle)
	t.Cleanup(func() {
		checkIndexerSearchPeerConvergence = oldSearchPeerCheck
		require.NoError(t, config.DefaultMutableFeatureGate.SetFromMap(
			map[string]bool{
				string(config.SplunkPodLifecycle):      oldPodLifecycle,
				string(config.IndexerClusterLifecycle): oldIndexerLifecycle,
			},
		))
	})
	checkIndexerSearchPeerConvergence = func(
		_ context.Context,
		_ *indexerClusterPodManager,
		_ *corev1.Pod,
	) (bool, bool, string, error) {
		return false, true, "No dependent SearchHeadCluster", nil
	}
	require.NoError(t, config.DefaultMutableFeatureGate.SetFromMap(
		map[string]bool{
			string(config.SplunkPodLifecycle):      true,
			string(config.IndexerClusterLifecycle): true,
		},
	))
}

func allowIndexerServingRecoveryForTest(t *testing.T) {
	t.Helper()
	oldCheck := checkIndexerServingRecovery
	t.Cleanup(func() {
		checkIndexerServingRecovery = oldCheck
	})
	checkIndexerServingRecovery = func(
		_ context.Context,
		_ *indexerClusterPodManager,
		_ *corev1.Pod,
	) (bool, error) {
		return true, nil
	}
}

func indexerLifecycleFixture(
	t *testing.T,
) (
	*indexerClusterPodManager,
	*appsv1.StatefulSet,
	[]*corev1.Pod,
) {
	t.Helper()
	replicas := int32(3)
	statefulSet := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-example-indexer",
			Namespace: "test",
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &replicas,
			UpdateStrategy: appsv1.StatefulSetUpdateStrategy{
				Type: appsv1.OnDeleteStatefulSetStrategyType,
			},
		},
		Status: appsv1.StatefulSetStatus{
			Replicas:        replicas,
			ReadyReplicas:   replicas,
			CurrentRevision: "old",
			UpdateRevision:  "new",
		},
	}
	pods := make([]*corev1.Pod, 0, replicas)
	objects := []client.Object{statefulSet}
	peers := make([]enterpriseApi.IndexerClusterMemberStatus, 0, replicas)
	for ordinal := int32(0); ordinal < replicas; ordinal++ {
		name := GetSplunkStatefulsetPodName(
			SplunkIndexer,
			"example",
			ordinal,
		)
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: "test",
				UID:       types.UID(name + "-uid"),
				Labels: map[string]string{
					"controller-revision-hash": "old",
				},
			},
			Status: corev1.PodStatus{
				Phase: corev1.PodRunning,
				Conditions: []corev1.PodCondition{{
					Type:   corev1.PodReady,
					Status: corev1.ConditionTrue,
				}},
				ContainerStatuses: []corev1.ContainerStatus{{
					Ready: true,
				}},
			},
		}
		pods = append(pods, pod)
		objects = append(objects, pod)
		peers = append(peers, enterpriseApi.IndexerClusterMemberStatus{
			Name:       name,
			Status:     "Up",
			Searchable: true,
		})
	}
	fakeClient := spltest.NewMockClient()
	fakeClient.AddObjects(objects)
	cr := &enterpriseApi.IndexerCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "example",
			Namespace: "test",
		},
		Spec: enterpriseApi.IndexerClusterSpec{
			Replicas: replicas,
		},
		Status: enterpriseApi.IndexerClusterStatus{
			Peers: peers,
		},
	}
	mgr := &indexerClusterPodManager{
		c:   fakeClient,
		log: logging.FromContext(context.Background()),
		cr:  cr,
	}
	return mgr, statefulSet, pods
}

func TestIndexerPodUpdateIntentPrecedesDecommission(t *testing.T) {
	enableIndexerLifecycleForTest(t)
	mgr, statefulSet, pods := indexerLifecycleFixture(t)
	target := pods[2]

	persisted, err := mgr.EnsurePodUpdateIntent(
		context.Background(),
		statefulSet,
		target,
		2,
	)
	require.NoError(t, err)
	if persisted {
		t.Fatal("new target intent reported persisted in the selecting reconcile")
	}
	operation := mgr.cr.Status.PodUpdate
	if operation == nil ||
		operation.Stage !=
			enterpriseApi.IndexerClusterPodUpdateStageTargetSelected ||
		operation.TargetPodUID != string(target.UID) ||
		operation.SourceRevision != "old" ||
		operation.DesiredRevision != "new" {
		t.Fatalf("unexpected Pod update operation: %#v", operation)
	}

	persisted, err = mgr.EnsurePodUpdateIntent(
		context.Background(),
		statefulSet,
		target,
		2,
	)
	require.NoError(t, err)
	if !persisted {
		t.Fatal("matching durable target intent was not accepted")
	}
}

func TestIndexerLifecycleRendersHECServingReadiness(t *testing.T) {
	enableIndexerLifecycleForTest(t)
	t.Setenv(
		"SPLUNK_GENERAL_TERMS",
		"--accept-sgt-current-at-splunk-com",
	)
	ctx := context.Background()
	cr := &enterpriseApi.IndexerCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "example",
			Namespace: "test",
		},
		Spec: enterpriseApi.IndexerClusterSpec{
			Replicas: 3,
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				ClusterManagerRef: corev1.ObjectReference{
					Name: "manager",
				},
				ReadinessProbe: &enterpriseApi.Probe{
					InitialDelaySeconds: 10,
					TimeoutSeconds:      5,
					PeriodSeconds:       5,
					FailureThreshold:    3,
				},
			},
		},
	}
	c := spltest.NewMockClient()
	_, err := splutil.ApplyNamespaceScopedSecretObject(ctx, c, cr.Namespace)
	require.NoError(t, err)
	c.AddObject(&enterpriseApi.ClusterManager{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "manager",
			Namespace: cr.Namespace,
		},
	})
	require.NoError(t, validateIndexerClusterSpec(ctx, c, cr))

	statefulSet, err := getIndexerStatefulSet(ctx, c, cr)
	require.NoError(t, err)
	container := statefulSet.Spec.Template.Spec.Containers[0]
	found := false
	stableSearchAddressCount := 0
	for _, env := range container.Env {
		if env.Name == indexerServingReadinessEnv {
			found = true
			if env.Value != "true" {
				t.Fatalf("%s = %q, want true", env.Name, env.Value)
			}
		}
		if env.Name == indexerRegisterSearchAddressEnv {
			stableSearchAddressCount++
		}
	}
	if !found {
		t.Fatalf("%s was not rendered", indexerServingReadinessEnv)
	}
	require.Zero(t, stableSearchAddressCount,
		"lifecycle enablement must not migrate an existing cluster's search-peer addresses")
	if container.ReadinessProbe == nil ||
		container.ReadinessProbe.TimeoutSeconds != 2 ||
		container.ReadinessProbe.PeriodSeconds != 2 ||
		container.ReadinessProbe.FailureThreshold != 1 {
		t.Fatalf(
			"unexpected Indexer lifecycle readiness: %#v",
			container.ReadinessProbe,
		)
	}

	cr.Spec.ReadinessProbe = &enterpriseApi.Probe{
		InitialDelaySeconds: 7,
		TimeoutSeconds:      4,
		PeriodSeconds:       6,
		FailureThreshold:    2,
	}
	statefulSet, err = getIndexerStatefulSet(ctx, c, cr)
	require.NoError(t, err)
	container = statefulSet.Spec.Template.Spec.Containers[0]
	require.Equal(t, int32(7), container.ReadinessProbe.InitialDelaySeconds)
	require.Equal(t, int32(4), container.ReadinessProbe.TimeoutSeconds)
	require.Equal(t, int32(6), container.ReadinessProbe.PeriodSeconds)
	require.Equal(t, int32(2), container.ReadinessProbe.FailureThreshold)

	cr.Spec.ExtraEnv = []corev1.EnvVar{{
		Name:  indexerRegisterSearchAddressEnv,
		Value: "customer-indexer.example",
	}}
	statefulSet, err = getIndexerStatefulSet(ctx, c, cr)
	require.NoError(t, err)
	stableSearchAddressCount = 0
	for _, env := range statefulSet.Spec.Template.Spec.Containers[0].Env {
		if env.Name == indexerRegisterSearchAddressEnv {
			stableSearchAddressCount++
			require.Equal(t, "customer-indexer.example", env.Value)
		}
	}
	require.Equal(t, 1, stableSearchAddressCount)
}

func TestIndexerPodUpdateAdoptsNewRevisionBeforeDecommission(t *testing.T) {
	enableIndexerLifecycleForTest(t)
	mgr, statefulSet, pods := indexerLifecycleFixture(t)
	target := pods[2]

	persisted, err := mgr.EnsurePodUpdateIntent(
		context.Background(),
		statefulSet,
		target,
		2,
	)
	require.NoError(t, err)
	if persisted {
		t.Fatal("new target intent reported persisted in selecting reconcile")
	}

	statefulSet.Status.UpdateRevision = "newer"
	persisted, err = mgr.EnsurePodUpdateIntent(
		context.Background(),
		statefulSet,
		target,
		2,
	)
	require.NoError(t, err)
	if persisted {
		t.Fatal("new desired revision advanced in the same reconcile")
	}
	if mgr.cr.Status.PodUpdate.DesiredRevision != "newer" {
		t.Fatalf(
			"desired revision = %s, want newer",
			mgr.cr.Status.PodUpdate.DesiredRevision,
		)
	}

	persisted, err = mgr.EnsurePodUpdateIntent(
		context.Background(),
		statefulSet,
		target,
		2,
	)
	require.NoError(t, err)
	if !persisted {
		t.Fatal("persisted newer desired revision was not accepted")
	}
}

func TestIndexerUntouchedTargetCancelsWhenRevisionReturnsToSource(
	t *testing.T,
) {
	enableIndexerLifecycleForTest(t)
	mgr, statefulSet, pods := indexerLifecycleFixture(t)
	target := pods[2]
	now := metav1.Now()
	mgr.cr.Status.PodUpdate =
		&enterpriseApi.IndexerClusterPodUpdateStatus{
			OperationID:        "operation",
			Stage:              enterpriseApi.IndexerClusterPodUpdateStageTargetSelected,
			TargetPod:          target.Name,
			TargetPodUID:       string(target.UID),
			TargetOrdinal:      2,
			SourceRevision:     "old",
			DesiredRevision:    "new",
			StartedAt:          &now,
			StageStartedAt:     &now,
			LastTransitionTime: &now,
		}
	statefulSet.Status.UpdateRevision = "old"
	require.NoError(t, mgr.c.Update(context.Background(), statefulSet))

	complete, err := mgr.FinishRecycle(context.Background(), 2)
	require.NoError(t, err)
	if complete ||
		mgr.cr.Status.PodUpdate.Stage !=
			enterpriseApi.IndexerClusterPodUpdateStageCancelled ||
		mgr.cr.Status.PodUpdate.FinishedAt == nil {
		t.Fatalf(
			"unexpected cancellation result: complete=%t operation=%#v",
			complete,
			mgr.cr.Status.PodUpdate,
		)
	}
	complete, err = mgr.FinishRecycle(context.Background(), 2)
	require.NoError(t, err)
	if !complete {
		t.Fatal("persisted cancellation was not accepted")
	}

	cancelledOperationID := mgr.cr.Status.PodUpdate.OperationID
	statefulSet.Status.UpdateRevision = "new"
	persisted, err := mgr.EnsurePodUpdateIntent(
		context.Background(),
		statefulSet,
		target,
		2,
	)
	require.NoError(t, err)
	if persisted ||
		mgr.cr.Status.PodUpdate.OperationID == cancelledOperationID ||
		mgr.cr.Status.PodUpdate.Stage !=
			enterpriseApi.IndexerClusterPodUpdateStageTargetSelected {
		t.Fatalf(
			"same revision was not started as a new operation: %#v",
			mgr.cr.Status.PodUpdate,
		)
	}
}

func TestIndexerDisruptedTargetRequiresReplacementAfterRevisionRollback(
	t *testing.T,
) {
	enableIndexerLifecycleForTest(t)
	mgr, statefulSet, pods := indexerLifecycleFixture(t)
	target := pods[2]
	now := metav1.Now()
	operation := &enterpriseApi.IndexerClusterPodUpdateStatus{
		OperationID:        "operation",
		Stage:              enterpriseApi.IndexerClusterPodUpdateStageWithdrawingReadiness,
		TargetPod:          target.Name,
		TargetPodUID:       string(target.UID),
		TargetOrdinal:      2,
		SourceRevision:     "old",
		DesiredRevision:    "new",
		StartedAt:          &now,
		StageStartedAt:     &now,
		LastTransitionTime: &now,
	}
	mgr.cr.Status.PodUpdate = operation
	statefulSet.Status.UpdateRevision = "old"

	required, err := mgr.RequiresOwnedPodReplacement(
		context.Background(),
		statefulSet,
		target,
		2,
	)
	require.NoError(t, err)
	if !required {
		t.Fatal("disrupted same-revision target replacement was abandoned")
	}

	operation.Stage =
		enterpriseApi.IndexerClusterPodUpdateStageTargetSelected
	required, err = mgr.RequiresOwnedPodReplacement(
		context.Background(),
		statefulSet,
		target,
		2,
	)
	require.NoError(t, err)
	if required {
		t.Fatal("untouched target was unnecessarily forced through replacement")
	}
}

func TestIndexerOwnedUnavailableTargetValidation(t *testing.T) {
	enableIndexerLifecycleForTest(t)
	mgr, statefulSet, pods := indexerLifecycleFixture(t)
	target := pods[2]
	now := metav1.Now()
	mgr.cr.Status.PodUpdate =
		&enterpriseApi.IndexerClusterPodUpdateStatus{
			OperationID:             "operation",
			Stage:                   enterpriseApi.IndexerClusterPodUpdateStageDecommissioning,
			TargetPod:               target.Name,
			TargetPodUID:            string(target.UID),
			TargetOrdinal:           2,
			SourceRevision:          "old",
			DesiredRevision:         "new",
			StartedAt:               &now,
			DecommissionRequestedAt: &now,
			ObservedDecommissioning: true,
			LastTransitionTime:      &now,
		}
	mgr.cr.Status.Peers[2].Status = "ReassigningPrimaries"
	target.Status.Conditions[0].Status = corev1.ConditionFalse
	target.Status.ContainerStatuses[0].Ready = false
	statefulSet.Status.ReadyReplicas = 2
	require.NoError(t, mgr.c.Update(context.Background(), target))
	require.NoError(t, mgr.c.Update(context.Background(), statefulSet))

	allowed, err := mgr.CanProceedWithPodUpdateDespiteNotReadyReplicas(
		context.Background(),
		statefulSet,
		3,
	)
	require.NoError(t, err)
	if !allowed {
		t.Fatal("exact owned unavailable target was rejected")
	}
	allowed, err = mgr.CanProceedWithUnavailablePodUpdate(
		context.Background(),
		statefulSet,
		target,
		2,
	)
	require.NoError(t, err)
	if !allowed {
		t.Fatal("per-Pod owned unavailable target was rejected")
	}

	statefulSet.Status.UpdateRevision = "newer"
	require.NoError(t, mgr.c.Update(context.Background(), statefulSet))
	allowed, err = mgr.CanProceedWithPodUpdateDespiteNotReadyReplicas(
		context.Background(),
		statefulSet,
		3,
	)
	require.NoError(t, err)
	if allowed {
		t.Fatal("new desired revision advanced in the same reconcile")
	}
	if mgr.cr.Status.PodUpdate.DesiredRevision != "newer" {
		t.Fatalf(
			"desired revision = %s, want newer",
			mgr.cr.Status.PodUpdate.DesiredRevision,
		)
	}
	allowed, err = mgr.CanProceedWithPodUpdateDespiteNotReadyReplicas(
		context.Background(),
		statefulSet,
		3,
	)
	require.NoError(t, err)
	if !allowed {
		t.Fatal("persisted newer desired revision was not accepted")
	}

	pods[1].Status.Conditions[0].Status = corev1.ConditionFalse
	require.NoError(t, mgr.c.Update(context.Background(), pods[1]))
	allowed, err = mgr.CanProceedWithPodUpdateDespiteNotReadyReplicas(
		context.Background(),
		statefulSet,
		3,
	)
	require.NoError(t, err)
	if allowed {
		t.Fatal("second unrelated unavailable Pod was accepted")
	}
}

func TestIndexerOwnedReadinessWithdrawalCanReachDecommission(t *testing.T) {
	enableIndexerLifecycleForTest(t)
	mgr, statefulSet, pods := indexerLifecycleFixture(t)
	target := pods[2]
	now := metav1.Now()
	mgr.cr.Status.PodUpdate =
		&enterpriseApi.IndexerClusterPodUpdateStatus{
			OperationID:        "operation",
			Stage:              enterpriseApi.IndexerClusterPodUpdateStageWithdrawingReadiness,
			TargetPod:          target.Name,
			TargetPodUID:       string(target.UID),
			TargetOrdinal:      2,
			SourceRevision:     "old",
			DesiredRevision:    "new",
			StartedAt:          &now,
			StageStartedAt:     &now,
			LastTransitionTime: &now,
		}
	target.Status.Conditions[0].Status = corev1.ConditionFalse
	target.Status.ContainerStatuses[0].Ready = false
	statefulSet.Status.ReadyReplicas = 2
	require.NoError(t, mgr.c.Update(context.Background(), target))
	require.NoError(t, mgr.c.Update(context.Background(), statefulSet))

	allowed, err := mgr.CanProceedWithPodUpdateDespiteNotReadyReplicas(
		context.Background(),
		statefulSet,
		3,
	)
	require.NoError(t, err)
	if !allowed {
		t.Fatal("owned readiness withdrawal could not reach decommission")
	}
	allowed, err = mgr.CanProceedWithUnavailablePodUpdate(
		context.Background(),
		statefulSet,
		target,
		2,
	)
	require.NoError(t, err)
	if !allowed {
		t.Fatal("per-Pod readiness withdrawal ownership was rejected")
	}
}

func TestIndexerRestartingTargetPersistsReplacementAuthorization(t *testing.T) {
	enableIndexerLifecycleForTest(t)
	mgr, _, pods := indexerLifecycleFixture(t)
	target := pods[2]
	now := metav1.Now()
	mgr.cr.Status.PodUpdate =
		&enterpriseApi.IndexerClusterPodUpdateStatus{
			OperationID:             "operation",
			Stage:                   enterpriseApi.IndexerClusterPodUpdateStageDecommissioning,
			TargetPod:               target.Name,
			TargetPodUID:            string(target.UID),
			TargetOrdinal:           2,
			SourceRevision:          "old",
			DesiredRevision:         "new",
			StartedAt:               &now,
			DecommissionRequestedAt: &now,
			ObservedDecommissioning: true,
			LastTransitionTime:      &now,
		}
	mgr.cr.Status.Peers[2].Status = "Restarting"

	ready, err := mgr.PrepareRecycle(context.Background(), 2)
	require.NoError(t, err)
	if ready {
		t.Fatal("target was authorized for deletion before stage persisted")
	}
	if mgr.cr.Status.PodUpdate.Stage !=
		enterpriseApi.IndexerClusterPodUpdateStageReadyForReplacement {
		t.Fatalf(
			"stage = %s, want ReadyForReplacement",
			mgr.cr.Status.PodUpdate.Stage,
		)
	}

	ready, err = mgr.PrepareRecycle(context.Background(), 2)
	require.NoError(t, err)
	if !ready {
		t.Fatal("persisted replacement authorization was not accepted")
	}
}

func TestIndexerRecoversDecommissionAcceptedBeforeStatusPersisted(
	t *testing.T,
) {
	enableIndexerLifecycleForTest(t)
	mgr, _, pods := indexerLifecycleFixture(t)
	target := pods[2]
	now := metav1.Now()
	mgr.cr.Status.PodUpdate =
		&enterpriseApi.IndexerClusterPodUpdateStatus{
			OperationID:        "operation",
			Stage:              enterpriseApi.IndexerClusterPodUpdateStageTargetSelected,
			TargetPod:          target.Name,
			TargetPodUID:       string(target.UID),
			TargetOrdinal:      2,
			SourceRevision:     "old",
			DesiredRevision:    "new",
			StartedAt:          &now,
			StageStartedAt:     &now,
			LastTransitionTime: &now,
		}
	mgr.cr.Status.Peers[2].Status = "Decommissioning"

	ready, err := mgr.PrepareRecycle(context.Background(), 2)
	require.NoError(t, err)
	if ready {
		t.Fatal("recovered decommission authorized replacement immediately")
	}
	operation := mgr.cr.Status.PodUpdate
	if operation.Stage !=
		enterpriseApi.IndexerClusterPodUpdateStageDecommissioning ||
		operation.DecommissionRequestedAt == nil ||
		!operation.ObservedDecommissioning ||
		operation.Reason != "IndexerDecommissionRecovered" {
		t.Fatalf("unexpected recovered operation: %#v", operation)
	}
}

func TestIndexerPersistsReadinessWithdrawalBeforePodMutation(t *testing.T) {
	enableIndexerLifecycleForTest(t)
	mgr, _, pods := indexerLifecycleFixture(t)
	target := pods[2]
	now := metav1.Now()
	mgr.cr.Status.PodUpdate =
		&enterpriseApi.IndexerClusterPodUpdateStatus{
			OperationID:        "operation",
			Stage:              enterpriseApi.IndexerClusterPodUpdateStageTargetSelected,
			TargetPod:          target.Name,
			TargetPodUID:       string(target.UID),
			TargetOrdinal:      2,
			SourceRevision:     "old",
			DesiredRevision:    "new",
			StartedAt:          &now,
			StageStartedAt:     &now,
			LastTransitionTime: &now,
		}
	mockExec := &spltest.MockPodExecClient{
		Client: mgr.c,
		Cr:     mgr.cr,
	}
	mockExec.AddMockPodExecReturnContext(
		context.Background(),
		"SPLUNK_OPERATOR_INDEXER_SERVING_READINESS",
		&spltest.MockPodExecReturnContext{},
	)
	oldGetPodExecClient := splutil.GetPodExecClient
	t.Cleanup(func() {
		splutil.GetPodExecClient = oldGetPodExecClient
	})
	splutil.GetPodExecClient = func(
		_ splcommon.ControllerClient,
		_ splcommon.MetaObject,
		targetPodName string,
	) splutil.PodExecClientImpl {
		mockExec.TargetPodName = targetPodName
		return mockExec
	}

	ready, err := mgr.PrepareRecycle(context.Background(), 2)
	require.NoError(t, err)
	if ready ||
		mgr.cr.Status.PodUpdate.Stage !=
			enterpriseApi.IndexerClusterPodUpdateStageWithdrawingReadiness ||
		mgr.cr.Status.PodUpdate.DecommissionRequestedAt != nil {
		t.Fatalf(
			"unexpected readiness-withdrawal transition: ready=%t operation=%#v",
			ready,
			mgr.cr.Status.PodUpdate,
		)
	}
	if len(mockExec.GotCmdList) != 0 {
		t.Fatalf(
			"Pod was mutated before withdrawal stage persisted: %v",
			mockExec.GotCmdList,
		)
	}

	ready, err = mgr.PrepareRecycle(context.Background(), 2)
	require.NoError(t, err)
	if ready || len(mockExec.GotCmdList) != 1 {
		t.Fatalf(
			"persisted withdrawal did not set the Pod signal: ready=%t commands=%v",
			ready,
			mockExec.GotCmdList,
		)
	}
	if mgr.cr.Status.PodUpdate.DecommissionRequestedAt != nil {
		t.Fatal("decommission began before Kubernetes readiness was withdrawn")
	}
}

func TestIndexerReplacementRequiresReadyUpAndSearchable(t *testing.T) {
	enableIndexerLifecycleForTest(t)
	allowIndexerServingRecoveryForTest(t)
	mgr, _, pods := indexerLifecycleFixture(t)
	target := pods[2]
	now := metav1.Now()
	mgr.cr.Status.PodUpdate =
		&enterpriseApi.IndexerClusterPodUpdateStatus{
			OperationID:               "operation",
			Stage:                     enterpriseApi.IndexerClusterPodUpdateStageReadyForReplacement,
			TargetPod:                 target.Name,
			TargetPodUID:              string(target.UID),
			TargetOrdinal:             2,
			SourceRevision:            "old",
			DesiredRevision:           "new",
			StartedAt:                 &now,
			DecommissionRequestedAt:   &now,
			ObservedDecommissioning:   true,
			LastTransitionTime:        &now,
			ServingRecoveryObservedAt: &now,
		}
	replacement := target.DeepCopy()
	replacement.UID = types.UID("replacement-uid")
	replacement.Labels["controller-revision-hash"] = "new"
	require.NoError(t, mgr.c.Update(context.Background(), replacement))
	mgr.cr.Status.PodUpdate.ServingRecoveryPodUID =
		string(replacement.UID)

	targetPeer := mgr.cr.Status.Peers[2]
	mgr.cr.Status.Peers = mgr.cr.Status.Peers[:2]
	complete, err := mgr.FinishRecycle(context.Background(), 2)
	require.NoError(t, err)
	if complete {
		t.Fatal("replacement completed while the target peer was absent")
	}
	mgr.cr.Status.Peers = append(mgr.cr.Status.Peers, targetPeer)

	mgr.cr.Status.Peers[2].Searchable = false
	complete, err = mgr.FinishRecycle(context.Background(), 2)
	require.NoError(t, err)
	if complete {
		t.Fatal("non-searchable replacement completed")
	}

	mgr.cr.Status.Peers[2].Searchable = true
	complete, err = mgr.FinishRecycle(context.Background(), 2)
	require.NoError(t, err)
	if complete ||
		mgr.cr.Status.PodUpdate.Stage !=
			enterpriseApi.IndexerClusterPodUpdateStageCompleted ||
		mgr.cr.Status.PodUpdate.ReplacementPodUID !=
			string(replacement.UID) ||
		mgr.cr.Status.PodUpdate.FinishedAt == nil {
		t.Fatalf(
			"unpersisted replacement completion = %v, stage = %s",
			complete,
			mgr.cr.Status.PodUpdate.Stage,
		)
	}
	complete, err = mgr.FinishRecycle(context.Background(), 2)
	require.NoError(t, err)
	if !complete {
		t.Fatal("persisted replacement completion was not accepted")
	}
}

func TestIndexerReplacementRevalidatesPersistedServingRecovery(t *testing.T) {
	enableIndexerLifecycleForTest(t)
	mgr, _, pods := indexerLifecycleFixture(t)
	target := pods[2]
	now := metav1.Now()
	mgr.cr.Status.PodUpdate =
		&enterpriseApi.IndexerClusterPodUpdateStatus{
			OperationID:             "operation",
			Stage:                   enterpriseApi.IndexerClusterPodUpdateStageReadyForReplacement,
			TargetPod:               target.Name,
			TargetPodUID:            string(target.UID),
			TargetOrdinal:           2,
			SourceRevision:          "old",
			DesiredRevision:         "new",
			StartedAt:               &now,
			DecommissionRequestedAt: &now,
			ObservedDecommissioning: true,
			LastTransitionTime:      &now,
		}
	replacement := target.DeepCopy()
	replacement.UID = types.UID("replacement-uid")
	replacement.Labels["controller-revision-hash"] = "new"
	require.NoError(t, mgr.c.Update(context.Background(), replacement))

	serving := true
	oldCheck := checkIndexerServingRecovery
	t.Cleanup(func() {
		checkIndexerServingRecovery = oldCheck
	})
	checkIndexerServingRecovery = func(
		_ context.Context,
		_ *indexerClusterPodManager,
		_ *corev1.Pod,
	) (bool, error) {
		return serving, nil
	}

	complete, err := mgr.FinishRecycle(context.Background(), 2)
	require.NoError(t, err)
	require.False(t, complete)
	require.NotNil(t, mgr.cr.Status.PodUpdate.ServingRecoveryObservedAt)
	require.Equal(
		t,
		string(replacement.UID),
		mgr.cr.Status.PodUpdate.ServingRecoveryPodUID,
	)
	require.Equal(
		t,
		int64(1),
		mgr.cr.Status.PodUpdate.ServingRecoverySequence,
	)
	require.Equal(
		t,
		enterpriseApi.IndexerClusterPodUpdateStageReadyForReplacement,
		mgr.cr.Status.PodUpdate.Stage,
	)

	replacement = replacement.DeepCopy()
	replacement.UID = types.UID("second-replacement-uid")
	require.NoError(t, mgr.c.Update(context.Background(), replacement))
	complete, err = mgr.FinishRecycle(context.Background(), 2)
	require.NoError(t, err)
	require.False(t, complete)
	require.Equal(
		t,
		string(replacement.UID),
		mgr.cr.Status.PodUpdate.ServingRecoveryPodUID,
	)
	require.Equal(
		t,
		int64(2),
		mgr.cr.Status.PodUpdate.ServingRecoverySequence,
	)
	require.Equal(
		t,
		enterpriseApi.IndexerClusterPodUpdateStageReadyForReplacement,
		mgr.cr.Status.PodUpdate.Stage,
	)

	serving = false
	complete, err = mgr.FinishRecycle(context.Background(), 2)
	require.NoError(t, err)
	require.False(t, complete)
	require.Equal(
		t,
		enterpriseApi.IndexerClusterPodUpdateStageReadyForReplacement,
		mgr.cr.Status.PodUpdate.Stage,
	)

	serving = true
	complete, err = mgr.FinishRecycle(context.Background(), 2)
	require.NoError(t, err)
	require.False(t, complete)
	require.Equal(
		t,
		enterpriseApi.IndexerClusterPodUpdateStageCompleted,
		mgr.cr.Status.PodUpdate.Stage,
	)
}

func TestIndexerReplacementWaitsForDurableSearchPeerConvergence(t *testing.T) {
	enableIndexerLifecycleForTest(t)
	allowIndexerServingRecoveryForTest(t)
	mgr, _, pods := indexerLifecycleFixture(t)
	target := pods[2]
	now := metav1.Now()
	mgr.cr.Status.PodUpdate =
		&enterpriseApi.IndexerClusterPodUpdateStatus{
			OperationID:               "operation",
			Stage:                     enterpriseApi.IndexerClusterPodUpdateStageReadyForReplacement,
			TargetPod:                 target.Name,
			TargetPodUID:              string(target.UID),
			TargetOrdinal:             2,
			SourceRevision:            "old",
			DesiredRevision:           "new",
			StartedAt:                 &now,
			DecommissionRequestedAt:   &now,
			ObservedDecommissioning:   true,
			LastTransitionTime:        &now,
			ServingRecoveryObservedAt: &now,
		}
	replacement := target.DeepCopy()
	replacement.UID = types.UID("replacement-uid")
	replacement.Labels["controller-revision-hash"] = "new"
	require.NoError(t, mgr.c.Update(context.Background(), replacement))
	mgr.cr.Status.PodUpdate.ServingRecoveryPodUID = string(replacement.UID)

	converged := false
	checkIndexerSearchPeerConvergence = func(
		_ context.Context,
		_ *indexerClusterPodManager,
		_ *corev1.Pod,
	) (bool, bool, string, error) {
		return true, converged, "test convergence observation", nil
	}

	complete, err := mgr.FinishRecycle(context.Background(), 2)
	require.NoError(t, err)
	require.False(t, complete)
	require.Equal(t, enterpriseApi.IndexerClusterPodUpdateStageAwaitingSearchPeerConvergence, mgr.cr.Status.PodUpdate.Stage)

	complete, err = mgr.FinishRecycle(context.Background(), 2)
	require.NoError(t, err)
	require.False(t, complete)
	require.Zero(t, mgr.cr.Status.PodUpdate.SearchPeerConvergenceSequence)

	converged = true
	complete, err = mgr.FinishRecycle(context.Background(), 2)
	require.NoError(t, err)
	require.False(t, complete)
	require.NotNil(t, mgr.cr.Status.PodUpdate.SearchPeerConvergenceObservedAt)
	require.Equal(t, string(replacement.UID), mgr.cr.Status.PodUpdate.SearchPeerConvergencePodUID)
	require.Equal(t, int64(1), mgr.cr.Status.PodUpdate.SearchPeerConvergenceSequence)

	converged = false
	complete, err = mgr.FinishRecycle(context.Background(), 2)
	require.NoError(t, err)
	require.False(t, complete)
	require.Equal(t, int64(1), mgr.cr.Status.PodUpdate.SearchPeerConvergenceInvalidatedSequence)
	require.NotNil(t, mgr.cr.Status.PodUpdate.SearchPeerConvergenceObservedAt)

	converged = true
	complete, err = mgr.FinishRecycle(context.Background(), 2)
	require.NoError(t, err)
	require.False(t, complete)
	require.Equal(t, int64(2), mgr.cr.Status.PodUpdate.SearchPeerConvergenceSequence)

	complete, err = mgr.FinishRecycle(context.Background(), 2)
	require.NoError(t, err)
	require.False(t, complete)
	require.Equal(t, enterpriseApi.IndexerClusterPodUpdateStageCompleted, mgr.cr.Status.PodUpdate.Stage)

	complete, err = mgr.FinishRecycle(context.Background(), 2)
	require.NoError(t, err)
	require.True(t, complete)
}

func TestIndexerReplacementAdoptsLatestStatefulSetRevision(t *testing.T) {
	enableIndexerLifecycleForTest(t)
	allowIndexerServingRecoveryForTest(t)
	mgr, statefulSet, pods := indexerLifecycleFixture(t)
	target := pods[2]
	now := metav1.Now()
	mgr.cr.Status.PodUpdate =
		&enterpriseApi.IndexerClusterPodUpdateStatus{
			OperationID:               "operation",
			Stage:                     enterpriseApi.IndexerClusterPodUpdateStageReadyForReplacement,
			TargetPod:                 target.Name,
			TargetPodUID:              string(target.UID),
			TargetOrdinal:             2,
			SourceRevision:            "old",
			DesiredRevision:           "new",
			StartedAt:                 &now,
			DecommissionRequestedAt:   &now,
			ObservedDecommissioning:   true,
			LastTransitionTime:        &now,
			ServingRecoveryObservedAt: &now,
		}
	statefulSet.Status.UpdateRevision = "newer"
	require.NoError(t, mgr.c.Update(context.Background(), statefulSet))
	replacement := target.DeepCopy()
	replacement.UID = types.UID("replacement-uid")
	replacement.Labels["controller-revision-hash"] = "newer"
	require.NoError(t, mgr.c.Update(context.Background(), replacement))
	mgr.cr.Status.PodUpdate.ServingRecoveryPodUID =
		string(replacement.UID)

	complete, err := mgr.FinishRecycle(context.Background(), 2)
	require.NoError(t, err)
	if complete {
		t.Fatal("replacement completed before latest revision was persisted")
	}
	if mgr.cr.Status.PodUpdate.DesiredRevision != "newer" {
		t.Fatalf(
			"desired revision = %s, want newer",
			mgr.cr.Status.PodUpdate.DesiredRevision,
		)
	}

	complete, err = mgr.FinishRecycle(context.Background(), 2)
	require.NoError(t, err)
	if complete ||
		mgr.cr.Status.PodUpdate.Stage !=
			enterpriseApi.IndexerClusterPodUpdateStageCompleted {
		t.Fatalf(
			"unpersisted replacement completion = %v, stage = %s",
			complete,
			mgr.cr.Status.PodUpdate.Stage,
		)
	}
	complete, err = mgr.FinishRecycle(context.Background(), 2)
	require.NoError(t, err)
	if !complete {
		t.Fatal("persisted replacement completion was not accepted")
	}
}

func TestIndexerLifecycleDisablesStatefulSetRecreationUpgradeFallback(
	t *testing.T,
) {
	enableIndexerLifecycleForTest(t)
	if shouldRecreateIndexerStatefulSetForUpgrade(
		"splunk/splunk:8.2.6",
		"splunk/splunk:9.0.0",
	) {
		t.Fatal(
			"durable lifecycle allowed the 8-to-9 StatefulSet recreation fallback",
		)
	}
}

func TestIndexerLifecycleDefersReplicaChangeUntilPodUpdateCompletes(
	t *testing.T,
) {
	enableIndexerLifecycleForTest(t)
	_, statefulSet, _ := indexerLifecycleFixture(t)
	operation := &enterpriseApi.IndexerClusterPodUpdateStatus{
		Stage: enterpriseApi.IndexerClusterPodUpdateStageDecommissioning,
	}
	if got := desiredIndexerReplicasDuringPodUpdate(
		statefulSet,
		1,
		operation,
	); got != 3 {
		t.Fatalf("active update desired replicas = %d, want 3", got)
	}

	operation.Stage = enterpriseApi.IndexerClusterPodUpdateStageCompleted
	if got := desiredIndexerReplicasDuringPodUpdate(
		statefulSet,
		1,
		operation,
	); got != 1 {
		t.Fatalf("completed update desired replicas = %d, want 1", got)
	}
}

func TestIndexerOwnedRecoveryRequiresAllNonTargetPeersHealthy(t *testing.T) {
	enableIndexerLifecycleForTest(t)
	mgr, statefulSet, pods := indexerLifecycleFixture(t)
	now := metav1.Now()
	mgr.cr.Status.PodUpdate =
		&enterpriseApi.IndexerClusterPodUpdateStatus{
			OperationID:        "operation",
			Stage:              enterpriseApi.IndexerClusterPodUpdateStageWithdrawingReadiness,
			TargetPod:          pods[2].Name,
			TargetPodUID:       string(pods[2].UID),
			TargetOrdinal:      2,
			SourceRevision:     "old",
			DesiredRevision:    "new",
			StartedAt:          &now,
			StageStartedAt:     &now,
			LastTransitionTime: &now,
		}
	if !mgr.canContinueOwnedUpdateDespiteClusterNotReady(statefulSet) {
		t.Fatal("exact owned recovery with healthy non-target peers was blocked")
	}

	mgr.cr.Status.Peers[1].Searchable = false
	if mgr.canContinueOwnedUpdateDespiteClusterNotReady(statefulSet) {
		t.Fatal("owned recovery ignored an unhealthy non-target peer")
	}

	mgr.cr.Status.Peers[1].Searchable = true
	mgr.cr.Status.PodUpdate.Stage =
		enterpriseApi.IndexerClusterPodUpdateStageTargetSelected
	if mgr.canContinueOwnedUpdateDespiteClusterNotReady(statefulSet) {
		t.Fatal("aggregate readiness bypassed before disruption was authorized")
	}
}

func TestSingleIndexerOwnedRecoveryDoesNotRequireANonTargetPeer(t *testing.T) {
	enableIndexerLifecycleForTest(t)
	mgr, statefulSet, pods := indexerLifecycleFixture(t)
	replicas := int32(1)
	statefulSet.Spec.Replicas = &replicas
	statefulSet.Status.Replicas = replicas
	statefulSet.Status.ReadyReplicas = 0
	mgr.cr.Spec.Replicas = replicas
	mgr.cr.Status.Peers = mgr.cr.Status.Peers[:1]

	now := metav1.Now()
	mgr.cr.Status.PodUpdate =
		&enterpriseApi.IndexerClusterPodUpdateStatus{
			OperationID:        "single-indexer-operation",
			Stage:              enterpriseApi.IndexerClusterPodUpdateStageWithdrawingReadiness,
			TargetPod:          pods[0].Name,
			TargetPodUID:       string(pods[0].UID),
			TargetOrdinal:      0,
			SourceRevision:     "old",
			DesiredRevision:    "new",
			StartedAt:          &now,
			StageStartedAt:     &now,
			LastTransitionTime: &now,
		}

	if !mgr.canContinueOwnedUpdateDespiteClusterNotReady(statefulSet) {
		t.Fatal("single-indexer owned recovery was blocked without a non-target peer")
	}
}
