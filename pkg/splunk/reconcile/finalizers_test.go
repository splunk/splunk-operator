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
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	enterprisev1 "github.com/splunk/splunk-operator/pkg/apis/enterprise/v1alpha2"
)

func TestCheckSplunkDeletion(t *testing.T) {
	now := time.Now().Add(time.Second * 100)
	currentTime := metav1.NewTime(now)
	cr := enterprisev1.Indexer{
		TypeMeta: metav1.TypeMeta{
			Kind: "Indexer",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:              "stack1",
			Namespace:         "test",
			DeletionTimestamp: &currentTime,
			Finalizers: []string{
				"enterprise.splunk.com/delete-pvc",
			},
		},
	}

	c := newMockClient()
	labels := map[string]string{
		"app":  "splunk",
		"for":  cr.GetIdentifier(),
		"kind": "indexer",
	}
	listOpts := []client.ListOption{
		client.InNamespace(cr.GetNamespace()),
		client.MatchingLabels(labels),
	}
	pvclist := corev1.PersistentVolumeClaimList{
		Items: []corev1.PersistentVolumeClaim{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "splunk-pvc-stack1-var",
					Namespace: "test",
				},
			},
		},
	}
	c.listObj = &pvclist
	deleted, err := CheckSplunkDeletion(&cr, c)
	if deleted != true || err != nil {
		t.Errorf("CheckSplunkDeletion() returned %t, %v; want true, nil", deleted, err)
	}
	c.checkCalls(t, "TestCheckSplunkDeletion", map[string][]mockFuncCall{
		"Update": {
			{metaName: "*v1alpha2.Indexer-test-stack1"},
		},
		"Delete": {
			{metaName: "*v1.PersistentVolumeClaim-test-splunk-pvc-stack1-var"},
		},
		"List": {
			{listOpts: listOpts},
		},
	})

	// try with unrecognized finalizer
	c = newMockClient()
	cr.ObjectMeta.Finalizers = append(cr.ObjectMeta.Finalizers, "bad-finalizer")
	deleted, err = CheckSplunkDeletion(&cr, c)
	if deleted != false || err == nil {
		t.Errorf("CheckSplunkDeletion() returned %t, %v; want false, (error)", deleted, err)
	}
}
