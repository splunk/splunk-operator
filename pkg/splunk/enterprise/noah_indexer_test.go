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

package enterprise

import (
	"context"
	"strings"
	"testing"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	"github.com/splunk/splunk-operator/pkg/splunk/resources"
	spltest "github.com/splunk/splunk-operator/pkg/splunk/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func TestApplyNoahIndexerResourcesCreatesIdentityAwareStatefulSet(t *testing.T) {
	t.Setenv(resources.ClusterDomainEnvName, "corp.example")

	ctx := context.Background()
	client := spltest.NewMockClient()
	cr := &enterpriseApi.IndexerCluster{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "enterprise.splunk.com/v4",
			Kind:       "IndexerCluster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "main",
			Namespace: "test",
			UID:       types.UID("indexer-cluster-uid"),
		},
		Spec: enterpriseApi.IndexerClusterSpec{
			Replicas:       1,
			NoahClusterRef: &corev1.LocalObjectReference{Name: "noah"},
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				Spec: enterpriseApi.Spec{Image: "splunk/splunk:latest"},
			},
		},
	}
	setVolumeDefaults(&cr.Spec.CommonSplunkSpec)
	require.NoError(t, client.Create(ctx, &enterpriseApi.NoahCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "noah", Namespace: cr.Namespace},
		Spec: enterpriseApi.NoahClusterSpec{
			Endpoint: "https://noah.test.svc:8080",
			Tenant:   "axolotl",
			AuthSecretRef: corev1.LocalObjectReference{
				Name: "noah-auth",
			},
		},
	}))
	require.NoError(t, client.Create(ctx, &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "noah-auth", Namespace: cr.Namespace},
		Data:       map[string][]byte{noahAuthSecretKey: []byte("unit-test-noah-key")},
	}))

	statefulSet, phase, err := applyNoahIndexerResources(ctx, client, cr)
	require.NoError(t, err)
	assert.Equal(t, enterpriseApi.PhasePending, phase)

	created := &appsv1.StatefulSet{}
	require.NoError(t, client.Get(ctx, types.NamespacedName{
		Name:      statefulSet.Name,
		Namespace: statefulSet.Namespace,
	}, created))
	for key, value := range created.Spec.Selector.MatchLabels {
		assert.Equal(t, value, created.Spec.Template.Labels[key])
	}
	for _, headless := range []bool{true, false} {
		service := &corev1.Service{}
		require.NoError(t, client.Get(ctx, types.NamespacedName{
			Name:      splcommon.GetSplunkServiceName(SplunkIndexer, cr.Name, headless),
			Namespace: cr.Namespace,
		}, service))
		assert.Equal(t, created.Spec.Selector.MatchLabels, service.Spec.Selector)
		assert.Equal(t, headless, service.Spec.PublishNotReadyAddresses)
		if headless {
			assert.Equal(t, created.Spec.ServiceName, service.Name)
		}
	}

	var defaultsConfigMapName string
	for _, volume := range created.Spec.Template.Spec.Volumes {
		if volume.ConfigMap != nil && strings.HasPrefix(volume.ConfigMap.Name, "sok-indexercluster-defaults-") {
			defaultsConfigMapName = volume.ConfigMap.Name
			break
		}
	}
	require.NotEmpty(t, defaultsConfigMapName)
	defaultsConfigMap := &corev1.ConfigMap{}
	require.NoError(t, client.Get(ctx, types.NamespacedName{
		Name:      defaultsConfigMapName,
		Namespace: created.Namespace,
	}, defaultsConfigMap))
	assert.Contains(t, defaultsConfigMap.Data["conf-defaults.yml"], "https://noah.test.svc:8080")
	assert.Contains(t, defaultsConfigMap.Data["conf-defaults.yml"], "tenant: axolotl")

	env := make(map[string]corev1.EnvVar)
	for _, item := range created.Spec.Template.Spec.Containers[0].Env {
		env[item.Name] = item
	}
	assert.Equal(t, "true", env[resources.NoahEnabledEnvName].Value)
	assert.Equal(t, created.Spec.ServiceName, env[resources.NoahHeadlessServiceEnvName].Value)
	assert.Equal(t, "corp.example", env[resources.ClusterDomainEnvName].Value)
	require.NotNil(t, env[resources.PodNameEnvName].ValueFrom)
	require.NotNil(t, env[resources.PodNamespaceEnvName].ValueFrom)

	var initEtc *corev1.Container
	for i := range created.Spec.Template.Spec.InitContainers {
		if created.Spec.Template.Spec.InitContainers[i].Name == "init-etc" {
			initEtc = &created.Spec.Template.Spec.InitContainers[i]
			break
		}
	}
	require.NotNil(t, initEtc)
	require.Len(t, initEtc.Command, 3)
	assert.Contains(t, initEtc.Command[2], `print "[noahService]"`)
	assert.Contains(t, initEtc.Command[2], `print "disabled = true"`)
	assert.Contains(t, initEtc.Command[2], noahAuthMountPath+"/"+noahAuthSecretKey)
	assert.NotContains(t, initEtc.Command[2], "unit-test-noah-key")

	var authVolume *corev1.Volume
	for i := range created.Spec.Template.Spec.Volumes {
		if created.Spec.Template.Spec.Volumes[i].Name == noahAuthVolumeName {
			authVolume = &created.Spec.Template.Spec.Volumes[i]
			break
		}
	}
	require.NotNil(t, authVolume)
	require.NotNil(t, authVolume.Secret)
	assert.Equal(t, "noah-auth", authVolume.Secret.SecretName)
	assert.Contains(t, initEtc.VolumeMounts, corev1.VolumeMount{
		Name:      noahAuthVolumeName,
		MountPath: noahAuthMountPath,
		ReadOnly:  true,
	})
	for _, mount := range created.Spec.Template.Spec.Containers[0].VolumeMounts {
		assert.NotEqual(t, noahAuthVolumeName, mount.Name, "the main container must not mount the plaintext Noah credential")
	}
}

func TestApplyNoahIndexerResourcesRequiresReferencedNoahCluster(t *testing.T) {
	client := spltest.NewMockClient()
	cr := &enterpriseApi.IndexerCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "main", Namespace: "test"},
		Spec: enterpriseApi.IndexerClusterSpec{
			NoahClusterRef: &corev1.LocalObjectReference{Name: "missing"},
		},
	}

	statefulSet, phase, err := applyNoahIndexerResources(context.Background(), client, cr)
	require.Error(t, err)
	assert.Nil(t, statefulSet)
	assert.Equal(t, enterpriseApi.PhaseError, phase)
	assert.Contains(t, err.Error(), "get referenced NoahCluster test/missing")
}

func TestResolveNoahAuthSecretRejectsInvalidSecret(t *testing.T) {
	ctx := context.Background()
	client := spltest.NewMockClient()
	require.NoError(t, client.Create(ctx, &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "noah-auth", Namespace: "test"},
		Data:       map[string][]byte{noahAuthSecretKey: []byte("valid-value\nsecond-line")},
	}))

	secret, err := resolveNoahAuthSecret(ctx, client, "test", corev1.LocalObjectReference{Name: "noah-auth"})
	require.ErrorContains(t, err, "must be a single line")
	assert.Nil(t, secret)
}

func TestNoahInitEtcUsesRenderedEtcVolume(t *testing.T) {
	statefulSet := &appsv1.StatefulSet{
		Spec: appsv1.StatefulSetSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name: "splunk",
						VolumeMounts: []corev1.VolumeMount{{
							Name:      "rendered-etc",
							MountPath: "/opt/splunk/etc",
						}},
					}},
				},
			},
		},
	}

	noahInitEtcOption(&enterpriseApi.CommonSplunkSpec{})(statefulSet)
	require.Len(t, statefulSet.Spec.Template.Spec.InitContainers, 1)
	initEtc := statefulSet.Spec.Template.Spec.InitContainers[0]
	assert.Equal(t, "rendered-etc", initEtc.VolumeMounts[0].Name)
	require.NotNil(t, initEtc.SecurityContext)
	require.NotNil(t, initEtc.SecurityContext.AllowPrivilegeEscalation)
	assert.False(t, *initEtc.SecurityContext.AllowPrivilegeEscalation)
	require.NotNil(t, initEtc.SecurityContext.Capabilities)
	assert.Equal(t, []corev1.Capability{"ALL"}, initEtc.SecurityContext.Capabilities.Drop)
	require.NotNil(t, initEtc.SecurityContext.SeccompProfile)
	assert.Equal(t, corev1.SeccompProfileTypeRuntimeDefault, initEtc.SecurityContext.SeccompProfile.Type)
}

func TestNoahIndexerStatefulSetOptionsAppliesStableIdentityLast(t *testing.T) {
	statefulSet := &appsv1.StatefulSet{
		Spec: appsv1.StatefulSetSpec{
			ServiceName: "splunk-main-indexer-headless",
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name: "splunk",
					}},
				},
			},
		},
	}

	callerOption := func(statefulSet *appsv1.StatefulSet) {
		statefulSet.Spec.ServiceName = "caller-selected-headless"
		statefulSet.Spec.Template.Spec.Containers[0].Env = append(
			statefulSet.Spec.Template.Spec.Containers[0].Env,
			corev1.EnvVar{Name: resources.NoahEnabledEnvName, Value: "false"},
		)
	}

	options := noahIndexerStatefulSetOptions("corp.example", callerOption)
	resources.ApplyStatefulSetOptions(statefulSet, options...)

	env := make(map[string]corev1.EnvVar)
	for _, item := range statefulSet.Spec.Template.Spec.Containers[0].Env {
		env[item.Name] = item
	}

	assert.Equal(t, "true", env[resources.NoahEnabledEnvName].Value)
	assert.Equal(t, "caller-selected-headless", env[resources.NoahHeadlessServiceEnvName].Value)
	assert.Equal(t, "corp.example", env[resources.ClusterDomainEnvName].Value)

	for envName, fieldPath := range map[string]string{
		resources.PodNameEnvName:      "metadata.name",
		resources.PodNamespaceEnvName: "metadata.namespace",
	} {
		fieldRef := env[envName].ValueFrom
		require.NotNil(t, fieldRef)
		require.NotNil(t, fieldRef.FieldRef)
		assert.Equal(t, fieldPath, fieldRef.FieldRef.FieldPath)
	}
}

func TestNoahIndexerStatefulSetConverged(t *testing.T) {
	const desiredReplicas int32 = 3

	converged := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Generation: 2},
		Status: appsv1.StatefulSetStatus{
			ObservedGeneration: 2,
			CurrentRevision:    "revision-2",
			UpdateRevision:     "revision-2",
			UpdatedReplicas:    desiredReplicas,
			ReadyReplicas:      desiredReplicas,
		},
	}

	tests := []struct {
		name        string
		statefulSet *appsv1.StatefulSet
		want        bool
	}{
		{
			name:        "latest revision is fully ready",
			statefulSet: converged,
			want:        true,
		},
		{
			name:        "missing StatefulSet",
			statefulSet: nil,
		},
		{
			name: "latest generation is not observed",
			statefulSet: func() *appsv1.StatefulSet {
				statefulSet := converged.DeepCopy()
				statefulSet.Status.ObservedGeneration--
				return statefulSet
			}(),
		},
		{
			name: "update revision is not available",
			statefulSet: func() *appsv1.StatefulSet {
				statefulSet := converged.DeepCopy()
				statefulSet.Status.UpdateRevision = ""
				return statefulSet
			}(),
		},
		{
			name: "pods are on the previous revision",
			statefulSet: func() *appsv1.StatefulSet {
				statefulSet := converged.DeepCopy()
				statefulSet.Status.CurrentRevision = "revision-1"
				return statefulSet
			}(),
		},
		{
			name: "not all replicas are updated",
			statefulSet: func() *appsv1.StatefulSet {
				statefulSet := converged.DeepCopy()
				statefulSet.Status.UpdatedReplicas--
				return statefulSet
			}(),
		},
		{
			name: "updated replicas are not all ready",
			statefulSet: func() *appsv1.StatefulSet {
				statefulSet := converged.DeepCopy()
				statefulSet.Status.ReadyReplicas--
				return statefulSet
			}(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, noahIndexerStatefulSetConverged(tt.statefulSet, desiredReplicas))
		})
	}
}
