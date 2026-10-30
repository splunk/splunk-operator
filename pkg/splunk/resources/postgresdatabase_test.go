// Copyright (c) 2026 Splunk Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package resources

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
)

func TestInjectPostgresDatabaseMounts(t *testing.T) {
	pod := postgresRuntimePod()
	pod.Spec.Containers = append(pod.Spec.Containers, corev1.Container{Name: "sidecar"})
	pod.Spec.InitContainers = []corev1.Container{{Name: "init"}}
	bundle := PostgresDatabaseBundle{{
		Database: "app", ConfigMapName: "producer-config",
		AdminPassword: &corev1.SecretKeySelector{Name: "admin", Key: "password"},
		RWPassword:    corev1.SecretKeySelector{Name: "rw", Key: "custom"}, CASecretName: "primary-ca",
	}}
	require.NoError(t, bundle.Inject(pod))
	require.Len(t, pod.Spec.Volumes, 2)
	projection := pod.Spec.Volumes[1].Projected
	require.NotNil(t, projection)
	assert.Equal(t, int32(0440), *projection.DefaultMode)
	require.Len(t, projection.Sources, 4)
	assert.Equal(t, "producer-config", projection.Sources[0].ConfigMap.Name)
	assert.Equal(t, []corev1.KeyToPath{
		{Key: "CLUSTER_RW_ENDPOINT", Path: "app/CLUSTER_RW_ENDPOINT"},
		{Key: "DEFAULT_CLUSTER_PORT", Path: "app/DEFAULT_CLUSTER_PORT"},
		{Key: "DATABASE_NAME", Path: "app/DATABASE_NAME"},
		{Key: "ADMIN_USER_NAME", Path: "app/ADMIN_USER_NAME"},
		{Key: "RW_USER_NAME", Path: "app/RW_USER_NAME"},
	}, projection.Sources[0].ConfigMap.Items)
	assert.Equal(t, "admin", projection.Sources[3].Secret.Name)
	assert.Equal(t, []corev1.KeyToPath{{Key: "password", Path: "app-admin"}}, projection.Sources[3].Secret.Items)
	assert.Equal(t, []corev1.KeyToPath{{Key: "custom", Path: "app-rw"}}, projection.Sources[1].Secret.Items)
	assert.Equal(t, "primary-ca", projection.Sources[2].Secret.Name)
	assert.Equal(t, []corev1.KeyToPath{{Key: "ca.crt", Path: "app-ca.crt"}}, projection.Sources[2].Secret.Items)
	assert.Equal(t, corev1.VolumeMount{Name: PostgresDatabaseVolumeName, MountPath: PostgresDatabaseMountRoot, ReadOnly: true}, pod.Spec.Containers[0].VolumeMounts[1])
	assert.Empty(t, pod.Spec.Containers[1].VolumeMounts)
	assert.Empty(t, pod.Spec.InitContainers[0].VolumeMounts)
	before := pod.DeepCopy()
	assert.Error(t, bundle.Inject(pod))
	assert.Equal(t, before, pod)
}

func TestPostgresDatabaseDefaultProjectionHasNoAdmin(t *testing.T) {
	pod := postgresRuntimePod()
	bundle := PostgresDatabaseBundle{{Database: "app", ConfigMapName: "metadata", RWPassword: corev1.SecretKeySelector{Name: "rw", Key: "password"}, CASecretName: "ca"}}
	require.NoError(t, bundle.Inject(pod))
	require.Len(t, pod.Spec.Volumes[1].Projected.Sources, 3)
	for _, source := range pod.Spec.Volumes[1].Projected.Sources {
		if source.Secret != nil {
			for _, item := range source.Secret.Items {
				assert.NotEqual(t, "app-admin", item.Path)
			}
		}
	}
}

func TestPostgresDatabaseMountHome(t *testing.T) {
	pod := &corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{
		Name: "splunk", Env: []corev1.EnvVar{{Name: "SPLUNK_HOME", Value: "/custom/splunk"}},
		VolumeMounts: []corev1.VolumeMount{{Name: "var", MountPath: "/custom/splunk/var"}},
	}}}}
	bundle := PostgresDatabaseBundle{{Database: "app", ConfigMapName: "producer-config"}}
	require.NoError(t, bundle.Inject(pod))
	assert.Equal(t, "/custom/splunk/var/run/supervisor/postgresok", pod.Spec.Containers[0].VolumeMounts[1].MountPath)
	assert.False(t, pod.Spec.Containers[0].VolumeMounts[0].ReadOnly)
}

func TestPostgresDatabaseProjectionChangesWithReferences(t *testing.T) {
	original := PostgresDatabaseMount{
		Database: "app", ConfigMapName: "producer-config",
		AdminPassword: &corev1.SecretKeySelector{Name: "admin", Key: "password"},
		RWPassword:    corev1.SecretKeySelector{Name: "rw", Key: "password"}, CASecretName: "primary-ca",
	}
	projection := func(mounts PostgresDatabaseBundle) *corev1.PodTemplateSpec {
		pod := postgresRuntimePod()
		require.NoError(t, mounts.Inject(pod))
		return pod
	}
	baseline := projection(PostgresDatabaseBundle{original})
	for _, tt := range []struct {
		name   string
		change func(*PostgresDatabaseMount)
	}{
		{"configmap", func(m *PostgresDatabaseMount) { m.ConfigMapName = "other-config" }},
		{"admin", func(m *PostgresDatabaseMount) { m.AdminPassword.Name = "other-admin" }},
		{"RW", func(m *PostgresDatabaseMount) { m.RWPassword.Name = "other-rw" }},
		{"password key", func(m *PostgresDatabaseMount) { m.RWPassword.Key = "other-key" }},
		{"CA", func(m *PostgresDatabaseMount) { m.CASecretName = "other-ca" }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			changed := original
			changed.AdminPassword = original.AdminPassword.DeepCopy()
			tt.change(&changed)
			assert.NotEqual(t, baseline, projection(PostgresDatabaseBundle{changed}))
		})
	}
	other := original
	other.Database = "other"
	assert.Equal(t, projection(PostgresDatabaseBundle{original, other}), projection(PostgresDatabaseBundle{other, original}), "spec reordering must not roll pods")
}
