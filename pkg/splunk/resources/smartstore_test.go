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
	"github.com/splunk/splunk-operator/pkg/splunk/resources"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPrepareSmartstoreConfigMap(t *testing.T) {
	configMap := resources.PrepareSmartstoreConfigMap("smartstore", "test", "defaults", "volumes", "indexes", "server")

	require.NotNil(t, configMap)
	assert.Equal(t, "smartstore", configMap.Name)
	assert.Equal(t, "test", configMap.Namespace)
	assert.Equal(t, "defaults volumes indexes", configMap.Data["indexes.conf"])
	assert.Equal(t, "server", configMap.Data["server.conf"])
}

func TestSmartstoreConfigBuilders(t *testing.T) {
	indexes := resources.GetSmartstoreIndexesConfig([]enterpriseApi.IndexSpec{{
		Name:       "salesdata",
		RemotePath: "remote",
		IndexAndGlobalCommonSpec: enterpriseApi.IndexAndGlobalCommonSpec{
			VolName: "s3-volume",
		},
	}})
	assert.Contains(t, indexes, "[salesdata]")
	assert.Contains(t, indexes, "remotePath = volume:s3-volume/remote")

	server := resources.GetServerConfigEntries(&enterpriseApi.CacheManagerSpec{MaxCacheSizeMB: 1024})
	assert.Contains(t, server, "[cachemanager]")
	assert.Contains(t, server, "max_cache_size = 1024")
	assert.Empty(t, resources.GetServerConfigEntries(nil))

	defaults := resources.GetSmartstoreIndexesDefaults(enterpriseApi.IndexConfDefaultsSpec{IndexAndGlobalCommonSpec: enterpriseApi.IndexAndGlobalCommonSpec{VolName: "s3-volume"}})
	assert.Contains(t, defaults, "[default]")
	assert.Contains(t, defaults, "remotePath = volume:s3-volume/$_index_name")
}
