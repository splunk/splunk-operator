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

package enterprise

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	enterprisev1 "github.com/splunk/splunk-operator/pkg/apis/enterprise/v1alpha3"
	spltest "github.com/splunk/splunk-operator/pkg/splunk/test"
)

func init() {
	spltest.MockObjectCopiers = append(spltest.MockObjectCopiers, enterpriseObjectCopier)
}

// enterpriseObjectCopier is used to copy enterprise runtime.Objects
func enterpriseObjectCopier(dst, src runtime.Object) bool {
	switch src.(type) {
	case *enterprisev1.IndexerCluster:
		*dst.(*enterprisev1.IndexerCluster) = *src.(*enterprisev1.IndexerCluster)
	case *enterprisev1.LicenseMaster:
		*dst.(*enterprisev1.LicenseMaster) = *src.(*enterprisev1.LicenseMaster)
	case *enterprisev1.SearchHeadCluster:
		*dst.(*enterprisev1.SearchHeadCluster) = *src.(*enterprisev1.SearchHeadCluster)
	case *enterprisev1.Spark:
		*dst.(*enterprisev1.Spark) = *src.(*enterprisev1.Spark)
	case *enterprisev1.Standalone:
		*dst.(*enterprisev1.Standalone) = *src.(*enterprisev1.Standalone)
	default:
		return false
	}
	return true
}

func TestApplySplunkConfig(t *testing.T) {
	funcCalls := []spltest.MockFuncCall{
		{MetaName: "*v1.Secret-test-splunk-stack1-search-head-secrets"},
		{MetaName: "*v1.ConfigMap-test-splunk-stack1-search-head-defaults"},
	}
	createCalls := map[string][]spltest.MockFuncCall{"Get": funcCalls, "Create": funcCalls}
	updateCalls := map[string][]spltest.MockFuncCall{"Get": funcCalls}
	searchHeadCR := enterprisev1.SearchHeadCluster{
		TypeMeta: metav1.TypeMeta{
			Kind: "SearcHead",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
	}
	searchHeadCR.Spec.Defaults = "defaults-yaml"
	searchHeadRevised := searchHeadCR.DeepCopy()
	searchHeadRevised.Spec.Image = "splunk/test"
	reconcile := func(c *spltest.MockClient, cr interface{}) error {
		obj := cr.(*enterprisev1.SearchHeadCluster)
		_, err := ApplySplunkConfig(c, obj, obj.Spec.CommonSplunkSpec, SplunkSearchHead)
		return err
	}
	spltest.ReconcileTester(t, "TestApplySplunkConfig", &searchHeadCR, searchHeadRevised, createCalls, updateCalls, reconcile)

	// test search head with indexer reference
	secret := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-stack2-indexer-secrets",
			Namespace: "test",
		},
		Data: map[string][]byte{
			"idxc_secret": {'a', 'b'},
		},
	}
	searchHeadRevised.Spec.IndexerClusterRef.Name = "stack2"
	updateCalls["Get"] = []spltest.MockFuncCall{
		{MetaName: "*v1.Secret-test-splunk-stack2-indexer-secrets"},
		{MetaName: "*v1.Secret-test-splunk-stack1-search-head-secrets"},
		{MetaName: "*v1.ConfigMap-test-splunk-stack1-search-head-defaults"},
	}
	spltest.ReconcileTester(t, "TestApplySplunkConfig", &searchHeadCR, searchHeadRevised, createCalls, updateCalls, reconcile, &secret)

	// test indexer with license master
	indexerCR := enterprisev1.IndexerCluster{
		TypeMeta: metav1.TypeMeta{
			Kind: "IndexerCluster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
	}
	secret = corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-stack2-license-master-secrets",
			Namespace: "test",
		},
		Data: map[string][]byte{
			"pass4SymmKey": {'a', 'b'},
			"idxc_secret":  {'a', 'b'},
		},
	}
	indexerRevised := indexerCR.DeepCopy()
	indexerRevised.Spec.Image = "splunk/test"
	indexerRevised.Spec.LicenseMasterRef.Name = "stack2"
	reconcile = func(c *spltest.MockClient, cr interface{}) error {
		obj := cr.(*enterprisev1.IndexerCluster)
		_, err := ApplySplunkConfig(c, obj, obj.Spec.CommonSplunkSpec, SplunkIndexer)
		return err
	}
	funcCalls = []spltest.MockFuncCall{
		{MetaName: "*v1.Secret-test-splunk-stack2-license-master-secrets"},
		{MetaName: "*v1.Secret-test-splunk-stack2-license-master-secrets"},
		{MetaName: "*v1.Secret-test-splunk-stack1-indexer-secrets"},
	}
	createCalls = map[string][]spltest.MockFuncCall{"Get": {funcCalls[2]}, "Create": {funcCalls[2]}}
	updateCalls = map[string][]spltest.MockFuncCall{"Get": funcCalls}
	spltest.ReconcileTester(t, "TestApplySplunkConfig", &indexerCR, indexerRevised, createCalls, updateCalls, reconcile, &secret)
}
