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

	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
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
					Replicas: 3,
				},
			},
			wantErrCount: 0,
		},
		{
			name: "valid indexer cluster - zero replicas",
			obj: &enterpriseApi.IndexerCluster{
				Spec: enterpriseApi.IndexerClusterSpec{
					Replicas: 0,
				},
			},
			wantErrCount: 0,
		},
		{
			name: "invalid indexer cluster - negative replicas",
			obj: &enterpriseApi.IndexerCluster{
				Spec: enterpriseApi.IndexerClusterSpec{
					Replicas: -1,
				},
			},
			wantErrCount: 1,
			wantErrField: "spec.replicas",
		},
		{
			name: "valid indexer cluster - with common spec",
			obj: &enterpriseApi.IndexerCluster{
				Spec: enterpriseApi.IndexerClusterSpec{
					Replicas: 3,
					CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
						Spec: enterpriseApi.Spec{
							ImagePullPolicy: "Always",
						},
					},
				},
			},
			wantErrCount: 0,
		},
		{
			name: "invalid indexer cluster - invalid image pull policy",
			obj: &enterpriseApi.IndexerCluster{
				Spec: enterpriseApi.IndexerClusterSpec{
					Replicas: 3,
					CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
						Spec: enterpriseApi.Spec{
							ImagePullPolicy: "InvalidPolicy",
						},
					},
				},
			},
			wantErrCount: 1,
			wantErrField: "spec.imagePullPolicy",
		},
		{
			name: "invalid indexer cluster - multiple errors",
			obj: &enterpriseApi.IndexerCluster{
				Spec: enterpriseApi.IndexerClusterSpec{
					Replicas: -1,
					CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
						Spec: enterpriseApi.Spec{
							ImagePullPolicy: "InvalidPolicy",
						},
					},
				},
			},
			wantErrCount: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := ValidateIndexerClusterCreate(tt.obj)
			if len(errs) != tt.wantErrCount {
				t.Errorf("ValidateIndexerClusterCreate() got %d errors, want %d. Errors: %v", len(errs), tt.wantErrCount, errs)
			}
			if tt.wantErrField != "" && len(errs) > 0 {
				if errs[0].Field != tt.wantErrField {
					t.Errorf("ValidateIndexerClusterCreate() error field = %s, want %s", errs[0].Field, tt.wantErrField)
				}
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
					Replicas: 3,
				},
			},
			oldObj: &enterpriseApi.IndexerCluster{
				Spec: enterpriseApi.IndexerClusterSpec{
					Replicas: 3,
				},
			},
			wantErrCount: 0,
		},
		{
			name: "valid update - scale up",
			obj: &enterpriseApi.IndexerCluster{
				Spec: enterpriseApi.IndexerClusterSpec{
					Replicas: 5,
				},
			},
			oldObj: &enterpriseApi.IndexerCluster{
				Spec: enterpriseApi.IndexerClusterSpec{
					Replicas: 3,
				},
			},
			wantErrCount: 0,
		},
		{
			name: "valid update - scale down",
			obj: &enterpriseApi.IndexerCluster{
				Spec: enterpriseApi.IndexerClusterSpec{
					Replicas: 1,
				},
			},
			oldObj: &enterpriseApi.IndexerCluster{
				Spec: enterpriseApi.IndexerClusterSpec{
					Replicas: 3,
				},
			},
			wantErrCount: 0,
		},
		{
			name: "invalid update - negative replicas",
			obj: &enterpriseApi.IndexerCluster{
				Spec: enterpriseApi.IndexerClusterSpec{
					Replicas: -1,
				},
			},
			oldObj: &enterpriseApi.IndexerCluster{
				Spec: enterpriseApi.IndexerClusterSpec{
					Replicas: 3,
				},
			},
			wantErrCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := ValidateIndexerClusterUpdate(tt.obj, tt.oldObj)
			if len(errs) != tt.wantErrCount {
				t.Errorf("ValidateIndexerClusterUpdate() got %d errors, want %d. Errors: %v", len(errs), tt.wantErrCount, errs)
			}
		})
	}
}

func TestGetIndexerClusterWarningsOnCreate(t *testing.T) {
	obj := &enterpriseApi.IndexerCluster{
		Spec: enterpriseApi.IndexerClusterSpec{
			Replicas: 3,
		},
	}

	warnings := GetIndexerClusterWarningsOnCreate(obj)
	if len(warnings) != 0 {
		t.Errorf("GetIndexerClusterWarningsOnCreate() returned %d warnings, expected 0", len(warnings))
	}
}

func TestGetIndexerClusterWarningsOnUpdate(t *testing.T) {
	obj := &enterpriseApi.IndexerCluster{
		Spec: enterpriseApi.IndexerClusterSpec{
			Replicas: 3,
		},
	}
	oldObj := &enterpriseApi.IndexerCluster{
		Spec: enterpriseApi.IndexerClusterSpec{
			Replicas: 3,
		},
	}

	warnings := GetIndexerClusterWarningsOnUpdate(obj, oldObj)
	if len(warnings) != 0 {
		t.Errorf("GetIndexerClusterWarningsOnUpdate() returned %d warnings, expected 0", len(warnings))
	}
}
