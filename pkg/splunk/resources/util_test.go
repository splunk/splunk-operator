// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.

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

package resources_test

import (
	"context"
	"testing"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	"github.com/splunk/splunk-operator/pkg/splunk/resources"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestGetSplunkService(t *testing.T) {
	cr := &enterpriseApi.IndexerCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "stack1", Namespace: "test"},
	}

	service := resources.GetSplunkService(context.Background(), cr, &cr.Spec.CommonSplunkSpec, splcommon.SplunkIndexer, false)

	require.NotNil(t, service)
	assert.Equal(t, "test", service.Namespace)
	assert.Equal(t, splcommon.GetSplunkServiceName(splcommon.SplunkIndexer, "stack1", false), service.Name)
	assert.NotEmpty(t, service.Spec.Ports)
	assert.Equal(t, service.Spec.Selector, service.Labels)
}

func TestGetSplunkDefaults(t *testing.T) {
	defaults := resources.GetSplunkDefaults("stack1", "test", splcommon.SplunkIndexer, "defaults_string")

	require.NotNil(t, defaults)
	assert.Equal(t, "test", defaults.Namespace)
	assert.Equal(t, "defaults_string", defaults.Data["default.yml"])
	assert.Equal(t, splutil.GetSplunkDefaultsName("stack1", splcommon.SplunkIndexer), defaults.Name)
}

func TestSetVolumeDefaults(t *testing.T) {
	mode := int32(644)
	spec := &enterpriseApi.CommonSplunkSpec{
		Volumes: []corev1.Volume{
			{Name: "secret", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: "secret"}}},
			{Name: "configured-secret", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: "secret", DefaultMode: &mode}}},
			{Name: "configmap", VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{}}},
		},
	}

	resources.SetVolumeDefaults(spec)

	require.NotNil(t, spec.Volumes[0].Secret.DefaultMode)
	assert.Equal(t, int32(corev1.SecretVolumeSourceDefaultMode), *spec.Volumes[0].Secret.DefaultMode)
	assert.Equal(t, mode, *spec.Volumes[1].Secret.DefaultMode)
	require.NotNil(t, spec.Volumes[2].ConfigMap.DefaultMode)
	assert.Equal(t, int32(corev1.ConfigMapVolumeSourceDefaultMode), *spec.Volumes[2].ConfigMap.DefaultMode)
}

func TestGetProbes(t *testing.T) {
	configured := &enterpriseApi.Probe{InitialDelaySeconds: 7, TimeoutSeconds: 8, PeriodSeconds: 9, FailureThreshold: 10}

	liveness := resources.GetLivenessProbe(configured, 11)
	assert.Equal(t, int32(7), liveness.InitialDelaySeconds)
	assert.Equal(t, int32(8), liveness.TimeoutSeconds)
	assert.Equal(t, int32(9), liveness.PeriodSeconds)
	assert.Equal(t, int32(10), liveness.FailureThreshold)

	readiness := resources.GetReadinessProbe(&enterpriseApi.Probe{}, 11)
	assert.Equal(t, int32(11), readiness.InitialDelaySeconds)

	startup := resources.GetStartupProbe(nil)
	require.NotNil(t, startup.Exec)
	assert.NotEmpty(t, startup.Exec.Command)

	probe := resources.GetProbe([]string{"check"}, 1, 2, 3)
	assert.Equal(t, []string{"check"}, probe.Exec.Command)
	assert.Equal(t, int32(1), probe.InitialDelaySeconds)
	assert.Equal(t, int32(2), probe.TimeoutSeconds)
	assert.Equal(t, int32(3), probe.PeriodSeconds)
}

func TestGetVolumeSourceMountFromConfigMapData(t *testing.T) {
	mode := int32(644)
	configMap := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "defaults"}, Data: map[string]string{"z": "last", "a": "first"}}

	volumeSource := resources.GetVolumeSourceMountFromConfigMapData(configMap, &mode)

	require.NotNil(t, volumeSource.ConfigMap)
	assert.Equal(t, "defaults", volumeSource.ConfigMap.Name)
	require.Len(t, volumeSource.ConfigMap.Items, 2)
	assert.Equal(t, "a", volumeSource.ConfigMap.Items[0].Key)
	assert.Equal(t, "z", volumeSource.ConfigMap.Items[1].Key)
	assert.Equal(t, mode, *volumeSource.ConfigMap.Items[0].Mode)
}

func TestRemoveDuplicateEnvVars(t *testing.T) {
	got := resources.RemoveDuplicateEnvVars([]corev1.EnvVar{
		{Name: "A", Value: "first"},
		{Name: "B", Value: "value"},
		{Name: "A", Value: "second"},
	})

	assert.Equal(t, []corev1.EnvVar{{Name: "A", Value: "first"}, {Name: "B", Value: "value"}}, got)
}
