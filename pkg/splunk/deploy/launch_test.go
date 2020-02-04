// Copyright (c) 2018-2020 Splunk Inc. All rights reserved.
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

package deploy

import (
	"fmt"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/splunk/splunk-operator/pkg/apis/enterprise/v1alpha2"
)

func TestLaunchDeployment(t *testing.T) {
	cr := v1alpha2.SplunkEnterprise{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
		Spec: v1alpha2.SplunkEnterpriseSpec{
			Topology: v1alpha2.SplunkTopologySpec{
				Standalones: 1,
			},
			EnableDFS:  true,
			Defaults:   "splunk-defaults",
			LicenseURL: "http://example.com/splunk.lic",
		},
	}

	// test standalone instance
	c := newMockClient()
	c.errors["Get"] = fmt.Errorf("NotFound")
	err := LaunchDeployment(&cr, c)
	if err != nil {
		t.Errorf("LaunchDeployment() returned %v; want nil", err)
	}
	c.checkCalls(t, true, "TestLaunchDeployment", map[string][]mockFuncCall{
		"Get": {
			{metaName: "splunk-stack1-spark-master-service"},
			{metaName: "splunk-stack1-spark-master"},
			{metaName: "splunk-stack1-spark-worker"},
			{metaName: "splunk-stack1-spark-worker-headless"},
			{metaName: "splunk-stack1-secrets"},
			{metaName: "splunk-stack1-defaults"},
			{metaName: "splunk-stack1-standalone"},
		},
		"Create": {
			{metaName: "splunk-stack1-spark-master-service"},
			{metaName: "splunk-stack1-spark-master"},
			{metaName: "splunk-stack1-spark-worker"},
			{metaName: "splunk-stack1-spark-worker-headless"},
			{metaName: "splunk-stack1-secrets"},
			{metaName: "splunk-stack1-defaults"},
			{metaName: "splunk-stack1-standalone"},
		},
	})

	// test cluster with DFS
	cr.Spec = v1alpha2.SplunkEnterpriseSpec{
		Topology: v1alpha2.SplunkTopologySpec{
			Indexers:    3,
			SearchHeads: 3,
		},
		EnableDFS:  true,
		Defaults:   "splunk-defaults",
		LicenseURL: "http://example.com/splunk.lic",
	}
	c = newMockClient()
	c.errors["Get"] = fmt.Errorf("NotFound")
	err = LaunchDeployment(&cr, c)
	if err != nil {
		t.Errorf("LaunchDeployment() returned %v; want nil", err)
	}
	c.checkCalls(t, true, "TestLaunchDeployment", map[string][]mockFuncCall{
		"Get": {
			{metaName: "splunk-stack1-spark-master-service"},
			{metaName: "splunk-stack1-spark-master"},
			{metaName: "splunk-stack1-spark-worker"},
			{metaName: "splunk-stack1-spark-worker-headless"},
			{metaName: "splunk-stack1-secrets"},
			{metaName: "splunk-stack1-defaults"},
			{metaName: "splunk-stack1-license-master-service"},
			{metaName: "splunk-stack1-license-master"},
			{metaName: "splunk-stack1-cluster-master-service"},
			{metaName: "splunk-stack1-cluster-master"},
			{metaName: "splunk-stack1-deployer-service"},
			{metaName: "splunk-stack1-deployer"},
			{metaName: "splunk-stack1-indexer"},
			{metaName: "splunk-stack1-indexer-headless"},
			{metaName: "splunk-stack1-indexer-service"},
			{metaName: "splunk-stack1-search-head"},
			{metaName: "splunk-stack1-search-head-headless"},
			{metaName: "splunk-stack1-search-head-service"},
		},
		"Create": {
			{metaName: "splunk-stack1-spark-master-service"},
			{metaName: "splunk-stack1-spark-master"},
			{metaName: "splunk-stack1-spark-worker"},
			{metaName: "splunk-stack1-spark-worker-headless"},
			{metaName: "splunk-stack1-secrets"},
			{metaName: "splunk-stack1-defaults"},
			{metaName: "splunk-stack1-license-master-service"},
			{metaName: "splunk-stack1-license-master"},
			{metaName: "splunk-stack1-cluster-master-service"},
			{metaName: "splunk-stack1-cluster-master"},
			{metaName: "splunk-stack1-deployer-service"},
			{metaName: "splunk-stack1-deployer"},
			{metaName: "splunk-stack1-indexer"},
			{metaName: "splunk-stack1-indexer-headless"},
			{metaName: "splunk-stack1-indexer-service"},
			{metaName: "splunk-stack1-search-head"},
			{metaName: "splunk-stack1-search-head-headless"},
			{metaName: "splunk-stack1-search-head-service"},
		},
	})
}
