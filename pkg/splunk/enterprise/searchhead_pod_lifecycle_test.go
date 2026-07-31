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
	spltest "github.com/splunk/splunk-operator/pkg/splunk/test"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestSearchHeadPreStopWiring(t *testing.T) {
	t.Run("disabled gates preserve the container", func(t *testing.T) {
		setLifecyclePolicyTestGates(t, false, false)
		container := &corev1.Container{Name: "splunk"}
		applySearchHeadPodLifecycle(container, SplunkSearchHead)
		require.Nil(t, container.Lifecycle)
	})

	t.Run("non Search Head preserves the container", func(t *testing.T) {
		setLifecyclePolicyTestGates(t, true, true)
		container := &corev1.Container{Name: "splunk"}
		applySearchHeadPodLifecycle(container, SplunkDeployer)
		require.Nil(t, container.Lifecycle)
	})

	t.Run("sidecars do not receive the Splunk lifecycle", func(t *testing.T) {
		setLifecyclePolicyTestGates(t, true, true)
		container := &corev1.Container{Name: "sidecar"}
		applySearchHeadPodLifecycle(container, SplunkSearchHead)
		require.Nil(t, container.Lifecycle)
	})

	t.Run("Search Head receives runtime handoff and retains postStart", func(t *testing.T) {
		setLifecyclePolicyTestGates(t, true, true)
		postStart := &corev1.LifecycleHandler{
			Exec: &corev1.ExecAction{Command: []string{"existing-post-start"}},
		}
		container := &corev1.Container{
			Name:      "splunk",
			Lifecycle: &corev1.Lifecycle{PostStart: postStart},
		}

		applySearchHeadPodLifecycle(container, SplunkSearchHead)

		require.Equal(t, postStart, container.Lifecycle.PostStart)
		require.Equal(t,
			[]string{"/bin/sh", "-ec", searchHeadPreStopScript},
			container.Lifecycle.PreStop.Exec.Command)
		require.Contains(t, searchHeadPreStopScript, searchHeadRuntimeShutdownExecutable)
		require.Contains(t, searchHeadPreStopScript, "--source=prestop")
		require.Contains(t, searchHeadPreStopScript, "splunk-container.state")
		require.Contains(t, searchHeadPreStopScript, "stopping")
		require.NotContains(t, searchHeadPreStopScript, "splunk stop")
		require.NotContains(t, searchHeadPreStopScript, "detention")
		require.NotContains(t, searchHeadPreStopScript, "captain")
	})
}

func TestSearchHeadStatefulSetUsesSupportedRuntimeLifecycle(t *testing.T) {
	t.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")
	setLifecyclePolicyTestGates(t, true, true)

	privateImage := "registry.airgap.example:5000/splunk/splunk@sha256:0123456789abcdef"
	pullSecrets := []corev1.LocalObjectReference{
		{Name: "airgap-registry-primary"},
		{Name: "airgap-registry-fallback"},
	}
	cr := &enterpriseApi.SearchHeadCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "shc", Namespace: "test"},
		Spec: enterpriseApi.SearchHeadClusterSpec{
			Replicas: 3,
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				Spec: enterpriseApi.Spec{
					Image:           privateImage,
					ImagePullPolicy: string(corev1.PullIfNotPresent),
				},
				ImagePullSecrets: pullSecrets,
			},
		},
	}

	statefulSet, err := getSplunkStatefulSet(
		context.Background(),
		spltest.NewMockClient(),
		cr,
		&cr.Spec.CommonSplunkSpec,
		SplunkSearchHead,
		cr.Spec.Replicas,
		getSearchHeadExtraEnv(cr, cr.Spec.Replicas),
		nil,
	)
	require.NoError(t, err)
	require.Len(t, statefulSet.Spec.Template.Spec.Containers, 1,
		"the lifecycle must not inject a helper image")
	require.Empty(t, statefulSet.Spec.Template.Spec.InitContainers,
		"the lifecycle must not introduce an undeclared init image")
	require.Equal(t, pullSecrets, statefulSet.Spec.Template.Spec.ImagePullSecrets)

	container := statefulSet.Spec.Template.Spec.Containers[0]
	require.Equal(t, privateImage, container.Image)
	require.Equal(t, corev1.PullIfNotPresent, container.ImagePullPolicy)
	require.Equal(t,
		[]string{GetProbeMountDirectory() + "/" + GetReadinessScriptName()},
		container.ReadinessProbe.Exec.Command,
		"current image checkstate remains the supported local readiness contract")
	require.False(t,
		strings.Contains(strings.Join(container.ReadinessProbe.Exec.Command, " "),
			"/services/shcluster/member/ready"),
		"the Operator must not render an unsupported SHC readiness endpoint")
	require.Equal(
		t,
		int32(60),
		container.StartupProbe.FailureThreshold,
		"startup must cover the controller's bounded first-start and upgrade window",
	)
	require.NotNil(t, container.StartupProbe.TerminationGracePeriodSeconds)
	require.Equal(
		t,
		DefaultProbeTerminationGracePeriodSeconds,
		*container.StartupProbe.TerminationGracePeriodSeconds,
	)
	require.NotNil(t, container.LivenessProbe.TerminationGracePeriodSeconds)
	require.Equal(
		t,
		DefaultProbeTerminationGracePeriodSeconds,
		*container.LivenessProbe.TerminationGracePeriodSeconds,
	)
	require.Nil(
		t,
		container.ReadinessProbe.TerminationGracePeriodSeconds,
		"readiness failures do not terminate containers",
	)
	require.NotNil(t, statefulSet.Spec.Template.Spec.TerminationGracePeriodSeconds)
	require.Equal(
		t,
		DefaultTerminationGracePeriodSeconds,
		*statefulSet.Spec.Template.Spec.TerminationGracePeriodSeconds,
	)
	require.Equal(t,
		[]string{"/bin/sh", "-ec", searchHeadPreStopScript},
		container.Lifecycle.PreStop.Exec.Command)
}
