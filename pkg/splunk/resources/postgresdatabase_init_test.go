// Copyright (c) 2026 Splunk Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
)

func TestPostgresDatabasePreparesRuntimeWithoutExposingCredentials(t *testing.T) {
	pod := postgresRuntimePod()
	bundle := PostgresDatabaseBundle{{Database: "app", ConfigMapName: "metadata"}}
	require.NoError(t, bundle.Inject(pod))
	require.Len(t, pod.Spec.InitContainers, 1, "prepare runtime parents before the main container mounts the projection")
	init := pod.Spec.InitContainers[0]
	assert.Equal(t, "splunk:test", init.Image)
	assert.Equal(t, []corev1.VolumeMount{{Name: "var", MountPath: "/opt/splunk/var"}}, init.VolumeMounts)
	require.NotNil(t, init.SecurityContext)
	assert.True(t, *init.SecurityContext.RunAsNonRoot)
	assert.False(t, *init.SecurityContext.AllowPrivilegeEscalation)
	assert.Contains(t, init.Args, "/opt/splunk/var/run/supervisor")
	assert.Empty(t, init.Env, "initializer must not receive credential environment variables")
	assert.Equal(t, int32(0440), *pod.Spec.Volumes[1].Projected.DefaultMode)
	assert.True(t, pod.Spec.Containers[0].VolumeMounts[1].ReadOnly)
}

func TestPostgresDatabaseRuntimeInitializerHonorsHomeAndRejectsUnwritableParents(t *testing.T) {
	bundle := PostgresDatabaseBundle{{Database: "app", ConfigMapName: "metadata"}}
	pod := postgresRuntimePod()
	pod.Spec.Containers[0].Env = []corev1.EnvVar{{Name: "SPLUNK_HOME", Value: "/custom/splunk"}}
	pod.Spec.Containers[0].VolumeMounts[0].MountPath = "/custom/splunk/var"
	require.NoError(t, bundle.Inject(pod))
	assert.Contains(t, pod.Spec.InitContainers[0].Args, "/custom/splunk/var/run/supervisor")
	assert.Equal(t, "/custom/splunk/var", pod.Spec.InitContainers[0].VolumeMounts[0].MountPath)

	pod = postgresRuntimePod()
	pod.Spec.Containers[0].VolumeMounts[0].ReadOnly = true
	before := pod.DeepCopy()
	require.ErrorContains(t, bundle.Inject(pod), "requires a writable parent volume")
	assert.Equal(t, before, pod)

	pod = postgresRuntimePod()
	pod.Spec.InitContainers = []corev1.Container{{Name: "sok-postgresok-init"}}
	before = pod.DeepCopy()
	require.ErrorContains(t, bundle.Inject(pod), "duplicate PostgresDatabase runtime initializer")
	assert.Equal(t, before, pod)
}

func postgresRuntimePod() *corev1.PodTemplateSpec {
	return &corev1.PodTemplateSpec{Spec: corev1.PodSpec{
		SecurityContext: &corev1.PodSecurityContext{RunAsUser: new(int64(41812)), FSGroup: new(int64(41812)), RunAsNonRoot: new(true)},
		Volumes:         []corev1.Volume{{Name: "var", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}}},
		Containers:      []corev1.Container{{Name: "splunk", Image: "splunk:test", ImagePullPolicy: corev1.PullIfNotPresent, VolumeMounts: []corev1.VolumeMount{{Name: "var", MountPath: "/opt/splunk/var"}}}},
	}}
}
