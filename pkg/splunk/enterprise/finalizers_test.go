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
	"fmt"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	enterprisev1 "github.com/splunk/splunk-operator/pkg/apis/enterprise/v1alpha3"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	splctrl "github.com/splunk/splunk-operator/pkg/splunk/controller"
	spltest "github.com/splunk/splunk-operator/pkg/splunk/test"
)

func splunkDeletionTester(t *testing.T, cr splcommon.MetaObject, delete func(splcommon.MetaObject, splcommon.ControllerClient) (bool, error)) {
	var component string
	switch cr.GetObjectKind().GroupVersionKind().Kind {
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
		"app.kubernetes.io/part-of": fmt.Sprintf("splunk-%s-%s", cr.GetName(), component),
	}
	listOptsA := []client.ListOption{
		client.InNamespace("test"),
	}
	listOptsB := []client.ListOption{
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

	mockCalls := make(map[string][]spltest.MockFuncCall)
	wantDeleted := false
	if cr.GetObjectMeta().GetDeletionTimestamp() != nil {
		wantDeleted = true
		apiVersion, _ := schema.ParseGroupVersion(enterprisev1.APIVersion)
		mockCalls["Update"] = []spltest.MockFuncCall{
			{MetaName: fmt.Sprintf("*%s.%s-%s-%s", apiVersion.Version, cr.GetObjectKind().GroupVersionKind().Kind, cr.GetNamespace(), cr.GetName())},
		}
		if component != "indexer" {
			mockCalls["Delete"] = []spltest.MockFuncCall{
				{MetaName: "*v1.PersistentVolumeClaim-test-splunk-pvc-stack1-var"},
			}
			mockCalls["List"] = []spltest.MockFuncCall{
				{ListOpts: listOptsA},
				{ListOpts: listOptsB},
			}
			mockCalls["Get"] = []spltest.MockFuncCall{
				{MetaName: "*v1.Secret-test-splunk-test-secret"},
				{MetaName: "*v1.Secret-test-splunk-test-secret"},
				{MetaName: "*v1.Secret-test-splunk-test-monitoring-console-secret-v1"},
				{MetaName: "*v1.Service-test-splunk-test-monitoring-console-service"},
				{MetaName: "*v1.Service-test-splunk-test-monitoring-console-headless"},
				{MetaName: "*v1.ConfigMap-test-splunk-test-monitoring-console"},
				{MetaName: "*v1.ConfigMap-test-splunk-test-monitoring-console"},
				{MetaName: "*v1.Deployment-test-splunk-test-monitoring-console"},
			}
			mockCalls["Create"] = []spltest.MockFuncCall{
				{MetaName: "*v1.Secret-test-splunk-test-secret"},
				{MetaName: "*v1.Secret-test-splunk-test-monitoring-console-secret-v1"},
				{MetaName: "*v1.Service-test-splunk-test-monitoring-console-service"},
				{MetaName: "*v1.Service-test-splunk-test-monitoring-console-headless"},
				{MetaName: "*v1.ConfigMap-test-splunk-test-monitoring-console"},
				{MetaName: "*v1.Deployment-test-splunk-test-monitoring-console"},
			}
		} else if component == "indexer" {
			mockCalls["Delete"] = []spltest.MockFuncCall{
				{MetaName: "*v1.PersistentVolumeClaim-test-splunk-pvc-stack1-var"},
			}
			mockCalls["List"] = []spltest.MockFuncCall{
				{ListOpts: listOptsB},
			}
		}
	}

	c := spltest.NewMockClient()
	c.ListObj = &pvclist
	deleted, err := delete(cr, c)
	if deleted != wantDeleted || err != nil {
		t.Errorf("splctrl.CheckForDeletion() returned %t, %v; want %t, nil", deleted, err, wantDeleted)
	}
	c.CheckCalls(t, "Testsplctrl.CheckForDeletion", mockCalls)
}

func TestDeleteSplunkPvc(t *testing.T) {
	cr := enterprisev1.IndexerCluster{
		TypeMeta: metav1.TypeMeta{
			Kind: "IndexerCluster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
	}
	splunkDeletionTester(t, &cr, splctrl.CheckForDeletion)

	now := time.Now().Add(time.Second * 100)
	currentTime := metav1.NewTime(now)
	cr.ObjectMeta.DeletionTimestamp = &currentTime
	cr.ObjectMeta.Finalizers = []string{"enterprise.splunk.com/delete-pvc"}
	splunkDeletionTester(t, &cr, splctrl.CheckForDeletion)

	// try with unrecognized finalizer
	c := spltest.NewMockClient()
	cr.ObjectMeta.Finalizers = append(cr.ObjectMeta.Finalizers, "bad-finalizer")
	deleted, err := splctrl.CheckForDeletion(&cr, c)
	if deleted != false || err == nil {
		t.Errorf("splctrl.CheckForDeletion() returned %t, %v; want false, (error)", deleted, err)
	}
}
