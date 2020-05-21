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

package reconcile

import (
	"fmt"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	enterprisev1 "github.com/splunk/splunk-operator/pkg/apis/enterprise/v1alpha3"
)

func splunkDeletionTester(t *testing.T, cr enterprisev1.MetaObject, delete func(enterprisev1.MetaObject, ControllerClient) (bool, error)) {
	var component string
	switch cr.GetTypeMeta().Kind {
	case "Standalone":
		component = "standalone"
	case "LicenseMaster":
		component = "license-master"
	case "SearchHeadCluster":
		component = "search-head"
	case "IndexerCluster":
		component = "indexer"
	}

	labels := map[string]string{
		"app.kubernetes.io/part-of": fmt.Sprintf("splunk-%s-%s", cr.GetIdentifier(), component),
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

	mockCalls := make(map[string][]mockFuncCall)
	wantDeleted := false
	if cr.GetObjectMeta().GetDeletionTimestamp() != nil {
		wantDeleted = true
		mockCalls["Update"] = []mockFuncCall{
			{metaName: fmt.Sprintf("*v1alpha3.%s-%s-%s", cr.GetTypeMeta().Kind, cr.GetNamespace(), cr.GetIdentifier())},
		}
		if component != "" {
			mockCalls["Delete"] = []mockFuncCall{
				{metaName: "*v1.PersistentVolumeClaim-test-splunk-pvc-stack1-var"},
			}
			mockCalls["List"] = []mockFuncCall{
				{listOpts: listOpts},
			}
		}
	}

	c := newMockClient()
	c.listObj = &pvclist
	deleted, err := delete(cr, c)
	if deleted != wantDeleted || err != nil {
		t.Errorf("CheckSplunkDeletion() returned %t, %v; want %t, nil", deleted, err, wantDeleted)
	}
	c.checkCalls(t, "TestCheckSplunkDeletion", mockCalls)
}

func TestCheckSplunkDeletion(t *testing.T) {
	cr := enterprisev1.IndexerCluster{
		TypeMeta: metav1.TypeMeta{
			Kind: "IndexerCluster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
	}
	splunkDeletionTester(t, &cr, CheckSplunkDeletion)

	now := time.Now().Add(time.Second * 100)
	currentTime := metav1.NewTime(now)
	cr.ObjectMeta.DeletionTimestamp = &currentTime
	cr.ObjectMeta.Finalizers = []string{"enterprise.splunk.com/delete-pvc"}
	splunkDeletionTester(t, &cr, CheckSplunkDeletion)

	// try with unrecognized finalizer
	c := newMockClient()
	cr.ObjectMeta.Finalizers = append(cr.ObjectMeta.Finalizers, "bad-finalizer")
	deleted, err := CheckSplunkDeletion(&cr, c)
	if deleted != false || err == nil {
		t.Errorf("CheckSplunkDeletion() returned %t, %v; want false, (error)", deleted, err)
	}
}
