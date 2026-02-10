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

func TestValidateClusterManagerCreate(t *testing.T) {
	tests := []struct {
		name         string
		obj          *enterpriseApi.ClusterManager
		wantErrCount int
		wantErrField string
	}{
		{
			name:         "valid cluster manager - minimal",
			obj:          &enterpriseApi.ClusterManager{},
			wantErrCount: 0,
		},
		{
			name: "valid cluster manager - with common spec",
			obj: &enterpriseApi.ClusterManager{
				Spec: enterpriseApi.ClusterManagerSpec{
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
			name: "invalid cluster manager - invalid image pull policy",
			obj: &enterpriseApi.ClusterManager{
				Spec: enterpriseApi.ClusterManagerSpec{
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
			name: "valid cluster manager - with SmartStore",
			obj: &enterpriseApi.ClusterManager{
				Spec: enterpriseApi.ClusterManagerSpec{
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
			name: "invalid cluster manager - SmartStore volume without name",
			obj: &enterpriseApi.ClusterManager{
				Spec: enterpriseApi.ClusterManagerSpec{
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
			name: "invalid cluster manager - SmartStore volume without endpoint or path",
			obj: &enterpriseApi.ClusterManager{
				Spec: enterpriseApi.ClusterManagerSpec{
					SmartStore: enterpriseApi.SmartStoreSpec{
						VolList: []enterpriseApi.VolumeSpec{
							{Name: "vol1", Endpoint: "", Path: ""},
						},
					},
				},
			},
			wantErrCount: 1,
			wantErrField: "spec.smartstore.volumes[0]",
		},
		{
			name: "valid cluster manager - with AppFramework",
			obj: &enterpriseApi.ClusterManager{
				Spec: enterpriseApi.ClusterManagerSpec{
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
			name: "invalid cluster manager - AppFramework source without name",
			obj: &enterpriseApi.ClusterManager{
				Spec: enterpriseApi.ClusterManagerSpec{
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
			name: "invalid cluster manager - multiple errors",
			obj: &enterpriseApi.ClusterManager{
				Spec: enterpriseApi.ClusterManagerSpec{
					CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
						Spec: enterpriseApi.Spec{
							ImagePullPolicy: "InvalidPolicy",
						},
					},
					SmartStore: enterpriseApi.SmartStoreSpec{
						VolList: []enterpriseApi.VolumeSpec{
							{Name: "", Endpoint: ""},
						},
					},
				},
			},
			wantErrCount: 3, // invalid policy + missing name + missing endpoint/path
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := ValidateClusterManagerCreate(tt.obj)
			assert.Len(t, errs, tt.wantErrCount, "unexpected error count")
			if tt.wantErrField != "" && len(errs) > 0 {
				assert.Equal(t, tt.wantErrField, errs[0].Field, "unexpected error field")
			}
		})
	}
}

func TestValidateClusterManagerUpdate(t *testing.T) {
	tests := []struct {
		name         string
		obj          *enterpriseApi.ClusterManager
		oldObj       *enterpriseApi.ClusterManager
		wantErrCount int
	}{
		{
			name:         "valid update - no changes",
			obj:          &enterpriseApi.ClusterManager{},
			oldObj:       &enterpriseApi.ClusterManager{},
			wantErrCount: 0,
		},
		{
			name: "valid update - add SmartStore",
			obj: &enterpriseApi.ClusterManager{
				Spec: enterpriseApi.ClusterManagerSpec{
					SmartStore: enterpriseApi.SmartStoreSpec{
						VolList: []enterpriseApi.VolumeSpec{
							{Name: "vol1", Endpoint: "s3://bucket"},
						},
					},
				},
			},
			oldObj:       &enterpriseApi.ClusterManager{},
			wantErrCount: 0,
		},
		{
			name: "invalid update - invalid SmartStore config",
			obj: &enterpriseApi.ClusterManager{
				Spec: enterpriseApi.ClusterManagerSpec{
					SmartStore: enterpriseApi.SmartStoreSpec{
						VolList: []enterpriseApi.VolumeSpec{
							{Name: "", Endpoint: ""},
						},
					},
				},
			},
			oldObj:       &enterpriseApi.ClusterManager{},
			wantErrCount: 2, // missing name + missing endpoint/path
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := ValidateClusterManagerUpdate(tt.obj, tt.oldObj)
			assert.Len(t, errs, tt.wantErrCount, "unexpected error count")
		})
	}
}

func TestGetClusterManagerWarningsOnCreate(t *testing.T) {
	obj := &enterpriseApi.ClusterManager{}
	warnings := GetClusterManagerWarningsOnCreate(obj)
	assert.Empty(t, warnings, "expected no warnings")
}

func TestGetClusterManagerWarningsOnUpdate(t *testing.T) {
	obj := &enterpriseApi.ClusterManager{}
	oldObj := &enterpriseApi.ClusterManager{}
	warnings := GetClusterManagerWarningsOnUpdate(obj, oldObj)
	assert.Empty(t, warnings, "expected no warnings")
}
