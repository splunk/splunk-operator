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
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	enterprisev1 "github.com/splunk/splunk-operator/pkg/apis/enterprise/v1alpha2"
)

func TestReconcileSplunkEnterprise(t *testing.T) {
	// test standalone
	createCalls := map[string][]mockFuncCall{
		"Get": []mockFuncCall{
			{metaName: "*v1alpha2.Standalone-test-stack1"},
			{metaName: "*v1alpha2.Spark-test-stack1"},
		},
		"Create": []mockFuncCall{
			{metaName: "*v1alpha2.Standalone-test-stack1"},
			{metaName: "*v1alpha2.Spark-test-stack1"},
		},
	}
	updateCalls := map[string][]mockFuncCall{
		"Get": []mockFuncCall{
			{metaName: "*v1alpha2.Indexer-test-stack1"},
			{metaName: "*v1alpha2.SearchHead-test-stack1"},
			{metaName: "*v1alpha2.LicenseMaster-test-stack1"},
			{metaName: "*v1alpha2.Spark-test-stack1"},
		},
		"Create": []mockFuncCall{
			{metaName: "*v1alpha2.Indexer-test-stack1"},
			{metaName: "*v1alpha2.SearchHead-test-stack1"},
			{metaName: "*v1alpha2.LicenseMaster-test-stack1"},
		},
		"Update": []mockFuncCall{
			{metaName: "*v1alpha2.Spark-test-stack1"},
		},
	}
	current := enterprisev1.SplunkEnterprise{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
		Spec: enterprisev1.SplunkEnterpriseSpec{
			Topology: enterprisev1.SplunkTopologySpec{
				Standalones: 1,
			},
			EnableDFS:  true,
			Defaults:   "splunk-defaults",
			LicenseURL: "http://example.com/splunk.lic",
		},
	}
	revised := current.DeepCopy()
	revised.Spec = enterprisev1.SplunkEnterpriseSpec{
		Topology: enterprisev1.SplunkTopologySpec{
			Indexers:     3,
			SearchHeads:  3,
			SparkWorkers: 3,
		},
		EnableDFS:  true,
		Defaults:   "splunk-defaults",
		LicenseURL: "http://example.com/splunk.lic",
	}
	reconcile := func(c *mockClient, cr interface{}) error {
		return ReconcileSplunkEnterprise(c, cr.(*enterprisev1.SplunkEnterprise))
	}
	reconcileTester(t, "TestReconcileSplunkEnterprise", &current, revised, createCalls, updateCalls, reconcile)
}
