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

func TestValidateStandaloneCreate(t *testing.T) {
	tests := []struct {
		name         string
		obj          *enterpriseApi.Standalone
		wantErrCount int
		wantErrField string
	}{
		{
			name: "valid standalone - minimal",
			obj: &enterpriseApi.Standalone{
				Spec: enterpriseApi.StandaloneSpec{
					Replicas: 1,
				},
			},
			wantErrCount: 0,
		},
		{
			name: "valid standalone - zero replicas",
			obj: &enterpriseApi.Standalone{
				Spec: enterpriseApi.StandaloneSpec{
					Replicas: 0,
				},
			},
			wantErrCount: 0,
		},
		{
			name: "invalid standalone - negative replicas",
			obj: &enterpriseApi.Standalone{
				Spec: enterpriseApi.StandaloneSpec{
					Replicas: -1,
				},
			},
			wantErrCount: 1,
			wantErrField: "spec.replicas",
		},
		{
			name: "valid standalone - with SmartStore",
			obj: &enterpriseApi.Standalone{
				Spec: enterpriseApi.StandaloneSpec{
					Replicas: 1,
					SmartStore: enterpriseApi.SmartStoreSpec{
						VolList: []enterpriseApi.VolumeSpec{
							{Name: "vol1", Endpoint: "s3://bucket"},
						},
						IndexList: []enterpriseApi.IndexSpec{
							{Name: "idx1", IndexAndGlobalCommonSpec: enterpriseApi.IndexAndGlobalCommonSpec{VolName: "vol1"}},
						},
					},
				},
			},
			wantErrCount: 0,
		},
		{
			name: "invalid standalone - SmartStore volume without name",
			obj: &enterpriseApi.Standalone{
				Spec: enterpriseApi.StandaloneSpec{
					Replicas: 1,
					SmartStore: enterpriseApi.SmartStoreSpec{
						VolList: []enterpriseApi.VolumeSpec{
							{Name: "", Endpoint: "s3://bucket"},
						},
					},
				},
			},
			wantErrCount: 1,
			wantErrField: "spec.smartstore.volumes[0].name",
		},
		{
			name: "valid standalone - with AppFramework",
			obj: &enterpriseApi.Standalone{
				Spec: enterpriseApi.StandaloneSpec{
					Replicas: 1,
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
			name: "invalid standalone - AppFramework source without name",
			obj: &enterpriseApi.Standalone{
				Spec: enterpriseApi.StandaloneSpec{
					Replicas: 1,
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
			name: "invalid standalone - multiple errors",
			obj: &enterpriseApi.Standalone{
				Spec: enterpriseApi.StandaloneSpec{
					Replicas: -1,
					SmartStore: enterpriseApi.SmartStoreSpec{
						VolList: []enterpriseApi.VolumeSpec{
							{Name: "", Endpoint: ""},
						},
					},
				},
			},
			wantErrCount: 3, // negative replicas + missing name + missing endpoint/path
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := ValidateStandaloneCreate(tt.obj)
			assert.Len(t, errs, tt.wantErrCount, "unexpected error count")
			if tt.wantErrField != "" && len(errs) > 0 {
				assert.Equal(t, tt.wantErrField, errs[0].Field, "unexpected error field")
			}
		})
	}
}

func TestValidateStandaloneUpdate(t *testing.T) {
	tests := []struct {
		name         string
		obj          *enterpriseApi.Standalone
		oldObj       *enterpriseApi.Standalone
		wantErrCount int
	}{
		{
			name: "valid update - same replicas",
			obj: &enterpriseApi.Standalone{
				Spec: enterpriseApi.StandaloneSpec{
					Replicas: 1,
				},
			},
			oldObj: &enterpriseApi.Standalone{
				Spec: enterpriseApi.StandaloneSpec{
					Replicas: 1,
				},
			},
			wantErrCount: 0,
		},
		{
			name: "valid update - increase replicas",
			obj: &enterpriseApi.Standalone{
				Spec: enterpriseApi.StandaloneSpec{
					Replicas: 3,
				},
			},
			oldObj: &enterpriseApi.Standalone{
				Spec: enterpriseApi.StandaloneSpec{
					Replicas: 1,
				},
			},
			wantErrCount: 0,
		},
		{
			name: "invalid update - negative replicas",
			obj: &enterpriseApi.Standalone{
				Spec: enterpriseApi.StandaloneSpec{
					Replicas: -1,
				},
			},
			oldObj: &enterpriseApi.Standalone{
				Spec: enterpriseApi.StandaloneSpec{
					Replicas: 1,
				},
			},
			wantErrCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := ValidateStandaloneUpdate(tt.obj, tt.oldObj)
			assert.Len(t, errs, tt.wantErrCount, "unexpected error count")
		})
	}
}

func TestGetStandaloneWarningsOnCreate(t *testing.T) {
	obj := &enterpriseApi.Standalone{
		Spec: enterpriseApi.StandaloneSpec{
			Replicas: 1,
		},
	}
	warnings := GetStandaloneWarningsOnCreate(obj)
	assert.Empty(t, warnings, "expected no warnings")
}

func TestGetStandaloneWarningsOnUpdate(t *testing.T) {
	obj := &enterpriseApi.Standalone{
		Spec: enterpriseApi.StandaloneSpec{
			Replicas: 1,
		},
	}
	oldObj := &enterpriseApi.Standalone{
		Spec: enterpriseApi.StandaloneSpec{
			Replicas: 1,
		},
	}
	warnings := GetStandaloneWarningsOnUpdate(obj, oldObj)
	assert.Empty(t, warnings, "expected no warnings")
}
