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

package enterprise

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
)

const (
	testStack1ClusterManagerService             = "Service-test-splunk-stack1-" + splcommon.ClusterManager + "-service"
	testStack1ClusterManagerStatefulSet         = "StatefulSet-test-splunk-stack1-" + splcommon.ClusterManager
	testStack1ClusterManagerConfigMapSmartStore = "ConfigMap-test-splunk-stack1-clustermaster-smartstore"
	testStack1ClusterManagerSmartStore          = "splunk-stack1-clustermaster-smartstore"
	testStack1ClusterManagerID                  = "splunk-stack1-" + splcommon.ClusterManager + "-%s"

	testStack1LicenseManagerServiceTestService = "Service-test-splunk-stack1-" + splcommon.LicenseManager + "-service"
	testStack1LicenseManagerStatefulSet        = "StatefulSet-test-splunk-stack1-" + splcommon.LicenseManager
)

func loadFixture(t *testing.T, filename string) string {
	t.Helper()
	path := filepath.Join("testdata", "fixtures", filename)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("Failed to load fixture %s: %v", filename, err)
	}

	var compactJSON bytes.Buffer
	if err := json.Compact(&compactJSON, data); err != nil {
		t.Fatalf("Failed to compact JSON from fixture %s: %v", filename, err)
	}
	return compactJSON.String()
}
