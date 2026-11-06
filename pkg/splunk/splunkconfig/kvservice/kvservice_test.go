/*
Copyright (c) 2018-2026 Splunk Inc. All rights reserved.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package kvservice

import (
	"testing"

	"github.com/splunk/splunk-operator/pkg/splunk/common"
	"github.com/stretchr/testify/require"
)

func TestConnectionString(t *testing.T) {
	for _, address := range []string{"http://splunk-backend-kvservice-service.default:8066", "https://splunk-backend-kvservice-service.default:8066/", "splunk-backend-kvservice-service.default:8066"} {
		t.Run(address, func(t *testing.T) {
			set, err := Conf(address, "tenant1").Contribute(common.SplunkStandalone)
			require.NoError(t, err)
			require.Len(t, set.NonSensitive, 1)
			stanzas := set.NonSensitive[0].Value.Stanzas
			want := address
			if address == "splunk-backend-kvservice-service.default:8066" {
				want = "http://" + address
			}
			require.Equal(t, want, stanzas["kvstore"]["kvserviceConnectionString"])
			require.Equal(t, "external", stanzas["kvstore"]["defaultKVStoreType"])
			require.Equal(t, "true", stanzas["scs"]["mvl-enabled"])
			require.NotContains(t, stanzas["kvstore"], "kvserviceScope")
			require.NotContains(t, stanzas["scs"], "environment")
			require.Equal(t, "tenant1", stanzas["scs"]["tenant"])
		})
	}
}

func TestRejectInvalidEndpoint(t *testing.T) {
	_, err := Conf("splunk-backend-kvservice-service.default.svc.cluster.local:8066", "tenant1").Contribute(common.SplunkStandalone)
	require.Error(t, err)
}

func TestEmptyConnectionStringSelectsLocal(t *testing.T) {
	set, err := Conf("", "tenant1").Contribute(common.SplunkStandalone)
	require.NoError(t, err)
	require.Len(t, set.NonSensitive, 1)
	require.Equal(t, common.ConfFileStanzas{
		"kvstore": {"defaultKVStoreType": "local", "postgresMigrateOnStartup": "false"},
	}, set.NonSensitive[0].Value.Stanzas)
}

func TestExcludedRolesDoNotReceiveConnectionString(t *testing.T) {
	for _, role := range []common.InstanceType{common.SplunkDeployer, common.SplunkIngestor, common.SplunkKVService} {
		set, err := Conf("http://splunk-backend-kvservice-service.default:8066", "tenant1").Contribute(role)
		require.NoError(t, err)
		require.Empty(t, set.NonSensitive)
	}
}
