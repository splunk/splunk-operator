// Copyright (c) 2018-2022 Splunk Inc. All rights reserved.

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
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	enterpriseApi "github.com/splunk/splunk-operator/api/v4"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	enterpriseApiV3 "github.com/splunk/splunk-operator/api/v3"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	splctrl "github.com/splunk/splunk-operator/pkg/splunk/controller"
	spltest "github.com/splunk/splunk-operator/pkg/splunk/test"
)

func splunkDeletionTester(t *testing.T, cr splcommon.MetaObject, delete func(splcommon.MetaObject, splcommon.ControllerClient) (bool, error)) {
	var component string
	switch cr.GetObjectKind().GroupVersionKind().Kind {
	case "Standalone":
		component = "standalone"
	case "LicenseManager":
		component = "license-manager"
	case "LicenseMaster":
		component = "license-master"
	case "SearchHeadCluster":
		component = "search-head"
	case "IndexerCluster":
		component = "indexer"
	case "ClusterManager":
		component = "cluster-manager"
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
		if component == "cluster-master" || component == "license-master" {
			apiVersion, _ = schema.ParseGroupVersion("enterprise.splunk.com/v3")
		}
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
				{MetaName: "*v1.Secret-test-splunk-test-secret"},
			}
			mockCalls["Create"] = []spltest.MockFuncCall{
				{MetaName: "*v1.Secret-test-splunk-test-secret"},
			}
			if component == "monitoring-console" {
				mockCalls["Create"] = []spltest.MockFuncCall{
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
				}
				mockCalls["Get"] = []spltest.MockFuncCall{
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
				}
				mockCalls["Update"] = []spltest.MockFuncCall{
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: fmt.Sprintf("*%s.%s-%s-%s", apiVersion.Version, cr.GetObjectKind().GroupVersionKind().Kind, cr.GetNamespace(), cr.GetName())},
				}
				mockCalls["Delete"] = []spltest.MockFuncCall{
					{MetaName: "*v1.PersistentVolumeClaim-test-splunk-pvc-stack1-var"},
				}
			}

			switch cr.GetObjectKind().GroupVersionKind().Kind {
			case "Standalone":
				mockCalls["Get"] = []spltest.MockFuncCall{
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.ConfigMap-test-splunk-standalone-stack1-configmap"},
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.StatefulSet-test-splunk-stack1-standalone"},
					{MetaName: "*v4.Standalone-test-stack1"},
					{MetaName: "*v4.Standalone-test-stack1"},
				}
				mockCalls["Create"] = []spltest.MockFuncCall{
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.ConfigMap-test-splunk-standalone-stack1-configmap"},
				}

			case "LicenseMaster":
				mockCalls["Get"] = []spltest.MockFuncCall{
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.ConfigMap-test-splunk-license-master-stack1-configmap"},
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.StatefulSet-test-splunk-stack1-license-master"},
					{MetaName: "*v3.LicenseMaster-test-stack1"},
					{MetaName: "*v3.LicenseMaster-test-stack1"},
				}
				mockCalls["Create"] = []spltest.MockFuncCall{
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.ConfigMap-test-splunk-license-master-stack1-configmap"},
				}

			case "LicenseManager":
				mockCalls["Get"] = []spltest.MockFuncCall{
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.ConfigMap-test-splunk-license-manager-stack1-configmap"},
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.StatefulSet-test-splunk-stack1-license-manager"},
					{MetaName: "*v4.LicenseManager-test-stack1"},
					{MetaName: "*v4.LicenseManager-test-stack1"},
				}
				mockCalls["Create"] = []spltest.MockFuncCall{
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.ConfigMap-test-splunk-license-manager-stack1-configmap"},
				}

			case "SearchHeadCluster":
				mockCalls["Get"] = []spltest.MockFuncCall{
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.ConfigMap-test-splunk-search-head-stack1-configmap"},
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.StatefulSet-test-splunk-stack1-search-head"},
					{MetaName: "*v4.SearchHeadCluster-test-stack1"},
					{MetaName: "*v4.SearchHeadCluster-test-stack1"},
				}
				mockCalls["Create"] = []spltest.MockFuncCall{
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.ConfigMap-test-splunk-search-head-stack1-configmap"},
				}

			case "ClusterMaster":
				mockCalls["Get"] = []spltest.MockFuncCall{
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.ConfigMap-test-splunk-cluster-master-stack1-configmap"},
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.StatefulSet-test-splunk-stack1-cluster-master"},
					{MetaName: "*v3.ClusterMaster-test-stack1"},
					{MetaName: "*v3.ClusterMaster-test-stack1"},
				}
				mockCalls["Create"] = []spltest.MockFuncCall{
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.ConfigMap-test-splunk-cluster-master-stack1-configmap"},
				}
			case "IndexerCluster":
				mockCalls["Create"] = []spltest.MockFuncCall{
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.ConfigMap-test-splunk-indexer-stack1-configmap"},
				}

			case "ClusterManager":
				mockCalls["Get"] = []spltest.MockFuncCall{
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.ConfigMap-test-splunk-cluster-manager-stack1-configmap"},
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.StatefulSet-test-splunk-stack1-cluster-manager"},
					{MetaName: "*v4.ClusterManager-test-stack1"},
					{MetaName: "*v4.ClusterManager-test-stack1"},
				}
				mockCalls["Create"] = []spltest.MockFuncCall{
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.ConfigMap-test-splunk-cluster-manager-stack1-configmap"},
				}

				listOptsTest := []client.ListOption{
					client.InNamespace(cr.GetNamespace()),
				}

				mockCalls["List"] = append(mockCalls["List"], []spltest.MockFuncCall{
					{ListOpts: listOptsTest},
					{ListOpts: listOptsTest},
					{ListOpts: listOptsTest},
					{ListOpts: listOptsTest},
				}...)
				mockCalls["List"][0], mockCalls["List"][len(mockCalls["List"])-1] = mockCalls["List"][len(mockCalls["List"])-1], mockCalls["List"][0]
			case "MonitoringConsole":
				mockCalls["Get"] = []spltest.MockFuncCall{
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.ConfigMap-test-splunk-monitoring-console-stack1-configmap"},
					{MetaName: "*v4.MonitoringConsole-test-stack1"},
					{MetaName: "*v4.MonitoringConsole-test-stack1"},
				}
				mockCalls["Create"] = []spltest.MockFuncCall{
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.ConfigMap-test-splunk-monitoring-console-stack1-configmap"},
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
				{MetaName: "*v1.Secret-test-splunk-test-secret"},
				{MetaName: "*v4.ClusterManager-test-manager1"},
				{MetaName: "*v1.Secret-test-splunk-test-secret"},
				{MetaName: "*v1.StatefulSet-test-splunk-stack1-indexer"},
				{MetaName: "*v4.IndexerCluster-test-stack1"},
				{MetaName: "*v4.IndexerCluster-test-stack1"},
			}
			switch cr.GetObjectKind().GroupVersionKind().Kind {
			case "IndexerCluster":
				mockCalls["Create"] = []spltest.MockFuncCall{
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.ConfigMap-test-splunk-indexer-stack1-configmap"},
				}
				mockCalls["Get"] = []spltest.MockFuncCall{
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.ConfigMap-test-splunk-indexer-stack1-configmap"},
					{MetaName: "*v4.ClusterManager-test-manager1"},
					{MetaName: "*v1.Secret-test-splunk-test-secret"},
					{MetaName: "*v1.StatefulSet-test-splunk-stack1-indexer"},
					{MetaName: "*v4.IndexerCluster-test-stack1"},
					{MetaName: "*v4.IndexerCluster-test-stack1"},
				}
			}
		}
	}

	c := spltest.NewMockClient()
	c.ListObj = &pvclist
	var err error
	deleted, err := delete(cr, c)
	if deleted != wantDeleted || err != nil {
		t.Errorf("splctrl.CheckForDeletion() returned %t, %v; want %t, nil", deleted, err, wantDeleted)
	}
	c.CheckCalls(t, "Testsplctrl.CheckForDeletion", mockCalls)
}

func splunkPVCDeletionTester(t *testing.T, cr splcommon.MetaObject, delete func(context.Context, splcommon.MetaObject, splcommon.ControllerClient) (bool, error)) {
	var component string
	ctx := context.TODO()
	switch cr.GetObjectKind().GroupVersionKind().Kind {
	case "Standalone":
		component = "standalone"
	case "LicenseManager":
		component = "license-manager"
	case "LicenseMaster":
		component = "license-master"
	case "SearchHeadCluster":
		component = "search-head"
	case "IndexerCluster":
		component = "indexer"
	case "ClusterMaster":
		component = "cluster-master"
	case "ClusterManager":
		component = "cluster-manager"
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
		if component == "cluster-master" || component == "license-master" {
			apiVersion, _ = schema.ParseGroupVersion("enterprise.splunk.com/v3")
		}
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
	deleted, err := delete(ctx, cr, c)
	if deleted != wantDeleted || err != nil {
		t.Errorf("splctrl.CheckForDeletion() returned %t, %v; want %t, nil", deleted, err, wantDeleted)
	}
	c.CheckCalls(t, "Testsplctrl.CheckForDeletion", mockCalls)
}
func TestDeleteSplunkPvc(t *testing.T) {
	ctx := context.TODO()
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
	deleted, err := splctrl.CheckForDeletion(ctx, &cr, c)
	if deleted != false || err == nil {
		t.Errorf("splctrl.CheckForDeletion() returned %t, %v; want false, (error)", deleted, err)
	}
}

func TestDeleteSplunkClusterManagerPvc(t *testing.T) {
	ctx := context.TODO()
	cr := enterpriseApiV3.ClusterMaster{
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
	deleted, err := splctrl.CheckForDeletion(ctx, &cr, c)
	if deleted != false || err == nil {
		t.Errorf("splctrl.CheckForDeletion() returned %t, %v; want false, (error)", deleted, err)
	}
}

func TestDeleteSplunkPvcError(t *testing.T) {
	ctx := context.TODO()
	cr := enterpriseApiV3.Standalone{
		TypeMeta: metav1.TypeMeta{
			Kind: "Standalone",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1-sa",
			Namespace: "test",
		},
	}

	rerr := errors.New(splcommon.Rerr)
	c := spltest.NewMockClient()
	c.InduceErrorKind[splcommon.MockClientInduceErrorList] = rerr
	err := DeleteSplunkPvc(ctx, &cr, c)
	if err == nil {
		t.Errorf("Expected error")
	}

	c.InduceErrorKind[splcommon.MockClientInduceErrorList] = nil
	c.InduceErrorKind[splcommon.MockClientInduceErrorDelete] = rerr
	pvclist := corev1.PersistentVolumeClaimList{
		Items: []corev1.PersistentVolumeClaim{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "splunk-pvc-stack1-sa-var",
					Namespace: "test",
				},
			},
		},
	}
	c.ListObj = &pvclist
	err = DeleteSplunkPvc(ctx, &cr, c)
	if err == nil {
		t.Errorf("Expected error")
	}

	// Different CR types

	// License Master
	c.InduceErrorKind[splcommon.MockClientInduceErrorList] = rerr
	lmasCr := &enterpriseApiV3.LicenseMaster{}
	err = DeleteSplunkPvc(ctx, lmasCr, c)
	if err != nil {
		t.Errorf("Incorrect kind, but no expected error")
	}

	lmasCr = &enterpriseApiV3.LicenseMaster{
		TypeMeta: metav1.TypeMeta{
			Kind: "LicenseMaster",
		},
	}
	err = DeleteSplunkPvc(ctx, lmasCr, c)
	if err == nil {
		t.Errorf("Expected error")
	}

	// License Manager
	lmanCr := &enterpriseApi.LicenseManager{
		TypeMeta: metav1.TypeMeta{
			Kind: "LicenseManager",
		},
	}
	err = DeleteSplunkPvc(ctx, lmanCr, c)
	if err == nil {
		t.Errorf("Expected error")
	}

	// Shc
	shcCr := &enterpriseApi.SearchHeadCluster{
		TypeMeta: metav1.TypeMeta{
			Kind: "SearchHeadCluster",
		},
	}
	err = DeleteSplunkPvc(ctx, shcCr, c)
	if err == nil {
		t.Errorf("Expected error")
	}

	// Cluster Manager
	cmanCr := &enterpriseApi.ClusterManager{
		TypeMeta: metav1.TypeMeta{
			Kind: "ClusterManager",
		},
	}
	err = DeleteSplunkPvc(ctx, cmanCr, c)
	if err == nil {
		t.Errorf("Expected error")
	}

	// MC
	mcCr := &enterpriseApi.MonitoringConsole{
		TypeMeta: metav1.TypeMeta{
			Kind: "MonitoringConsole",
		},
	}
	err = DeleteSplunkPvc(ctx, mcCr, c)
	if err == nil {
		t.Errorf("Expected error")
	}
}
