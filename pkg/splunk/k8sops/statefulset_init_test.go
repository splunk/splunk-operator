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

package k8sops

import (
	"testing"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/splunk/splunk-operator/pkg/splunk/resources"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestApplyStatefulSetReplacesPostgresInitializerWithSmartStore(t *testing.T) {
	ctx := t.Context()
	scheme := runtime.NewScheme()
	require.NoError(t, appsv1.AddToScheme(scheme))
	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	current := initializerWorkload()
	bundle := resources.PostgresDatabaseBundle{{Database: "appdb", ConfigMapName: "connection", RWPassword: corev1.SecretKeySelector{Name: "rw", Key: "password"}, CASecretName: "postgres-ca"}}
	require.NoError(t, bundle.Inject(&current.Spec.Template))
	phase, err := ApplyStatefulSet(ctx, c, current)
	require.NoError(t, err)
	require.Equal(t, enterpriseApi.PhasePending, phase)
	require.Len(t, current.Spec.Template.Spec.InitContainers, 1)
	require.Equal(t, "sok-postgresok-init", current.Spec.Template.Spec.InitContainers[0].Name)

	desired := initializerWorkload()
	const command = "mkdir -p /opt/splk/etc/apps/splunk-operator/local && ln -sfn /mnt/splunk-operator/local/indexes.conf /opt/splk/etc/apps/splunk-operator/local/indexes.conf && ln -sfn /mnt/splunk-operator/local/server.conf /opt/splk/etc/apps/splunk-operator/local/server.conf"
	resources.SetupInitContainer(&desired.Spec.Template, "splunk/splunk:10.2.0", string(corev1.PullIfNotPresent), command, true)
	expected := desired.DeepCopy()
	phase, err = ApplyStatefulSet(ctx, c, desired)
	require.NoError(t, err)
	assert.Equal(t, enterpriseApi.PhaseUpdating, phase)
	stored := &appsv1.StatefulSet{}
	require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(desired), stored))
	require.Len(t, stored.Spec.Template.Spec.InitContainers, 1)
	init := stored.Spec.Template.Spec.InitContainers[0]
	assert.Equal(t, "init", init.Name, "the stored initializer must be SmartStore's, not the removed PostgreSQL initializer")
	assert.Equal(t, []string{"bash", "-c", command}, init.Command)
	assert.Equal(t, []corev1.VolumeMount{{Name: "mnt-splunk-etc", MountPath: "/opt/splk/etc"}}, init.VolumeMounts)
	assert.True(t, apiequality.Semantic.DeepEqual(expected.Spec.Template.Spec.InitContainers, stored.Spec.Template.Spec.InitContainers), "the stored initializer must match the complete desired specification")

	version := stored.ResourceVersion
	phase, err = ApplyStatefulSet(ctx, c, expected.DeepCopy())
	require.NoError(t, err)
	assert.Equal(t, enterpriseApi.PhaseReady, phase)
	require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(expected), stored))
	assert.Equal(t, version, stored.ResourceVersion, "an unchanged reconcile must not write another revision")

	// Switching back also keeps the same initializer count and image.
	back := initializerWorkload()
	require.NoError(t, bundle.Inject(&back.Spec.Template))
	expectedBack := back.DeepCopy()
	phase, err = ApplyStatefulSet(ctx, c, back)
	require.NoError(t, err)
	assert.Equal(t, enterpriseApi.PhaseUpdating, phase)
	require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(back), stored))
	assert.True(t, apiequality.Semantic.DeepEqual(expectedBack.Spec.Template.Spec.InitContainers, stored.Spec.Template.Spec.InitContainers))
	version = stored.ResourceVersion
	phase, err = ApplyStatefulSet(ctx, c, expectedBack.DeepCopy())
	require.NoError(t, err)
	assert.Equal(t, enterpriseApi.PhaseReady, phase)
	require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(back), stored))
	assert.Equal(t, version, stored.ResourceVersion)
}

func TestApplyStatefulSetReconcilesInitializerSpecifications(t *testing.T) {
	for _, tt := range []struct {
		name   string
		change func([]corev1.Container)
	}{
		{"identity", func(c []corev1.Container) { c[0].Name = "replacement" }},
		{"command", func(c []corev1.Container) { c[0].Command = []string{"/bin/bash"} }},
		{"arguments", func(c []corev1.Container) { c[0].Args = []string{"-c", "echo updated"} }},
		{"remove arguments", func(c []corev1.Container) { c[0].Args = nil }},
		{"environment", func(c []corev1.Container) { c[0].Env = []corev1.EnvVar{{Name: "MODE", Value: "updated"}} }},
		{"mounts", func(c []corev1.Container) {
			c[0].VolumeMounts = []corev1.VolumeMount{{Name: "mnt-splunk-etc", MountPath: "/work"}}
		}},
		{"security context", func(c []corev1.Container) {
			c[0].SecurityContext = &corev1.SecurityContext{RunAsNonRoot: new(true), AllowPrivilegeEscalation: new(false)}
		}},
		{"resources", func(c []corev1.Container) {
			c[0].Resources.Requests = corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("250m")}
		}},
		{"image pull policy", func(c []corev1.Container) { c[0].ImagePullPolicy = corev1.PullAlways }},
		{"working directory", func(c []corev1.Container) { c[0].WorkingDir = "/work" }},
		{"execution order", func(c []corev1.Container) { c[0], c[1] = c[1], c[0] }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			require.NoError(t, appsv1.AddToScheme(scheme))
			current := initializerWorkload()
			current.Spec.Template.Spec.InitContainers = []corev1.Container{
				{Name: "prepare", Image: "splunk/splunk:10.2.0", Command: []string{"/bin/sh"}, Args: []string{"-c", "echo initial"}},
				{Name: "finish", Image: "splunk/splunk:10.2.0"},
			}
			c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(current).Build()
			desired := current.DeepCopy()
			tt.change(desired.Spec.Template.Spec.InitContainers)
			expected := desired.DeepCopy()
			phase, err := ApplyStatefulSet(t.Context(), c, desired)
			require.NoError(t, err)
			assert.Equal(t, enterpriseApi.PhaseUpdating, phase)
			stored := &appsv1.StatefulSet{}
			require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(desired), stored))
			assert.True(t, apiequality.Semantic.DeepEqual(expected.Spec.Template.Spec.InitContainers, stored.Spec.Template.Spec.InitContainers))
			version := stored.ResourceVersion
			phase, err = ApplyStatefulSet(t.Context(), c, expected.DeepCopy())
			require.NoError(t, err)
			assert.Equal(t, enterpriseApi.PhaseReady, phase)
			require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(desired), stored))
			assert.Equal(t, version, stored.ResourceVersion)
		})
	}
}

func TestApplyStatefulSetIgnoresInitializerAPIDefaults(t *testing.T) {
	for _, tt := range []struct {
		image  string
		policy corev1.PullPolicy
	}{
		{"splunk/splunk:10.2.0", corev1.PullIfNotPresent},
		{"splunk/splunk:latest", corev1.PullAlways},
		{"splunk/splunk", corev1.PullAlways},
		{"splunk/splunk@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", corev1.PullIfNotPresent},
		{"registry.example.com:5000/team/splunk", corev1.PullAlways},
		{"registry.example.com:5000/team/splunk:latest", corev1.PullAlways},
		{"registry.example.com:5000/team/splunk:10.2.0", corev1.PullIfNotPresent},
		{"registry.example.com:5000/team/splunk@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", corev1.PullIfNotPresent},
		{"registry.example.com:5000/team/splunk:latest@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", corev1.PullAlways},
		{"registry.example.com:5000/team/splunk:10.2.0@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", corev1.PullIfNotPresent},
		{"[2001:db8::1]:5000/team/splunk", corev1.PullAlways},
		{"[2001:db8::1]:5000/team/splunk:10.2.0", corev1.PullIfNotPresent},
		{"splunk/splunk:Latest", corev1.PullIfNotPresent},
	} {
		t.Run(tt.image, func(t *testing.T) {
			scheme := runtime.NewScheme()
			require.NoError(t, appsv1.AddToScheme(scheme))
			desired := initializerWorkload()
			desired.Spec.Template.Spec.InitContainers = []corev1.Container{{Name: "prepare", Image: tt.image}}
			current := desired.DeepCopy()
			current.Spec.Template.Spec.InitContainers[0].ImagePullPolicy = tt.policy
			current.Spec.Template.Spec.InitContainers[0].TerminationMessagePath = "/dev/termination-log"
			current.Spec.Template.Spec.InitContainers[0].TerminationMessagePolicy = corev1.TerminationMessageReadFile
			c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(current).Build()
			stored := &appsv1.StatefulSet{}
			require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(current), stored))
			version := stored.ResourceVersion
			for range 2 {
				phase, err := ApplyStatefulSet(t.Context(), c, desired.DeepCopy())
				require.NoError(t, err)
				assert.Equal(t, enterpriseApi.PhaseReady, phase)
				require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(current), stored))
				assert.Equal(t, version, stored.ResourceVersion)
			}
		})
	}
}

func TestApplyStatefulSetIgnoresNestedInitializerAPIDefaults(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, appsv1.AddToScheme(scheme))
	desired := initializerWorkload()
	desired.Spec.Template.Spec.InitContainers = []corev1.Container{{
		Name: "prepare", Image: "splunk/splunk:10.2.0", ImagePullPolicy: corev1.PullIfNotPresent,
		TerminationMessagePath: "/dev/termination-log", TerminationMessagePolicy: corev1.TerminationMessageReadFile,
		RestartPolicy:  new(corev1.ContainerRestartPolicyAlways),
		Ports:          []corev1.ContainerPort{{ContainerPort: 8080}},
		Env:            []corev1.EnvVar{{Name: "POD_NAME", ValueFrom: &corev1.EnvVarSource{FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"}}}},
		StartupProbe:   &corev1.Probe{ProbeHandler: corev1.ProbeHandler{HTTPGet: &corev1.HTTPGetAction{Port: intstr.FromInt32(8080)}}},
		ReadinessProbe: &corev1.Probe{ProbeHandler: corev1.ProbeHandler{GRPC: &corev1.GRPCAction{Port: 8080}}},
		Lifecycle:      &corev1.Lifecycle{PostStart: &corev1.LifecycleHandler{HTTPGet: &corev1.HTTPGetAction{Port: intstr.FromInt32(8080)}}},
	}}
	current := desired.DeepCopy()
	init := &current.Spec.Template.Spec.InitContainers[0]
	init.Ports[0].Protocol = corev1.ProtocolTCP
	init.Env[0].ValueFrom.FieldRef.APIVersion = "v1"
	for _, probe := range []*corev1.Probe{init.StartupProbe, init.ReadinessProbe} {
		probe.TimeoutSeconds, probe.PeriodSeconds, probe.SuccessThreshold, probe.FailureThreshold = 1, 10, 1, 3
	}
	init.StartupProbe.HTTPGet.Path, init.StartupProbe.HTTPGet.Scheme = "/", corev1.URISchemeHTTP
	init.ReadinessProbe.GRPC.Service = new("")
	init.Lifecycle.PostStart.HTTPGet.Path, init.Lifecycle.PostStart.HTTPGet.Scheme = "/", corev1.URISchemeHTTP
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(current).Build()
	stored := &appsv1.StatefulSet{}
	require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(current), stored))
	version := stored.ResourceVersion
	phase, err := ApplyStatefulSet(t.Context(), c, desired.DeepCopy())
	require.NoError(t, err)
	assert.Equal(t, enterpriseApi.PhaseReady, phase)
	require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(current), stored))
	assert.Equal(t, version, stored.ResourceVersion, "server defaults must not trigger a rewrite")
}

func initializerWorkload() *appsv1.StatefulSet {
	return &appsv1.StatefulSet{Name: "search", Namespace: "ns", Spec: appsv1.StatefulSetSpec{
		Replicas: new(int32(1)), Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "search"}},
		Template: corev1.PodTemplateSpec{Labels: map[string]string{"app": "search"}, Spec: corev1.PodSpec{
			SecurityContext: &corev1.PodSecurityContext{RunAsUser: new(int64(41812)), FSGroup: new(int64(41812))},
			Volumes: []corev1.Volume{
				{Name: "mnt-splunk-var", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
				{Name: "mnt-splunk-etc", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
			},
			Containers: []corev1.Container{{Name: "splunk", Image: "splunk/splunk:10.2.0", VolumeMounts: []corev1.VolumeMount{
				{Name: "mnt-splunk-var", MountPath: "/opt/splunk/var"}, {Name: "mnt-splunk-etc", MountPath: "/opt/splunk/etc"},
			}}},
		}},
	}}
}
