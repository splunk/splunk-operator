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
					Replicas: 3,
				},
			},
			wantErrCount: 0,
		},
		{
			name: "invalid indexer cluster - zero replicas",
			obj: &enterpriseApi.IndexerCluster{
				Spec: enterpriseApi.IndexerClusterSpec{
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
			name: "invalid update - scale down below minimum",
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
			wantErrCount: 1,
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
			assert.Len(t, errs, tt.wantErrCount, "unexpected error count")
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
	assert.Empty(t, warnings, "expected no warnings")
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
	assert.Empty(t, warnings, "expected no warnings")
}
