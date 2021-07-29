// Copyright (c) 2018-2021 Splunk Inc. All rights reserved.
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

	enterpriseApi "github.com/splunk/splunk-operator/pkg/apis/enterprise/v2"
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
	case "ClusterMaster":
		component = "cluster-master"
	case "MonitoringConsole":
		component = "monitoring-console"
	}

	labelsB := map[string]string{
		"app.kubernetes.io/instance": fmt.Sprintf("splunk-%s-%s", cr.GetName(), component),
	}

	listOptsB := []client.ListOption{
		client.InNamespace(cr.GetNamespace()),
		client.MatchingLabels(labelsB),
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
		apiVersion, _ := schema.ParseGroupVersion(enterpriseApi.APIVersion)
		mockCalls["Update"] = []spltest.MockFuncCall{
			{MetaName: fmt.Sprintf("*%s.%s-%s-%s", apiVersion.Version, cr.GetObjectKind().GroupVersionKind().Kind, cr.GetNamespace(), cr.GetName())},
		}
		if cr.GetObjectKind().GroupVersionKind().Kind != "IndexerCluster" {
			mockCalls["Update"] = []spltest.MockFuncCall{
				{MetaName: "*v1.Secret-test-splunk-test-secret"},
				{MetaName: "*v1.Secret-test-splunk-test-secret"},
				{MetaName: fmt.Sprintf("*%s.%s-%s-%s", apiVersion.Version, cr.GetObjectKind().GroupVersionKind().Kind, cr.GetNamespace(), cr.GetName())},
			}
			mockCalls["Delete"] = []spltest.MockFuncCall{
				{MetaName: "*v1.PersistentVolumeClaim-test-splunk-pvc-stack1-var"},
			}
			mockCalls["List"] = []spltest.MockFuncCall{
				{ListOpts: listOptsB},
			}
			// account for extra calls in the shc case due to the deployer
			if component == "search-head" {
				labelsC := map[string]string{
					"app.kubernetes.io/instance": fmt.Sprintf("splunk-%s-%s", cr.GetName(), "deployer"),
				}
				listOptsC := []client.ListOption{
					client.InNamespace(cr.GetNamespace()),
					client.MatchingLabels(labelsC),
				}
				mockCalls["Delete"] = append(mockCalls["Delete"], spltest.MockFuncCall{MetaName: "*v1.PersistentVolumeClaim-test-splunk-pvc-stack1-var"})
				mockCalls["List"] = append(mockCalls["List"], spltest.MockFuncCall{ListOpts: listOptsC})
			}
			mockCalls["Get"] = []spltest.MockFuncCall{
				{MetaName: "*v1.Secret-test-splunk-test-secret"},
				{MetaName: "*v1.Secret-test-splunk-test-secret"},
				{MetaName: "*v1.Secret-test-splunk-test-secret"},
			}
			mockCalls["Create"] = []spltest.MockFuncCall{
				{MetaName: "*v1.Secret-test-splunk-test-secret"},
			}
			if component == "monitoring-console" {
				mockCalls["Get"] = []spltest.MockFuncCall{
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
				}
				mockCalls["Update"] = []spltest.MockFuncCall{
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: fmt.Sprintf("*%s.%s-%s-%s", apiVersion.Version, cr.GetObjectKind().GroupVersionKind().Kind, cr.GetNamespace(), cr.GetName())},
				}
			}
		} else {
			mockCalls["Update"] = []spltest.MockFuncCall{
				{MetaName: "*v1.Secret-test-splunk-test-secret"},
				{MetaName: "*v1.Secret-test-splunk-test-secret"},
				{MetaName: fmt.Sprintf("*%s.%s-%s-%s", apiVersion.Version, cr.GetObjectKind().GroupVersionKind().Kind, cr.GetNamespace(), cr.GetName())},
			}
			mockCalls["Delete"] = []spltest.MockFuncCall{
				{MetaName: "*v1.PersistentVolumeClaim-test-splunk-pvc-stack1-var"},
			}
			mockCalls["List"] = []spltest.MockFuncCall{
				{ListOpts: listOptsB},
			}
			mockCalls["Create"] = []spltest.MockFuncCall{
				{MetaName: "*v1.Secret-test-splunk-test-secret"},
			}
			mockCalls["Get"] = []spltest.MockFuncCall{
				{MetaName: "*v1.Secret-test-splunk-test-secret"},
				{MetaName: "*v1.Secret-test-splunk-test-secret"},
				{MetaName: "*v2.ClusterMaster-test-master1"},
				{MetaName: "*v1.Secret-test-splunk-test-secret"},
			}
		}
	}

	c := spltest.NewMockClient()
	c.ListObj = &pvclist
	var err error
	if err != nil {
		return
	}

	deleted, err := delete(cr, c)
	if deleted != wantDeleted || err != nil {
		t.Errorf("splctrl.CheckForDeletion() returned %t, %v; want %t, nil", deleted, err, wantDeleted)
	}
	c.CheckCalls(t, "Testsplctrl.CheckForDeletion", mockCalls)
}

func splunkPVCDeletionTester(t *testing.T, cr splcommon.MetaObject, delete func(splcommon.MetaObject, splcommon.ControllerClient) (bool, error)) {
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
	case "ClusterMaster":
		component = "cluster-master"
	case "MonitoringConsole":
		component = "monitoring-console"
	}

	labels := map[string]string{
		"app.kubernetes.io/instance": fmt.Sprintf("splunk-%s-%s", cr.GetName(), component),
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

	mockCalls := make(map[string][]spltest.MockFuncCall)
	wantDeleted := false
	if cr.GetObjectMeta().GetDeletionTimestamp() != nil {
		wantDeleted = true
		apiVersion, _ := schema.ParseGroupVersion(enterpriseApi.APIVersion)
		mockCalls["Update"] = []spltest.MockFuncCall{
			{MetaName: fmt.Sprintf("*%s.%s-%s-%s", apiVersion.Version, cr.GetObjectKind().GroupVersionKind().Kind, cr.GetNamespace(), cr.GetName())},
		}
		mockCalls["Delete"] = []spltest.MockFuncCall{
			{MetaName: "*v1.PersistentVolumeClaim-test-splunk-pvc-stack1-var"},
		}
		mockCalls["List"] = []spltest.MockFuncCall{
			{ListOpts: listOpts},
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
	cr := enterpriseApi.IndexerCluster{
		TypeMeta: metav1.TypeMeta{
			Kind: "IndexerCluster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
	}
	splunkPVCDeletionTester(t, &cr, splctrl.CheckForDeletion)

	now := time.Now().Add(time.Second * 100)
	currentTime := metav1.NewTime(now)
	cr.ObjectMeta.DeletionTimestamp = &currentTime
	cr.ObjectMeta.Finalizers = []string{"enterprise.splunk.com/delete-pvc"}
	splunkPVCDeletionTester(t, &cr, splctrl.CheckForDeletion)

	// try with unrecognized finalizer
	c := spltest.NewMockClient()
	cr.ObjectMeta.Finalizers = append(cr.ObjectMeta.Finalizers, "bad-finalizer")
	deleted, err := splctrl.CheckForDeletion(&cr, c)
	if deleted != false || err == nil {
		t.Errorf("splctrl.CheckForDeletion() returned %t, %v; want false, (error)", deleted, err)
	}
}

func TestDeleteSplunkClusterMasterPvc(t *testing.T) {
	cr := enterpriseApi.ClusterMaster{
		TypeMeta: metav1.TypeMeta{
			Kind: "ClusterMaster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1-cm",
			Namespace: "test",
		},
	}
	splunkPVCDeletionTester(t, &cr, splctrl.CheckForDeletion)

	now := time.Now().Add(time.Second * 100)
	currentTime := metav1.NewTime(now)
	cr.ObjectMeta.DeletionTimestamp = &currentTime
	cr.ObjectMeta.Finalizers = []string{"enterprise.splunk.com/delete-pvc"}
	splunkPVCDeletionTester(t, &cr, splctrl.CheckForDeletion)

	// try with unrecognized finalizer
	c := spltest.NewMockClient()
	cr.ObjectMeta.Finalizers = append(cr.ObjectMeta.Finalizers, "bad-finalizer")
	deleted, err := splctrl.CheckForDeletion(&cr, c)
	if deleted != false || err == nil {
		t.Errorf("splctrl.CheckForDeletion() returned %t, %v; want false, (error)", deleted, err)
	}
}
