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
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/splunk/splunk-operator/pkg/splunk/common"
	"github.com/splunk/splunk-operator/pkg/splunk/resources"
)

// --- helpers -----------------------------------------------------------------

func someEntries() []common.ConfFileEntry {
	return []common.ConfFileEntry{
		{
			ConfFileName: "outputs",
			Value: common.ConfFileValue{
				Directory: "/opt/splunk/etc/apps/100-sok/local",
				Stanzas:   common.ConfFileStanzas{"remote_queue:q": {"remote_queue.type": "sqs_smartbus"}},
			},
		},
	}
}

func differentEntries() []common.ConfFileEntry {
	return []common.ConfFileEntry{
		{
			ConfFileName: "inputs",
			Value: common.ConfFileValue{
				Directory: "/opt/splunk/etc/apps/100-sok/local",
				Stanzas:   common.ConfFileStanzas{"remote_queue:q": {"remote_queue.type": "sqs_smartbus"}},
			},
		},
	}
}

func makeStatefulSet() *appsv1.StatefulSet {
	return &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: "test-ss", Namespace: "ns"},
		Spec: appsv1.StatefulSetSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "splunk",
							Env: []corev1.EnvVar{
								{Name: "SPLUNK_DEFAULTS_URL", Value: "/mnt/splunk-secrets/default.yml"},
							},
						},
					},
				},
			},
		},
	}
}

// fakeCR returns a minimal resources.CRObject with namespace, kind, and name set.
func fakeCR(namespace, kind, name string) resources.CRObject {
	cm := &corev1.ConfigMap{}
	cm.Namespace = namespace
	cm.Name = name
	cm.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind(kind))
	return cm
}

// --- DefaultsConfigMapName ---------------------------------------------------

func TestDefaultsConfigMapName_StableForSameInput(t *testing.T) {
	name1, err := resources.DefaultsConfigMapName("IndexerCluster", "my-indexer", someEntries())
	require.NoError(t, err)
	name2, err := resources.DefaultsConfigMapName("IndexerCluster", "my-indexer", someEntries())
	require.NoError(t, err)
	assert.Equal(t, name1, name2)
}

func TestDefaultsConfigMapName_ChangesWithEntries(t *testing.T) {
	name1, err := resources.DefaultsConfigMapName("IndexerCluster", "cr", someEntries())
	require.NoError(t, err)
	name2, err := resources.DefaultsConfigMapName("IndexerCluster", "cr", differentEntries())
	require.NoError(t, err)
	assert.NotEqual(t, name1, name2)
}

func TestDefaultsConfigMapName_Format(t *testing.T) {
	name, err := resources.DefaultsConfigMapName("IndexerCluster", "my-indexer", someEntries())
	require.NoError(t, err)
	assert.Regexp(t, regexp.MustCompile(`^sok-indexercluster-defaults-[0-9a-f]{6}$`), name)
}

func TestDefaultsConfigMapName_KindIsLowercased(t *testing.T) {
	name, err := resources.DefaultsConfigMapName("IngestorCluster", "my-ingestor", someEntries())
	require.NoError(t, err)
	assert.Contains(t, name, "sok-ingestorcluster-defaults-")
}

// --- NewDefaultsConfigMap ----------------------------------------------------

func TestNewDefaultsConfigMap_Immutable(t *testing.T) {
	cm, err := resources.NewDefaultsConfigMap(fakeCR("ns", "IndexerCluster", "cr"), someEntries(), nil)
	require.NoError(t, err)
	require.NotNil(t, cm.Immutable)
	assert.True(t, *cm.Immutable)
}

func TestNewDefaultsConfigMap_DataKey(t *testing.T) {
	cm, err := resources.NewDefaultsConfigMap(fakeCR("ns", "IndexerCluster", "cr"), someEntries(), nil)
	require.NoError(t, err)
	_, ok := cm.Data["conf-defaults.yml"]
	assert.True(t, ok, "ConfigMap must have a 'conf-defaults.yml' data key")
}

func TestNewDefaultsConfigMap_YAMLRoundTrip(t *testing.T) {
	cm, err := resources.NewDefaultsConfigMap(fakeCR("ns", "IndexerCluster", "cr"), someEntries(), nil)
	require.NoError(t, err)

	var d common.DefaultYML
	err = yaml.Unmarshal([]byte(cm.Data["conf-defaults.yml"]), &d)
	require.NoError(t, err)
	require.Len(t, d.Splunk.Conf, 1)
	assert.Equal(t, "outputs", d.Splunk.Conf[0].ConfFileName)
}

func TestNewDefaultsConfigMap_Labels(t *testing.T) {
	cm, err := resources.NewDefaultsConfigMap(fakeCR("ns", "IndexerCluster", "my-indexer"), someEntries(), nil)
	require.NoError(t, err)
	assert.Equal(t, "my-indexer", cm.Labels[resources.LabelCRName])
	assert.Equal(t, "IndexerCluster", cm.Labels[resources.LabelCRKind])
}

func TestNewDefaultsConfigMap_NameMatchesConfigMapName(t *testing.T) {
	cm, err := resources.NewDefaultsConfigMap(fakeCR("my-ns", "IndexerCluster", "cr"), someEntries(), nil)
	require.NoError(t, err)

	expectedName, err := resources.DefaultsConfigMapName("IndexerCluster", "cr", someEntries())
	require.NoError(t, err)

	assert.Equal(t, expectedName, cm.Name)
	assert.Equal(t, "my-ns", cm.Namespace)
}

func TestNewDefaultsConfigMap_ChangedEntriesProduceDifferentName(t *testing.T) {
	cm1, err := resources.NewDefaultsConfigMap(fakeCR("ns", "IndexerCluster", "cr"), someEntries(), nil)
	require.NoError(t, err)
	cm2, err := resources.NewDefaultsConfigMap(fakeCR("ns", "IndexerCluster", "cr"), differentEntries(), nil)
	require.NoError(t, err)
	assert.NotEqual(t, cm1.Name, cm2.Name)
}

// --- DefaultsConfigMap.StatefulSetOption -------------------------------------

func TestStatefulSetOption_NoopForZeroValue(t *testing.T) {
	ss := makeStatefulSet()
	resources.DefaultsConfigMap{}.AsStatefulSetOption()(ss)
	assert.Empty(t, ss.Spec.Template.Spec.Volumes)
	assert.Empty(t, ss.Spec.Template.Spec.Containers[0].VolumeMounts)
}

func TestStatefulSetOption_AddsDefaultsURLWhenAbsent(t *testing.T) {
	cm, err := resources.NewDefaultsConfigMap(fakeCR("ns", "IndexerCluster", "cr"), someEntries(), nil)
	require.NoError(t, err)
	ss := makeStatefulSet()
	ss.Spec.Template.Spec.Containers[0].Env = nil
	cm.AsStatefulSetOption()(ss)

	var found string
	for _, e := range ss.Spec.Template.Spec.Containers[0].Env {
		if e.Name == "SPLUNK_DEFAULTS_URL" {
			found = e.Value
		}
	}
	assert.Equal(t, resources.DefaultsMountPath(), found)
}

func TestStatefulSetOption_AddsVolumeAndMount(t *testing.T) {
	cm, err := resources.NewDefaultsConfigMap(fakeCR("ns", "IndexerCluster", "cr"), someEntries(), nil)
	require.NoError(t, err)
	ss := makeStatefulSet()
	cm.AsStatefulSetOption()(ss)

	require.Len(t, ss.Spec.Template.Spec.Volumes, 1)
	vol := ss.Spec.Template.Spec.Volumes[0]
	require.NotNil(t, vol.VolumeSource.ConfigMap)
	assert.Equal(t, cm.Name, vol.VolumeSource.ConfigMap.LocalObjectReference.Name)

	require.Len(t, ss.Spec.Template.Spec.Containers[0].VolumeMounts, 1)
	mount := ss.Spec.Template.Spec.Containers[0].VolumeMounts[0]
	assert.Equal(t, vol.Name, mount.Name)
	assert.Equal(t, "/mnt/sok-defaults", mount.MountPath)
	assert.True(t, mount.ReadOnly)
}

func TestStatefulSetOption_AppendsDefaultsURL(t *testing.T) {
	cm, err := resources.NewDefaultsConfigMap(fakeCR("ns", "IndexerCluster", "cr"), someEntries(), nil)
	require.NoError(t, err)
	ss := makeStatefulSet()
	cm.AsStatefulSetOption()(ss)

	var found string
	for _, e := range ss.Spec.Template.Spec.Containers[0].Env {
		if e.Name == "SPLUNK_DEFAULTS_URL" {
			found = e.Value
		}
	}
	assert.Equal(t, "/mnt/splunk-secrets/default.yml,"+resources.DefaultsMountPath(), found)
}

// --- DefaultsMountPath -------------------------------------------------------

func TestDefaultsMountPath_HasExpectedValue(t *testing.T) {
	assert.Equal(t, "/mnt/sok-defaults/conf-defaults.yml", resources.DefaultsMountPath())
}
