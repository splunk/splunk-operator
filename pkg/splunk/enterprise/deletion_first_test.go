// Copyright (c) 2026 Splunk Inc. All rights reserved.

// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
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
	"reflect"
	"testing"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	spltest "github.com/splunk/splunk-operator/pkg/splunk/test"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestPausedDeletionBypassesNormalApplyForAllV4Tiers(t *testing.T) {
	invalidAppFramework := enterpriseApi.AppFrameworkSpec{
		AppSources: []enterpriseApi.AppSourceSpec{
			{
				Name:     "invalid-without-volume",
				Location: "apps",
				AppSourceDefaultSpec: enterpriseApi.AppSourceDefaultSpec{
					VolName: "missing-volume",
					Scope:   enterpriseApi.ScopeLocal,
				},
			},
		},
	}

	tests := []struct {
		name            string
		pauseAnnotation string
		object          splcommon.MetaObject
		apply           func(context.Context, *spltest.MockClient, splcommon.MetaObject) error
	}{
		{
			name:            "Standalone",
			pauseAnnotation: enterpriseApi.StandalonePausedAnnotation,
			object: &enterpriseApi.Standalone{
				Spec: enterpriseApi.StandaloneSpec{
					CommonSplunkSpec:   enterpriseApi.CommonSplunkSpec{Mock: true},
					AppFrameworkConfig: invalidAppFramework,
				},
			},
			apply: func(ctx context.Context, c *spltest.MockClient, object splcommon.MetaObject) error {
				_, err := ApplyStandalone(ctx, c, object.(*enterpriseApi.Standalone))
				return err
			},
		},
		{
			name:            "ClusterManager",
			pauseAnnotation: enterpriseApi.ClusterManagerPausedAnnotation,
			object: &enterpriseApi.ClusterManager{
				Spec: enterpriseApi.ClusterManagerSpec{
					CommonSplunkSpec:   enterpriseApi.CommonSplunkSpec{Mock: true},
					AppFrameworkConfig: invalidAppFramework,
				},
			},
			apply: func(ctx context.Context, c *spltest.MockClient, object splcommon.MetaObject) error {
				_, err := ApplyClusterManager(ctx, c, object.(*enterpriseApi.ClusterManager), nil)
				return err
			},
		},
		{
			name:            "MonitoringConsole",
			pauseAnnotation: enterpriseApi.MonitoringConsolePausedAnnotation,
			object: &enterpriseApi.MonitoringConsole{
				Spec: enterpriseApi.MonitoringConsoleSpec{
					CommonSplunkSpec:   enterpriseApi.CommonSplunkSpec{Mock: true},
					AppFrameworkConfig: invalidAppFramework,
				},
			},
			apply: func(ctx context.Context, c *spltest.MockClient, object splcommon.MetaObject) error {
				_, err := ApplyMonitoringConsole(ctx, c, object.(*enterpriseApi.MonitoringConsole))
				return err
			},
		},
		{
			name:            "IndexerCluster with ClusterManager",
			pauseAnnotation: enterpriseApi.IndexerClusterPausedAnnotation,
			object: &enterpriseApi.IndexerCluster{
				Spec: enterpriseApi.IndexerClusterSpec{
					CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
						ClusterManagerRef: corev1.ObjectReference{Name: "manager"},
						Mock:              true,
					},
					Replicas: 1,
				},
			},
			apply: func(ctx context.Context, c *spltest.MockClient, object splcommon.MetaObject) error {
				_, err := ApplyIndexerClusterManager(ctx, c, object.(*enterpriseApi.IndexerCluster))
				return err
			},
		},
		{
			name:            "IndexerCluster with ClusterMaster",
			pauseAnnotation: enterpriseApi.IndexerClusterPausedAnnotation,
			object: &enterpriseApi.IndexerCluster{
				Spec: enterpriseApi.IndexerClusterSpec{
					CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
						ClusterMasterRef: corev1.ObjectReference{Name: "master"},
						Mock:             true,
					},
					Replicas: 1,
				},
			},
			apply: func(ctx context.Context, c *spltest.MockClient, object splcommon.MetaObject) error {
				_, err := ApplyIndexerCluster(ctx, c, object.(*enterpriseApi.IndexerCluster))
				return err
			},
		},
		{
			name:            "IngestorCluster",
			pauseAnnotation: enterpriseApi.IngestorClusterPausedAnnotation,
			object: &enterpriseApi.IngestorCluster{
				Spec: enterpriseApi.IngestorClusterSpec{
					CommonSplunkSpec:   enterpriseApi.CommonSplunkSpec{Mock: true},
					Replicas:           1,
					AppFrameworkConfig: invalidAppFramework,
				},
			},
			apply: func(ctx context.Context, c *spltest.MockClient, object splcommon.MetaObject) error {
				_, err := ApplyIngestorCluster(ctx, c, object.(*enterpriseApi.IngestorCluster))
				return err
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			object := test.object.DeepCopyObject().(splcommon.MetaObject)
			object.SetName("deleting")
			object.SetNamespace("terminating")
			object.SetAnnotations(map[string]string{test.pauseAnnotation: "true"})
			object.SetFinalizers([]string{"enterprise.splunk.com/delete-pvc"})
			now := metav1.Now()
			object.SetDeletionTimestamp(&now)

			c := spltest.NewMockClient()
			c.ListObj = &corev1.PersistentVolumeClaimList{}
			c.InduceErrorKind[splcommon.MockClientInduceErrorCreate] =
				errors.New("namespace is terminating: creates are forbidden")

			err := test.apply(context.Background(), c, object)
			if err != nil {
				t.Fatalf("deletion returned error: %v", err)
			}
			if got := len(c.Calls["Create"]); got != 0 {
				t.Fatalf("deletion attempted %d resource creates", got)
			}
			if finalizers := object.GetFinalizers(); len(finalizers) != 0 {
				t.Fatalf("deletion retained finalizers: %v", finalizers)
			}

			statusRefreshCalls := 0
			for _, call := range c.Calls["Get"] {
				if reflect.TypeOf(call.Obj) == reflect.TypeOf(object) {
					statusRefreshCalls++
				}
			}
			if statusRefreshCalls != 0 {
				t.Fatalf(
					"successful deletion attempted %d post-finalization status refreshes",
					statusRefreshCalls,
				)
			}
		})
	}
}

func TestDeletionFailurePreservesErrorStatusPath(t *testing.T) {
	object := &enterpriseApi.Standalone{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "deleting",
			Namespace:  "active",
			Finalizers: []string{"enterprise.splunk.com/delete-pvc"},
		},
		Spec: enterpriseApi.StandaloneSpec{
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{Mock: true},
		},
	}
	now := metav1.Now()
	object.SetDeletionTimestamp(&now)

	c := spltest.NewMockClient()
	c.AddObject(object.DeepCopy())
	c.ListObj = &corev1.PersistentVolumeClaimList{
		Items: []corev1.PersistentVolumeClaim{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "splunk-deleting-standalone-etc",
					Namespace: object.GetNamespace(),
				},
			},
		},
	}
	deleteErr := errors.New("PVC delete failed")
	c.InduceErrorKind[splcommon.MockClientInduceErrorDelete] = deleteErr

	_, err := ApplyStandalone(context.Background(), c, object)
	if !errors.Is(err, deleteErr) {
		t.Fatalf("deletion returned %v; want %v", err, deleteErr)
	}
	if got := len(c.Calls["Create"]); got != 0 {
		t.Fatalf("failed deletion attempted %d resource creates", got)
	}
	if finalizers := object.GetFinalizers(); len(finalizers) != 1 {
		t.Fatalf("failed deletion changed finalizers: %v", finalizers)
	}

	statusRefreshCalls := 0
	for _, call := range c.Calls["Get"] {
		if reflect.TypeOf(call.Obj) == reflect.TypeOf(object) {
			statusRefreshCalls++
		}
	}
	if statusRefreshCalls == 0 {
		t.Fatal("failed deletion did not execute the status update path")
	}
}
