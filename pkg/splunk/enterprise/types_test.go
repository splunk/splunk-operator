// Copyright (c) 2018-2022 Splunk Inc. All rights reserved.

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
	"testing"

	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
)

func TestInstanceType(t *testing.T) {
	var insType InstanceType
	insType = "standalone"
	if insType.ToString() != "standalone" {
		t.Errorf("Incorrect conversion")
	}

	// to_role
	instMap := map[InstanceType]string{
		SplunkStandalone:        "splunk_standalone",
		SplunkClusterManager:    "splunk_cluster_master",
		SplunkClusterMaster:     "splunk_cluster_master",
		SplunkSearchHead:        "splunk_search_head",
		SplunkIndexer:           "splunk_indexer",
		SplunkDeployer:          "splunk_deployer",
		SplunkLicenseMaster:     splcommon.LicenseManagerRole,
		SplunkLicenseManager:    splcommon.LicenseManagerRole,
		SplunkMonitoringConsole: "splunk_monitor",
	}
	for key, val := range instMap {
		if key.ToRole() != val {
			t.Errorf("Incorrect conversion")
		}
	}

	// to_kind
	instMap = map[InstanceType]string{
		SplunkStandalone:        "standalone",
		SplunkClusterManager:    "indexer",
		SplunkClusterMaster:     "indexer",
		SplunkSearchHead:        "search-head",
		SplunkIndexer:           "indexer",
		SplunkDeployer:          "search-head",
		SplunkLicenseMaster:     splcommon.LicenseManager,
		SplunkLicenseManager:    "license-manager",
		SplunkMonitoringConsole: "monitoring-console",
	}
	for key, val := range instMap {
		if key.ToKind() != val {
			t.Errorf("Incorrect conversion")
		}
	}

}
