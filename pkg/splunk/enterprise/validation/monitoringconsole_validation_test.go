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

func TestValidateMonitoringConsoleCreate(t *testing.T) {
	tests := []struct {
		name         string
		obj          *enterpriseApi.MonitoringConsole
		wantErrCount int
		wantErrField string
	}{
		{
			name:         "valid monitoring console - minimal",
			obj:          &enterpriseApi.MonitoringConsole{},
			wantErrCount: 0,
		},
		{
			name: "valid monitoring console - with common spec",
			obj: &enterpriseApi.MonitoringConsole{
				Spec: enterpriseApi.MonitoringConsoleSpec{
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
			name: "invalid monitoring console - invalid image pull policy",
			obj: &enterpriseApi.MonitoringConsole{
				Spec: enterpriseApi.MonitoringConsoleSpec{
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
			name: "valid monitoring console - with storage config",
			obj: &enterpriseApi.MonitoringConsole{
				Spec: enterpriseApi.MonitoringConsoleSpec{
					CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
						VarVolumeStorageConfig: enterpriseApi.StorageClassSpec{
							StorageCapacity:  "100Gi",
							StorageClassName: "standard",
						},
					},
				},
			},
			wantErrCount: 0,
		},
		{
			name: "invalid monitoring console - invalid storage capacity format",
			obj: &enterpriseApi.MonitoringConsole{
				Spec: enterpriseApi.MonitoringConsoleSpec{
					CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
						VarVolumeStorageConfig: enterpriseApi.StorageClassSpec{
							StorageCapacity:  "100GB",
							StorageClassName: "standard",
						},
					},
				},
			},
			wantErrCount: 1,
			wantErrField: "spec.varVolumeStorageConfig.storageCapacity",
		},
		{
			name: "invalid monitoring console - missing storageClassName for persistent storage",
			obj: &enterpriseApi.MonitoringConsole{
				Spec: enterpriseApi.MonitoringConsoleSpec{
					CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
						VarVolumeStorageConfig: enterpriseApi.StorageClassSpec{
							StorageCapacity:  "100Gi",
							EphemeralStorage: false,
						},
					},
				},
			},
			wantErrCount: 1,
			wantErrField: "spec.varVolumeStorageConfig.storageClassName",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := ValidateMonitoringConsoleCreate(tt.obj)
			if len(errs) != tt.wantErrCount {
				t.Errorf("ValidateMonitoringConsoleCreate() got %d errors, want %d. Errors: %v", len(errs), tt.wantErrCount, errs)
			}
			if tt.wantErrField != "" && len(errs) > 0 {
				if errs[0].Field != tt.wantErrField {
					t.Errorf("ValidateMonitoringConsoleCreate() error field = %s, want %s", errs[0].Field, tt.wantErrField)
				}
			}
		})
	}
}

func TestValidateMonitoringConsoleUpdate(t *testing.T) {
	tests := []struct {
		name         string
		obj          *enterpriseApi.MonitoringConsole
		oldObj       *enterpriseApi.MonitoringConsole
		wantErrCount int
	}{
		{
			name:         "valid update - no changes",
			obj:          &enterpriseApi.MonitoringConsole{},
			oldObj:       &enterpriseApi.MonitoringConsole{},
			wantErrCount: 0,
		},
		{
			name: "valid update - change image pull policy",
			obj: &enterpriseApi.MonitoringConsole{
				Spec: enterpriseApi.MonitoringConsoleSpec{
					CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
						Spec: enterpriseApi.Spec{
							ImagePullPolicy: "Never",
						},
					},
				},
			},
			oldObj: &enterpriseApi.MonitoringConsole{
				Spec: enterpriseApi.MonitoringConsoleSpec{
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
			errs := ValidateMonitoringConsoleUpdate(tt.obj, tt.oldObj)
			if len(errs) != tt.wantErrCount {
				t.Errorf("ValidateMonitoringConsoleUpdate() got %d errors, want %d. Errors: %v", len(errs), tt.wantErrCount, errs)
			}
		})
	}
}

func TestGetMonitoringConsoleWarningsOnCreate(t *testing.T) {
	obj := &enterpriseApi.MonitoringConsole{}

	warnings := GetMonitoringConsoleWarningsOnCreate(obj)
	if len(warnings) != 0 {
		t.Errorf("GetMonitoringConsoleWarningsOnCreate() returned %d warnings, expected 0", len(warnings))
	}
}

func TestGetMonitoringConsoleWarningsOnUpdate(t *testing.T) {
	obj := &enterpriseApi.MonitoringConsole{}
	oldObj := &enterpriseApi.MonitoringConsole{}

	warnings := GetMonitoringConsoleWarningsOnUpdate(obj, oldObj)
	if len(warnings) != 0 {
		t.Errorf("GetMonitoringConsoleWarningsOnUpdate() returned %d warnings, expected 0", len(warnings))
	}
}
