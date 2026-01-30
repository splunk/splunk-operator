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

func TestValidateLicenseManagerCreate(t *testing.T) {
	tests := []struct {
		name         string
		obj          *enterpriseApi.LicenseManager
		wantErrCount int
		wantErrField string
	}{
		{
			name:         "valid license manager - minimal",
			obj:          &enterpriseApi.LicenseManager{},
			wantErrCount: 0,
		},
		{
			name: "valid license manager - with common spec",
			obj: &enterpriseApi.LicenseManager{
				Spec: enterpriseApi.LicenseManagerSpec{
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
			name: "invalid license manager - invalid image pull policy",
			obj: &enterpriseApi.LicenseManager{
				Spec: enterpriseApi.LicenseManagerSpec{
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
			name: "valid license manager - with storage config",
			obj: &enterpriseApi.LicenseManager{
				Spec: enterpriseApi.LicenseManagerSpec{
					CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
						EtcVolumeStorageConfig: enterpriseApi.StorageClassSpec{
							StorageCapacity:  "10Gi",
							StorageClassName: "standard",
						},
					},
				},
			},
			wantErrCount: 0,
		},
		{
			name: "invalid license manager - invalid storage capacity format",
			obj: &enterpriseApi.LicenseManager{
				Spec: enterpriseApi.LicenseManagerSpec{
					CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
						EtcVolumeStorageConfig: enterpriseApi.StorageClassSpec{
							StorageCapacity:  "10GB",
							StorageClassName: "standard",
						},
					},
				},
			},
			wantErrCount: 1,
			wantErrField: "spec.etcVolumeStorageConfig.storageCapacity",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := ValidateLicenseManagerCreate(tt.obj)
			if len(errs) != tt.wantErrCount {
				t.Errorf("ValidateLicenseManagerCreate() got %d errors, want %d. Errors: %v", len(errs), tt.wantErrCount, errs)
			}
			if tt.wantErrField != "" && len(errs) > 0 {
				if errs[0].Field != tt.wantErrField {
					t.Errorf("ValidateLicenseManagerCreate() error field = %s, want %s", errs[0].Field, tt.wantErrField)
				}
			}
		})
	}
}

func TestValidateLicenseManagerUpdate(t *testing.T) {
	tests := []struct {
		name         string
		obj          *enterpriseApi.LicenseManager
		oldObj       *enterpriseApi.LicenseManager
		wantErrCount int
	}{
		{
			name:         "valid update - no changes",
			obj:          &enterpriseApi.LicenseManager{},
			oldObj:       &enterpriseApi.LicenseManager{},
			wantErrCount: 0,
		},
		{
			name: "valid update - change image pull policy",
			obj: &enterpriseApi.LicenseManager{
				Spec: enterpriseApi.LicenseManagerSpec{
					CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
						Spec: enterpriseApi.Spec{
							ImagePullPolicy: "Never",
						},
					},
				},
			},
			oldObj: &enterpriseApi.LicenseManager{
				Spec: enterpriseApi.LicenseManagerSpec{
					CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
						Spec: enterpriseApi.Spec{
							ImagePullPolicy: "Always",
						},
					},
				},
			},
			wantErrCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := ValidateLicenseManagerUpdate(tt.obj, tt.oldObj)
			if len(errs) != tt.wantErrCount {
				t.Errorf("ValidateLicenseManagerUpdate() got %d errors, want %d. Errors: %v", len(errs), tt.wantErrCount, errs)
			}
		})
	}
}

func TestGetLicenseManagerWarningsOnCreate(t *testing.T) {
	obj := &enterpriseApi.LicenseManager{}

	warnings := GetLicenseManagerWarningsOnCreate(obj)
	if len(warnings) != 0 {
		t.Errorf("GetLicenseManagerWarningsOnCreate() returned %d warnings, expected 0", len(warnings))
	}
}

func TestGetLicenseManagerWarningsOnUpdate(t *testing.T) {
	obj := &enterpriseApi.LicenseManager{}
	oldObj := &enterpriseApi.LicenseManager{}

	warnings := GetLicenseManagerWarningsOnUpdate(obj, oldObj)
	if len(warnings) != 0 {
		t.Errorf("GetLicenseManagerWarningsOnUpdate() returned %d warnings, expected 0", len(warnings))
	}
}
