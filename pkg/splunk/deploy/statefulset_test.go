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

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestApplyStatefulSet(t *testing.T) {
	var replicas int32 = 1
	statefulSet := appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-stack1-indexer",
			Namespace: "test",
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &replicas,
		},
	}

	// test standalone instance
	c := newMockClient()
	c.errors["Get"] = fmt.Errorf("NotFound")
	err := ApplyStatefulSet(c, &statefulSet)
	if err != nil {
		t.Errorf("ApplyStatefulSet() returned %v; want nil", err)
	}
	c.checkCalls(t, true, "TestApplyStatefulSet", map[string][]mockFuncCall{
		"Get": {
			{metaName: "splunk-stack1-indexer"},
		},
		"Create": {
			{metaName: "splunk-stack1-indexer"},
		},
	})

	// run again; should be no changes
	c = newMockClient()
	c.errors["Get"] = nil
	c.getObj = &statefulSet
	err = ApplyStatefulSet(c, &statefulSet)
	if err != nil {
		t.Errorf("ApplyStatefulSet() re-run returned %v; want nil", err)
	}
	c.checkCalls(t, true, "TestApplyStatefulSet", map[string][]mockFuncCall{
		"Get": {
			{metaName: "splunk-stack1-indexer"},
		},
	})

	// test update existing
	c = newMockClient()
	var newReplicas int32 = 3
	c.getObj = &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-stack1-indexer",
			Namespace: "test",
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &newReplicas,
		},
	}
	err = ApplyStatefulSet(c, &statefulSet)
	if err != nil {
		t.Errorf("ApplyStatefulSet() returned %v; want nil", err)
	}
	c.checkCalls(t, true, "TestApplyStatefulSet", map[string][]mockFuncCall{
		"Get": {
			{metaName: "splunk-stack1-indexer"},
		},
		"Update": {
			{metaName: "splunk-stack1-indexer"},
		},
	})
}
