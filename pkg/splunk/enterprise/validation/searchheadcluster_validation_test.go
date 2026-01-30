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

func TestValidateSearchHeadClusterCreate(t *testing.T) {
	tests := []struct {
		name         string
		obj          *enterpriseApi.SearchHeadCluster
		wantErrCount int
		wantErrField string
	}{
		{
			name: "valid search head cluster - minimal",
			obj: &enterpriseApi.SearchHeadCluster{
				Spec: enterpriseApi.SearchHeadClusterSpec{
					Replicas: 3,
				},
			},
			wantErrCount: 0,
		},
		{
			name: "valid search head cluster - zero replicas",
			obj: &enterpriseApi.SearchHeadCluster{
				Spec: enterpriseApi.SearchHeadClusterSpec{
					Replicas: 0,
				},
			},
			wantErrCount: 0,
		},
		{
			name: "invalid search head cluster - negative replicas",
			obj: &enterpriseApi.SearchHeadCluster{
				Spec: enterpriseApi.SearchHeadClusterSpec{
					Replicas: -1,
				},
			},
			wantErrCount: 1,
			wantErrField: "spec.replicas",
		},
		{
			name: "valid search head cluster - with AppFramework",
			obj: &enterpriseApi.SearchHeadCluster{
				Spec: enterpriseApi.SearchHeadClusterSpec{
					Replicas: 3,
					AppFrameworkConfig: enterpriseApi.AppFrameworkSpec{
						VolList: []enterpriseApi.VolumeSpec{
							{Name: "appvol", Endpoint: "s3://apps"},
						},
						AppSources: []enterpriseApi.AppSourceSpec{
							{Name: "apps", Location: "/apps"},
						},
					},
				},
			},
			wantErrCount: 0,
		},
		{
			name: "invalid search head cluster - AppFramework source without name",
			obj: &enterpriseApi.SearchHeadCluster{
				Spec: enterpriseApi.SearchHeadClusterSpec{
					Replicas: 3,
					AppFrameworkConfig: enterpriseApi.AppFrameworkSpec{
						AppSources: []enterpriseApi.AppSourceSpec{
							{Name: "", Location: "/apps"},
						},
					},
				},
			},
			wantErrCount: 1,
			wantErrField: "spec.appRepo.appSources[0].name",
		},
		{
			name: "invalid search head cluster - AppFramework source without location",
			obj: &enterpriseApi.SearchHeadCluster{
				Spec: enterpriseApi.SearchHeadClusterSpec{
					Replicas: 3,
					AppFrameworkConfig: enterpriseApi.AppFrameworkSpec{
						AppSources: []enterpriseApi.AppSourceSpec{
							{Name: "apps", Location: ""},
						},
					},
				},
			},
			wantErrCount: 1,
			wantErrField: "spec.appRepo.appSources[0].location",
		},
		{
			name: "invalid search head cluster - multiple errors",
			obj: &enterpriseApi.SearchHeadCluster{
				Spec: enterpriseApi.SearchHeadClusterSpec{
					Replicas: -1,
					AppFrameworkConfig: enterpriseApi.AppFrameworkSpec{
						AppSources: []enterpriseApi.AppSourceSpec{
							{Name: "", Location: ""},
						},
					},
				},
			},
			wantErrCount: 3, // negative replicas + missing name + missing location
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := ValidateSearchHeadClusterCreate(tt.obj)
			if len(errs) != tt.wantErrCount {
				t.Errorf("ValidateSearchHeadClusterCreate() got %d errors, want %d. Errors: %v", len(errs), tt.wantErrCount, errs)
			}
			if tt.wantErrField != "" && len(errs) > 0 {
				if errs[0].Field != tt.wantErrField {
					t.Errorf("ValidateSearchHeadClusterCreate() error field = %s, want %s", errs[0].Field, tt.wantErrField)
				}
			}
		})
	}
}

func TestValidateSearchHeadClusterUpdate(t *testing.T) {
	tests := []struct {
		name         string
		obj          *enterpriseApi.SearchHeadCluster
		oldObj       *enterpriseApi.SearchHeadCluster
		wantErrCount int
	}{
		{
			name: "valid update - same replicas",
			obj: &enterpriseApi.SearchHeadCluster{
				Spec: enterpriseApi.SearchHeadClusterSpec{
					Replicas: 3,
				},
			},
			oldObj: &enterpriseApi.SearchHeadCluster{
				Spec: enterpriseApi.SearchHeadClusterSpec{
					Replicas: 3,
				},
			},
			wantErrCount: 0,
		},
		{
			name: "valid update - scale up",
			obj: &enterpriseApi.SearchHeadCluster{
				Spec: enterpriseApi.SearchHeadClusterSpec{
					Replicas: 5,
				},
			},
			oldObj: &enterpriseApi.SearchHeadCluster{
				Spec: enterpriseApi.SearchHeadClusterSpec{
					Replicas: 3,
				},
			},
			wantErrCount: 0,
		},
		{
			name: "invalid update - negative replicas",
			obj: &enterpriseApi.SearchHeadCluster{
				Spec: enterpriseApi.SearchHeadClusterSpec{
					Replicas: -1,
				},
			},
			oldObj: &enterpriseApi.SearchHeadCluster{
				Spec: enterpriseApi.SearchHeadClusterSpec{
					Replicas: 3,
				},
			},
			wantErrCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := ValidateSearchHeadClusterUpdate(tt.obj, tt.oldObj)
			if len(errs) != tt.wantErrCount {
				t.Errorf("ValidateSearchHeadClusterUpdate() got %d errors, want %d. Errors: %v", len(errs), tt.wantErrCount, errs)
			}
		})
	}
}

func TestGetSearchHeadClusterWarningsOnCreate(t *testing.T) {
	obj := &enterpriseApi.SearchHeadCluster{
		Spec: enterpriseApi.SearchHeadClusterSpec{
			Replicas: 3,
		},
	}

	warnings := GetSearchHeadClusterWarningsOnCreate(obj)
	if len(warnings) != 0 {
		t.Errorf("GetSearchHeadClusterWarningsOnCreate() returned %d warnings, expected 0", len(warnings))
	}
}

func TestGetSearchHeadClusterWarningsOnUpdate(t *testing.T) {
	obj := &enterpriseApi.SearchHeadCluster{
		Spec: enterpriseApi.SearchHeadClusterSpec{
			Replicas: 3,
		},
	}
	oldObj := &enterpriseApi.SearchHeadCluster{
		Spec: enterpriseApi.SearchHeadClusterSpec{
			Replicas: 3,
		},
	}

	warnings := GetSearchHeadClusterWarningsOnUpdate(obj, oldObj)
	if len(warnings) != 0 {
		t.Errorf("GetSearchHeadClusterWarningsOnUpdate() returned %d warnings, expected 0", len(warnings))
	}
}
