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

	"github.com/splunk/splunk-operator/pkg/splunk/common"
	"github.com/splunk/splunk-operator/pkg/splunk/resources"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// --- helpers -----------------------------------------------------------------

func someCredEntries() []common.ConfFileEntry {
	return []common.ConfFileEntry{
		{
			ConfFileName: "outputs",
			Value: common.ConfFileValue{
				Directory: "/opt/splunk/etc/apps/101-sok-secrets/local",
				Stanzas: common.ConfFileStanzas{"remote_queue:q": {
					"remote_queue.sqs_smartbus.access_key": "AKIA",
					"remote_queue.sqs_smartbus.secret_key": "shhh",
				}},
			},
		},
	}
}

func differentCredEntries() []common.ConfFileEntry {
	return []common.ConfFileEntry{
		{
			ConfFileName: "outputs",
			Value: common.ConfFileValue{
				Directory: "/opt/splunk/etc/apps/101-sok-secrets/local",
				Stanzas: common.ConfFileStanzas{"remote_queue:q": {
					"remote_queue.sqs_smartbus.access_key": "AKIA",
					"remote_queue.sqs_smartbus.secret_key": "rotated",
				}},
			},
		},
	}
}

// --- DefaultsSecretName ------------------------------------------------------

func TestDefaultsSecretName_StableForSameInput(t *testing.T) {
	name1, err := resources.DefaultsSecretName("IndexerCluster", "my-indexer", someCredEntries())
	require.NoError(t, err)
	name2, err := resources.DefaultsSecretName("IndexerCluster", "my-indexer", someCredEntries())
	require.NoError(t, err)
	assert.Equal(t, name1, name2)
}

func TestDefaultsSecretName_ChangesWithEntries(t *testing.T) {
	name1, err := resources.DefaultsSecretName("IndexerCluster", "cr", someCredEntries())
	require.NoError(t, err)
	name2, err := resources.DefaultsSecretName("IndexerCluster", "cr", differentCredEntries())
	require.NoError(t, err)
	assert.NotEqual(t, name1, name2)
}

func TestDefaultsSecretName_Format(t *testing.T) {
	name, err := resources.DefaultsSecretName("IndexerCluster", "my-indexer", someCredEntries())
	require.NoError(t, err)
	assert.Regexp(t, regexp.MustCompile(`^sok-indexercluster-creds-[0-9a-f]{6}$`), name)
}

func TestDefaultsSecretName_KindIsLowercased(t *testing.T) {
	name, err := resources.DefaultsSecretName("IngestorCluster", "my-ingestor", someCredEntries())
	require.NoError(t, err)
	assert.Contains(t, name, "sok-ingestorcluster-creds-")
}

// --- NewDefaultsSecret -------------------------------------------------------

func TestNewDefaultsSecret_Immutable(t *testing.T) {
	s, err := resources.NewDefaultsSecret(fakeCR("ns", "IndexerCluster", "cr"), someCredEntries(), nil)
	require.NoError(t, err)
	require.NotNil(t, s.Immutable)
	assert.True(t, *s.Immutable)
}

func TestNewDefaultsSecret_DataKey(t *testing.T) {
	s, err := resources.NewDefaultsSecret(fakeCR("ns", "IndexerCluster", "cr"), someCredEntries(), nil)
	require.NoError(t, err)
	_, ok := s.Data["conf-defaults.yml"]
	assert.True(t, ok, "Secret must have a 'conf-defaults.yml' data key")
}

func TestNewDefaultsSecret_YAMLRoundTrip(t *testing.T) {
	s, err := resources.NewDefaultsSecret(fakeCR("ns", "IndexerCluster", "cr"), someCredEntries(), nil)
	require.NoError(t, err)

	var d common.DefaultYML
	err = yaml.Unmarshal(s.Data["conf-defaults.yml"], &d)
	require.NoError(t, err)
	require.Len(t, d.Splunk.Conf, 1)
	assert.Equal(t, "outputs", d.Splunk.Conf[0].ConfFileName)
	assert.Equal(t, "/opt/splunk/etc/apps/101-sok-secrets/local", d.Splunk.Conf[0].Value.Directory)
}

func TestNewDefaultsSecret_Labels(t *testing.T) {
	s, err := resources.NewDefaultsSecret(fakeCR("ns", "IndexerCluster", "my-indexer"), someCredEntries(), nil)
	require.NoError(t, err)
	assert.Equal(t, "my-indexer", s.Labels[resources.LabelCRName])
	assert.Equal(t, "IndexerCluster", s.Labels[resources.LabelCRKind])
}

func TestNewDefaultsSecret_NameMatchesSecretName(t *testing.T) {
	s, err := resources.NewDefaultsSecret(fakeCR("my-ns", "IndexerCluster", "cr"), someCredEntries(), nil)
	require.NoError(t, err)

	expectedName, err := resources.DefaultsSecretName("IndexerCluster", "cr", someCredEntries())
	require.NoError(t, err)

	assert.Equal(t, expectedName, s.Name)
	assert.Equal(t, "my-ns", s.Namespace)
}

func TestNewDefaultsSecret_ChangedEntriesProduceDifferentName(t *testing.T) {
	s1, err := resources.NewDefaultsSecret(fakeCR("ns", "IndexerCluster", "cr"), someCredEntries(), nil)
	require.NoError(t, err)
	s2, err := resources.NewDefaultsSecret(fakeCR("ns", "IndexerCluster", "cr"), differentCredEntries(), nil)
	require.NoError(t, err)
	assert.NotEqual(t, s1.Name, s2.Name)
}

// --- DefaultsSecret.AsStatefulSetOption --------------------------------------

func TestSecretStatefulSetOption_NoopForZeroValue(t *testing.T) {
	ss := makeStatefulSet()
	resources.DefaultsSecret{}.AsStatefulSetOption()(ss)
	assert.Empty(t, ss.Spec.Template.Spec.Volumes)
	assert.Empty(t, ss.Spec.Template.Spec.Containers[0].VolumeMounts)
}

func TestSecretStatefulSetOption_AddsVolumeAndMount(t *testing.T) {
	s, err := resources.NewDefaultsSecret(fakeCR("ns", "IndexerCluster", "cr"), someCredEntries(), nil)
	require.NoError(t, err)
	ss := makeStatefulSet()
	s.AsStatefulSetOption()(ss)

	require.Len(t, ss.Spec.Template.Spec.Volumes, 1)
	vol := ss.Spec.Template.Spec.Volumes[0]
	require.NotNil(t, vol.VolumeSource.Secret)
	assert.Equal(t, s.Name, vol.VolumeSource.Secret.SecretName)

	require.Len(t, ss.Spec.Template.Spec.Containers[0].VolumeMounts, 1)
	mount := ss.Spec.Template.Spec.Containers[0].VolumeMounts[0]
	assert.Equal(t, vol.Name, mount.Name)
	assert.Equal(t, "/mnt/sok-conf-secrets", mount.MountPath)
	assert.True(t, mount.ReadOnly)
}

func TestSecretStatefulSetOption_AppendsDefaultsURL(t *testing.T) {
	s, err := resources.NewDefaultsSecret(fakeCR("ns", "IndexerCluster", "cr"), someCredEntries(), nil)
	require.NoError(t, err)
	ss := makeStatefulSet()
	s.AsStatefulSetOption()(ss)

	var found string
	for _, e := range ss.Spec.Template.Spec.Containers[0].Env {
		if e.Name == "SPLUNK_DEFAULTS_URL" {
			found = e.Value
		}
	}
	assert.Equal(t, "/mnt/splunk-secrets/default.yml,"+resources.SecretMountPath(), found)
}

// --- SecretMountPath ----------------------------------------------------------

func TestSecretMountPath_HasExpectedValue(t *testing.T) {
	assert.Equal(t, "/mnt/sok-conf-secrets/conf-defaults.yml", resources.SecretMountPath())
}
