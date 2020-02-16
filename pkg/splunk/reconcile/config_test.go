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

	enterprisev1 "github.com/splunk/splunk-operator/pkg/apis/enterprise/v1alpha2"
	"github.com/splunk/splunk-operator/pkg/splunk/enterprise"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestReconcileSplunkConfig(t *testing.T) {
	funcCalls := []mockFuncCall{
		{metaName: "*v1.Secret-test-splunk-stack1-search-head-secrets"},
		{metaName: "*v1.ConfigMap-test-splunk-stack1-search-head-defaults"},
	}
	createCalls := map[string][]mockFuncCall{"Get": funcCalls, "Create": funcCalls}
	updateCalls := map[string][]mockFuncCall{"Get": funcCalls}
	searchHeadCR := enterprisev1.SearchHead{
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
	reconcile := func(c *mockClient, cr interface{}) error {
		obj := cr.(*enterprisev1.SearchHead)
		return ReconcileSplunkConfig(c, obj, obj.Spec.CommonSplunkSpec, enterprise.SplunkSearchHead)
	}
	reconcileTester(t, "TestReconcileSplunkConfig", &searchHeadCR, searchHeadRevised, createCalls, updateCalls, reconcile)

	// test search head with indexer reference
	secret := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-stack2-indexer-secrets",
			Namespace: "test",
		},
		Data: map[string][]byte{
			"idxc_secret": []byte{'a', 'b'},
		},
	}
	searchHeadRevised.Spec.IndexerRef.Name = "stack2"
	updateCalls["Get"] = []mockFuncCall{
		{metaName: "*v1.Secret-test-splunk-stack2-indexer-secrets"},
		{metaName: "*v1.Secret-test-splunk-stack1-search-head-secrets"},
		{metaName: "*v1.ConfigMap-test-splunk-stack1-search-head-defaults"},
	}
	reconcileTester(t, "TestReconcileSplunkConfig", &searchHeadCR, searchHeadRevised, createCalls, updateCalls, reconcile, &secret)

	// test indexer with license master
	indexerCR := enterprisev1.Indexer{
		TypeMeta: metav1.TypeMeta{
			Kind: "Indexer",
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
			"pass4SymmKey": []byte{'a', 'b'},
			"idxc_secret":  []byte{'a', 'b'},
		},
	}
	indexerRevised := indexerCR.DeepCopy()
	indexerRevised.Spec.Image = "splunk/test"
	indexerRevised.Spec.LicenseMasterRef.Name = "stack2"
	reconcile = func(c *mockClient, cr interface{}) error {
		obj := cr.(*enterprisev1.Indexer)
		return ReconcileSplunkConfig(c, obj, obj.Spec.CommonSplunkSpec, enterprise.SplunkIndexer)
	}
	funcCalls = []mockFuncCall{
		{metaName: "*v1.Secret-test-splunk-stack2-license-master-secrets"},
		{metaName: "*v1.Secret-test-splunk-stack2-license-master-secrets"},
		{metaName: "*v1.Secret-test-splunk-stack1-indexer-secrets"},
	}
	createCalls = map[string][]mockFuncCall{"Get": {funcCalls[2]}, "Create": {funcCalls[2]}}
	updateCalls = map[string][]mockFuncCall{"Get": funcCalls}
	reconcileTester(t, "TestReconcileSplunkConfig", &indexerCR, indexerRevised, createCalls, updateCalls, reconcile, &secret)
}

func TestApplyConfigMap(t *testing.T) {
	funcCalls := []mockFuncCall{{metaName: "*v1.ConfigMap-test-defaults"}}
	createCalls := map[string][]mockFuncCall{"Get": funcCalls, "Create": funcCalls}
	updateCalls := map[string][]mockFuncCall{"Get": funcCalls, "Update": funcCalls}
	current := corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "defaults",
			Namespace: "test",
		},
	}
	revised := current.DeepCopy()
	revised.Data = map[string]string{"a": "b"}
	reconcile := func(c *mockClient, cr interface{}) error {
		return ApplyConfigMap(c, cr.(*corev1.ConfigMap))
	}
	reconcileTester(t, "TestApplyConfigMap", &current, revised, createCalls, updateCalls, reconcile)
}

func TestApplySecret(t *testing.T) {
	funcCalls := []mockFuncCall{{metaName: "*v1.Secret-test-secrets"}}
	createCalls := map[string][]mockFuncCall{"Get": funcCalls, "Create": funcCalls}
	updateCalls := map[string][]mockFuncCall{"Get": funcCalls}
	current := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "secrets",
			Namespace: "test",
		},
	}
	revised := current.DeepCopy()
	revised.Data = map[string][]byte{"a": []byte{'1', '2'}}
	reconcile := func(c *mockClient, cr interface{}) error {
		return ApplySecret(c, cr.(*corev1.Secret))
	}
	reconcileTester(t, "TestApplySecret", &current, revised, createCalls, updateCalls, reconcile)
}
