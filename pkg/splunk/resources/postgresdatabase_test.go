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

	spltest "github.com/splunk/splunk-operator/pkg/splunk/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
)

func TestInjectPostgresDatabaseMounts(t *testing.T) {
	pod := postgresRuntimePod()
	pod.Spec.Containers = append(pod.Spec.Containers, corev1.Container{Name: "sidecar"})
	pod.Spec.InitContainers = []corev1.Container{{Name: "init"}}
	bundle := PostgresDatabaseBundle{{
		Name: "sidecar-abc", ConfigMapName: "producer-config",
		AdminPassword: &corev1.SecretKeySelector{Name: "admin", Key: "password"},
		RWPassword:    corev1.SecretKeySelector{Name: "rw", Key: "custom"}, CASecretName: "primary-ca",
	}}
	require.NoError(t, bundle.Inject(pod))
	require.Len(t, pod.Spec.Volumes, 2)
	assert.Equal(t, int32(0440), *pod.Spec.Volumes[1].Projected.DefaultMode)
	assert.Equal(t, map[string]spltest.PostgresProjectedFile{
		"sidecar-abc/CLUSTER_RW_ENDPOINT":  {Source: "producer-config", Key: "CLUSTER_RW_ENDPOINT"},
		"sidecar-abc/DEFAULT_CLUSTER_PORT": {Source: "producer-config", Key: "DEFAULT_CLUSTER_PORT"},
		"sidecar-abc/DATABASE_NAME":        {Source: "producer-config", Key: "DATABASE_NAME"},
		"sidecar-abc/ADMIN_USER_NAME":      {Source: "producer-config", Key: "ADMIN_USER_NAME"},
		"sidecar-abc/RW_USER_NAME":         {Source: "producer-config", Key: "RW_USER_NAME"},
		"sidecar-abc/RW_PASSWORD":          {Source: "rw", Key: "custom"},
		"sidecar-abc/ADMIN_PASSWORD":       {Source: "admin", Key: "password"},
		"sidecar-abc/ca.crt":               {Source: "primary-ca", Key: "ca.crt"},
	}, spltest.PostgresDatabaseFiles(t, pod))
	assert.Equal(t, corev1.VolumeMount{Name: PostgresDatabaseVolumeName, MountPath: PostgresDatabaseMountRoot, ReadOnly: true}, pod.Spec.Containers[0].VolumeMounts[1])
	assert.Empty(t, pod.Spec.Containers[1].VolumeMounts)
	assert.Empty(t, pod.Spec.InitContainers[0].VolumeMounts)
	before := pod.DeepCopy()
	assert.Error(t, bundle.Inject(pod))
	assert.Equal(t, before, pod)
}

func TestPostgresDatabaseDefaultProjectionHasNoAdmin(t *testing.T) {
	pod := postgresRuntimePod()
	bundle := PostgresDatabaseBundle{{Name: "sidecar", ConfigMapName: "metadata", RWPassword: corev1.SecretKeySelector{Name: "rw", Key: "password"}, CASecretName: "ca"}}
	require.NoError(t, bundle.Inject(pod))
	files := spltest.PostgresDatabaseFiles(t, pod)
	assert.Contains(t, files, "sidecar/RW_PASSWORD")
	assert.NotContains(t, files, "sidecar/ADMIN_PASSWORD")
}

func TestPostgresDatabaseMountHome(t *testing.T) {
	pod := &corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{
		Name: "splunk", Env: []corev1.EnvVar{{Name: "SPLUNK_HOME", Value: "/custom/splunk"}},
		VolumeMounts: []corev1.VolumeMount{{Name: "var", MountPath: "/custom/splunk/var"}},
	}}}}
	bundle := PostgresDatabaseBundle{{Name: "sidecar", ConfigMapName: "producer-config"}}
	require.NoError(t, bundle.Inject(pod))
	assert.Equal(t, "/custom/splunk/var/run/supervisor/postgresok", pod.Spec.Containers[0].VolumeMounts[1].MountPath)
	assert.False(t, pod.Spec.Containers[0].VolumeMounts[0].ReadOnly)
}

func TestPostgresDatabaseProjectionChangesWithReferences(t *testing.T) {
	original := PostgresDatabaseMount{
		Name: "sidecar", ConfigMapName: "producer-config",
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
		{"directory", func(m *PostgresDatabaseMount) { m.Name = "other-sidecar" }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			changed := original
			changed.AdminPassword = original.AdminPassword.DeepCopy()
			tt.change(&changed)
			assert.NotEqual(t, baseline, projection(PostgresDatabaseBundle{changed}))
		})
	}
	other := original
	other.Name = "other"
	assert.Equal(t, projection(PostgresDatabaseBundle{original, other}), projection(PostgresDatabaseBundle{other, original}), "spec reordering must not roll pods")
}

func TestPostgresDatabaseSidecarsUseSeparateDirectories(t *testing.T) {
	pod := postgresRuntimePod()
	// Both entries come from the same database publication.
	shared := PostgresDatabaseMount{ConfigMapName: "metadata", RWPassword: corev1.SecretKeySelector{Name: "rw", Key: "password"}, CASecretName: "ca"}
	abc, xyz := shared, shared
	abc.Name, xyz.Name = "sidecar-abc", "sidecar-xyz"
	require.NoError(t, PostgresDatabaseBundle{xyz, abc}.Inject(pod))
	files := spltest.PostgresDatabaseFiles(t, pod)
	assert.Len(t, files, 14, "each sidecar gets its own seven files")
	for _, sidecar := range []string{"sidecar-abc", "sidecar-xyz"} {
		assert.Equal(t, spltest.PostgresProjectedFile{Source: "metadata", Key: "CLUSTER_RW_ENDPOINT"}, files[sidecar+"/CLUSTER_RW_ENDPOINT"])
		assert.Equal(t, spltest.PostgresProjectedFile{Source: "rw", Key: "password"}, files[sidecar+"/RW_PASSWORD"])
		assert.Equal(t, spltest.PostgresProjectedFile{Source: "ca", Key: "ca.crt"}, files[sidecar+"/ca.crt"])
	}
}
