// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.
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
	"strings"
	"testing"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/splunk/splunk-operator/pkg/splunk/test"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestSHCPodRolloutActiveFailsClosed(t *testing.T) {
	tests := []struct {
		name      string
		operation *enterpriseApi.SearchHeadClusterLifecycleOperationStatus
		want      bool
	}{
		{name: "no operation"},
		{
			name: "scale down is not Pod rollout",
			operation: &enterpriseApi.SearchHeadClusterLifecycleOperationStatus{
				Intent: enterpriseApi.SearchHeadClusterLifecycleIntentScaleDown,
				Stage:  enterpriseApi.SearchHeadClusterLifecycleStageDrainingSearches,
			},
		},
		{
			name: "completed Pod rollout",
			operation: &enterpriseApi.SearchHeadClusterLifecycleOperationStatus{
				Intent: enterpriseApi.SearchHeadClusterLifecycleIntentPodUpdate,
				Stage:  enterpriseApi.SearchHeadClusterLifecycleStageCompleted,
			},
		},
		{
			name: "draining Pod rollout",
			operation: &enterpriseApi.SearchHeadClusterLifecycleOperationStatus{
				Intent: enterpriseApi.SearchHeadClusterLifecycleIntentPodUpdate,
				Stage:  enterpriseApi.SearchHeadClusterLifecycleStageDrainingSearches,
			},
			want: true,
		},
		{
			name: "blocked Pod rollout",
			operation: &enterpriseApi.SearchHeadClusterLifecycleOperationStatus{
				Intent: enterpriseApi.SearchHeadClusterLifecycleIntentPodUpdate,
				Stage:  enterpriseApi.SearchHeadClusterLifecycleStageBlocked,
			},
			want: true,
		},
		{
			name: "failed Pod rollout",
			operation: &enterpriseApi.SearchHeadClusterLifecycleOperationStatus{
				Intent: enterpriseApi.SearchHeadClusterLifecycleIntentPodUpdate,
				Stage:  enterpriseApi.SearchHeadClusterLifecycleStageFailed,
			},
			want: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := shcPodRolloutActive(test.operation); got != test.want {
				t.Fatalf("shcPodRolloutActive() = %t, want %t", got, test.want)
			}
		})
	}
}

func TestSHCAppFrameworkKubernetesRestartOwnership(t *testing.T) {
	replicas := int32(3)
	tests := []struct {
		name         string
		podGate      bool
		shcGate      bool
		strategy     enterpriseApi.SearchHeadClusterPodUpdateStrategy
		initialStage enterpriseApi.SearchHeadClusterInitialFormationStage
		stable       *int32
		want         bool
	}{
		{
			name:     "feature gates disabled retains Splunk restart",
			strategy: enterpriseApi.SearchHeadClusterPodUpdateStrategyRollingUpdate,
			stable:   &replicas,
		},
		{
			name:     "OnDelete compatibility retains Splunk restart",
			podGate:  true,
			shcGate:  true,
			strategy: enterpriseApi.SearchHeadClusterPodUpdateStrategyOnDelete,
			stable:   &replicas,
		},
		{
			name:         "initial formation retains bundle-owned restart",
			podGate:      true,
			shcGate:      true,
			strategy:     enterpriseApi.SearchHeadClusterPodUpdateStrategyRollingUpdate,
			initialStage: enterpriseApi.SearchHeadClusterInitialFormationStageAppFrameworkPending,
		},
		{
			name:     "operational RollingUpdate uses Kubernetes restart",
			podGate:  true,
			shcGate:  true,
			strategy: enterpriseApi.SearchHeadClusterPodUpdateStrategyRollingUpdate,
			stable:   &replicas,
			want:     true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			setLifecyclePolicyTestGates(t, test.podGate, test.shcGate)
			cr := &enterpriseApi.SearchHeadCluster{
				Spec: enterpriseApi.SearchHeadClusterSpec{
					LifecyclePolicy: &enterpriseApi.SearchHeadClusterLifecyclePolicy{
						PodUpdateStrategy: test.strategy,
					},
				},
				Status: enterpriseApi.SearchHeadClusterStatus{
					InitialFormationStage: test.initialStage,
					LastStableReplicas:    test.stable,
				},
			}
			got, err := shcAppFrameworkKubernetesRestartEnabled(cr)
			if err != nil {
				t.Fatalf("resolve restart ownership: %v", err)
			}
			if got != test.want {
				t.Fatalf("Kubernetes restart ownership=%t, want %t", got, test.want)
			}
		})
	}
}

func TestValidateSHCAppFrameworkRestartBaseline(t *testing.T) {
	clean := &enterpriseApi.SearchHeadCluster{
		Status: enterpriseApi.SearchHeadClusterStatus{
			Members: []enterpriseApi.SearchHeadClusterMemberStatus{
				{Name: "search-head-0", RestartState: "NoRestart"},
				{Name: "search-head-1"},
			},
		},
	}
	if err := validateSHCAppFrameworkRestartBaseline(clean); err != nil {
		t.Fatalf("clean restart baseline rejected: %v", err)
	}

	advertised := clean.DeepCopy()
	advertised.Status.Members[0].AdvertiseRestartRequired = true
	if err := validateSHCAppFrameworkRestartBaseline(advertised); err == nil ||
		!strings.Contains(err.Error(), "already advertises restart-required") {
		t.Fatalf("advertised restart baseline error=%v", err)
	}

	restarting := clean.DeepCopy()
	restarting.Status.Members[0].RestartState = "Restarting"
	if err := validateSHCAppFrameworkRestartBaseline(restarting); err == nil ||
		!strings.Contains(err.Error(), "restart state") {
		t.Fatalf("active restart baseline error=%v", err)
	}
}

func TestSHCImageUpgradeActiveFailsClosed(t *testing.T) {
	tests := []struct {
		name      string
		operation *enterpriseApi.SearchHeadClusterImageUpgradeStatus
		want      bool
	}{
		{name: "no operation"},
		{
			name:      "empty stored operation",
			operation: &enterpriseApi.SearchHeadClusterImageUpgradeStatus{},
			want:      true,
		},
		{
			name: "pending initialization",
			operation: &enterpriseApi.SearchHeadClusterImageUpgradeStatus{
				Phase: enterpriseApi.
					SearchHeadClusterImageUpgradePhasePendingInitialization,
			},
			want: true,
		},
		{
			name: "blocked",
			operation: &enterpriseApi.SearchHeadClusterImageUpgradeStatus{
				Phase: enterpriseApi.
					SearchHeadClusterImageUpgradePhaseBlocked,
			},
			want: true,
		},
		{
			name: "failed",
			operation: &enterpriseApi.SearchHeadClusterImageUpgradeStatus{
				Phase: enterpriseApi.
					SearchHeadClusterImageUpgradePhaseFailed,
			},
			want: true,
		},
		{
			name: "completed",
			operation: &enterpriseApi.SearchHeadClusterImageUpgradeStatus{
				Phase: enterpriseApi.
					SearchHeadClusterImageUpgradePhaseCompleted,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := shcImageUpgradeActive(test.operation); got != test.want {
				t.Fatalf("shcImageUpgradeActive() = %t, want %t", got, test.want)
			}
		})
	}
}

func TestSHCBundleTargetRejectsActiveImageUpgradeOwner(t *testing.T) {
	setLifecyclePolicyTestGates(t, true, true)
	cr := &enterpriseApi.SearchHeadCluster{
		Status: enterpriseApi.SearchHeadClusterStatus{
			ImageUpgrade: &enterpriseApi.SearchHeadClusterImageUpgradeStatus{
				OperationID: "image-upgrade:search-head:revision-2",
				Phase: enterpriseApi.
					SearchHeadClusterImageUpgradePhasePendingInitialization,
			},
		},
	}

	_, err := resolveSHCBundlePushTarget(
		context.Background(),
		nil,
		cr,
	)
	if err == nil || !strings.Contains(err.Error(), "image-upgrade operation") {
		t.Fatalf("active image owner bundle target error = %v", err)
	}
}

func TestSHCBundleTargetUsesContainerReadinessDuringInitialFormation(
	t *testing.T,
) {
	setLifecyclePolicyTestGates(t, true, true)
	ctx := context.Background()
	client := test.NewMockClient()
	shc := &enterpriseApi.SearchHeadCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
		Status: enterpriseApi.SearchHeadClusterStatus{
			Captain:      "splunk-stack1-search-head-0",
			CaptainReady: true,
			InitialFormationStage: enterpriseApi.
				SearchHeadClusterInitialFormationStageTelemetryPending,
			Members: []enterpriseApi.SearchHeadClusterMemberStatus{
				{
					Name:       "splunk-stack1-search-head-0",
					Status:     "Up",
					Registered: true,
				},
			},
		},
	}
	client.AddObject(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-stack1-search-head-0",
			Namespace: "test",
		},
		Status: corev1.PodStatus{
			Conditions: []corev1.PodCondition{
				{
					Type:   corev1.ContainersReady,
					Status: corev1.ConditionTrue,
				},
				{
					Type:   corev1.PodReady,
					Status: corev1.ConditionFalse,
				},
			},
		},
	})

	got, err := resolveSHCBundlePushTarget(ctx, client, shc)
	if err != nil {
		t.Fatalf("resolve initial-formation bundle target: %v", err)
	}
	want := GetSplunkStatefulsetURL(
		"test",
		SplunkSearchHead,
		"stack1",
		0,
		false,
	)
	if got != want {
		t.Fatalf("initial-formation bundle target = %q, want %q", got, want)
	}
}

func TestSHCBundleTargetRejectsContainerNotReadyDuringInitialFormation(
	t *testing.T,
) {
	setLifecyclePolicyTestGates(t, true, true)
	ctx := context.Background()
	client := test.NewMockClient()
	shc := &enterpriseApi.SearchHeadCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
		Status: enterpriseApi.SearchHeadClusterStatus{
			Captain:      "splunk-stack1-search-head-0",
			CaptainReady: true,
			InitialFormationStage: enterpriseApi.
				SearchHeadClusterInitialFormationStageTelemetryPending,
			Members: []enterpriseApi.SearchHeadClusterMemberStatus{
				{
					Name:       "splunk-stack1-search-head-0",
					Status:     "Up",
					Registered: true,
				},
			},
		},
	}
	client.AddObject(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-stack1-search-head-0",
			Namespace: "test",
		},
		Status: corev1.PodStatus{
			Conditions: []corev1.PodCondition{
				{
					Type:   corev1.ContainersReady,
					Status: corev1.ConditionFalse,
				},
				{
					Type:   corev1.PodReady,
					Status: corev1.ConditionFalse,
				},
			},
		},
	})

	_, err := resolveSHCBundlePushTarget(ctx, client, shc)
	if err == nil ||
		!strings.Contains(err.Error(), "no registered, Up") {
		t.Fatalf("container-not-ready bundle target error = %v", err)
	}
}

func TestSHCAppFrameworkWorkActive(t *testing.T) {
	tests := []struct {
		name       string
		appContext *enterpriseApi.AppDeploymentContext
		want       bool
	}{
		{name: "nil"},
		{name: "empty", appContext: &enterpriseApi.AppDeploymentContext{}},
		{
			name: "empty repository poll lock",
			appContext: &enterpriseApi.AppDeploymentContext{
				IsDeploymentInProgress: true,
			},
		},
		{
			name: "app pending",
			appContext: appDeploymentContextWithStatus(
				enterpriseApi.DeployStatusPending,
			),
			want: true,
		},
		{
			name: "app in progress",
			appContext: appDeploymentContextWithStatus(
				enterpriseApi.DeployStatusInProgress,
			),
			want: true,
		},
		{
			name: "app complete",
			appContext: appDeploymentContextWithStatus(
				enterpriseApi.DeployStatusComplete,
			),
		},
		{
			name: "app error",
			appContext: appDeploymentContextWithStatus(
				enterpriseApi.DeployStatusError,
			),
		},
		{
			name: "phase 3 cluster app complete with legacy pending status",
			appContext: appDeploymentContextWithPhaseStatus(
				enterpriseApi.DeployStatusPending,
				enterpriseApi.PhaseInstall,
				enterpriseApi.AppPkgInstallComplete,
			),
		},
		{
			name: "phase 3 terminal app error with legacy pending status",
			appContext: appDeploymentContextWithPhaseStatus(
				enterpriseApi.DeployStatusPending,
				enterpriseApi.PhaseInstall,
				enterpriseApi.AppPkgInstallError,
			),
		},
		{
			name: "phase 3 download complete still has copy work",
			appContext: appDeploymentContextWithPhaseStatus(
				enterpriseApi.DeployStatusPending,
				enterpriseApi.PhaseDownload,
				enterpriseApi.AppPkgDownloadComplete,
			),
			want: true,
		},
		{
			name: "completed app with in-progress bundle",
			appContext: func() *enterpriseApi.AppDeploymentContext {
				appContext := appDeploymentContextWithPhaseStatus(
					enterpriseApi.DeployStatusPending,
					enterpriseApi.PhaseInstall,
					enterpriseApi.AppPkgInstallComplete,
				)
				appContext.BundlePushStatus.BundlePushStage =
					enterpriseApi.BundlePushInProgress
				return appContext
			}(),
			want: true,
		},
		{
			name: "bundle pending",
			appContext: &enterpriseApi.AppDeploymentContext{
				BundlePushStatus: enterpriseApi.BundlePushTracker{
					BundlePushStage: enterpriseApi.BundlePushPending,
				},
			},
			want: true,
		},
		{
			name: "bundle in progress",
			appContext: &enterpriseApi.AppDeploymentContext{
				BundlePushStatus: enterpriseApi.BundlePushTracker{
					BundlePushStage: enterpriseApi.BundlePushInProgress,
				},
			},
			want: true,
		},
		{
			name: "bundle complete",
			appContext: &enterpriseApi.AppDeploymentContext{
				BundlePushStatus: enterpriseApi.BundlePushTracker{
					BundlePushStage: enterpriseApi.BundlePushComplete,
				},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := shcAppFrameworkWorkActive(test.appContext); got != test.want {
				t.Fatalf(
					"shcAppFrameworkWorkActive() = %t, want %t",
					got,
					test.want,
				)
			}
		})
	}
}

func appDeploymentContextWithStatus(
	status enterpriseApi.AppDeploymentStatus,
) *enterpriseApi.AppDeploymentContext {
	return appDeploymentContextWithPhaseStatus(status, "", 0)
}

func appDeploymentContextWithPhaseStatus(
	status enterpriseApi.AppDeploymentStatus,
	phase enterpriseApi.AppPhaseType,
	phaseStatus enterpriseApi.AppPhaseStatusType,
) *enterpriseApi.AppDeploymentContext {
	return &enterpriseApi.AppDeploymentContext{
		IsDeploymentInProgress: true,
		AppsSrcDeployStatus: map[string]enterpriseApi.AppSrcDeployInfo{
			"test-source": {
				AppDeploymentInfoList: []enterpriseApi.AppDeploymentInfo{
					{
						DeployStatus: status,
						PhaseInfo: enterpriseApi.PhaseInfo{
							Phase:  phase,
							Status: phaseStatus,
						},
					},
				},
			},
		},
	}
}
