/*
Copyright (c) 2018-2026 Splunk Inc. All rights reserved.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package validation

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/stretchr/testify/assert"
)

func TestValidateIndexerClusterCreate(t *testing.T) {
	tests := []struct {
		name         string
		obj          *enterpriseApi.IndexerCluster
		wantErrCount int
		wantErrField string
	}{
		{
			name: "valid indexer cluster - minimal",
			obj: &enterpriseApi.IndexerCluster{
				Spec: enterpriseApi.IndexerClusterSpec{
					CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
						ClusterManagerRef: corev1.ObjectReference{Name: "cluster-manager"},
					},
					Replicas: 3,
				},
			},
			wantErrCount: 0,
		},
		{
			name: "invalid indexer cluster - zero replicas",
			obj: &enterpriseApi.IndexerCluster{
				Spec: enterpriseApi.IndexerClusterSpec{
					CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
						ClusterManagerRef: corev1.ObjectReference{Name: "cluster-manager"},
					},
					Replicas: 0,
				},
			},
			wantErrCount: 1,
			wantErrField: "spec.replicas",
		},
		{
			name: "invalid indexer cluster - less than 3 replicas",
			obj: &enterpriseApi.IndexerCluster{
				Spec: enterpriseApi.IndexerClusterSpec{
					CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
						ClusterManagerRef: corev1.ObjectReference{Name: "cluster-manager"},
					},
					Replicas: 2,
				},
			},
			wantErrCount: 1,
			wantErrField: "spec.replicas",
		},
		{
			name: "invalid indexer cluster - negative replicas",
			obj: &enterpriseApi.IndexerCluster{
				Spec: enterpriseApi.IndexerClusterSpec{
					CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
						ClusterManagerRef: corev1.ObjectReference{Name: "cluster-manager"},
					},
					Replicas: -1,
				},
			},
			wantErrCount: 1,
			wantErrField: "spec.replicas",
		},
		{
			name: "valid indexer cluster - both refs set with names",
			obj: &enterpriseApi.IndexerCluster{
				Spec: enterpriseApi.IndexerClusterSpec{
					CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
						ClusterManagerRef: corev1.ObjectReference{Name: "cluster-manager"},
					},
					QueueRef:         &corev1.ObjectReference{Name: "my-queue"},
					ObjectStorageRef: &corev1.ObjectReference{Name: "my-storage"},
					Replicas:         3,
				},
			},
			wantErrCount: 0,
		},
		{
			name: "valid indexer cluster - neither ref set",
			obj: &enterpriseApi.IndexerCluster{
				Spec: enterpriseApi.IndexerClusterSpec{
					CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
						ClusterManagerRef: corev1.ObjectReference{Name: "cluster-manager"},
					},
					Replicas: 3,
				},
			},
			wantErrCount: 0,
		},
		{
			name: "invalid indexer cluster - queueRef name set but objectStorageRef name empty",
			obj: &enterpriseApi.IndexerCluster{
				Spec: enterpriseApi.IndexerClusterSpec{
					CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
						ClusterManagerRef: corev1.ObjectReference{Name: "cluster-manager"},
					},
					QueueRef: &corev1.ObjectReference{Name: "my-queue"},
					Replicas: 3,
				},
			},
			wantErrCount: 1,
			wantErrField: "spec.objectStorageRef.name",
		},
		{
			name: "invalid indexer cluster - objectStorageRef name set but queueRef name empty",
			obj: &enterpriseApi.IndexerCluster{
				Spec: enterpriseApi.IndexerClusterSpec{
					CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
						ClusterManagerRef: corev1.ObjectReference{Name: "cluster-manager"},
					},
					ObjectStorageRef: &corev1.ObjectReference{Name: "my-storage"},
					Replicas:         3,
				},
			},
			wantErrCount: 1,
			wantErrField: "spec.queueRef.name",
		},
		{
			name: "invalid indexer cluster - missing clusterManagerRef",
			obj: &enterpriseApi.IndexerCluster{
				Spec: enterpriseApi.IndexerClusterSpec{
					Replicas: 3,
				},
			},
			wantErrCount: 1,
			wantErrField: "spec.clusterManagerRef.name",
		},
		{
			name: "valid indexer cluster - clusterMasterRef accepted for backwards compat",
			obj: &enterpriseApi.IndexerCluster{
				Spec: enterpriseApi.IndexerClusterSpec{
					CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
						ClusterMasterRef: corev1.ObjectReference{Name: "cluster-master"},
					},
					Replicas: 3,
				},
			},
			wantErrCount: 0,
		},
		// cross-namespace ClusterManagerRef tests
		{
			name: "valid - clusterManagerRef without namespace is allowed",
			obj: &enterpriseApi.IndexerCluster{
				ObjectMeta: metav1.ObjectMeta{Namespace: "splunk-ns"},
				Spec: enterpriseApi.IndexerClusterSpec{
					CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
						ClusterManagerRef: corev1.ObjectReference{Name: "cm"},
					},
					Replicas: 3,
				},
			},
			wantErrCount: 0,
		},
		{
			name: "valid - clusterManagerRef with same namespace is allowed",
			obj: &enterpriseApi.IndexerCluster{
				ObjectMeta: metav1.ObjectMeta{Namespace: "splunk-ns"},
				Spec: enterpriseApi.IndexerClusterSpec{
					CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
						ClusterManagerRef: corev1.ObjectReference{Name: "cm", Namespace: "splunk-ns"},
					},
					Replicas: 3,
				},
			},
			wantErrCount: 0,
		},
		{
			name: "invalid - clusterManagerRef with different namespace is rejected",
			obj: &enterpriseApi.IndexerCluster{
				ObjectMeta: metav1.ObjectMeta{Namespace: "splunk-ns"},
				Spec: enterpriseApi.IndexerClusterSpec{
					CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
						ClusterManagerRef: corev1.ObjectReference{Name: "cm", Namespace: "other-ns"},
					},
					Replicas: 3,
				},
			},
			wantErrCount: 1,
			wantErrField: "spec.clusterManagerRef.namespace",
		},
		{
			name: "invalid - clusterMasterRef (deprecated) with different namespace is rejected",
			obj: &enterpriseApi.IndexerCluster{
				ObjectMeta: metav1.ObjectMeta{Namespace: "splunk-ns"},
				Spec: enterpriseApi.IndexerClusterSpec{
					CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
						ClusterMasterRef: corev1.ObjectReference{Name: "cm", Namespace: "other-ns"},
					},
					Replicas: 3,
				},
			},
			wantErrCount: 1,
			wantErrField: "spec.clusterManagerRef.namespace",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := ValidateIndexerClusterCreate(tt.obj)
			assert.Len(t, errs, tt.wantErrCount, "unexpected error count")
			if tt.wantErrField != "" && len(errs) > 0 {
				assert.Equal(t, tt.wantErrField, errs[0].Field, "unexpected error field")
			}
		})
	}
}

func TestValidateIndexerClusterUpdate(t *testing.T) {
	tests := []struct {
		name         string
		obj          *enterpriseApi.IndexerCluster
		oldObj       *enterpriseApi.IndexerCluster
		wantErrCount int
	}{
		{
			name: "valid update - same replicas",
			obj: &enterpriseApi.IndexerCluster{
				Spec: enterpriseApi.IndexerClusterSpec{
					CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
						ClusterManagerRef: corev1.ObjectReference{Name: "cm"},
					},
					Replicas: 3,
				},
			},
			oldObj: &enterpriseApi.IndexerCluster{
				Spec: enterpriseApi.IndexerClusterSpec{
					CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
						ClusterManagerRef: corev1.ObjectReference{Name: "cm"},
					},
					Replicas: 3,
				},
			},
			wantErrCount: 0,
		},
		{
			name: "valid update - scale up",
			obj: &enterpriseApi.IndexerCluster{
				Spec: enterpriseApi.IndexerClusterSpec{
					CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
						ClusterManagerRef: corev1.ObjectReference{Name: "cm"},
					},
					Replicas: 5,
				},
			},
			oldObj: &enterpriseApi.IndexerCluster{
				Spec: enterpriseApi.IndexerClusterSpec{
					CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
						ClusterManagerRef: corev1.ObjectReference{Name: "cm"},
					},
					Replicas: 3,
				},
			},
			wantErrCount: 0,
		},
		{
			name: "invalid update - scale down below minimum",
			obj: &enterpriseApi.IndexerCluster{
				Spec: enterpriseApi.IndexerClusterSpec{
					CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
						ClusterManagerRef: corev1.ObjectReference{Name: "cm"},
					},
					Replicas: 1,
				},
			},
			oldObj: &enterpriseApi.IndexerCluster{
				Spec: enterpriseApi.IndexerClusterSpec{
					CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
						ClusterManagerRef: corev1.ObjectReference{Name: "cm"},
					},
					Replicas: 3,
				},
			},
			wantErrCount: 1,
		},
		{
			name: "invalid update - negative replicas",
			obj: &enterpriseApi.IndexerCluster{
				Spec: enterpriseApi.IndexerClusterSpec{
					CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
						ClusterManagerRef: corev1.ObjectReference{Name: "cm"},
					},
					Replicas: -1,
				},
			},
			oldObj: &enterpriseApi.IndexerCluster{
				Spec: enterpriseApi.IndexerClusterSpec{
					CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
						ClusterManagerRef: corev1.ObjectReference{Name: "cm"},
					},
					Replicas: 3,
				},
			},
			wantErrCount: 1,
		},
		{
			name: "invalid update - queueRef cleared after being set",
			obj: &enterpriseApi.IndexerCluster{
				Spec: enterpriseApi.IndexerClusterSpec{
					CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
						ClusterManagerRef: corev1.ObjectReference{Name: "cm"},
					},
					Replicas: 3,
				},
			},
			oldObj: &enterpriseApi.IndexerCluster{
				Spec: enterpriseApi.IndexerClusterSpec{
					CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
						ClusterManagerRef: corev1.ObjectReference{Name: "cm"},
					},
					QueueRef: &corev1.ObjectReference{Name: "my-queue"},
					Replicas: 3,
				},
			},
			wantErrCount: 1,
		},
		{
			name: "valid update - queueRef unchanged",
			obj: &enterpriseApi.IndexerCluster{
				Spec: enterpriseApi.IndexerClusterSpec{
					CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
						ClusterManagerRef: corev1.ObjectReference{Name: "cm"},
					},
					QueueRef:         &corev1.ObjectReference{Name: "my-queue"},
					ObjectStorageRef: &corev1.ObjectReference{Name: "my-storage"},
					Replicas:         3,
				},
			},
			oldObj: &enterpriseApi.IndexerCluster{
				Spec: enterpriseApi.IndexerClusterSpec{
					CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
						ClusterManagerRef: corev1.ObjectReference{Name: "cm"},
					},
					QueueRef:         &corev1.ObjectReference{Name: "my-queue"},
					ObjectStorageRef: &corev1.ObjectReference{Name: "my-storage"},
					Replicas:         3,
				},
			},
			wantErrCount: 0,
		},
		{
			name: "valid update - queueRef never set",
			obj: &enterpriseApi.IndexerCluster{
				Spec: enterpriseApi.IndexerClusterSpec{
					CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
						ClusterManagerRef: corev1.ObjectReference{Name: "cm"},
					},
					Replicas: 3,
				},
			},
			oldObj: &enterpriseApi.IndexerCluster{
				Spec: enterpriseApi.IndexerClusterSpec{
					CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
						ClusterManagerRef: corev1.ObjectReference{Name: "cm"},
					},
					Replicas: 3,
				},
			},
			wantErrCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := ValidateIndexerClusterUpdate(tt.obj, tt.oldObj)
			assert.Len(t, errs, tt.wantErrCount, "unexpected error count")
		})
	}
}

func TestGetIndexerClusterWarningsOnCreate(t *testing.T) {
	obj := &enterpriseApi.IndexerCluster{
		Spec: enterpriseApi.IndexerClusterSpec{
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				ClusterManagerRef: corev1.ObjectReference{Name: "cm"},
			},
			Replicas: 3,
		},
	}
	warnings := GetIndexerClusterWarningsOnCreate(obj)
	assert.Empty(t, warnings, "expected no warnings")
}

func TestGetIndexerClusterWarningsOnUpdate(t *testing.T) {
	obj := &enterpriseApi.IndexerCluster{
		Spec: enterpriseApi.IndexerClusterSpec{
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				ClusterManagerRef: corev1.ObjectReference{Name: "cm"},
			},
			Replicas: 3,
		},
	}
	oldObj := &enterpriseApi.IndexerCluster{
		Spec: enterpriseApi.IndexerClusterSpec{
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				ClusterManagerRef: corev1.ObjectReference{Name: "cm"},
			},
			Replicas: 3,
		},
	}
	warnings := GetIndexerClusterWarningsOnUpdate(obj, oldObj)
	assert.Empty(t, warnings, "expected no warnings")
}
