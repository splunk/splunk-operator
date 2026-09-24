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

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/stretchr/testify/assert"
)

func TestValidateIngestorClusterCreate(t *testing.T) {
	tests := []struct {
		name         string
		obj          *enterpriseApi.IngestorCluster
		wantErrCount int
		wantErrField string
	}{
		{
			name: "valid ingestor cluster - minimal",
			obj: &enterpriseApi.IngestorCluster{
				Spec: enterpriseApi.IngestorClusterSpec{
					QueueRef:         corev1.ObjectReference{Name: "my-queue"},
					ObjectStorageRef: corev1.ObjectReference{Name: "my-storage"},
					Replicas:         1,
				},
			},
			wantErrCount: 0,
		},
		{
			name: "invalid - queueRef.name empty",
			obj: &enterpriseApi.IngestorCluster{
				Spec: enterpriseApi.IngestorClusterSpec{
					ObjectStorageRef: corev1.ObjectReference{Name: "my-storage"},
					Replicas:         1,
				},
			},
			wantErrCount: 1,
			wantErrField: "spec.queueRef.name",
		},
		{
			name: "invalid - objectStorageRef.name empty",
			obj: &enterpriseApi.IngestorCluster{
				Spec: enterpriseApi.IngestorClusterSpec{
					QueueRef: corev1.ObjectReference{Name: "my-queue"},
					Replicas: 1,
				},
			},
			wantErrCount: 1,
			wantErrField: "spec.objectStorageRef.name",
		},
		{
			name: "invalid - both refs empty",
			obj: &enterpriseApi.IngestorCluster{
				Spec: enterpriseApi.IngestorClusterSpec{
					Replicas: 1,
				},
			},
			wantErrCount: 2,
		},
		{
			name: "invalid - app framework with cluster scope (local-only CR)",
			obj: &enterpriseApi.IngestorCluster{
				Spec: enterpriseApi.IngestorClusterSpec{
					QueueRef:         corev1.ObjectReference{Name: "my-queue"},
					ObjectStorageRef: corev1.ObjectReference{Name: "my-storage"},
					Replicas:         1,
					AppFrameworkConfig: enterpriseApi.AppFrameworkSpec{
						VolList: []enterpriseApi.VolumeSpec{{Name: "vol", Endpoint: "s3://bucket"}},
						AppSources: []enterpriseApi.AppSourceSpec{
							{Name: "src", Location: "/apps", AppSourceDefaultSpec: enterpriseApi.AppSourceDefaultSpec{Scope: "cluster", VolName: "vol"}},
						},
					},
				},
			},
			wantErrCount: 1,
			wantErrField: "spec.appRepo.appSources[0].scope",
		},
		{
			name: "valid - app framework with local scope",
			obj: &enterpriseApi.IngestorCluster{
				Spec: enterpriseApi.IngestorClusterSpec{
					QueueRef:         corev1.ObjectReference{Name: "my-queue"},
					ObjectStorageRef: corev1.ObjectReference{Name: "my-storage"},
					Replicas:         1,
					AppFrameworkConfig: enterpriseApi.AppFrameworkSpec{
						VolList: []enterpriseApi.VolumeSpec{
							{Name: "vol1", Endpoint: "s3://bucket"},
						},
						AppSources: []enterpriseApi.AppSourceSpec{
							{Name: "src", Location: "/apps", AppSourceDefaultSpec: enterpriseApi.AppSourceDefaultSpec{Scope: "local", VolName: "vol1"}},
						},
					},
				},
			},
			wantErrCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := ValidateIngestorClusterCreate(tt.obj)
			assert.Len(t, errs, tt.wantErrCount, "unexpected error count")
			if tt.wantErrField != "" && len(errs) > 0 {
				assert.Equal(t, tt.wantErrField, errs[0].Field, "unexpected error field")
			}
		})
	}
}

func TestValidateIngestorClusterUpdate(t *testing.T) {
	valid := &enterpriseApi.IngestorCluster{
		Spec: enterpriseApi.IngestorClusterSpec{
			QueueRef:         corev1.ObjectReference{Name: "my-queue"},
			ObjectStorageRef: corev1.ObjectReference{Name: "my-storage"},
			Replicas:         1,
		},
	}
	errs := ValidateIngestorClusterUpdate(valid, valid)
	assert.Empty(t, errs, "expected no errors on valid update")
}

func TestGetIngestorClusterWarningsOnCreate(t *testing.T) {
	obj := &enterpriseApi.IngestorCluster{
		Spec: enterpriseApi.IngestorClusterSpec{
			QueueRef:         corev1.ObjectReference{Name: "my-queue"},
			ObjectStorageRef: corev1.ObjectReference{Name: "my-storage"},
			Replicas:         1,
		},
	}
	warnings := GetIngestorClusterWarningsOnCreate(obj)
	assert.Empty(t, warnings, "expected no warnings")
}

func TestGetIngestorClusterWarningsOnUpdate(t *testing.T) {
	obj := &enterpriseApi.IngestorCluster{
		Spec: enterpriseApi.IngestorClusterSpec{
			QueueRef:         corev1.ObjectReference{Name: "my-queue"},
			ObjectStorageRef: corev1.ObjectReference{Name: "my-storage"},
			Replicas:         1,
		},
	}
	warnings := GetIngestorClusterWarningsOnUpdate(obj, obj)
	assert.Empty(t, warnings, "expected no warnings")
}
