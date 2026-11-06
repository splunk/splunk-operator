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
	"testing"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/splunk/splunk-operator/pkg/splunk/common"
	"github.com/splunk/splunk-operator/pkg/splunk/resources"
	"github.com/splunk/splunk-operator/pkg/splunk/splunkconfig"
	"github.com/splunk/splunk-operator/pkg/splunk/splunkconfig/kvservice"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func TestKVServiceDefaultsConfigMap(t *testing.T) {
	cr := &enterpriseApi.Standalone{
		TypeMeta:   metav1.TypeMeta{APIVersion: enterpriseApi.GroupVersion.String(), Kind: "Standalone"},
		ObjectMeta: metav1.ObjectMeta{Name: "s1", Namespace: "tenant2", UID: types.UID("s1-uid")},
	}
	set, err := splunkconfig.ForRole(common.SplunkStandalone).
		NonSensitiveConfDir("").
		With(kvservice.Conf("splunk-kvs2-kvservice-service.default:8066", "tenant2")).Build()
	require.NoError(t, err)
	owner := common.AsOwner(cr, true)
	cm, err := resources.NewDefaultsConfigMap(cr, set.NonSensitive, &owner)
	require.NoError(t, err)
	assert.Contains(t, cm.Name, "sok-standalone-defaults-")
	assert.Equal(t, "tenant2", cm.Namespace)
	require.NotEmpty(t, cm.OwnerReferences, "owner reference is required for the following checks")
	assert.Equal(t, cr.UID, cm.OwnerReferences[0].UID)
	assert.Equal(t, enterpriseApi.GroupVersion.String(), cm.OwnerReferences[0].APIVersion)
	require.NotNil(t, cm.Immutable)
	assert.True(t, *cm.Immutable)

	var defaults common.DefaultYML
	require.NoError(t, yaml.Unmarshal([]byte(cm.Data["conf-defaults.yml"]), &defaults))
	assert.Len(t, defaults.Splunk.Conf, 1)
	require.NotEmpty(t, defaults.Splunk.Conf, "configuration entry is required for the following checks")
	entry := defaults.Splunk.Conf[0]
	assert.Equal(t, "server", entry.ConfFileName)
	assert.Empty(t, entry.Value.Directory, "use the implicit system/local target")
	assert.Equal(t, "external", entry.Value.Stanzas["kvstore"]["defaultKVStoreType"])
	assert.Equal(t, "http://splunk-kvs2-kvservice-service.default:8066", entry.Value.Stanzas["kvstore"]["kvserviceConnectionString"])
	assert.NotContains(t, entry.Value.Stanzas["kvstore"], "kvserviceScope")
	assert.NotContains(t, entry.Value.Stanzas["kvstore"], "postgresMigrateOnStartup")
	assert.Equal(t, common.StanzaFields{"tenant": "tenant2", "mvl-enabled": "true"}, entry.Value.Stanzas["scs"])
	assert.NotContains(t, entry.Value.Stanzas["scs"], "environment")

	sts := makeStatefulSet()
	cm.AsStatefulSetOption()(sts)
	require.NotEmpty(t, sts.Spec.Template.Spec.Volumes, "volume is required for the following checks")
	require.NotNil(t, sts.Spec.Template.Spec.Volumes[0].ConfigMap, "ConfigMap source is required for the following checks")
	assert.Equal(t, "mnt-sok-conf-defaults", sts.Spec.Template.Spec.Volumes[0].Name)
	assert.Equal(t, cm.Name, sts.Spec.Template.Spec.Volumes[0].ConfigMap.Name)
	require.NotEmpty(t, sts.Spec.Template.Spec.Containers, "container is required for the following checks")
	require.NotEmpty(t, sts.Spec.Template.Spec.Containers[0].VolumeMounts, "volume mount is required for the following checks")
	require.NotEmpty(t, sts.Spec.Template.Spec.Containers[0].Env, "environment entry is required for the following checks")
	assert.Equal(t, "/mnt/sok-defaults", sts.Spec.Template.Spec.Containers[0].VolumeMounts[0].MountPath)
	assert.Equal(t, "/mnt/splunk-secrets/default.yml,/mnt/sok-defaults/conf-defaults.yml", sts.Spec.Template.Spec.Containers[0].Env[0].Value)
	set, err = splunkconfig.ForRole(common.SplunkStandalone).
		NonSensitiveConfDir("").
		With(kvservice.Conf("", "")).Build()
	require.NoError(t, err)
	assert.Equal(t, common.ConfFileStanzas{"kvstore": {"defaultKVStoreType": "local", "postgresMigrateOnStartup": "false"}}, set.NonSensitive[0].Value.Stanzas)
	changed, err := resources.NewDefaultsConfigMap(cr, set.NonSensitive, &owner)
	require.NoError(t, err)
	assert.NotEqual(t, cm.Name, changed.Name)
}
